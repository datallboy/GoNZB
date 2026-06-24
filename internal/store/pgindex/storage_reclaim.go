package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

var indexerStorageReclaimTableOrder = []string{
	"release_family_readiness_summaries",
	"binary_grouping_evidence",
	"poster_materialization_queue",
	"binary_inspection_ready_queue",
	"article_header_assembly_queue",
	"indexer_stage_runs",
	"scrape_runs",
	"article_header_crosspost_groups",
	"yenc_recovery_work_items",
	"binary_core",
	"binary_parts",
	"binary_identity_current",
	"binary_observation_stats",
	"binary_recovery_current",
	"binary_inspections",
	"binary_inspection_artifacts",
	"binary_archive_entries",
	"binary_text_evidence",
	"binary_media_streams",
	"binary_par2_sets",
	"binary_par2_targets",
	"binary_completion_keys",
	"binary_lifecycle",
	"binary_projection_events",
	"binary_superseded_sources",
	"article_headers",
	"article_header_ingest_payloads",
	"article_header_poster_refs",
}

var indexerStorageReclaimTableAliases = map[string]string{
	"readiness":                          "release_family_readiness_summaries",
	"readiness-summaries":                "release_family_readiness_summaries",
	"release_family_readiness_summaries": "release_family_readiness_summaries",
	"grouping-evidence":                  "binary_grouping_evidence",
	"binary_grouping_evidence":           "binary_grouping_evidence",
	"poster-queue":                       "poster_materialization_queue",
	"poster_materialization_queue":       "poster_materialization_queue",
	"inspect-ready-queue":                "binary_inspection_ready_queue",
	"binary_inspection_ready_queue":      "binary_inspection_ready_queue",
	"assembly-queue":                     "article_header_assembly_queue",
	"article_header_assembly_queue":      "article_header_assembly_queue",
	"stage-runs":                         "indexer_stage_runs",
	"indexer_stage_runs":                 "indexer_stage_runs",
	"scrape-runs":                        "scrape_runs",
	"scrape_runs":                        "scrape_runs",
	"crosspost-groups":                   "article_header_crosspost_groups",
	"article_header_crosspost_groups":    "article_header_crosspost_groups",
	"yenc-work":                          "yenc_recovery_work_items",
	"yenc_recovery_work_items":           "yenc_recovery_work_items",
	"binary-core":                        "binary_core",
	"binary_core":                        "binary_core",
	"binary-parts":                       "binary_parts",
	"binary_parts":                       "binary_parts",
	"binary-identity":                    "binary_identity_current",
	"binary_identity_current":            "binary_identity_current",
	"binary-observation-stats":           "binary_observation_stats",
	"binary_observation_stats":           "binary_observation_stats",
	"binary-recovery":                    "binary_recovery_current",
	"binary_recovery_current":            "binary_recovery_current",
	"binary-inspections":                 "binary_inspections",
	"binary_inspections":                 "binary_inspections",
	"binary-inspection-artifacts":        "binary_inspection_artifacts",
	"binary_inspection_artifacts":        "binary_inspection_artifacts",
	"binary-archive-entries":             "binary_archive_entries",
	"binary_archive_entries":             "binary_archive_entries",
	"binary-text-evidence":               "binary_text_evidence",
	"binary_text_evidence":               "binary_text_evidence",
	"binary-media-streams":               "binary_media_streams",
	"binary_media_streams":               "binary_media_streams",
	"binary-par2-sets":                   "binary_par2_sets",
	"binary_par2_sets":                   "binary_par2_sets",
	"binary-par2-targets":                "binary_par2_targets",
	"binary_par2_targets":                "binary_par2_targets",
	"binary-completion-keys":             "binary_completion_keys",
	"binary_completion_keys":             "binary_completion_keys",
	"binary-lifecycle":                   "binary_lifecycle",
	"binary_lifecycle":                   "binary_lifecycle",
	"binary-projection-events":           "binary_projection_events",
	"binary_projection_events":           "binary_projection_events",
	"binary-superseded-sources":          "binary_superseded_sources",
	"binary_superseded_sources":          "binary_superseded_sources",
	"headers":                            "article_headers",
	"article-headers":                    "article_headers",
	"article_headers":                    "article_headers",
	"payloads":                           "article_header_ingest_payloads",
	"article-header-ingest-payloads":     "article_header_ingest_payloads",
	"article_header_ingest_payloads":     "article_header_ingest_payloads",
	"article-header-poster-refs":         "article_header_poster_refs",
	"article_header_poster_refs":         "article_header_poster_refs",
}

type IndexerStorageReclaimOptions struct {
	Tables    []string
	Full      bool
	CheckOnly bool
}

type IndexerStorageReclaimTableResult struct {
	Table       string
	BeforeBytes int64
	AfterBytes  int64
}

type IndexerStorageReclaimResult struct {
	Mode   string
	Tables []IndexerStorageReclaimTableResult
}

func normalizeIndexerStorageReclaimTables(values []string) ([]string, error) {
	if len(values) == 0 {
		return append([]string(nil), indexerStorageReclaimTableOrder...), nil
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value))
		table, ok := indexerStorageReclaimTableAliases[key]
		if !ok {
			return nil, fmt.Errorf("unsupported reclaim table %q", value)
		}
		if _, exists := seen[table]; exists {
			continue
		}
		seen[table] = struct{}{}
		normalized = append(normalized, table)
	}

	slices.SortStableFunc(normalized, func(left, right string) int {
		return slices.Index(indexerStorageReclaimTableOrder, left) - slices.Index(indexerStorageReclaimTableOrder, right)
	})
	return normalized, nil
}

func indexerStorageReclaimStatement(table string, full bool) (string, error) {
	if !slices.Contains(indexerStorageReclaimTableOrder, table) {
		return "", fmt.Errorf("unsupported reclaim table %q", table)
	}
	if full {
		return fmt.Sprintf("VACUUM (FULL, ANALYZE) %s", quoteIdentifier(table)), nil
	}
	return fmt.Sprintf("VACUUM (ANALYZE) %s", quoteIdentifier(table)), nil
}

func (s *Store) RunIndexerStorageReclaim(ctx context.Context, options IndexerStorageReclaimOptions) (*IndexerStorageReclaimResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}

	tables, err := normalizeIndexerStorageReclaimTables(options.Tables)
	if err != nil {
		return nil, err
	}

	mode := "analyze"
	if options.CheckOnly {
		mode = "check"
	} else if options.Full {
		mode = "full"
	}

	var conn *sql.Conn
	if !options.CheckOnly {
		dbConn, err := s.db.Conn(ctx)
		if err != nil {
			return nil, fmt.Errorf("open reclaim connection: %w", err)
		}
		defer dbConn.Close()
		conn = dbConn
	}

	result := &IndexerStorageReclaimResult{
		Mode:   mode,
		Tables: make([]IndexerStorageReclaimTableResult, 0, len(tables)),
	}

	for _, table := range tables {
		beforeBytes, err := s.tableTotalBytes(ctx, table)
		if err != nil {
			return nil, err
		}

		if !options.CheckOnly {
			statement, err := indexerStorageReclaimStatement(table, options.Full)
			if err != nil {
				return nil, err
			}
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return nil, fmt.Errorf("run %s for %s: %w", mode, table, err)
			}
		}

		afterBytes, err := s.tableTotalBytes(ctx, table)
		if err != nil {
			return nil, err
		}

		result.Tables = append(result.Tables, IndexerStorageReclaimTableResult{
			Table:       table,
			BeforeBytes: beforeBytes,
			AfterBytes:  afterBytes,
		})
	}

	return result, nil
}
