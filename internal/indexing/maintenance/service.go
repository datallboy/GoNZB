package maintenance

import (
	"context"
	"fmt"

	inspectpkg "github.com/datallboy/gonzb/internal/indexing/inspect"
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
	repo             repository
	log              logger
	workspaceCleanup func(context.Context) (int, error)
}

func NewService(repo repository, log logger, workspaceCleanup func(context.Context) (int, error)) *Service {
	return &Service{repo: repo, log: log, workspaceCleanup: workspaceCleanup}
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
	metrics["purged_inspect_workspaces"] = 0
	if s.workspaceCleanup != nil {
		cleaned, err := s.workspaceCleanup(ctx)
		if err != nil {
			return nil, fmt.Errorf("cleanup stale inspect workspaces: %w", err)
		}
		metrics["purged_inspect_workspaces"] = cleaned
		if s.log != nil && cleaned > 0 {
			s.log.Info("indexer maintenance: purged_inspect_workspaces=%d ttl=%s", cleaned, inspectpkg.WorkspaceStaleTTL)
		}
	}
	if s.log != nil && out != nil {
		s.log.Info(
			"indexer maintenance: abandoned_stage_runs=%d cleared_stage_leases=%d abandoned_scrape_runs=%d abandoned_binary_inspections=%d repaired_binary_observations=%d yenc_work_items_upserted=%d yenc_work_items_retired=%d inspect_discovery_ready_upserted=%d inspect_discovery_ready_retired=%d inspect_discovery_ready_requeued=%d inspect_par2_ready_upserted=%d inspect_par2_ready_retired=%d inspect_par2_ready_requeued=%d inspect_archive_ready_upserted=%d inspect_archive_ready_retired=%d inspect_archive_ready_requeued=%d inspect_media_ready_upserted=%d inspect_media_ready_retired=%d inspect_media_ready_requeued=%d backfilled_catalog_files=%d purged_stage_runs=%d purged_scrape_runs=%d purged_binary_inspections=%d purged_header_payloads=%d purged_grouping_evidence=%d purged_readiness_summaries=%d purged_orphan_releases=%d skipped_readiness_cleanup=%t refresh_queue_backlog=%d purged_inspect_workspaces=%d",
			out.AbandonedStageRuns,
			out.ClearedStageLeases,
			out.AbandonedScrapeRuns,
			out.AbandonedBinaryInspections,
			out.RepairedBinaryObservations,
			out.YEncWorkItemsUpserted,
			out.YEncWorkItemsRetired,
			out.InspectDiscoveryReadyRows,
			out.InspectDiscoveryRetired,
			out.InspectDiscoveryRequeued,
			out.InspectPAR2ReadyRows,
			out.InspectPAR2Retired,
			out.InspectPAR2Requeued,
			out.InspectArchiveReadyRows,
			out.InspectArchiveRetired,
			out.InspectArchiveRequeued,
			out.InspectMediaReadyRows,
			out.InspectMediaRetired,
			out.InspectMediaRequeued,
			out.BackfilledCatalogFiles,
			out.PurgedStageRuns,
			out.PurgedScrapeRuns,
			out.PurgedBinaryInspections,
			out.PurgedHeaderPayloads,
			out.PurgedGroupingEvidence,
			out.PurgedReadinessSummaries,
			out.PurgedOrphanReleases,
			out.SkippedReadinessCleanup,
			out.RefreshQueueBacklog,
			metrics["purged_inspect_workspaces"],
		)
	}
	if out != nil {
		metrics["abandoned_stage_runs"] = out.AbandonedStageRuns
		metrics["cleared_stage_leases"] = out.ClearedStageLeases
		metrics["abandoned_scrape_runs"] = out.AbandonedScrapeRuns
		metrics["abandoned_binary_inspections"] = out.AbandonedBinaryInspections
		metrics["repaired_binary_observations"] = out.RepairedBinaryObservations
		metrics["yenc_work_items_upserted"] = out.YEncWorkItemsUpserted
		metrics["yenc_work_items_retired"] = out.YEncWorkItemsRetired
		metrics["inspect_discovery_ready_upserted"] = out.InspectDiscoveryReadyRows
		metrics["inspect_discovery_ready_retired"] = out.InspectDiscoveryRetired
		metrics["inspect_discovery_ready_requeued"] = out.InspectDiscoveryRequeued
		metrics["inspect_par2_ready_upserted"] = out.InspectPAR2ReadyRows
		metrics["inspect_par2_ready_retired"] = out.InspectPAR2Retired
		metrics["inspect_par2_ready_requeued"] = out.InspectPAR2Requeued
		metrics["inspect_archive_ready_upserted"] = out.InspectArchiveReadyRows
		metrics["inspect_archive_ready_retired"] = out.InspectArchiveRetired
		metrics["inspect_archive_ready_requeued"] = out.InspectArchiveRequeued
		metrics["inspect_media_ready_upserted"] = out.InspectMediaReadyRows
		metrics["inspect_media_ready_retired"] = out.InspectMediaRetired
		metrics["inspect_media_ready_requeued"] = out.InspectMediaRequeued
		metrics["backfilled_catalog_files"] = out.BackfilledCatalogFiles
		metrics["purged_stage_runs"] = out.PurgedStageRuns
		metrics["purged_scrape_runs"] = out.PurgedScrapeRuns
		metrics["purged_binary_inspections"] = out.PurgedBinaryInspections
		metrics["purged_header_payloads"] = out.PurgedHeaderPayloads
		metrics["purged_grouping_evidence"] = out.PurgedGroupingEvidence
		metrics["purged_readiness_summaries"] = out.PurgedReadinessSummaries
		metrics["purged_orphan_releases"] = out.PurgedOrphanReleases
		metrics["skipped_readiness_cleanup"] = out.SkippedReadinessCleanup
		metrics["refresh_queue_backlog"] = out.RefreshQueueBacklog
	}
	return metrics, nil
}
