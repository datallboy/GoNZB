package wiring

import (
	"context"
	"strings"
	"testing"

	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type fakeIndexerIntegrityReader struct {
	report *pgindex.IndexerIntegrityReport
	calls  int
}

func (f *fakeIndexerIntegrityReader) CheckCriticalIndexerIntegrity(context.Context, bool) (*pgindex.IndexerIntegrityReport, error) {
	f.calls++
	return f.report, nil
}

func TestIndexerIntegrityGuardBlocksScheduledStagesOnCriticalFailure(t *testing.T) {
	repo := &fakeIndexerIntegrityReader{
		report: &pgindex.IndexerIntegrityReport{Checks: []pgindex.IndexerIntegrityCheck{{
			Relation: "public.article_headers_newsgroup_id_message_id_key",
			OK:       false,
			Detail:   "high key invariant violated",
		}}},
	}
	guard := newIndexerIntegrityGuard(repo)

	decision, err := guard(context.Background(), supervisor.Stage{Name: supervisor.StageRecoverYEnc}, "scheduled")
	if err != nil {
		t.Fatalf("guard returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected stage to be blocked")
	}
	if !strings.Contains(decision.Reason, "critical index integrity failed") {
		t.Fatalf("expected integrity failure reason, got %q", decision.Reason)
	}
	if repo.calls != 1 {
		t.Fatalf("expected one integrity check, got %d", repo.calls)
	}
}

func TestIndexerIntegrityGuardAllowsManualRunsForRepair(t *testing.T) {
	repo := &fakeIndexerIntegrityReader{
		report: &pgindex.IndexerIntegrityReport{Checks: []pgindex.IndexerIntegrityCheck{{
			Relation: "public.article_headers_newsgroup_id_message_id_key",
			OK:       false,
			Detail:   "high key invariant violated",
		}}},
	}
	guard := newIndexerIntegrityGuard(repo)

	decision, err := guard(context.Background(), supervisor.Stage{Name: supervisor.StageMaintenance}, "manual")
	if err != nil {
		t.Fatalf("guard returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected manual run to bypass integrity gate, got %+v", decision)
	}
	if repo.calls != 0 {
		t.Fatalf("manual bypass should not run integrity check, got %d calls", repo.calls)
	}
}

func TestIndexerIntegrityGuardCachesDecision(t *testing.T) {
	repo := &fakeIndexerIntegrityReader{
		report: &pgindex.IndexerIntegrityReport{Checks: []pgindex.IndexerIntegrityCheck{{
			Relation: "public.article_headers_newsgroup_id_message_id_key",
			OK:       true,
			Detail:   "amcheck passed",
		}}},
	}
	guard := newIndexerIntegrityGuard(repo)

	for i := 0; i < 2; i++ {
		decision, err := guard(context.Background(), supervisor.Stage{Name: supervisor.StageRelease}, "scheduled")
		if err != nil {
			t.Fatalf("guard returned error: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("expected stage allowed, got %+v", decision)
		}
	}
	if repo.calls != 1 {
		t.Fatalf("expected cached integrity decision, got %d checks", repo.calls)
	}
}
