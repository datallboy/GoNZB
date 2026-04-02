package tmdb

import (
	"context"
	"fmt"

	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type repository interface {
	ListReleaseEnrichmentCandidates(ctx context.Context, stageName string, limit int) ([]pgindex.ReleaseEnrichmentCandidate, error)
}

type Service struct {
	repo  repository
	log   logger
	limit int
}

func NewService(repo repository, log logger, limit int) *Service {
	if limit <= 0 {
		limit = 100
	}
	return &Service{repo: repo, log: log, limit: limit}
}

func (s *Service) RunOnce(ctx context.Context) error {
	candidates, err := s.repo.ListReleaseEnrichmentCandidates(ctx, string(supervisor.StageEnrichTMDB), s.limit)
	if err != nil {
		return fmt.Errorf("list enrich_tmdb candidates: %w", err)
	}
	if len(candidates) == 0 {
		if s != nil && s.log != nil {
			s.log.Debug("enrich_tmdb: no enrichment candidates available")
		}
		return nil
	}
	if s != nil && s.log != nil {
		s.log.Info("enrich_tmdb: candidates=%d (external matching still deferred)", len(candidates))
	}
	return nil
}
