package app

import (
	"context"
	"errors"
	"testing"

	"denova/config"
	agentconversation "denova/internal/agents/conversation"
	"denova/internal/agents/session"
	apptask "denova/internal/app/task"
	"denova/internal/interactive"
)

func TestConversationConfigUpdateDoesNotMutatePreparedCycle(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeCfg := config.Config{
		OpenAIModel:       "test-model",
		AgentApprovalMode: config.AgentApprovalWrite,
		AgentModels: config.AgentModelSettings{
			IDE: config.AgentModelOverride{ThinkingLevel: "medium"},
		},
	}
	sess, initial, err := agentconversation.GetOrCreateSession(store, "writing", &runtimeCfg, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}

	preparedCycle := runtimeCfg
	if _, err := agentconversation.ApplySession(sess, &preparedCycle, config.AgentKindIDE); err != nil {
		t.Fatal(err)
	}
	next := initial.Config
	next.ThinkingLevel = "high"
	next.ApprovalMode = config.AgentApprovalFullAccess
	if _, err := sess.SetRuntimeConfig(next, initial.Revision); err != nil {
		t.Fatal(err)
	}

	preparedModel := config.ResolveAgentModel(&preparedCycle, config.AgentKindIDE)
	if preparedModel.ThinkingLevel != "medium" || preparedCycle.AgentApprovalMode != config.AgentApprovalWrite {
		t.Fatalf("prepared cycle changed after conversation update: model=%#v approval=%q", preparedModel, preparedCycle.AgentApprovalMode)
	}
	followingCycle := runtimeCfg
	if _, err := agentconversation.ApplySession(sess, &followingCycle, config.AgentKindIDE); err != nil {
		t.Fatal(err)
	}
	followingModel := config.ResolveAgentModel(&followingCycle, config.AgentKindIDE)
	if followingModel.ThinkingLevel != "high" || followingCycle.AgentApprovalMode != config.AgentApprovalFullAccess {
		t.Fatalf("following cycle did not use updated conversation config: model=%#v approval=%q", followingModel, followingCycle.AgentApprovalMode)
	}
}

func TestAppAllowsConversationConfigUpdatesDuringWritingAndGameRuns(t *testing.T) {
	application := newExecutionProfileTestApp(t)
	application.mu.RLock()
	projectID := application.cfg.ProjectID
	workspace := application.workspace
	sess := application.session
	application.mu.RUnlock()

	writingBinding := ConversationConfigBinding{
		Mode: ConversationModeWriting, ProjectID: projectID, SessionID: sess.ID,
	}
	writingConfig, err := application.ConversationConfig(context.Background(), writingBinding)
	if err != nil {
		t.Fatal(err)
	}
	writingTask, err := apptask.NewDeferred(nil)
	if err != nil {
		t.Fatal(err)
	}
	application.mu.Lock()
	application.activeWritingRun = &writingTaskRun{
		task: writingTask, runtime: ideChatRuntime{workspace: workspace, sess: sess},
	}
	application.mu.Unlock()
	approvalMode := config.AgentApprovalFullAccess
	updatedWriting, err := application.PatchConversationConfig(context.Background(), writingBinding, ConversationConfigPatch{
		ApprovalMode: &approvalMode,
	}, writingConfig.Revision)
	writingTask.RejectStart(errors.New("test complete"))
	if err != nil {
		t.Fatalf("update writing config during active run: %v", err)
	}
	if updatedWriting.Revision != writingConfig.Revision+1 || updatedWriting.ApprovalMode != approvalMode {
		t.Fatalf("updated writing config = %#v", updatedWriting)
	}

	story, err := application.CreateInteractiveStory(interactive.CreateStoryRequest{
		Title: "Active config", StoryTellerID: "classic",
	})
	if err != nil {
		t.Fatal(err)
	}
	gameBinding := ConversationConfigBinding{
		Mode: ConversationModeInteractive, ProjectID: projectID, StoryID: story.ID, BranchID: "main",
	}
	gameConfig, err := application.ConversationConfig(context.Background(), gameBinding)
	if err != nil {
		t.Fatal(err)
	}
	gameTask, err := apptask.NewDeferred(nil)
	if err != nil {
		t.Fatal(err)
	}
	application.mu.Lock()
	application.activeInteractiveRun = &interactiveTaskRun{
		task: gameTask,
		info: InteractiveTaskInfo{ProjectID: projectID, Workspace: workspace, StoryID: story.ID, BranchID: "main"},
	}
	application.mu.Unlock()
	thinkingLevel := "high"
	updatedGame, err := application.PatchConversationConfig(context.Background(), gameBinding, ConversationConfigPatch{
		ThinkingLevel: &thinkingLevel,
	}, gameConfig.Revision)
	gameTask.RejectStart(errors.New("test complete"))
	if err != nil {
		t.Fatalf("update game config during active run: %v", err)
	}
	if updatedGame.Revision != gameConfig.Revision+1 || updatedGame.ThinkingLevel != thinkingLevel {
		t.Fatalf("updated game config = %#v", updatedGame)
	}
}

func TestNewConversationUsesModelDefaultDespiteOlderSessionActivity(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeCfg := config.Config{
		AgentApprovalMode: config.AgentApprovalWrite,
		AgentModels: config.AgentModelSettings{
			IDE:     config.AgentModelOverride{ThinkingLevel: "medium"},
			General: config.AgentModelOverride{ThinkingLevel: "low"},
		},
	}

	first, firstSnapshot, err := agentconversation.GetOrCreateSession(store, "writing-first", &runtimeCfg, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}
	firstSelection := firstSnapshot.Config
	firstSelection.ThinkingLevel = "high"
	firstSelection.ApprovalMode = config.AgentApprovalFullAccess
	configuredFirst, err := first.SetRuntimeConfig(firstSelection, firstSnapshot.Revision)
	if err != nil {
		t.Fatal(err)
	}

	_, inherited, err := agentconversation.GetOrCreateSession(store, "writing-second", &runtimeCfg, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}
	expected := configuredFirst.Config
	expected.ThinkingLevel = "medium"
	if inherited.Revision != 1 || inherited.Config != expected {
		t.Fatalf("new writing conversation did not use its model default: %#v", inherited)
	}

	firstAfter, ok := first.RuntimeConfig()
	if !ok || firstAfter != configuredFirst {
		t.Fatalf("creating a new conversation changed the older one: %#v", firstAfter)
	}

	_, general, err := agentconversation.GetOrCreateSession(store, "general-first", &runtimeCfg, config.AgentKindGeneral)
	if err != nil {
		t.Fatal(err)
	}
	if general.AgentKind != config.AgentKindGeneral || general.ThinkingLevel != "low" || general.ApprovalMode != config.AgentApprovalWrite {
		t.Fatalf("different Agent kind should use its own default/history: %#v", general)
	}
}

func TestLegacyConversationInitializesOnceAndThenStaysIndependent(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := store.GetOrCreate("legacy-writing")
	if err != nil {
		t.Fatal(err)
	}
	runtimeCfg := config.Config{
		AgentApprovalMode: config.AgentApprovalWrite,
		AgentModels: config.AgentModelSettings{
			IDE: config.AgentModelOverride{ThinkingLevel: "medium"},
		},
	}
	initialized, err := agentconversation.EnsureSession(legacy, &runtimeCfg, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}

	runtimeCfg.AgentApprovalMode = config.AgentApprovalAsk
	runtimeCfg.AgentModels.IDE.ThinkingLevel = "max"
	again, err := agentconversation.EnsureSession(legacy, &runtimeCfg, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}
	if again != initialized {
		t.Fatalf("initialized old conversation followed later Settings: before=%#v after=%#v", initialized, again)
	}
}

func TestConversationConfigPreviewDoesNotPersistDraftSession(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeCfg := config.Config{
		AgentApprovalMode: config.AgentApprovalWrite,
		AgentModels: config.AgentModelSettings{
			General: config.AgentModelOverride{ThinkingLevel: "medium"},
		},
	}
	snapshot, err := agentconversation.PreviewSession(store, "local-draft", &runtimeCfg, config.AgentKindGeneral)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 0 || snapshot.AgentKind != config.AgentKindGeneral || snapshot.ThinkingLevel != "medium" {
		t.Fatalf("draft preview = %#v", snapshot)
	}
	if store.Exists("local-draft") {
		t.Fatal("reading draft configuration must not create a durable session")
	}
	_, persisted, err := agentconversation.GetOrCreateSession(store, "local-draft", &runtimeCfg, config.AgentKindGeneral)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Revision != 1 || !store.Exists("local-draft") {
		t.Fatalf("persisted draft configuration = %#v", persisted)
	}
}

func TestInteractiveConversationConfigPreviewUsesModelDefaultWithoutCreatingStory(t *testing.T) {
	application := newExecutionProfileTestApp(t)
	application.mu.RLock()
	projectID := application.cfg.ProjectID
	application.mu.RUnlock()

	story, err := application.CreateInteractiveStory(interactive.CreateStoryRequest{
		Title: "Configured opening", StoryTellerID: "classic", ProfileID: "default", ThinkingLevel: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	thinking := "medium"
	if _, err := application.PatchConversationConfig(context.Background(), ConversationConfigBinding{
		Mode: ConversationModeInteractive, ProjectID: projectID,
	}, ConversationConfigPatch{ThinkingLevel: &thinking}, 0); err != nil {
		t.Fatal(err)
	}
	before, err := application.InteractiveStories()
	if err != nil {
		t.Fatal(err)
	}

	preview, err := application.ConversationConfig(context.Background(), ConversationConfigBinding{
		Mode: ConversationModeInteractive, ProjectID: projectID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Revision != 0 || preview.ProfileID != "default" || preview.ThinkingLevel != thinking {
		t.Fatalf("new story runtime preview = %#v", preview)
	}
	after, err := application.InteractiveStories()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Stories) != len(before.Stories) || after.CurrentStoryID != story.ID {
		t.Fatalf("preview changed story index: before=%#v after=%#v", before, after)
	}
}

func TestInteractiveStoryCreationPersistsOpeningRuntimeSelection(t *testing.T) {
	application := newExecutionProfileTestApp(t)
	application.mu.RLock()
	projectID := application.cfg.ProjectID
	application.mu.RUnlock()

	story, err := application.CreateInteractiveStory(interactive.CreateStoryRequest{
		Title: "Explicit opening runtime", StoryTellerID: "classic", ProfileID: "default", ThinkingLevel: "max",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := application.ConversationConfig(context.Background(), ConversationConfigBinding{
		Mode: ConversationModeInteractive, ProjectID: projectID, StoryID: story.ID, BranchID: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 1 || snapshot.ProfileID != "default" || snapshot.ThinkingLevel != "max" {
		t.Fatalf("created story runtime config = %#v", snapshot)
	}
}
