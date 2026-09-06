package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	agent "github.com/alfredxw/denova/agent"
	"github.com/invopop/jsonschema"
)

type schemaTaskExecutor struct{}

func (schemaTaskExecutor) Identity() agent.CapabilityIdentity {
	return agent.CapabilityIdentity{Kind: "tasks.schema-test", Version: 1}
}
func (schemaTaskExecutor) Start(context.Context, TaskRequest) (Task, error) { return Task{}, nil }
func (schemaTaskExecutor) Observe(context.Context, TaskRef, string) (TaskObservation, error) {
	return TaskObservation{}, nil
}
func (schemaTaskExecutor) Wait(context.Context, []TaskRef) ([]TaskWaitOutcome, error) {
	return nil, nil
}
func (schemaTaskExecutor) Steer(context.Context, TaskRef, agent.Input) error { return nil }
func (schemaTaskExecutor) Respond(context.Context, TaskRef, string, agent.InteractionResponse) error {
	return nil
}
func (schemaTaskExecutor) Abort(context.Context, TaskRef, agent.AbortRequest) error { return nil }

type schemaSkillLoader struct{}

func (schemaSkillLoader) Identity() agent.CapabilityIdentity {
	return agent.CapabilityIdentity{Kind: "skills.schema-test", Version: 1}
}
func (schemaSkillLoader) Load(context.Context, string) (string, error) { return "instructions", nil }

// Local inference servers may enumerate only properties when building tool-call
// grammars. Operation arguments must be visible without following a root union.
func TestActionToolsExposeParametersDirectly(t *testing.T) {
	for _, test := range []struct {
		name       string
		build      func() agent.Toolset
		properties []string
	}{
		{"task", func() agent.Toolset { return Tasks(schemaTaskExecutor{}) }, []string{"action", "input", "reason", "refs", "starts", "targets"}},
		{"todo", func() agent.Toolset { return Todo() }, []string{"action", "expected_revision", "items", "mutations"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema := preparedNamedToolSchema(t, test.build, test.name)
			if schema.Type != "object" || len(schema.OneOf) != 0 || len(schema.AnyOf) != 0 || !reflect.DeepEqual(schemaPropertyNames(schema), test.properties) {
				t.Fatalf("parameters are hidden from a properties-only tool parser: %#v", schema)
			}
			if !reflect.DeepEqual(schema.Required, []string{"action"}) {
				t.Fatalf("unconditional required parameters = %#v", schema.Required)
			}
		})
	}
}

func TestTaskWaitUsesOneClosedTargetSchema(t *testing.T) {
	schema := preparedNamedToolSchema(t, func() agent.Toolset { return Tasks(schemaTaskExecutor{}) }, "task_wait")
	if schema.Type != "object" || !containsString(schema.Required, "targets") {
		t.Fatalf("task_wait schema = %#v", schema)
	}
	if got := schemaPropertyNames(schema); !reflect.DeepEqual(got, []string{"targets"}) {
		t.Fatalf("task_wait properties = %#v", got)
	}
	encoded, err := json.Marshal(schema)
	if err != nil || !strings.Contains(string(encoded), `"additionalProperties":false`) {
		t.Fatalf("task_wait schema is not closed: %s, %v", encoded, err)
	}
}

func TestSkillToolLoadsOneExactName(t *testing.T) {
	schema := preparedNamedToolSchema(t, func() agent.Toolset { return Skills(schemaSkillLoader{}) }, "skill")
	if schema.Type != "object" || len(schema.OneOf) != 0 || !containsString(schema.Required, "name") {
		t.Fatalf("skill schema = %#v", schema)
	}
	if got := schemaPropertyNames(schema); !reflect.DeepEqual(got, []string{"name"}) {
		t.Fatalf("skill properties = %#v", got)
	}
	encoded, err := json.Marshal(schema)
	if err != nil || !strings.Contains(string(encoded), `"additionalProperties":false`) {
		t.Fatalf("skill schema is not closed: %s, %v", encoded, err)
	}
}

func TestAskSchemaUsesOneQuestionShape(t *testing.T) {
	schema := preparedToolSchema(t, Ask)
	questions, ok := schema.Properties.Get("questions")
	if !ok || questions.Items == nil || len(questions.Items.OneOf) != 0 || len(questions.Items.AnyOf) != 0 {
		t.Fatalf("ask questions must directly expose their fields: %#v", questions)
	}
	if got := schemaPropertyNames(questions.Items); !reflect.DeepEqual(got, []string{"id", "multiple", "options", "prompt"}) {
		t.Fatalf("question properties = %#v", got)
	}
	if !reflect.DeepEqual(questions.Items.Required, []string{"id", "prompt"}) {
		t.Fatalf("required question fields = %#v", questions.Items.Required)
	}
	options, _ := questions.Items.Properties.Get("options")
	if options == nil || options.Contains != nil || options.MinContains != nil || options.MaxContains != nil {
		t.Fatalf("recommendation cardinality belongs to interaction validation: %#v", options)
	}
	info := &agent.ToolInfo{Name: "ask", ParamsOneOf: agent.NewParamsOneOfByJSONSchema(schema)}
	for _, arguments := range []string{
		`{"questions":[{"id":"scope","prompt":"Which scope?"}]}`,
		`{"questions":[{"id":"scope","prompt":"Which scope?","options":[]}]}`,
		`{"questions":[{"id":"scope","prompt":"Which scope?","options":[{"value":"a","label":"A","recommended":true},{"value":"b","label":"B"}]}]}`,
	} {
		if _, err := agent.NormalizeToolArguments(info, arguments); err != nil {
			t.Errorf("valid question shape rejected: %s: %v", arguments, err)
		}
	}
}

func preparedToolSchema(t *testing.T, build func() agent.Toolset) *jsonschema.Schema {
	return preparedNamedToolSchema(t, build, "")
}

func preparedNamedToolSchema(t *testing.T, build func() agent.Toolset, name string) *jsonschema.Schema {
	t.Helper()
	toolset := build()
	definitions, err := toolset.PrepareTools(context.Background(), agent.ToolRequest{})
	if err != nil || len(definitions) == 0 {
		t.Fatalf("definitions = %d, error = %v", len(definitions), err)
	}
	definition := definitions[0]
	if name != "" {
		found := false
		for _, candidate := range definitions {
			info, infoErr := candidate.Tool.Info(context.Background())
			if infoErr != nil {
				t.Fatal(infoErr)
			}
			if info.Name == name {
				definition, found = candidate, true
				break
			}
		}
		if !found {
			t.Fatalf("tool %q was not prepared", name)
		}
	}
	info, err := definition.Tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	schema, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func schemaPropertyNames(schema *jsonschema.Schema) []string {
	result := make([]string, 0)
	if schema != nil && schema.Properties != nil {
		for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			result = append(result, pair.Key)
		}
	}
	sort.Strings(result)
	return result
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
