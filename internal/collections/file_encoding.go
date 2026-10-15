package collections

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"fluxport/internal/conversion"
)

// File editing preserves a document root; an array is not silently introduced.
func encodeFileRows(c Collection, original any, rows []any) ([]byte, error) {
	if _, array := original.([]any); !array {
		if len(rows) != 1 {
			return nil, fmt.Errorf("a single document must retain one record")
		}
		if _, array := rows[0].([]any); array {
			return nil, fmt.Errorf("use import to change the document root to an array")
		}
		return conversion.Encode(rows[0], c.Format)
	}
	if c.Format == "csv" && len(rows) == 0 {
		var fields []string
		before := conversion.Rows(original)
		if len(before) > 0 {
			if object, ok := before[0].(map[string]any); ok {
				for key := range object {
					fields = append(fields, key)
				}
			}
		}
		if len(fields) == 0 {
			var schema []Field
			if err := json.Unmarshal([]byte(c.SchemaJSON), &schema); err != nil {
				return nil, err
			}
			for _, field := range schema {
				fields = append(fields, field.Name)
			}
		}
		sort.Strings(fields)
		var output bytes.Buffer
		writer, err := conversion.NewRowWriter(&output, "csv", fields)
		if err != nil {
			return nil, err
		}
		if err = writer.Close(); err != nil {
			return nil, err
		}
		return output.Bytes(), nil
	}
	return conversion.Encode(rows, c.Format)
}
func (s *Service) validateFileOutput(c Collection, b []byte, records int) error {
	if int64(len(b)) > s.MaxBytes {
		return ErrTooLarge
	}
	count := 0
	err := conversion.Iterate(context.Background(), bytes.NewReader(b), c.Format, s.RecordBytes, s.MaxBytes, func(row any) error {
		count++
		if count > 100000 {
			return fmt.Errorf("file editing supports at most 100000 records")
		}
		payload, err := conversion.MarshalJSON(row)
		if err != nil {
			return err
		}
		if int64(len(payload)) > s.RecordBytes {
			return fmt.Errorf("record exceeds size limit")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count != records {
		return fmt.Errorf("conversion changed the record structure; file left unchanged")
	}
	return nil
}
