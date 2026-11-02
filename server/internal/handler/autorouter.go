package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/autorouter"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// errAutoRouterUnavailable marks a failure to obtain any decision from the
// router's local model: a timeout, an unreachable Ollama or a malformed answer.
// It is distinct from a deliberate no-match, and from the authorization and
// readiness failures around the decision, which keep their own messages.
var errAutoRouterUnavailable = errors.New("AutoRouter could not decide")

// resolveAutoRouter only runs for the stable built-in identity. Candidate
// descriptions never leave the router's selected daemon or grant access.
func (h *Handler) resolveAutoRouter(r *http.Request, router db.Agent, state string) (db.Agent, error) {
	if router.SystemKey.String != autorouter.SystemKey {
		return router, nil
	}
	workspaceID := uuidToString(router.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	originator := h.invokeOriginatorFromRequest(r, actorType, actorID)
	if !h.canInvokeAgent(r.Context(), router, actorType, actorID, originator, workspaceID) {
		return db.Agent{}, errors.New("AutoRouter invocation not allowed")
	}
	if !router.RuntimeID.Valid {
		return db.Agent{}, errors.New("AutoRouter needs a local runtime")
	}
	agents, err := h.Queries.ListAgents(r.Context(), router.WorkspaceID)
	if err != nil {
		return db.Agent{}, err
	}
	input := autorouter.Input{State: state}
	for _, a := range agents {
		if a.SystemKey.String == autorouter.SystemKey || !a.RuntimeID.Valid || !h.canInvokeAgent(r.Context(), a, actorType, actorID, originator, workspaceID) {
			continue
		}
		verdict, err := service.AgentReadiness(r.Context(), h.runtimeLookup(obsmetrics.RuntimeLookupSourceChat), a)
		if err != nil || verdict.Blocked() {
			continue
		}
		labels, err := h.Queries.ListLabelsByAgent(r.Context(), db.ListLabelsByAgentParams{AgentID: a.ID, WorkspaceID: router.WorkspaceID})
		if err != nil {
			return db.Agent{}, err
		}
		categories := []string{}
		for _, label := range labels {
			categories = append(categories, label.Name+": "+label.Description)
		}
		input.Candidates = append(input.Candidates, autorouter.Candidate{ID: uuidToString(a.ID), Name: a.Name, Description: a.Description, Categories: categories})
	}
	if err := input.Validate(); err != nil {
		return db.Agent{}, err
	}
	id, err := h.awaitAutoRouterDecision(r.Context(), router.RuntimeID, input)
	if err != nil {
		return db.Agent{}, err
	}
	if !input.Contains(id) {
		return db.Agent{}, errors.New("invalid routing target")
	}
	selected, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: parseUUID(id), WorkspaceID: router.WorkspaceID})
	if err != nil || selected.ArchivedAt.Valid || selected.SystemKey.String == autorouter.SystemKey || !selected.RuntimeID.Valid {
		return db.Agent{}, errors.New("routing target is no longer available")
	}
	if !h.canInvokeAgent(r.Context(), selected, actorType, actorID, originator, workspaceID) {
		return db.Agent{}, errors.New("routing target is no longer permitted")
	}
	verdict, err := service.AgentReadiness(r.Context(), h.runtimeLookup(obsmetrics.RuntimeLookupSourceChat), selected)
	if err != nil || verdict.Blocked() {
		return db.Agent{}, errors.New("routing target cannot run")
	}
	return selected, nil
}

func (h *Handler) awaitAutoRouterDecision(ctx context.Context, runtimeID pgtype.UUID, input autorouter.Input) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	body, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	id := dbid.NewV7()
	if err := h.Queries.DeleteExpiredAutoRouterDecisions(ctx); err != nil {
		return "", err
	}
	if err := h.Queries.CreateAutoRouterDecision(ctx, db.CreateAutoRouterDecisionParams{ID: id, RuntimeID: runtimeID, Input: body, ExpiresAt: pgtype.Timestamptz{Time: deadline, Valid: true}}); err != nil {
		return "", err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = h.Queries.DeleteAutoRouterDecision(cleanup, id)
	}()
	h.requestDaemonPendingWork(uuidToString(runtimeID), protocol.PendingWorkKindAutoRouter)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("%w: timed out; check the daemon, Ollama and %s", errAutoRouterUnavailable, autorouter.Model)
		case <-ticker.C:
			decision, err := h.Queries.GetAutoRouterDecision(ctx, id)
			if err != nil {
				return "", fmt.Errorf("read routing decision: %w", err)
			}
			if len(decision.Result) == 0 {
				continue
			}
			var result autorouter.Result
			if err := json.Unmarshal(decision.Result, &result); err != nil {
				return "", err
			}
			if result.Error != "" {
				if result.NoMatch {
					return "", fmt.Errorf("%w: %s", autorouter.ErrNoMatch, result.Error)
				}
				return "", fmt.Errorf("%w: %s", errAutoRouterUnavailable, result.Error)
			}
			return result.AgentID, nil
		}
	}
}

func (h *Handler) ReportAutoRouterResult(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	requestID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "requestId"), "request_id")
	if !ok {
		return
	}
	decision, err := h.Queries.GetAutoRouterDecision(r.Context(), requestID)
	if err != nil || decision.RuntimeID != runtime.ID {
		writeError(w, http.StatusNotFound, "routing request not found")
		return
	}
	var input autorouter.Input
	var result autorouter.Result
	if json.Unmarshal(decision.Input, &input) != nil || json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&result) != nil {
		writeError(w, http.StatusBadRequest, "invalid routing result")
		return
	}
	if result.Error == "" && !input.Contains(result.AgentID) {
		writeError(w, http.StatusBadRequest, "invalid routing target")
		return
	}
	body, _ := json.Marshal(result)
	rows, err := h.Queries.CompleteAutoRouterDecision(r.Context(), db.CompleteAutoRouterDecisionParams{ID: requestID, RuntimeID: runtime.ID, Result: body})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save routing result")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusConflict, "routing request expired or completed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) claimAutoRouterDecision(ctx context.Context, runtimeID string) *protocol.DaemonHeartbeatPendingAutoRouter {
	if h.Queries == nil {
		return nil
	}
	pending, err := h.Queries.ClaimAutoRouterDecision(ctx, parseUUID(runtimeID))
	if err != nil {
		return nil
	}
	return &protocol.DaemonHeartbeatPendingAutoRouter{ID: uuidToString(pending.ID), Input: pending.Input, Deadline: pending.ExpiresAt.Time}
}

var errAutoRouterAssigneeNotFound = errors.New("assignee not found")

// resolveIssueAssignee resolves only explicit assignments, without writing an
// HTTP response: batch callers keep their existing per-item skip semantics.
func (h *Handler) resolveIssueAssignee(r *http.Request, workspaceID string, kind pgtype.Text, id pgtype.UUID, state string) (pgtype.UUID, error) {
	if !kind.Valid || kind.String != "agent" || !id.Valid {
		return id, nil
	}
	a, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: parseUUID(workspaceID)})
	if err != nil {
		return pgtype.UUID{}, errAutoRouterAssigneeNotFound
	}
	a, err = h.resolveAutoRouter(r, a, state)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return a.ID, nil
}

func (h *Handler) routeIssueAssignee(w http.ResponseWriter, r *http.Request, workspaceID string, kind pgtype.Text, id pgtype.UUID, state string) (pgtype.UUID, bool) {
	id, err := h.resolveIssueAssignee(r, workspaceID, kind, id, state)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, errAutoRouterAssigneeNotFound) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return pgtype.UUID{}, false
	}
	return id, true
}
