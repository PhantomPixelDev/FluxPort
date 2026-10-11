package quality

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"unicode/utf8"
)

type Column struct {
	NumericSkipped int64    `json:"numeric_skipped"`
	Name           string   `json:"name"`
	Types          []string `json:"types"`
	Present        int64    `json:"present"`
	Missing        int64    `json:"missing"`
	Null           int64    `json:"null"`
	Distinct       int      `json:"distinct"`
	DistinctCapped bool     `json:"distinct_capped"`
	NumericCount   int64    `json:"numeric_count"`
	Min            string   `json:"min,omitempty"`
	Max            string   `json:"max,omitempty"`
	Mean           string   `json:"mean,omitempty"`
	MinLength      *int     `json:"min_length,omitempty"`
	MaxLength      *int     `json:"max_length,omitempty"`
}
type columnState struct {
	Column
	types         map[string]bool
	distinct      map[[32]byte]bool
	min, max, sum *big.Rat
}
type Profiler struct {
	Records int64
	columns map[string]*columnState
}

func NewProfiler() *Profiler { return &Profiler{columns: map[string]*columnState{}} }
func (p *Profiler) Add(row any) error {
	p.Records++
	obj, ok := row.(map[string]any)
	if !ok {
		obj = map[string]any{"$value": row}
	}
	for name, value := range obj {
		if len(name) > 4096 {
			return fmt.Errorf("profiling column names are limited to 4096 bytes")
		}
		c := p.columns[name]
		if c == nil {
			if len(p.columns) >= 256 {
				return fmt.Errorf("profiling supports up to 256 columns; select fewer columns")
			}
			c = &columnState{Column: Column{Name: name}, types: map[string]bool{}, distinct: map[[32]byte]bool{}, sum: new(big.Rat)}
			p.columns[name] = c
		}
		c.Present++
		c.types[Type(value)] = true
		if value == nil {
			c.Null++
		}
		b, err := json.Marshal(value)
		if err != nil {
			return err
		}
		key := sha256.Sum256(b)
		if !c.distinct[key] {
			if len(c.distinct) < 1024 {
				c.distinct[key] = true
			} else {
				c.DistinctCapped = true
			}
		}
		if n, ok := Number(value); ok {
			c.NumericCount++
			c.sum.Add(c.sum, n)
			if c.min == nil || n.Cmp(c.min) < 0 {
				c.min = new(big.Rat).Set(n)
				c.Min = string(b)
			}
			if c.max == nil || n.Cmp(c.max) > 0 {
				c.max = new(big.Rat).Set(n)
				c.Max = string(b)
			}
		} else if Type(value) == "number" {
			c.NumericSkipped++
		}
		if text, ok := value.(string); ok {
			n := utf8.RuneCountInString(text)
			if c.MinLength == nil || n < *c.MinLength {
				c.MinLength = &n
			}
			if c.MaxLength == nil || n > *c.MaxLength {
				c.MaxLength = &n
			}
		}
	}
	return nil
}
func (p *Profiler) Columns() []Column {
	result := make([]Column, 0, len(p.columns))
	for _, c := range p.columns {
		c.Missing = p.Records - c.Present
		c.Distinct = len(c.distinct)
		c.Types = nil
		for t := range c.types {
			c.Types = append(c.Types, t)
		}
		sort.Strings(c.Types)
		if c.NumericCount > 0 {
			c.Mean = new(big.Rat).Quo(c.sum, new(big.Rat).SetInt64(c.NumericCount)).FloatString(8)
		}
		result = append(result, c.Column)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
