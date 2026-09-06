package app

import (
	"context"
	"errors"
	"testing"

	"denova/config"
	agentconversation "denova/internal/agents/conversation"
	"denova/internal/agents/session"
	appsettings "denova/internal/app/settings"
	"denova/internal/interactive"
)

func TestManualModelSelectionPersistsAcrossRestartAndProjects(t *testing.T) {
	application := newExecutionProfileTestApp(t)
	root := application.cfg.DataDir()
	workspace := application.workspace
	projectID := application.ProjectID()
	ctx := context.Background()
	if _, err := application.SettingsService().Patch(appsettings.Global(), config.SettingsLayerUser, []byte(`{
		"model_profiles": [{"id":"alternate","model":"alternate-model"}],
		"agent_models": {"ide":{"thinking_level":"medium"},"interactive_story":{"thinking_level":"low"}}
	}`), ""); err != nil {
		t.Fatal(err)
	}
	writing := ConversationConfigBinding{Mode: ConversationModeWriting, ProjectID: projectID, SessionID: application.session.ID}
	initial, err := application.ConversationConfig(ctx, writing)
	if err != nil {
		t.Fatal(err)
	}
	profile, thinking := "alternate", "high"
	saved, err := application.PatchConversationConfig(ctx, writing, ConversationConfigPatch{ProfileID: &profile, ThinkingLevel: &thinking}, initial.Revision)
	if err != nil {
		t.Fatal(err)
	}
	story, err := application.CreateInteractiveStory(interactive.CreateStoryRequest{Title: "Old game", StoryTellerID: "classic"})
	if err != nil {
		t.Fatal(err)
	}
	game := ConversationConfigBinding{Mode: ConversationModeInteractive, ProjectID: projectID, StoryID: story.ID, BranchID: "main"}
	gameInitial, err := application.ConversationConfig(ctx, game)
	if err != nil {
		t.Fatal(err)
	}
	gameThinking := "max"
	if _, err := application.PatchConversationConfig(ctx, game, ConversationConfigPatch{ThinkingLevel: &gameThinking}, gameInitial.Revision); err != nil {
		t.Fatal(err)
	}
	// Revisiting and changing permissions in an older conversation must not
	// replace either remembered model preference.
	approval := config.AgentApprovalFullAccess
	if _, err := application.PatchConversationConfig(ctx, writing, ConversationConfigPatch{ApprovalMode: &approval}, saved.Revision); err != nil {
		t.Fatal(err)
	}
	application.Close()
	restartConfig, _, err := config.LoadWithProject(root, workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(ctx, restartConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	layered, err := restarted.SettingsService().Snapshot(appsettings.Global())
	if err != nil {
		t.Fatal(err)
	}
	if got := layered.User.AgentModels.IDE; got.ProfileID != profile || got.ThinkingLevel != thinking {
		t.Fatalf("remembered writing model after restart = %#v", got)
	}
	if got := layered.User.AgentModels.InteractiveStory; got.ProfileID != "" || got.ThinkingLevel != gameThinking {
		t.Fatalf("remembered game model after restart = %#v", got)
	}
	// A fresh Project store has no recent conversation to infer a model from.
	otherStore, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = otherStore.Close() })
	_, fresh, err := agentconversation.GetOrCreateSession(otherStore, "new-project", restarted.cfg, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ProfileID != profile || fresh.ThinkingLevel != thinking {
		t.Fatalf("new Project did not inherit remembered writing model: %#v", fresh)
	}
	oldGame, err := restarted.ConversationConfig(ctx, game)
	if err != nil {
		t.Fatal(err)
	}
	if oldGame.ProfileID != gameInitial.ProfileID || oldGame.ThinkingLevel != gameThinking {
		t.Fatalf("existing game changed after restart: %#v", oldGame)
	}
}

func TestGameDraftModelSelectionPersistsWithoutCreatingStory(t *testing.T) {
	application := newExecutionProfileTestApp(t)
	binding := ConversationConfigBinding{Mode: ConversationModeInteractive, ProjectID: application.ProjectID()}
	thinking := "high"
	if _, err := application.PatchConversationConfig(context.Background(), binding, ConversationConfigPatch{ThinkingLevel: &thinking}, 0); err != nil {
		t.Fatal(err)
	}
	preview, err := application.ConversationConfig(context.Background(), binding)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Revision != 0 || preview.ThinkingLevel != thinking {
		t.Fatalf("draft did not read saved default: %#v", preview)
	}
	stories, err := application.InteractiveStories()
	if err != nil {
		t.Fatal(err)
	}
	if len(stories.Stories) != 0 {
		t.Fatalf("selecting a draft model created a story: %#v", stories)
	}
}

func TestWritingDraftRemembersSelectionAcrossBookProjects(t *testing.T) {
	application := newExecutionProfileTestApp(t)
	ctx := context.Background()
	firstBook, err := application.CreateBook(ctx, t.TempDir(), "First", "", "")
	if err != nil {
		t.Fatal(err)
	}
	secondBook, err := application.CreateBook(ctx, t.TempDir(), "Second", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first := ConversationConfigBinding{Mode: ConversationModeAgentChat, ProjectID: firstBook.ProjectID, SessionID: "first-draft"}
	second := ConversationConfigBinding{Mode: ConversationModeAgentChat, ProjectID: secondBook.ProjectID, SessionID: "second-draft"}
	// Load both Project runtimes before saving, so this also checks cache refresh.
	if _, err := application.ConversationConfig(ctx, second); err != nil {
		t.Fatal(err)
	}
	thinking := "high"
	saved, err := application.PatchConversationConfig(ctx, first, ConversationConfigPatch{ThinkingLevel: &thinking}, 0)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := application.ConversationConfig(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ThinkingLevel != thinking || preview.Revision != 0 {
		t.Fatalf("other Book draft did not use saved model preference: %#v", preview)
	}
	// A rejected stale edit must not change the remembered preference.
	rejectedThinking := "max"
	_, err = application.PatchConversationConfig(ctx, first, ConversationConfigPatch{ThinkingLevel: &rejectedThinking}, 0)
	if !IsConversationConfigRevisionConflict(err) {
		t.Fatalf("stale update = %v", err)
	}
	invalidProfile := "missing-model"
	_, err = application.PatchConversationConfig(ctx, first, ConversationConfigPatch{ProfileID: &invalidProfile}, saved.Revision)
	if err == nil || errors.Is(err, ErrConversationModelDefaultsNotSaved) {
		t.Fatalf("invalid model must fail before persistence: %v", err)
	}
	settings, err := application.SettingsService().Snapshot(appsettings.Global())
	if err != nil {
		t.Fatal(err)
	}
	if got := settings.User.AgentModels.IDE; got.ThinkingLevel != thinking || got.ProfileID == invalidProfile {
		t.Fatalf("rejected selection changed model defaults: %#v", got)
	}
}
