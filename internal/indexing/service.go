package indexing

import (
	"context"
	"fmt"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/supervisor"
)

type Service struct {
	supervisor    *supervisor.Supervisor
	releaseReform func(ctx context.Context) error
}

type Options struct {
	ReleaseReform func(ctx context.Context) error
}

func NewService(supervisorSvc *supervisor.Supervisor, opts ...Options) *Service {
	var cfg Options
	if len(opts) > 0 {
		cfg = opts[0]
	}
	return &Service{
		supervisor:    supervisorSvc,
		releaseReform: cfg.ReleaseReform,
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

func (s *Service) ReformReleasesOnce(ctx context.Context) error {
	if s.releaseReform == nil {
		return fmt.Errorf("release reform service is not configured")
	}
	return s.releaseReform(ctx)
}

func (s *Service) AssembleOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageAssemble)
}

func (s *Service) InspectOnce(ctx context.Context) error {
	return s.runStagesOnce(
		ctx,
		supervisor.StageInspectPAR2,
		supervisor.StageInspectNFO,
		supervisor.StageInspectArchive,
		supervisor.StageInspectPassword,
		supervisor.StageInspectMedia,
	)
}

func (s *Service) InspectPAR2Once(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageInspectPAR2)
}

func (s *Service) InspectNFOOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageInspectNFO)
}

func (s *Service) InspectArchiveOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageInspectArchive)
}

func (s *Service) InspectPasswordOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageInspectPassword)
}

func (s *Service) InspectMediaOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageInspectMedia)
}

func (s *Service) EnrichPredbOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageEnrichPreDB)
}

func (s *Service) EnrichTMDBOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageEnrichTMDB)
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
