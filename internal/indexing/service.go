package indexing

import (
	"context"
	"fmt"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
)

type Service struct {
	supervisor              *supervisor.Supervisor
	assembleLaneA           func(ctx context.Context) error
	assembleLaneB           func(ctx context.Context) error
	recoverYEnc             func(ctx context.Context) error
	releaseReform           func(ctx context.Context) error
	enrichPredbSceneName    func(ctx context.Context) error
	enrichPredbMetadataOnly func(ctx context.Context) error
	enrichPredbSyncFeed     func(ctx context.Context) error
	enrichPredbSyncBackfill func(ctx context.Context) error
	nntpStats               func() app.NNTPRuntimeStats
}

type Options struct {
	ReleaseReform           func(ctx context.Context) error
	AssembleLaneA           func(ctx context.Context) error
	AssembleLaneB           func(ctx context.Context) error
	RecoverYEnc             func(ctx context.Context) error
	EnrichPredbSceneName    func(ctx context.Context) error
	EnrichPredbMetadataOnly func(ctx context.Context) error
	EnrichPredbSyncFeed     func(ctx context.Context) error
	EnrichPredbSyncBackfill func(ctx context.Context) error
	NNTPStats               func() app.NNTPRuntimeStats
}

func NewService(supervisorSvc *supervisor.Supervisor, opts ...Options) *Service {
	var cfg Options
	if len(opts) > 0 {
		cfg = opts[0]
	}
	return &Service{
		supervisor:              supervisorSvc,
		assembleLaneA:           cfg.AssembleLaneA,
		assembleLaneB:           cfg.AssembleLaneB,
		recoverYEnc:             cfg.RecoverYEnc,
		releaseReform:           cfg.ReleaseReform,
		enrichPredbSceneName:    cfg.EnrichPredbSceneName,
		enrichPredbMetadataOnly: cfg.EnrichPredbMetadataOnly,
		enrichPredbSyncFeed:     cfg.EnrichPredbSyncFeed,
		enrichPredbSyncBackfill: cfg.EnrichPredbSyncBackfill,
		nntpStats:               cfg.NNTPStats,
	}
}

func (s *Service) NNTPStats(ctx context.Context) (*app.NNTPRuntimeStats, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.nntpStats == nil {
		return nil, fmt.Errorf("indexer nntp manager is not configured")
	}
	stats := s.nntpStats()
	return &stats, nil
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

func (s *Service) AssembleLaneAOnce(ctx context.Context) error {
	if s.assembleLaneA == nil {
		return fmt.Errorf("assemble lane A service is not configured")
	}
	return s.assembleLaneA(ctx)
}

func (s *Service) AssembleLaneBOnce(ctx context.Context) error {
	if s.assembleLaneB == nil {
		return fmt.Errorf("assemble lane B service is not configured")
	}
	return s.assembleLaneB(ctx)
}

func (s *Service) RecoverYEncOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageRecoverYEnc)
}

func (s *Service) InspectOnce(ctx context.Context) error {
	return s.runStagesOnce(
		ctx,
		supervisor.StageInspectDiscovery,
		supervisor.StageInspectPAR2,
		supervisor.StageInspectNFO,
		supervisor.StageInspectArchive,
		supervisor.StageInspectPassword,
		supervisor.StageInspectMedia,
	)
}

func (s *Service) InspectDiscoveryOnce(ctx context.Context) error {
	return s.runStageOnce(ctx, supervisor.StageInspectDiscovery)
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

func (s *Service) EnrichPredbSceneNameRecoveryOnce(ctx context.Context) error {
	if s.enrichPredbSceneName == nil {
		return fmt.Errorf("predb scene-name-recovery service is not configured")
	}
	return s.enrichPredbSceneName(ctx)
}

func (s *Service) EnrichPredbMetadataFallbackOnce(ctx context.Context) error {
	if s.enrichPredbMetadataOnly == nil {
		return fmt.Errorf("predb metadata-only-fallback service is not configured")
	}
	return s.enrichPredbMetadataOnly(ctx)
}

func (s *Service) EnrichPredbSyncFeedOnce(ctx context.Context) error {
	if s.enrichPredbSyncFeed == nil {
		return fmt.Errorf("predb sync-feed service is not configured")
	}
	return s.enrichPredbSyncFeed(ctx)
}

func (s *Service) EnrichPredbSyncBackfillOnce(ctx context.Context) error {
	if s.enrichPredbSyncBackfill == nil {
		return fmt.Errorf("predb sync-backfill service is not configured")
	}
	return s.enrichPredbSyncBackfill(ctx)
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
