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
	if s.repo == nil {
		return fmt.Errorf("maintenance repo is required")
	}

	out, err := s.repo.RunIndexerMaintenance(ctx)
	if err != nil {
		return err
	}
	if s.log != nil && out != nil {
		s.log.Info(
			"indexer maintenance: abandoned_stage_runs=%d cleared_stage_leases=%d abandoned_scrape_runs=%d abandoned_binary_inspections=%d purged_stage_runs=%d purged_scrape_runs=%d purged_binary_inspections=%d purged_header_payloads=%d purged_orphan_releases=%d",
			out.AbandonedStageRuns,
			out.ClearedStageLeases,
			out.AbandonedScrapeRuns,
			out.AbandonedBinaryInspections,
			out.PurgedStageRuns,
			out.PurgedScrapeRuns,
			out.PurgedBinaryInspections,
			out.PurgedHeaderPayloads,
			out.PurgedOrphanReleases,
		)
	}
	return nil
}
