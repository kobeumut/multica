package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/autorouter"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func autoRouterRequest(t *testing.T, workspaceID, method, path string, body any) *http.Request {
	t.Helper()
	req := newRequest(method, path, body)
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(req.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: parseUUID(testUserID), WorkspaceID: parseUUID(workspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), workspaceID, member))
}

func TestAutoRouterProvisionKeepsEditableCategories(t *testing.T) {
	ws := dbfx.Workspace(t, "AutoRouter test", "router-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	runtimeID := dbfx.Runtime(t, "router runtime", testutil.Cols{"workspace_id": ws})
	create := func() (int, string) {
		req := autoRouterRequest(t, ws, "POST", "/api/agents/autorouter", map[string]string{"runtime_id": runtimeID})
		w := httptest.NewRecorder()
		testHandler.CreateAutoRouterAgent(w, req)
		var result struct {
			ID        string `json:"id"`
			SystemKey string `json:"system_key"`
		}
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.SystemKey != autorouter.SystemKey {
			t.Fatalf("provision: %d %s", w.Code, w.Body.String())
		}
		if w.Code == http.StatusOK {
			var fields map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &fields)
			if len(fields) != 2 {
				t.Fatalf("retry exposed existing agent configuration: %v", fields)
			}
		}
		return w.Code, result.ID
	}
	code, id := create()
	if code != http.StatusCreated {
		t.Fatalf("first create %d", code)
	}
	dbfx.Cleanup(t, "DELETE FROM agent WHERE id=$1", id)
	dbfx.Cleanup(t, "DELETE FROM issue_label WHERE workspace_id=$1", ws)
	if n := dbfx.Count(t, "SELECT count(*) FROM issue_label WHERE workspace_id=$1 AND resource_type='agent'", ws); n != 3 {
		t.Fatalf("seed categories: %d", n)
	}
	dbfx.Exec(t, "UPDATE issue_label SET description='Custom difficulty' WHERE workspace_id=$1 AND name='Normal'", ws)
	dbfx.Exec(t, "DELETE FROM issue_label WHERE workspace_id=$1 AND name='Entry-level'", ws)
	code, again := create()
	if code != http.StatusOK || again != id {
		t.Fatalf("not idempotent: %d %s", code, again)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM issue_label WHERE workspace_id=$1 AND resource_type='agent'", ws); n != 2 {
		t.Fatal("retry reseeded deleted categories")
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM issue_label WHERE workspace_id=$1 AND description='Custom difficulty'", ws); n != 1 {
		t.Fatal("retry reset edited category")
	}
}

func TestAutoRouterDecisionIsolationLeaseAndSingleCompletion(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "decision runtime")
	otherWS := dbfx.Workspace(t, "Other router workspace", "router-"+uuid.NewString())
	dbfx.Member(t, otherWS, testUserID, "owner")
	otherRuntimeID := dbfx.Runtime(t, "other runtime", testutil.Cols{"workspace_id": otherWS})
	decisionID := dbfx.Insert(t, "autorouter_decision", testutil.Cols{"id": uuid.NewString(), "runtime_id": runtimeID, "input": `{"state":"test","candidates":[{"id":"allowed"}]}`, "expires_at": time.Now().Add(time.Minute)})
	report := func(runtimeID, agentID string) int {
		req := newOwnerDaemonRequest("POST", "/result", map[string]string{"agent_id": agentID})
		req = withURLParams(req, "runtimeId", runtimeID, "requestId", decisionID)
		w := httptest.NewRecorder()
		testHandler.ReportAutoRouterResult(w, req)
		return w.Code
	}
	if got := report(otherRuntimeID, "allowed"); got != http.StatusNotFound {
		t.Fatalf("other runtime got %d", got)
	}
	if got := report(runtimeID, "allowed"); got != http.StatusConflict {
		t.Fatalf("unclaimed result got %d", got)
	}
	if testHandler.claimAutoRouterDecision(ctx, runtimeID) == nil {
		t.Fatal("claim missing")
	}
	if testHandler.claimAutoRouterDecision(ctx, runtimeID) != nil {
		t.Fatal("active lease reclaimed")
	}
	dbfx.Exec(t, "UPDATE autorouter_decision SET claim_until=now()-interval '1 second' WHERE id=$1", decisionID)
	freshID := dbfx.Insert(t, "autorouter_decision", testutil.Cols{"id": uuid.NewString(), "runtime_id": runtimeID, "input": `{}`, "expires_at": time.Now().Add(2 * time.Minute)})
	fresh := testHandler.claimAutoRouterDecision(ctx, runtimeID)
	if fresh == nil || fresh.ID != freshID {
		t.Fatal("expired lease starved never-claimed request")
	}
	dbfx.Exec(t, `UPDATE autorouter_decision SET result='{}' WHERE id=$1`, freshID)
	if testHandler.claimAutoRouterDecision(ctx, runtimeID) == nil {
		t.Fatal("lost heartbeat could not be reclaimed")
	}
	if got := report(runtimeID, "private"); got != http.StatusBadRequest {
		t.Fatalf("invented candidate got %d", got)
	}
	if got := report(runtimeID, "allowed"); got != http.StatusOK {
		t.Fatalf("valid result got %d", got)
	}
	if got := report(runtimeID, "allowed"); got != http.StatusConflict {
		t.Fatalf("duplicate got %d", got)
	}
	if testHandler.claimAutoRouterDecision(ctx, runtimeID) != nil {
		t.Fatal("completed request reclaimed")
	}
	dbfx.Exec(t, "UPDATE autorouter_decision SET expires_at=now()-interval '1 second' WHERE id=$1", decisionID)
	if got := report(runtimeID, "allowed"); got != http.StatusNotFound {
		t.Fatalf("expired result got %d", got)
	}
}

func TestAutoRouterNewChatRoutesOnceAndQueuesSelectedAgent(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "chat router runtime")
	routerID := dbfx.Agent(t, "AutoRouter", runtimeID, testutil.Cols{"system_key": autorouter.SystemKey})
	selectedID := dbfx.Agent(t, "Mobile developer", runtimeID, testutil.Cols{"description": "React Native mobile development"})
	sessionID := createHandlerTestChatSession(t, routerID)
	// A deterministic daemon substitute tests real persistence/dispatch without a model download.
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-ticker.C:
				request := testHandler.claimAutoRouterDecision(ctx, runtimeID)
				if request == nil {
					continue
				}
				body, _ := json.Marshal(autorouter.Result{AgentID: selectedID})
				_, err := testHandler.Queries.CompleteAutoRouterDecision(ctx, db.CompleteAutoRouterDecisionParams{ID: parseUUID(request.ID), RuntimeID: parseUUID(runtimeID), Result: body})
				done <- err
				return
			}
		}
	}()
	send := func(content string) {
		req := autoRouterRequest(t, testWorkspaceID, "POST", "/messages", map[string]string{"content": content})
		req = withURLParam(req, "sessionId", sessionID)
		w := httptest.NewRecorder()
		testHandler.SendChatMessage(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("send: %d %s", w.Code, w.Body.String())
		}
	}
	send("Fix a React Native screen")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	send("And add accessibility labels")
	if n := dbfx.Count(t, "SELECT count(*) FROM chat_session WHERE id=$1 AND agent_id=$2", sessionID, selectedID); n != 1 {
		t.Fatal("session did not keep selected agent")
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1 AND agent_id=$2", sessionID, selectedID); n != 2 {
		t.Fatalf("selected agent tasks: %d", n)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE agent_id=$1", routerID); n != 0 {
		t.Fatal("router executed general-purpose task")
	}
	_, err := testHandler.Queries.RouteEmptyChatSession(context.Background(), db.RouteEmptyChatSessionParams{ID: parseUUID(sessionID), RouterID: parseUUID(routerID), AgentID: parseUUID(routerID)})
	if err == nil {
		t.Fatal("active chat was rerouted")
	}
}

func TestAutoRouterOpeningTurnIsAtomicAndSingleWinner(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "atomic router runtime")
	routerID := dbfx.Agent(t, "AutoRouter", runtimeID, testutil.Cols{"system_key": autorouter.SystemKey})
	selectedID := dbfx.Agent(t, "Selected", runtimeID)
	sessionID := createHandlerTestChatSession(t, routerID)
	session, err := testHandler.Queries.GetChatSession(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := testHandler.Queries.GetAgent(ctx, parseUUID(selectedID))
	if err != nil {
		t.Fatal(err)
	}
	send := func() error {
		_, err := testHandler.TaskService.SendDirectChatMessage(ctx, session, selected, parseUUID(testUserID), "Opening request", nil, "member", parseUUID(testUserID))
		return err
	}
	// The carrier check fails AFTER the handoff CAS: the entire turn must roll back.
	dbfx.Exec(t, "UPDATE agent SET archived_at=now() WHERE id=$1", selectedID)
	if err := send(); !errors.Is(err, service.ErrChatTaskAgentArchived) {
		t.Fatalf("send: %v", err)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM chat_session WHERE id=$1 AND agent_id=$2", sessionID, routerID); n != 1 {
		t.Fatal("failed opening committed its handoff")
	}
	dbfx.Exec(t, "UPDATE agent SET archived_at=NULL WHERE id=$1", selectedID)
	done := make(chan error, 2)
	go func() { done <- send() }()
	go func() { done <- send() }()
	first, second := <-done, <-done
	if !((first == nil && errors.Is(second, service.ErrChatSessionAgentChanged)) || (second == nil && errors.Is(first, service.ErrChatSessionAgentChanged))) {
		t.Fatalf("expected one winner and one conflict, got %v / %v", first, second)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1 AND agent_id=$2 AND chat_input_task_id=id", sessionID, selectedID); n != 1 {
		t.Fatalf("opening tasks: %d", n)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM chat_message WHERE chat_session_id=$1 AND task_id IS NOT NULL", sessionID); n != 1 {
		t.Fatalf("opening messages: %d", n)
	}
}

func TestAutoRouterRejectsPermissionRevokedDuringDecision(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "revoke router runtime", testutil.Cols{"visibility": "public"})
	routerID := dbfx.Agent(t, "AutoRouter", runtimeID, testutil.Cols{"system_key": autorouter.SystemKey})
	otherOwner := dbfx.User(t, "Candidate owner", uuid.NewString()+"@example.test")
	dbfx.Member(t, testWorkspaceID, otherOwner, "member")
	selectedID := dbfx.Agent(t, "Initially shared agent", runtimeID, testutil.Cols{"owner_id": otherOwner, "visibility": "workspace", "permission_mode": "public_to"})
	dbfx.InsertNoID(t, "agent_invocation_target", testutil.Cols{"agent_id": selectedID, "target_type": "workspace", "target_id": testWorkspaceID}, "agent_id=$1", selectedID)
	sessionID := createHandlerTestChatSession(t, routerID)
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-ticker.C:
				request := testHandler.claimAutoRouterDecision(ctx, runtimeID)
				if request == nil {
					continue
				}
				_, err := testPool.Exec(ctx, "UPDATE agent SET permission_mode='private' WHERE id=$1", selectedID)
				if err != nil {
					done <- err
					return
				}
				body, _ := json.Marshal(autorouter.Result{AgentID: selectedID})
				_, err = testHandler.Queries.CompleteAutoRouterDecision(ctx, db.CompleteAutoRouterDecisionParams{ID: parseUUID(request.ID), RuntimeID: parseUUID(runtimeID), Result: body})
				done <- err
				return
			}
		}
	}()
	req := autoRouterRequest(t, testWorkspaceID, "POST", "/messages", map[string]string{"content": "A test request"})
	req = withURLParam(req, "sessionId", sessionID)
	w := httptest.NewRecorder()
	testHandler.SendChatMessage(w, req)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "no longer permitted") {
		t.Fatalf("revoked agent was not refused: %d %s", w.Code, w.Body.String())
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1", sessionID); n != 0 {
		t.Fatal("queued work after permission revocation")
	}
}

func TestAutoRouterUpdateUsesExistingIssueText(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprintf("batch=%v", batch), func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "issue router runtime")
			routerID := dbfx.Agent(t, "AutoRouter", runtimeID, testutil.Cols{"system_key": autorouter.SystemKey})
			selectedID := dbfx.Agent(t, "Mobile developer", runtimeID)
			issueID := dbfx.Issue(t, "Existing mobile task", testutil.Cols{"description": "Original requirements", "status": "backlog"})
			done := make(chan string, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						done <- ctx.Err().Error()
						return
					case <-ticker.C:
						request := testHandler.claimAutoRouterDecision(ctx, runtimeID)
						if request == nil {
							continue
						}
						var input autorouter.Input
						if json.Unmarshal(request.Input, &input) != nil || !strings.Contains(input.State, "Existing mobile task") || !strings.Contains(input.State, "Original requirements") {
							done <- "missing existing issue text"
							return
						}
						body, _ := json.Marshal(autorouter.Result{AgentID: selectedID})
						_, err := testHandler.Queries.CompleteAutoRouterDecision(ctx, db.CompleteAutoRouterDecisionParams{ID: parseUUID(request.ID), RuntimeID: parseUUID(runtimeID), Result: body})
						if err != nil {
							done <- err.Error()
						} else {
							done <- ""
						}
						return
					}
				}
			}()
			updates := map[string]string{"assignee_type": "agent", "assignee_id": routerID}
			req := autoRouterRequest(t, testWorkspaceID, "PUT", "/issue", updates)
			w := httptest.NewRecorder()
			if batch {
				req = autoRouterRequest(t, testWorkspaceID, "PUT", "/issues/batch", map[string]any{"issue_ids": []string{issueID}, "updates": updates})
				testHandler.BatchUpdateIssues(w, req)
			} else {
				req = withURLParam(req, "id", issueID)
				testHandler.UpdateIssue(w, req)
			}
			if w.Code != http.StatusOK {
				t.Fatalf("update: %d %s", w.Code, w.Body.String())
			}
			if err := <-done; err != "" {
				t.Fatal(err)
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM issue WHERE id=$1 AND assignee_id=$2", issueID, selectedID); n != 1 {
				t.Fatal("issue not assigned to selected agent")
			}
		})
	}
}
