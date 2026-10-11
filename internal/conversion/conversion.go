// Package conversion normalizes supported formats into JSON-compatible values.
package conversion

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"
)

var Formats = []string{"json", "csv", "yaml", "xml", "ndjson", "toml"}

// errEmptyInput is returned for whitespace-only input instead of the underlying
// decoder's io.EOF, and applies uniformly to every format.
var errEmptyInput = errors.New("input is empty")

func Format(s string) string {
	s = strings.ToLower(strings.TrimPrefix(s, "."))
	if s == "yml" {
		return "yaml"
	}
	if s == "jsonl" {
		return "ndjson"
	}
	return s
}
func FromFilename(name string) string { return Format(filepath.Ext(name)) }
func Supported(f string) bool {
	for _, s := range Formats {
		if Format(f) == s {
			return true
		}
	}
	return false
}
func MIME(f string) string {
	return map[string]string{"json": "application/json", "csv": "text/csv", "yaml": "application/yaml", "xml": "application/xml", "ndjson": "application/x-ndjson", "toml": "application/toml"}[Format(f)]
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func Decode(b []byte, f string) (any, error) {
	var v any
	b = bytes.TrimPrefix(b, utf8BOM)
	if !utf8.Valid(b) {
		return nil, fmt.Errorf("input contains invalid UTF-8")
	}
	if len(bytes.TrimSpace(b)) == 0 {
		if Format(f)=="ndjson"{return []any{},nil}
		if Format(f)=="toml"{return map[string]any{},nil}
		return nil, errEmptyInput
	}
	switch Format(f) {
	case "json":
		return decodeJSON(b)
	case "yaml":
		return decodeYAML(b)
	case "toml":
		if err := validateTOMLNumbers(b); err != nil {
			return nil, err
		}
		if err := toml.Unmarshal(b, &v); err != nil {
			return nil, err
		}
	case "ndjson":
		rows := []any{}
		for i, line := range bytes.Split(b, []byte{'\n'}) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			row, err := Decode(line, "json")
			if err != nil {
				return nil, fmt.Errorf("NDJSON line %d: %w", i+1, err)
			}
			rows = append(rows, row)
		}
		v = rows
	case "csv":
		return DecodeWithOptions(b, f, Options{})
	case "xml":
		n, err := readXML(xml.NewDecoder(bytes.NewReader(b)), 0)
		if err != nil {
			return nil, err
		}
		if n.Key == "fluxport" && n.Attrs["encoding"] == "json" {
			if len(n.Children)>0||len(n.Attrs)!=1{return nil,fmt.Errorf("FluxPort XML JSON envelope cannot contain child elements or extra attributes")}
			return Decode([]byte(n.Text), "json")
		}
		v = map[string]any{n.Key: n.value()}
	default:
		return nil, fmt.Errorf("unsupported format %q", f)
	}
	// Normalize YAML maps and TOML dates, reject non-string keys / non-finite numbers.
	b, err := marshalJSON(v)
	if err != nil {
		return nil, fmt.Errorf("data is not JSON-compatible: %w", err)
	}
	return decodeJSON(b)
}

// MarshalJSON encodes without HTML escaping, so < > & keep their source bytes.
func MarshalJSON(v any) ([]byte, error) {
	return marshalJSON(v)
}

// MarshalJSONIndent encodes without HTML escaping, matching MarshalJSON so
// indented and compact output differ only in whitespace.
func MarshalJSONIndent(v any) ([]byte, error) {
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}

// htmlEscape reports whether the standard library would rewrite < > & as \u003c
// and friends. FluxPort never writes HTML, so output keeps the source bytes.
func marshalJSON(v any) ([]byte, error) {
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}

// Encode serializes a decoded value within the target format's capabilities.
// CSV stores text cells; TOML rejects values it cannot represent faithfully.
func Encode(v any, f string) ([]byte, error) {
	switch Format(f) {
	case "json":
		return MarshalJSONIndent(v)
	case "yaml":
		v, err := numericValues(v, "yaml")
		if err != nil {
			return nil, err
		}
		var out bytes.Buffer
		encoder := yaml.NewEncoder(&out)
		encoder.SetIndent(2)
		if err := encoder.Encode(v); err != nil {
			return nil, err
		}
		if err := encoder.Close(); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	case "toml":
		if _, ok := v.(map[string]any); !ok {
			return nil, fmt.Errorf("TOML requires an object at the root; wrap arrays in a named field")
		}
		v, err := numericValues(v, "toml")
		if err != nil {
			return nil, err
		}
		return toml.Marshal(v)
	case "xml":
		// A typed JSON envelope preserves arrays, scalars, names, and nulls losslessly.
		b, err := marshalJSON(v)
		if err != nil {
			return nil, err
		}
		var out bytes.Buffer
		out.WriteString(xml.Header)
		out.WriteString("<fluxport encoding=\"json\">")
		if err = escapeXMLText(&out, b); err != nil {
			return nil, err
		}
		out.WriteString("</fluxport>\n")
		return out.Bytes(), nil
	case "ndjson":
		var out bytes.Buffer
		e := json.NewEncoder(&out)
		e.SetEscapeHTML(false)
		for _, row := range Rows(v) {
			if err := e.Encode(row); err != nil {
				return nil, err
			}
		}
		return out.Bytes(), nil
	case "csv":
		return encodeCSV(v, false)
	default:
		return nil, fmt.Errorf("unsupported format %q", f)
	}
}

// escapeXMLText escapes only the characters that cannot appear literally in XML
// character data. xml.EscapeText also rewrites " ' \n and \t into numeric
// entities, which makes the JSON envelope unreadable without any benefit.
func escapeXMLText(out io.Writer, b []byte) error {
	start := 0
	for i := 0; i < len(b); i++ {
		var esc []byte
		switch b[i] {
		case '<':
			esc = []byte("&lt;")
		case '>':
			esc = []byte("&gt;")
		case '&':
			esc = []byte("&amp;")
		case '\r':
			esc = []byte("&#13;")
		}
		if esc == nil {
			continue
		}
		if _, err := out.Write(b[start:i]); err != nil {
			return err
		}
		if _, err := out.Write(esc); err != nil {
			return err
		}
		start = i + 1
	}
	_, err := out.Write(b[start:])
	return err
}

func Rows(v any) []any {
	if rows, ok := v.([]any); ok {
		return rows
	}
	return []any{v}
}

// json.Number must not become a quoted string in YAML/TOML serializers.
// Unsupported TOML numbers fail explicitly instead of silently changing types.
func numericValues(v any, format string) (any, error) {
	return convertNumbers(v, format, "$")
}

func convertNumbers(v any, format, path string) (any, error) {
	if v == nil && format == "toml" {
		return nil, fmt.Errorf("%s: TOML cannot represent null values", path)
	}
	switch value := v.(type) {
	case json.Number:
		return numberNode(value, format, path)
	case map[string]any:
		out := map[string]any{}
		for k, item := range value {
			next, err := convertNumbers(item, format, path+"."+k)
			if err != nil {
				return nil, err
			}
			out[k] = next
		}
		return out, nil
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			next, err := convertNumbers(item, format, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out[i] = next
		}
		return out, nil
	default:
		return v, nil
	}
}

// Preserve numeric types; reject TOML values that would change value or type.
func numberNode(n json.Number, format, path string) (any, error) {
	lexeme := n.String()
	if format == "yaml" {
		tag := "!!int"
		if strings.ContainsAny(lexeme, ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: lexeme}, nil
	}
	if !strings.ContainsAny(lexeme, ".eE") {
		v, err := strconv.ParseInt(lexeme, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: TOML integers must fit signed 64 bits", path)
		}
		return v, nil
	}
	v, err := strconv.ParseFloat(lexeme, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return nil, fmt.Errorf("%s: TOML cannot represent this number without overflow", path)
	}
	if v == 0 {
		mantissa := strings.Split(strings.ToLower(lexeme), "e")[0]
		if strings.ContainsAny(mantissa, "123456789") {
			return nil, fmt.Errorf("%s: TOML number would underflow to zero", path)
		}
		return v, nil
	}
	original, ok := new(big.Rat).SetString(lexeme)
	if !ok {
		return nil, fmt.Errorf("%s: invalid number", path)
	}
	converted, ok := new(big.Rat).SetString(strconv.FormatFloat(v, 'g', -1, 64))
	if !ok || original.Cmp(converted) != 0 {
		return nil, fmt.Errorf("%s: TOML float would lose decimal precision; use JSON, YAML or XML", path)
	}
	return v, nil
}

type xmlNode struct {
	Name     xml.Name
	Key      string
	Text     string
	Attrs    map[string]string
	Children []*xmlNode
	Content  []any
}

// nsScope binds a namespace URI to the prefix the document declared for it.
// Go's decoder resolves prefixes to URIs, so the prefix is recovered by reverse
// lookup: a URI present in scope came from a declaration, while an unresolved
// prefix is echoed back verbatim by the decoder.
type nsScope struct {
	parent *nsScope
	uri    string
	prefix string
}

// newScope records namespace declarations made on one element. A bare xmlns
// attribute binds the default namespace, which contributes no prefix.
func newScope(parent *nsScope, t xml.StartElement) *nsScope {
	head := parent
	for _, a := range t.Attr {
		switch {
		case a.Name.Space == "xmlns":
			head = &nsScope{parent: head, uri: a.Value, prefix: a.Name.Local}
		case a.Name.Space == "" && a.Name.Local == "xmlns":
			head = &nsScope{parent: head, uri: a.Value, prefix: ""}
		}
	}
	return head
}

func (s *nsScope) prefixFor(uri string) (string, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.uri == uri {
			return cur.prefix, true
		}
	}
	return "", false
}

// qualified renders an element or attribute name with the prefix the source
// document used, so <a:x> and <b:x> stay distinct JSON keys. A default
// namespace contributes no prefix, matching the source document.
func qualified(n xml.Name, sc *nsScope) string {
	if n.Space != "" {
		return n.Space + ":" + n.Local
	}
	return n.Local
}
func readXML(d *xml.Decoder, depth int) (*xmlNode, error) {
	if depth > 64 {
		return nil, fmt.Errorf("XML nesting exceeds 64")
	}
	var n *xmlNode
	for {
		t, err := d.RawToken()
		if err != nil {
			if err == io.EOF && n != nil {
				return n, nil
			}
			return nil, err
		}
		switch t := t.(type) {
		case xml.StartElement:
			if n != nil {
				return nil, fmt.Errorf("multiple XML root elements")
			}
			scope := newScope(nil, t)
			n = &xmlNode{Name: t.Name, Key: qualified(t.Name, scope), Attrs: map[string]string{}}
			for _, a := range t.Attr {
				key := qualified(a.Name, scope)
				if _, ok := n.Attrs[key]; ok {
					return nil, fmt.Errorf("duplicate XML attribute %q", key)
				}
				n.Attrs[key] = a.Value
			}
			if err := readChildren(d, n, scope, depth); err != nil {
				return nil, err
			}
		case xml.EndElement:
			return nil, fmt.Errorf("XML closing element outside root")
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return nil, fmt.Errorf("text outside XML root")
			}
		case xml.Directive:
			return nil, fmt.Errorf("XML directives and DTDs are unsupported")
		}
	}
}
func readChildren(d *xml.Decoder, n *xmlNode, scope *nsScope, depth int) error {
	if depth > 64 {
		return fmt.Errorf("XML nesting exceeds 64")
	}
	for {
		t, err := d.RawToken()
		if err != nil {
			return err
		}
		switch t := t.(type) {
		case xml.StartElement:
			childScope := newScope(scope, t)
			child := &xmlNode{Name: t.Name, Key: qualified(t.Name, childScope), Attrs: map[string]string{}}
			for _, a := range t.Attr {
				key := qualified(a.Name, childScope)
				if _, ok := child.Attrs[key]; ok {
					return fmt.Errorf("duplicate XML attribute %q", key)
				}
				child.Attrs[key] = a.Value
			}
			if err := readChildren(d, child, childScope, depth+1); err != nil {
				return err
			}
			n.Children = append(n.Children, child)
			n.Content = append(n.Content, child)
		case xml.CharData:
			n.Text += string(t)
			n.Content = append(n.Content, string(t))
		case xml.EndElement:
			if t.Name != n.Name {
				return fmt.Errorf("XML closing element %q does not match %q", t.Name.Local, n.Name.Local)
			}
			return nil
		case xml.Directive:
			return fmt.Errorf("XML directives and DTDs are unsupported")
		}
	}
}
func (n *xmlNode) value() any {
	if len(n.Children) == 0 && len(n.Attrs) == 0 {
		return n.Text
	}
	v := map[string]any{}
	for k, s := range n.Attrs {
		v["@"+k] = s
	}
	if len(n.Children) > 0 && strings.TrimSpace(n.Text) != "" {
		content := []any{}
		for _, item := range n.Content {
			switch x := item.(type) {
			case string:
				if len(content) > 0 {
					if prev, ok := content[len(content)-1].(map[string]any); ok {
						if text, ok := prev["#text"].(string); ok {
							prev["#text"] = text + x
							continue
						}
					}
				}
				content = append(content, map[string]any{"#text": x})
			case *xmlNode:
				content = append(content, map[string]any{x.Key: x.value()})
			}
		}
		v["#content"] = content
		return v
	}
	if n.Text != "" {
		v["#text"] = n.Text
	}
	for _, c := range n.Children {
		item := c.value()
		if prev, ok := v[c.Key]; ok {
			if arr, ok := prev.([]any); ok {
				v[c.Key] = append(arr, item)
			} else {
				v[c.Key] = []any{prev, item}
			}
		} else {
			v[c.Key] = item
		}
	}
	return v
}
