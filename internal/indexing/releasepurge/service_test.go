package releasepurge

import (
	"context"
	"testing"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestRunOnceAggregatesPurgeMetrics(t *testing.T) {
	svc := NewService(&purgeRepoStub{
		candidates: []pgindex.ReleasePurgeCandidate{{ReleaseID: "rel-1"}},
		result: &pgindex.ReleasePurgeResult{
			ReleaseID:               "rel-1",
			SkippedSharedBinaryRows: 3,
			DeletedRowsByTable: map[string]int64{
				"binaries":                       2,
				"article_header_ingest_payloads": 5,
			},
		},
	}, Options{BatchSize: 1})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("run once: %v", err)
	}

	if got := metrics["purged_count"]; got != 1 {
		t.Fatalf("purged_count=%v want 1", got)
	}
	if got := metrics["skipped_shared_lineage_rows"]; got != int64(3) {
		t.Fatalf("skipped_shared_lineage_rows=%v want 3", got)
	}
	rowsDeleted := metrics["rows_deleted_by_table"].(map[string]int64)
	if rowsDeleted["binaries"] != 2 || rowsDeleted["article_header_ingest_payloads"] != 5 {
		t.Fatalf("unexpected rows_deleted_by_table=%v", rowsDeleted)
	}
}

type purgeRepoStub struct {
	candidates []pgindex.ReleasePurgeCandidate
	result     *pgindex.ReleasePurgeResult
}

func (s *purgeRepoStub) ClaimReleasePurgeCandidates(context.Context, int) ([]pgindex.ReleasePurgeCandidate, error) {
	return s.candidates, nil
}

func (s *purgeRepoStub) PurgeArchivedReleaseSources(context.Context, string) (*pgindex.ReleasePurgeResult, error) {
	return s.result, nil
}
