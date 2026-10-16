package collections

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxport/internal/jobs"
	"fluxport/internal/quality"
	"fluxport/internal/resources"
)

type qualityExportPayload struct {
	Generation string            `json:"generation"`
	Mode       string            `json:"mode"`
	Rules      []quality.Rule    `json:"rules"`
	Contract   *quality.Contract `json:"contract,omitempty"`
}

func (s *Service) QualityExportJob(id, mode string) (jobs.Job, error) {
	if mode != "stop" && mode != "separate" {
		return jobs.Job{}, fmt.Errorf("choose stop or separate")
	}
	c, err := s.Get(id)
	if err != nil {
		return jobs.Job{}, err
	}
	c, err = s.ready(c)
	if err != nil {
		return jobs.Job{}, err
	}
	rules, err := s.ValidationRules(id)
	if err != nil {
		return jobs.Job{}, err
	}
	contract, err := s.Contract(id)
	if err != nil {
		return jobs.Job{}, err
	}
	if len(rules) == 0 && contract == nil {
		return jobs.Job{}, fmt.Errorf("save validation rules or a contract first")
	}
	b, err := json.Marshal(qualityExportPayload{c.Generation, mode, rules, contract})
	if err != nil {
		return jobs.Job{}, err
	}
	return s.Jobs.Submit("quality_export", id, c.Name+" · checked records", string(b), c.Rows)
}

func (s *Service) qualityExportJob(ctx context.Context, j jobs.Job, progress func(int64, int64)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	var p qualityExportPayload
	decoder := json.NewDecoder(strings.NewReader(j.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&p); err != nil {
		return "", err
	}
	if p.Mode != "stop" && p.Mode != "separate" {
		return "", fmt.Errorf("invalid validation mode")
	}
	c, err := s.Get(j.Resource)
	if err != nil {
		return "", err
	}
	ref, err := s.sourceReference(c, p.Generation)
	if err != nil {
		return "", err
	}
	defer ref.release()
	v, err := quality.NewContractValidator(p.Rules, p.Contract)
	if err != nil {
		return "", err
	}
	if err = s.DiskCheck(s.DownloadDir, s.Reserve); err != nil {
		return "", err
	}
	valid, err := os.CreateTemp(s.DownloadDir, ".quality-valid-*")
	if err != nil {
		return "", err
	}
	defer func() { valid.Close(); os.Remove(valid.Name()) }()
	rejected, err := os.CreateTemp(s.DownloadDir, ".quality-rejected-*")
	if err != nil {
		return "", err
	}
	defer func() { rejected.Close(); os.Remove(rejected.Name()) }()
	acceptedEncoder := json.NewEncoder(&resources.Writer{W: valid, Path: s.DownloadDir, Reserve: s.Reserve, Check: s.DiskCheck})
	rejectedEncoder := json.NewEncoder(&resources.Writer{W: rejected, Path: s.DownloadDir, Reserve: s.Reserve, Check: s.DiskCheck})
	samples := []quality.Issue{}
	err = s.eachReference(ctx, ref, Query{}, func(row any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		before := v.Report.Invalid
		v.Report.Issues = nil
		if err := v.Check(row); err != nil {
			return err
		}
		for _, issue := range v.Report.Issues {
			if len(samples) < 200 {
				samples = append(samples, issue)
			}
		}
		if v.Report.Invalid > before {
			if p.Mode == "stop" {
				return fmt.Errorf("row %d failed validation; no output published", v.Report.Records)
			}
			if err := rejectedEncoder.Encode(struct {
				Row    int64           `json:"row"`
				Record any             `json:"record"`
				Issues []quality.Issue `json:"issues"`
			}{v.Report.Records, row, v.Report.Issues}); err != nil {
				return err
			}
		} else if err := acceptedEncoder.Encode(row); err != nil {
			return err
		}
		if v.Report.Records%1000 == 0 {
			progress(v.Report.Records, v.Report.Records)
			return s.DiskCheck(s.DownloadDir, s.Reserve)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	v.Report.Issues = samples
	v.Report.IssuesTruncated = v.Report.Errors > int64(len(samples))
	path := filepath.Join(s.DownloadDir, j.ID+".zip")
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer func() { f.Close(); os.Remove(tmp) }()
	z := zip.NewWriter(&resources.Writer{W: f, Path: s.DownloadDir, Reserve: s.Reserve, Check: s.DiskCheck})
	for _, entry := range []struct {
		name string
		file *os.File
	}{{"valid.ndjson", valid}, {"rejected.ndjson", rejected}} {
		w, e := z.Create(entry.name)
		if e != nil {
			return "", e
		}
		if _, e = entry.file.Seek(0, 0); e != nil {
			return "", e
		}
		if _, e = io.Copy(w, contextReaderForExport{ctx, entry.file}); e != nil {
			return "", e
		}
	}
	w, err := z.Create("report.json")
	if err != nil {
		return "", err
	}
	err = json.NewEncoder(w).Encode(struct {
		Collection, Generation, Mode string
		Accepted                     int64
		Validation                   quality.Validation
	}{c.ID, ref.generation, p.Mode, v.Report.Records - v.Report.Invalid, v.Report})
	if err != nil {
		return "", err
	}
	if err = z.Close(); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Rename(tmp, path); err != nil {
		return "", err
	}
	progress(v.Report.Records, v.Report.Records)
	return path, nil
}

type contextReaderForExport struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReaderForExport) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
