package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

const staleNonreleaseSourceRetentionDays = 7

type RawStageRetentionPolicy struct {
	HotHours         int
	WarmHours        int
	ColdHours        int
	FailedProbeHours int
	DoneYEncHours    int
}

type MaintenanceTaskResult struct {
	TaskKey              string                          `json:"task_key"`
	DryRun               bool                            `json:"dry_run"`
	EstimatedRowsByTable map[string]int64                `json:"estimated_rows_by_table,omitempty"`
	DeletedRowsByTable   map[string]int64                `json:"deleted_rows_by_table,omitempty"`
	VacuumedTables       []string                        `json:"vacuumed_tables,omitempty"`
	EstimatedBytes       int64                           `json:"estimated_bytes,omitempty"`
	BeforeStorage        *MaintenanceTaskStorageSnapshot `json:"before_storage,omitempty"`
	AfterStorage         *MaintenanceTaskStorageSnapshot `json:"after_storage,omitempty"`
	Blockers             []string                        `json:"blockers,omitempty"`
	Warnings             []string                        `json:"warnings,omitempty"`
}

type MaintenanceTaskStorageSnapshot struct {
	GeneratedAt            time.Time        `json:"generated_at"`
	DatabaseBytes          int64            `json:"database_bytes"`
	DataDirectory          string           `json:"data_directory,omitempty"`
	FilesystemFreeBytes    int64            `json:"filesystem_free_bytes,omitempty"`
	FilesystemTotalBytes   int64            `json:"filesystem_total_bytes,omitempty"`
	FilesystemFreePercent  float64          `json:"filesystem_free_percent,omitempty"`
	FilesystemVisible      bool             `json:"filesystem_visible"`
	TableTotalBytesByTable map[string]int64 `json:"table_total_bytes_by_table,omitempty"`
	TableLiveRowsByTable   map[string]int64 `json:"table_live_rows_by_table,omitempty"`
	TableDeadRowsByTable   map[string]int64 `json:"table_dead_rows_by_table,omitempty"`
}

type maintenanceVacuumCandidate struct {
	TableName  string
	LiveRows   int64
	DeadRows   int64
	TotalBytes int64
	DeadPct    float64
}

type partitionRetentionCandidate struct {
	Day         time.Time
	TableName   string
	ChildTable  string
	RowEstimate int64
}

func (s *Store) DryRunReleaseSourcePurge(ctx context.Context, limit int, policy ReleaseReadyPolicy) (*MaintenanceTaskResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	candidates, err := s.ClaimReleasePurgeCandidates(ctx, limit, policy)
	if err != nil {
		return nil, err
	}
	result := &MaintenanceTaskResult{
		TaskKey:              "release_source_purge",
		DryRun:               true,
		EstimatedRowsByTable: map[string]int64{"release_archive_state": int64(len(candidates))},
		Warnings:             []string{"article headers are only purgeable when no remaining binary parts reference them"},
	}
	for _, candidate := range candidates {
		estimate, err := s.estimateArchivedReleaseSourcePurge(ctx, candidate.ReleaseID)
		if err != nil {
			result.Blockers = append(result.Blockers, err.Error())
			continue
		}
		for table, count := range estimate.DeletedRowsByTable {
			result.EstimatedRowsByTable[table] += count
		}
	}
	return result, nil
}

func (s *Store) RunReleaseSourcePurge(ctx context.Context, limit int, policy ReleaseReadyPolicy) (*MaintenanceTaskResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	candidates, err := s.ClaimReleasePurgeCandidates(ctx, limit, policy)
	if err != nil {
		return nil, err
	}
	result := &MaintenanceTaskResult{
		TaskKey:            "release_source_purge",
		DryRun:             false,
		DeletedRowsByTable: map[string]int64{"release_archive_state": 0},
		Warnings:           []string{"article headers are only purged when no remaining binary parts reference them"},
	}
	for _, candidate := range candidates {
		purged, err := s.PurgeArchivedReleaseSources(ctx, candidate.ReleaseID)
		if err != nil {
			result.Blockers = append(result.Blockers, err.Error())
			continue
		}
		if purged == nil {
			continue
		}
		result.DeletedRowsByTable["release_archive_state"]++
		for table, count := range purged.DeletedRowsByTable {
			result.DeletedRowsByTable[table] += count
		}
	}
	s.vacuumDeletedMaintenanceTables(ctx, result)
	return result, nil
}

func (s *Store) estimateArchivedReleaseSourcePurge(ctx context.Context, releaseID string) (*ReleasePurgeResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin purge dry-run tx: %w", err)
	}
	defer rollbackTx(tx)

	result := &ReleasePurgeResult{
		ReleaseID:          releaseID,
		DeletedRowsByTable: map[string]int64{},
	}
	if err := estimateArchivedReleaseSourcesTx(ctx, tx, releaseID, result); err != nil {
		return nil, err
	}
	return result, nil
}

func estimateArchivedReleaseSourcesTx(ctx context.Context, tx *sql.Tx, releaseID string, result *ReleasePurgeResult) error {
	preflight, err := loadReleasePurgePreflight(ctx, tx, releaseID, false)
	if err != nil {
		return err
	}
	if preflight.ArchiveStatus != "purge_pending" {
		return fmt.Errorf("release %s is not purge_pending", releaseID)
	}
	if preflight.ObjectKey == "" || !preflight.ReleaseExists || !preflight.HasCatalogFiles || !preflight.HasCompletedMediaInspect {
		return fmt.Errorf("release %s does not satisfy purge preflight", releaseID)
	}

	var totalLineageBinaries int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM release_archive_lineage_binaries WHERE release_id = $1`, releaseID).Scan(&totalLineageBinaries); err != nil {
		return fmt.Errorf("count release lineage binaries %s: %w", releaseID, err)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT lb.binary_id
		FROM release_archive_lineage_binaries lb
		WHERE lb.release_id = $1
		  AND NOT EXISTS (
			SELECT 1
			FROM release_files other_rf
			LEFT JOIN release_archive_state other_ras ON other_ras.release_id = other_rf.release_id
			WHERE other_rf.binary_id = lb.binary_id
			  AND other_rf.release_id <> $1
			  AND COALESCE(other_ras.archive_status, 'active') NOT IN ('archived', 'purge_pending', 'purged')
		  )`, releaseID)
	if err != nil {
		return fmt.Errorf("list purgeable binaries %s: %w", releaseID, err)
	}
	defer rows.Close()
	binaryIDs := make([]int64, 0, 64)
	for rows.Next() {
		var binaryID int64
		if err := rows.Scan(&binaryID); err != nil {
			return fmt.Errorf("scan purgeable binary id: %w", err)
		}
		binaryIDs = append(binaryIDs, binaryID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate purgeable binary ids: %w", err)
	}
	result.SkippedSharedBinaryRows = totalLineageBinaries - int64(len(binaryIDs))
	if len(binaryIDs) > 0 {
		if err := stageReleasePurgeBinaryIDs(ctx, tx, binaryIDs); err != nil {
			return fmt.Errorf("stage purgeable binary ids for %s: %w", releaseID, err)
		}
		for _, table := range []string{"binary_parts", "binary_grouping_evidence", "binary_inspections", "binary_inspection_artifacts", "binary_archive_entries", "binary_text_evidence", "binary_media_streams", "binary_par2_sets", "binary_par2_targets", "yenc_recovery_work_items"} {
			count, err := countRowsByStagedBinaryIDs(ctx, tx, table)
			if err != nil {
				return fmt.Errorf("count %s rows for %s: %w", table, releaseID, err)
			}
			result.DeletedRowsByTable[table] = count
		}
		result.DeletedRowsByTable["binary_core"] = int64(len(binaryIDs))
		result.DeletedBinaryRows = int64(len(binaryIDs))
	}
	countQueries := map[string]string{
		"release_files":      `SELECT COUNT(*) FROM release_files WHERE release_id = $1`,
		"release_newsgroups": `SELECT COUNT(*) FROM release_newsgroups WHERE release_id = $1`,
		"nzb_cache":          `SELECT COUNT(*) FROM nzb_cache WHERE release_id = $1`,
		"article_header_ingest_payloads": `
			SELECT COUNT(*)
			FROM article_header_ingest_payloads p
			WHERE p.article_header_id IN (
				SELECT lah.article_header_id
				FROM release_archive_lineage_article_headers lah
				WHERE lah.release_id = $1
			)
			  AND NOT EXISTS (
				SELECT 1 FROM binary_parts bp WHERE bp.article_header_id = p.article_header_id
			  )`,
		"article_headers": `
			SELECT COUNT(*)
			FROM article_headers ah
			WHERE ah.id IN (
				SELECT lah.article_header_id
				FROM release_archive_lineage_article_headers lah
				WHERE lah.release_id = $1
			)
			  AND NOT EXISTS (
				SELECT 1 FROM binary_parts bp WHERE bp.article_header_id = ah.id
			  )`,
		"release_archive_lineage_binaries":        `SELECT COUNT(*) FROM release_archive_lineage_binaries WHERE release_id = $1`,
		"release_archive_lineage_article_headers": `SELECT COUNT(*) FROM release_archive_lineage_article_headers WHERE release_id = $1`,
	}
	for table, query := range countQueries {
		var count int64
		if err := tx.QueryRowContext(ctx, query, releaseID).Scan(&count); err != nil {
			return fmt.Errorf("count %s rows for %s: %w", table, releaseID, err)
		}
		result.DeletedRowsByTable[table] = count
	}
	result.DeletedArticlePayloadRows = result.DeletedRowsByTable["article_header_ingest_payloads"]
	result.DeletedArticleHeaderRows = result.DeletedRowsByTable["article_headers"]
	return nil
}

func (s *Store) DryRunSimpleMaintenanceTask(ctx context.Context, taskKey string, batchSize int) (*MaintenanceTaskResult, error) {
	return s.runSimpleMaintenanceTask(ctx, taskKey, true, batchSize)
}

func (s *Store) RunSimpleMaintenanceTask(ctx context.Context, taskKey string, batchSize int) (*MaintenanceTaskResult, error) {
	return s.runSimpleMaintenanceTask(ctx, taskKey, false, batchSize)
}

func (s *Store) DryRunRawStageRetentionTask(ctx context.Context, batchSize int, policy RawStageRetentionPolicy) (*MaintenanceTaskResult, error) {
	return s.runRawStageRetentionTask(ctx, true, batchSize, normalizeRawStageRetentionPolicy(policy))
}

func (s *Store) RunRawStageRetentionTask(ctx context.Context, batchSize int, policy RawStageRetentionPolicy) (*MaintenanceTaskResult, error) {
	return s.runRawStageRetentionTask(ctx, false, batchSize, normalizeRawStageRetentionPolicy(policy))
}

func (s *Store) DryRunPartitionRetentionTask(ctx context.Context, batchSize int) (*MaintenanceTaskResult, error) {
	return s.partitionRetentionReport(ctx, true, batchSize)
}

func (s *Store) RunPartitionRetentionTask(ctx context.Context, batchSize int) (*MaintenanceTaskResult, error) {
	return s.partitionRetentionReport(ctx, false, batchSize)
}

func (s *Store) DryRunPartitionDefaultRehomeTask(ctx context.Context, batchSize int) (*MaintenanceTaskResult, error) {
	return s.partitionDefaultRehomeReport(ctx, true, batchSize)
}

func (s *Store) RunPartitionDefaultRehomeTask(ctx context.Context, batchSize int) (*MaintenanceTaskResult, error) {
	return s.partitionDefaultRehomeReport(ctx, false, batchSize)
}

func (s *Store) partitionRetentionReport(ctx context.Context, dryRun bool, batchSize int) (*MaintenanceTaskResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if batchSize <= 0 {
		batchSize = 7
	}
	targetTables := nativeSourceWorkPartitionTables()
	result := &MaintenanceTaskResult{
		TaskKey:              "partition_retention_drop",
		DryRun:               dryRun,
		EstimatedRowsByTable: map[string]int64{},
		Warnings: []string{
			"targets the native source/work/projection partition set; binary_core and durable release/archive/catalog tables remain unpartitioned by design",
			"batch size is interpreted as retention-days horizon",
		},
	}
	if !dryRun {
		result.DeletedRowsByTable = map[string]int64{}
	}
	targetValues := strings.Builder{}
	args := make([]any, 0, len(targetTables))
	for i, table := range targetTables {
		if i > 0 {
			targetValues.WriteString(",")
		}
		fmt.Fprintf(&targetValues, "($%d::text)", len(args)+1)
		args = append(args, table)
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH target(table_name) AS (
			VALUES `+targetValues.String()+`
		),
		column_state AS (
			SELECT
				t.table_name,
				EXISTS (
					SELECT 1
					FROM information_schema.columns c
					WHERE c.table_schema = 'public'
					  AND c.table_name = t.table_name
					  AND c.column_name = 'source_posted_at'
				) AS has_source_posted_at,
				EXISTS (
					SELECT 1
					FROM pg_class cls
					JOIN pg_namespace ns ON ns.oid = cls.relnamespace
					JOIN pg_partitioned_table pt ON pt.partrelid = cls.oid
					WHERE ns.nspname = 'public'
					  AND cls.relname = t.table_name
				) AS is_partitioned
			FROM target t
		)
		SELECT table_name, has_source_posted_at, is_partitioned
		FROM column_state
		ORDER BY table_name`, args...)
	if err != nil {
		return nil, fmt.Errorf("partition retention readiness report: %w", err)
	}
	defer rows.Close()

	var withSourcePostedAt int64
	var partitioned int64
	var nonPartitioned int64
	var missingSourcePostedAt []string
	for rows.Next() {
		var table string
		var hasSourcePostedAt bool
		var isPartitioned bool
		if err := rows.Scan(&table, &hasSourcePostedAt, &isPartitioned); err != nil {
			return nil, fmt.Errorf("scan partition retention readiness row: %w", err)
		}
		if hasSourcePostedAt {
			withSourcePostedAt++
		} else {
			missingSourcePostedAt = append(missingSourcePostedAt, table)
		}
		if isPartitioned {
			partitioned++
		} else {
			nonPartitioned++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate partition retention readiness rows: %w", err)
	}
	result.EstimatedRowsByTable["target_tables"] = int64(len(targetTables))
	result.EstimatedRowsByTable["tables_with_source_posted_at"] = withSourcePostedAt
	result.EstimatedRowsByTable["native_partitioned_tables"] = partitioned
	result.EstimatedRowsByTable["non_partitioned_target_tables"] = nonPartitioned
	result.EstimatedRowsByTable["retention_days_horizon"] = int64(batchSize)
	partitionRows, err := s.db.QueryContext(ctx, `
		WITH target(table_name) AS (
			VALUES `+targetValues.String()+`
		)
		SELECT
			t.table_name,
			COUNT(i.inhrelid)::bigint AS partition_count,
			COUNT(*) FILTER (WHERE pg_get_expr(c.relpartbound, c.oid) = 'DEFAULT')::bigint AS default_partition_count
		FROM target t
		LEFT JOIN pg_class p
		  ON p.relname = t.table_name
		LEFT JOIN pg_namespace pn
		  ON pn.oid = p.relnamespace
		 AND pn.nspname = 'public'
		LEFT JOIN pg_inherits i
		  ON i.inhparent = p.oid
		LEFT JOIN pg_class c
		  ON c.oid = i.inhrelid
		GROUP BY t.table_name
		ORDER BY t.table_name`, args...)
	if err != nil {
		return nil, fmt.Errorf("partition retention partition counts: %w", err)
	}
	defer partitionRows.Close()
	for partitionRows.Next() {
		var table string
		var partitionCount int64
		var defaultPartitionCount int64
		if err := partitionRows.Scan(&table, &partitionCount, &defaultPartitionCount); err != nil {
			return nil, fmt.Errorf("scan partition retention partition counts: %w", err)
		}
		result.EstimatedRowsByTable[table+"_partitions"] = partitionCount
		result.EstimatedRowsByTable[table+"_default_partitions"] = defaultPartitionCount
	}
	if err := partitionRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate partition retention partition counts: %w", err)
	}
	if len(missingSourcePostedAt) > 0 {
		result.Blockers = append(result.Blockers, "missing source_posted_at: "+strings.Join(missingSourcePostedAt, ", "))
	}
	if nonPartitioned > 0 {
		result.Blockers = append(result.Blockers, "native partition drop is blocked until target source/work tables are rebuilt as daily partitions")
	}
	if len(result.Blockers) > 0 {
		return result, nil
	}

	defaultRows, err := s.partitionDefaultRows(ctx, targetTables)
	if err != nil {
		return nil, err
	}
	for table, count := range defaultRows {
		result.EstimatedRowsByTable[table+"_default_rows"] = count
		if count > 0 {
			result.Blockers = append(result.Blockers, fmt.Sprintf("%s default partition contains %d rows", table, count))
		}
	}
	if len(result.Blockers) > 0 {
		return result, nil
	}

	candidates, err := s.partitionRetentionCandidates(ctx, batchSize, targetTables)
	if err != nil {
		return nil, err
	}
	byDay := map[string][]partitionRetentionCandidate{}
	for _, candidate := range candidates {
		dayKey := candidate.Day.Format("2006-01-02")
		byDay[dayKey] = append(byDay[dayKey], candidate)
		result.EstimatedRowsByTable["partition_"+candidate.ChildTable] = candidate.RowEstimate
		result.EstimatedRowsByTable["eligible_partition_rows"] += candidate.RowEstimate
	}
	result.EstimatedRowsByTable["eligible_partition_count"] = int64(len(candidates))
	result.EstimatedRowsByTable["eligible_day_count"] = int64(len(byDay))

	eligibleDays := make([]string, 0, len(byDay))
	for dayKey := range byDay {
		eligibleDays = append(eligibleDays, dayKey)
	}
	sort.Strings(eligibleDays)
	for _, dayKey := range eligibleDays {
		day, err := time.Parse("2006-01-02", dayKey)
		if err != nil {
			return nil, err
		}
		dayBlockers, err := s.partitionRetentionDayBlockers(ctx, day)
		if err != nil {
			return nil, err
		}
		if len(dayBlockers) > 0 {
			for _, blocker := range dayBlockers {
				result.Blockers = append(result.Blockers, dayKey+": "+blocker)
			}
			continue
		}
		if dryRun {
			continue
		}
		dropped, err := s.dropPartitionRetentionDay(ctx, byDay[dayKey])
		if err != nil {
			return nil, err
		}
		for table, count := range dropped {
			result.DeletedRowsByTable[table] += count
		}
	}
	return result, nil
}

func nativeSourceWorkPartitionTables() []string {
	return []string{
		"article_headers",
		"article_header_ingest_payloads",
		"article_header_crosspost_groups",
		"article_header_poster_refs",
		"article_header_assembly_queue",
		"poster_materialization_queue",
		"binary_parts",
		"binary_observation_stats",
		"binary_identity_current",
		"binary_recovery_current",
		"binary_lifecycle",
		"binary_completion_keys",
		"binary_grouping_evidence",
		"binary_projection_events",
		"binary_superseded_sources",
		"yenc_recovery_work_items",
		"article_cohort_candidates",
		"article_cohort_assembly_queue",
		"article_cohort_yenc_queue",
		"binary_inspection_ready_queue",
		"binary_inspections",
		"binary_inspection_artifacts",
		"binary_archive_entries",
		"binary_text_evidence",
		"binary_media_streams",
		"binary_par2_sets",
		"binary_par2_targets",
		"release_family_readiness_summaries",
		"release_ready_candidates",
		"release_recovered_file_set_candidates",
		"release_stage_dirty_families",
	}
}

func nativeSourceWorkPartitionDropOrder() []string {
	return []string{
		"release_stage_dirty_families",
		"release_ready_candidates",
		"release_recovered_file_set_candidates",
		"release_family_readiness_summaries",
		"binary_inspection_ready_queue",
		"binary_inspection_artifacts",
		"binary_archive_entries",
		"binary_text_evidence",
		"binary_media_streams",
		"binary_par2_targets",
		"binary_par2_sets",
		"binary_inspections",
		"yenc_recovery_work_items",
		"article_cohort_yenc_queue",
		"article_cohort_assembly_queue",
		"article_cohort_candidates",
		"binary_projection_events",
		"binary_superseded_sources",
		"binary_grouping_evidence",
		"binary_completion_keys",
		"binary_lifecycle",
		"binary_recovery_current",
		"binary_identity_current",
		"binary_observation_stats",
		"article_header_assembly_queue",
		"poster_materialization_queue",
		"binary_parts",
		"article_header_poster_refs",
		"article_header_crosspost_groups",
		"article_header_ingest_payloads",
		"article_headers",
	}
}

func (s *Store) partitionDefaultRows(ctx context.Context, targetTables []string) (map[string]int64, error) {
	out := make(map[string]int64, len(targetTables))
	for _, table := range targetTables {
		defaultTable := table + "_default"
		exists, err := s.tableExists(ctx, defaultTable)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		var count int64
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+quoteIdentifier(defaultTable)).Scan(&count); err != nil {
			return nil, fmt.Errorf("count %s rows: %w", defaultTable, err)
		}
		out[table] = count
	}
	return out, nil
}

type partitionDefaultDay struct {
	DayKey string
	Rows   int64
}

func (s *Store) partitionDefaultRehomeReport(ctx context.Context, dryRun bool, batchSize int) (*MaintenanceTaskResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if batchSize <= 0 {
		batchSize = 1
	}
	if batchSize > 7 {
		batchSize = 7
	}
	targetTables := nativeSourceWorkPartitionTables()
	result := &MaintenanceTaskResult{
		TaskKey:              "partition_default_rehome",
		DryRun:               dryRun,
		EstimatedRowsByTable: map[string]int64{},
		Warnings: []string{
			"moves rows out of default partitions into dated child partitions",
			"batch size is interpreted as number of default-partition days to move, capped at 7",
			"run manually or while scrape is quiet; default partitions are briefly detached in a transaction",
		},
	}
	if !dryRun {
		result.DeletedRowsByTable = map[string]int64{}
	}
	days, err := s.partitionDefaultRehomeDays(ctx, targetTables, batchSize)
	if err != nil {
		return nil, err
	}
	for _, day := range days {
		result.EstimatedRowsByTable["default_day_"+day.DayKey] = day.Rows
	}
	result.EstimatedRowsByTable["eligible_default_day_count"] = int64(len(days))
	if err := s.addPartitionDefaultRehomeBlockers(ctx, result); err != nil {
		return nil, err
	}
	if len(result.Blockers) > 0 {
		if err := s.addPartitionDefaultRowEstimates(ctx, result, targetTables); err != nil {
			return nil, err
		}
		return result, nil
	}
	if dryRun || len(days) == 0 {
		if err := s.addPartitionDefaultRowEstimates(ctx, result, targetTables); err != nil {
			return nil, err
		}
		return result, nil
	}
	for _, day := range days {
		moved, err := s.rehomeDefaultPartitionDay(ctx, day.DayKey, targetTables)
		if err != nil {
			return nil, err
		}
		for table, count := range moved {
			result.DeletedRowsByTable[table+"_default_moved"] += count
		}
	}
	if err := s.addPartitionDefaultRowEstimates(ctx, result, targetTables); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) addPartitionDefaultRehomeBlockers(ctx context.Context, result *MaintenanceTaskResult) error {
	exists, err := s.tableExists(ctx, "article_headers_default")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	var defaultRows int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM article_headers_default`).Scan(&defaultRows); err != nil {
		return fmt.Errorf("count article_headers_default rows: %w", err)
	}
	if defaultRows == 0 {
		return nil
	}
	var dependentFKs int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM pg_constraint
		WHERE contype = 'f'
		  AND confrelid = 'article_headers_default'::regclass`).Scan(&dependentFKs); err != nil {
		return fmt.Errorf("count article_headers_default dependent fks: %w", err)
	}
	if dependentFKs > 0 {
		result.Blockers = append(result.Blockers, fmt.Sprintf("article_headers_default has %d rows and %d partition-specific foreign keys; online rehome would require dropping inherited FK constraints, so leave these rows for raw retention or perform an offline migration", defaultRows, dependentFKs))
	}
	return nil
}

func (s *Store) addPartitionDefaultRowEstimates(ctx context.Context, result *MaintenanceTaskResult, targetTables []string) error {
	defaultRows, err := s.partitionDefaultRows(ctx, targetTables)
	if err != nil {
		return err
	}
	for table, count := range defaultRows {
		result.EstimatedRowsByTable[table+"_default_rows"] = count
	}
	return nil
}

func (s *Store) partitionDefaultRehomeDays(ctx context.Context, targetTables []string, limit int) ([]partitionDefaultDay, error) {
	counts := map[string]int64{}
	for _, table := range targetTables {
		defaultTable := table + "_default"
		exists, err := s.tableExists(ctx, defaultTable)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT (date_trunc('day', source_posted_at))::date::text AS day_key, COUNT(*)::bigint
			FROM `+quoteIdentifier(defaultTable)+`
			GROUP BY day_key
			ORDER BY day_key`)
		if err != nil {
			return nil, fmt.Errorf("list default partition days for %s: %w", defaultTable, err)
		}
		for rows.Next() {
			var dayKey string
			var count int64
			if err := rows.Scan(&dayKey, &count); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan default partition day for %s: %w", defaultTable, err)
			}
			counts[dayKey] += count
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate default partition days for %s: %w", defaultTable, err)
		}
		rows.Close()
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	out := make([]partitionDefaultDay, 0, len(keys))
	for _, key := range keys {
		out = append(out, partitionDefaultDay{DayKey: key, Rows: counts[key]})
	}
	return out, nil
}

func (s *Store) rehomeDefaultPartitionDay(ctx context.Context, dayKey string, targetTables []string) (map[string]int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin default partition rehome tx: %w", err)
	}
	defer rollbackTx(tx)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('pgindex-partition-default-rehome'))`); err != nil {
		return nil, fmt.Errorf("lock default partition rehome: %w", err)
	}

	detachOrder := orderedPartitionTables(targetTables, nativeSourceWorkPartitionDropOrder())
	detached := make([]string, 0, len(detachOrder))
	for _, table := range detachOrder {
		defaultTable := table + "_default"
		exists, err := s.tableExists(ctx, defaultTable)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s DETACH PARTITION %s`, quoteIdentifier(table), quoteIdentifier(defaultTable))); err != nil {
			return nil, fmt.Errorf("detach default partition %s: %w", defaultTable, err)
		}
		detached = append(detached, table)
		childTable := table + "_" + strings.ReplaceAll(dayKey, "-", "")
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM (%s::date::timestamptz) TO ((%s::date + 1)::timestamptz)`,
			quoteIdentifier(childTable),
			quoteIdentifier(table),
			quoteLiteral(dayKey),
			quoteLiteral(dayKey),
		)); err != nil {
			return nil, fmt.Errorf("create dated partition %s: %w", childTable, err)
		}
	}

	moved := map[string]int64{}
	for _, table := range targetTables {
		if !stringInSlice(table, detached) {
			continue
		}
		defaultTable := table + "_default"
		columns, err := s.tableInsertColumns(ctx, table)
		if err != nil {
			return nil, err
		}
		if len(columns) == 0 {
			continue
		}
		columnSQL := quoteIdentifierList(columns)
		res, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s (%s)
			SELECT %s
			FROM %s
			WHERE source_posted_at >= %s::date::timestamptz
			  AND source_posted_at < (%s::date + 1)::timestamptz
			ON CONFLICT DO NOTHING`,
			quoteIdentifier(table),
			columnSQL,
			columnSQL,
			quoteIdentifier(defaultTable),
			quoteLiteral(dayKey),
			quoteLiteral(dayKey),
		))
		if err != nil {
			return nil, fmt.Errorf("copy %s default rows for %s: %w", table, dayKey, err)
		}
		count, _ := res.RowsAffected()
		moved[table] += count
	}

	dropOrder := orderedPartitionTables(detached, nativeSourceWorkPartitionDropOrder())
	for _, table := range dropOrder {
		if !stringInSlice(table, detached) {
			continue
		}
		defaultTable := table + "_default"
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			DELETE FROM %s
			WHERE source_posted_at >= %s::date::timestamptz
			  AND source_posted_at < (%s::date + 1)::timestamptz`,
			quoteIdentifier(defaultTable),
			quoteLiteral(dayKey),
			quoteLiteral(dayKey),
		)); err != nil {
			return nil, fmt.Errorf("delete moved %s rows for %s: %w", defaultTable, dayKey, err)
		}
	}

	for i := len(dropOrder) - 1; i >= 0; i-- {
		table := dropOrder[i]
		defaultTable := table + "_default"
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ATTACH PARTITION %s DEFAULT`, quoteIdentifier(table), quoteIdentifier(defaultTable))); err != nil {
			return nil, fmt.Errorf("reattach default partition %s: %w", defaultTable, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit default partition rehome tx: %w", err)
	}
	return moved, nil
}

func orderedPartitionTables(tables []string, preferredOrder []string) []string {
	remaining := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		remaining[table] = struct{}{}
	}
	out := make([]string, 0, len(tables))
	for _, table := range preferredOrder {
		if _, ok := remaining[table]; !ok {
			continue
		}
		out = append(out, table)
		delete(remaining, table)
	}
	for _, table := range tables {
		if _, ok := remaining[table]; !ok {
			continue
		}
		out = append(out, table)
		delete(remaining, table)
	}
	return out
}

func (s *Store) tableInsertColumns(ctx context.Context, table string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = $1
		  AND is_generated = 'NEVER'
		ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, fmt.Errorf("list insert columns for %s: %w", table, err)
	}
	defer rows.Close()
	columns := []string{}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, fmt.Errorf("scan insert column for %s: %w", table, err)
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func quoteIdentifierList(values []string) string {
	out := strings.Builder{}
	for i, value := range values {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(quoteIdentifier(value))
	}
	return out.String()
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func stringInSlice(value string, values []string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func (s *Store) tableExists(ctx context.Context, table string) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public'
			  AND c.relname = $1
		)`, table).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *Store) partitionRetentionCandidates(ctx context.Context, retentionDays int, targetTables []string) ([]partitionRetentionCandidate, error) {
	targetValues := strings.Builder{}
	args := make([]any, 0, len(targetTables)+1)
	for i, table := range targetTables {
		if i > 0 {
			targetValues.WriteString(",")
		}
		fmt.Fprintf(&targetValues, "($%d::text)", len(args)+1)
		args = append(args, table)
	}
	args = append(args, retentionDays)
	rows, err := s.db.QueryContext(ctx, `
		WITH target(table_name) AS (
			VALUES `+targetValues.String()+`
		)
		SELECT
			parent.relname AS parent_table,
			child.relname AS child_table,
			to_date(substring(child.relname from '([0-9]{8})$'), 'YYYYMMDD')::date AS partition_day,
			GREATEST(COALESCE(child.reltuples, 0), 0)::bigint AS row_estimate
		FROM target t
		JOIN pg_class parent
		  ON parent.relname = t.table_name
		JOIN pg_namespace pn
		  ON pn.oid = parent.relnamespace
		 AND pn.nspname = 'public'
		JOIN pg_inherits i
		  ON i.inhparent = parent.oid
		JOIN pg_class child
		  ON child.oid = i.inhrelid
		WHERE child.relname ~ '_[0-9]{8}$'
		  AND to_date(substring(child.relname from '([0-9]{8})$'), 'YYYYMMDD') < CURRENT_DATE - ($`+fmt.Sprint(len(args))+`::int)
		ORDER BY partition_day, parent.relname`, args...)
	if err != nil {
		return nil, fmt.Errorf("list partition retention candidates: %w", err)
	}
	defer rows.Close()
	out := []partitionRetentionCandidate{}
	for rows.Next() {
		var item partitionRetentionCandidate
		if err := rows.Scan(&item.TableName, &item.ChildTable, &item.Day, &item.RowEstimate); err != nil {
			return nil, fmt.Errorf("scan partition retention candidate: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate partition retention candidates: %w", err)
	}
	return out, nil
}

func (s *Store) partitionRetentionDayBlockers(ctx context.Context, day time.Time) ([]string, error) {
	start := day.UTC()
	end := start.AddDate(0, 0, 1)
	checks := []struct {
		label string
		query string
	}{
		{
			label: "assembly queue rows still exist",
			query: `SELECT COUNT(*) FROM article_header_assembly_queue WHERE source_posted_at >= $1 AND source_posted_at < $2`,
		},
		{
			label: "ready/running yEnc work still exists",
			query: `SELECT COUNT(*) FROM yenc_recovery_work_items WHERE source_posted_at >= $1 AND source_posted_at < $2 AND status IN ('ready', 'running')`,
		},
		{
			label: "active article cohort scheduler work still exists",
			query: `
				SELECT (
					(SELECT COUNT(*) FROM article_cohort_assembly_queue WHERE source_posted_at >= $1 AND source_posted_at < $2 AND status IN ('ready', 'running')) +
					(SELECT COUNT(*) FROM article_cohort_yenc_queue WHERE source_posted_at >= $1 AND source_posted_at < $2 AND status = 'ready') +
					(SELECT COUNT(*) FROM article_cohort_candidates WHERE source_posted_at >= $1 AND source_posted_at < $2 AND status IN ('ready', 'active'))
				)`,
		},
		{
			label: "running inspect ready queue rows still exist",
			query: `SELECT COUNT(*) FROM binary_inspection_ready_queue WHERE source_posted_at >= $1 AND source_posted_at < $2 AND status = 'running'`,
		},
		{
			label: "running binary inspections still exist",
			query: `SELECT COUNT(*) FROM binary_inspections WHERE source_updated_at >= $1 AND source_updated_at < $2 AND status = 'running'`,
		},
		{
			label: "non-archived release files still reference this day",
			query: `
				SELECT COUNT(*)
				FROM release_files rf
				JOIN binary_parts bp ON bp.binary_id = rf.binary_id
				LEFT JOIN release_archive_state ras ON ras.release_id = rf.release_id
				WHERE bp.source_posted_at >= $1
				  AND bp.source_posted_at < $2
				  AND COALESCE(ras.archive_status, 'active') NOT IN ('archived', 'purge_pending', 'purged')`,
		},
	}
	out := []string{}
	for _, check := range checks {
		var count int64
		if err := s.db.QueryRowContext(ctx, check.query, start, end).Scan(&count); err != nil {
			return nil, fmt.Errorf("check partition retention blocker %q: %w", check.label, err)
		}
		if count > 0 {
			out = append(out, fmt.Sprintf("%s: %d", check.label, count))
		}
	}
	return out, nil
}

func (s *Store) dropPartitionRetentionDay(ctx context.Context, candidates []partitionRetentionCandidate) (map[string]int64, error) {
	byTable := map[string]partitionRetentionCandidate{}
	for _, candidate := range candidates {
		byTable[candidate.TableName] = candidate
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin partition drop tx: %w", err)
	}
	defer rollbackTx(tx)
	out := map[string]int64{}
	for _, table := range nativeSourceWorkPartitionDropOrder() {
		candidate, ok := byTable[table]
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`ALTER TABLE %s DETACH PARTITION %s`,
			quoteIdentifier(candidate.TableName),
			quoteIdentifier(candidate.ChildTable),
		)); err != nil {
			return nil, fmt.Errorf("detach partition %s: %w", candidate.ChildTable, err)
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+quoteIdentifier(candidate.ChildTable)); err != nil {
			return nil, fmt.Errorf("drop partition %s: %w", candidate.ChildTable, err)
		}
		out[candidate.ChildTable] = candidate.RowEstimate
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit partition drop tx: %w", err)
	}
	return out, nil
}

func normalizeRawStageRetentionPolicy(policy RawStageRetentionPolicy) RawStageRetentionPolicy {
	if policy.HotHours <= 0 {
		policy.HotHours = 48
	}
	if policy.WarmHours <= 0 {
		policy.WarmHours = 24
	}
	if policy.ColdHours <= 0 {
		policy.ColdHours = 12
	}
	if policy.FailedProbeHours <= 0 {
		policy.FailedProbeHours = 48
	}
	if policy.DoneYEncHours <= 0 {
		policy.DoneYEncHours = 48
	}
	return policy
}

func (s *Store) runSimpleMaintenanceTask(ctx context.Context, taskKey string, dryRun bool, batchSize int) (*MaintenanceTaskResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if batchSize <= 0 {
		batchSize = 1000
	}
	result := &MaintenanceTaskResult{TaskKey: taskKey, DryRun: dryRun}
	if dryRun {
		result.EstimatedRowsByTable = map[string]int64{}
	} else {
		result.DeletedRowsByTable = map[string]int64{}
	}
	add := func(table string, count int64) {
		if dryRun {
			result.EstimatedRowsByTable[table] += count
		} else {
			result.DeletedRowsByTable[table] += count
		}
	}
	switch taskKey {
	case "vacuum_dead_tuple_tables":
		candidates, err := s.maintenanceVacuumCandidates(ctx, batchSize)
		if err != nil {
			return nil, err
		}
		result.EstimatedRowsByTable = map[string]int64{}
		tables := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			tables = append(tables, candidate.TableName)
			result.EstimatedRowsByTable[candidate.TableName] = candidate.DeadRows
			result.EstimatedBytes += candidate.TotalBytes
		}
		if len(tables) > 0 {
			before, err := s.maintenanceStorageSnapshot(ctx, tables...)
			if err != nil {
				return nil, err
			}
			result.BeforeStorage = before
		}
		result.Warnings = append(result.Warnings,
			"runs plain VACUUM (ANALYZE) only; it deletes no application rows",
			"space becomes reusable inside PostgreSQL but OS disk space is not returned without VACUUM FULL, pg_repack, CLUSTER, or a table rewrite",
			"batch size limits the number of tables vacuumed per run",
		)
		if dryRun {
			break
		}
		for _, candidate := range candidates {
			if _, err := s.db.ExecContext(ctx, `VACUUM (ANALYZE) `+quoteIdentifier(candidate.TableName)); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("VACUUM (ANALYZE) failed for %s: %v", candidate.TableName, err))
				continue
			}
			result.VacuumedTables = append(result.VacuumedTables, candidate.TableName)
		}
		if len(result.VacuumedTables) > 0 {
			after, err := s.maintenanceStorageSnapshot(ctx, result.VacuumedTables...)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("post-vacuum storage snapshot failed: %v", err))
			} else {
				result.AfterStorage = after
			}
		}
	case "poster_queue_done_cleanup":
		const table = "poster_materialization_queue"
		from := `FROM poster_materialization_queue WHERE status = 'done'`
		before, err := s.maintenanceStorageSnapshot(ctx, table)
		if err != nil {
			return nil, err
		}
		result.BeforeStorage = before
		if dryRun {
			count, err := countRowsFrom(ctx, s.db, from)
			if err != nil {
				return nil, err
			}
			add(table, count)
			if before != nil {
				result.EstimatedBytes = before.TableTotalBytesByTable[table]
			}
			result.Warnings = append(result.Warnings,
				"dry-run estimates all done rows; run deletes at most the configured batch size",
				"delete creates PostgreSQL reusable space but does not return OS space until VACUUM FULL, pg_repack, CLUSTER, or a table rewrite",
			)
		} else {
			count, err := execDeleteCount(ctx, s.db, `
				DELETE FROM poster_materialization_queue q
				WHERE q.article_header_id IN (
					SELECT article_header_id
					FROM poster_materialization_queue
					WHERE status = 'done'
					ORDER BY article_header_id
					LIMIT $1
				)`, batchSize)
			if err != nil {
				return nil, err
			}
			add(table, count)
			after, err := s.maintenanceStorageSnapshot(ctx, table)
			if err != nil {
				return nil, err
			}
			result.AfterStorage = after
			result.Warnings = append(result.Warnings,
				"batch-limited delete completed; rerun or schedule the task to continue draining done rows",
				"OS free space is not expected to increase until VACUUM FULL, pg_repack, CLUSTER, or a table rewrite runs",
			)
		}
	case "inspect_ready_queue_cleanup":
		const table = "binary_inspection_ready_queue"
		from := `
			FROM binary_inspection_ready_queue q
			WHERE q.status IN ('completed', 'blocked')
			  AND EXISTS (
				SELECT 1
				FROM binary_inspections bi
				WHERE bi.stage_name = q.stage_name
				  AND bi.binary_id = q.binary_id
				  AND bi.status IN ('completed', 'failed')
			  )`
		before, err := s.maintenanceStorageSnapshot(ctx, table)
		if err != nil {
			return nil, err
		}
		result.BeforeStorage = before
		if dryRun {
			count, err := countRowsFrom(ctx, s.db, from)
			if err != nil {
				return nil, err
			}
			add(table, count)
			if before != nil {
				result.EstimatedBytes = before.TableTotalBytesByTable[table]
			}
		} else {
			count, err := execDeleteCount(ctx, s.db, `
				WITH eligible AS (
					SELECT q.stage_name, q.binary_id
					FROM binary_inspection_ready_queue q
					WHERE q.status IN ('completed', 'blocked')
					  AND EXISTS (
						SELECT 1
						FROM binary_inspections bi
						WHERE bi.stage_name = q.stage_name
						  AND bi.binary_id = q.binary_id
						  AND bi.status IN ('completed', 'failed')
					  )
					ORDER BY q.updated_at, q.stage_name, q.binary_id
					LIMIT $1
				)
				DELETE FROM binary_inspection_ready_queue q
				USING eligible e
				WHERE q.stage_name = e.stage_name
				  AND q.binary_id = e.binary_id`, batchSize)
			if err != nil {
				return nil, err
			}
			add(table, count)
			after, err := s.maintenanceStorageSnapshot(ctx, table)
			if err != nil {
				return nil, err
			}
			result.AfterStorage = after
		}
	case "assembly_queue_stale_cleanup":
		const table = "article_header_assembly_queue"
		before, err := s.maintenanceStorageSnapshot(ctx, table)
		if err != nil {
			return nil, err
		}
		result.BeforeStorage = before
		if dryRun {
			count, err := countRowsFrom(ctx, s.db, `FROM article_header_assembly_queue q WHERE EXISTS (SELECT 1 FROM binary_parts bp WHERE bp.article_header_id = q.article_header_id)`)
			if err != nil {
				return nil, err
			}
			add(table, count)
			if before != nil {
				result.EstimatedBytes = before.TableTotalBytesByTable[table]
			}
		} else {
			count, err := execDeleteCount(ctx, s.db, `
				WITH eligible AS (
					SELECT q.article_header_id
					FROM article_header_assembly_queue q
					WHERE EXISTS (
						SELECT 1
						FROM binary_parts bp
						WHERE bp.article_header_id = q.article_header_id
					)
					ORDER BY q.article_header_id
					LIMIT $1
				)
				DELETE FROM article_header_assembly_queue q
				USING eligible e
				WHERE q.article_header_id = e.article_header_id`, batchSize)
			if err != nil {
				return nil, err
			}
			add(table, count)
			after, err := s.maintenanceStorageSnapshot(ctx, table)
			if err != nil {
				return nil, err
			}
			result.AfterStorage = after
		}
	case "runtime_history_cleanup":
		tasks := []struct {
			table       string
			countQuery  string
			deleteQuery string
		}{
			{
				table:      "indexer_stage_runs",
				countQuery: `FROM indexer_stage_runs WHERE (status IN ('completed', 'abandoned') AND started_at < NOW() - INTERVAL '14 days') OR (status = 'failed' AND started_at < NOW() - INTERVAL '30 days')`,
				deleteQuery: `
					WITH eligible AS (
						SELECT id
						FROM indexer_stage_runs
						WHERE (status IN ('completed', 'abandoned') AND started_at < NOW() - INTERVAL '14 days')
						   OR (status = 'failed' AND started_at < NOW() - INTERVAL '30 days')
						ORDER BY started_at, id
						LIMIT $1
					)
					DELETE FROM indexer_stage_runs r
					USING eligible e
					WHERE r.id = e.id`,
			},
			{
				table:      "scrape_runs",
				countQuery: `FROM scrape_runs WHERE (status IN ('completed', 'abandoned') AND started_at < NOW() - INTERVAL '14 days') OR (status = 'failed' AND started_at < NOW() - INTERVAL '30 days')`,
				deleteQuery: `
					WITH eligible AS (
						SELECT id
						FROM scrape_runs
						WHERE (status IN ('completed', 'abandoned') AND started_at < NOW() - INTERVAL '14 days')
						   OR (status = 'failed' AND started_at < NOW() - INTERVAL '30 days')
						ORDER BY started_at, id
						LIMIT $1
					)
					DELETE FROM scrape_runs r
					USING eligible e
					WHERE r.id = e.id`,
			},
		}
		result.BeforeStorage, _ = s.maintenanceStorageSnapshot(ctx, "indexer_stage_runs", "scrape_runs")
		result.Warnings = append(result.Warnings, "binary_inspections are retained because completed rows gate release generation/archive and suppress unnecessary reinspection")
		for _, task := range tasks {
			var (
				count int64
				err   error
			)
			if dryRun {
				count, err = countRowsFrom(ctx, s.db, task.countQuery)
			} else {
				count, err = execDeleteCount(ctx, s.db, task.deleteQuery, batchSize)
			}
			if err != nil {
				return nil, err
			}
			add(task.table, count)
		}
		if !dryRun {
			result.AfterStorage, _ = s.maintenanceStorageSnapshot(ctx, "indexer_stage_runs", "scrape_runs")
		}
	case "grouping_evidence_cleanup":
		from := `FROM binary_grouping_evidence bge JOIN binary_identity_current bic ON bic.source_posted_at = bge.source_posted_at AND bic.binary_id = bge.binary_id WHERE bge.updated_at < NOW() - INTERVAL '24 hours' AND bic.match_confidence >= 0.85 AND LOWER(COALESCE(bic.identity_strength, '')) NOT IN ('weak', 'provisional') AND LOWER(COALESCE(bic.family_kind, '')) NOT IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set') AND COALESCE((bge.payload_json->'summary'->>'fallback_used')::boolean, false) = false AND bge.payload_json ? 'summary'`
		if dryRun {
			count, err := countRowsFrom(ctx, s.db, from)
			if err != nil {
				return nil, err
			}
			add("binary_grouping_evidence", count)
		} else {
			count, err := execDeleteCount(ctx, s.db, `WITH eligible AS (SELECT bge.source_posted_at, bge.binary_id `+from+`) DELETE FROM binary_grouping_evidence bge USING eligible e WHERE bge.source_posted_at = e.source_posted_at AND bge.binary_id = e.binary_id`)
			if err != nil {
				return nil, err
			}
			add("binary_grouping_evidence", count)
		}
	case "crosspost_group_raw_purge":
		const table = "article_header_crosspost_groups"
		from := `
			FROM article_header_crosspost_groups g
			JOIN article_header_crosspost_group_summary s
			  ON s.observed_group_name = g.observed_group_name
			JOIN crosspost_popularity_refresh_queue q
			  ON q.observed_group_name = g.observed_group_name
			 AND q.status = 'done'
			WHERE g.observed_at < NOW() - INTERVAL '72 hours'
			  AND g.article_header_id <= COALESCE(s.last_refreshed_article_header_id, 0)`
		before, err := s.maintenanceStorageSnapshot(ctx, table)
		if err != nil {
			return nil, err
		}
		result.BeforeStorage = before
		result.Warnings = append(result.Warnings,
			"deletes only raw Xref telemetry already consumed by crosspost popularity summary and older than 72h",
			"does not delete crosspost summaries, refresh queue rows, article headers, binaries, releases, catalog files, or archived NZBs",
			"delete creates PostgreSQL reusable space but does not return OS space until VACUUM FULL, pg_repack, CLUSTER, or a table rewrite",
		)
		if dryRun {
			count, err := countRowsFrom(ctx, s.db, from)
			if err != nil {
				return nil, err
			}
			add(table, count)
			if before != nil {
				result.EstimatedBytes = before.TableTotalBytesByTable[table]
			}
		} else {
			count, err := execDeleteCount(ctx, s.db, `
				WITH eligible AS (
					SELECT g.article_header_id, g.observed_group_name
					FROM article_header_crosspost_groups g
					JOIN article_header_crosspost_group_summary s
					  ON s.observed_group_name = g.observed_group_name
					JOIN crosspost_popularity_refresh_queue q
					  ON q.observed_group_name = g.observed_group_name
					 AND q.status = 'done'
					WHERE g.observed_at < NOW() - INTERVAL '72 hours'
					  AND g.article_header_id <= COALESCE(s.last_refreshed_article_header_id, 0)
					ORDER BY g.observed_at, g.article_header_id, g.observed_group_name
					LIMIT $1
				)
				DELETE FROM article_header_crosspost_groups g
				USING eligible e
				WHERE g.article_header_id = e.article_header_id
				  AND g.observed_group_name = e.observed_group_name`, batchSize)
			if err != nil {
				return nil, err
			}
			add(table, count)
			after, err := s.maintenanceStorageSnapshot(ctx, table)
			if err != nil {
				return nil, err
			}
			result.AfterStorage = after
		}
	case "yenc_done_work_item_cleanup":
		const table = "yenc_recovery_work_items"
		from := `
			FROM yenc_recovery_work_items wi
			JOIN binary_recovery_current brc
			  ON brc.binary_id = wi.binary_id
			 AND brc.recovered_source = 'yenc_header'
			WHERE wi.status = 'done'
			  AND wi.updated_at < NOW() - INTERVAL '72 hours'
			  AND NOT EXISTS (
				SELECT 1
				FROM release_files rf
				WHERE rf.binary_id = wi.binary_id
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM release_archive_lineage_binaries lb
				WHERE lb.binary_id = wi.binary_id
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM binary_inspection_ready_queue rq
				WHERE rq.binary_id = wi.binary_id
				  AND rq.status = 'running'
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM binary_inspections bi
				WHERE bi.binary_id = wi.binary_id
				  AND bi.status = 'running'
			  )`
		before, err := s.maintenanceStorageSnapshot(ctx, table)
		if err != nil {
			return nil, err
		}
		result.BeforeStorage = before
		result.Warnings = append(result.Warnings,
			"deletes only completed yEnc recovery work receipts older than 72h after durable binary_recovery_current yEnc projection exists",
			"keeps ready/running yEnc backlog, article headers, payloads, binary roots, release files, archive lineage, catalog data, and NZBs",
			"delete creates PostgreSQL reusable space after post-delete VACUUM (ANALYZE) but does not return OS space until VACUUM FULL, pg_repack, CLUSTER, or a table rewrite",
		)
		if dryRun {
			count, err := countRowsFrom(ctx, s.db, from)
			if err != nil {
				return nil, err
			}
			add(table, count)
			if before != nil {
				result.EstimatedBytes = before.TableTotalBytesByTable[table]
			}
		} else {
			count, err := execDeleteCount(ctx, s.db, `
				WITH eligible AS (
					SELECT wi.binary_id
					FROM yenc_recovery_work_items wi
					JOIN binary_recovery_current brc
					  ON brc.binary_id = wi.binary_id
					 AND brc.recovered_source = 'yenc_header'
					WHERE wi.status = 'done'
					  AND wi.updated_at < NOW() - INTERVAL '72 hours'
					  AND NOT EXISTS (
						SELECT 1
						FROM release_files rf
						WHERE rf.binary_id = wi.binary_id
					  )
					  AND NOT EXISTS (
						SELECT 1
						FROM release_archive_lineage_binaries lb
						WHERE lb.binary_id = wi.binary_id
					  )
					  AND NOT EXISTS (
						SELECT 1
						FROM binary_inspection_ready_queue rq
						WHERE rq.binary_id = wi.binary_id
						  AND rq.status = 'running'
					  )
					  AND NOT EXISTS (
						SELECT 1
						FROM binary_inspections bi
						WHERE bi.binary_id = wi.binary_id
						  AND bi.status = 'running'
					  )
					ORDER BY wi.updated_at, wi.binary_id
					LIMIT $1
				)
				DELETE FROM yenc_recovery_work_items wi
				USING eligible e
				WHERE wi.binary_id = e.binary_id`, batchSize)
			if err != nil {
				return nil, err
			}
			add(table, count)
			after, err := s.maintenanceStorageSnapshot(ctx, table)
			if err != nil {
				return nil, err
			}
			result.AfterStorage = after
		}
	case "header_payload_purge":
		result.EstimatedRowsByTable = map[string]int64{"article_header_ingest_payloads": 0}
		result.Warnings = []string{
			"legacy payload purge is disabled because assembled_at is not authoritative in the queue-based assembly path",
			"use the storage audit source-window and stale-source sections before implementing relationship-guarded payload cleanup",
		}
		if !dryRun {
			result.Blockers = append(result.Blockers, "header_payload_purge is audit-blocked until it is rewritten without assembled_at")
		}
	case "stale_nonrelease_source_purge":
		if err := s.runStaleNonreleaseSourcePurge(ctx, result, dryRun, batchSize); err != nil {
			return nil, err
		}
	case "emergency_source_window_reset":
		if err := s.runEmergencySourceWindowReset(ctx, result, dryRun, batchSize); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported maintenance task %q", taskKey)
	}
	if !dryRun {
		s.vacuumDeletedMaintenanceTables(ctx, result)
	}
	return result, nil
}

func (s *Store) maintenanceVacuumCandidates(ctx context.Context, limit int) ([]maintenanceVacuumCandidate, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			c.relname,
			COALESCE(st.n_live_tup, 0)::bigint,
			COALESCE(st.n_dead_tup, 0)::bigint,
			pg_total_relation_size(c.oid)::bigint,
			CASE
				WHEN COALESCE(st.n_live_tup, 0) + COALESCE(st.n_dead_tup, 0) = 0 THEN 0
				ELSE (COALESCE(st.n_dead_tup, 0)::numeric * 100.0 / (COALESCE(st.n_live_tup, 0) + COALESCE(st.n_dead_tup, 0)))
			END::float8 AS dead_pct
		FROM pg_stat_user_tables st
		JOIN pg_class c ON c.oid = st.relid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind IN ('r', 'p')
		  AND COALESCE(st.n_dead_tup, 0) >= 5000
		  AND (
			COALESCE(st.n_dead_tup, 0) >= 1000000
			OR (
				COALESCE(st.n_live_tup, 0) + COALESCE(st.n_dead_tup, 0) > 0
				AND COALESCE(st.n_dead_tup, 0) >= 50000
				AND COALESCE(st.n_dead_tup, 0)::numeric / (COALESCE(st.n_live_tup, 0) + COALESCE(st.n_dead_tup, 0)) >= 0.02
			)
			OR (
				pg_total_relation_size(c.oid) >= 1073741824
				AND COALESCE(st.n_dead_tup, 0) >= 250000
				AND COALESCE(st.n_live_tup, 0) + COALESCE(st.n_dead_tup, 0) > 0
				AND COALESCE(st.n_dead_tup, 0)::numeric / (COALESCE(st.n_live_tup, 0) + COALESCE(st.n_dead_tup, 0)) >= 0.01
			)
		  )
		ORDER BY COALESCE(st.n_dead_tup, 0) DESC, pg_total_relation_size(c.oid) DESC, c.relname
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := []maintenanceVacuumCandidate{}
	for rows.Next() {
		var candidate maintenanceVacuumCandidate
		if err := rows.Scan(&candidate.TableName, &candidate.LiveRows, &candidate.DeadRows, &candidate.TotalBytes, &candidate.DeadPct); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (s *Store) runRawStageRetentionTask(ctx context.Context, dryRun bool, batchSize int, policy RawStageRetentionPolicy) (*MaintenanceTaskResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if batchSize <= 0 {
		batchSize = 10000
	}
	result := &MaintenanceTaskResult{TaskKey: "raw_stage_retention", DryRun: dryRun}
	if dryRun {
		result.EstimatedRowsByTable = map[string]int64{}
	} else {
		result.DeletedRowsByTable = map[string]int64{}
	}
	tables := []string{
		"yenc_recovery_work_items",
		"article_headers",
		"article_header_ingest_payloads",
		"article_header_crosspost_groups",
		"article_header_poster_refs",
		"poster_materialization_queue",
	}
	if before, err := s.maintenanceStorageSnapshot(ctx, tables...); err == nil {
		result.BeforeStorage = before
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf("pre-retention storage snapshot failed: %v", err))
	}
	result.Warnings = append(result.Warnings,
		fmt.Sprintf("tier-aware raw retention uses hot=%dh warm=%dh cold=%dh terminal_yenc=%dh stale_probe=%dh", policy.HotHours, policy.WarmHours, policy.ColdHours, policy.DoneYEncHours, policy.FailedProbeHours),
		"ready/running yEnc work, assembled binary parts, release files, archive lineage, and running inspections are retained",
		"source headers are deleted only when no assembly queue, binary_parts, yEnc work item, or archive lineage still references them",
		"delete creates PostgreSQL reusable space but does not return OS space until VACUUM FULL, pg_repack, CLUSTER, or a table rewrite",
	)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin raw stage retention tx: %w", err)
	}
	defer rollbackTx(tx)

	if err := stageRawStageRetentionYEnc(ctx, tx, batchSize, policy); err != nil {
		return nil, err
	}
	if err := stageRawStageRetentionHeaders(ctx, tx, batchSize, policy); err != nil {
		return nil, err
	}
	counts, err := countRawStageRetentionRows(ctx, tx)
	if err != nil {
		return nil, err
	}
	if dryRun {
		for table, count := range counts {
			result.EstimatedRowsByTable[table] = count
		}
		return result, nil
	}

	var deletedYEnc int64
	if err := tx.QueryRowContext(ctx, `
		WITH deleted AS (
			DELETE FROM yenc_recovery_work_items wi
			USING tmp_raw_stage_retention_yenc_ids e
			WHERE wi.article_header_id = e.article_header_id
			RETURNING 1
		)
		SELECT COUNT(*) FROM deleted`).Scan(&deletedYEnc); err != nil {
		return nil, fmt.Errorf("delete raw retention yenc work items: %w", err)
	}
	counts["yenc_recovery_work_items"] = deletedYEnc

	var deletedHeaders int64
	if err := tx.QueryRowContext(ctx, `
		WITH deleted AS (
			DELETE FROM article_headers ah
			USING tmp_raw_stage_retention_header_ids e
			WHERE ah.id = e.article_header_id
			RETURNING 1
		)
		SELECT COUNT(*) FROM deleted`).Scan(&deletedHeaders); err != nil {
		return nil, fmt.Errorf("delete raw retention article headers: %w", err)
	}
	counts["article_headers"] = deletedHeaders

	for table, count := range counts {
		result.DeletedRowsByTable[table] = count
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit raw stage retention tx: %w", err)
	}
	if after, err := s.maintenanceStorageSnapshot(ctx, tables...); err == nil {
		result.AfterStorage = after
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf("post-retention storage snapshot failed: %v", err))
	}
	s.vacuumDeletedMaintenanceTables(ctx, result)
	return result, nil
}

func stageRawStageRetentionYEnc(ctx context.Context, tx *sql.Tx, batchSize int, policy RawStageRetentionPolicy) error {
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE tmp_raw_stage_retention_yenc_ids (article_header_id bigint PRIMARY KEY) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create raw retention yenc temp table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tmp_raw_stage_retention_yenc_ids (article_header_id)
		SELECT wi.article_header_id
		FROM yenc_recovery_work_items wi
		LEFT JOIN binary_recovery_current brc
		  ON brc.binary_id = wi.binary_id
		WHERE wi.status IN ('done', 'stale')
		  AND wi.updated_at < NOW() - (
		  	CASE WHEN wi.status = 'done' THEN $2::int ELSE $3::int END * INTERVAL '1 hour'
		  )
		  AND (wi.status <> 'done' OR COALESCE(brc.recovered_source, '') = 'yenc_header')
		  AND NOT EXISTS (SELECT 1 FROM release_files rf WHERE rf.binary_id = wi.binary_id)
		  AND NOT EXISTS (SELECT 1 FROM release_archive_lineage_binaries lb WHERE lb.binary_id = wi.binary_id)
		  AND NOT EXISTS (
		  	SELECT 1
		  	FROM binary_inspection_ready_queue rq
		  	WHERE rq.binary_id = wi.binary_id
		  	  AND rq.status = 'running'
		  )
		  AND NOT EXISTS (
		  	SELECT 1
		  	FROM binary_inspections bi
		  	WHERE bi.binary_id = wi.binary_id
		  	  AND bi.status = 'running'
		  )
		ORDER BY wi.updated_at, wi.article_header_id
		LIMIT $1`, batchSize, policy.DoneYEncHours, policy.FailedProbeHours); err != nil {
		return fmt.Errorf("stage raw retention yenc work items: %w", err)
	}
	return nil
}

func stageRawStageRetentionHeaders(ctx context.Context, tx *sql.Tx, batchSize int, policy RawStageRetentionPolicy) error {
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE tmp_raw_stage_retention_header_ids (article_header_id bigint PRIMARY KEY) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create raw retention header temp table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tmp_raw_stage_retention_header_ids (article_header_id)
		SELECT ah.id
		FROM article_headers ah
		LEFT JOIN indexer_group_profiles igp
		  ON igp.provider_id = ah.provider_id
		 AND igp.newsgroup_id = ah.newsgroup_id
		WHERE ah.date_utc < NOW() - (
			CASE COALESCE(NULLIF(igp.tier_override, ''), NULLIF(igp.tier, ''), 'warm')
				WHEN 'hot' THEN $2::int
				WHEN 'cold' THEN $4::int
				ELSE $3::int
			END * INTERVAL '1 hour'
		)
		  AND NOT EXISTS (SELECT 1 FROM article_header_assembly_queue q WHERE q.article_header_id = ah.id)
		  AND NOT EXISTS (SELECT 1 FROM binary_parts bp WHERE bp.article_header_id = ah.id)
		  AND NOT EXISTS (SELECT 1 FROM yenc_recovery_work_items wi WHERE wi.article_header_id = ah.id)
		  AND NOT EXISTS (SELECT 1 FROM release_archive_lineage_article_headers lah WHERE lah.article_header_id = ah.id)
		ORDER BY ah.date_utc, ah.id
		LIMIT $1`, batchSize, policy.HotHours, policy.WarmHours, policy.ColdHours); err != nil {
		return fmt.Errorf("stage raw retention article headers: %w", err)
	}
	return nil
}

func countRawStageRetentionRows(ctx context.Context, tx *sql.Tx) (map[string]int64, error) {
	queries := map[string]string{
		"yenc_recovery_work_items": `SELECT COUNT(*) FROM tmp_raw_stage_retention_yenc_ids`,
		"article_headers":          `SELECT COUNT(*) FROM tmp_raw_stage_retention_header_ids`,
		"article_header_ingest_payloads": `
			SELECT COUNT(*)
			FROM article_header_ingest_payloads p
			JOIN tmp_raw_stage_retention_header_ids e ON e.article_header_id = p.article_header_id`,
		"article_header_crosspost_groups": `
			SELECT COUNT(*)
			FROM article_header_crosspost_groups g
			JOIN tmp_raw_stage_retention_header_ids e ON e.article_header_id = g.article_header_id`,
		"article_header_poster_refs": `
			SELECT COUNT(*)
			FROM article_header_poster_refs r
			JOIN tmp_raw_stage_retention_header_ids e ON e.article_header_id = r.article_header_id`,
		"poster_materialization_queue": `
			SELECT COUNT(*)
			FROM poster_materialization_queue q
			JOIN tmp_raw_stage_retention_header_ids e ON e.article_header_id = q.article_header_id`,
	}
	counts := make(map[string]int64, len(queries))
	for table, query := range queries {
		var count int64
		if err := tx.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return nil, fmt.Errorf("count raw retention %s: %w", table, err)
		}
		counts[table] = count
	}
	return counts, nil
}

func (s *Store) runStaleNonreleaseSourcePurge(ctx context.Context, result *MaintenanceTaskResult, dryRun bool, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 1000
	}
	tables := []string{
		"article_headers",
		"article_header_ingest_payloads",
		"article_header_crosspost_groups",
		"article_header_poster_refs",
		"poster_materialization_queue",
	}
	before, err := s.maintenanceStorageSnapshot(ctx, tables...)
	if err != nil {
		return err
	}
	result.BeforeStorage = before
	result.Warnings = append(result.Warnings,
		fmt.Sprintf("high-risk source purge uses a fixed %d-day default retention window", staleNonreleaseSourceRetentionDays),
		"only headers with no assembly queue row, no binary_parts row, no yEnc work item, and no archive lineage are eligible",
		"deleting article_headers cascades associated ingest payload, crosspost, poster-ref, and poster queue rows",
		"dry-run and run are batch-limited; dry-run estimates only the next batch, not the full table",
		"delete creates PostgreSQL reusable space but does not return OS space until VACUUM FULL, pg_repack, CLUSTER, or a table rewrite",
	)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin stale source purge tx: %w", err)
	}
	defer rollbackTx(tx)

	if err := stageStaleNonreleaseSourceHeaders(ctx, tx, batchSize); err != nil {
		return err
	}
	counts, err := countStagedStaleSourceRows(ctx, tx)
	if err != nil {
		return err
	}
	if dryRun {
		for table, count := range counts {
			addMaintenanceTaskCount(result.EstimatedRowsByTable, table, count)
		}
		if before != nil {
			result.EstimatedBytes = before.TableTotalBytesByTable["article_headers"] +
				before.TableTotalBytesByTable["article_header_ingest_payloads"] +
				before.TableTotalBytesByTable["article_header_crosspost_groups"] +
				before.TableTotalBytesByTable["article_header_poster_refs"] +
				before.TableTotalBytesByTable["poster_materialization_queue"]
		}
		return nil
	}

	var deletedHeaders int64
	if err := tx.QueryRowContext(ctx, `
		WITH deleted AS (
			DELETE FROM article_headers ah
			USING tmp_stale_nonrelease_source_header_ids e
			WHERE ah.id = e.article_header_id
			RETURNING 1
		)
		SELECT COUNT(*) FROM deleted`).Scan(&deletedHeaders); err != nil {
		return fmt.Errorf("delete stale non-release source headers: %w", err)
	}
	counts["article_headers"] = deletedHeaders
	for table, count := range counts {
		addMaintenanceTaskCount(result.DeletedRowsByTable, table, count)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit stale source purge tx: %w", err)
	}
	if after, err := s.maintenanceStorageSnapshot(ctx, tables...); err == nil {
		result.AfterStorage = after
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf("post-delete storage snapshot failed: %v", err))
	}
	return nil
}

func stageStaleNonreleaseSourceHeaders(ctx context.Context, tx *sql.Tx, batchSize int) error {
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE tmp_stale_nonrelease_source_header_ids (article_header_id bigint PRIMARY KEY) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create stale source temp table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tmp_stale_nonrelease_source_header_ids (article_header_id)
		SELECT ah.id
		FROM article_headers ah
		WHERE ah.date_utc < NOW() - ($2::int * INTERVAL '1 day')
		  AND NOT EXISTS (SELECT 1 FROM article_header_assembly_queue q WHERE q.article_header_id = ah.id)
		  AND NOT EXISTS (SELECT 1 FROM binary_parts bp WHERE bp.article_header_id = ah.id)
		  AND NOT EXISTS (SELECT 1 FROM yenc_recovery_work_items wi WHERE wi.article_header_id = ah.id)
		  AND NOT EXISTS (SELECT 1 FROM release_archive_lineage_article_headers lah WHERE lah.article_header_id = ah.id)
		ORDER BY ah.date_utc, ah.id
		LIMIT $1`,
		batchSize,
		staleNonreleaseSourceRetentionDays,
	); err != nil {
		return fmt.Errorf("stage stale non-release source headers: %w", err)
	}
	return nil
}

func countStagedStaleSourceRows(ctx context.Context, tx *sql.Tx) (map[string]int64, error) {
	queries := map[string]string{
		"article_headers": `SELECT COUNT(*) FROM tmp_stale_nonrelease_source_header_ids`,
		"article_header_ingest_payloads": `
			SELECT COUNT(*)
			FROM article_header_ingest_payloads p
			JOIN tmp_stale_nonrelease_source_header_ids e ON e.article_header_id = p.article_header_id`,
		"article_header_crosspost_groups": `
			SELECT COUNT(*)
			FROM article_header_crosspost_groups g
			JOIN tmp_stale_nonrelease_source_header_ids e ON e.article_header_id = g.article_header_id`,
		"article_header_poster_refs": `
			SELECT COUNT(*)
			FROM article_header_poster_refs r
			JOIN tmp_stale_nonrelease_source_header_ids e ON e.article_header_id = r.article_header_id`,
		"poster_materialization_queue": `
			SELECT COUNT(*)
			FROM poster_materialization_queue q
			JOIN tmp_stale_nonrelease_source_header_ids e ON e.article_header_id = q.article_header_id`,
	}
	counts := make(map[string]int64, len(queries))
	for table, query := range queries {
		var count int64
		if err := tx.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return nil, fmt.Errorf("count staged stale source %s: %w", table, err)
		}
		counts[table] = count
	}
	return counts, nil
}

func (s *Store) runEmergencySourceWindowReset(ctx context.Context, result *MaintenanceTaskResult, dryRun bool, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 10000
	}
	tables := []string{
		"binary_core",
		"binary_parts",
		"binary_identity_current",
		"binary_observation_stats",
		"binary_recovery_current",
		"binary_grouping_evidence",
		"binary_inspection_ready_queue",
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
		"yenc_recovery_work_items",
		"article_headers",
		"article_header_ingest_payloads",
		"article_header_crosspost_groups",
		"article_header_poster_refs",
		"poster_materialization_queue",
	}
	before, err := s.maintenanceStorageSnapshot(ctx, tables...)
	if err != nil {
		return err
	}
	result.BeforeStorage = before
	result.Warnings = append(result.Warnings,
		fmt.Sprintf("high-risk emergency reset intentionally discards unreleased binary/source work outside the fixed %d-day retention window", staleNonreleaseSourceRetentionDays),
		"eligible binaries are skipped when release_files, release archive lineage, running inspect work, or running yEnc work references them",
		"deleting binary_core cascades binary parts, yEnc work items, identity/current projections, inspection rows, and other binary-derived rows",
		"after binary deletion, only fully orphaned old article headers are deleted; release catalog files, release detail, archive metadata, and archived NZBs are not deleted",
		"dry-run estimates staged binary cascades without deleting; source-header cleanup is estimated separately by stale_nonrelease_source_purge",
		"future releases cannot be formed from source/binary rows removed by this task",
		"delete creates PostgreSQL reusable space but does not return OS space until VACUUM FULL, pg_repack, CLUSTER, or a table rewrite",
	)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin emergency source window reset tx: %w", err)
	}
	defer rollbackTx(tx)

	if err := stageEmergencySourceWindowBinaries(ctx, tx, batchSize); err != nil {
		return err
	}
	binaryCounts, err := countStagedEmergencyBinaryRows(ctx, tx)
	if err != nil {
		return err
	}
	for table, count := range binaryCounts {
		addMaintenanceTaskCount(resultCountMap(result, dryRun), table, count)
	}
	if dryRun {
		if before != nil {
			for _, table := range tables {
				result.EstimatedBytes += before.TableTotalBytesByTable[table]
			}
		}
		result.Warnings = append(result.Warnings, "dry-run estimates binary cascades only; run stale_nonrelease_source_purge dry-run for source-header-only estimates, and expect a real emergency run to delete newly orphaned old headers after binary cascades")
		return nil
	}

	var deletedBinaries int64
	if err := tx.QueryRowContext(ctx, `
		WITH deleted AS (
			DELETE FROM binary_core bc
			USING tmp_emergency_source_window_binary_ids e
			WHERE bc.binary_id = e.binary_id
			RETURNING 1
		)
		SELECT COUNT(*) FROM deleted`).Scan(&deletedBinaries); err != nil {
		return fmt.Errorf("delete emergency source window binaries: %w", err)
	}
	if deletedBinaries != binaryCounts["binary_core"] {
		addMaintenanceTaskCount(resultCountMap(result, dryRun), "binary_core_delete_delta", deletedBinaries-binaryCounts["binary_core"])
	}

	if err := stageStaleNonreleaseSourceHeaders(ctx, tx, batchSize); err != nil {
		return err
	}
	sourceCounts, err := countStagedStaleSourceRows(ctx, tx)
	if err != nil {
		return err
	}
	for table, count := range sourceCounts {
		addMaintenanceTaskCount(resultCountMap(result, dryRun), table, count)
	}

	var deletedHeaders int64
	if err := tx.QueryRowContext(ctx, `
		WITH deleted AS (
			DELETE FROM article_headers ah
			USING tmp_stale_nonrelease_source_header_ids e
			WHERE ah.id = e.article_header_id
			RETURNING 1
		)
		SELECT COUNT(*) FROM deleted`).Scan(&deletedHeaders); err != nil {
		return fmt.Errorf("delete emergency stale source headers: %w", err)
	}
	if deletedHeaders != sourceCounts["article_headers"] {
		addMaintenanceTaskCount(resultCountMap(result, dryRun), "article_headers_delete_delta", deletedHeaders-sourceCounts["article_headers"])
	}

	if before != nil {
		for _, table := range tables {
			result.EstimatedBytes += before.TableTotalBytesByTable[table]
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit emergency source window reset tx: %w", err)
	}
	if after, err := s.maintenanceStorageSnapshot(ctx, tables...); err == nil {
		result.AfterStorage = after
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf("post-delete storage snapshot failed: %v", err))
	}
	return nil
}

func resultCountMap(result *MaintenanceTaskResult, dryRun bool) map[string]int64 {
	if dryRun {
		return result.EstimatedRowsByTable
	}
	return result.DeletedRowsByTable
}

func stageEmergencySourceWindowBinaries(ctx context.Context, tx *sql.Tx, batchSize int) error {
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE tmp_emergency_source_window_binary_ids (binary_id bigint PRIMARY KEY) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create emergency source window binary temp table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE tmp_emergency_source_window_candidate_binary_ids (binary_id bigint PRIMARY KEY) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create emergency source window candidate temp table: %w", err)
	}
	candidateLimit := batchSize * 4
	if candidateLimit < batchSize {
		candidateLimit = batchSize
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tmp_emergency_source_window_candidate_binary_ids (binary_id)
		SELECT bos.binary_id
		FROM binary_observation_stats bos
		WHERE bos.posted_at < NOW() - ($2::int * INTERVAL '1 day')
		ORDER BY bos.binary_id
		LIMIT $1`,
		candidateLimit,
		staleNonreleaseSourceRetentionDays,
	); err != nil {
		return fmt.Errorf("stage emergency source window binary candidates: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tmp_emergency_source_window_binary_ids (binary_id)
		SELECT c.binary_id
		FROM tmp_emergency_source_window_candidate_binary_ids c
		WHERE EXISTS (SELECT 1 FROM binary_core bc WHERE bc.binary_id = c.binary_id)
		  AND NOT EXISTS (SELECT 1 FROM release_files rf WHERE rf.binary_id = c.binary_id)
		  AND NOT EXISTS (SELECT 1 FROM release_archive_lineage_binaries lb WHERE lb.binary_id = c.binary_id)
		  AND NOT EXISTS (SELECT 1 FROM binary_inspection_ready_queue rq WHERE rq.binary_id = c.binary_id AND rq.status = 'running')
		  AND NOT EXISTS (SELECT 1 FROM binary_inspections bi WHERE bi.binary_id = c.binary_id AND bi.status = 'running')
		  AND NOT EXISTS (SELECT 1 FROM yenc_recovery_work_items wi WHERE wi.binary_id = c.binary_id AND wi.status = 'running')
		ORDER BY c.binary_id
		LIMIT $1`,
		batchSize,
	); err != nil {
		return fmt.Errorf("filter emergency source window binaries: %w", err)
	}
	return nil
}

func countStagedEmergencyBinaryRows(ctx context.Context, tx *sql.Tx) (map[string]int64, error) {
	queries := map[string]string{
		"binary_core":                   `SELECT COUNT(*) FROM tmp_emergency_source_window_binary_ids`,
		"binary_parts":                  `SELECT COUNT(*) FROM binary_parts t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_identity_current":       `SELECT COUNT(*) FROM binary_identity_current t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_observation_stats":      `SELECT COUNT(*) FROM binary_observation_stats t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_recovery_current":       `SELECT COUNT(*) FROM binary_recovery_current t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_grouping_evidence":      `SELECT COUNT(*) FROM binary_grouping_evidence t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_inspection_ready_queue": `SELECT COUNT(*) FROM binary_inspection_ready_queue t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_inspections":            `SELECT COUNT(*) FROM binary_inspections t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_inspection_artifacts":   `SELECT COUNT(*) FROM binary_inspection_artifacts t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_archive_entries":        `SELECT COUNT(*) FROM binary_archive_entries t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_text_evidence":          `SELECT COUNT(*) FROM binary_text_evidence t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_media_streams":          `SELECT COUNT(*) FROM binary_media_streams t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_par2_sets":              `SELECT COUNT(*) FROM binary_par2_sets t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_par2_targets":           `SELECT COUNT(*) FROM binary_par2_targets t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_completion_keys":        `SELECT COUNT(*) FROM binary_completion_keys t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_lifecycle":              `SELECT COUNT(*) FROM binary_lifecycle t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_projection_events":      `SELECT COUNT(*) FROM binary_projection_events t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
		"binary_superseded_sources": `
			SELECT COUNT(*)
			FROM (
				SELECT t.source_binary_id
				FROM binary_superseded_sources t
				JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.source_binary_id
				UNION
				SELECT t.source_binary_id
				FROM binary_superseded_sources t
				JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.target_binary_id
			) rows`,
		"yenc_recovery_work_items": `SELECT COUNT(*) FROM yenc_recovery_work_items t JOIN tmp_emergency_source_window_binary_ids e ON e.binary_id = t.binary_id`,
	}
	counts := make(map[string]int64, len(queries))
	for table, query := range queries {
		var count int64
		if err := tx.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return nil, fmt.Errorf("count staged emergency binary %s: %w", table, err)
		}
		counts[table] = count
	}
	return counts, nil
}

func addMaintenanceTaskCount(counts map[string]int64, table string, count int64) {
	if counts == nil {
		return
	}
	counts[table] += count
}

func (s *Store) vacuumDeletedMaintenanceTables(ctx context.Context, result *MaintenanceTaskResult) {
	if s == nil || s.db == nil || result == nil || len(result.DeletedRowsByTable) == 0 {
		return
	}
	for _, table := range sortedDeletedMaintenanceTables(result.DeletedRowsByTable) {
		if _, err := s.db.ExecContext(ctx, `VACUUM (ANALYZE) `+quoteIdentifier(table)); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("post-delete VACUUM (ANALYZE) failed for %s: %v", table, err))
			continue
		}
		result.VacuumedTables = append(result.VacuumedTables, table)
	}
	if len(result.VacuumedTables) > 0 {
		result.Warnings = append(result.Warnings, "post-delete VACUUM (ANALYZE) completed for affected tables; this makes space reusable inside PostgreSQL but does not return OS disk space")
		if snapshot, err := s.maintenanceStorageSnapshot(ctx, result.VacuumedTables...); err == nil {
			result.AfterStorage = snapshot
		} else {
			result.Warnings = append(result.Warnings, fmt.Sprintf("post-vacuum storage snapshot failed: %v", err))
		}
	}
}

func sortedDeletedMaintenanceTables(deleted map[string]int64) []string {
	tables := make([]string, 0, len(deleted))
	for table, count := range deleted {
		if count <= 0 || !isVacuumableMaintenanceTable(table) {
			continue
		}
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables
}

func isVacuumableMaintenanceTable(table string) bool {
	switch table {
	case "article_header_assembly_queue",
		"article_headers",
		"article_header_crosspost_groups",
		"article_header_ingest_payloads",
		"binary_archive_entries",
		"binary_completion_keys",
		"binary_core",
		"binary_grouping_evidence",
		"binary_identity_current",
		"binary_inspection_artifacts",
		"binary_inspection_ready_queue",
		"binary_inspections",
		"binary_lifecycle",
		"binary_media_streams",
		"binary_observation_stats",
		"binary_par2_sets",
		"binary_par2_targets",
		"binary_projection_events",
		"binary_recovery_current",
		"binary_superseded_sources",
		"binary_parts",
		"binary_text_evidence",
		"indexer_stage_runs",
		"nzb_cache",
		"poster_materialization_queue",
		"release_archive_lineage_article_headers",
		"release_archive_lineage_binaries",
		"release_files",
		"release_newsgroups",
		"scrape_runs",
		"yenc_recovery_work_items":
		return true
	default:
		return false
	}
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func countRowsFrom(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, from string, args ...any) (int64, error) {
	var count int64
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) `+from, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) maintenanceStorageSnapshot(ctx context.Context, tables ...string) (*MaintenanceTaskStorageSnapshot, error) {
	status, err := s.DatabaseStorageStatus(ctx)
	if err != nil {
		return nil, err
	}
	snapshot := &MaintenanceTaskStorageSnapshot{
		GeneratedAt:            time.Now().UTC(),
		DatabaseBytes:          status.DatabaseBytes,
		DataDirectory:          status.DataDirectory,
		FilesystemFreeBytes:    status.FilesystemFreeBytes,
		FilesystemTotalBytes:   status.FilesystemTotalBytes,
		FilesystemFreePercent:  status.FilesystemFreePercent,
		FilesystemVisible:      status.FilesystemVisible,
		TableTotalBytesByTable: map[string]int64{},
		TableLiveRowsByTable:   map[string]int64{},
		TableDeadRowsByTable:   map[string]int64{},
	}
	for _, table := range tables {
		if table == "" {
			continue
		}
		var totalBytes, liveRows, deadRows int64
		if err := s.db.QueryRowContext(ctx, `
			SELECT
				pg_total_relation_size(c.oid)::bigint,
				COALESCE(st.n_live_tup, 0)::bigint,
				COALESCE(st.n_dead_tup, 0)::bigint
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			LEFT JOIN pg_stat_user_tables st ON st.relid = c.oid
			WHERE n.nspname = 'public'
			  AND c.relname = $1`,
			table,
		).Scan(&totalBytes, &liveRows, &deadRows); err != nil {
			return nil, fmt.Errorf("read maintenance storage snapshot for %s: %w", table, err)
		}
		snapshot.TableTotalBytesByTable[table] = totalBytes
		snapshot.TableLiveRowsByTable[table] = liveRows
		snapshot.TableDeadRowsByTable[table] = deadRows
	}
	return snapshot, nil
}
