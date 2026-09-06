package tools

import (
	"fmt"

	agent "github.com/alfredxw/denova/agent"
	"github.com/invopop/jsonschema"
)

func reflectedToolSchema[T any]() (*jsonschema.Schema, error) {
	params, err := agent.GoStruct2ParamsOneOf[T]()
	if err != nil {
		return nil, err
	}
	return params.ToJSONSchema()
}

func newSchemaTool[T, D any](name, description string, schema *jsonschema.Schema, invoke agent.InvokeFunc[T, D]) (agent.Tool, error) {
	if schema == nil {
		return nil, fmt.Errorf("build %s tool: schema is nil", name)
	}
	info := &agent.ToolInfo{
		Name: name, Desc: description, ParamsOneOf: agent.NewParamsOneOfByJSONSchema(schema),
	}
	return agent.NewTool(info, invoke), nil
}
