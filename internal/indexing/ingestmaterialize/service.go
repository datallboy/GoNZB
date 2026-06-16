package ingestmaterialize

import (
	"context"
	"fmt"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type Repository interface {
	MaterializeArticleHeaderPosters(ctx context.Context, limit int) (*pgindex.IndexerPosterMaterializationResult, error)
	RefreshCrosspostPopularity(ctx context.Context, limit int) (*pgindex.IndexerCrosspostPopularityRefreshResult, error)
}

type Options struct {
	BatchSize int
}

type Service struct {
	repo      Repository
	batchSize int
}

func NewService(repo Repository, opts Options) *Service {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 10000
	}
	return &Service{repo: repo, batchSize: opts.BatchSize}
}

func (s *Service) RunPostersOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("ingest materializer repo is required")
	}
	out, err := s.repo.MaterializeArticleHeaderPosters(ctx, s.batchSize)
	if err != nil {
		return map[string]any{"batch_size": s.batchSize}, err
	}
	return map[string]any{
		"batch_size":    s.batchSize,
		"claimed":       out.Claimed,
		"posters":       out.Posters,
		"refs_upserted": out.RefsUpserted,
	}, nil
}

func (s *Service) RunCrosspostPopularityOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("ingest materializer repo is required")
	}
	out, err := s.repo.RefreshCrosspostPopularity(ctx, s.batchSize)
	if err != nil {
		return map[string]any{"batch_size": s.batchSize}, err
	}
	return map[string]any{
		"batch_size":                 s.batchSize,
		"claimed":                    out.Claimed,
		"groups_refreshed":           out.GroupsRefreshed,
		"distinct_messages_observed": out.DistinctMessagesObserved,
		"distinct_sources_observed":  out.DistinctSourcesObserved,
	}, nil
}
