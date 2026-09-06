package interactive

import (
	"context"
	interactivestate "denova/internal/interactive/state"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

const turnSubmissionStateChangesField = "state_changes"

// TurnStateChangeInput is the model-facing state mutation shape. Stable IDs
// are separate fields so the model never has to construct or escape a JSON
// Pointer; the backend compiles this shape to the persisted interactivestate.Update.
type TurnStateChangeInput struct {
	Op           string         `json:"op" jsonschema:"enum=replace,enum=delta,enum=create,enum=archive,enum=restore" jsonschema_description:"replace writes a field's complete new value; delta changes an existing number; create adds an Actor; archive removes an Actor from runtime participation while preserving complete historical state; restore returns an archived Actor to active runtime participation."`
	ActorID      string         `json:"actor_id" jsonschema_description:"For an existing Actor, copy the ID from the state handbook exactly. For create, use the character name in the story's language and make it identical to name."`
	FieldID      string         `json:"field_id,omitempty" jsonschema_description:"Required only for replace/delta. Copy an exact Field ID from the target Actor template in the Actor State Handbook. For delta, the field or selected subpath must be an existing number."`
	Subpath      []string       `json:"subpath,omitempty" jsonschema:"maxItems=16" jsonschema_description:"Optional only for replace/delta nested updates inside object fields. For delta, target an existing nested number. Supply one string segment per level; do not construct a path string."`
	Value        any            `json:"value,omitempty" jsonschema_description:"Required only for replace/delta. For replace, provide the complete non-empty field value at the end of this turn. For delta, provide a finite amount to add to or subtract from an existing number. Match the target field type; number, bool, object, and list fields require native JSON values, not quoted strings. Zero and false are valid values."`
	TemplateID   string         `json:"template_id,omitempty" jsonschema_description:"Required only for create. Copy a Template ID from Templates Available to New Actors exactly."`
	Name         string         `json:"name,omitempty" jsonschema_description:"Required only for create. Use the character name in the story's language and make it identical to actor_id."`
	Role         string         `json:"role,omitempty" jsonschema_description:"Optional new Actor's role in the current story, used only for create."`
	Description  string         `json:"description,omitempty" jsonschema_description:"Brief Actor description used only for create."`
	InitialState map[string]any `json:"initial_state,omitempty" jsonschema:"maxProperties=64" jsonschema_description:"Used only for create. Include every reliable initial field value; keys must be exact Field IDs from the selected template. Number, bool, object, and list fields require native JSON values, not quoted strings."`
	Reason       string         `json:"reason,omitempty" jsonschema_description:"Non-empty reason required only for archive/restore. For archive, cite facts established by prose that confirm death or permanent departure. For restore, cite facts established by prose that confirm the archived Actor's return."`
}

// DecodeInteractiveTurnSubmissionInput independently decodes state_changes,
// choices, and plan_update from one model-facing tool call. A malformed module
// does not discard valid siblings; valid plan sections also survive malformed
// siblings so later calls can provide only retry_modules and retry_sections.
func DecodeInteractiveTurnSubmissionInput(arguments string) TurnSubmissionInput {
	if len([]byte(arguments)) > maxTurnSubmissionArgumentsBytes {
		return invalidUnifiedTurnSubmissionInput("submission_too_large", "", fmt.Sprintf("%d bytes", len([]byte(arguments))), fmt.Sprintf("Tool arguments exceed %d bytes.", maxTurnSubmissionArgumentsBytes))
	}
	var root map[string]json.RawMessage
	if err := decodeStrictJSON([]byte(arguments), &root, false); err != nil {
		return invalidUnifiedTurnSubmissionInput(TurnSubmissionDiagnosticInvalidJSON, "", "invalid JSON", fmt.Sprintf("Turn submission arguments are not valid JSON: %v", err))
	}
	if root == nil {
		return invalidUnifiedTurnSubmissionInput(TurnSubmissionDiagnosticInvalidTopLevel, "", "null", "Turn submission arguments must be an object.")
	}
	allowed := map[string]bool{
		turnSubmissionStateChangesField: true,
		TurnSubmissionModuleChoices:     true,
		TurnSubmissionModulePlanUpdate:  true,
	}
	unknown := make([]string, 0)
	for key := range root {
		if !allowed[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return invalidUnifiedTurnSubmissionInput(
			TurnSubmissionDiagnosticInvalidTopLevel,
			"",
			strings.Join(unknown, ","),
			"Turn submission arguments may only contain state_changes, choices, and optional plan_update.",
		)
	}

	input := TurnSubmissionInput{}
	if raw, exists := root[turnSubmissionStateChangesField]; exists {
		updates, diagnostics := decodeStructuredStateChangesModule(raw)
		input.Diagnostics = append(input.Diagnostics, diagnostics...)
		if len(diagnostics) == 0 {
			input.StateUpdates = &updates
		}
	}
	if raw, exists := root[TurnSubmissionModuleChoices]; exists {
		choices, diagnostics := decodeChoicesModule(raw)
		input.Diagnostics = append(input.Diagnostics, diagnostics...)
		if !turnSubmissionHasDiagnostic(input.Diagnostics, TurnSubmissionModuleChoices) {
			input.Choices = &choices
		}
	}
	if raw, exists := root[TurnSubmissionModulePlanUpdate]; exists {
		plan, diagnostics := decodeTurnPlanUpdate(raw)
		input.Diagnostics = append(input.Diagnostics, diagnostics...)
		input.PlanUpdate = plan
	}
	return input
}

func decodeTurnPlanUpdate(raw json.RawMessage) (*TurnPlanUpdateInput, []TurnSubmissionDiagnostic) {
	var root map[string]json.RawMessage
	if err := decodeStrictJSON(raw, &root, false); err != nil || root == nil {
		return nil, []TurnSubmissionDiagnostic{*newTurnSubmissionDiagnostic(
			TurnSubmissionModulePlanUpdate, nil, TurnSubmissionDiagnosticInvalidModule,
			"/plan_update", "plan update object", jsonValueKind(raw), "plan_update must be an object with mode and matching content.",
		)}
	}
	allowed := map[string]bool{"mode": true, "markdown": true, "sections": true}
	unknown := make([]string, 0)
	for key := range root {
		if !allowed[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, []TurnSubmissionDiagnostic{*newTurnSubmissionDiagnostic(
			TurnSubmissionModulePlanUpdate, nil, TurnSubmissionDiagnosticInvalidModule,
			"/plan_update", "only mode, markdown, and sections", strings.Join(unknown, ","), "plan_update contains unsupported fields.",
		)}
	}

	var mode string
	modeRaw, hasMode := root["mode"]
	if !hasMode || decodeStrictJSON(modeRaw, &mode, false) != nil {
		return nil, []TurnSubmissionDiagnostic{*newTurnSubmissionDiagnostic(
			TurnSubmissionModulePlanUpdate, nil, TurnSubmissionDiagnosticInvalidPlanMode,
			"/plan_update/mode", "replace_document or replace_sections", jsonValueKind(modeRaw), "plan_update.mode is required and must be a string.",
		)}
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	update := &TurnPlanUpdateInput{Mode: mode}
	switch mode {
	case TurnPlanUpdateModeReplaceDocument:
		if _, hasSections := root["sections"]; hasSections {
			return nil, []TurnSubmissionDiagnostic{*newTurnSubmissionDiagnostic(
				TurnSubmissionModulePlanUpdate, nil, TurnSubmissionDiagnosticInvalidModule,
				"/plan_update/sections", "omitted for replace_document", "present", "replace_document accepts markdown only.",
			)}
		}
		markdownRaw, exists := root["markdown"]
		if !exists || decodeStrictJSON(markdownRaw, &update.Markdown, false) != nil {
			return nil, []TurnSubmissionDiagnostic{*newTurnSubmissionDiagnostic(
				TurnSubmissionModulePlanUpdate, nil, TurnSubmissionDiagnosticInvalidModule,
				"/plan_update/markdown", "complete Markdown string", jsonValueKind(markdownRaw), "replace_document requires a Markdown string.",
			)}
		}
		return update, nil

	case TurnPlanUpdateModeReplaceSections:
		if _, hasMarkdown := root["markdown"]; hasMarkdown {
			return nil, []TurnSubmissionDiagnostic{*newTurnSubmissionDiagnostic(
				TurnSubmissionModulePlanUpdate, nil, TurnSubmissionDiagnosticInvalidModule,
				"/plan_update/markdown", "omitted for replace_sections", "present", "replace_sections accepts sections only.",
			)}
		}
		sectionsRaw, exists := root["sections"]
		if !exists {
			return update, []TurnSubmissionDiagnostic{*newTurnSubmissionDiagnostic(
				TurnSubmissionModulePlanUpdate, nil, TurnSubmissionDiagnosticInvalidModule,
				"/plan_update/sections", "non-empty array", "missing", "replace_sections requires at least one section update.",
			)}
		}
		var items []json.RawMessage
		if err := decodeStrictJSON(sectionsRaw, &items, false); err != nil || items == nil {
			return update, []TurnSubmissionDiagnostic{*newTurnSubmissionDiagnostic(
				TurnSubmissionModulePlanUpdate, nil, TurnSubmissionDiagnosticInvalidModule,
				"/plan_update/sections", "non-empty array", jsonValueKind(sectionsRaw), "replace_sections.sections must be a native array.",
			)}
		}
		if len(items) == 0 {
			return update, []TurnSubmissionDiagnostic{*newTurnSubmissionDiagnostic(
				TurnSubmissionModulePlanUpdate, nil, TurnSubmissionDiagnosticInvalidModule,
				"/plan_update/sections", "non-empty array", "empty array", "replace_sections requires at least one section update.",
			)}
		}
		diagnostics := make([]TurnSubmissionDiagnostic, 0)
		for index, item := range items {
			section, diagnostic := decodeTurnPlanSectionUpdate(item, index)
			if diagnostic != nil {
				diagnostics = append(diagnostics, *diagnostic)
				continue
			}
			update.Sections = append(update.Sections, section)
		}
		return update, diagnostics

	default:
		return update, []TurnSubmissionDiagnostic{*newTurnSubmissionDiagnostic(
			TurnSubmissionModulePlanUpdate, nil, TurnSubmissionDiagnosticInvalidPlanMode,
			"/plan_update/mode", "replace_document or replace_sections", mode, "plan_update.mode is not supported.",
		)}
	}
}

func decodeTurnPlanSectionUpdate(raw json.RawMessage, index int) (TurnPlanSectionUpdate, *TurnSubmissionDiagnostic) {
	var root map[string]json.RawMessage
	path := fmt.Sprintf("/plan_update/sections/%d", index)
	if err := decodeStrictJSON(raw, &root, false); err != nil || root == nil {
		return TurnPlanSectionUpdate{}, newTurnSubmissionDiagnostic(
			TurnSubmissionModulePlanUpdate, intPointer(index), TurnSubmissionDiagnosticInvalidModule,
			path, "object with heading and markdown", jsonValueKind(raw), "Each plan section update must be an object.",
		)
	}
	if len(root) != 2 || root["heading"] == nil || root["markdown"] == nil {
		return TurnPlanSectionUpdate{}, newTurnSubmissionDiagnostic(
			TurnSubmissionModulePlanUpdate, intPointer(index), TurnSubmissionDiagnosticInvalidModule,
			path, "exactly heading and markdown", "missing or unsupported fields", "Each plan section update accepts exactly heading and markdown.",
		)
	}
	var section TurnPlanSectionUpdate
	if err := decodeStrictJSON(root["heading"], &section.Heading, false); err != nil {
		return TurnPlanSectionUpdate{}, newTurnSubmissionDiagnostic(
			TurnSubmissionModulePlanUpdate, intPointer(index), TurnSubmissionDiagnosticInvalidModule,
			path+"/heading", "string", jsonValueKind(root["heading"]), "Plan section heading must be a string.",
		)
	}
	if err := decodeStrictJSON(root["markdown"], &section.Markdown, false); err != nil {
		return TurnPlanSectionUpdate{}, newTurnSubmissionDiagnostic(
			TurnSubmissionModulePlanUpdate, intPointer(index), TurnSubmissionDiagnosticInvalidModule,
			path+"/markdown", "string", jsonValueKind(root["markdown"]), "Plan section markdown must be a string.",
		)
	}
	section.sourceIndex = intPointer(index)
	return section, nil
}

func decodeStructuredStateChangesModule(raw json.RawMessage) ([]interactivestate.Update, []TurnSubmissionDiagnostic) {
	items, err := decodeStructuredStateChangeItems(raw)
	if err != nil {
		return nil, []TurnSubmissionDiagnostic{*newTurnSubmissionDiagnostic(
			TurnSubmissionModuleStateChanges,
			nil,
			TurnSubmissionDiagnosticInvalidModule,
			"/state_changes",
			"array",
			jsonValueKind(raw),
			fmt.Sprintf("state_changes must be a native array; only one string layer containing valid array JSON is tolerated: %v", err),
		)}
	}
	updates := make([]interactivestate.Update, 0, len(items))
	diagnostics := make([]TurnSubmissionDiagnostic, 0)
	for index, item := range items {
		var change TurnStateChangeInput
		if err := decodeStrictJSON(item, &change, true); err != nil {
			diagnostics = append(diagnostics, *newTurnSubmissionDiagnostic(
				TurnSubmissionModuleStateChanges,
				intPointer(index),
				TurnSubmissionDiagnosticInvalidModule,
				fmt.Sprintf("/state_changes/%d", index),
				"structured state change",
				jsonValueKind(item),
				fmt.Sprintf("The state change shape is invalid: %v", err),
			))
			continue
		}
		update, err := stateUpdateFromStructuredInput(change)
		if err != nil {
			diagnostics = append(diagnostics, *newTurnSubmissionDiagnostic(
				TurnSubmissionModuleStateChanges,
				intPointer(index),
				TurnSubmissionDiagnosticInvalidModule,
				fmt.Sprintf("/state_changes/%d", index),
				"valid replace, delta, create, archive, or restore fields",
				"invalid state change",
				err.Error(),
			))
			continue
		}
		updates = append(updates, update)
	}
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}
	return updates, nil
}

// decodeStructuredStateChangeItems keeps the model-facing contract strict while
// tolerating the one legacy shape observed in real runs: an otherwise valid
// array JSON value encoded once as a string. It intentionally does not recurse,
// repair malformed pseudo-JSON, or accept null so invalid facts still trigger a
// targeted state_changes retry.
func decodeStructuredStateChangeItems(raw json.RawMessage) ([]json.RawMessage, error) {
	var items []json.RawMessage
	directErr := decodeStrictJSON(raw, &items, false)
	if directErr == nil && items != nil {
		return items, nil
	}
	if jsonValueKind(raw) != "string" {
		if directErr != nil {
			return nil, directErr
		}
		return nil, errors.New("state_changes cannot be null")
	}

	var encoded string
	if err := decodeStrictJSON(raw, &encoded, false); err != nil {
		return nil, err
	}
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, errors.New("state_changes string cannot be empty")
	}
	items = nil
	if err := decodeStrictJSON([]byte(encoded), &items, false); err != nil {
		return nil, fmt.Errorf("state_changes string does not contain valid array JSON: %w", err)
	}
	if items == nil {
		return nil, errors.New("state_changes string cannot contain null")
	}
	slog.InfoContext(context.Background(), fmt.Sprintf("[interactive-turn-submission] accepted one-layer string-encoded state_changes bytes=%d location=internal/interactive/turn_submission_decode.go", len(encoded)))
	return items, nil
}

func stateUpdateFromStructuredInput(change TurnStateChangeInput) (interactivestate.Update, error) {
	change.Op = strings.ToLower(strings.TrimSpace(change.Op))
	change.ActorID = strings.TrimSpace(change.ActorID)
	change.FieldID = strings.TrimSpace(change.FieldID)
	change.TemplateID = strings.TrimSpace(change.TemplateID)
	if change.ActorID == "" {
		return interactivestate.Update{}, fmt.Errorf("state_changes requires actor_id")
	}
	switch change.Op {
	case interactivestate.Replace, interactivestate.Delta:
		if change.FieldID == "" {
			return interactivestate.Update{}, fmt.Errorf("%s requires field_id", change.Op)
		}
		if change.Value == nil {
			return interactivestate.Update{}, fmt.Errorf("%s requires a non-null value", change.Op)
		}
		if change.TemplateID != "" || change.Name != "" || change.Role != "" || change.Description != "" || change.InitialState != nil || strings.TrimSpace(change.Reason) != "" {
			return interactivestate.Update{}, fmt.Errorf("%s does not accept template_id, name, role, description, initial_state, or reason", change.Op)
		}
		segments := []string{change.ActorID, change.FieldID}
		for _, segment := range change.Subpath {
			segment = strings.TrimSpace(segment)
			if segment == "" {
				return interactivestate.Update{}, fmt.Errorf("subpath must not contain empty segments")
			}
			segments = append(segments, segment)
		}
		return interactivestate.Update{Op: change.Op, Path: interactivestate.FormatPath(segments), Value: change.Value}, nil
	case interactivestate.Create:
		if change.TemplateID == "" {
			return interactivestate.Update{}, fmt.Errorf("create requires template_id")
		}
		if strings.TrimSpace(change.Name) == "" {
			return interactivestate.Update{}, fmt.Errorf("create requires name matching actor_id")
		}
		if change.FieldID != "" || len(change.Subpath) > 0 || change.Value != nil || strings.TrimSpace(change.Reason) != "" {
			return interactivestate.Update{}, fmt.Errorf("create does not accept field_id, subpath, value, or reason")
		}
		value := map[string]any{"template_id": change.TemplateID}
		if name := strings.TrimSpace(change.Name); name != "" {
			value["name"] = name
		}
		if role := strings.TrimSpace(change.Role); role != "" {
			value["role"] = role
		}
		if description := strings.TrimSpace(change.Description); description != "" {
			value["description"] = description
		}
		if change.InitialState != nil {
			value["state"] = change.InitialState
		}
		return interactivestate.Update{Op: interactivestate.Create, Path: interactivestate.FormatPath([]string{change.ActorID}), Value: value}, nil
	case interactivestate.Archive, interactivestate.Restore:
		reason := strings.TrimSpace(change.Reason)
		if reason == "" {
			return interactivestate.Update{}, fmt.Errorf("%s requires a non-empty reason", change.Op)
		}
		if len([]byte(reason)) > maxActorArchiveReasonBytes {
			return interactivestate.Update{}, fmt.Errorf("%s reason exceeds %d bytes", change.Op, maxActorArchiveReasonBytes)
		}
		if change.FieldID != "" || len(change.Subpath) > 0 || change.Value != nil || change.TemplateID != "" || change.Name != "" || change.Role != "" || change.Description != "" || change.InitialState != nil {
			return interactivestate.Update{}, fmt.Errorf("%s accepts only op, actor_id, and reason", change.Op)
		}
		return interactivestate.Update{Op: change.Op, Path: interactivestate.FormatPath([]string{change.ActorID}), Value: map[string]any{"reason": reason}}, nil
	default:
		return interactivestate.Update{}, fmt.Errorf("op must be replace, delta, create, archive, or restore")
	}
}

func invalidUnifiedTurnSubmissionInput(code, path, actual, message string) TurnSubmissionInput {
	diagnostics := make([]TurnSubmissionDiagnostic, 0, 2)
	for _, module := range []string{TurnSubmissionModuleStateChanges, TurnSubmissionModuleChoices, TurnSubmissionModulePlanUpdate} {
		diagnostics = append(diagnostics, *newTurnSubmissionDiagnostic(
			module,
			nil,
			code,
			path,
			"object containing state_changes and/or choices",
			actual,
			message,
		))
	}
	return TurnSubmissionInput{Diagnostics: diagnostics}
}

func turnSubmissionHasDiagnostic(diagnostics []TurnSubmissionDiagnostic, module string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Module == module {
			return true
		}
	}
	return false
}
