package collections

import (
	"encoding/json"
	"fluxport/internal/conversion"
	"fluxport/internal/quality"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

type parquetColumn struct {
	Kind          string
	Scale, Digits int
	Small         bool
}

func exportFormat(format string, options conversion.Options) (string, conversion.Options) {
	format = conversion.Format(format)
	if format == "parquet-typed" {
		format = "parquet"
		options.ParquetTyped = true
	}
	return format, options
}
func ExportFormats() []string { return append(DatasetFormats(), "parquet-typed") }

func TypedParquetPreview(rows []any) ([]string, error) {
	columns, err := inferParquetColumns(func(emit func(any) error) error {
		for _, row := range rows {
			if err := emit(row); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	notes := []string{}
	for name, column := range columns {
		notes = append(notes, name+": "+parquetColumnType(column)+" in checked rows.")
	}
	sort.Strings(notes)
	return notes, nil
}

// Exact decimal bounds, without allocating expanded strings for huge exponents.
func decimalShape(text string) (digits, scale int, small bool, ok bool) {
	if len(text) > 1024 {
		return 0, 0, false, false
	}
	n, valid := quality.Number(json.Number(text))
	if !valid {
		return 0, 0, false, false
	}
	denominator := new(big.Int).Set(n.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	factors := []int{0, 0}
	for i, factor := range []*big.Int{two, five} {
		for denominator.Cmp(big.NewInt(1)) > 0 && new(big.Int).Mod(denominator, factor).Sign() == 0 {
			denominator.Quo(denominator, factor)
			factors[i]++
			if factors[i] > 38 {
				return 0, 0, false, false
			}
		}
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return 0, 0, false, false
	}
	scale = max(factors[0], factors[1])
	integer := new(big.Int).Quo(n.Num(), n.Denom())
	digits = len(new(big.Int).Abs(integer).String())
	if integer.Sign() == 0 {
		digits = 0
	}
	if digits+scale > 38 {
		return 0, 0, false, false
	}
	return digits, scale, scale == 0 && n.Num().IsInt64(), true
}

func inferParquetColumns(each func(func(any) error) error) (map[string]*parquetColumn, error) {
	columns := map[string]*parquetColumn{}
	err := each(func(row any) error {
		obj, ok := row.(map[string]any)
		if !ok {
			return fmt.Errorf("Typed Parquet requires object records; use lossless Parquet for scalar records")
		}
		for name, value := range obj {
			if name == "" || len(name) > 4096 {
				return fmt.Errorf("Typed Parquet requires non-empty column names of at most 4096 bytes")
			}
			c := columns[name]
			if c == nil {
				if len(columns) >= 2048 {
					return fmt.Errorf("Typed Parquet supports at most 2048 columns")
				}
				c = &parquetColumn{Small: true}
				columns[name] = c
			}
			if value == nil {
				continue
			}
			kind := "text"
			switch v := value.(type) {
			case bool:
				kind = "boolean"
			case json.Number:
				d, scale, small, ok := decimalShape(v.String())
				if ok {
					kind = "number"
					c.Digits = max(c.Digits, d)
					c.Scale = max(c.Scale, scale)
					c.Small = c.Small && small
					if c.Digits+c.Scale > 38 {
						kind = "text"
					}
				}
			}
			if c.Kind == "" {
				c.Kind = kind
			} else if c.Kind != kind {
				c.Kind = "text"
			}
		}
		return nil
	})
	if err == nil && len(columns) == 1 && columns["__fluxport_record_json_v1"] != nil {
		return nil, fmt.Errorf("A sole __fluxport_record_json_v1 column is reserved for lossless records; rename it or choose lossless Parquet")
	}
	return columns, err
}
func parquetColumnType(c *parquetColumn) string {
	if c.Kind == "boolean" {
		return "BOOLEAN"
	}
	if c.Kind == "number" {
		if c.Small {
			return "BIGINT"
		}
		return fmt.Sprintf("DECIMAL(38,%d)", c.Scale)
	}
	return "VARCHAR"
}
func typedParquetSQL(path string, columns map[string]*parquetColumn, recordBytes int64) (string, error) {
	if len(columns) == 0 {
		return "", fmt.Errorf("Typed Parquet requires at least one named column")
	}
	names := []string{}
	for name := range columns {
		names = append(names, name)
	}
	sort.Strings(names)
	schema, projection := []string{}, []string{}
	for _, name := range names {
		schema = append(schema, quoteSQL(name)+":'VARCHAR'")
		id := quoteIdentifier(name)
		projection = append(projection, "CAST("+id+" AS "+parquetColumnType(columns[name])+") AS "+id)
	}
	return "SELECT " + strings.Join(projection, ",") + " FROM read_ndjson(" + quoteSQL(path) + ",columns={" + strings.Join(schema, ",") + "},maximum_object_size=" + fmt.Sprint(recordBytes*2+1024) + ")", nil
}
