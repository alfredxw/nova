package interactiveapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"denova/config"
	"denova/internal/agents/conversationconfig"
	"denova/internal/interactive"
)

func RecentConversationSeed(store *interactive.Store, runtimeCfg *config.Config, excludeStoryID string) (conversationconfig.Config, error) {
	if store != nil {
		recent, ok, err := store.RecentRuntimeConfig(excludeStoryID)
		if err != nil {
			return conversationconfig.Config{}, err
		}
		if ok {
			// Keep the existing Agent/permission inheritance, but resolve the
			// model from the user's remembered selection across all Projects.
			defaults := conversationconfig.Default(runtimeCfg, config.AgentKindInteractiveStory)
			recent.ProfileID = defaults.ProfileID
			recent.ThinkingLevel = defaults.ThinkingLevel
			if err := conversationconfig.Validate(runtimeCfg, recent, config.AgentKindInteractiveStory); err == nil {
				return recent, nil
			} else {
				slog.ErrorContext(context.Background(), fmt.Sprintf("[conversation-config] recent Game selection is no longer usable profile_id=%s error=%v; using Settings default", recent.ProfileID, err))
			}
		}
	}
	seed := conversationconfig.Default(runtimeCfg, config.AgentKindInteractiveStory)
	if err := conversationconfig.Validate(runtimeCfg, seed, config.AgentKindInteractiveStory); err != nil {
		return conversationconfig.Config{}, err
	}
	return seed, nil
}

func ApplyConversationConfig(store *interactive.Store, runtimeCfg *config.Config, storyID, branchID string) (conversationconfig.Snapshot, error) {
	if store == nil {
		return conversationconfig.Snapshot{}, errors.New("interactive conversation store is unavailable")
	}
	snapshot, ok, err := store.BranchRuntimeConfig(storyID, branchID)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	if !ok {
		seed := conversationconfig.Default(runtimeCfg, config.AgentKindInteractiveStory)
		if err := conversationconfig.Validate(runtimeCfg, seed, config.AgentKindInteractiveStory); err != nil {
			return conversationconfig.Snapshot{}, err
		}
		snapshot, err = store.EnsureBranchRuntimeConfig(storyID, branchID, seed)
		if err != nil {
			return conversationconfig.Snapshot{}, err
		}
	}
	if err := conversationconfig.Apply(runtimeCfg, snapshot.Config); err != nil {
		return conversationconfig.Snapshot{}, fmt.Errorf("apply interactive conversation runtime config: %w", err)
	}
	return snapshot, nil
}
