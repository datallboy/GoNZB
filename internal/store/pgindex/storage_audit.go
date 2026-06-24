package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type IndexerStorageAuditReport struct {
	GeneratedAt   time.Time                     `json:"generated_at"`
	Tables        []IndexerStorageAuditTable    `json:"tables"`
	Indexes       []IndexerStorageAuditIndex    `json:"indexes"`
	SourceAges    []IndexerStorageAuditAgeRange `json:"source_ages"`
	SourceWindows []IndexerSourceWindowAudit    `json:"source_windows,omitempty"`
	YEncBacklog   []IndexerYEncBacklogAudit     `json:"yenc_backlog,omitempty"`
	GuardCounts   []IndexerStorageGuardCount    `json:"guard_counts"`
	CleanupMatrix []IndexerStorageCleanupAudit  `json:"cleanup_matrix"`
}

type IndexerStorageAuditTable struct {
	TableName       string     `json:"table_name"`
	RowEstimate     int64      `json:"row_estimate"`
	TotalBytes      int64      `json:"total_bytes"`
	TableBytes      int64      `json:"table_bytes"`
	IndexBytes      int64      `json:"index_bytes"`
	TOASTBytes      int64      `json:"toast_bytes"`
	DeadTuples      int64      `json:"dead_tuples"`
	LastVacuum      *time.Time `json:"last_vacuum,omitempty"`
	LastAutovacuum  *time.Time `json:"last_autovacuum,omitempty"`
	LastAnalyze     *time.Time `json:"last_analyze,omitempty"`
	LastAutoAnalyze *time.Time `json:"last_autoanalyze,omitempty"`
}

type IndexerStorageAuditIndex struct {
	TableName   string `json:"table_name"`
	IndexName   string `json:"index_name"`
	IndexBytes  int64  `json:"index_bytes"`
	Scans       int64  `json:"scans"`
	TuplesRead  int64  `json:"tuples_read"`
	TuplesFetch int64  `json:"tuples_fetch"`
	Primary     bool   `json:"primary"`
	Unique      bool   `json:"unique"`
}

type IndexerStorageAuditAgeRange struct {
	Scope     string `json:"scope"`
	Bucket    string `json:"bucket"`
	Rows      int64  `json:"rows"`
	Risk      string `json:"risk"`
	DataUse   string `json:"data_use"`
	PurgeNote string `json:"purge_note"`
}

type IndexerStorageCleanupAudit struct {
	TaskKey              string           `json:"task_key"`
	Label                string           `json:"label"`
	Risk                 string           `json:"risk"`
	Implemented          bool             `json:"implemented"`
	EstimatedRowsByTable map[string]int64 `json:"estimated_rows_by_table,omitempty"`
	SpaceEffect          string           `json:"space_effect"`
	SupervisorEffect     string           `json:"supervisor_effect"`
	DataEffect           string           `json:"data_effect"`
	ReleaseSafety        string           `json:"release_safety"`
}

type IndexerStorageGuardCount struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Rows  int64  `json:"rows"`
	Risk  string `json:"risk"`
	Notes string `json:"notes"`
}

type IndexerSourceWindowAudit struct {
	Bucket         string `json:"bucket"`
	Headers        int64  `json:"headers"`
	Payloads       int64  `json:"payloads"`
	AssemblyQueue  int64  `json:"assembly_queue"`
	BinaryParts    int64  `json:"binary_parts"`
	YEncWorkItems  int64  `json:"yenc_work_items"`
	ArchiveLineage int64  `json:"archive_lineage"`
	OrphanHeaders  int64  `json:"orphan_headers"`
	Risk           string `json:"risk"`
	Notes          string `json:"notes"`
}

type IndexerYEncBacklogAudit struct {
	Bucket          string `json:"bucket"`
	Status          string `json:"status"`
	PriorityRank    int    `json:"priority_rank"`
	ReadinessBucket string `json:"readiness_bucket"`
	Rows            int64  `json:"rows"`
	BlockingRows    int64  `json:"blocking_rows"`
	OldestDate      string `json:"oldest_date,omitempty"`
	NewestDate      string `json:"newest_date,omitempty"`
	Notes           string `json:"notes"`
}

func (s *Store) GetIndexerStorageAudit(ctx context.Context) (*IndexerStorageAuditReport, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	report := &IndexerStorageAuditReport{GeneratedAt: time.Now().UTC()}
	tables, err := s.queryStorageAuditTables(ctx)
	if err != nil {
		return nil, err
	}
	report.Tables = tables
	indexes, err := s.queryStorageAuditIndexes(ctx)
	if err != nil {
		return nil, err
	}
	report.Indexes = indexes
	ages, err := s.queryStorageAuditSourceAges(ctx)
	if err != nil {
		return nil, err
	}
	report.SourceAges = ages
	sourceWindows, err := s.queryStorageSourceWindowAudit(ctx)
	if err != nil {
		return nil, err
	}
	report.SourceWindows = sourceWindows
	yencBacklog, err := s.queryStorageYEncBacklogAudit(ctx)
	if err != nil {
		return nil, err
	}
	report.YEncBacklog = yencBacklog
	guards, err := s.queryStorageAuditGuardCounts(ctx)
	if err != nil {
		return nil, err
	}
	report.GuardCounts = guards
	matrix, err := s.queryStorageCleanupMatrix(ctx)
	if err != nil {
		return nil, err
	}
	report.CleanupMatrix = matrix
	return report, nil
}

func (s *Store) queryStorageAuditTables(ctx context.Context) ([]IndexerStorageAuditTable, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			c.relname,
			COALESCE(st.n_live_tup, c.reltuples, 0)::bigint,
			pg_total_relation_size(c.oid)::bigint,
			pg_relation_size(c.oid)::bigint,
			pg_indexes_size(c.oid)::bigint,
			GREATEST(pg_total_relation_size(c.oid) - pg_relation_size(c.oid) - pg_indexes_size(c.oid), 0)::bigint,
			COALESCE(st.n_dead_tup, 0)::bigint,
			st.last_vacuum,
			st.last_autovacuum,
			st.last_analyze,
			st.last_autoanalyze
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_stat_user_tables st ON st.relid = c.oid
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		ORDER BY pg_total_relation_size(c.oid) DESC
		LIMIT 30`)
	if err != nil {
		return nil, fmt.Errorf("query storage audit tables: %w", err)
	}
	defer rows.Close()

	out := []IndexerStorageAuditTable{}
	for rows.Next() {
		var item IndexerStorageAuditTable
		var lastVacuum, lastAutovacuum, lastAnalyze, lastAutoAnalyze sql.NullTime
		if err := rows.Scan(
			&item.TableName,
			&item.RowEstimate,
			&item.TotalBytes,
			&item.TableBytes,
			&item.IndexBytes,
			&item.TOASTBytes,
			&item.DeadTuples,
			&lastVacuum,
			&lastAutovacuum,
			&lastAnalyze,
			&lastAutoAnalyze,
		); err != nil {
			return nil, fmt.Errorf("scan storage audit table: %w", err)
		}
		item.LastVacuum = nullTimePtr(lastVacuum)
		item.LastAutovacuum = nullTimePtr(lastAutovacuum)
		item.LastAnalyze = nullTimePtr(lastAnalyze)
		item.LastAutoAnalyze = nullTimePtr(lastAutoAnalyze)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate storage audit tables: %w", err)
	}
	return out, nil
}

func (s *Store) queryStorageAuditIndexes(ctx context.Context) ([]IndexerStorageAuditIndex, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			t.relname,
			i.relname,
			pg_relation_size(i.oid)::bigint,
			COALESCE(st.idx_scan, 0)::bigint,
			COALESCE(st.idx_tup_read, 0)::bigint,
			COALESCE(st.idx_tup_fetch, 0)::bigint,
			ix.indisprimary,
			ix.indisunique
		FROM pg_class i
		JOIN pg_index ix ON ix.indexrelid = i.oid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		LEFT JOIN pg_stat_user_indexes st ON st.indexrelid = i.oid
		WHERE n.nspname = 'public'
		ORDER BY pg_relation_size(i.oid) DESC
		LIMIT 30`)
	if err != nil {
		return nil, fmt.Errorf("query storage audit indexes: %w", err)
	}
	defer rows.Close()

	out := []IndexerStorageAuditIndex{}
	for rows.Next() {
		var item IndexerStorageAuditIndex
		if err := rows.Scan(&item.TableName, &item.IndexName, &item.IndexBytes, &item.Scans, &item.TuplesRead, &item.TuplesFetch, &item.Primary, &item.Unique); err != nil {
			return nil, fmt.Errorf("scan storage audit index: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate storage audit indexes: %w", err)
	}
	return out, nil
}

func (s *Store) queryStorageAuditSourceAges(ctx context.Context) ([]IndexerStorageAuditAgeRange, error) {
	specs := []struct {
		scope     string
		query     string
		risk      string
		dataUse   string
		purgeNote string
	}{
		{
			scope: "Scraped headers",
			query: `
				SELECT ` + storageAuditAgeBucketSQL("ah.date_utc") + `, COUNT(*)
				FROM article_headers ah
				WHERE ah.date_utc IS NOT NULL
				GROUP BY 1`,
			risk:      "high",
			dataUse:   "Canonical source article locators referenced by queues, binary parts, yEnc work, release files, and archive lineage.",
			purgeNote: "Do not infer assembly state from assembled_at; use queue, binary_parts, yEnc, release, and archive guards.",
		},
		{
			scope:     "Retained ingest payloads",
			query:     `SELECT 'disabled'::text, 0::bigint`,
			risk:      "high",
			dataUse:   "Raw subject/poster/xref and parsed filename/yEnc hints for assemble, recovery, poster/crosspost, and debugging.",
			purgeNote: "Exact age bucketing is disabled in the UI audit because it requires a large header join; use targeted CLI SQL before any purge.",
		},
		{
			scope:     "Binary observation rows",
			query:     `SELECT 'disabled'::text, 0::bigint`,
			risk:      "high",
			dataUse:   "Release family formation, public release detail, archive lineage, and inspection joins.",
			purgeNote: "Exact age bucketing is disabled in the UI audit; only terminal release source purge should delete binary roots after catalog/NZB gates pass.",
		},
	}

	out := []IndexerStorageAuditAgeRange{}
	for _, spec := range specs {
		rows, err := s.db.QueryContext(ctx, spec.query)
		if err != nil {
			return nil, fmt.Errorf("query storage source age %s: %w", spec.scope, err)
		}
		for rows.Next() {
			var item IndexerStorageAuditAgeRange
			if err := rows.Scan(&item.Bucket, &item.Rows); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan storage source age %s: %w", spec.scope, err)
			}
			item.Scope = spec.scope
			item.Risk = spec.risk
			item.DataUse = spec.dataUse
			item.PurgeNote = spec.purgeNote
			out = append(out, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate storage source age %s: %w", spec.scope, err)
		}
		rows.Close()
	}
	return out, nil
}

func (s *Store) queryStorageSourceWindowAudit(ctx context.Context) ([]IndexerSourceWindowAudit, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH header_buckets AS (
			SELECT
				`+storageAuditAgeBucketSQL("ah.date_utc")+` AS bucket
			FROM article_headers ah
			WHERE ah.date_utc IS NOT NULL
		),
		bucket_totals AS (
		SELECT
			hb.bucket AS bucket,
			COUNT(*)::bigint AS headers,
			0::bigint AS payloads,
			0::bigint AS assembly_queue,
			0::bigint AS binary_parts,
			0::bigint AS yenc_work_items,
			0::bigint AS archive_lineage,
			0::bigint AS orphan_headers
		FROM header_buckets hb
		GROUP BY hb.bucket
		)
		SELECT
			bucket,
			headers,
			payloads,
			assembly_queue,
			binary_parts,
			yenc_work_items,
			archive_lineage,
			orphan_headers
		FROM bucket_totals
		ORDER BY `+storageAuditBucketOrderSQL("bucket"))
	if err != nil {
		return nil, fmt.Errorf("query source window audit: %w", err)
	}
	defer rows.Close()

	out := []IndexerSourceWindowAudit{}
	for rows.Next() {
		var item IndexerSourceWindowAudit
		if err := rows.Scan(&item.Bucket, &item.Headers, &item.Payloads, &item.AssemblyQueue, &item.BinaryParts, &item.YEncWorkItems, &item.ArchiveLineage, &item.OrphanHeaders); err != nil {
			return nil, fmt.Errorf("scan source window audit: %w", err)
		}
		item.Risk = "high"
		item.Notes = "Audit-only. Header counts are exact; relationship counts are disabled until implemented with bounded/index-friendly queries."
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source window audit: %w", err)
	}
	return out, nil
}

func (s *Store) queryStorageYEncBacklogAudit(ctx context.Context) ([]IndexerYEncBacklogAudit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			`+storageAuditAgeBucketSQL("wi.date_utc")+` AS bucket,
			wi.status,
			wi.priority_rank,
			COALESCE(NULLIF(wi.current_readiness_bucket, ''), '(none)') AS readiness_bucket,
			COUNT(*)::bigint AS rows,
			COUNT(*) FILTER (
				WHERE wi.status IN ('ready', 'running')
				  AND (
					wi.priority_rank = 0
					OR wi.current_readiness_bucket IN ('overgrouped_contextual', 'weak_single_binary', 'weak_obfuscated_set')
				  )
			)::bigint AS blocking_rows,
			MIN(wi.date_utc),
			MAX(wi.date_utc)
		FROM yenc_recovery_work_items wi
		WHERE wi.date_utc IS NOT NULL
		GROUP BY bucket, wi.status, wi.priority_rank, readiness_bucket
		ORDER BY `+storageAuditBucketOrderSQL(storageAuditAgeBucketSQL("wi.date_utc"))+`, wi.status, wi.priority_rank, readiness_bucket`)
	if err != nil {
		return nil, fmt.Errorf("query yenc backlog audit: %w", err)
	}
	defer rows.Close()

	out := []IndexerYEncBacklogAudit{}
	for rows.Next() {
		var item IndexerYEncBacklogAudit
		var oldest, newest sql.NullTime
		if err := rows.Scan(&item.Bucket, &item.Status, &item.PriorityRank, &item.ReadinessBucket, &item.Rows, &item.BlockingRows, &oldest, &newest); err != nil {
			return nil, fmt.Errorf("scan yenc backlog audit: %w", err)
		}
		if oldest.Valid {
			item.OldestDate = oldest.Time.UTC().Format(time.RFC3339)
		}
		if newest.Valid {
			item.NewestDate = newest.Time.UTC().Format(time.RFC3339)
		}
		item.Notes = "BODY requests should stay in recover_yenc and prioritize active-window or release-blocking rows."
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate yenc backlog audit: %w", err)
	}
	return out, nil
}

func storageAuditAgeBucketSQL(column string) string {
	return `CASE
		WHEN ` + column + ` >= NOW() - INTERVAL '24 hours' THEN '<24h'
		WHEN ` + column + ` >= NOW() - INTERVAL '72 hours' THEN '24-72h'
		WHEN ` + column + ` >= NOW() - INTERVAL '7 days' THEN '72h-7d'
		WHEN ` + column + ` >= NOW() - INTERVAL '30 days' THEN '7d-30d'
		ELSE '>30d'
	END`
}

func storageAuditBucketOrderSQL(expr string) string {
	return `CASE ` + expr + `
		WHEN '<24h' THEN 1
		WHEN '24-72h' THEN 2
		WHEN '72h-7d' THEN 3
		WHEN '7d-30d' THEN 4
		ELSE 5
	END`
}

func (s *Store) queryStorageAuditGuardCounts(ctx context.Context) ([]IndexerStorageGuardCount, error) {
	specs := []struct {
		key   string
		label string
		risk  string
		notes string
		query string
	}{
		{
			key:   "source.headers_older_72h",
			label: "Headers older than 72h",
			risk:  "high",
			notes: "All old source headers. This is not a purge candidate count because many rows are referenced by binary_parts and release/archive data.",
			query: `SELECT COUNT(*) FROM article_headers WHERE date_utc < NOW() - INTERVAL '72 hours'`,
		},
		{
			key:   "source.payloads_older_72h",
			label: "Payloads older than 72h",
			risk:  "high",
			notes: "Disabled in the UI audit because exact age bucketing requires a large header join. Payloads feed assemble, yEnc, poster/crosspost, and debug paths.",
			query: `SELECT 0::bigint`,
		},
		{
			key:   "source.orphan_headers_older_72h_audit_only",
			label: "Orphan headers older than 72h audit-only",
			risk:  "high",
			notes: "Disabled until the stale-source predicate is rewritten as a bounded/index-friendly estimator. No delete task is implemented.",
			query: `SELECT 0::bigint`,
		},
		{
			key:   "source.payloads_with_yenc_retry_markers",
			label: "Payloads with yEnc retry markers",
			risk:  "blocker",
			notes: "Payload rows with recovery retry/missing state are not liberal purge candidates.",
			query: `SELECT COUNT(*) FROM article_header_ingest_payloads WHERE yenc_recovery_missing_count > 0 OR yenc_recovery_last_missing_at IS NOT NULL OR yenc_recovery_retry_after IS NOT NULL`,
		},
		{
			key:   "source.assembled_payloads_existing_purge_predicate",
			label: "Legacy assembled_at payload purge predicate",
			risk:  "high",
			notes: "Legacy predicate only. assembled_at is not authoritative in the current queue-based assembly path.",
			query: storageAuditHeaderPayloadPurgeCountSQL(),
		},
		{
			key:   "release.archive_purge_pending_with_object",
			label: "Archive purge-pending releases with object key",
			risk:  "high",
			notes: "Approximate release source purge pool before readiness/catalog/inspection gates.",
			query: `SELECT COUNT(*) FROM release_archive_state WHERE archive_status = 'purge_pending' AND COALESCE(object_key, '') <> ''`,
		},
		{
			key:   "release.archive_purge_pending_missing_catalog",
			label: "Purge-pending releases missing catalog files",
			risk:  "blocker",
			notes: "Source purge must preserve durable catalog data; these are not eligible.",
			query: `SELECT COUNT(*) FROM release_archive_state ras WHERE archive_status = 'purge_pending' AND NOT EXISTS (SELECT 1 FROM release_catalog_files cf WHERE cf.release_id = ras.release_id)`,
		},
		{
			key:   "release.archive_purge_pending_missing_media_inspect",
			label: "Purge-pending releases missing completed media inspect",
			risk:  "blocker",
			notes: "Current purge gate requires completed media inspection.",
			query: `
				SELECT COUNT(*)
				FROM release_archive_state ras
				WHERE ras.archive_status = 'purge_pending'
				  AND NOT EXISTS (
					SELECT 1
					FROM release_files rf
					JOIN binary_inspections bi
					  ON bi.binary_id = rf.binary_id
					 AND bi.stage_name = 'inspect_media'
					 AND bi.status = 'completed'
					WHERE rf.release_id = ras.release_id
				  )`,
		},
		{
			key:   "guard.running_stage_runs",
			label: "Running stage runs",
			risk:  "blocker",
			notes: "Skip source cleanup while related stage work is running.",
			query: `SELECT COUNT(*) FROM indexer_stage_runs WHERE status = 'running'`,
		},
		{
			key:   "guard.assembly_queue_claimed_headers",
			label: "Active assemble queue claims",
			risk:  "blocker",
			notes: "Skip source cleanup for actively claimed assembly queue rows.",
			query: `SELECT COUNT(*) FROM article_header_assembly_queue WHERE claim_until > NOW()`,
		},
		{
			key:   "guard.inspect_ready_running",
			label: "Running inspect ready rows",
			risk:  "blocker",
			notes: "Skip source cleanup for active inspect queue work.",
			query: `SELECT COUNT(*) FROM binary_inspection_ready_queue WHERE status = 'running'`,
		},
		{
			key:   "guard.yenc_running",
			label: "Running yEnc recovery rows",
			risk:  "blocker",
			notes: "Skip source cleanup for active yEnc recovery.",
			query: `SELECT COUNT(*) FROM yenc_recovery_work_items WHERE status = 'running'`,
		},
	}

	out := make([]IndexerStorageGuardCount, 0, len(specs))
	for _, spec := range specs {
		var rows int64
		if err := s.db.QueryRowContext(ctx, spec.query).Scan(&rows); err != nil {
			out = append(out, IndexerStorageGuardCount{
				Key:   spec.key,
				Label: spec.label,
				Rows:  -1,
				Risk:  spec.risk,
				Notes: fmt.Sprintf("%s Audit count unavailable: %v", spec.notes, err),
			})
			continue
		}
		out = append(out, IndexerStorageGuardCount{
			Key:   spec.key,
			Label: spec.label,
			Rows:  rows,
			Risk:  spec.risk,
			Notes: spec.notes,
		})
	}
	return out, nil
}

func (s *Store) queryStorageCleanupMatrix(ctx context.Context) ([]IndexerStorageCleanupAudit, error) {
	items := []IndexerStorageCleanupAudit{
		{
			TaskKey:          "vacuum_dead_tuple_tables",
			Label:            "Vacuum dead tuple tables",
			Risk:             "low",
			Implemented:      true,
			SpaceEffect:      "Makes dead-tuple space reusable inside PostgreSQL and refreshes planner stats; OS space is not returned until vacuum full/table rewrite.",
			SupervisorEffect: "No pipeline work is enqueued. Running vacuum can add I/O on the selected tables.",
			DataEffect:       "Deletes no application rows; updates PostgreSQL free-space maps and planner statistics.",
			ReleaseSafety:    "No source, binary, catalog, or NZB data is deleted.",
		},
		{
			TaskKey:          "poster_queue_done_cleanup",
			Label:            "Poster queue done cleanup",
			Risk:             "low",
			Implemented:      true,
			SpaceEffect:      "DB-internal row cleanup; OS space is not returned until vacuum/table rewrite.",
			SupervisorEffect: "No pipeline work is enqueued. Poster materialize keeps only pending queue state.",
			DataEffect:       "Deletes completed poster_materialization_queue rows only.",
			ReleaseSafety:    "Does not delete headers, binaries, release files, catalog data, or archived NZBs.",
		},
		{
			TaskKey:          "inspect_ready_queue_cleanup",
			Label:            "Inspect ready queue cleanup",
			Risk:             "low",
			Implemented:      true,
			SpaceEffect:      "DB-internal row cleanup; OS space is not returned until vacuum/table rewrite.",
			SupervisorEffect: "Inspect refresh stages may repopulate work only if source inspections require it.",
			DataEffect:       "Deletes completed or blocked ready-queue rows after inspection history exists.",
			ReleaseSafety:    "Keeps binary_inspections and release/catalog/archive rows intact.",
		},
		{
			TaskKey:          "assembly_queue_stale_cleanup",
			Label:            "Assembly queue stale cleanup",
			Risk:             "low",
			Implemented:      true,
			SpaceEffect:      "DB-internal row cleanup; OS space is not returned until vacuum/table rewrite.",
			SupervisorEffect: "Assemble stage skips queue entries already represented by binary parts.",
			DataEffect:       "Deletes queue residue for already assembled article headers.",
			ReleaseSafety:    "Does not delete source headers, payloads, binaries, or release lineage.",
		},
		{
			TaskKey:          "runtime_history_cleanup",
			Label:            "Runtime history cleanup",
			Risk:             "low",
			Implemented:      true,
			SpaceEffect:      "DB-internal row cleanup; OS space is not returned until vacuum/table rewrite.",
			SupervisorEffect: "No stage behavior changes; old run/debug history is shortened.",
			DataEffect:       "Deletes old completed, abandoned, and failed operational history rows.",
			ReleaseSafety:    "No source, binary, catalog, or NZB data is deleted.",
		},
		{
			TaskKey:          "readiness_cleanup",
			Label:            "Readiness cleanup",
			Risk:             "medium",
			Implemented:      true,
			SpaceEffect:      "DB-internal row cleanup; OS space is not returned until vacuum/table rewrite.",
			SupervisorEffect: "Release readiness summaries may be recomputed from retained source/projection rows.",
			DataEffect:       "Deletes stale derived readiness rows and orphan release shells.",
			ReleaseSafety:    "Safe only while raw source, binary projections, public release details, and archive state are retained.",
		},
		{
			TaskKey:          "grouping_evidence_cleanup",
			Label:            "Grouping evidence cleanup",
			Risk:             "medium",
			Implemented:      true,
			SpaceEffect:      "DB-internal row cleanup; OS space is not returned until vacuum/table rewrite.",
			SupervisorEffect: "Grouping/release-family debug evidence is reduced; current identity projections remain.",
			DataEffect:       "Deletes older stable side-table evidence rows.",
			ReleaseSafety:    "Does not delete current binary identity, source headers, catalog files, or NZBs.",
		},
		{
			TaskKey:          "crosspost_group_raw_purge",
			Label:            "Crosspost raw group purge",
			Risk:             "medium",
			Implemented:      true,
			SpaceEffect:      "DB-internal row cleanup; OS space is not returned until vacuum/table rewrite.",
			SupervisorEffect: "Crosspost popularity refresh keeps summary and queue state; historical raw Xref telemetry older than 72h is no longer available for recompute/debug.",
			DataEffect:       "Deletes raw article_header_crosspost_groups rows only after their group queue is done and the summary watermark has consumed them.",
			ReleaseSafety:    "Current release formation uses binary identity and release-family evidence, then persists release_newsgroups from clustered binary IDs; this does not delete headers, binary rows, release rows, catalog files, or NZBs.",
		},
		{
			TaskKey:          "yenc_done_work_item_cleanup",
			Label:            "yEnc done work item cleanup",
			Risk:             "medium",
			Implemented:      true,
			SpaceEffect:      "DB-internal row cleanup followed by normal VACUUM (ANALYZE); OS space is not returned until vacuum/table rewrite.",
			SupervisorEffect: "recover_yenc keeps ready/running backlog intact; completed receipts older than 72h are no longer available for queue audit.",
			DataEffect:       "Deletes completed yEnc work receipts only after durable yEnc recovery projection exists and release/archive/running inspect guards pass.",
			ReleaseSafety:    "Does not delete article headers, payloads, binary roots, recovery projections, release files, archive lineage, catalog files, or NZBs.",
		},
		{
			TaskKey:          "header_payload_purge",
			Label:            "Header payload purge",
			Risk:             "high",
			Implemented:      true,
			SpaceEffect:      "DB-internal row cleanup; OS space is not returned until vacuum/table rewrite.",
			SupervisorEffect: "Can reduce yEnc recovery and forensic source detail for already assembled rows.",
			DataEffect:       "Deletes retained article_header_ingest_payloads for aged assembled headers that match the existing predicate.",
			ReleaseSafety:    "Keep manual until archive/source lineage gates are verified for the affected window.",
		},
		{
			TaskKey:          "release_source_purge",
			Label:            "Release source purge",
			Risk:             "high",
			Implemented:      true,
			SpaceEffect:      "DB-internal row cleanup; OS space is not returned until vacuum/table rewrite.",
			SupervisorEffect: "Terminal cleanup after release generation/archive; source rows are no longer available to rebuild those releases.",
			DataEffect:       "Deletes release lineage, safe binary source rows, article source rows, and legacy NZB cache rows after durable archive state.",
			ReleaseSafety:    "Requires purge_pending archive state, durable NZB object, catalog files, and completed release gates.",
		},
		{
			TaskKey:          "stale_nonrelease_source_purge",
			Label:            "Stale non-release source purge",
			Risk:             "high",
			Implemented:      true,
			SpaceEffect:      "DB-internal row cleanup followed by normal VACUUM (ANALYZE); OS space is not returned until vacuum full/table rewrite.",
			SupervisorEffect: "Deletes old source rows outside the active scrape window only when no assemble/yEnc/binary/archive relationship remains.",
			DataEffect:       "Deletes eligible article_headers and cascades ingest payload, crosspost, poster-ref, and poster queue rows.",
			ReleaseSafety:    "Skips headers with assembly queue, binary_parts, yEnc work item, or archive lineage. Dry-run estimates only the next batch.",
		},
	}
	for i := range items {
		estimate, err := s.estimateCleanupRows(ctx, items[i].TaskKey)
		if err != nil {
			return nil, err
		}
		items[i].EstimatedRowsByTable = estimate
	}
	return items, nil
}

func (s *Store) estimateCleanupRows(ctx context.Context, taskKey string) (map[string]int64, error) {
	out := map[string]int64{}
	queries := map[string]string{}
	switch taskKey {
	case "vacuum_dead_tuple_tables":
		queries["dead_tuple_candidate_rows"] = `
			SELECT COALESCE(SUM(n_dead_tup), 0)::bigint
			FROM pg_stat_user_tables
			WHERE COALESCE(n_dead_tup, 0) >= 5000
			  AND (
				COALESCE(n_dead_tup, 0) >= 1000000
				OR (
					COALESCE(n_live_tup, 0) + COALESCE(n_dead_tup, 0) > 0
					AND COALESCE(n_dead_tup, 0) >= 50000
					AND COALESCE(n_dead_tup, 0)::numeric / (COALESCE(n_live_tup, 0) + COALESCE(n_dead_tup, 0)) >= 0.02
				)
				OR (
					pg_total_relation_size(relid) >= 1073741824
					AND COALESCE(n_dead_tup, 0) >= 250000
					AND COALESCE(n_live_tup, 0) + COALESCE(n_dead_tup, 0) > 0
					AND COALESCE(n_dead_tup, 0)::numeric / (COALESCE(n_live_tup, 0) + COALESCE(n_dead_tup, 0)) >= 0.01
				)
			  )`
	case "poster_queue_done_cleanup":
		queries["poster_materialization_queue"] = `SELECT COUNT(*) FROM poster_materialization_queue WHERE status = 'done'`
	case "inspect_ready_queue_cleanup":
		queries["binary_inspection_ready_queue"] = `
			SELECT COUNT(*)
			FROM binary_inspection_ready_queue q
			WHERE q.status IN ('completed', 'blocked')
			  AND EXISTS (
				SELECT 1
				FROM binary_inspections bi
				WHERE bi.stage_name = q.stage_name
				  AND bi.binary_id = q.binary_id
				  AND bi.status IN ('completed', 'failed')
			  )`
	case "assembly_queue_stale_cleanup":
		queries["article_header_assembly_queue"] = `SELECT COUNT(*) FROM article_header_assembly_queue q WHERE EXISTS (SELECT 1 FROM binary_parts bp WHERE bp.article_header_id = q.article_header_id)`
	case "runtime_history_cleanup":
		queries["indexer_stage_runs"] = `SELECT COUNT(*) FROM indexer_stage_runs WHERE (status IN ('completed', 'abandoned') AND started_at < NOW() - INTERVAL '14 days') OR (status = 'failed' AND started_at < NOW() - INTERVAL '30 days')`
		queries["scrape_runs"] = `SELECT COUNT(*) FROM scrape_runs WHERE finished_at < NOW() - INTERVAL '30 days'`
		queries["binary_inspection_artifacts"] = `SELECT COUNT(*) FROM binary_inspection_artifacts WHERE created_at < NOW() - INTERVAL '30 days'`
	case "grouping_evidence_cleanup":
		queries["binary_grouping_evidence"] = `SELECT COUNT(*) FROM binary_grouping_evidence WHERE evidence_source = 'stable_signature' AND updated_at < NOW() - INTERVAL '7 days'`
	case "crosspost_group_raw_purge":
		queries["article_header_crosspost_groups"] = `
			SELECT COUNT(*)
			FROM article_header_crosspost_groups g
			JOIN article_header_crosspost_group_summary s
			  ON s.observed_group_name = g.observed_group_name
			JOIN crosspost_popularity_refresh_queue q
			  ON q.observed_group_name = g.observed_group_name
			 AND q.status = 'done'
			WHERE g.observed_at < NOW() - INTERVAL '72 hours'
			  AND g.article_header_id <= COALESCE(s.last_refreshed_article_header_id, 0)`
	case "yenc_done_work_item_cleanup":
		queries["yenc_recovery_work_items"] = `
			SELECT COUNT(*)
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
	case "header_payload_purge":
		queries["article_header_ingest_payloads"] = storageAuditHeaderPayloadPurgeCountSQL()
	case "release_source_purge":
		queries["release_archive_state"] = `SELECT COUNT(*) FROM release_archive_state WHERE archive_status = 'purge_pending' AND COALESCE(object_key, '') <> ''`
	case "stale_nonrelease_source_purge":
		queries["article_headers"] = `SELECT 0::bigint`
		queries["article_header_ingest_payloads"] = `SELECT 0::bigint`
	}
	for table, query := range queries {
		var count int64
		if err := s.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			out[table] = -1
			continue
		}
		out[table] = count
	}
	return out, nil
}

func storageAuditHeaderPayloadPurgeCountSQL() string {
	return `SELECT 0::bigint`
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}
