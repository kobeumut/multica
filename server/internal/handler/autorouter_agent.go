package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/autorouter"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CreateAutoRouterAgent provisions one stable, workspace-invocable entry point.
// The selected runtime hosts the decision model; candidate agents keep their own runtimes.
func (h *Handler) CreateAutoRouterAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsID := parseUUID(workspaceID)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	var req struct {
		RuntimeID string `json:"runtime_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	runtimeID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	runtime, err := h.getAgentRuntime(r.Context(), obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if err != nil || runtime.WorkspaceID != wsID {
		writeError(w, http.StatusBadRequest, "runtime not found in workspace")
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "you cannot bind an agent to this runtime")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if _, err = tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "autorouter:"+workspaceID); err != nil {
		writeError(w, 500, "failed to lock AutoRouter")
		return
	}
	existing, err := q.GetAgentBySystemKey(r.Context(), db.GetAgentBySystemKeyParams{WorkspaceID: wsID, SystemKey: pgtype.Text{String: autorouter.SystemKey, Valid: true}})
	if err == nil {
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		if !h.canAccessPrivateAgent(r.Context(), existing, actorType, actorID, workspaceID) {
			writeError(w, http.StatusForbidden, "AutoRouter access not allowed")
			return
		}
		// Provisioning needs only identity, never the existing agent's secrets.
		writeJSON(w, http.StatusOK, map[string]string{"id": uuidToString(existing.ID), "system_key": autorouter.SystemKey})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "failed to find AutoRouter")
		return
	}
	if _, err = q.LockWorkspaceForChatSessionCreate(r.Context(), wsID); err != nil {
		writeError(w, 404, "workspace not found")
		return
	}
	runtime, err = q.LockAgentRuntimeForAccessChange(r.Context(), runtimeID)
	if err != nil || runtime.WorkspaceID != wsID || !canUseRuntimeForAgent(member, runtime) {
		writeError(w, 403, "runtime no longer available")
		return
	}
	agent, err := q.CreateSystemUserAgent(r.Context(), db.CreateSystemUserAgentParams{
		WorkspaceID: wsID, Name: "AutoRouter", Description: "Routes new chats and explicit issue assignments to an eligible agent using a local decision model, descriptions and routing categories.",
		RuntimeMode: runtime.RuntimeMode, RuntimeID: runtime.ID, Model: pgtype.Text{String: autorouter.Model, Valid: true}, Visibility: "workspace", PermissionMode: "public_to", MaxConcurrentTasks: 1, OwnerID: parseUUID(userID), SystemKey: pgtype.Text{String: autorouter.SystemKey, Valid: true},
	})
	if err != nil {
		writeError(w, 500, "failed to create AutoRouter")
		return
	}
	if err = replaceInvocationTargetsWithQueries(r.Context(), q, agent.ID, parseUUID(userID), []targetSpec{{targetType: invocationTargetWorkspace, targetID: wsID}}); err != nil {
		writeError(w, 500, "failed to grant AutoRouter access")
		return
	}
	// Seed only once. Labels remain ordinary editable workspace data afterwards.
	if _, err = tx.Exec(r.Context(), `INSERT INTO issue_label (workspace_id,resource_type,name,description,color) VALUES
 ($1,'agent','Entry-level','Simple tasks: fix typos, edit documentation, make small code changes and give short explanations.','#22c55e'),
 ($1,'agent','Normal','Standard implementation and general tasks.','#3b82f6'),
 ($1,'agent','Advanced','Complex architecture, debugging and high-risk changes.','#a855f7')
 ON CONFLICT DO NOTHING`, wsID); err != nil {
		writeError(w, 500, "failed to seed routing categories")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to commit AutoRouter")
		return
	}
	resp := h.agentToResponse(agent)
	if err = h.enrichAgentResponseWithTargets(r.Context(), &resp, agent.ID); err != nil {
		writeError(w, 500, "failed to read AutoRouter access")
		return
	}
	h.publish(protocol.EventAgentCreated, workspaceID, "member", userID, map[string]any{"agent": broadcastAgentResponse(resp)})
	writeJSON(w, http.StatusCreated, resp)
}
