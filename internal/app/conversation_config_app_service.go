package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"denova/config"
	agentconversation "denova/internal/agents/conversation"
	"denova/internal/agents/conversationconfig"
	agentexecution "denova/internal/agents/execution"
	agentrun "denova/internal/agents/run"
	"denova/internal/agents/session"
	agentchatapp "denova/internal/app/agentchat"
	interactiveapp "denova/internal/app/interactive"
	appsettings "denova/internal/app/settings"
	"denova/internal/interactive"

	agent "github.com/alfredxw/denova/agent"
	publicgoal "github.com/alfredxw/denova/agent/goal"
)

const (
	ConversationModeWriting     = "writing"
	ConversationModeAgentChat   = "agent_chat"
	ConversationModeInteractive = "interactive"
)

// ConversationConfigBinding is the stable transport identity for every
// creator-visible conversation surface. AgentKind is always derived server
// side from the owning project/mode and is never caller-controlled.
type ConversationConfigBinding struct {
	Mode       string `json:"mode"`
	ProjectID  string `json:"project_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	StoryID    string `json:"story_id,omitempty"`
	BranchID   string `json:"branch_id,omitempty"`
	Origin     string `json:"origin,omitempty"`
	ResourceID string `json:"resource_id,omitempty"`
	RunID      string `json:"run_id,omitempty"`
}

// ConversationConfigPatch is the application-facing mutation contract. The
// transport layer depends on this type instead of reaching through app into
// the Agent implementation package.
type ConversationConfigPatch struct {
	CustomAgentID *string                   `json:"custom_agent_id,omitempty"`
	ProfileID     *string                   `json:"profile_id,omitempty"`
	ThinkingLevel *string                   `json:"thinking_level,omitempty"`
	ApprovalMode  *config.AgentApprovalMode `json:"approval_mode,omitempty"`
}

type ConversationGoalMutation struct {
	Action           string `json:"action"`
	Objective        string `json:"objective,omitempty"`
	ExpectedRevision uint64 `json:"expected_revision,omitempty"`
}

func IsConversationGoalRevisionConflict(err error) bool {
	return errors.Is(err, publicgoal.ErrRevisionConflict) || agentchatapp.IsGoalRevisionConflict(err)
}

func IsConversationGoalStateChanged(err error) bool {
	return errors.Is(err, publicgoal.ErrNotFound) || errors.Is(err, publicgoal.ErrNotActive)
}

func (a *App) ConversationGoal(ctx context.Context, binding ConversationConfigBinding) (agent.GoalState, bool, error) {
	switch normalizeConversationMode(binding.Mode) {
	case ConversationModeWriting:
		if err := a.requireForegroundConversationProject(binding.ProjectID); err != nil {
			return agent.GoalState{}, false, err
		}
		service := a.chat()
		service.admission.Lock()
		defer service.admission.Unlock()
		runtime, options, _, err := a.writingGoalRuntime(binding.SessionID)
		if err != nil {
			return agent.GoalState{}, false, err
		}
		return runtime.Goal(ctx, options)
	case ConversationModeAgentChat:
		return a.AgentChat().ConversationGoal(ctx, agentchatapp.Binding{ProjectID: binding.ProjectID, SessionID: binding.SessionID})
	case ConversationModeInteractive:
		if err := a.requireForegroundConversationProject(binding.ProjectID); err != nil {
			return agent.GoalState{}, false, err
		}
		service := a.interactiveService()
		service.admission.Lock()
		defer service.admission.Unlock()
		runtime, options, err := a.interactiveGoalRuntime(binding)
		if err != nil {
			return agent.GoalState{}, false, err
		}
		return runtime.Goal(ctx, options)
	default:
		return agent.GoalState{}, false, fmt.Errorf("goal is unsupported for conversation mode %q", binding.Mode)
	}
}

func (a *App) MutateConversationGoal(ctx context.Context, binding ConversationConfigBinding, mutation ConversationGoalMutation) (agent.GoalState, error) {
	action := strings.TrimSpace(mutation.Action)
	switch normalizeConversationMode(binding.Mode) {
	case ConversationModeWriting:
		if err := a.requireForegroundConversationProject(binding.ProjectID); err != nil {
			return agent.GoalState{}, err
		}
		service := a.chat()
		service.admission.Lock()
		defer service.admission.Unlock()
		runtime, options, _, err := a.writingGoalRuntime(binding.SessionID)
		if err != nil {
			return agent.GoalState{}, err
		}
		goalMutation := agent.GoalMutation{ExpectedRevision: mutation.ExpectedRevision}
		switch action {
		case "set":
			goalMutation.Kind, goalMutation.Objective = agent.GoalSet, mutation.Objective
		case "pause":
			goalMutation.Kind = agent.GoalPause
		case "resume":
			goalMutation.Kind = agent.GoalResume
		case "clear":
			goalMutation.Kind = agent.GoalClear
		default:
			return agent.GoalState{}, fmt.Errorf("unsupported goal action %q", action)
		}
		return runtime.UpdateGoal(ctx, options, goalMutation)
	case ConversationModeAgentChat:
		return a.AgentChat().MutateConversationGoal(ctx, agentchatapp.Binding{ProjectID: binding.ProjectID, SessionID: binding.SessionID}, action, mutation.Objective, mutation.ExpectedRevision)
	case ConversationModeInteractive:
		if err := a.requireForegroundConversationProject(binding.ProjectID); err != nil {
			return agent.GoalState{}, err
		}
		service := a.interactiveService()
		service.admission.Lock()
		defer service.admission.Unlock()
		runtime, options, err := a.interactiveGoalRuntime(binding)
		if err != nil {
			return agent.GoalState{}, err
		}
		goalMutation := agent.GoalMutation{ExpectedRevision: mutation.ExpectedRevision}
		switch action {
		case "set":
			goalMutation.Kind, goalMutation.Objective = agent.GoalSet, mutation.Objective
		case "pause":
			goalMutation.Kind = agent.GoalPause
		case "resume":
			goalMutation.Kind = agent.GoalResume
		case "clear":
			goalMutation.Kind = agent.GoalClear
		default:
			return agent.GoalState{}, fmt.Errorf("unsupported goal action %q", action)
		}
		return runtime.UpdateGoal(ctx, options, goalMutation)
	default:
		return agent.GoalState{}, fmt.Errorf("goal is unsupported for conversation mode %q", binding.Mode)
	}
}

func (a *App) interactiveGoalRuntime(binding ConversationConfigBinding) (*agentexecution.Runtime, agentrun.Options, error) {
	store, runtimeCfg, err := a.interactiveConversationRuntime(binding)
	if err != nil {
		return nil, agentrun.Options{}, err
	}
	branchID, err := resolveInteractiveProjectionBranch(store, binding.StoryID, binding.BranchID)
	if err != nil {
		return nil, agentrun.Options{}, err
	}
	a.mu.RLock()
	executionRuntime := a.executionRuntime
	workspace := strings.TrimSpace(a.workspace)
	a.mu.RUnlock()
	if executionRuntime == nil || workspace == "" {
		return nil, agentrun.Options{}, ErrNoWorkspace
	}
	return executionRuntime, agentrun.Options{
		AgentKind: agentrun.AgentKindInteractiveStory, ProjectID: runtimeCfg.ProjectID, StateRoot: runtimeCfg.ProjectStoreDir,
		Workspace: workspace, StoryID: binding.StoryID, BranchID: branchID, Mode: "interactive",
	}, nil
}

func (a *App) writingGoalRuntime(requestedSessionID string) (*agentexecution.Runtime, agentrun.Options, config.Config, error) {
	store, runtimeCfg, sessionID, err := a.foregroundConversationRuntime(requestedSessionID)
	if err != nil {
		return nil, agentrun.Options{}, config.Config{}, err
	}
	// The product Session remains the canonical writing-history owner, but Goal
	// state is held only by the public Agent Session.
	if _, err := store.Get(sessionID); err != nil {
		return nil, agentrun.Options{}, config.Config{}, err
	}
	a.mu.RLock()
	executionRuntime := a.executionRuntime
	workspace := strings.TrimSpace(a.workspace)
	a.mu.RUnlock()
	if executionRuntime == nil || workspace == "" {
		return nil, agentrun.Options{}, config.Config{}, ErrNoWorkspace
	}
	return executionRuntime, agentrun.Options{
		AgentKind: agentrun.AgentKindIDE, ProjectID: runtimeCfg.ProjectID, StateRoot: runtimeCfg.ProjectStoreDir,
		Workspace: workspace, SessionID: sessionID, Mode: "ide",
	}, runtimeCfg, nil
}

// UnmarshalJSON delegates the strict omitted-versus-null validation to the
// conversation domain and then projects the validated transport value.
func (patch *ConversationConfigPatch) UnmarshalJSON(data []byte) error {
	if patch == nil {
		return errors.New("conversation config patch is nil")
	}
	var parsed conversationconfig.Patch
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*patch = ConversationConfigPatch{
		CustomAgentID: parsed.CustomAgentID,
		ProfileID:     parsed.ProfileID,
		ThinkingLevel: parsed.ThinkingLevel,
		ApprovalMode:  parsed.ApprovalMode,
	}
	return nil
}

// IsConversationConfigRevisionConflict keeps transport error classification
// on the application boundary rather than exposing the Agent package.
func IsConversationConfigRevisionConflict(err error) bool {
	return errors.Is(err, conversationconfig.ErrRevisionConflict)
}

func (a *App) ConversationConfig(ctx context.Context, binding ConversationConfigBinding) (conversationconfig.Snapshot, error) {
	if a == nil {
		return conversationconfig.Snapshot{}, ErrNoWorkspace
	}
	switch normalizeConversationMode(binding.Mode) {
	case ConversationModeWriting:
		if err := a.requireForegroundConversationProject(binding.ProjectID); err != nil {
			return conversationconfig.Snapshot{}, err
		}
		return a.writingConversationConfig(binding)
	case ConversationModeAgentChat:
		return a.AgentChat().ConversationConfig(ctx, agentchatapp.Binding{
			ProjectID: binding.ProjectID, SessionID: binding.SessionID,
		})
	case ConversationModeInteractive:
		if err := a.requireForegroundConversationProject(binding.ProjectID); err != nil {
			return conversationconfig.Snapshot{}, err
		}
		return a.interactiveConversationConfig(binding)
	default:
		return conversationconfig.Snapshot{}, fmt.Errorf("unsupported conversation mode %q", binding.Mode)
	}
}

func (a *App) PatchConversationConfig(ctx context.Context, binding ConversationConfigBinding, patch ConversationConfigPatch, baseRevision uint64) (conversationconfig.Snapshot, error) {
	if patch.CustomAgentID == nil && patch.ProfileID == nil && patch.ThinkingLevel == nil && patch.ApprovalMode == nil {
		return conversationconfig.Snapshot{}, errors.New("conversation config changes are empty")
	}
	if patch.ProfileID != nil || patch.ThinkingLevel != nil {
		a.modelSelectionMu.Lock()
		defer a.modelSelectionMu.Unlock()
	}
	change := conversationconfig.Patch{
		CustomAgentID: patch.CustomAgentID,
		ProfileID:     patch.ProfileID,
		ThinkingLevel: patch.ThinkingLevel,
		ApprovalMode:  patch.ApprovalMode,
	}
	var snapshot conversationconfig.Snapshot
	var err error
	switch normalizeConversationMode(binding.Mode) {
	case ConversationModeWriting:
		if err := a.requireForegroundConversationProject(binding.ProjectID); err != nil {
			return conversationconfig.Snapshot{}, err
		}
		snapshot, err = a.patchWritingConversationConfig(binding, change, baseRevision)
	case ConversationModeAgentChat:
		snapshot, err = a.AgentChat().PatchConversationConfig(ctx, agentchatapp.Binding{
			ProjectID: binding.ProjectID, SessionID: binding.SessionID,
		}, change, baseRevision)
	case ConversationModeInteractive:
		if err := a.requireForegroundConversationProject(binding.ProjectID); err != nil {
			return conversationconfig.Snapshot{}, err
		}
		snapshot, err = a.patchInteractiveConversationConfig(binding, change, baseRevision)
	default:
		return conversationconfig.Snapshot{}, fmt.Errorf("unsupported conversation mode %q", binding.Mode)
	}
	if err != nil {
		return snapshot, err
	}
	return snapshot, a.rememberConversationModel(snapshot, change)
}

func (a *App) requireForegroundConversationProject(projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return errors.New("Project ID is required for a foreground conversation")
	}
	a.mu.RLock()
	foregroundProjectID := ""
	if a.cfg != nil {
		foregroundProjectID = strings.TrimSpace(a.cfg.ProjectID)
	}
	a.mu.RUnlock()
	if foregroundProjectID == "" || foregroundProjectID != projectID {
		return fmt.Errorf("foreground conversation Project mismatch: requested=%s current=%s", projectID, foregroundProjectID)
	}
	return nil
}

func (a *App) writingConversationConfig(binding ConversationConfigBinding) (conversationconfig.Snapshot, error) {
	service := a.chat()
	service.admission.Lock()
	defer service.admission.Unlock()
	store, runtimeCfg, sessionID, err := a.foregroundConversationRuntime(binding.SessionID)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	sess, err := store.Get(sessionID)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	return agentconversation.EnsureSession(sess, &runtimeCfg, config.AgentKindIDE)
}

func (a *App) patchWritingConversationConfig(binding ConversationConfigBinding, patch conversationconfig.Patch, baseRevision uint64) (conversationconfig.Snapshot, error) {
	service := a.chat()
	service.admission.Lock()
	defer service.admission.Unlock()
	store, runtimeCfg, sessionID, err := a.foregroundConversationRuntime(binding.SessionID)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	sess, err := store.Get(sessionID)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	current, err := agentconversation.EnsureSession(sess, &runtimeCfg, config.AgentKindIDE)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	next, err := conversationconfig.Merge(&runtimeCfg, current.Config, patch)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	return sess.SetRuntimeConfig(next, baseRevision)
}

func (a *App) interactiveConversationConfig(binding ConversationConfigBinding) (conversationconfig.Snapshot, error) {
	service := a.interactiveService()
	service.admission.Lock()
	defer service.admission.Unlock()
	if strings.TrimSpace(binding.StoryID) == "" {
		store, runtimeCfg, err := a.interactiveStoreRuntime()
		if err != nil {
			return conversationconfig.Snapshot{}, err
		}
		seed, err := interactiveapp.RecentConversationSeed(store, &runtimeCfg, "")
		if err != nil {
			return conversationconfig.Snapshot{}, err
		}
		return conversationconfig.Snapshot{Config: seed}, nil
	}
	store, runtimeCfg, err := a.interactiveConversationRuntime(binding)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	return interactiveapp.ApplyConversationConfig(store, &runtimeCfg, binding.StoryID, binding.BranchID)
}

func (a *App) patchInteractiveConversationConfig(binding ConversationConfigBinding, patch conversationconfig.Patch, baseRevision uint64) (conversationconfig.Snapshot, error) {
	service := a.interactiveService()
	service.admission.Lock()
	defer service.admission.Unlock()
	if strings.TrimSpace(binding.StoryID) == "" {
		if baseRevision != 0 || patch.CustomAgentID != nil || patch.ApprovalMode != nil {
			return conversationconfig.Snapshot{}, errors.New("a Game draft only accepts model preferences")
		}
		store, runtimeCfg, err := a.interactiveStoreRuntime()
		if err != nil {
			return conversationconfig.Snapshot{}, err
		}
		seed, err := interactiveapp.RecentConversationSeed(store, &runtimeCfg, "")
		if err != nil {
			return conversationconfig.Snapshot{}, err
		}
		next, err := conversationconfig.Merge(&runtimeCfg, seed, patch)
		return conversationconfig.Snapshot{Config: next}, err
	}
	store, runtimeCfg, err := a.interactiveConversationRuntime(binding)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	current, ok, err := store.BranchRuntimeConfig(binding.StoryID, binding.BranchID)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	if !ok {
		seed := conversationconfig.Default(&runtimeCfg, config.AgentKindInteractiveStory)
		current, err = store.EnsureBranchRuntimeConfig(binding.StoryID, binding.BranchID, seed)
		if err != nil {
			return conversationconfig.Snapshot{}, err
		}
	}
	next, err := conversationconfig.Merge(&runtimeCfg, current.Config, patch)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	return store.SetBranchRuntimeConfig(binding.StoryID, binding.BranchID, next, baseRevision)
}

func (a *App) foregroundConversationRuntime(requestedSessionID string) (*session.Store, config.Config, string, error) {
	a.mu.RLock()
	store := a.sessionStore
	runtimeCfg := config.Config{}
	if a.cfg != nil {
		runtimeCfg = *a.cfg
	}
	sessionID := strings.TrimSpace(requestedSessionID)
	if sessionID == "" && a.session != nil {
		sessionID = a.session.ID
	}
	workspace := strings.TrimSpace(a.workspace)
	a.mu.RUnlock()
	if store == nil || workspace == "" || sessionID == "" || isAgentSessionID(sessionID) {
		return nil, config.Config{}, "", ErrNoWorkspace
	}
	fresh, err := refreshConversationRuntimeConfig(runtimeCfg, workspace, runtimeCfg.ProjectStoreDir)
	return store, fresh, sessionID, err
}

func (a *App) interactiveConversationRuntime(binding ConversationConfigBinding) (*interactive.Store, config.Config, error) {
	if strings.TrimSpace(binding.StoryID) == "" {
		return nil, config.Config{}, errors.New("interactive story is required")
	}
	return a.interactiveStoreRuntime()
}

func (a *App) interactiveStoreRuntime() (*interactive.Store, config.Config, error) {
	a.mu.RLock()
	store := a.interactive
	workspace := strings.TrimSpace(a.workspace)
	runtimeCfg := config.Config{}
	if a.cfg != nil {
		runtimeCfg = *a.cfg
	}
	a.mu.RUnlock()
	if store == nil || workspace == "" {
		return nil, config.Config{}, ErrNoWorkspace
	}
	fresh, err := refreshConversationRuntimeConfig(runtimeCfg, workspace, runtimeCfg.ProjectStoreDir)
	return store, fresh, err
}

func refreshConversationRuntimeConfig(runtimeCfg config.Config, workspace, stateRoot string) (config.Config, error) {
	return appsettings.RefreshProject(runtimeCfg, workspace, stateRoot)
}

func normalizeConversationMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}
