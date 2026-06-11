package releasepurge

import (
	"context"
	"fmt"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type repository interface {
	ClaimReleasePurgeCandidates(ctx context.Context, limit int, policy pgindex.ReleaseReadyPolicy) ([]pgindex.ReleasePurgeCandidate, error)
	PurgeArchivedReleaseSources(ctx context.Context, releaseID string) (*pgindex.ReleasePurgeResult, error)
}

type Options struct {
	BatchSize int
	Policy    pgindex.ReleaseReadyPolicy
}

type Service struct {
	repo repository
	opts Options
}

func NewService(repo repository, opts Options) *Service {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 50
	}
	opts.Policy = pgindex.NormalizeReleaseReadyPolicy(opts.Policy)
	return &Service{repo: repo, opts: opts}
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("purge repo is required")
	}

	candidates, err := s.repo.ClaimReleasePurgeCandidates(ctx, s.opts.BatchSize, s.opts.Policy)
	if err != nil {
		return nil, err
	}

	metrics := map[string]any{
		"purge_candidates":            len(candidates),
		"purged_count":                0,
		"skipped_shared_lineage_rows": int64(0),
		"rows_deleted_by_table":       map[string]int64{},
	}
	rowsDeleted := metrics["rows_deleted_by_table"].(map[string]int64)

	for _, candidate := range candidates {
		result, err := s.repo.PurgeArchivedReleaseSources(ctx, candidate.ReleaseID)
		if err != nil {
			return metrics, err
		}
		if result == nil {
			continue
		}
		metrics["purged_count"] = metrics["purged_count"].(int) + 1
		metrics["skipped_shared_lineage_rows"] = metrics["skipped_shared_lineage_rows"].(int64) + result.SkippedSharedBinaryRows
		for table, count := range result.DeletedRowsByTable {
			rowsDeleted[table] += count
		}
	}

	return metrics, nil
}
