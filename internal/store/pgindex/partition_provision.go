package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var nativePartitionProvisionMu sync.Mutex

type partitionBundle string

const (
	partitionBundleScrape    partitionBundle = "scrape"
	partitionBundleScheduler partitionBundle = "scheduler"
	partitionBundleAssemble  partitionBundle = "assemble"
	partitionBundleYEnc      partitionBundle = "yenc"
	partitionBundleInspect   partitionBundle = "inspect"
	partitionBundleRelease   partitionBundle = "release"
)

var partitionBundleParents = map[partitionBundle][]string{
	partitionBundleScrape: {
		"article_headers",
		"article_header_ingest_payloads",
		"article_header_crosspost_groups",
		"article_header_poster_refs",
		"article_header_assembly_queue",
		"poster_materialization_queue",
	},
	partitionBundleScheduler: {
		"article_cohort_candidates",
		"article_cohort_assembly_queue",
		"article_cohort_yenc_queue",
	},
	partitionBundleAssemble: {
		"binary_parts",
		"binary_observation_stats",
		"binary_identity_current",
		"binary_recovery_current",
		"binary_lifecycle",
		"binary_completion_keys",
		"binary_grouping_evidence",
		"binary_projection_events",
		"binary_superseded_sources",
	},
	partitionBundleYEnc: {
		"yenc_recovery_work_items",
		"binary_recovery_current",
		"binary_lifecycle",
		"binary_projection_events",
		"binary_superseded_sources",
	},
	partitionBundleInspect: {
		"binary_inspection_ready_queue",
		"binary_inspections",
		"binary_inspection_artifacts",
		"binary_archive_entries",
		"binary_text_evidence",
		"binary_media_streams",
		"binary_par2_sets",
		"binary_par2_targets",
	},
	partitionBundleRelease: {
		"release_family_readiness_summaries",
		"release_ready_candidates",
		"release_recovered_file_set_candidates",
		"release_stage_dirty_families",
	},
}

// ConfigurePartitionProvisioning applies the lock budget used by each
// individual parent/day DDL transaction.
func (s *Store) ConfigurePartitionProvisioning(ddlLockTimeout time.Duration) {
	if s == nil {
		return
	}
	if ddlLockTimeout <= 0 {
		ddlLockTimeout = 5 * time.Second
	}
	s.partitionPolicyMu.Lock()
	s.partitionDDLLockLimit = ddlLockTimeout
	s.partitionPolicyMu.Unlock()
}

func (s *Store) partitionDDLLockTimeout() time.Duration {
	if s == nil {
		return 5 * time.Second
	}
	s.partitionPolicyMu.RLock()
	timeout := s.partitionDDLLockLimit
	s.partitionPolicyMu.RUnlock()
	if timeout <= 0 {
		return 5 * time.Second
	}
	return timeout
}

func (s *Store) ProvisionSourceWorkPartitions(ctx context.Context, _ int, daysAhead int) error {
	if s == nil || s.db == nil {
		return nil
	}
	if daysAhead < 0 {
		daysAhead = 0
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	sourcePostedAt := make([]time.Time, 0, daysAhead+1)
	for offset := 0; offset <= daysAhead; offset++ {
		sourcePostedAt = append(sourcePostedAt, today.AddDate(0, 0, offset))
	}
	return s.provisionPartitionBundleForDays(ctx, partitionBundleScrape, sourcePostedAt)
}

func (s *Store) verifyPartitionBundleForDays(ctx context.Context, bundle partitionBundle, sourcePostedAt []time.Time) error {
	if s == nil || s.db == nil || len(sourcePostedAt) == 0 {
		return nil
	}
	days := make(map[string]time.Time, len(sourcePostedAt))
	for _, postedAt := range sourcePostedAt {
		dayKey, _ := nativePartitionDayKeys(postedAt)
		dayStart, err := time.ParseInLocation("2006-01-02", dayKey, time.UTC)
		if err != nil {
			return fmt.Errorf("parse partition day %s: %w", dayKey, err)
		}
		days[dayKey] = dayStart
	}
	dayKeys := make([]string, 0, len(days))
	for dayKey := range days {
		dayKeys = append(dayKeys, dayKey)
	}
	sort.Strings(dayKeys)

	for _, dayKey := range dayKeys {
		if err := s.verifyPartitionBundleForDay(ctx, bundle, days[dayKey]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) provisionPartitionBundleForDays(ctx context.Context, bundle partitionBundle, sourcePostedAt []time.Time) error {
	if s == nil || s.db == nil || len(sourcePostedAt) == 0 {
		return nil
	}
	days := make(map[string]time.Time, len(sourcePostedAt))
	for _, postedAt := range sourcePostedAt {
		dayKey, _ := nativePartitionDayKeys(postedAt)
		dayStart, err := time.ParseInLocation("2006-01-02", dayKey, time.UTC)
		if err != nil {
			return fmt.Errorf("parse partition day %s: %w", dayKey, err)
		}
		days[dayKey] = dayStart
	}
	dayKeys := make([]string, 0, len(days))
	for dayKey := range days {
		dayKeys = append(dayKeys, dayKey)
	}
	sort.Strings(dayKeys)

	nativePartitionProvisionMu.Lock()
	defer nativePartitionProvisionMu.Unlock()

	for _, dayKey := range dayKeys {
		if err := s.provisionPartitionBundleForDay(ctx, bundle, days[dayKey]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) verifyPartitionBundleForDay(ctx context.Context, bundle partitionBundle, dayStart time.Time) error {
	dayKey, childSuffix := nativePartitionDayKeys(dayStart)
	missing, err := s.missingPartitionBundleChildren(ctx, bundle, childSuffix)
	if err != nil {
		return fmt.Errorf("check %s daily partitions for source day %s: %w", bundle, dayKey, err)
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing %s daily partitions for source day %s: %s; refusing to route rows into default partitions", bundle, dayKey, strings.Join(missing, ", "))
}

func (s *Store) provisionPartitionBundleForDay(ctx context.Context, bundle partitionBundle, dayStart time.Time) error {
	dayKey, childSuffix := nativePartitionDayKeys(dayStart)
	missing, err := s.missingPartitionBundleChildren(ctx, bundle, childSuffix)
	if err != nil {
		return fmt.Errorf("check %s daily partitions for source day %s: %w", bundle, dayKey, err)
	}
	for _, childName := range missing {
		parentTable := strings.TrimSuffix(childName, "_"+childSuffix)
		if parentTable == childName {
			return fmt.Errorf("derive native partition parent for source day %s child %s", dayKey, childName)
		}
		if err := s.ensureNativeDailyPartitionForTable(ctx, parentTable, dayKey); err != nil {
			return fmt.Errorf("precreate native daily partition %s for source day %s: %w", childName, dayKey, err)
		}
	}

	missing, err = s.missingPartitionBundleChildren(ctx, bundle, childSuffix)
	if err != nil {
		return fmt.Errorf("verify native daily partitions for source day %s: %w", dayKey, err)
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing %s daily partitions for source day %s after precreate: %s; refusing to route rows into default partitions", bundle, dayKey, strings.Join(missing, ", "))
	}
	return nil
}

func (s *Store) ensureNativeDailyPartitionForTable(ctx context.Context, parentTable, dayKey string) error {
	parentTable = strings.TrimSpace(parentTable)
	if parentTable == "" {
		return fmt.Errorf("parent table is required")
	}
	valid := false
	for _, table := range nativeSourceWorkPartitionTables() {
		if table == parentTable {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("table %q is not a native source/work partition target", parentTable)
	}
	defaultHasRows, err := s.defaultPartitionHasRowsForDay(ctx, parentTable, dayKey)
	if err != nil {
		return fmt.Errorf("check %s_default for source day %s: %w", parentTable, dayKey, err)
	}
	if defaultHasRows {
		return fmt.Errorf("%s_default contains rows for source day %s; run the offline default-rehome workflow before provisioning", parentTable, dayKey)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	lockTimeout := s.partitionDDLLockTimeout().Round(time.Millisecond).String()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true)`, lockTimeout); err != nil {
		return fmt.Errorf("set partition DDL lock timeout: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT public.pgindex_ensure_daily_partition($1, $2::date)`, parentTable, dayKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) missingPartitionBundleChildren(ctx context.Context, bundle partitionBundle, childSuffix string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	parents, ok := partitionBundleParents[bundle]
	if !ok || len(parents) == 0 {
		return nil, fmt.Errorf("unknown partition bundle %q", bundle)
	}
	missing := make([]string, 0)
	for _, table := range parents {
		ok, err := s.nativeDailyPartitionExists(ctx, table, childSuffix)
		if err != nil {
			return nil, fmt.Errorf("table=%s: %w", table, err)
		}
		if !ok {
			missing = append(missing, table+"_"+childSuffix)
		}
	}
	return missing, nil
}

// ExistingScrapeSourceDays reports which requested UTC source days already
// have the complete scrape-owned partition bundle. Callers use this to ensure
// existing sparse days do not consume the per-pass new-day admission budget.
func (s *Store) ExistingScrapeSourceDays(ctx context.Context, sourcePostedAt []time.Time) (map[string]bool, error) {
	days, dayKeys, err := normalizedPartitionDays(sourcePostedAt)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool, len(dayKeys))
	for _, dayKey := range dayKeys {
		_, childSuffix := nativePartitionDayKeys(days[dayKey])
		missing, err := s.missingPartitionBundleChildren(ctx, partitionBundleScrape, childSuffix)
		if err != nil {
			return nil, fmt.Errorf("check existing scrape partitions for source day %s: %w", dayKey, err)
		}
		existing[dayKey] = len(missing) == 0
	}
	return existing, nil
}

func normalizedPartitionDays(sourcePostedAt []time.Time) (map[string]time.Time, []string, error) {
	days := make(map[string]time.Time, len(sourcePostedAt))
	for _, postedAt := range sourcePostedAt {
		dayKey, _ := nativePartitionDayKeys(postedAt)
		dayStart, err := time.ParseInLocation("2006-01-02", dayKey, time.UTC)
		if err != nil {
			return nil, nil, fmt.Errorf("parse partition day %s: %w", dayKey, err)
		}
		days[dayKey] = dayStart
	}
	dayKeys := make([]string, 0, len(days))
	for dayKey := range days {
		dayKeys = append(dayKeys, dayKey)
	}
	sort.Strings(dayKeys)
	return days, dayKeys, nil
}

func (s *Store) ensurePartitionBundleForBinaryIDs(ctx context.Context, bundle partitionBundle, binaryIDs []int64) error {
	if s == nil || s.db == nil || len(binaryIDs) == 0 {
		return nil
	}
	unique := make([]int64, 0, len(binaryIDs))
	seen := make(map[int64]struct{}, len(binaryIDs))
	for _, binaryID := range binaryIDs {
		if binaryID <= 0 {
			continue
		}
		if _, ok := seen[binaryID]; ok {
			continue
		}
		seen[binaryID] = struct{}{}
		unique = append(unique, binaryID)
	}
	if len(unique) == 0 {
		return nil
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })

	rows, err := s.db.QueryContext(ctx, `
		WITH requested(binary_id) AS (
			SELECT DISTINCT unnest($1::bigint[])
		)
		SELECT DISTINCT bc.source_posted_at
		FROM requested r
		JOIN binary_core bc ON bc.binary_id = r.binary_id
		WHERE bc.source_posted_at IS NOT NULL
		ORDER BY bc.source_posted_at`, unique)
	if err != nil {
		return fmt.Errorf("load binary source days for partition precreate: %w", err)
	}
	defer rows.Close()

	days := make([]time.Time, 0)
	for rows.Next() {
		var postedAt time.Time
		if err := rows.Scan(&postedAt); err != nil {
			return fmt.Errorf("scan binary source day for partition precreate: %w", err)
		}
		days = append(days, postedAt)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate binary source days for partition precreate: %w", err)
	}
	return s.provisionPartitionBundleForDays(ctx, bundle, days)
}

func (s *Store) provisionSchedulerPartitionsForReadyWork(ctx context.Context, limitDays int) error {
	if s == nil || s.db == nil {
		return nil
	}
	if limitDays <= 0 || limitDays > 32 {
		limitDays = 32
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH source_days AS (
			SELECT DISTINCT date_trunc('day', source_posted_at)::timestamptz AS source_day
			FROM article_header_assembly_queue
			WHERE source_posted_at IS NOT NULL
			  AND (claim_until IS NULL OR claim_until < NOW())
			UNION
			SELECT DISTINCT date_trunc('day', source_posted_at)::timestamptz
			FROM binary_observation_stats
			WHERE source_posted_at IS NOT NULL
			  AND total_parts <= 1
			  AND observed_parts <= 1
		)
		SELECT source_day
		FROM source_days
		ORDER BY source_day DESC
		LIMIT $1`, limitDays)
	if err != nil {
		return fmt.Errorf("list scheduler source days for partition precreate: %w", err)
	}
	defer rows.Close()
	days := make([]time.Time, 0, limitDays)
	for rows.Next() {
		var day time.Time
		if err := rows.Scan(&day); err != nil {
			return fmt.Errorf("scan scheduler source day for partition precreate: %w", err)
		}
		days = append(days, day)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate scheduler source days for partition precreate: %w", err)
	}
	return s.provisionPartitionBundleForDays(ctx, partitionBundleScheduler, days)
}

func (s *Store) provisionReleasePartitionsForQueuedWork(ctx context.Context, limit int) error {
	if s == nil || s.db == nil || limit <= 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH queued AS MATERIALIZED (
			SELECT provider_id, newsgroup_id, key_kind, family_key
			FROM release_family_summary_refresh_queue
			ORDER BY queued_at, provider_id, newsgroup_id, key_kind, family_key
			LIMIT $1
		), queued_days AS (
			SELECT COALESCE(MIN(bos.posted_at), NOW()) AS source_posted_at
			FROM queued q
			LEFT JOIN binary_identity_current bic
			  ON bic.provider_id = q.provider_id
			 AND bic.newsgroup_id = q.newsgroup_id
			 AND (
			      (q.key_kind = 'release_family' AND bic.release_family_key = q.family_key)
			      OR
			      (q.key_kind = 'base_stem' AND LOWER(BTRIM(bic.base_stem)) = q.family_key)
			 )
			LEFT JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bic.source_posted_at
			 AND bos.binary_id = bic.binary_id
			GROUP BY q.provider_id, q.newsgroup_id, q.key_kind, q.family_key
		), candidate_days AS (
			SELECT source_posted_at
			FROM release_family_readiness_summaries s
			WHERE s.readiness_bucket = 'actionable'
			  AND NOT EXISTS (
			      SELECT 1 FROM release_ready_candidates c
			      WHERE c.source_posted_at = s.source_posted_at
			        AND c.provider_id = s.provider_id
			        AND c.newsgroup_id = s.newsgroup_id
			        AND c.key_kind = s.key_kind
			        AND c.family_key = s.family_key
			  )
			ORDER BY s.updated_at DESC
			LIMIT $1
		)
		SELECT DISTINCT source_posted_at
		FROM (
			SELECT source_posted_at FROM queued_days
			UNION ALL
			SELECT source_posted_at FROM candidate_days
		) days
		WHERE source_posted_at IS NOT NULL
		ORDER BY source_posted_at`, limit)
	if err != nil {
		return fmt.Errorf("list release source days for partition precreate: %w", err)
	}
	defer rows.Close()
	days := make([]time.Time, 0)
	for rows.Next() {
		var day time.Time
		if err := rows.Scan(&day); err != nil {
			return fmt.Errorf("scan release source day for partition precreate: %w", err)
		}
		days = append(days, day)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate release source days for partition precreate: %w", err)
	}
	return s.provisionPartitionBundleForDays(ctx, partitionBundleRelease, days)
}

func ensureYEncRecoveryPartitionsForBinaryIDsInTx(ctx context.Context, tx *sql.Tx, binaryIDs []int64) error {
	if tx == nil || len(binaryIDs) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `
		WITH requested(binary_id) AS (
			SELECT DISTINCT unnest($1::bigint[])
		),
		source_days AS (
			SELECT DISTINCT
				(bc.source_posted_at AT TIME ZONE 'UTC')::date::text AS day_key,
				to_char(bc.source_posted_at AT TIME ZONE 'UTC', 'YYYYMMDD') AS child_suffix
			FROM requested r
			JOIN binary_core bc ON bc.binary_id = r.binary_id
			WHERE bc.source_posted_at IS NOT NULL
		)
		SELECT day_key
		FROM source_days
		WHERE to_regclass('public.yenc_recovery_work_items_' || child_suffix) IS NULL
		ORDER BY day_key
		LIMIT 16`, binaryIDs)
	if err != nil {
		return fmt.Errorf("check yenc recovery daily partitions: %w", err)
	}
	defer rows.Close()
	missing := []string{}
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return fmt.Errorf("scan missing yenc recovery partition day: %w", err)
		}
		missing = append(missing, day)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate missing yenc recovery partition days: %w", err)
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing yenc recovery daily partitions for source days %s; refusing to route yenc work rows into default partition", strings.Join(missing, ", "))
	}
	return nil
}
