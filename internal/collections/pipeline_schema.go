package collections

import "strings"

// Empty results still retain columns whose shape is known from the recipe.
// This is display/export metadata only; it never synthesizes data records.
func (s *Service) emptyPipelineFields(p Pipeline) []Field {
	c, err := s.Get(p.Source)
	if err != nil {
		return nil
	}
	fields, err := s.Fields(c)
	if err != nil {
		return nil
	}
	known := map[string]map[string]bool{}
	add := func(fields []Field) {
		for _, f := range fields {
			if known[f.Name] == nil {
				known[f.Name] = map[string]bool{}
			}
			for _, typ := range strings.Split(f.Types, ", ") {
				known[f.Name][typ] = true
			}
		}
	}
	add(fields)
	for _, step := range p.Steps {
		if len(step.Rules.Select) > 0 {
			next := map[string]map[string]bool{}
			for _, name := range step.Rules.Select {
				if types, ok := known[name]; ok {
					next[name] = types
				}
			}
			known = next
		}
		for _, name := range step.Rules.Drop {
			delete(known, name)
		}
		if len(step.Rules.Rename) > 0 {
			next := map[string]map[string]bool{}
			for name, types := range known {
				if to, ok := step.Rules.Rename[name]; ok {
					name = to
				}
				next[name] = types
			}
			known = next
		}
		if step.Op == "group" {
			next := map[string]map[string]bool{}
			for _, name := range step.Keys {
				types := known[name]
				if types == nil {
					types = map[string]bool{"object": true}
				}
				next[name] = types
			}
			for _, a := range step.Aggregates {
				next[a.As] = map[string]bool{"number": true}
			}
			known = next
		}
		if step.Op == "map" {
			add(Schema([]any{step.Set}))
			for to, from := range step.Copy {
				parts, e := fieldParts(from)
				if e == nil && len(parts) == 1 && known[parts[0]] != nil {
					known[to] = known[parts[0]]
				} else {
					known[to] = map[string]bool{"object": true}
				}
			}
		}
		if step.Join != nil {
			known[step.Join.As] = map[string]bool{"object": true, "null": true}
		}
		if step.Append != "" {
			if c, e := s.Get(step.Append); e == nil {
				if f, e := s.Fields(c); e == nil {
					add(f)
				}
			}
		}
		if len(known) > 2048 {
			return nil
		}
	}
	return sortedFields(known)
}
