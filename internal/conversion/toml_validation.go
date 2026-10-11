package conversion

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// TOML's decoder uses float64. Validate literals before that decoder can round
// them, including floats inside nested arrays and inline tables.
func validateTOMLNumbers(b []byte) error {
	var p unstable.Parser
	p.Reset(b)
	var visit func(*unstable.Node, int) error
	visit = func(n *unstable.Node, depth int) error {
		if depth > 512 {
			return fmt.Errorf("TOML nesting exceeds 512 levels")
		}
		if n.Kind == unstable.Float {
			raw := strings.ReplaceAll(string(p.Raw(n.Raw)), "_", "")
			raw = strings.TrimPrefix(raw, "+")
			position := p.Shape(n.Raw).Start
			if _, e := numberNode(json.Number(raw), "toml", fmt.Sprintf("line %d, column %d", position.Line, position.Column)); e != nil {
				return e
			}
		}
		children := n.Children()
		for children.Next() {
			if e := visit(children.Node(), depth+1); e != nil {
				return e
			}
		}
		return nil
	}
	for p.NextExpression() {
		if e := visit(p.Expression(), 0); e != nil {
			return e
		}
	}
	return p.Error()
}
