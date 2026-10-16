package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"fluxport/internal/collections"
	"fluxport/internal/config"
	"fluxport/internal/conversion"
	"fluxport/internal/database"
	"fluxport/internal/quality"
	"fluxport/internal/resources"
)

func dataCommand(command string, args []string) error {
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
	s.Configure(cfg)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	printJSON := func(value any) error {
		b, e := conversion.MarshalJSONIndent(value)
		if e != nil {
			return e
		}
		_, e = os.Stdout.Write(append(b, '\n'))
		return e
	}
	if command == "pipeline" && len(args) > 0 && args[0] == "list" {
		if len(args) != 1 {
			return fmt.Errorf("pipeline list takes no arguments")
		}
		p, e := s.Pipelines()
		if e != nil {
			return e
		}
		return printJSON(p)
	}
	if command == "query" {
		f := flag.NewFlagSet("query", flag.ContinueOnError)
		sql := f.String("sql", "", "read-only SQL")
		ids := f.String("collections", "", "comma-separated collection IDs")
		engine := f.String("engine", "auto", "auto|native|duckdb")
		params := f.String("params", "{}", "named SQL parameters as a JSON object")
		explain := f.Bool("explain", false, "show the query plan")
		if err = f.Parse(args); err != nil {
			return err
		}
		if f.NArg() != 0 || *sql == "" || *ids == "" {
			return fmt.Errorf("query requires --sql and --collections")
		}
		value, e := conversion.Decode([]byte(*params), "json")
		if e != nil {
			return e
		}
		values, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("parameters must be an object")
		}
		query, e := collections.BindSQL(*sql, values)
		if e != nil {
			return e
		}
		if *explain {
			if e = collections.ValidateSQL(query); e != nil {
				return e
			}
			if *engine == "native" {
				query = "EXPLAIN QUERY PLAN " + query
			} else {
				*engine = "duckdb"
				query = "EXPLAIN " + query
			}
		}
		result, e := s.SQLWithEngine(ctx, strings.Split(*ids, ","), query, *engine)
		if e != nil {
			return e
		}
		return printJSON(result)
	}
	var pipeline *collections.Pipeline
	if command == "pipeline" {
		if len(args) < 2 || args[0] != "run" {
			return fmt.Errorf("use pipeline list or pipeline run <id> --format <format> --output <file>")
		}
		p, e := s.GetPipeline(args[1])
		if e != nil {
			file, openErr := os.Open(args[1])
			if openErr != nil {
				return e
			}
			b, readErr := io.ReadAll(io.LimitReader(file, 65537))
			file.Close()
			if readErr != nil {
				return readErr
			}
			if len(b) > 65536 {
				return fmt.Errorf("recipe exceeds 64 KiB")
			}
			p, e = collections.DecodePipeline(b)
			if e != nil {
				return e
			}
		}
		pipeline = &p
		args = append([]string{p.Source}, args[2:]...)
		command = "export"
	}
	if len(args) == 0 {
		return fmt.Errorf("%s requires a file path or collection ID", command)
	}
	input := args[0]
	if command == "import" {
		flags := flag.NewFlagSet("import", flag.ContinueOnError)
		encoding := flags.String("encoding", "utf-8", "utf-8|windows-1252|utf-16le|utf-16be")
		delimiter := flags.String("delimiter", "comma", "comma|tab|semicolon|pipe|auto")
		if err = flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("import takes one file path")
		}
		f, e := os.Open(input)
		if e != nil {
			return e
		}
		defer f.Close()
		col, e := s.ImportInput(ctx, filepath.Base(input), f, collections.InputOptions{Encoding: *encoding, CSVDelimiter: *delimiter})
		if e != nil {
			return e
		}
		return printJSON(col)
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	limit := f.Int("limit", 10000, "records to check (1–100000)")
	rulesFile := f.String("rules", "", "validation rule JSON array")
	format := f.String("format", "json", "export format")
	output := f.String("output", "", "export destination")
	parameters := f.String("params", "{}", "recipe parameter overrides as JSON")
	sourceID := f.String("source", "", "override the recipe source collection")
	if err = f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if pipeline != nil {
		if *sourceID != "" {
			pipeline.Source = *sourceID
			input = *sourceID
		}
		value, e := conversion.Decode([]byte(*parameters), "json")
		if e != nil {
			return e
		}
		values, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("recipe parameters must be an object")
		}
		bound, e := collections.BindPipeline(*pipeline, values)
		if e != nil {
			return e
		}
		pipeline = &bound
	}
	col, err := s.Get(input)
	if err != nil {
		return err
	}
	switch command {
	case "inspect", "validate":
		rules := []quality.Rule{}
		if command == "validate" {
			rules, err = s.ValidationRules(input)
			if err != nil {
				return err
			}
			if *rulesFile != "" {
				file, e := os.Open(*rulesFile)
				if e != nil {
					return e
				}
				b, e := io.ReadAll(io.LimitReader(file, 65537))
				file.Close()
				if e != nil {
					return e
				}
				if len(b) > 65536 {
					return fmt.Errorf("rules exceed 64 KiB")
				}
				if _, e = conversion.Decode(b, "json"); e != nil {
					return e
				}
				decoder := json.NewDecoder(bytes.NewReader(b))
				decoder.UseNumber()
				decoder.DisallowUnknownFields()
				if e = decoder.Decode(&rules); e != nil {
					return e
				}
			}
		}
		var contract *quality.Contract
		if command == "validate" {
			contract, err = s.Contract(input)
			if err != nil {
				return err
			}
		}
		report, e := s.Quality(ctx, col, collections.QualityRequest{Limit: *limit, Rules: rules, Contract: contract})
		if e != nil {
			return e
		}
		if e = printJSON(report); e != nil {
			return e
		}
		if command == "validate" && report.Validation.Errors > 0 {
			return fmt.Errorf("validation failed: %d invalid records", report.Validation.Invalid)
		}
		return nil
	case "export":
		if *output == "" {
			return fmt.Errorf("export requires --output")
		}
		if sameSource(col.Path, *output) {
			return fmt.Errorf("output must differ from source")
		}
		path, e := s.ExportFile(ctx, input, *format, pipeline)
		if e != nil {
			return e
		}
		defer os.Remove(path)
		// Publish only after a complete copy has been synced in the destination directory.
		source, e := os.Open(path)
		if e != nil {
			return e
		}
		defer source.Close()
		dest, e := os.CreateTemp(filepath.Dir(*output), ".fluxport-export-*")
		if e != nil {
			return e
		}
		defer os.Remove(dest.Name())
		defer dest.Close()
		if _, e = io.Copy(&resources.Writer{W: dest, Path: filepath.Dir(*output), Reserve: cfg.MinFreeDiskBytes}, source); e != nil {
			return e
		}
		if e = dest.Sync(); e != nil {
			return e
		}
		if e = dest.Close(); e != nil {
			return e
		}
		return os.Rename(dest.Name(), *output)
	}
	return fmt.Errorf("unknown data command")
}
