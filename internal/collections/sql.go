package collections

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

type SQLDataset struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Table  string   `json:"table"`
	Fields []string `json:"fields"`
}
type SQLResult struct {
	Columns    []string     `json:"columns"`
	Rows       [][]any      `json:"rows"`
	Truncated  bool         `json:"truncated"`
	DurationMS int64        `json:"duration_ms"`
	Engine     string       `json:"engine,omitempty"`
	Datasets   []SQLDataset `json:"datasets"`
}

// SQLWithEngine selects a processing engine without changing the existing SQL
// API. Auto keeps small interactive queries on SQLite and delegates larger
// analytical queries to DuckDB.
func (s *Service) SQLWithEngine(ctx context.Context, ids []string, text, engine string) (SQLResult, error) {
	engine = strings.ToLower(strings.TrimSpace(engine))
	if engine == "" {
		engine = "auto"
	}
	if engine != "auto" && engine != "native" && engine != "duckdb" {
		return SQLResult{}, fmt.Errorf("engine must be auto, native, or duckdb")
	}
	if engine == "duckdb" || (engine == "auto" && s.shouldUseDuckDB(ids, text)) {
		result, err := s.duckDBSQL(ctx, ids, text)
		if err == nil {
			result.Engine = "DuckDB"
		}
		return result, err
	}
	result, err := s.SQL(ctx, ids, text)
	result.Engine = "SQLite"
	return result, err
}

func (s *Service) shouldUseDuckDB(ids []string, query string) bool {
	parts := strings.Fields(strings.ToUpper(query))
	workload := " " + strings.Join(parts, " ") + " "
	analytical := false
	for _, operation := range []string{" JOIN ", " GROUP BY ", " ORDER BY ", " WHERE ", " HAVING ", " SUM(", " SUM (", " COUNT(", " COUNT (", " AVG(", " AVG (", " MIN(", " MIN (", " MAX(", " MAX ("} {
		if strings.Contains(workload, operation) {
			analytical = true
			break
		}
	}
	if !analytical {
		return false
	}
	var totalRows int64
	for _, id := range ids {
		c, err := s.Get(id)
		if err != nil || c.Status != "ready" {
			return false
		}
		totalRows += c.Rows
	}
	// DuckDB's first cache build is disk-backed and measured at about 1.2s
	// for 250k records on the Windows dev machine; repeated queries are much
	// faster. Large analytical SQL workloads amortize that one-time cost.
	return totalRows >= 250_000
}

// ValidateSQL scans SQL tokens, ignoring quoted literals, identifiers and
// comments. Only one SELECT/WITH statement is allowed, including in CTEs.
func ValidateSQL(text string) error {
	if len(text) == 0 || len(text) > 16384 {
		return fmt.Errorf("SQL must contain between 1 and 16384 bytes")
	}
	if !utf8.ValidString(text) {
		return fmt.Errorf("SQL must be valid UTF-8")
	}
	words := []string{}
	terminated := false
	for i := 0; i < len(text); {
		c := text[i]
		if unicode.IsSpace(rune(c)) {
			i++
			continue
		}
		if c == '-' && i+1 < len(text) && text[i+1] == '-' {
			for i < len(text) && text[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(text) && text[i+1] == '*' {
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return fmt.Errorf("unterminated SQL comment")
			}
			i += end + 4
			continue
		}
		if terminated {
			return fmt.Errorf("only one SQL statement is allowed")
		}
		if c == ';' {
			terminated = true
			i++
			continue
		}
		if c == '\'' || c == '"' || c == '`' || c == '[' {
			end := c
			if c == '[' {
				end = ']'
			}
			i++
			start := i
			closed := false
			for i < len(text) {
				if text[i] == end {
					if i+1 < len(text) && text[i+1] == end && end != ']' {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return fmt.Errorf("unterminated SQL quote")
			}
			if c != '\'' {
				name := strings.ToUpper(text[start : i-1])
				if name == "LOAD_EXTENSION" || name == "READFILE" || name == "WRITEFILE" || strings.HasPrefix(name, "PRAGMA_") {
					return fmt.Errorf("SQL function is not allowed")
				}
			}
			continue
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
			start := i
			i++
			for i < len(text) && ((text[i] >= 'a' && text[i] <= 'z') || (text[i] >= 'A' && text[i] <= 'Z') || (text[i] >= '0' && text[i] <= '9') || text[i] == '_') {
				i++
			}
			word := strings.ToUpper(text[start:i])
			words = append(words, word)
			continue
		}
		i++
	}
	if len(words) == 0 || (words[0] != "SELECT" && words[0] != "WITH") {
		return fmt.Errorf("only read-only SELECT and WITH queries are supported")
	}
	blocked := map[string]bool{"ATTACH": true, "DETACH": true, "PRAGMA": true, "INSERT": true, "UPDATE": true, "DELETE": true, "REPLACE": true, "CREATE": true, "DROP": true, "ALTER": true, "VACUUM": true, "REINDEX": true, "ANALYZE": true, "BEGIN": true, "COMMIT": true, "ROLLBACK": true, "SAVEPOINT": true, "RELEASE": true, "LOAD_EXTENSION": true, "READFILE": true, "WRITEFILE": true}
	for _, word := range words {
		if blocked[word] || strings.HasPrefix(word, "PRAGMA_") {
			return fmt.Errorf("SQL operation %s is not allowed", word)
		}
	}
	return nil
}
func quoteIdentifier(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
func quoteSQL(text string) string        { return "'" + strings.ReplaceAll(text, "'", "''") + "'" }

// The SQL connection contains only disposable record indexes chosen by the
// caller. The metadata database is never attached or exposed to user SQL.
func (s *Service) SQL(ctx context.Context, ids []string, text string) (SQLResult, error) {
	return s.sqlRun(ctx, ids, text, 1000, 10*time.Second, nil, nil)
}

func (s *Service) SQLStream(ctx context.Context, ids []string, text string, emit func([]string, []any) error) error {
	_, err := s.sqlRun(ctx, ids, text, 0, 30*time.Minute, emit, nil)
	return err
}

func (s *Service) sqlRun(ctx context.Context, ids []string, text string, rowLimit int, timeout time.Duration, emit func([]string, []any) error, expected map[string]string) (out SQLResult, err error) {
	releases := []func(){}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	start := time.Now()
	defer func() { out.DurationMS = time.Since(start).Milliseconds() }()
	out.Rows = [][]any{}
	if err = ValidateSQL(text); err != nil {
		return out, err
	}
	if len(ids) == 0 || len(ids) > 8 {
		return out, fmt.Errorf("choose between 1 and 8 collections")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err = s.DiskCheck(os.TempDir(), s.Reserve); err != nil {
		return out, err
	}
	diskErrors := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if e := s.DiskCheck(os.TempDir(), s.Reserve); e != nil {
					diskErrors <- e
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		cancel()
		<-done
		select {
		case e := <-diskErrors:
			err = e
		default:
		}
	}()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return out, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return out, err
	}
	defer conn.Close()
	for id, value := range map[int]int{sqlitelib.SQLITE_LIMIT_LENGTH: 2 << 20, sqlitelib.SQLITE_LIMIT_SQL_LENGTH: 16384, sqlitelib.SQLITE_LIMIT_COLUMN: 256, sqlitelib.SQLITE_LIMIT_EXPR_DEPTH: 100, sqlitelib.SQLITE_LIMIT_COMPOUND_SELECT: 50, sqlitelib.SQLITE_LIMIT_VDBE_OP: 100000, sqlitelib.SQLITE_LIMIT_ATTACHED: 8, sqlitelib.SQLITE_LIMIT_FUNCTION_ARG: 32} {
		if _, err = sqlite.Limit(conn, id, value); err != nil {
			return out, err
		}
	}
	for _, pragma := range []string{`PRAGMA temp_store=FILE`, `PRAGMA cache_size=-4096`, `PRAGMA trusted_schema=OFF`} {
		if _, err = conn.ExecContext(ctx, pragma); err != nil {
			return out, err
		}
	}
	seen := map[string]bool{}
	for i, id := range ids {
		if seen[id] {
			return out, fmt.Errorf("collection selected twice")
		}
		seen[id] = true
		var c Collection
		c, err = s.Get(id)
		if err != nil {
			return out, err
		}
		c, err = s.ready(c)
		if err != nil {
			return out, err
		}
		unlock := s.lock(c.ID + "-generation")
		c, err = s.Get(id)
		if err != nil {
			unlock()
			return out, err
		}
		if c.Status != "ready" || c.Generation == "" || filepath.Base(c.Generation) != c.Generation {
			unlock()
			return out, ErrPending
		}
		if expected != nil && expected[c.ID] != c.Generation {
			unlock()
			return out, ErrConflict
		}
		s.retainGeneration(c.Generation)
		collectionID, generation := c.ID, c.Generation
		releases = append(releases, func() { s.releaseGeneration(collectionID, generation) })
		alias := fmt.Sprintf("data%d", i)
		indexPath := filepath.ToSlash(filepath.Join(s.IndexDir, c.Generation))
		if len(indexPath) > 1 && indexPath[1] == ':' {
			indexPath = "/" + indexPath
		}
		uri := (&url.URL{Scheme: "file", Path: indexPath, RawQuery: "mode=ro&immutable=1"}).String()
		_, err = conn.ExecContext(ctx, `ATTACH DATABASE ? AS `+quoteIdentifier(alias), uri)
		unlock()
		if err != nil {
			return out, err
		}
		fields, e := s.Fields(c)
		if e != nil {
			return out, e
		}
		if emit != nil {
			fields = nil
		}
		if len(fields) > 128 {
			return out, fmt.Errorf("SQL supports up to 128 fields per collection")
		}
		dataset := SQLDataset{ID: id, Name: c.Name, Table: "c_" + id, Fields: []string{"_record_id", "_ordinal", "_json"}}
		columns := []string{`record_id AS "_record_id"`, `ordinal AS "_ordinal"`, `payload AS "_json"`}
		for _, field := range fields {
			if field.Name == "_record_id" || field.Name == "_ordinal" || field.Name == "_json" {
				continue
			}
			path, e := jsonPath("/" + strings.ReplaceAll(strings.ReplaceAll(field.Name, "~", "~0"), "/", "~1"))
			if e != nil {
				continue
			}
			columns = append(columns, `json_extract(payload,`+quoteSQL(path)+`) AS `+quoteIdentifier(field.Name))
			dataset.Fields = append(dataset.Fields, field.Name)
		}
		if _, err = conn.ExecContext(ctx, `CREATE TEMP VIEW `+quoteIdentifier(dataset.Table)+` AS SELECT `+strings.Join(columns, ",")+` FROM `+quoteIdentifier(alias)+`.records`); err != nil {
			return out, err
		}
		out.Datasets = append(out.Datasets, dataset)
	}
	if _, err = conn.ExecContext(ctx, `PRAGMA query_only=ON`); err != nil {
		return out, err
	}
	rows, err := conn.QueryContext(ctx, text)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Columns, err = rows.Columns()
	if err != nil {
		return out, err
	}
	if len(out.Columns) > 128 {
		return out, fmt.Errorf("result exceeds 128 columns")
	}
	bytes := 0
	count := 0
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		if emit != nil {
			bytes = 0
		}
		if rowLimit > 0 && count >= rowLimit {
			out.Truncated = true
			break
		}
		row := make([]any, len(out.Columns))
		ptrs := make([]any, len(row))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			return out, err
		}
		for i, v := range row {
			if b, ok := v.([]byte); ok {
				row[i] = base64.StdEncoding.EncodeToString(b)
			}
			if text, ok := v.(string); ok && !utf8.ValidString(text) {
				return out, fmt.Errorf("SQL result contains invalid UTF-8 text")
			}
			if number, ok := v.(float64); ok && (math.IsInf(number, 0) || math.IsNaN(number)) {
				return out, fmt.Errorf("SQL result contains a non-finite number")
			}
			bytes += len(fmt.Sprint(row[i]))
			if bytes > 8<<20 {
				return out, fmt.Errorf("SQL results exceed 8 MiB; select fewer columns or use a narrower filter")
			}
		}
		if emit != nil {
			if err = emit(out.Columns, row); err != nil {
				return out, err
			}
		} else {
			out.Rows = append(out.Rows, row)
		}
		count++
	}
	return out, rows.Err()
}
