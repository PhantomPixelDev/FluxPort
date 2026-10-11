package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"fluxport/internal/collections"
	"fluxport/internal/config"
	"fluxport/internal/conversion"
	"fluxport/internal/database"
	"fluxport/internal/filesystem"
	"fluxport/internal/fixtures"
	"fluxport/internal/resources"
	"fluxport/internal/web"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

var version = "dev"

const help = `FluxPort — files into APIs, in one executable.

Usage:
  fluxport --help
  fluxport version
  fluxport healthcheck [--url http://127.0.0.1:8080/healthz]
  fluxport serve
  fluxport convert input.json --to xml [--from json] [--output output.xml]
    CSV options: --delimiter auto|comma|semicolon|tab|pipe --infer-types --spreadsheet-safe
  fluxport fixtures --suite smoke|integration|large|stress --out <directory> --seed 42 [--rows N | --bytes N]
  fluxport import input.xlsx
  fluxport inspect <collection-id> [--limit 10000]
  fluxport validate <collection-id> [--rules rules.json] [--limit 10000]
  fluxport query --collections <id,id> --sql "SELECT * FROM c_<id>" [--engine auto|native|duckdb]
  fluxport pipeline list
  fluxport pipeline run <pipeline-id> --format parquet --output result.parquet
  fluxport export <collection-id> --format xlsx --output result.xlsx

Formats: json, csv, yaml, xml, ndjson, toml
Configuration: FLUXPORT_CONFIG (optional JSON file), FLUXPORT_ADDRESS,
FLUXPORT_DATA_DIR, FLUXPORT_STATE_DIR, FLUXPORT_ADMIN_PASSWORD,
FLUXPORT_API_KEY, FLUXPORT_MAX_FILE_BYTES.
Default server: http://127.0.0.1:8080
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("fluxport", "error", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Print(help)
		return nil
	}
	switch args[0] {
	case "version":
		fmt.Println("FluxPort", version)
		return nil
	case "serve":
		if len(args) > 1 {
			return fmt.Errorf("serve takes no arguments; use environment configuration")
		}
		return serve()
	case "fixtures":
		return fixtureCommand(args[1:])
	case "healthcheck":
		return healthcheckCommand(args[1:])
	case "convert":
		return convert(args[1:])
	case "import", "inspect", "validate", "query", "pipeline", "export":
		return dataCommand(args[0], args[1:])
	default:
		return fmt.Errorf("unknown command %q; see fluxport --help", args[0])
	}
}
func convert(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("convert requires an input path")
	}
	if args[0] == "--help" {
		fmt.Print(help)
		return nil
	}
	input := args[0]
	f := flag.NewFlagSet("convert", flag.ContinueOnError)
	to := f.String("to", "", "output format")
	from := f.String("from", conversion.FromFilename(input), "input format")
	output := f.String("output", "", "output path (default stdout)")
	infer := f.Bool("infer-types", false, "infer CSV numbers, booleans and null; preserve leading-zero IDs")
	delimiter := f.String("delimiter", "comma", "CSV input delimiter: auto|comma|semicolon|tab|pipe")
	safeCSV := f.Bool("spreadsheet-safe", false, "prefix formula-like CSV text for spreadsheet downloads")
	columnTypes := f.String("column-types", "", "CSV column rules, one name=type per line")
	parseJSON := f.Bool("parse-json-cells", false, "decode object/array JSON in CSV cells")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	columns, columnsErr := conversion.ParseColumnTypes(*columnTypes)
	if columnsErr != nil {
		return columnsErr
	}
	options := conversion.Options{CSVInferTypes: *infer, CSVDelimiter: *delimiter, SpreadsheetSafe: *safeCSV, CSVColumnTypes: columns, CSVParseJSON: *parseJSON}
	if *to == "" {
		return fmt.Errorf("--to is required")
	}
	limit := int64(16 << 20)
	if value := os.Getenv("FLUXPORT_MAX_FILE_BYTES"); value != "" {
		var n int64
		if _, err := fmt.Sscan(value, &n); err != nil || n < 1024 || n > 256<<20 {
			return fmt.Errorf("invalid FLUXPORT_MAX_FILE_BYTES")
		}
		limit = n
	}
	recordLimit := int64(1 << 20)
	if value := os.Getenv("FLUXPORT_MAX_RECORD_BYTES"); value != "" {
		n, e := strconv.ParseInt(value, 10, 64)
		if e != nil || n < 1024 || n > 16<<20 {
			return fmt.Errorf("invalid FLUXPORT_MAX_RECORD_BYTES")
		}
		recordLimit = n
	}
	if input != "-" {
		if stat, e := os.Stat(input); e == nil && stat.Size() > limit && conversion.Streaming(*from) {
			return streamConvertWithOptions(input, *from, *to, *output, recordLimit, limit, options)
		}
	}
	var r io.ReadCloser
	var err error
	if input == "-" {
		if conversion.Streaming(*from) {
			tmp, e := os.CreateTemp("", "fluxport-stdin-*")
			if e != nil {
				return e
			}
			defer os.Remove(tmp.Name())
			defer tmp.Close()
			reserve, e := diskReserve()
			if e != nil {
				return e
			}
			if _, e = io.CopyBuffer(&resources.Writer{W: tmp, Path: filepath.Dir(tmp.Name()), Reserve: reserve}, os.Stdin, make([]byte, 64<<10)); e != nil {
				return e
			}
			info, e := tmp.Stat()
			if e != nil {
				return e
			}
			if info.Size() > limit {
				return streamConvertWithOptions(tmp.Name(), *from, *to, *output, recordLimit, limit, options)
			}
			if _, e = tmp.Seek(0, 0); e != nil {
				return e
			}
			r = tmp
		} else {
			r = os.Stdin
		}
	} else {
		r, err = os.Open(input)
	}
	if err != nil {
		return err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if int64(len(b)) > limit {
		return collections.ErrTooLarge
	}
	v, err := conversion.DecodeWithOptions(b, *from, options)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	b, err = conversion.EncodeConversion(v, b, *from, *to, options)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if *output != "" {
		if sameSource(input, *output) {
			return fmt.Errorf("output must differ from input")
		}
		return os.WriteFile(*output, b, 0600)
	}
	_, err = os.Stdout.Write(append(b, '\n'))
	return err
}
func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := database.Open(cfg.StateDir)
	if err != nil {
		return err
	}
	defer db.Close()
	s, err := collections.New(db, cfg.DataDir, cfg.MaxFileBytes)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	s.Configure(cfg)
	if err = s.Start(ctx, cfg.Workers); err != nil {
		return err
	}
	defer s.Stop()
	if cfg.PublicDemo {
		if err = s.Demo(); err != nil {
			return fmt.Errorf("load public demo examples: %w", err)
		}
	}
	if err = s.Sync(); err != nil {
		return err
	}
	stop, err := filesystem.Watch(ctx, s)
	if err != nil {
		return err
	}
	defer stop()
	app, err := web.New(cfg, s)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
			slog.Error("shutdown", "error", err)
		}
	}()
	slog.Info("FluxPort ready", "address", cfg.Address, "data", s.Root, "version", version)
	return app.Listen(cfg.Address)
}

func fixtureCommand(args []string) error {
	f := flag.NewFlagSet("fixtures", flag.ContinueOnError)
	suite := f.String("suite", "smoke", "fixture suite")
	out := f.String("out", "", "output directory")
	seed := f.Int64("seed", 42, "deterministic seed")
	rows := f.Int64("rows", 0, "stress record count")
	size := f.Int64("bytes", 0, "stress bytes per file")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() > 0 {
		return fmt.Errorf("unexpected fixture arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	m, err := fixtures.Generate(ctx, fixtures.Options{Suite: *suite, Out: *out, Seed: *seed, Rows: *rows, Bytes: *size})
	if err != nil {
		return err
	}
	if *suite == "smoke" {
		if err = fixtures.EdgeCases(filepath.Join(*out, "edge-cases")); err != nil {
			return err
		}
	}
	fmt.Printf("Generated %d datasets; manifest: %s\n", len(m.Files), filepath.Join(*out, "manifest.json"))
	return nil
}
func streamConvert(input, from, to, output string, recordLimit, documentLimit int64) error {
	return streamConvertWithOptions(input, from, to, output, recordLimit, documentLimit, conversion.Options{})
}
func streamConvertWithOptions(input, from, to, output string, recordLimit, documentLimit int64, options conversion.Options) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	defer f.Close()
	fields := []string{}
	if conversion.Format(to) == "csv" {
		keys := map[string]bool{}
		err = conversion.IterateWithOptions(ctx, f, from, recordLimit, documentLimit, options, func(v any) error {
			for _, field := range collections.Schema([]any{v}) {
				if !keys[field.Name] && len(keys) >= 2048 {
					return fmt.Errorf("too many fields")
				}
				keys[field.Name] = true
			}
			return nil
		})
		if err != nil {
			return err
		}
		for k := range keys {
			fields = append(fields, k)
		}
		sort.Strings(fields)
		if _, err = f.Seek(0, 0); err != nil {
			return err
		}
	}
	if len(fields) == 0 && conversion.Format(from) == "csv" && conversion.Format(to) == "csv" {
		if _, err = f.Seek(0, 0); err != nil {
			return err
		}
		fields, err = conversion.CSVFields(f, options, recordLimit)
		if err != nil {
			return err
		}
		if _, err = f.Seek(0, 0); err != nil {
			return err
		}
	}
	var w io.Writer = os.Stdout
	var dest *os.File
	tmp := ""
	if output != "" {
		if sameSource(input, output) {
			return fmt.Errorf("output must differ from input")
		}
		dest, err = os.CreateTemp(filepath.Dir(output), ".fluxport-convert-*")
		if err != nil {
			return err
		}
		tmp = dest.Name()
		defer os.Remove(tmp)
		defer dest.Close()
		reserve, e := diskReserve()
		if e != nil {
			return e
		}
		w = &resources.Writer{W: dest, Path: filepath.Dir(output), Reserve: reserve}
	}
	writer, err := conversion.NewRowWriterWithOptions(w, to, fields, options)
	if err != nil {
		return err
	}
	if err = conversion.IterateWithOptions(ctx, f, from, recordLimit, documentLimit, options, writer.Write); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	if dest != nil {
		if err = dest.Sync(); err != nil {
			return err
		}
		if err = dest.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, output)
	}
	return nil
}
func diskReserve() (uint64, error) {
	reserve := uint64(256 << 20)
	if value := os.Getenv("FLUXPORT_MIN_FREE_DISK_BYTES"); value != "" {
		n, e := strconv.ParseUint(value, 10, 64)
		if e != nil || n > 1<<50 {
			return 0, fmt.Errorf("invalid FLUXPORT_MIN_FREE_DISK_BYTES")
		}
		reserve = n
	}
	return reserve, nil
}

func sameSource(input, output string) bool {
	a, e := filepath.Abs(input)
	if e != nil {
		return true
	}
	b, e := filepath.Abs(output)
	if e != nil {
		return true
	}
	if a == b || (runtime.GOOS == "windows" && strings.EqualFold(a, b)) {
		return true
	}
	x, ex := os.Stat(a)
	y, ey := os.Stat(b)
	return ex == nil && ey == nil && os.SameFile(x, y)
}
