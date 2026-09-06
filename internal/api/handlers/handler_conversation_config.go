package handlers

import (
	"context"
	"errors"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	appsvc "denova/internal/app"
)

func (h *Handlers) HandleConversationConfigGet(ctx context.Context, c *app.RequestContext) {
	binding, ok := conversationConfigBindingFromQuery(c)
	if !ok {
		return
	}
	snapshot, err := h.app.ConversationConfig(ctx, binding)
	if err != nil {
		writeConversationConfigError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, snapshot)
}

func (h *Handlers) HandleConversationConfigPatch(ctx context.Context, c *app.RequestContext) {
	var body struct {
		Binding      appsvc.ConversationConfigBinding `json:"binding"`
		BaseRevision uint64                           `json:"base_revision"`
		Changes      appsvc.ConversationConfigPatch   `json:"changes"`
	}
	if err := decodeStrictJSONRequest(c.Request.Body(), &body); err != nil {
		writeErrorKey(c, consts.StatusBadRequest, "api.common.invalidRequestWithDetail", "detail", err.Error())
		return
	}
	if !bindConversationConfigProject(c, &body.Binding) {
		return
	}
	snapshot, err := h.app.PatchConversationConfig(ctx, body.Binding, body.Changes, body.BaseRevision)
	if err != nil {
		writeConversationConfigError(c, err)
		return
	}
	writeJSON(c, consts.StatusOK, snapshot)
}

func conversationConfigBindingFromQuery(c *app.RequestContext) (appsvc.ConversationConfigBinding, bool) {
	binding := appsvc.ConversationConfigBinding{
		Mode: c.Query("mode"), ProjectID: c.Query("project_id"), SessionID: c.Query("session_id"),
		StoryID: c.Query("story_id"), BranchID: c.Query("branch_id"),
		Origin: c.Query("origin"), ResourceID: c.Query("resource_id"), RunID: c.Query("run_id"),
	}
	return binding, bindConversationConfigProject(c, &binding)
}

func bindConversationConfigProject(c *app.RequestContext, binding *appsvc.ConversationConfigBinding) bool {
	if binding == nil {
		writeErrorKey(c, consts.StatusBadRequest, "api.common.invalidBody")
		return false
	}
	if scope := projectScope(c); scope.ProjectID != "" {
		binding.ProjectID = scope.ProjectID
		return true
	}
	writeErrorKey(c, consts.StatusBadRequest, "api.project.idRequired")
	return false
}

func writeConversationConfigError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, appsvc.ErrConversationModelDefaultsNotSaved):
		writeErrorKey(c, consts.StatusInternalServerError, "api.conversationConfig.rememberModelFailed")
	case appsvc.IsConversationConfigRevisionConflict(err):
		writeErrorKey(c, consts.StatusConflict, "api.conversationConfig.revisionConflict")
	case errors.Is(err, appsvc.ErrNoWorkspace), errors.Is(err, appsvc.ErrNoWorkspaceOpen):
		writeErrorKey(c, consts.StatusBadRequest, "api.settings.workspaceMissing")
	default:
		writeErrorKey(c, consts.StatusBadRequest, "api.common.invalidRequestWithDetail", "detail", err.Error())
	}
}
