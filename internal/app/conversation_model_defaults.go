package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"denova/config"
	"denova/internal/agents/conversationconfig"
	appsettings "denova/internal/app/settings"
)

// ErrConversationModelDefaultsNotSaved distinguishes a failed preference write
// from a rejected conversation update. The conversation may already be saved.
var ErrConversationModelDefaultsNotSaved = errors.New("conversation model defaults were not saved")

// Only explicit model controls update the user-wide new-conversation defaults.
// Restoring, running or creating a conversation must never remember its snapshot.
func (a *App) rememberConversationModel(snapshot conversationconfig.Snapshot, patch conversationconfig.Patch) error {
	if snapshot.AgentKind != config.AgentKindIDE && snapshot.AgentKind != config.AgentKindInteractiveStory {
		return nil
	}
	model := map[string]string{}
	if patch.ProfileID != nil {
		model["profile_id"] = snapshot.ProfileID
	}
	if patch.ThinkingLevel != nil {
		model["thinking_level"] = snapshot.ThinkingLevel
	}
	if len(model) == 0 {
		return nil
	}
	changes, err := json.Marshal(map[string]any{"agent_models": map[string]any{snapshot.AgentKind: model}})
	if err == nil {
		_, err = a.SettingsService().Patch(appsettings.Global(), config.SettingsLayerUser, changes, "")
	}
	if err != nil {
		slog.Error("[conversation-config] remember model selection failed", "agent_kind", snapshot.AgentKind, "changes", model, "error", err)
		return fmt.Errorf("%w: %v", ErrConversationModelDefaultsNotSaved, err)
	}
	slog.Info("[conversation-config] remembered model selection", "agent_kind", snapshot.AgentKind, "changes", model)
	return nil
}
