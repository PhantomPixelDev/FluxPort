package conversion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestStreamFormats(t *testing.T) {
	for _, format := range Formats {
		t.Run(format, func(t *testing.T) {
			var b bytes.Buffer
			w, e := NewRowWriter(&b, format, []string{"id", "name", "note"})
			if e != nil {
				t.Fatal(e)
			}
			for i := 0; i < 100; i++ {
				if e = w.Write(map[string]any{"id": i, "name": "Zoë 東京", "note": "line one\nline two, \"quoted\""}); e != nil {
					t.Fatal(e)
				}
			}
			if e = w.Close(); e != nil {
				t.Fatal(e)
			}
			count := 0
			e = Iterate(context.Background(), &b, format, 1<<20, 16<<20, func(v any) error {
				count++
				if format == "toml" {
					if _, ok := v.(map[string]any)["records"]; !ok {
						t.Fatal("missing TOML wrapper")
					}
					return nil
				}
				if v.(map[string]any)["name"] != "Zoë 東京" {
					t.Fatalf("lost Unicode: %v", v)
				}
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			want := 100
			if format == "toml" {
				want = 1
			}
			if count != want {
				t.Fatalf("%d records, expected %d", count, want)
			}
		})
	}
}
func TestStreamLimitsAndMalformed(t *testing.T) {
	for _, input := range []string{`[1,]`, `[1]true`, `[{"x":}]`, `[`, "[\"" + strings.Repeat("x", 2048) + "\"]"} {
		if e := Iterate(context.Background(), strings.NewReader(input), "json", 1024, 1024, func(any) error { return nil }); e == nil {
			t.Fatalf("accepted %q", input[:min(len(input), 40)])
		}
	}
	for _, format := range []string{"ndjson", "csv"} {
		if e := Iterate(context.Background(), strings.NewReader(strings.Repeat("x", 2048)), format, 1024, 1024, func(any) error { return nil }); e == nil {
			t.Fatal("accepted oversized record")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := Iterate(ctx, strings.NewReader("[1]"), "json", 1024, 1024, func(any) error { return nil }); e == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestStreamLargeIntegerAndEscapes(t *testing.T) {
	input := `[{"id":9223372036854775807,"text":"comma, and bracket ] and quote \""},[1,2],null]`
	n := 0
	if e := Iterate(context.Background(), strings.NewReader(input), "json", 1024, 1024, func(v any) error {
		if n == 0 && v.(map[string]any)["id"].(json.Number).String() != "9223372036854775807" {
			t.Fatal("integer precision lost")
		}
		n++
		return nil
	}); e != nil || n != 3 {
		t.Fatalf("%d %v", n, e)
	}
}

// Streaming a source must produce the same bytes as converting it in memory, so
// a user never sees different formatting for the same data depending on size.
func TestStreamMatchesBufferedOutput(t *testing.T) {
	records := []any{}
	for i := 0; i < 4; i++ {
		records = append(records, map[string]any{
			"id":   json.Number(fmt.Sprintf("%d", i+1)),
			"name": "Ada & <Grace>",
			"note": "line one\nline two",
			"tags": []any{"a", "b"},
			"meta": map[string]any{"k": []any{1, 2, map[string]any{"z": "y"}}},
		})
	}
	fields := []string{"id", "meta", "name", "note", "tags"}
	for _, format := range Formats {
		t.Run(format, func(t *testing.T) {
			// TOML has no top-level array root; both paths wrap records.
			input := any(records)
			if format == "toml" {
				input = map[string]any{"records": records}
			}
			want, err := Encode(input, format)
			if err != nil {
				t.Fatal(err)
			}
			var streamed bytes.Buffer
			w, e := NewRowWriter(&streamed, format, fields)
			if e != nil {
				t.Fatal(e)
			}
			for _, row := range records {
				if e = w.Write(row); e != nil {
					t.Fatal(e)
				}
			}
			if e = w.Close(); e != nil {
				t.Fatal(e)
			}
			if streamed.String() != string(want) {
				t.Fatalf("%s differs:\n--- streamed ---\n%s\n--- buffered ---\n%s", format, streamed.String(), want)
			}
			// Whatever is emitted must also read back.
			n := 0
			e = Iterate(context.Background(), bytes.NewReader(streamed.Bytes()), format, 1<<20, 16<<20, func(v any) error {
				n++
				return nil
			})
			if e != nil {
				t.Fatalf("%s output unreadable: %v\n%s", format, e, streamed.String())
			}
			if format == "toml" {
				if n != 1 {
					t.Fatalf("%d TOML records", n)
				}
			} else if n != len(records) {
				t.Fatalf("%d %s records, expected %d", n, format, len(records))
			}
		})
	}
}

// Streamed output must not HTML-escape, and out-of-range numbers must stay
// readable the same way the buffered path writes them.
func TestStreamEscapingAndNumbers(t *testing.T) {
	for _, format := range Formats {
		var b bytes.Buffer
		w, e := NewRowWriter(&b, format, []string{"html", "big", "max"})
		if e != nil {
			t.Fatal(e)
		}
		if e = w.Write(map[string]any{
			"html": "<b>&</b>",
			"big":  json.Number("99999999999999999999"),
			"max":  json.Number("9223372036854775807"),
		}); e != nil {
			if format == "toml" && strings.Contains(e.Error(), "signed 64") {
				continue
			}
			t.Fatal(e)
		}
		if e = w.Close(); e != nil {
			t.Fatal(e)
		}
		out := b.String()
		if strings.Contains(out, `\u003c`) || strings.Contains(out, `\u0026`) {
			t.Fatalf("%s HTML-escaped: %s", format, out)
		}
		if format == "yaml" || format == "toml" {
			got, err := Decode([]byte(out), format)
			if err != nil {
				t.Fatalf("%s unreadable: %v\n%s", format, err, out)
			}
			// JSON-family readers return the record sequence; TOML and YAML
			// readers give the single record or the records wrapper directly.
			v := map[string]any{}
			if rows := Rows(got); len(rows) > 0 {
				if obj, isObj := rows[0].(map[string]any); isObj {
					v = obj
				}
			}
			if format == "toml" {
				v = Rows(v["records"])[0].(map[string]any)
			}
			if v["big"] != json.Number("99999999999999999999") {
				t.Fatalf("%s lost the out-of-range lexeme: %#v", format, v)
			}
			if _, ok := v["max"].(json.Number); !ok {
				t.Fatalf("%s demoted int64max to %#v", format, v["max"])
			}
		}
	}
}
