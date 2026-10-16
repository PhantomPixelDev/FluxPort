package collections

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"fluxport/internal/conversion"
	"fluxport/internal/quality"
)

type CompareRequest struct {
	Other string   `json:"other"`
	Keys  []string `json:"keys"`
}
type RowDifference struct {
	Kind   string `json:"kind"`
	Key    any    `json:"key"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}
type Comparison struct {
	Before, After                                string
	BeforeGeneration, AfterGeneration            string
	Added, Removed, Changed, Unchanged           int64
	AddedColumns, RemovedColumns, ChangedColumns []string
	Sample                                       []RowDifference
	DurationMS                                   int64
}

// Compare uses a private disk-backed join, retaining only twenty sample records.
// Missing keys and duplicate keys are errors rather than arbitrary row pairing.
func (s *Service) Compare(ctx context.Context, id string, request CompareRequest) (out Comparison, err error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if len(request.Keys) == 0 || len(request.Keys) > 8 {
		return out, fmt.Errorf("choose 1–8 literal key columns")
	}
	seen := map[string]bool{}
	for _, key := range request.Keys {
		if key == "" || len(key) > 256 || seen[key] {
			return out, fmt.Errorf("key columns must be distinct and 1–256 bytes")
		}
		seen[key] = true
	}
	a, err := s.Get(id)
	if err != nil {
		return out, err
	}
	b, err := s.Get(request.Other)
	if err != nil {
		return out, err
	}
	ar, err := s.sourceReference(a, "")
	if err != nil {
		return out, err
	}
	defer ar.release()
	br, err := s.sourceReference(b, "")
	if err != nil {
		return out, err
	}
	defer br.release()
	if err = s.DiskCheck(s.DownloadDir, s.Reserve); err != nil {
		return out, err
	}
	f, err := os.CreateTemp(s.DownloadDir, ".compare-*.sqlite")
	if err != nil {
		return out, err
	}
	name := f.Name()
	f.Close()
	defer os.Remove(name)
	db, err := sql.Open("sqlite", name)
	if err != nil {
		return out, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, `PRAGMA cache_size=-4096; PRAGMA temp_store=FILE; CREATE TABLE before_rows(k TEXT PRIMARY KEY,p TEXT NOT NULL) WITHOUT ROWID; CREATE TABLE after_rows(k TEXT PRIMARY KEY,p TEXT NOT NULL) WITHOUT ROWID;`); err != nil {
		return out, err
	}
	for i, ref := range []indexReference{ar, br} {
		table := []string{"before_rows", "after_rows"}[i]
		tx, e := db.BeginTx(ctx, nil)
		if e != nil {
			return out, e
		}
		stmt, e := tx.PrepareContext(ctx, "INSERT INTO "+table+" VALUES(?,?)")
		if e != nil {
			tx.Rollback()
			return out, e
		}
		var n int64
		e = s.eachReference(ctx, ref, Query{}, func(row any) error {
			obj, ok := row.(map[string]any)
			if !ok {
				return fmt.Errorf("comparison requires object records")
			}
			key := make([]any, len(request.Keys))
			for j, k := range request.Keys {
				v, ok := obj[k]
				if !ok || v == nil {
					return fmt.Errorf("key %q is missing or null at row %d", k, n+1)
				}
				switch v.(type) {
				case string, bool, json.Number:
				default:
					return fmt.Errorf("key %q must be a scalar", k)
				}
				key[j] = v
			}
			kb, e := conversion.MarshalJSON(key)
			if e != nil {
				return e
			}
			payload, e := conversion.MarshalJSON(row)
			if e != nil {
				return e
			}
			if _, e = stmt.ExecContext(ctx, string(kb), string(payload)); e != nil {
				return fmt.Errorf("comparison requires unique keys in each file (row %d): %w", n+1, e)
			}
			n++
			if n%1000 == 0 {
				return s.DiskCheck(s.DownloadDir, s.Reserve)
			}
			return nil
		})
		stmt.Close()
		if e != nil {
			tx.Rollback()
			return out, e
		}
		if e = tx.Commit(); e != nil {
			return out, e
		}
	}
	out = Comparison{Before: a.Name, After: b.Name, BeforeGeneration: ar.generation, AfterGeneration: br.generation, Sample: []RowDifference{}, AddedColumns: []string{}, RemovedColumns: []string{}, ChangedColumns: []string{}}
	if err = db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM after_rows b WHERE NOT EXISTS(SELECT 1 FROM before_rows a WHERE a.k=b.k)),(SELECT count(*) FROM before_rows a WHERE NOT EXISTS(SELECT 1 FROM after_rows b WHERE b.k=a.k)),(SELECT count(*) FROM before_rows a JOIN after_rows b USING(k) WHERE a.p<>b.p),(SELECT count(*) FROM before_rows a JOIN after_rows b USING(k) WHERE a.p=b.p)`).Scan(&out.Added, &out.Removed, &out.Changed, &out.Unchanged); err != nil {
		return out, err
	}
	rows, err := db.QueryContext(ctx, `SELECT 'changed',a.k,a.p,b.p FROM before_rows a JOIN after_rows b USING(k) WHERE a.p<>b.p UNION ALL SELECT 'removed',a.k,a.p,NULL FROM before_rows a WHERE NOT EXISTS(SELECT 1 FROM after_rows b WHERE b.k=a.k) UNION ALL SELECT 'added',b.k,NULL,b.p FROM after_rows b WHERE NOT EXISTS(SELECT 1 FROM before_rows a WHERE a.k=b.k) LIMIT 20`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	bytes := 0
	for rows.Next() {
		var d RowDifference
		var key string
		var before, after sql.NullString
		if err = rows.Scan(&d.Kind, &key, &before, &after); err != nil {
			return out, err
		}
		bytes += len(key) + len(before.String) + len(after.String)
		if bytes > 8<<20 {
			break
		}
		d.Key, err = conversion.Decode([]byte(key), "json")
		if err != nil {
			return out, err
		}
		if before.Valid {
			d.Before, err = conversion.Decode([]byte(before.String), "json")
			if err != nil {
				return out, err
			}
		}
		if after.Valid {
			d.After, err = conversion.Decode([]byte(after.String), "json")
			if err != nil {
				return out, err
			}
		}
		out.Sample = append(out.Sample, d)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	left := map[string]string{}
	right := map[string]string{}
	for _, f := range ar.fields {
		v, _ := json.Marshal(f)
		left[f.Name] = string(v)
	}
	for _, f := range br.fields {
		v, _ := json.Marshal(f)
		right[f.Name] = string(v)
	}
	for k, v := range left {
		if w, ok := right[k]; !ok {
			out.RemovedColumns = append(out.RemovedColumns, k)
		} else if v != w {
			out.ChangedColumns = append(out.ChangedColumns, k)
		}
	}
	for k := range right {
		if _, ok := left[k]; !ok {
			out.AddedColumns = append(out.AddedColumns, k)
		}
	}
	sort.Strings(out.AddedColumns)
	sort.Strings(out.RemovedColumns)
	sort.Strings(out.ChangedColumns)
	out.DurationMS = time.Since(start).Milliseconds()
	return out, nil
}

func (s *Service) Contract(id string) (*quality.Contract, error) {
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	var text string
	err := s.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, "contract."+id).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c quality.Contract
	err = json.Unmarshal([]byte(text), &c)
	return &c, err
}
func (s *Service) SaveContract(id string, c *quality.Contract) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	if c == nil {
		_, err := s.DB.Exec(`DELETE FROM settings WHERE key=?`, "contract."+id)
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if len(b) > 65536 {
		return fmt.Errorf("contract exceeds 64 KiB")
	}
	_, err = s.DB.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "contract."+id, string(b))
	return err
}

// CaptureContract is explicit: schema inference does not change the source.
func (s *Service) CaptureContract(id string) (*quality.Contract, error) {
	c, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	ref, err := s.sourceReference(c, "")
	if err != nil {
		return nil, err
	}
	defer ref.release()
	out := &quality.Contract{AllowExtra: false}
	for _, f := range ref.fields {
		types := strings.FieldsFunc(f.Types, func(r rune) bool { return r == ',' || r == ' ' || r == '|' })
		out.Columns = append(out.Columns, quality.ContractColumn{Name: f.Name, Types: types, Required: true, Nullable: true})
	}
	if err = out.Validate(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) checkPipelineContract(ctx context.Context, p Pipeline, generations map[string]string) error {
	contract := p.InputContract
	if contract == nil {
		var err error
		contract, err = s.Contract(p.Source)
		if err != nil {
			return err
		}
	}
	if contract == nil {
		return nil
	}
	v, err := quality.NewContractValidator(nil, contract)
	if err != nil {
		return err
	}
	c, err := s.Get(p.Source)
	if err != nil {
		return err
	}
	q := Query{}
	if p.InputQuery != nil {
		q = NormalizeInputQuery(*p.InputQuery)
	}
	return s.EachQuery(ctx, c, q, generations[p.Source], func(row any) error {
		if err := v.Check(row); err != nil {
			return err
		}
		if v.Report.Errors > 0 {
			issue := v.Report.Issues[0]
			return fmt.Errorf("input contract failed at row %d, column %q: expected %s, got %s; review Checks before running", issue.Row, issue.Field, issue.Expected, issue.Actual)
		}
		return nil
	})
}
