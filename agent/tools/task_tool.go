package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	agent "github.com/alfredxw/denova/agent"
)

var (
	ErrTaskCapacityExceeded = errors.New("task capacity exceeded")
	ErrTaskInvalidInput     = errors.New("invalid task input")
)

// DefaultTaskAgentName is the built-in general delegated Agent selected when
// task.start omits an explicit catalog name.
const DefaultTaskAgentName = "general-purpose"

type TaskRef struct {
	Agent   string `json:"agent,omitempty" jsonschema_description:"Delegated Agent name returned by start."`
	Session string `json:"session,omitempty" jsonschema_description:"Child Session ID returned by start."`
	Run     string `json:"run,omitempty" jsonschema_description:"Child Run ID returned by start."`
}

type TaskRequest struct {
	Agent          string `json:"agent,omitempty" jsonschema_description:"Optional stable delegated Agent name from the catalog in this tool description. Omit it to use the built-in general-purpose Agent."`
	Prompt         string `json:"prompt,omitempty" jsonschema_description:"Self-contained goal, constraints, relevant references, expected output, and write scope."`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema_description:"Stable retry identity; omit to derive it from this tool execution."`
}

type Task struct {
	Ref    TaskRef `json:"ref"`
	Status string  `json:"status"`
	Reason string  `json:"reason,omitempty"`
	Output string  `json:"output,omitempty"`
}

type TaskObservation struct {
	Task       Task        `json:"task"`
	Cursor     string      `json:"cursor,omitempty"`
	Output     string      `json:"output,omitempty"`
	Events     []TaskEvent `json:"events,omitempty"`
	Incomplete bool        `json:"incomplete,omitempty"`
}

type TaskObserveTarget struct {
	Ref    TaskRef `json:"ref,omitempty" jsonschema_description:"Task to observe."`
	Cursor string  `json:"cursor,omitempty" jsonschema_description:"Opaque cursor returned by the previous observation of this task."`
}

// TaskWaitOutcome is one executor-owned synchronization snapshot returned
// after any target is ready. Task contains identity and status only; terminal
// payloads arrive through the parent completion mailbox or explicit observe.
// Err is per-target; a top-level Wait error is reserved for an interrupted wait
// or a failed Host interaction.
type TaskWaitOutcome struct {
	Task  *Task
	Ready bool
	Err   error
}

// TaskEvent is the bounded reconnect projection returned by observe. Live
// child events are also forwarded through the parent Agent invocation while
// task_wait is active.
type TaskEvent struct {
	Cursor string      `json:"cursor,omitempty"`
	Type   string      `json:"type"`
	Run    string      `json:"run,omitempty"`
	Text   string      `json:"text,omitempty"`
	Tool   string      `json:"tool,omitempty"`
	Event  agent.Event `json:"event"`
}

type TaskExecutor interface {
	Identity() agent.CapabilityIdentity
	Start(context.Context, TaskRequest) (Task, error)
	Observe(context.Context, TaskRef, string) (TaskObservation, error)
	Wait(context.Context, []TaskRef) ([]TaskWaitOutcome, error)
	Steer(context.Context, TaskRef, agent.Input) error
	Respond(context.Context, TaskRef, string, agent.InteractionResponse) error
	Abort(context.Context, TaskRef, agent.AbortRequest) error
}

type TaskAgentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type taskAgentCatalog interface {
	TaskAgents() []TaskAgentInfo
}

type taskToolInput struct {
	Action  string              `json:"action" jsonschema:"enum=start,enum=observe,enum=steer,enum=abort"`
	Starts  []TaskRequest       `json:"starts,omitempty" jsonschema:"maxItems=32" jsonschema_description:"Required and non-empty for start only. Independent task requests with per-item outcomes."`
	Targets []TaskObserveTarget `json:"targets,omitempty" jsonschema:"maxItems=32" jsonschema_description:"Required and non-empty for observe only. Tasks with their independent observation cursors."`
	Refs    []TaskRef           `json:"refs,omitempty" jsonschema:"maxItems=32" jsonschema_description:"Required and non-empty for steer or abort. Every task returns its own outcome."`
	Input   *string             `json:"input,omitempty" jsonschema:"minLength=1,maxLength=1048576" jsonschema_description:"Required for steer only. Additional instructions for the referenced tasks."`
	Reason  *string             `json:"reason,omitempty" jsonschema:"minLength=1,maxLength=65536" jsonschema_description:"Required for abort only. Non-empty reason recorded with the abort request."`
}

func (input taskToolInput) validate() error {
	switch input.Action {
	case "start":
		if len(input.Starts) == 0 || input.Targets != nil || input.Refs != nil || input.Input != nil || input.Reason != nil {
			return errors.New("task start requires non-empty starts and accepts no other action fields")
		}
	case "observe":
		if len(input.Targets) == 0 || input.Starts != nil || input.Refs != nil || input.Input != nil || input.Reason != nil {
			return errors.New("task observe requires non-empty targets and accepts no other action fields")
		}
	case "steer", "abort":
		if len(input.Refs) == 0 || input.Starts != nil || input.Targets != nil {
			return errors.New("task steer and abort require non-empty refs and do not accept starts or targets")
		}
		if input.Action == "steer" {
			if input.Input == nil || input.Reason != nil {
				return errors.New("task steer requires input and does not accept reason")
			}
			return validateTaskString("input", *input.Input, 1<<20)
		}
		if input.Reason == nil || input.Input != nil {
			return errors.New("task abort requires reason and does not accept input")
		}
		return validateTaskString("reason", *input.Reason, 65536)
	default:
		return fmt.Errorf("unsupported task action %q", input.Action)
	}
	return nil
}

type taskItemResult struct {
	Index       int              `json:"index"`
	Task        *Task            `json:"task,omitempty"`
	Observation *TaskObservation `json:"observation,omitempty"`
	Ready       bool             `json:"ready,omitempty"`
	ErrorCode   string           `json:"error_code,omitempty"`
	Error       string           `json:"error,omitempty"`
}

// Tasks connects the common task tool to a local subagent, remote worker, or
// host task system. Every batch item is attempted independently.
func Tasks(executor TaskExecutor) agent.Toolset {
	return defineToolset(func(context.Context) (agent.Toolset, error) {
		return buildTasks(executor)
	})
}

func buildTasks(executor TaskExecutor) (agent.Toolset, error) {
	if executor == nil {
		return nil, errors.New("tasks Toolset requires a TaskExecutor")
	}
	identity := executor.Identity()
	if strings.TrimSpace(identity.Kind) == "" || identity.Version == 0 {
		return nil, errors.New("tasks TaskExecutor requires a stable Identity")
	}
	description := "Start delegated tasks asynchronously, inspect their current state, add instructions, or abort them. Use this tool only when delegation was explicitly requested. Batch operations return per-item outcomes. task.start defaults to the built-in general-purpose Agent when agent is omitted."
	if catalog, ok := executor.(taskAgentCatalog); ok {
		for _, candidate := range catalog.TaskAgents() {
			description += fmt.Sprintf("\n- %s: %s", candidate.Name, candidate.Description)
		}
	}
	invoke := func(ctx context.Context, input taskToolInput) (agent.ToolResult, error) {
		if err := input.validate(); err != nil {
			return agent.ToolResult{}, err
		}
		results := make([]taskItemResult, 0)
		switch strings.TrimSpace(input.Action) {
		case "start":
			for index, request := range input.Starts {
				item := taskItemResult{Index: index}
				if itemErr := validateTaskRequest(request); itemErr != nil {
					setTaskItemError(&item, itemErr)
					results = append(results, item)
					continue
				}
				if strings.TrimSpace(request.IdempotencyKey) == "" {
					if executionID := agent.CurrentToolExecutionID(ctx); executionID != "" {
						request.IdempotencyKey = fmt.Sprintf("%s:%d", executionID, index)
					}
				}
				task, itemErr := executor.Start(ctx, request)
				if itemErr != nil {
					setTaskItemError(&item, itemErr)
				} else {
					item.Task = &task
				}
				results = append(results, item)
			}
		case "observe":
			for index, target := range input.Targets {
				item := taskItemResult{Index: index}
				if itemErr := validateTaskObserveTarget(target); itemErr != nil {
					setTaskItemError(&item, itemErr)
					results = append(results, item)
					continue
				}
				observation, itemErr := executor.Observe(ctx, target.Ref, target.Cursor)
				if itemErr != nil {
					setTaskItemError(&item, itemErr)
				} else {
					item.Observation = &observation
				}
				results = append(results, item)
			}
		case "steer", "abort":
			for index, ref := range input.Refs {
				item := taskItemResult{Index: index}
				if itemErr := validateTaskRef(ref); itemErr != nil {
					setTaskItemError(&item, itemErr)
					results = append(results, item)
					continue
				}
				commandID := taskActionCommandID(ctx, input.Action, index)
				switch input.Action {
				case "steer":
					steer := agent.Text(*input.Input)
					steer.IdempotencyKey = commandID
					if itemErr := executor.Steer(ctx, ref, steer); itemErr != nil {
						setTaskItemError(&item, itemErr)
					}
				case "abort":
					if itemErr := executor.Abort(ctx, ref, agent.AbortRequest{Reason: *input.Reason, IdempotencyKey: commandID}); itemErr != nil {
						setTaskItemError(&item, itemErr)
					}
				}
				results = append(results, item)
			}
		default:
			return agent.ToolResult{}, fmt.Errorf("unsupported task action %q", input.Action)
		}
		return JSONResult(struct {
			Results []taskItemResult `json:"results"`
		}{Results: results})
	}
	tool, err := agent.InferTool(
		"task", description, invoke,
	)
	if err != nil {
		return nil, err
	}
	descriptor := writeDescriptor()
	descriptor.Source = agent.ToolSourceOther
	descriptor.Capability = "delegation"
	descriptor.Execution = agent.ToolExecutionChild
	descriptor.MutationScope = agent.ToolMutationNone
	descriptor.PostCheck = agent.ToolPostCheckNone
	descriptor.Recovery = agent.ToolRecoveryReconcilable
	descriptor.Presentation = agent.UniformToolPresentation(agent.ToolPresentationDelegation)
	waitDefinition, err := newTaskWaitDefinition(executor)
	if err != nil {
		return nil, err
	}
	return agent.StaticToolsIdentified(
		toolsetIdentity("tools.tasks", identity),
		agent.ToolDefinition{Tool: tool, Descriptor: descriptor},
		waitDefinition,
	)
}

func setTaskItemError(item *taskItemResult, err error) {
	if item == nil || err == nil {
		return
	}
	item.Error = err.Error()
	item.ErrorCode = taskErrorCode(err)
}

func taskErrorCode(err error) string {
	if errors.Is(err, ErrTaskInvalidInput) {
		return "invalid_input"
	}
	if errors.Is(err, ErrTaskCapacityExceeded) {
		return "capacity_exceeded"
	}
	return "task_error"
}

func validateTaskRequest(request TaskRequest) error {
	if len(request.Agent) > 256 {
		return fmt.Errorf("%w: agent exceeds 256 bytes", ErrTaskInvalidInput)
	}
	if err := validateTaskString("prompt", request.Prompt, 1<<20); err != nil {
		return err
	}
	if len(request.IdempotencyKey) > 65536 {
		return fmt.Errorf("%w: idempotency_key exceeds 65536 bytes", ErrTaskInvalidInput)
	}
	return nil
}

func validateTaskObserveTarget(target TaskObserveTarget) error {
	if err := validateTaskRef(target.Ref); err != nil {
		return err
	}
	if len(target.Cursor) > 65536 {
		return fmt.Errorf("%w: cursor exceeds 65536 bytes", ErrTaskInvalidInput)
	}
	return nil
}

func validateTaskRef(ref TaskRef) error {
	if err := validateTaskString("ref.agent", ref.Agent, 256); err != nil {
		return err
	}
	if err := validateTaskString("ref.session", ref.Session, 1024); err != nil {
		return err
	}
	return validateTaskString("ref.run", ref.Run, 1024)
}

func validateTaskString(name, value string, maxBytes int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is required", ErrTaskInvalidInput, name)
	}
	if len(value) > maxBytes {
		return fmt.Errorf("%w: %s exceeds %d bytes", ErrTaskInvalidInput, name, maxBytes)
	}
	return nil
}

func taskActionCommandID(ctx context.Context, action string, index int) string {
	executionID := agent.CurrentToolExecutionID(ctx)
	if executionID == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s:%d", executionID, strings.TrimSpace(action), index)
}
