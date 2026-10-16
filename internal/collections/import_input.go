package collections

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"fluxport/internal/conversion"
	"fluxport/internal/resources"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

type InputOptions struct {
	Encoding     string `json:"encoding,omitempty"`
	CSVDelimiter string `json:"csv_delimiter,omitempty"`
}

type InputPreview struct {
	Format     string `json:"format"`
	Records    []any  `json:"records"`
	SampleOnly bool   `json:"sample_only"`
}

func (s *Service) PreviewInput(ctx context.Context, name string, r io.Reader, o InputOptions) (InputPreview, error) {
	name, r, close, err := inputReader(name, r, o)
	if err != nil {
		return InputPreview{}, err
	}
	defer close()
	format := conversion.FromFilename(name)
	if !conversion.Supported(format) {
		return InputPreview{}, fmt.Errorf("import preview supports the six text formats")
	}
	out := InputPreview{Format: format, Records: []any{}, SampleOnly: true}
	stop := errors.New("preview complete")
	size := 0
	err = conversion.IterateWithOptions(ctx, r, format, s.RecordBytes, s.MaxBytes, conversion.Options{CSVDelimiter: o.CSVDelimiter}, func(row any) error {
		if len(out.Records) >= 20 {
			return stop
		}
		b, e := conversion.MarshalJSON(row)
		if e != nil {
			return e
		}
		size += len(b)
		if size > 8<<20 {
			return fmt.Errorf("preview exceeds 8 MiB")
		}
		out.Records = append(out.Records, row)
		return nil
	})
	if errors.Is(err, stop) {
		err = nil
	}
	return out, err
}

func inputReader(name string, r io.Reader, o InputOptions) (string, io.Reader, func(), error) {
	close := func() {}
	if strings.HasSuffix(strings.ToLower(name), ".gz") {
		name = name[:len(name)-3]
		format := conversion.FromFilename(name)
		if !conversion.Supported(format) {
			return "", nil, close, fmt.Errorf("gzip input must contain one supported text-format file")
		}
		g, err := gzip.NewReader(r)
		if err != nil {
			return "", nil, close, err
		}
		r = g
		close = func() { g.Close() }
	}
	switch o.Encoding {
	case "", "utf-8":
	case "windows-1252":
		r = transform.NewReader(r, charmap.Windows1252.NewDecoder())
	case "utf-16le":
		r = transform.NewReader(r, unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder())
	case "utf-16be":
		r = transform.NewReader(r, unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder())
	default:
		close()
		return "", nil, func() {}, fmt.Errorf("unsupported text encoding")
	}
	return name, r, close, nil
}

// ImportInput leaves ordinary uploads byte-for-byte intact. Explicit delimiter
// or encoding choices normalize to a clearly named UTF-8 NDJSON source.
func (s *Service) ImportInput(ctx context.Context, name string, r io.Reader, o InputOptions) (Collection, error) {
	name, r, close, err := inputReader(name, r, o)
	if err != nil {
		return Collection{}, err
	}
	defer close()
	if (o.Encoding == "" || o.Encoding == "utf-8") && (o.CSVDelimiter == "" || o.CSVDelimiter == "comma") {
		return s.Import(name, contextReaderForExport{ctx, r})
	}
	format := conversion.FromFilename(name)
	if !conversion.Supported(format) {
		return Collection{}, fmt.Errorf("text import options require a supported text format")
	}
	if err = s.DiskCheck(s.DownloadDir, s.Reserve); err != nil {
		return Collection{}, err
	}
	f, err := os.CreateTemp(s.DownloadDir, ".normalized-input-*")
	if err != nil {
		return Collection{}, err
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	w, err := conversion.NewRowWriterWithOptions(&resources.Writer{W: f, Path: s.DownloadDir, Reserve: s.Reserve, Check: s.DiskCheck}, "ndjson", nil, conversion.Options{})
	if err != nil {
		return Collection{}, err
	}
	err = conversion.IterateWithOptions(ctx, r, format, s.RecordBytes, s.MaxBytes, conversion.Options{CSVDelimiter: o.CSVDelimiter}, w.Write)
	if err != nil {
		return Collection{}, err
	}
	if err = w.Close(); err != nil {
		return Collection{}, err
	}
	if _, err = f.Seek(0, 0); err != nil {
		return Collection{}, err
	}
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	return s.Import(base+"-imported.ndjson", f)
}
