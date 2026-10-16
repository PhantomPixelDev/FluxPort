package collections

import (
	"fmt"
	"strings"
)

// BindSQL recognizes named scalar parameters outside quotes and comments.
// Values become escaped literals before the existing SQL sandbox validates them.
func BindSQL(text string, values map[string]any) (string, error) {
	if len(text) > 16384 || len(values) > 32 {
		return "", fmt.Errorf("query or parameter limit exceeded")
	}
	var out strings.Builder
	used := map[string]bool{}
	for i := 0; i < len(text); {
		start := i
		if text[i] == '\'' || text[i] == '"' || text[i] == '`' || text[i] == '[' {
			end := text[i]
			if end == '[' {
				end = ']'
			}
			i++
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
				return "", fmt.Errorf("unterminated SQL quote")
			}
			out.WriteString(text[start:i])
			continue
		}
		if i+1 < len(text) && text[i:i+2] == "--" {
			for i < len(text) && text[i] != '\n' {
				i++
			}
			out.WriteString(text[start:i])
			continue
		}
		if i+1 < len(text) && text[i:i+2] == "/*" {
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return "", fmt.Errorf("unterminated SQL comment")
			}
			i += end + 4
			out.WriteString(text[start:i])
			continue
		}
		if i+1 < len(text) && text[i:i+2] == "::" {
			out.WriteString("::")
			i += 2
			continue
		}
		if text[i] == ':' && i+1 < len(text) && sqlParameterStart(text[i+1]) {
			i += 2
			for i < len(text) && (sqlParameterStart(text[i]) || text[i] >= '0' && text[i] <= '9') {
				i++
			}
			key := text[start+1 : i]
			value, ok := values[key]
			if !ok {
				return "", fmt.Errorf("missing SQL parameter %q", key)
			}
			literal, err := sqlLiteral(value)
			if err != nil {
				return "", fmt.Errorf("parameter %s: %w", key, err)
			}
			if len(literal) > 4096 {
				return "", fmt.Errorf("SQL parameter exceeds 4 KiB")
			}
			out.WriteString(literal)
			used[key] = true
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	for key := range values {
		if !used[key] {
			return "", fmt.Errorf("unused SQL parameter %q", key)
		}
	}
	if out.Len() > 16384 {
		return "", fmt.Errorf("expanded SQL exceeds 16 KiB")
	}
	return out.String(), nil
}
func sqlParameterStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
