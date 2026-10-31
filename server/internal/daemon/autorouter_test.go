package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/autorouter"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestAutoRouterDrainsCoalescedPendingWork(t *testing.T) {
	var heartbeats atomic.Int32
	reports := make(chan autorouter.Result, 2)
	empty := make(chan struct{})
	d, calls := pendingWorkHintDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/result") {
			var result autorouter.Result
			if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
				t.Errorf("result: %v", err)
			}
			select {
			case reports <- result:
			default:
				t.Error("duplicate report")
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		n := heartbeats.Add(1)
		response := HeartbeatResponse{}
		if n <= 2 {
			response.PendingAutoRouter = &protocol.DaemonHeartbeatPendingAutoRouter{ID: []string{"one", "two"}[n-1], Input: json.RawMessage(`"malformed"`), Deadline: time.Now().Add(10 * time.Second)}
		}
		_ = json.NewEncoder(w).Encode(response)
		if n == 3 {
			close(empty)
		}
	})
	// Both decisions finish inside one external-hint throttle window.
	withPendingWorkHintMinInterval(t, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d.rootCtx = ctx
	d.handlePendingWorkHint("rt-1", protocol.PendingWorkKindAutoRouter)
	select {
	case <-empty:
	case <-time.After(3 * time.Second):
		t.Fatal("pending requests did not drain")
	}
	for range 2 {
		select {
		case got := <-reports:
			if got.Error != "invalid routing input" || got.AgentID != "" {
				t.Fatalf("unexpected result: %+v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("missing report")
		}
	}
	time.Sleep(20 * time.Millisecond)
	if heartbeats.Load() != 3 || atomic.LoadInt32(calls) != 5 {
		t.Fatalf("unexpected polling: %d heartbeats, %d calls", heartbeats.Load(), atomic.LoadInt32(calls))
	}
}
