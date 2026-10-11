package collections

import (
	"context"
	"encoding/json"
	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
	"fluxport/internal/resources"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type convertPayload struct {
	Pipeline                     *Pipeline         `json:",omitempty"`
	Generations                  map[string]string `json:",omitempty"`
	CollectionID, Path, From, To string
	Options                      conversion.Options
	Query                        *Query `json:",omitempty"`
	Generation                   string `json:",omitempty"`
}

func (s *Service) ExportQueryJob(id, generation, format string, q Query) (jobs.Job, error) {
	if err := ValidateQuery(q); err != nil {
		return jobs.Job{}, err
	}
	c, err := s.Get(id)
	if err != nil {
		return jobs.Job{}, err
	}
	c, err = s.ready(c)
	if err != nil {
		return jobs.Job{}, err
	}
	if generation == "" || generation != c.Generation {
		return jobs.Job{}, ErrConflict
	}
	format = conversion.Format(format)
	if !datasetSupported(format) {
		return jobs.Job{}, fmt.Errorf("unsupported export format")
	}
	b, err := json.Marshal(convertPayload{CollectionID: id, To: format, Query: &q, Generation: generation})
	if err != nil {
		return jobs.Job{}, err
	}
	return s.Jobs.Submit("convert", id, c.Name+" · filtered export → "+format, string(b), c.Size)
}

func (s *Service) ExportJob(id, format string) (jobs.Job, error) {
	return s.ExportJobWithOptions(id, format, conversion.Options{})
}
func (s *Service) ExportJobWithOptions(id, format string, options conversion.Options) (jobs.Job, error) {
	format = conversion.Format(format)
	c, err := s.Get(id)
	if err != nil {
		return jobs.Job{}, err
	}
	if _, err = s.ready(c); err != nil {
		return jobs.Job{}, err
	}
	if !datasetSupported(format) {
		return jobs.Job{}, fmt.Errorf("unsupported format")
	}
	b, _ := json.Marshal(convertPayload{CollectionID: id, To: format, Options: options})
	return s.Jobs.Submit("convert", id, c.Name+" → "+format, string(b), c.Size)
}
func (s *Service) ConvertJob(path, from, to string) (jobs.Job, error) {
	return s.ConvertJobWithOptions(path, from, to, conversion.Options{})
}
func (s *Service) ConvertJobWithOptions(path, from, to string, options conversion.Options) (jobs.Job, error) {
	from = conversion.Format(from)
	to = conversion.Format(to)
	if !conversion.Supported(from) || !conversion.Supported(to) {
		return jobs.Job{}, fmt.Errorf("unsupported format")
	}
	if err := s.safe(path); err != nil {
		return jobs.Job{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return jobs.Job{}, err
	}
	b, _ := json.Marshal(convertPayload{Path: path, From: from, To: to, Options: options})
	return s.Jobs.Submit("convert", "", friendly(filepath.Base(path))+" → "+to, string(b), info.Size())
}
func (s *Service) convertJob(ctx context.Context, j jobs.Job, progress func(int64, int64)) (result string, err error) {
	s.cleanupDownloads()
	var p convertPayload
	decoder := json.NewDecoder(strings.NewReader(j.Payload))
	decoder.UseNumber()
	if err = decoder.Decode(&p); err != nil {
		return "", err
	}
	if binaryFormat(p.To) {
		return s.binaryExportJob(ctx, j, p, progress)
	}
	if err = s.DiskCheck(s.DownloadDir, s.Reserve); err != nil {
		return "", err
	}
	fields := []string{}
	var c Collection
	if p.CollectionID != "" {
		c, err = s.Get(p.CollectionID)
		if err != nil {
			return "", err
		}
		schema, err := s.Fields(c)
		if err != nil {
			return "", err
		}
		for _, f := range schema {
			fields = append(fields, f.Name)
		}
		if p.Pipeline != nil {
			fields = nil
			if p.To == "csv" {
				observed := map[string]map[string]bool{}
				err = s.eachPipeline(ctx, *p.Pipeline, p.Generations, func(row any) error {
					for _, field := range Schema([]any{row}) {
						if observed[field.Name] == nil {
							if len(observed) >= 2048 {
								return fmt.Errorf("pipeline CSV schema exceeds 2048 fields")
							}
							observed[field.Name] = map[string]bool{}
						}
					}
					return nil
				})
				if err != nil {
					return "", err
				}
				for _, field := range sortedFields(observed) {
					fields = append(fields, field.Name)
				}
			}
		}
	} else {
		if err = s.safe(p.Path); err != nil {
			return "", err
		}
		if p.To == "csv" {
			f, err := os.Open(p.Path)
			if err != nil {
				return "", err
			}
			seen := map[string]bool{}
			err = conversion.IterateWithOptions(ctx, f, p.From, s.RecordBytes, s.MaxBytes, p.Options, func(v any) error {
				for _, field := range Schema([]any{v}) {
					if !seen[field.Name] && len(seen) >= 2048 {
						return fmt.Errorf("schema field limit exceeded")
					}
					seen[field.Name] = true
				}
				return nil
			})
			f.Close()
			if err != nil {
				return "", err
			}
			schema := map[string]map[string]bool{}
			for k := range seen {
				schema[k] = map[string]bool{}
			}
			for _, f := range sortedFields(schema) {
				fields = append(fields, f.Name)
			}
		}
	}
	if p.To == "csv" && len(fields) == 0 && p.Pipeline == nil {
		source := p.Path
		format := p.From
		if p.CollectionID != "" {
			source = c.Path
			format = c.Format
		}
		if format == "csv" {
			input, e := os.Open(source)
			if e != nil {
				return "", e
			}
			fields, e = conversion.CSVFields(input, p.Options, s.RecordBytes)
			input.Close()
			if e != nil {
				return "", e
			}
		}
	}
	path := filepath.Join(s.DownloadDir, j.ID+"."+p.To)
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer func() { f.Close(); os.Remove(tmp) }()
	writer, err := conversion.NewRowWriterWithOptions(&resources.Writer{W: f, Path: s.DownloadDir, Reserve: s.Reserve, Check: s.DiskCheck}, p.To, fields, p.Options)
	if err != nil {
		return "", err
	}
	var count int64
	emit := func(v any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if count%1000 == 0 {
			if err := s.DiskCheck(s.DownloadDir, s.Reserve); err != nil {
				return err
			}
		}
		if err := writer.Write(v); err != nil {
			return err
		}
		count++
		if count%1000 == 0 {
			processed := int64(0)
			if c.Rows > 0 {
				processed = int64(float64(j.Total) * float64(count) / float64(c.Rows))
			}
			progress(processed, count)
		}
		return nil
	}
	if p.CollectionID != "" {
		if p.Pipeline != nil {
			err = s.eachPipeline(ctx, *p.Pipeline, p.Generations, emit)
		} else if p.Query != nil {
			err = s.EachQuery(ctx, c, *p.Query, p.Generation, emit)
		} else {
			err = s.Each(ctx, c, emit)
		}
	} else {
		input, e := os.Open(p.Path)
		if e != nil {
			return "", e
		}
		reader := &countedReader{r: input}
		err = conversion.IterateWithOptions(ctx, reader, p.From, s.RecordBytes, s.MaxBytes, p.Options, func(v any) error {
			if e := emit(v); e != nil {
				return e
			}
			progress(reader.n, count)
			return nil
		})
		input.Close()
	}
	if err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
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
	s.DB.Exec(`UPDATE jobs SET records=? WHERE id=?`, count, j.ID)
	return path, nil
}
func (s *Service) Download(id string) (string, error) {
	j, err := s.Jobs.Get(id)
	if err != nil {
		return "", err
	}
	if j.State != "ready" || j.Result == "" || filepath.Dir(j.Result) != s.DownloadDir {
		return "", fmt.Errorf("download is not ready")
	}
	if info, e := os.Stat(j.Result); e != nil || time.Since(info.ModTime()) > 24*time.Hour {
		return "", fmt.Errorf("download expired; retry the conversion")
	}
	return j.Result, nil
}
func (s *Service) cleanupDownloads() {
	files, _ := os.ReadDir(s.DownloadDir)
	for _, f := range files {
		if info, err := f.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
			os.Remove(filepath.Join(s.DownloadDir, f.Name()))
		}
	}
	active := map[string]bool{}
	rows, err := s.DB.Query(`SELECT payload FROM jobs WHERE kind='convert' AND state IN ('queued','running')`)
	if err != nil {
		return
	}
	for rows.Next() {
		var data string
		if rows.Scan(&data) == nil {
			var p convertPayload
			if json.Unmarshal([]byte(data), &p) == nil {
				active[p.Path] = true
			}
		}
	}
	rows.Close()
	dir := filepath.Join(s.Root, ".conversion-inputs")
	inputs, _ := os.ReadDir(dir)
	for _, f := range inputs {
		path := filepath.Join(dir, f.Name())
		if info, e := f.Info(); e == nil && !active[path] && time.Since(info.ModTime()) > 24*time.Hour {
			os.Remove(path)
		}
	}
}
func (s *Service) RetryJob(id string) (jobs.Job, error) {
	j, err := s.Jobs.Get(id)
	if err != nil {
		return j, err
	}
	if j.Kind == "import" {
		return j, fmt.Errorf("re-upload the file to retry this import")
	}
	if j.Kind == "index" {
		c, err := s.Get(j.Resource)
		if err != nil {
			return j, err
		}
		if err = s.SyncFile(c.Path); err != nil {
			return j, err
		}
		c, err = s.Get(c.ID)
		if err != nil {
			return j, err
		}
		return s.Jobs.Get(c.JobID)
	}
	if j.Kind == "convert" && j.State == "ready" {
		return s.Jobs.Submit(j.Kind, j.Resource, j.Name, j.Payload, j.Total)
	}
	return s.Jobs.Retry(id)
}
func (s *Service) RawSource(c Collection) (io.ReadCloser, error) {
	if err := s.safe(c.Path); err != nil {
		return nil, err
	}
	return os.Open(c.Path)
}
