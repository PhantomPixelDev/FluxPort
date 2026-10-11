package fixtures

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"fluxport/internal/conversion"
)

type Options struct {
	Suite, Out        string
	Seed, Rows, Bytes int64
}
type Entry struct {
	File    string `json:"file"`
	Profile string `json:"profile"`
	Format  string `json:"format"`
	Records int64  `json:"records"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
	Notes   string `json:"notes,omitempty"`
}
type Manifest struct {
	Version int     `json:"version"`
	Suite   string  `json:"suite"`
	Seed    int64   `json:"seed"`
	Files   []Entry `json:"files"`
}

var Profiles = []string{"users", "products", "orders", "events"}
var Fields = []string{"id", "profile", "name", "email", "category", "amount", "active", "timestamp", "note", "user_id", "product_id", "sku", "currency", "stock", "quantity", "status", "country", "role", "event_type", "session_id", "duration_ms"}

func Record(profile string, i, seed int64) map[string]any {
	n := (i*7919 + seed*104729) % 1000003
	if n < 0 {
		n = -n
	}
	names := []string{"Ada Lovelace", "Grace Hopper", "Margaret Hamilton", "Linus Torvalds", "Renée Müller", "山田 太郎"}
	categories := []string{"engineering", "platform", "retail", "analytics"}
	row := map[string]any{"id": i + 1, "profile": profile, "name": names[n%int64(len(names))], "email": fmt.Sprintf("user%07d@example.test", n), "category": categories[n%4], "amount": json.Number(fmt.Sprintf("%d.%02d", n%10000, n%100)), "active": n%3 != 0, "timestamp": time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i%31536000) * time.Second).Format(time.RFC3339), "note": fmt.Sprintf("%s record %d · synthetic test data", profile, i+1)}
	switch profile {
	case "users":
		row["country"] = []string{"DE", "US", "JP", "GB"}[n%4]
		row["role"] = []string{"member", "editor", "admin"}[n%3]
	case "products":
		row["name"] = []string{"Wireless keyboard", "USB-C hub", "Desk lamp", "Notebook", "Water bottle", "Laptop stand"}[n%6]
		row["sku"] = fmt.Sprintf("SKU-%06d", i+1)
		row["currency"] = "EUR"
		row["stock"] = n % 500
		row["category"] = []string{"electronics", "office", "home"}[n%3]
	case "orders":
		row["name"] = fmt.Sprintf("Order FP-%08d", i+1)
		row["user_id"] = n%10000 + 1
		row["product_id"] = n%1000 + 1
		row["quantity"] = n%5 + 1
		row["status"] = []string{"pending", "paid", "shipped", "delivered", "refunded"}[n%5]
		row["currency"] = "EUR"
	case "events":
		kind := []string{"page_view", "product_view", "add_to_cart", "checkout", "sign_in"}[n%5]
		row["name"] = kind
		row["event_type"] = kind
		row["user_id"] = n%10000 + 1
		row["session_id"] = fmt.Sprintf("session-%08x", n)
		row["duration_ms"] = n % 5000
	}
	return row
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (w *countingWriter) Write(b []byte) (int, error) {
	n, err := w.w.Write(b)
	w.n += int64(n)
	return n, err
}
func Generate(ctx context.Context, o Options) (Manifest, error) {
	m := Manifest{Version: 1, Suite: o.Suite, Seed: o.Seed, Files: []Entry{}}
	if o.Out == "" {
		return m, fmt.Errorf("--out is required")
	}
	counts := []int64{}
	switch o.Suite {
	case "smoke":
		counts = []int64{100}
	case "integration":
		counts = []int64{10000}
	case "large":
		counts = []int64{100000, 1000000}
	case "stress":
		if o.Rows <= 0 && o.Bytes <= 0 {
			return m, fmt.Errorf("stress requires --rows or --bytes")
		}
		if o.Rows > 0 {
			counts = []int64{o.Rows}
		} else {
			counts = []int64{0}
		}
	default:
		return m, fmt.Errorf("unknown fixture suite %q", o.Suite)
	}
	if o.Rows < 0 || o.Bytes < 0 {
		return m, fmt.Errorf("rows and bytes cannot be negative")
	}
	if o.Rows > 0 && o.Bytes > 0 {
		return m, fmt.Errorf("choose --rows or --bytes, not both")
	}
	if o.Suite != "stress" && (o.Rows > 0 || o.Bytes > 0) {
		return m, fmt.Errorf("custom sizes require --suite stress")
	}
	if err := os.MkdirAll(o.Out, 0700); err != nil {
		return m, err
	}
	for _, count := range counts {
		for i, format := range conversion.Formats {
			profile := Profiles[i%len(Profiles)]
			suffix := fmt.Sprintf("%d", count)
			if count == 0 {
				suffix = fmt.Sprintf("%d-bytes", o.Bytes)
			}
			name := profile + "-" + suffix + "." + format
			path := filepath.Join(o.Out, name)
			f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return m, err
			}
			h := sha256.New()
			cw := &countingWriter{w: io.MultiWriter(f, h)}
			writer, err := conversion.NewRowWriter(cw, format, Fields)
			var rows int64
			if err == nil {
				for (count > 0 && rows < count) || (count == 0 && cw.n < o.Bytes) {
					if err = ctx.Err(); err != nil {
						break
					}
					if err = writer.Write(Record(profile, rows, o.Seed)); err != nil {
						break
					}
					rows++
				}
				if err == nil {
					err = writer.Close()
				}
			}
			if err == nil {
				err = f.Sync()
			}
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				os.Remove(path)
				return m, err
			}
			notes := "Top-level record array; JSON numeric lexemes are preserved."
			if format == "ndjson" {
				notes = "One JSON record per line; blank lines are ignored on import."
			}
			if format == "yaml" {
				notes = "Record sequence; numeric formatting can normalize decimals."
			}
			if format == "csv" {
				notes = "Cells are strings; missing and null values become empty cells; nested values use JSON text."
			}
			if format == "toml" {
				notes = "Record sequence is wrapped in records; TOML imports expose one root object. Nulls are unsupported; integers are signed 64-bit."
			}
			if format == "xml" {
				notes = "Lossless fluxport JSON envelope."
			}
			m.Files = append(m.Files, Entry{name, profile, format, rows, cw.n, hex.EncodeToString(h.Sum(nil)), notes})
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, err
	}
	err = os.WriteFile(filepath.Join(o.Out, "manifest.json"), append(b, '\n'), 0600)
	return m, err
}
func EdgeCases(out string) error {
	cases := map[string]string{"missing-values.ndjson": "{\"id\":1,\"optional\":null}\n{\"id\":2}\n{\"id\":3,\"optional\":\"\"}\n", "unicode.json": "[{\"name\":\"Renée · 山田\",\"id\":9007199254740993,\"missing\":null,\"nested\":{\"tags\":[\"a\",\"b\"]}}]", "multiline.csv": "name,note\nAda,\"first line\nsecond line\"\nGrace,\"quote \"\"inside\"\"\"\n", "malformed.json": "[{\"unfinished\":", "duplicate-headers.csv": "name,name\nAda,Grace\n", "invalid.yaml": "x: [\n", "invalid.xml": "<!DOCTYPE root><root/>", "invalid.ndjson": "{\n\"x\":1\n}\n", "invalid.toml": "x = [\n", "incompatible-toml.json": "{\"cannot_represent_in_toml\":null}", "incompatible-csv.json": "[1,true,null]"}
	if err := os.MkdirAll(out, 0700); err != nil {
		return err
	}
	for name, body := range cases {
		if err := os.WriteFile(filepath.Join(out, name), []byte(body), 0600); err != nil {
			return err
		}
	}
	return nil
}
