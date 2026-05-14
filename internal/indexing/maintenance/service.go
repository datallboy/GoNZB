package maintenance

import (
	"context"
	"fmt"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type repository interface {
	RunIndexerMaintenance(ctx context.Context) (*pgindex.IndexerMaintenanceResult, error)
}

type Service struct {
	repo repository
	log  logger
}

func NewService(repo repository, log logger) *Service {
	return &Service{repo: repo, log: log}
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("maintenance repo is required")
	}

	out, err := s.repo.RunIndexerMaintenance(ctx)
	if err != nil {
		return nil, err
	}
	metrics := map[string]any{}
	if s.log != nil && out != nil {
		s.log.Info(
			"indexer maintenance: abandoned_stage_runs=%d cleared_stage_leases=%d abandoned_scrape_runs=%d abandoned_binary_inspections=%d purged_stage_runs=%d purged_scrape_runs=%d purged_binary_inspections=%d purged_header_payloads=%d purged_readiness_summaries=%d purged_orphan_releases=%d",
			out.AbandonedStageRuns,
			out.ClearedStageLeases,
			out.AbandonedScrapeRuns,
			out.AbandonedBinaryInspections,
			out.PurgedStageRuns,
			out.PurgedScrapeRuns,
			out.PurgedBinaryInspections,
			out.PurgedHeaderPayloads,
			out.PurgedReadinessSummaries,
			out.PurgedOrphanReleases,
		)
	}
	if out != nil {
		metrics["abandoned_stage_runs"] = out.AbandonedStageRuns
		metrics["cleared_stage_leases"] = out.ClearedStageLeases
		metrics["abandoned_scrape_runs"] = out.AbandonedScrapeRuns
		metrics["abandoned_binary_inspections"] = out.AbandonedBinaryInspections
		metrics["purged_stage_runs"] = out.PurgedStageRuns
		metrics["purged_scrape_runs"] = out.PurgedScrapeRuns
		metrics["purged_binary_inspections"] = out.PurgedBinaryInspections
		metrics["purged_header_payloads"] = out.PurgedHeaderPayloads
		metrics["purged_readiness_summaries"] = out.PurgedReadinessSummaries
		metrics["purged_orphan_releases"] = out.PurgedOrphanReleases
	}
	return metrics, nil
}
