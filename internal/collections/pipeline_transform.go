package collections

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"fluxport/internal/conversion"
	"fluxport/internal/transformations"
	"modernc.org/sqlite"
)

func init() {
	err := sqlite.RegisterDeterministicScalarFunction("fluxport_transform", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		payload, ok := args[0].(string)
		if !ok || len(payload) > 1<<20 {
			return nil, fmt.Errorf("transform requires a record under 1 MiB")
		}
		config, ok := args[1].(string)
		if !ok || len(config) > 8192 {
			return nil, fmt.Errorf("invalid transform configuration")
		}
		row, err := conversion.Decode([]byte(payload), "json")
		if err != nil {
			return nil, err
		}
		var op transformations.Operation
		d := json.NewDecoder(strings.NewReader(config))
		d.UseNumber()
		d.DisallowUnknownFields()
		if err = d.Decode(&op); err != nil {
			return nil, err
		}
		out, err := transformations.ApplyOperation(row, op)
		if err != nil {
			return nil, err
		}
		b, err := conversion.MarshalJSON(out)
		if len(b) > 1<<20 {
			return nil, fmt.Errorf("transformed record exceeds 1 MiB")
		}
		return string(b), err
	})
	if err != nil {
		panic(err)
	}
}
