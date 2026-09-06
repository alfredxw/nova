package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	agent "github.com/alfredxw/denova/agent"
)

type TodoStatus = agent.TodoStatus
type TodoItem = agent.TodoItem

const (
	TodoSchema     = "agent.todo.v1"
	TodoPending    = agent.TodoPending
	TodoInProgress = agent.TodoInProgress
	TodoCompleted  = agent.TodoCompleted
)

type TodoMutation struct {
	ID     string      `json:"id" jsonschema:"minLength=1,maxLength=256" jsonschema_description:"Stable item ID; use an existing ID to update or delete it."`
	Text   *string     `json:"text,omitempty" jsonschema:"minLength=1,maxLength=65536" jsonschema_description:"Complete task text; required when creating a new ID."`
	Status *TodoStatus `json:"status,omitempty" jsonschema:"enum=pending,enum=in_progress,enum=completed" jsonschema_description:"New task status; at most one plan item may be in_progress."`
	Delete bool        `json:"delete,omitempty" jsonschema_description:"Delete the existing ID; omit text and status when true."`
}

type TodoApplyMode string

const (
	TodoApplyUpdate  TodoApplyMode = "update"
	TodoApplyReplace TodoApplyMode = "replace"
	TodoApplyClear   TodoApplyMode = "clear"
)

// TodoApplyRequest is the complete durable mutation contract for both the
// built-in Session store and custom stores. Replace is a complete plan
// replacement (an empty Items slice clears it); Clear is its explicit concise
// form. Update retains per-item partial-success semantics.
type TodoApplyRequest struct {
	ExpectedRevision uint64         `json:"expected_revision"`
	Mode             TodoApplyMode  `json:"mode"`
	Mutations        []TodoMutation `json:"mutations,omitempty"`
	Items            []TodoItem     `json:"items,omitempty"`
}

// TodoStore is the optional durable state adapter. Identity covers storage
// scope and mutation semantics, while revisions provide per-call concurrency.
type TodoStore interface {
	Identity() agent.CapabilityIdentity
	Load(context.Context) ([]TodoItem, uint64, error)
	Apply(context.Context, TodoApplyRequest) (TodoApplyResult, error)
}

type TodoMutationResult struct {
	Index int    `json:"index"`
	ID    string `json:"id,omitempty"`
	Error string `json:"error,omitempty"`
}

type TodoApplyResult struct {
	Schema   string               `json:"schema"`
	Items    []TodoItem           `json:"items"`
	Revision uint64               `json:"revision"`
	Results  []TodoMutationResult `json:"results"`
}

type todoToolInput struct {
	Action           string           `json:"action" jsonschema:"enum=read,enum=update,enum=replace,enum=clear" jsonschema_description:"Read the current plan, update selected items, replace the plan, or clear it."`
	ExpectedRevision *uint64          `json:"expected_revision,omitempty" jsonschema_description:"Required for update, replace, and clear. Copy the revision from the latest read or mutation; zero is a valid explicit revision."`
	Mutations        []TodoMutation   `json:"mutations,omitempty" jsonschema:"maxItems=256" jsonschema_description:"Required and non-empty for update only. Every item returns its own outcome."`
	Items            []todoItemSchema `json:"items,omitempty" jsonschema:"maxItems=256" jsonschema_description:"Required for replace only. An explicit empty array clears the plan; omission does not."`
}

type todoItemSchema struct {
	ID     string     `json:"id" jsonschema:"minLength=1,maxLength=256" jsonschema_description:"Stable task ID."`
	Text   string     `json:"text" jsonschema:"minLength=1,maxLength=65536" jsonschema_description:"Complete task text."`
	Status TodoStatus `json:"status" jsonschema:"enum=pending,enum=in_progress,enum=completed" jsonschema_description:"Task status; at most one item may be in_progress."`
}

func (input todoToolInput) validate() error {
	if input.Action != "read" && input.ExpectedRevision == nil {
		return errors.New("todo " + input.Action + " requires expected_revision from the latest read or mutation")
	}
	switch input.Action {
	case "read":
		if input.ExpectedRevision != nil || input.Mutations != nil || input.Items != nil {
			return errors.New("todo read accepts action only")
		}
	case "update":
		if len(input.Mutations) == 0 || input.Items != nil {
			return errors.New("todo update requires non-empty mutations and does not accept items")
		}
	case "replace":
		if input.Items == nil || input.Mutations != nil {
			return errors.New("todo replace requires items (use [] to clear) and does not accept mutations")
		}
	case "clear":
		if input.Items != nil || input.Mutations != nil {
			return errors.New("todo clear does not accept items or mutations")
		}
	default:
		return errors.New("todo action must be read, update, replace, or clear")
	}
	return nil
}

// Todo exposes revisioned read/update semantics. With no Store it uses the
// current Session's durable Agent state; callers may inject a product Store.
func Todo(stores ...TodoStore) agent.Toolset {
	cloned := append([]TodoStore(nil), stores...)
	return defineToolset(func(context.Context) (agent.Toolset, error) {
		return buildTodo(cloned...)
	})
}

func buildTodo(stores ...TodoStore) (agent.Toolset, error) {
	if len(stores) > 1 {
		return nil, errors.New("todo Toolset accepts at most one TodoStore")
	}
	var store TodoStore
	var storeIdentity agent.CapabilityIdentity
	if len(stores) == 1 {
		store = stores[0]
		if store == nil {
			return nil, errors.New("todo Toolset received a nil TodoStore")
		}
		storeIdentity = store.Identity()
		if err := validateAdapterIdentity("todo Store", storeIdentity); err != nil {
			return nil, err
		}
	}
	invoke := func(ctx context.Context, input todoToolInput) (agent.ToolResult, error) {
		if err := input.validate(); err != nil {
			return agent.ToolResult{}, err
		}
		var result TodoApplyResult
		var err error
		switch input.Action {
		case "read":
			if store == nil {
				result.Items, result.Revision, err = loadSessionTodo(ctx)
			} else {
				result.Items, result.Revision, err = store.Load(ctx)
			}
		case "update", "replace", "clear":
			items := make([]TodoItem, len(input.Items))
			for index, item := range input.Items {
				items[index] = TodoItem{ID: item.ID, Text: item.Text, Status: item.Status}
			}
			request := TodoApplyRequest{
				ExpectedRevision: *input.ExpectedRevision, Mode: TodoApplyMode(input.Action),
				Mutations: input.Mutations, Items: items,
			}
			if store == nil {
				result, err = applySessionTodo(ctx, request)
			} else {
				result, err = store.Apply(ctx, request)
			}
		default:
			return agent.ToolResult{}, errors.New("todo action must be read, update, replace, or clear")
		}
		if err != nil {
			return agent.ToolResult{}, err
		}
		result.Schema = TodoSchema
		return JSONResult(result)
	}
	tool, err := agent.InferTool(
		"todo", "Read or revise the durable task plan. Mutations use optimistic revisions; updates return one outcome per item, and at most one item may be in_progress.", invoke,
	)
	if err != nil {
		return nil, err
	}
	descriptor := writeDescriptor()
	descriptor.Capability = "todo"
	descriptor.MutationScope = agent.ToolMutationSession
	descriptor.Execution = agent.ToolExecutionSessionExclusive
	descriptor.PostCheck = agent.ToolPostCheckSessionState
	descriptor.Recovery = agent.ToolRecoveryIdempotent
	descriptor.Presentation = agent.UniformToolPresentation(agent.ToolPresentationTodo)
	identity := agent.CapabilityIdentity{Kind: "tools.todo.session", Version: 2}
	if store != nil {
		identity = toolsetIdentity("tools.todo.custom", storeIdentity)
	}
	return agent.StaticToolsIdentified(identity, agent.ToolDefinition{
		Tool: tool, Descriptor: descriptor, ImplementationIdentity: identity,
	})
}

func loadSessionTodo(ctx context.Context) ([]TodoItem, uint64, error) {
	var state agent.TodoState
	present, err := agent.LoadSessionState(ctx, agent.TodoCapability, &state)
	if err != nil || !present {
		return nil, 0, err
	}
	return append([]TodoItem(nil), state.Items...), state.Revision, nil
}

func applySessionTodo(ctx context.Context, request TodoApplyRequest) (TodoApplyResult, error) {
	var result TodoApplyResult
	err := agent.UpdateSessionState(ctx, agent.TodoCapability, func(raw json.RawMessage, present bool) (json.RawMessage, bool, error) {
		state := agent.TodoState{}
		if present {
			if err := json.Unmarshal(raw, &state); err != nil {
				return nil, false, fmt.Errorf("decode Todo state: %w", err)
			}
		}
		if state.Revision != request.ExpectedRevision {
			return nil, false, fmt.Errorf("Todo revision conflict: have=%d want=%d", state.Revision, request.ExpectedRevision)
		}
		items, outcomes, applyErr := applyTodoRequest(state.Items, request)
		if applyErr != nil {
			return nil, false, applyErr
		}
		if !hasTodoSuccess(outcomes) {
			result = TodoApplyResult{Schema: TodoSchema, Items: append([]TodoItem(nil), state.Items...), Revision: state.Revision, Results: outcomes}
			if !present {
				return nil, false, nil
			}
			return raw, false, nil
		}
		state.Revision++
		state.Items = items
		result = TodoApplyResult{Schema: TodoSchema, Items: append([]TodoItem(nil), items...), Revision: state.Revision, Results: outcomes}
		encoded, err := json.Marshal(state)
		return encoded, false, err
	})
	return result, err
}

func applyTodoRequest(current []TodoItem, request TodoApplyRequest) ([]TodoItem, []TodoMutationResult, error) {
	switch request.Mode {
	case TodoApplyUpdate:
		items, results := applyTodoMutations(current, request.Mutations)
		return items, results, nil
	case TodoApplyReplace:
		if len(request.Items) == 0 {
			return nil, []TodoMutationResult{{Index: 0}}, nil
		}
		mutations := make([]TodoMutation, len(request.Items))
		for index := range request.Items {
			item := request.Items[index]
			if item.Status == "" {
				item.Status = TodoPending
			}
			text, status := item.Text, item.Status
			mutations[index] = TodoMutation{ID: item.ID, Text: &text, Status: &status}
		}
		items, results := applyTodoMutations(nil, mutations)
		return items, results, nil
	case TodoApplyClear:
		if len(request.Items) != 0 || len(request.Mutations) != 0 {
			return nil, nil, errors.New("todo clear does not accept items or mutations")
		}
		return nil, []TodoMutationResult{{Index: 0}}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported Todo apply mode %q", request.Mode)
	}
}

func applyTodoMutations(current []TodoItem, mutations []TodoMutation) ([]TodoItem, []TodoMutationResult) {
	items := append([]TodoItem(nil), current...)
	seen := make(map[string]bool, len(mutations))
	results := make([]TodoMutationResult, len(mutations))
	valid := make([]bool, len(mutations))
	for index, mutation := range mutations {
		id := strings.TrimSpace(mutation.ID)
		results[index] = TodoMutationResult{Index: index, ID: id}
		if id == "" || len(id) > 256 || seen[id] {
			results[index].Error = "Todo mutation requires a unique ID of at most 256 bytes"
			continue
		}
		seen[id] = true
		_, exists := todoIndex(items, id)
		if mutation.Delete {
			if !exists {
				results[index].Error = "Todo item does not exist"
				continue
			}
			valid[index] = true
			continue
		}
		if mutation.Text == nil && mutation.Status == nil {
			results[index].Error = "Todo mutation requires text, status, or delete"
			continue
		}
		if !exists && mutation.Text == nil {
			results[index].Error = "New Todo item requires text"
			continue
		}
		if mutation.Text != nil {
			text := strings.TrimSpace(*mutation.Text)
			if text == "" || len(text) > 64<<10 {
				results[index].Error = "Todo text must contain 1..65536 bytes"
				continue
			}
		}
		if mutation.Status != nil {
			if *mutation.Status != TodoPending && *mutation.Status != TodoInProgress && *mutation.Status != TodoCompleted {
				results[index].Error = "Todo status is invalid"
				continue
			}
		}
		valid[index] = true
	}
	// Apply deletions and every mutation whose final state is not entering
	// in_progress first. This validates the batch's final intent instead of
	// rejecting the common `[next -> in_progress, current -> completed]` order.
	for index, mutation := range mutations {
		if !valid[index] || mutationEntersInProgress(items, mutation) {
			continue
		}
		items = applyTodoMutation(items, mutation)
	}
	for index, mutation := range mutations {
		if !valid[index] || !mutationEntersInProgress(items, mutation) {
			continue
		}
		id := strings.TrimSpace(mutation.ID)
		if hasOtherInProgress(items, id) {
			results[index].Error = "Todo plan may contain at most one in_progress item"
			continue
		}
		items = applyTodoMutation(items, mutation)
	}
	return items, results
}

func mutationEntersInProgress(items []TodoItem, mutation TodoMutation) bool {
	if mutation.Delete {
		return false
	}
	if mutation.Status != nil {
		return *mutation.Status == TodoInProgress
	}
	index, exists := todoIndex(items, strings.TrimSpace(mutation.ID))
	return exists && items[index].Status == TodoInProgress
}

func applyTodoMutation(items []TodoItem, mutation TodoMutation) []TodoItem {
	id := strings.TrimSpace(mutation.ID)
	index, exists := todoIndex(items, id)
	if mutation.Delete {
		return append(items[:index], items[index+1:]...)
	}
	item := TodoItem{ID: id, Status: TodoPending}
	if exists {
		item = items[index]
	}
	if mutation.Text != nil {
		item.Text = strings.TrimSpace(*mutation.Text)
	}
	if mutation.Status != nil {
		item.Status = *mutation.Status
	}
	if exists {
		items[index] = item
		return items
	}
	return append(items, item)
}

func todoIndex(items []TodoItem, id string) (int, bool) {
	for index := range items {
		if items[index].ID == id {
			return index, true
		}
	}
	return 0, false
}

func hasOtherInProgress(items []TodoItem, id string) bool {
	for _, item := range items {
		if item.ID != id && item.Status == TodoInProgress {
			return true
		}
	}
	return false
}

func hasTodoSuccess(results []TodoMutationResult) bool {
	for _, result := range results {
		if result.Error == "" {
			return true
		}
	}
	return false
}
