package tools

import (
	"context"
	"testing"

	browserruntime "denova/internal/browser"
)

type schemaBrowserController struct{ actions []string }

func (controller *schemaBrowserController) Open(context.Context, browserruntime.OpenRequest) (browserruntime.Result, error) {
	controller.actions = append(controller.actions, "open")
	return browserruntime.Result{}, nil
}
func (controller *schemaBrowserController) Run(context.Context, browserruntime.RunRequest) (browserruntime.Result, error) {
	controller.actions = append(controller.actions, "run")
	return browserruntime.Result{}, nil
}
func (controller *schemaBrowserController) Close(context.Context, browserruntime.CloseRequest) (browserruntime.Result, error) {
	controller.actions = append(controller.actions, "close")
	return browserruntime.Result{}, nil
}

func TestBrowserExposesParametersAndValidatesBeforeController(t *testing.T) {
	controller := &schemaBrowserController{}
	definition, err := newBrowserTool(controller)
	if err != nil {
		t.Fatal(err)
	}
	info, err := definition.Tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	schema, err := info.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"action", "tab", "url", "command", "selector", "text", "key", "values", "expression", "full_page", "timeout_seconds", "all"} {
		if _, ok := schema.Properties.Get(name); !ok {
			t.Errorf("parameter %s is hidden from provider", name)
		}
	}
	if len(schema.OneOf) != 0 || len(schema.AnyOf) != 0 {
		t.Fatal("browser still requires root union traversal")
	}
	for _, arguments := range []string{
		`{"action":"open"}`,
		`{"action":"run","tab":"main"}`,
		`{"action":"run","command":"observe"}`,
		`{"action":"close"}`,
		`{"action":"close","all":false}`,
		`{"action":"open","tab":"main","command":"observe"}`,
		`{"action":"run","tab":"main","command":"observe","all":true}`,
		`{"action":"close","all":true,"url":"https://example.com"}`,
	} {
		if _, err := definition.Tool.Run(context.Background(), arguments); err == nil {
			t.Errorf("invalid action reached browser: %s", arguments)
		}
	}
	if len(controller.actions) != 0 {
		t.Fatalf("invalid browser calls = %#v", controller.actions)
	}
	for _, arguments := range []string{
		`{"action":"open","tab":"main"}`,
		`{"action":"run","tab":"main","command":"observe"}`,
		`{"action":"close","all":true}`,
	} {
		if _, err := definition.Tool.Run(context.Background(), arguments); err != nil {
			t.Fatal(err)
		}
	}
	if len(controller.actions) != 3 {
		t.Fatalf("valid browser calls = %#v", controller.actions)
	}
}
