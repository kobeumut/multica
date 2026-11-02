package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAutoRouterNeverEnqueuesAsGeneralAgent(t *testing.T) {
	s := &TaskService{}
	_, err := s.applyAttributionFallback(context.Background(), attribution.Result{}, db.Agent{SystemKey: pgtype.Text{String: "autorouter", Valid: true}})
	if err == nil {
		t.Fatal("unresolved AutoRouter was allowed at the enqueue boundary")
	}
}
