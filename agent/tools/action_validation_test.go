package tools

import (
	"context"
	"testing"

	agent "github.com/alfredxw/denova/agent"
)

type recordingTodoStore struct {
	loads    int
	requests []TodoApplyRequest
}

func (*recordingTodoStore) Identity() agent.CapabilityIdentity {
	return agent.CapabilityIdentity{Kind: "test.todo.schema", Version: 1}
}
func (store *recordingTodoStore) Load(context.Context) ([]TodoItem, uint64, error) {
	store.loads++
	return nil, 0, nil
}
func (store *recordingTodoStore) Apply(_ context.Context, request TodoApplyRequest) (TodoApplyResult, error) {
	store.requests = append(store.requests, request)
	return TodoApplyResult{Revision: request.ExpectedRevision + 1}, nil
}

func TestTodoRejectsMissingAndMixedMutationFieldsBeforeStoreAccess(t *testing.T) {
	store := &recordingTodoStore{}
	definitions, err := Todo(store).PrepareTools(context.Background(), agent.ToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	tool := definitions[0].Tool
	for _, arguments := range []string{
		`{"action":"replace","items":[]}`,
		`{"action":"replace","expected_revision":0}`,
		`{"action":"replace","expected_revision":0,"items":null}`,
		`{"action":"replace","expected_revision":null,"items":[]}`,
		`{"action":"clear"}`,
		`{"action":"update","expected_revision":0,"mutations":[]}`,
		`{"action":"update","expected_revision":0,"mutations":[{"id":"one","text":"One"}],"items":[]}`,
		`{"action":"replace","expected_revision":0,"items":[],"mutations":[]}`,
		`{"action":"clear","expected_revision":0,"items":[]}`,
		`{"action":"read","items":[]}`,
	} {
		if _, err := tool.Run(context.Background(), arguments); err == nil {
			t.Errorf("invalid action reached the store: %s", arguments)
		}
	}
	if len(store.requests) != 0 || store.loads != 0 {
		t.Fatalf("invalid input touched store: %#v", store)
	}
	// Explicit zero and an explicit empty list mean an intentional replacement.
	if _, err := tool.Run(context.Background(), `{"action":"replace","expected_revision":0,"items":[]}`); err != nil {
		t.Fatal(err)
	}
	if len(store.requests) != 1 || store.requests[0].ExpectedRevision != 0 || store.requests[0].Mode != TodoApplyReplace || len(store.requests[0].Items) != 0 {
		t.Fatalf("replacement = %#v", store.requests)
	}
}

type recordingTaskExecutor struct {
	schemaTaskExecutor
	calls int
}

func (executor *recordingTaskExecutor) Start(context.Context, TaskRequest) (Task, error) {
	executor.calls++
	return Task{}, nil
}
func (executor *recordingTaskExecutor) Observe(context.Context, TaskRef, string) (TaskObservation, error) {
	executor.calls++
	return TaskObservation{}, nil
}
func (executor *recordingTaskExecutor) Steer(context.Context, TaskRef, agent.Input) error {
	executor.calls++
	return nil
}
func (executor *recordingTaskExecutor) Abort(context.Context, TaskRef, agent.AbortRequest) error {
	executor.calls++
	return nil
}

func TestTaskRejectsMissingAndMixedActionFieldsBeforeExecution(t *testing.T) {
	executor := &recordingTaskExecutor{}
	tool := taskDefinition(t, executor, "task").Tool
	for _, arguments := range []string{
		`{"action":"start"}`,
		`{"action":"observe","targets":[]}`,
		`{"action":"start","starts":[{"prompt":"inspect"}],"refs":[]}`,
		`{"action":"observe","targets":[{"ref":{"agent":"a","session":"s","run":"r"}}],"input":"extra"}`,
		`{"action":"steer","refs":[{"agent":"a","session":"s","run":"r"}]}`,
		`{"action":"steer","refs":[{"agent":"a","session":"s","run":"r"}],"input":" "}`,
		`{"action":"steer","refs":[{"agent":"a","session":"s","run":"r"}],"input":"continue","reason":"extra"}`,
		`{"action":"abort","refs":[{"agent":"a","session":"s","run":"r"}]}`,
		`{"action":"abort","refs":[{"agent":"a","session":"s","run":"r"}],"reason":" "}`,
	} {
		if _, err := tool.Run(context.Background(), arguments); err == nil {
			t.Errorf("invalid action reached executor: %s", arguments)
		}
	}
	if executor.calls != 0 {
		t.Fatalf("invalid calls executed: %d", executor.calls)
	}
	for _, arguments := range []string{
		`{"action":"steer","refs":[{"agent":"a","session":"s","run":"r"}],"input":"continue"}`,
		`{"action":"abort","refs":[{"agent":"a","session":"s","run":"r"}],"reason":"finished"}`,
	} {
		if _, err := tool.Run(context.Background(), arguments); err != nil {
			t.Fatal(err)
		}
	}
	if executor.calls != 2 {
		t.Fatalf("valid calls executed: %d", executor.calls)
	}
}
