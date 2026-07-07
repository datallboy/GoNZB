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

func (s *Store) ProvisionSourceWorkPartitions(ctx context.Context, daysBefore, daysAhead int) error {
	if s == nil || s.db == nil {
		return nil
	}
	if daysBefore < 0 {
		daysBefore = 0
	}
	if daysAhead < 0 {
		daysAhead = 0
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	sourcePostedAt := make([]time.Time, 0, daysBefore+daysAhead+1)
	for offset := -daysBefore; offset <= daysAhead; offset++ {
		sourcePostedAt = append(sourcePostedAt, today.AddDate(0, 0, offset))
	}
	return s.provisionSourceWorkPartitionsForDays(ctx, sourcePostedAt)
}

func (s *Store) verifySourceWorkPartitionsForDays(ctx context.Context, sourcePostedAt []time.Time) error {
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
		if err := s.verifySourceWorkPartitionsForDay(ctx, days[dayKey]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) provisionSourceWorkPartitionsForDays(ctx context.Context, sourcePostedAt []time.Time) error {
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
		if err := s.provisionSourceWorkPartitionsForDay(ctx, days[dayKey]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) verifySourceWorkPartitionsForDay(ctx context.Context, dayStart time.Time) error {
	dayKey, childSuffix := nativePartitionDayKeys(dayStart)
	missing, err := s.missingNativeDailyPartitions(ctx, childSuffix)
	if err != nil {
		return fmt.Errorf("check native daily partitions for source day %s: %w", dayKey, err)
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing native daily partitions for source day %s: %s; partition DDL is not allowed from active indexer stage write paths; pre-provision the partition horizon before running the supervisor", dayKey, strings.Join(missing, ", "))
}

func (s *Store) provisionSourceWorkPartitionsForDay(ctx context.Context, dayStart time.Time) error {
	dayKey, childSuffix := nativePartitionDayKeys(dayStart)
	missing, err := s.missingNativeDailyPartitions(ctx, childSuffix)
	if err != nil {
		return fmt.Errorf("check native daily partitions for source day %s: %w", dayKey, err)
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

	missing, err = s.missingNativeDailyPartitions(ctx, childSuffix)
	if err != nil {
		return fmt.Errorf("verify native daily partitions for source day %s: %w", dayKey, err)
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing native daily partitions for source day %s after precreate: %s; refusing to route rows into default partitions", dayKey, strings.Join(missing, ", "))
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
	_, err := s.db.ExecContext(ctx, `SELECT public.pgindex_ensure_daily_partition($1, $2::date)`, parentTable, dayKey)
	return err
}

func (s *Store) missingNativeDailyPartitions(ctx context.Context, childSuffix string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	missing := make([]string, 0)
	for _, table := range nativeSourceWorkPartitionTables() {
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

func (s *Store) ensureSourceWorkPartitionsForBinaryIDs(ctx context.Context, binaryIDs []int64) error {
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
	return s.verifySourceWorkPartitionsForDays(ctx, days)
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
