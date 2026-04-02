package indexing

import (
	"context"
	"fmt"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/supervisor"
)

type Service struct {
	supervisor *supervisor.Supervisor
}

func NewService(supervisorSvc *supervisor.Supervisor) *Service {
	return &Service{
		supervisor: supervisorSvc,
	}
}

// backward-compatible alias to latest mode.
func (s *Service) ScrapeOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageScrapeLatest)
}

// explicit latest scrape mode
func (s *Service) ScrapeLatestOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageScrapeLatest)
}

// explicit backfill scrape mode
func (s *Service) ScrapeBackfillOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageScrapeBackfill)
}

func (s *Service) Start(ctx context.Context, interval time.Duration) error {
	_ = interval
	if s.supervisor == nil {
		return fmt.Errorf("supervisor service is not configured")
	}
	return s.supervisor.Run(ctx)
}

func (s *Service) RunPipelineOnce(ctx context.Context) error {
	return s.runStagesOnce(
		ctx,
		supervisor.StageScrapeLatest,
		supervisor.StageAssemble,
		supervisor.StageRelease,
	)
}

func (s *Service) ReleaseOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageRelease)
}

func (s *Service) AssembleOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageAssemble)
}

func (s *Service) RunStageOnce(ctx context.Context, stageName string) error {
	return s.runStageOnce(ctx, supervisor.StageName(stageName))
}

func (s *Service) runStageOnce(ctx context.Context, stageName supervisor.StageName) error {
	if s.supervisor == nil {
		return fmt.Errorf("supervisor service is not configured")
	}
	return s.supervisor.RunStageOnce(ctx, stageName)
}

func (s *Service) runStagesOnce(ctx context.Context, stageNames ...supervisor.StageName) error {
	if s.supervisor == nil {
		return fmt.Errorf("supervisor service is not configured")
	}
	return s.supervisor.RunStagesOnce(ctx, stageNames...)
}
