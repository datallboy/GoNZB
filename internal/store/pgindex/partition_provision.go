package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Store) ensureSourceWorkPartitionsForDays(ctx context.Context, sourcePostedAt []time.Time) error {
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
		if err := s.ensureSourceWorkPartitionsForDay(ctx, days[dayKey]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureSourceWorkPartitionsForDay(ctx context.Context, dayStart time.Time) error {
	dayKey, childSuffix := nativePartitionDayKeys(dayStart)
	missing, err := s.missingNativeDailyPartitions(ctx, childSuffix)
	if err != nil {
		return fmt.Errorf("check native daily partitions for source day %s: %w", dayKey, err)
	}
	if len(missing) == 0 {
		return nil
	}

	var created int
	if err := s.db.QueryRowContext(ctx,
		`SELECT public.pgindex_ensure_source_work_partitions($1::date, 0)`,
		dayKey,
	).Scan(&created); err != nil {
		return fmt.Errorf("precreate native daily partitions for source day %s: %w", dayKey, err)
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
	return s.ensureSourceWorkPartitionsForDays(ctx, days)
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
				(bc.source_posted_at)::date::text AS day_key,
				to_char(bc.source_posted_at, 'YYYYMMDD') AS child_suffix
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
