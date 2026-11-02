package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/internal/autorouter"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (d *Daemon) handleAutoRouter(ctx context.Context, runtimeID string, request protocol.DaemonHeartbeatPendingAutoRouter) {
	if _, active := d.autoRouterInflight.LoadOrStore(request.ID, struct{}{}); active {
		return
	}
	defer func() {
		d.autoRouterInflight.Delete(request.ID)
		d.requestPendingWorkCheck(runtimeID, protocol.PendingWorkKindAutoRouter)
	}()
	// ponytail: failed reports may re-run this side-effect-free decision on
	// lease redelivery; cache completed answers only if inference cost warrants it.
	decisionCtx, cancel := context.WithDeadline(ctx, request.Deadline)
	defer cancel()
	var input autorouter.Input
	result := autorouter.Result{}
	if err := json.Unmarshal(request.Input, &input); err != nil {
		result.Error = "invalid routing input"
	} else {
		id, err := autorouter.Decide(decisionCtx, input)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.AgentID = id
		}
	}
	// Reporting has its own short budget so an inference timeout is visible to
	// the caller instead of preventing the error report itself.
	reportCtx, reportCancel := context.WithTimeout(ctx, 5*time.Second)
	defer reportCancel()
	if err := d.client.ReportAutoRouterResult(reportCtx, runtimeID, request.ID, result); err != nil {
		d.logger.Warn("report AutoRouter decision", "request_id", request.ID, "error", err)
	}
}

func (c *Client) ReportAutoRouterResult(ctx context.Context, runtimeID, requestID string, result autorouter.Result) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/autorouter/%s/result", runtimeID, requestID), result, nil)
}
