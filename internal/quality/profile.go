package quality

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"unicode/utf8"
)

type Column struct {
	Repeated       int64        `json:"repeated_non_null"`
	CommonCapped   bool         `json:"common_capped"`
	Empty          int64        `json:"empty"`
	Whitespace     int64        `json:"whitespace"`
	Common         []ValueCount `json:"common_values,omitempty"`
	NumericSkipped int64        `json:"numeric_skipped"`
	Name           string       `json:"name"`
	Types          []string     `json:"types"`
	Present        int64        `json:"present"`
	Missing        int64        `json:"missing"`
	Null           int64        `json:"null"`
	Distinct       int          `json:"distinct"`
	DistinctCapped bool         `json:"distinct_capped"`
	NumericCount   int64        `json:"numeric_count"`
	Min            string       `json:"min,omitempty"`
	Max            string       `json:"max,omitempty"`
	Mean           string       `json:"mean,omitempty"`
	MinLength      *int         `json:"min_length,omitempty"`
	MaxLength      *int         `json:"max_length,omitempty"`
}
type ValueCount struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}
type columnState struct {
	Column
	types         map[string]bool
	distinct      map[[32]byte]bool
	counts        map[[32]byte]ValueCount
	min, max, sum *big.Rat
}
type Profiler struct {
	limit         int
	Records       int64
	DuplicateRows int64
	RowsCapped    bool
	rows          map[[32]byte]bool
	columns       map[string]*columnState
}

func NewProfiler() *Profiler {
	return NewProfilerWithLimit(256)
}
func NewProfilerWithLimit(limit int) *Profiler {
	return &Profiler{limit: min(max(limit, 1), 2048), columns: map[string]*columnState{}, rows: map[[32]byte]bool{}}
}
func (p *Profiler) Add(row any) error {
	return p.AddColumn(row, "")
}
func (p *Profiler) EnsureColumn(name string) error {
	if len(name) > 4096 {
		return fmt.Errorf("profiling column names are limited to 4096 bytes")
	}
	if p.columns[name] == nil {
		if len(p.columns) >= p.limit {
			return fmt.Errorf("profiling supports up to %d columns; select fewer columns", p.limit)
		}
		p.columns[name] = &columnState{Column: Column{Name: name}, types: map[string]bool{}, distinct: map[[32]byte]bool{}, counts: map[[32]byte]ValueCount{}, sum: new(big.Rat)}
	}
	return nil
}
func (p *Profiler) AddColumn(row any, selected string) error {
	b, err := json.Marshal(row)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(b)
	if p.rows[hash] {
		p.DuplicateRows++
	} else if len(p.rows) < 10000 {
		p.rows[hash] = true
	} else {
		p.RowsCapped = true
	}
	p.Records++
	obj, ok := row.(map[string]any)
	if !ok {
		obj = map[string]any{"$value": row}
	}
	if selected != "" {
		projected := map[string]any{}
		if value, exists := obj[selected]; exists {
			projected[selected] = value
		}
		obj = projected
	}
	for name, value := range obj {
		if len(name) > 4096 {
			return fmt.Errorf("profiling column names are limited to 4096 bytes")
		}
		c := p.columns[name]
		if c == nil {
			if len(p.columns) >= p.limit {
				return fmt.Errorf("profiling supports up to %d columns; select fewer columns", p.limit)
			}
			c = &columnState{Column: Column{Name: name}, types: map[string]bool{}, distinct: map[[32]byte]bool{}, counts: map[[32]byte]ValueCount{}, sum: new(big.Rat)}
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
		if value != nil && c.distinct[key] {
			c.Repeated++
		}
		if _, tracked := c.counts[key]; tracked || len(c.counts) < 64 {
			entry := c.counts[key]
			text := string(b)
			if utf8.RuneCountInString(text) > 160 {
				text = string([]rune(text)[:160]) + "…"
			}
			entry.Value, entry.Count = text, entry.Count+1
			c.counts[key] = entry
		} else {
			c.CommonCapped = true
		}
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
			if text == "" {
				c.Empty++
			}
			if strings.TrimSpace(text) != text {
				c.Whitespace++
			}
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
		c.Common = nil
		for _, value := range c.counts {
			c.Common = append(c.Common, value)
		}
		sort.Slice(c.Common, func(i, j int) bool {
			if c.Common[i].Count != c.Common[j].Count {
				return c.Common[i].Count > c.Common[j].Count
			}
			return c.Common[i].Value < c.Common[j].Value
		})
		if len(c.Common) > 5 {
			c.Common = c.Common[:5]
		}
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
