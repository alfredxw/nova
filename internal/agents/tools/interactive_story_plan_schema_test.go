package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"denova/internal/interactive"
	agent "github.com/alfredxw/denova/agent"
)

func TestSubmitInteractiveTurnExposesStateAndPlanFieldsDirectly(t *testing.T) {
	tool, err := newSubmitInteractiveTurnTool("test", func(context.Context, interactive.TurnSubmissionInput) (interactive.TurnSubmissionReceipt, error) {
		return interactive.TurnSubmissionReceipt{}, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	schema, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	planUpdate, ok := schema.Properties.Get("plan_update")
	if !ok || planUpdate == nil || len(planUpdate.OneOf) != 0 || planUpdate.Properties.Len() != 3 {
		t.Fatalf("plan_update schema should directly expose its fields: %#v", planUpdate)
	}
	changes, ok := schema.Properties.Get("state_changes")
	if !ok || changes.Items == nil || len(changes.Items.OneOf) != 0 || changes.Items.Properties.Len() != 11 {
		t.Fatalf("state_changes items must directly expose operation fields: %#v", changes)
	}
	// Flattening operation variants must preserve their guidance in the schema
	// the model receives, including rules that the host cannot infer from prose.
	for _, tc := range []struct {
		path     string
		guidance []string
	}{
		{"state_changes.op", []string{"complete historical state", "archived Actor"}},
		{"state_changes.field_id", []string{"replace/delta", "existing number"}},
		{"state_changes.subpath", []string{"replace/delta", "existing nested number", "one string segment per level"}},
		{"state_changes.value", []string{"Required only for replace/delta", "at the end of this turn", "finite amount to add to or subtract from", "native JSON values"}},
		{"state_changes.initial_state", []string{"every reliable initial field value", "exact Field IDs", "native JSON values"}},
		{"state_changes.reason", []string{"Non-empty", "prose", "archive", "death or permanent departure", "restore", "return"}},
		{"plan_update.markdown", []string{"replace_document", "injected planning template", "replaces the entire current document"}},
		{"plan_update.sections", []string{"replace_sections", "changed existing unique H2", "accepted or rejected independently", "retry only failed headings"}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			field := schema
			for _, name := range strings.Split(tc.path, ".") {
				if field.Type == "array" {
					field = field.Items
				}
				next, exists := field.Properties.Get(name)
				if !exists || next == nil {
					t.Fatalf("model-visible schema is missing %s", tc.path)
				}
				field = next
			}
			for _, guidance := range tc.guidance {
				if !strings.Contains(field.Description, guidance) {
					t.Errorf("model-visible description for %s lost %q: %s", tc.path, guidance, field.Description)
				}
			}
		})
	}
}

func TestSubmitInteractiveTurnKeepsConditionalValidationAndNativeValues(t *testing.T) {
	var received interactive.TurnSubmissionInput
	tool, err := newSubmitInteractiveTurnTool("test", func(_ context.Context, input interactive.TurnSubmissionInput) (interactive.TurnSubmissionReceipt, error) {
		received = input
		return interactive.TurnSubmissionReceipt{}, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(arguments string) {
		t.Helper()
		normalized, err := agent.NormalizeToolArguments(info, arguments)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tool.Run(context.Background(), normalized); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{`0`, `false`, `{"nested":[1,true]}`, `[1,false]`} {
		invoke(`{"state_changes":[{"op":"replace","actor_id":"hero","field_id":"field","value":` + value + `}]}`)
		if received.StateUpdates == nil || len(*received.StateUpdates) != 1 || len(received.Diagnostics) != 0 {
			t.Fatalf("valid native value rejected: %#v", received)
		}
		encoded, err := json.Marshal((*received.StateUpdates)[0].Value)
		if err != nil || string(encoded) != value {
			t.Fatalf("native value changed: %s -> %s (%v)", value, encoded, err)
		}
	}
	// An invalid state change rejects its whole module, including valid siblings,
	// while the unrelated choices module remains available for acceptance.
	invoke(`{"state_changes":[{"op":"delta","actor_id":"hero","field_id":"hp","value":0},{"op":"replace","actor_id":"hero","field_id":"hp"}],"choices":["Continue"]}`)
	if received.StateUpdates != nil || received.Choices == nil || len(received.Diagnostics) != 1 || !strings.Contains(received.Diagnostics[0].Message, "value") {
		t.Fatalf("module isolation or actionable feedback lost: %#v", received)
	}
	invoke(`{"state_changes":[{"op":"create","actor_id":"hero","template_id":"character","name":"hero","field_id":"hp"}],"choices":["Continue"]}`)
	if received.StateUpdates != nil || received.Choices == nil || len(received.Diagnostics) != 1 || !strings.Contains(received.Diagnostics[0].Message, "field_id") {
		t.Fatalf("mixed action fields accepted: %#v", received)
	}
	invoke(`{"plan_update":{"mode":"replace_document","markdown":"# Plan","sections":[{"heading":"Next","markdown":"Continue"}]},"choices":["Continue"]}`)
	if received.PlanUpdate != nil || received.Choices == nil || len(received.Diagnostics) != 1 || received.Diagnostics[0].Module != interactive.TurnSubmissionModulePlanUpdate {
		t.Fatalf("mixed plan fields accepted: %#v", received)
	}
}
