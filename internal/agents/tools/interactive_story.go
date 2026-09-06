package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	agent "github.com/alfredxw/denova/agent"

	"denova/internal/interactive"
)

const submitInteractiveTurnToolName = "submit_interactive_turn"
const SubmitInteractiveTurnToolName = submitInteractiveTurnToolName

type searchStoryHistoryInput struct {
	Keywords     []string `json:"keywords,omitempty" jsonschema:"description=Character, location, item, clue, or event keywords to search. Every keyword participates in matching. Omit to browse recent turns."`
	Match        string   `json:"match,omitempty" jsonschema:"description=Keyword matching: any matches at least one keyword; all requires every keyword. Default any."`
	BeforeTurnID string   `json:"before_turn_id,omitempty" jsonschema:"description=Search only current-branch history before this turn_id, preventing the current turn from being treated as an earlier fact."`
	Limit        int      `json:"limit,omitempty" jsonschema:"minimum=1,description=Desired page size, default 8. The shared tool-result byte budget may reduce the actual result count."`
	Cursor       string   `json:"cursor,omitempty" jsonschema:"description=Opaque next_cursor from the previous page; valid only for an identical search."`
}

// interactiveTurnCheckToolInput deliberately omits model-authored
// outcomes.state_changes. Deterministic State Bindings produce rule state
// changes; all remaining state mutations are submitted after the narrative.
type interactiveTurnCheckToolInput struct {
	Action       string                            `json:"action" jsonschema_description:"The action the player actually attempts this turn."`
	Intent       string                            `json:"intent" jsonschema_description:"The goal the player intends to achieve through this action."`
	Challenge    string                            `json:"challenge" jsonschema_description:"The risk, obstacle, or conflict requiring a fixed d20 ruling."`
	Cost         string                            `json:"cost" jsonschema_description:"Potential consequences such as failure, exposure, resource use, or relationship loss."`
	State        string                            `json:"state" jsonschema_description:"Only visible state, resources, position, relationships, or constraints directly relevant to this check."`
	Adjudication interactive.TurnCheckAdjudication `json:"adjudication,omitempty"`
	Rule         interactive.TurnCheckRule         `json:"rule,omitempty"`
	Bonuses      []interactive.TurnCheckBonus      `json:"bonuses,omitempty"`
	Difficulty   string                            `json:"difficulty" jsonschema:"enum=very_easy,enum=easy,enum=normal,enum=hard,enum=very_hard"`
	Outcomes     interactiveTurnCheckToolOutcomes  `json:"outcomes"`
}

type interactiveTurnCheckToolOutcomes struct {
	CriticalSuccess interactiveTurnCheckToolOutcome `json:"critical_success"`
	Success         interactiveTurnCheckToolOutcome `json:"success"`
	Failure         interactiveTurnCheckToolOutcome `json:"failure"`
	CriticalFailure interactiveTurnCheckToolOutcome `json:"critical_failure"`
}

type interactiveTurnCheckToolOutcome struct {
	Result string `json:"result" jsonschema_description:"Final consequence that prose must follow when this tier is selected."`
}

func (input interactiveTurnCheckToolInput) request() interactive.TurnCheckRequest {
	return interactive.TurnCheckRequest{
		Action: input.Action, Intent: input.Intent, Challenge: input.Challenge, Cost: input.Cost, State: input.State,
		Adjudication: input.Adjudication, Rule: input.Rule, Bonuses: input.Bonuses, Difficulty: input.Difficulty,
		Outcomes: interactive.TurnCheckOutcomes{
			CriticalSuccess: interactive.TurnCheckOutcome{Result: input.Outcomes.CriticalSuccess.Result},
			Success:         interactive.TurnCheckOutcome{Result: input.Outcomes.Success.Result},
			Failure:         interactive.TurnCheckOutcome{Result: input.Outcomes.Failure.Result},
			CriticalFailure: interactive.TurnCheckOutcome{Result: input.Outcomes.CriticalFailure.Result},
		},
	}
}

func newInteractiveHistoryTools(ctx InteractiveContext) ([]agent.ToolDefinition, error) {
	ctx.StoryID = strings.TrimSpace(ctx.StoryID)
	ctx.BranchID = strings.TrimSpace(ctx.BranchID)
	if ctx.MaxResultBytes <= 0 {
		ctx.MaxResultBytes = defaultToolResultMaxBytes
	}
	if ctx.Store == nil || ctx.StoryID == "" {
		return nil, nil
	}
	searchTool, err := agent.InferTool("search_story_history", "Search committed historical turns on the current branch. Turn events are the source of historical truth. Results contain only bounded player actions, narrative excerpts, state changes, and exact turn_id values and can be rebuilt from the event log. Use this to continue earlier characters, locations, clues, promises, or causality. Never treat results as current Actor State or future Director planning.", func(callCtx context.Context, input searchStoryHistoryInput) (string, error) {
		_ = callCtx
		result, err := ctx.Store.SearchStoryHistory(ctx.StoryID, ctx.BranchID, interactive.StoryHistorySearchRequest{
			Keywords:     input.Keywords,
			Match:        input.Match,
			BeforeTurnID: input.BeforeTurnID,
			Limit:        input.Limit,
			Cursor:       input.Cursor,
			MaxBytes:     ctx.MaxResultBytes,
		})
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(result)
		return string(data), err
	})
	if err != nil {
		return nil, err
	}
	descriptor := boundedReadDescriptor(ToolSourceHistory, "", agent.ToolResultRecoveryRerun)
	if ctx.MaxResultBytes > 0 {
		descriptor.MaxResultBytes = ctx.MaxResultBytes
	}
	definedSearchTool, err := defineTool(searchTool, descriptor)
	if err != nil {
		return nil, err
	}
	return []agent.ToolDefinition{definedSearchTool}, nil
}

func newInteractiveTurnTools(ctx InteractiveContext) ([]agent.ToolDefinition, error) {
	if ctx.PrepareTurn == nil && ctx.SubmitTurnResult == nil {
		return nil, nil
	}
	tools := make([]agent.ToolDefinition, 0, 2)
	if ctx.PrepareTurn != nil {
		desc := strings.Join([]string{
			"Execute one fixed d20 rule check for this turn. The Interactive Agent provides the action, intent, challenge, cost, relevant current state, pre-roll adjudication, runtime bonus sources and values, difficulty, and critical-success/success/failure/critical-failure consequences. This tool rolls, applies advantage or disadvantage, computes the target, resolves the tier, and returns the selected final consequence.",
			"Protocol: difficulty is very_easy/easy/normal/hard/very_hard; use normal for ordinary difficulty, never medium/moderate. adjudication explains why a check is required, the stakes, the difficulty basis, and the advantage/disadvantage basis. State references use actor_id + field_id in state_refs. rule is optional; when present, template is dice_check and roll_mode is normal/advantage/disadvantage. When a TRPG rule catalog is available, provide its exact template_id; the backend resolves fixed template values and audit metadata.",
			"When context provides a TRPG check configuration, first use trigger, must_check_examples, and skip_check_examples to decide whether to check, then use difficulty_guidance for difficulty/bonuses. The four outcomes describe narrative consequences only and do not include state operations.",
			"When state_bindings are available, choose binding_id and provide actor_id plus target_actor_id when needed. The tool reads Actor State to calculate binding modifiers and outcome_state_changes; do not calculate them again. narrative_state_refs only help write the four outcomes.*.result values before the roll.",
			`Minimal example: {"action":"pick the lock","intent":"enter the warehouse","challenge":"open it before the patrol arrives","cost":"failure reveals the intrusion","state":"The protagonist has simple tools.","adjudication":{"reason":"Time pressure and failure would change the alert state.","stakes":"Failure brings the patrol closer.","difficulty_reason":"The old lock is simple but a patrol is nearby, so use normal difficulty.","roll_mode_reason":"The tools fit but the environment is tense, so roll normally.","state_refs":[{"actor_id":"protagonist","field_id":"stamina"}]},"rule":{"template_id":"dm-osr-player-skill","label":"OSR player-skill priority","failure_policy":"blocked"},"bonuses":[{"kind":"equipment","reason":"Simple lock-picking tools","value":2}],"difficulty":"normal","outcomes":{"critical_success":{"result":"Open it silently and find an extra clue."},"success":{"result":"Open it, but lose time."},"failure":{"result":"The lock stays shut and the patrol draws closer."},"critical_failure":{"result":"The tool breaks and alerts the patrol."}}}`,
		}, "\n")
		prepareTool, err := agent.InferTool("prepare_interactive_turn", desc, func(callCtx context.Context, input interactiveTurnCheckToolInput) (agent.ToolResult, error) {
			resolution, err := ctx.PrepareTurn(callCtx, input.request())
			if err != nil {
				return agent.ToolResult{}, err
			}
			modelData, err := json.MarshalIndent(resolution.ModelToolOutput(), "", "  ")
			if err != nil {
				return agent.ToolResult{}, err
			}
			displayData, err := json.MarshalIndent(resolution.ToolOutput(), "", "  ")
			if err != nil {
				return agent.ToolResult{}, err
			}
			return agent.ToolResult{
				ModelContent:   string(modelData),
				DisplayContent: string(displayData),
				Details:        json.RawMessage(displayData),
				Status:         agent.ToolResultSuccess,
			}, nil
		})
		if err != nil {
			return nil, err
		}
		definedPrepareTool, err := defineTool(prepareTool, interactiveStoryWorkflowDescriptor())
		if err != nil {
			return nil, err
		}
		tools = append(tools, definedPrepareTool)
	}
	if ctx.SubmitTurnResult != nil {
		desc := strings.Join([]string{
			"After outputting all player-visible prose, submit this turn's state_changes and choices through one entry point. Include both in the first call. The current runtime context states whether Game Agent planning is enabled. When ready=false, resubmit only fields named by retry_modules; accepted modules are retained. End immediately when ready=true without repeating or rewriting prose.",
			"When initialize_story_state_schema is available, finalize the schema draft before prose. The first state_changes must fill every field in initialization_guide.required_state_changes together with other major state established by prose. Do not use empty, unset, unknown, or pending placeholders. This tool does not replace schema initialization.",
			fmt.Sprintf("Submit state_changes as a native JSON array, never as a JSON.stringify string. A normal turn should usually stay within %d items; this is not a validation limit, and a complex opening or genuinely larger fact set may exceed it. Never drop important state established by prose merely to reduce the count. Each item uses exactly one operation: replace={op,actor_id,field_id,value,optional subpath}, delta={op,actor_id,field_id,value,optional subpath}, create={op,actor_id,template_id,name,optional role/description/initial_state}, or archive/restore={op,actor_id,reason}. create has no field_id, subpath, or value. Put every initial field for a new Actor in one create.initial_state instead of replacing fields before creation. archive is only for an Actor confirmed dead or permanently gone whose history must remain; restore is only for returning an archived Actor. Both require established factual reasons and must not be inferred automatically from health or narrative wording. Include only fields changed in prose, use exact IDs from the Actor State Handbook, and do not repeat fields consumed by RuleResolution.", recommendedTurnStateChangesPerSubmission),
			"Creating an Actor requires name, and actor_id must exactly equal name in the story language. Do not invent an English, romanized, or slug ID. For an existing Actor, copy actor_id exactly from the state handbook.",
			"Every turn must replace story_context actor_id=story, field_id=当前事件. Also replace 当前详细地点 when it is uninitialized or prose establishes a location change. Do not write empty values to unchanged fields.",
			"choices must match the prose ending and contain exactly the number of distinct suggestions configured for the current story. Submit an empty array only for a terminal turn whose prepare_interactive_turn result has terminal_candidate.",
			"When a module is rejected, repair the same intended state facts. Do not bypass validation by deleting an important character, ability, item, location, or situation already established in prose. You may merge a new Actor's initial_state or compress redundant descriptions.",
			fmt.Sprintf("When planning is enabled, plan_update maintains private future intent as Markdown up to %d bytes. If no plan exists, initialize it with mode=replace_document and follow the injected planning template. For routine changes to an existing modular plan, prefer mode=replace_sections and send only changed section bodies; headings must copy existing unique H2 text exactly. Use replace_document for major replans or any heading, order, or module change. Otherwise omit plan_update while the plan remains useful. Valid sibling section edits are retained when another section is rejected, so retry only retry_sections. When planning is disabled, omit plan_update.", 64*1024),
			"Use the current turn's Actor State Handbook as the authority for the complete parameter template, available IDs, field types, and the number of choices placeholders matching this story's choice_count.",
		}, "\n")
		submitTool, err := newSubmitInteractiveTurnTool(desc, ctx.SubmitTurnResult, ctx.RequestTurnCompletion)
		if err != nil {
			return nil, err
		}
		definedSubmitTool, err := defineTool(submitTool, interactiveStoryWorkflowDescriptor())
		if err != nil {
			return nil, err
		}
		tools = append(tools, definedSubmitTool)
	}
	return tools, nil
}

// NewInteractiveTurn builds the rule-resolution and turn-submission tools for
// one story-scoped Agent run.
func NewInteractiveTurn(ctx InteractiveContext) ([]agent.ToolDefinition, error) {
	return newInteractiveTurnTools(ctx)
}

type submitInteractiveTurnToolSchema struct {
	StateChanges []interactive.TurnStateChangeInput `json:"state_changes,omitempty" jsonschema_description:"Incremental Actor state changes established by this turn's prose. Submit a native JSON array, never a serialized string. Submit an empty array when nothing changed."`
	Choices      []string                           `json:"choices,omitempty" jsonschema_description:"The configured number of distinct next-action suggestions. Use an empty array only when RuleResolution declared terminal_candidate."`
	PlanUpdate   *interactive.TurnPlanUpdateInput   `json:"plan_update,omitempty" jsonschema_description:"Only when Game Agent planning is enabled. Initialize or restructure with replace_document; routinely update existing unique H2 bodies with replace_sections. Omit while the current plan remains useful."`
}

const recommendedTurnStateChangesPerSubmission = 24

type submitInteractiveTurnTool struct {
	info              *agent.ToolInfo
	submit            func(context.Context, interactive.TurnSubmissionInput) (interactive.TurnSubmissionReceipt, error)
	requestCompletion func(context.Context) bool
}

func newSubmitInteractiveTurnTool(
	description string,
	submit func(context.Context, interactive.TurnSubmissionInput) (interactive.TurnSubmissionReceipt, error),
	requestCompletion func(context.Context) bool,
) (agent.Tool, error) {
	info, err := agent.GoStruct2ToolInfo[submitInteractiveTurnToolSchema](submitInteractiveTurnToolName, description)
	if err != nil {
		return nil, err
	}
	parameters, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		return nil, err
	}
	if parameters == nil || parameters.Properties == nil {
		return nil, fmt.Errorf("submit_interactive_turn schema missing root properties")
	}
	stateChanges, ok := parameters.Properties.Get("state_changes")
	if !ok || stateChanges == nil {
		return nil, fmt.Errorf("submit_interactive_turn schema missing state_changes")
	}
	stateChanges.Description = fmt.Sprintf(
		"%s A normal turn should usually stay within %d items; this is not a validation limit, and a complex opening or genuinely larger fact set may exceed it.",
		strings.TrimSpace(stateChanges.Description),
		recommendedTurnStateChangesPerSubmission,
	)
	info.ParamsOneOf = agent.NewParamsOneOfByJSONSchema(parameters)
	return &submitInteractiveTurnTool{info: info, submit: submit, requestCompletion: requestCompletion}, nil
}

func (t *submitInteractiveTurnTool) Info(context.Context) (*agent.ToolInfo, error) {
	return t.info, nil
}

func (t *submitInteractiveTurnTool) Run(ctx context.Context, argumentsInJSON string, _ ...agent.ToolOption) (agent.ToolResult, error) {
	input := interactive.DecodeInteractiveTurnSubmissionInput(argumentsInJSON)
	receipt, err := t.submit(ctx, input)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if receipt.Ready {
		requested := false
		if t.requestCompletion != nil {
			requested = t.requestCompletion(ctx)
		}
		slog.InfoContext(ctx, fmt.Sprintf("[interactive-turn] accepted all result modules completion_requested=%t", requested))
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return agent.ToolResult{}, err
	}
	result := agent.TextToolResult(string(data))
	result.Details = data
	return result, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
