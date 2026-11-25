package handler

import (
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// jsonschemaCompile compiles a schema with the validator already in go.mod
// (santhosh-tekuri/jsonschema/v5). The contract table's schema column uses it;
// google/jsonschema-go is the cross-check where a second opinion is needed.
func jsonschemaCompile(schema string) (*jsonschema.Schema, error) {
	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", strings.NewReader(schema)); err != nil {
		return nil, err
	}
	return c.Compile("schema.json")
}
