package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agent "github.com/alfredxw/denova/agent"
)

func TestReadMergedSchemaPreservesAdapterRequirementsAndStableOrder(t *testing.T) {
	type plainInput struct {
		Path string `json:"path"`
	}
	type cursorInput struct {
		Path   string `json:"path"`
		Cursor string `json:"cursor" jsonschema:"minLength=1"`
	}
	ctx := context.Background()
	calls := 0
	plain, err := NewReadAdapter(agent.CapabilityIdentity{Kind: "test.read.plain", Version: 1}, "plain", func(_ context.Context, path string) (bool, error) {
		return strings.HasPrefix(path, "plain://"), nil
	}, func(_ context.Context, input plainInput) (ReadResult, error) {
		calls++
		return ReadResult{Path: input.Path, Content: "plain"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := NewReadAdapter(agent.CapabilityIdentity{Kind: "test.read.cursor", Version: 1}, "cursor", func(_ context.Context, path string) (bool, error) {
		return strings.HasPrefix(path, "cursor://"), nil
	}, func(_ context.Context, input cursorInput) (ReadResult, error) {
		calls++
		return ReadResult{Path: input.Path, Content: input.Cursor}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := Read([]ReadAdapter{plain, cursor})
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := Read([]ReadAdapter{cursor, plain})
	if err != nil {
		t.Fatal(err)
	}
	info, err := definition.Tool.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reversedInfo, err := reversed.Tool.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := info.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "path" {
		t.Fatalf("adapter-specific parameter became globally required: %#v", schema.Required)
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	reversedEncoded, err := json.Marshal(reversedInfo)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != string(reversedEncoded) {
		t.Fatal("adapter registration order changed the model-visible schema")
	}
	if _, err := definition.Tool.Run(ctx, `{"path":"plain://item"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := definition.Tool.Run(ctx, `{"path":"cursor://item"}`); err == nil || !strings.Contains(err.Error(), "cursor") {
		t.Fatalf("missing adapter field accepted: %v", err)
	}
	if calls != 1 {
		t.Fatalf("invalid read reached adapter: %d calls", calls)
	}
	if _, err := definition.Tool.Run(ctx, `{"path":"cursor://item","cursor":"next"}`); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("valid read did not reach adapter: %d calls", calls)
	}
}
