package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"denova/config"
	novaskills "denova/internal/agents/skills"
	"denova/internal/interactive"

	agent "github.com/alfredxw/denova/agent"
	"github.com/alfredxw/denova/agent/providers"
	"github.com/alfredxw/denova/agent/providers/protocols/anthropicmessages"
	"github.com/alfredxw/denova/agent/providers/protocols/openaichatcompletions"
	"github.com/alfredxw/denova/agent/providers/protocols/openairesponses"
	agentscript "github.com/alfredxw/denova/agent/script"
	publictools "github.com/alfredxw/denova/agent/tools"
)

// Exercise the actual wire payload, since a valid in-memory schema alone does
// not prove that a provider's tool-parameter parser can see the arguments.
func TestBuiltinSchemasExposeSameParametersAcrossProtocols(t *testing.T) {
	infos := builtinSchemaInfos(t)
	t.Logf("Checking %d built-in tool definitions across three protocols", len(infos))
	expected := make(map[string]map[string]any, len(infos))
	for _, info := range infos {
		parameters, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(parameters)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatal(err)
		}
		assertDirectBuiltinSchema(t, info.Name, schema)
		expected[info.Name] = schema
	}
	for _, adapter := range []providers.ProtocolAdapter{
		openaichatcompletions.NewAdapter(), openairesponses.NewAdapter(), anthropicmessages.NewAdapter(),
	} {
		t.Run(string(adapter.ID()), func(t *testing.T) {
			bodies := make(chan map[string]any, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				bodies <- body
				w.Header().Set("Content-Type", "application/json")
				switch adapter.ID() {
				case providers.ProtocolOpenAIChatCompletions:
					io.WriteString(w, `{"id":"test","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
				case providers.ProtocolOpenAIResponses:
					io.WriteString(w, `{"id":"test","object":"response","status":"completed","model":"test","output":[{"id":"msg","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok","annotations":[]}]}]}`)
				case providers.ProtocolAnthropicMessages:
					io.WriteString(w, `{"id":"test","type":"message","role":"assistant","model":"test","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
				default:
					t.Errorf("unhandled protocol: %s", adapter.ID())
				}
			}))
			defer server.Close()
			model, err := adapter.New(context.Background(), providers.ModelConfig{
				Provider: providers.ProviderOpenAICompatible, Protocol: adapter.ID(), Model: "test",
				BaseURL: server.URL, APIKey: "test", HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := model.Generate(context.Background(), []*agent.Message{agent.UserMessage("inspect")}, agent.WithTools(infos)); err != nil {
				t.Fatal(err)
			}
			body := <-bodies
			wireTools, ok := body["tools"].([]any)
			if !ok || len(wireTools) != len(infos) {
				t.Fatalf("tool payload count = %d, want %d", len(wireTools), len(infos))
			}
			for _, raw := range wireTools {
				tool := raw.(map[string]any)
				if function, ok := tool["function"].(map[string]any); ok {
					tool = function
				}
				name := tool["name"].(string)
				schema, ok := tool["parameters"].(map[string]any)
				if !ok {
					schema, ok = tool["input_schema"].(map[string]any)
				}
				if !ok {
					t.Fatalf("tool %s has no parameter schema", name)
				}
				assertDirectBuiltinSchema(t, name, schema)
				if !reflect.DeepEqual(schema, expected[name]) {
					t.Errorf("%s schema changed during serialization", name)
				}
			}
		})
	}
}

func assertDirectBuiltinSchema(t *testing.T, name string, schema map[string]any) {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if schema["type"] != "object" || !ok || len(properties) == 0 {
		t.Fatalf("%s arguments are hidden from a properties-only parser: %#v", name, schema)
	}
	var inspect func(string, any)
	inspect = func(path string, value any) {
		switch value := value.(type) {
		case map[string]any:
			for _, keyword := range []string{"oneOf", "anyOf", "allOf", "contains", "if", "then", "else"} {
				if _, exists := value[keyword]; exists {
					t.Errorf("%s uses conditional schema keyword %s", path, keyword)
				}
			}
			for key, child := range value {
				inspect(path+"."+key, child)
			}
		case []any:
			for _, child := range value {
				inspect(path+"[]", child)
			}
		}
	}
	inspect(name, schema)
}

type schemaOnlyTaskExecutor struct{ publictools.TaskExecutor }

func (schemaOnlyTaskExecutor) Identity() agent.CapabilityIdentity {
	return agent.CapabilityIdentity{Kind: "test.schema.tasks", Version: 1}
}

type schemaOnlyCommandRunner struct{ publictools.CommandRunner }

func (schemaOnlyCommandRunner) Identity() agent.CapabilityIdentity {
	return agent.CapabilityIdentity{Kind: "test.schema.shell", Version: 1}
}

// Build real writing/game/common definitions with isolated paths. No tool is
// executed and no model, browser, shell, or user workspace is accessed.
func builtinSchemaInfos(t *testing.T) []*agent.ToolInfo {
	t.Helper()
	ctx := context.Background()
	cfg := &config.Config{NovaDir: t.TempDir(), Workspace: t.TempDir(), ProjectStoreDir: t.TempDir()}
	settings := config.ResolvedAgentToolSettings{
		config.AgentToolFilesystemRead: true, config.AgentToolWorkspaceWrite: true,
		config.AgentToolLoreRead: true, config.AgentToolLoreWrite: true, config.AgentToolImageGeneration: true,
		config.AgentToolConfigRead: true, config.AgentToolConfigApply: true,
		config.AgentToolWebSearch: true, config.AgentToolWebFetch: true,
	}
	catalog := NewCatalog(cfg, nil, RuntimeExecutables{})
	definitions := []agent.ToolDefinition{}
	add := func(items []agent.ToolDefinition, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		definitions = append(definitions, items...)
	}
	add(catalog.Workspace(settings))
	for _, build := range []func(publictools.CommandRunner, ...publictools.DefinitionOption) (agent.ToolDefinition, error){publictools.Bash, publictools.Pwsh} {
		definition, err := build(schemaOnlyCommandRunner{})
		if err != nil {
			t.Fatal(err)
		}
		definitions = append(definitions, definition)
	}
	add(catalog.IDE()(settings))
	add(catalog.WebAccess(settings))
	add(catalog.InteractiveStory(InteractiveContext{
		Store: &interactive.Store{}, StoryID: "schema-test",
		SubmitStateSchemaBatch: func(context.Context, interactive.ActorStateSchemaBatch) (interactive.ActorStateSchemaBatchResult, error) {
			panic("schema only")
		},
		PrepareTurn: func(context.Context, interactive.TurnCheckRequest) (interactive.RuleResolution, error) {
			panic("schema only")
		},
		SelectStoryProtagonist: func(context.Context, string) (interactive.StoryProtagonist, error) { panic("schema only") },
		SubmitTurnResult: func(context.Context, interactive.TurnSubmissionInput) (interactive.TurnSubmissionReceipt, error) {
			panic("schema only")
		},
	})(config.ResolvedAgentToolSettings{}))
	for _, toolset := range []agent.Toolset{publictools.Ask(), publictools.Todo(), publictools.Tasks(schemaOnlyTaskExecutor{})} {
		add(toolset.PrepareTools(ctx, agent.ToolRequest{}))
	}
	browser, err := newBrowserTool(&schemaBrowserController{})
	if err != nil {
		t.Fatal(err)
	}
	definitions = append(definitions, browser)
	skill, err := newSkillTool(ctx, novaskills.NewBackend(nil), 65536)
	if err != nil {
		t.Fatal(err)
	}
	definitions = append(definitions, skill)
	engine, err := agentscript.NewEngine(agentscript.Config{})
	if err != nil {
		t.Fatal(err)
	}
	script, err := publictools.Script(publictools.ScriptConfig{Engine: engine, MaxResultBytes: 65536})
	if err != nil {
		t.Fatal(err)
	}
	definitions = append(definitions, script)
	infos := make([]*agent.ToolInfo, 0, len(definitions))
	for _, definition := range definitions {
		info, err := definition.Tool.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos
}
