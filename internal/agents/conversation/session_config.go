package conversation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"denova/config"
	"denova/internal/agents/conversationconfig"
	"denova/internal/agents/session"
)

// IsReservedSessionID reports whether an ID belongs to a fixed Agent surface
// rather than a creator-owned conversation. Prefixes cover scoped resources
// such as Config Manager sessions.
func IsReservedSessionID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	// Config Manager sessions remain on disk after the Agent's retirement, but
	// must never appear as runnable Project Agent conversations.
	if id == "config-manager-agent" || strings.HasPrefix(id, "config-manager-agent-") {
		return true
	}
	for _, definition := range config.AgentKindDefinitions() {
		if definition.SessionID == id || (definition.SessionID != "" && strings.HasPrefix(id, definition.SessionID+"-")) {
			return true
		}
	}
	return false
}

// ErrSessionStoreUnavailable reports that a conversation has no durable store.
var ErrSessionStoreUnavailable = errors.New("conversation session store is unavailable")

// RecentSessionSeed resolves the newest valid per-Agent selection or falls
// back to the current Settings default. Writing model preferences always come
// from Settings; other fields retain their existing per-Project inheritance.
func RecentSessionSeed(store *session.Store, runtime *config.Config, agentKind, excludeID string) (conversationconfig.Config, error) {
	if store != nil {
		recent, ok, err := store.RecentRuntimeConfig(agentKind, excludeID)
		if err != nil {
			return conversationconfig.Config{}, err
		}
		if ok {
			if agentKind == config.AgentKindIDE {
				// Model preferences belong to the user. Activity in an older
				// Project conversation must not replace the latest manual choice.
				defaults := conversationconfig.Default(runtime, agentKind)
				recent.ProfileID = defaults.ProfileID
				recent.ThinkingLevel = defaults.ThinkingLevel
			}
			if err := conversationconfig.Validate(runtime, recent, agentKind); err == nil {
				return recent, nil
			} else {
				slog.ErrorContext(context.Background(), fmt.Sprintf("[conversation-config] recent selection is unavailable agent_kind=%s profile_id=%s err=%v; using Settings default", agentKind, recent.ProfileID, err))
			}
		}
	}
	seed := conversationconfig.Default(runtime, agentKind)
	if err := conversationconfig.Validate(runtime, seed, agentKind); err != nil {
		return conversationconfig.Config{}, fmt.Errorf("resolve default conversation config: %w", err)
	}
	return seed, nil
}

// EnsureSession initializes a legacy session once and otherwise returns its
// immutable runtime-selection snapshot.
func EnsureSession(sess *session.Session, runtime *config.Config, agentKind string) (conversationconfig.Snapshot, error) {
	if sess == nil {
		return conversationconfig.Snapshot{}, errors.New("conversation session is nil")
	}
	if snapshot, ok := sess.RuntimeConfig(); ok {
		if snapshot.AgentKind != agentKind {
			return conversationconfig.Snapshot{}, fmt.Errorf("conversation Agent kind is %q, expected %q", snapshot.AgentKind, agentKind)
		}
		return snapshot, nil
	}
	seed := conversationconfig.Default(runtime, agentKind)
	if err := conversationconfig.Validate(runtime, seed, agentKind); err != nil {
		return conversationconfig.Snapshot{}, err
	}
	return sess.EnsureRuntimeConfig(seed)
}

// GetOrCreateSession resolves and durably initializes one conversation.
func GetOrCreateSession(store *session.Store, sessionID string, runtime *config.Config, agentKind string) (*session.Session, conversationconfig.Snapshot, error) {
	return resolveSession(store, sessionID, runtime, agentKind, true)
}

// PreviewSession resolves what a new conversation would inherit without
// persisting an empty draft.
func PreviewSession(store *session.Store, sessionID string, runtime *config.Config, agentKind string) (conversationconfig.Snapshot, error) {
	_, snapshot, err := resolveSession(store, sessionID, runtime, agentKind, false)
	return snapshot, err
}

func resolveSession(
	store *session.Store,
	sessionID string,
	runtime *config.Config,
	agentKind string,
	create bool,
) (*session.Session, conversationconfig.Snapshot, error) {
	if store == nil {
		return nil, conversationconfig.Snapshot{}, ErrSessionStoreUnavailable
	}
	if store.Exists(sessionID) {
		sess, err := store.Get(sessionID)
		if err != nil {
			return nil, conversationconfig.Snapshot{}, err
		}
		snapshot, err := EnsureSession(sess, runtime, agentKind)
		return sess, snapshot, err
	}
	seed, err := RecentSessionSeed(store, runtime, agentKind, sessionID)
	if err != nil {
		return nil, conversationconfig.Snapshot{}, err
	}
	if !create {
		return nil, conversationconfig.Snapshot{Config: seed}, nil
	}
	sess, err := store.GetOrCreateWithRuntimeConfig(sessionID, seed)
	if err != nil {
		return nil, conversationconfig.Snapshot{}, err
	}
	snapshot, ok := sess.RuntimeConfig()
	if !ok {
		return nil, conversationconfig.Snapshot{}, conversationconfig.ErrNotInitialized
	}
	return sess, snapshot, nil
}

// ApplySession validates and injects a session snapshot into a request-local
// runtime configuration.
func ApplySession(sess *session.Session, runtime *config.Config, agentKind string) (conversationconfig.Snapshot, error) {
	snapshot, err := EnsureSession(sess, runtime, agentKind)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	if err := conversationconfig.Apply(runtime, snapshot.Config); err != nil {
		return conversationconfig.Snapshot{}, fmt.Errorf("apply conversation runtime config: %w", err)
	}
	return snapshot, nil
}
