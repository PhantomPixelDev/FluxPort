package conversion

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestColumnRulesAndStreaming(t *testing.T) {
	columns, e := ParseColumnTypes("id=text\nprice=number\nactive=boolean\nmeta=json")
	if e != nil {
		t.Fatal(e)
	}
	input := []byte("id,price,active,meta\n00123,2.50,true,\"{\"\"a\"\":[1,null]}\"\n")
	o := Options{CSVInferTypes: true, CSVColumnTypes: columns}
	v, e := DecodeWithOptions(input, "csv", o)
	if e != nil {
		t.Fatal(e)
	}
	row := v.([]any)[0].(map[string]any)
	if row["id"] != "00123" || row["price"] != json.Number("2.50") || row["active"] != true {
		t.Fatal(row)
	}
	var got []any
	e = IterateWithOptions(context.Background(), bytes.NewReader(input), "csv", 1<<20, 16<<20, o, func(v any) error { got = append(got, v); return nil })
	if e != nil || !reflect.DeepEqual(v, got) {
		t.Fatal(got, e)
	}
	for _, bad := range []string{"price=invalid", "id=text\nid=number", "missing"} {
		if _, e = ParseColumnTypes(bad); e == nil {
			t.Fatal(bad)
		}
	}
	for _, input := range []string{"price\nnull\n", "price\n\"2\"\n", "price\n0012\n"} {
		_, e = DecodeWithOptions([]byte(input), "csv", Options{CSVColumnTypes: map[string]string{"price": "number"}})
		// CSV quotes are syntax, so quoted 2 is a legitimate number.
		if strings.Contains(input, "\"2\"") {
			if e != nil {
				t.Fatal(e)
			}
		} else if e == nil {
			t.Fatal(input)
		}
	}
	if _, e = DecodeWithOptions(input, "csv", Options{CSVColumnTypes: map[string]string{"typo": "text"}}); e == nil {
		t.Fatal("unknown header accepted")
	}
	v, e = DecodeWithOptions([]byte("meta\n\"[1,{\"\"x\"\":2}]\"\n"), "csv", Options{CSVParseJSON: true})
	if e != nil || len(v.([]any)) != 1 {
		t.Fatal(v, e)
	}
	v, e = DecodeWithOptions([]byte("meta\n\"[1,{\"\"x\"\":2}]\"\n"), "csv", Options{CSVParseJSON: true, CSVColumnTypes: map[string]string{"meta": "text"}})
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := v.([]any)[0].(map[string]any)["meta"].(string); !ok {
		t.Fatal(v)
	}
}

func TestInspectionLimitsAndPaths(t *testing.T) {
	d := Differences(map[string]any{"a": nil}, map[string]any{"a": ""})
	if len(d) != 1 || !strings.Contains(d[0], `["a"]`) {
		t.Fatal(d)
	}
	values := make([]any, 11000)
	for i := range values {
		values[i] = json.Number("1")
	}
	d = Differences(values, values)
	if len(d) == 0 || !strings.Contains(d[len(d)-1], "limit") {
		t.Fatal("partial check incorrectly reported equality")
	}
	nodes := TreePreview(map[string]any{"id": json.Number("184467440737095516160")})
	if nodes[0].Children[0].Value != "184467440737095516160" {
		t.Fatal(nodes)
	}
	_, e := Decode([]byte("{\n \"x\": ]\n}"), "json")
	if e == nil || !strings.Contains(ExplainError([]byte("{\n \"x\": ]\n}"), e, "json"), "Line 2") {
		t.Fatal(e)
	}
}

func TestGeneratedNestedRoundTrips(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	var value func(int) any
	value = func(depth int) any {
		if depth > 4 {
			return json.Number("184467440737095516160")
		}
		switch rng.Intn(7) {
		case 0:
			return nil
		case 1:
			return rng.Intn(2) == 0
		case 2:
			return json.Number("1.234567890123456789")
		case 3:
			return "  Unicode 世界\nsecond <&> \t"
		case 4:
			return []any{value(depth + 1), value(depth + 1)}
		case 5:
			return map[string]any{"date": "2024-01-01", "nested": value(depth + 1)}
		default:
			return map[string]any{"empty": []any{}, "child": value(depth + 1)}
		}
	}
	for i := 0; i < 300; i++ {
		v := value(0)
		for _, f := range []string{"json", "yaml", "xml"} {
			b, e := Encode(v, f)
			if e != nil {
				t.Fatal(i, f, e)
			}
			back, e := Decode(b, f)
			if e != nil || !reflect.DeepEqual(v, back) {
				t.Fatalf("case %d %s: %v\n%s", i, f, e, b)
			}
		}
	}
	// TOML's representable subset and NDJSON's record-sequence semantics.
	for i := 0; i < 100; i++ {
		v := map[string]any{"id": json.Number(strconv.Itoa(rng.Intn(1000000))), "active": rng.Intn(2) == 0, "nested": map[string]any{"label": "世界\nhello " + strconv.Itoa(i)}}
		b, e := Encode(v, "toml")
		if e != nil {
			t.Fatal(e)
		}
		back, e := Decode(b, "toml")
		if e != nil || !reflect.DeepEqual(v, back) {
			t.Fatal(back, e)
		}
		rows := []any{v}
		b, e = Encode(rows, "ndjson")
		if e != nil {
			t.Fatal(e)
		}
		back, e = Decode(b, "ndjson")
		if e != nil || !reflect.DeepEqual(rows, back) {
			t.Fatal(back, e)
		}
		csvRows := []any{map[string]any{"id": "00123", "price": json.Number(strconv.Itoa(rng.Intn(100000))), "active": rng.Intn(2) == 0, "note": "世界,\nline " + strconv.Itoa(i)}}
		b, e = Encode(csvRows, "csv")
		if e != nil {
			t.Fatal(e)
		}
		back, e = DecodeWithOptions(b, "csv", Options{CSVInferTypes: true})
		if e != nil || !reflect.DeepEqual(csvRows, back) {
			t.Fatal(back, e)
		}
	}
}

func FuzzConversionRoundTrip(f *testing.F) {
	for _, seed := range []string{`{"a":[1,null,{"s":"世界"}]}`, `{"x":"  first\nsecond"}`, `184467440737095516160`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 16384 {
			return
		}
		v, e := Decode([]byte(input), "json")
		if e != nil {
			return
		}
		for _, format := range []string{"json", "yaml", "xml"} {
			b, e := Encode(v, format)
			if e != nil {
				continue
			}
			back, e := Decode(b, format)
			if e != nil || !reflect.DeepEqual(v, back) {
				t.Fatalf("%s round trip: %v", format, e)
			}
		}
	})
}
