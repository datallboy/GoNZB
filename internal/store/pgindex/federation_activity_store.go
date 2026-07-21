package pgindex

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/activity"
)

type FederationActivityQuery struct {
	PoolID string
	NodeID string
	Job    string
	Since  time.Time
	Limit  int
}

func (s *Store) UpsertFederationActivityRollups(ctx context.Context, items []activity.Rollup) error {
	if s == nil || s.db == nil || len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO federation_activity_rollups (
				bucket_start, bucket_seconds, node_id, pool_id, component, job,
				attempts, successes, failures, items_in, items_out, bytes_in,
				bytes_out, duration_ms, last_error, last_attempt_at,
				last_success_at, last_failure_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NULLIF($15,''),$16,$17,$18,now())
			ON CONFLICT (bucket_start, bucket_seconds, node_id, pool_id, component)
			DO UPDATE SET
				attempts = federation_activity_rollups.attempts + EXCLUDED.attempts,
				successes = federation_activity_rollups.successes + EXCLUDED.successes,
				failures = federation_activity_rollups.failures + EXCLUDED.failures,
				items_in = federation_activity_rollups.items_in + EXCLUDED.items_in,
				items_out = federation_activity_rollups.items_out + EXCLUDED.items_out,
				bytes_in = federation_activity_rollups.bytes_in + EXCLUDED.bytes_in,
				bytes_out = federation_activity_rollups.bytes_out + EXCLUDED.bytes_out,
				duration_ms = federation_activity_rollups.duration_ms + EXCLUDED.duration_ms,
				last_error = CASE WHEN EXCLUDED.last_failure_at >= federation_activity_rollups.last_failure_at OR federation_activity_rollups.last_failure_at IS NULL THEN EXCLUDED.last_error ELSE federation_activity_rollups.last_error END,
				last_attempt_at = GREATEST(federation_activity_rollups.last_attempt_at, EXCLUDED.last_attempt_at),
				last_success_at = GREATEST(federation_activity_rollups.last_success_at, EXCLUDED.last_success_at),
				last_failure_at = GREATEST(federation_activity_rollups.last_failure_at, EXCLUDED.last_failure_at),
				updated_at = now()`,
			item.BucketStart, item.BucketSeconds, strings.TrimSpace(item.NodeID), strings.TrimSpace(item.PoolID),
			item.Component, item.Job, item.Attempts, item.Successes, item.Failures, item.ItemsIn, item.ItemsOut,
			item.BytesIn, item.BytesOut, item.DurationMS, item.LastError, item.LastAttemptAt, item.LastSuccessAt, item.LastFailureAt); err != nil {
			return fmt.Errorf("upsert federation activity rollup: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) ListFederationActivityRollups(ctx context.Context, query FederationActivityQuery) ([]activity.Rollup, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is unavailable")
	}
	if query.Limit <= 0 || query.Limit > 10000 {
		query.Limit = 5000
	}
	if query.Since.IsZero() {
		query.Since = time.Now().UTC().Add(-24 * time.Hour)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT bucket_start, bucket_seconds, node_id, pool_id, component, job,
		       attempts, successes, failures, items_in, items_out, bytes_in,
		       bytes_out, duration_ms, COALESCE(last_error, ''),
		       last_attempt_at, last_success_at, last_failure_at
		FROM federation_activity_rollups
		WHERE bucket_start >= $1
		  AND ($2 = '' OR pool_id = $2)
		  AND ($3 = '' OR node_id = $3)
		  AND ($4 = '' OR job = $4)
		ORDER BY bucket_start DESC, component ASC
		LIMIT $5`, query.Since, strings.TrimSpace(query.PoolID), strings.TrimSpace(query.NodeID), strings.TrimSpace(query.Job), query.Limit)
	if err != nil {
		return nil, fmt.Errorf("list federation activity rollups: %w", err)
	}
	defer rows.Close()
	out := make([]activity.Rollup, 0)
	for rows.Next() {
		var item activity.Rollup
		if err := rows.Scan(
			&item.BucketStart, &item.BucketSeconds, &item.NodeID, &item.PoolID, &item.Component, &item.Job,
			&item.Attempts, &item.Successes, &item.Failures, &item.ItemsIn, &item.ItemsOut,
			&item.BytesIn, &item.BytesOut, &item.DurationMS, &item.LastError,
			&item.LastAttemptAt, &item.LastSuccessAt, &item.LastFailureAt,
		); err != nil {
			return nil, fmt.Errorf("scan federation activity rollup: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate federation activity rollups: %w", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BucketStart.Equal(out[j].BucketStart) {
			return out[i].Component < out[j].Component
		}
		return out[i].BucketStart.Before(out[j].BucketStart)
	})
	return out, nil
}

func (s *Store) CompactFederationActivityRollups(ctx context.Context, now time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cutoff := now.Add(-48 * time.Hour)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO federation_activity_rollups (
			bucket_start, bucket_seconds, node_id, pool_id, component, job,
			attempts, successes, failures, items_in, items_out, bytes_in,
			bytes_out, duration_ms, last_error, last_attempt_at,
			last_success_at, last_failure_at, updated_at
		)
		SELECT date_trunc('hour', bucket_start), 3600, node_id, pool_id, component, job,
		       SUM(attempts), SUM(successes), SUM(failures), SUM(items_in), SUM(items_out),
		       SUM(bytes_in), SUM(bytes_out), SUM(duration_ms),
		       (array_agg(last_error ORDER BY last_failure_at DESC NULLS LAST))[1],
		       MAX(last_attempt_at), MAX(last_success_at), MAX(last_failure_at), now()
		FROM federation_activity_rollups
		WHERE bucket_seconds = 300 AND bucket_start < $1
		GROUP BY date_trunc('hour', bucket_start), node_id, pool_id, component, job
		ON CONFLICT (bucket_start, bucket_seconds, node_id, pool_id, component)
		DO UPDATE SET
			attempts = EXCLUDED.attempts, successes = EXCLUDED.successes,
			failures = EXCLUDED.failures, items_in = EXCLUDED.items_in,
			items_out = EXCLUDED.items_out, bytes_in = EXCLUDED.bytes_in,
			bytes_out = EXCLUDED.bytes_out, duration_ms = EXCLUDED.duration_ms,
			last_error = EXCLUDED.last_error, last_attempt_at = EXCLUDED.last_attempt_at,
			last_success_at = EXCLUDED.last_success_at, last_failure_at = EXCLUDED.last_failure_at,
			updated_at = now()`, cutoff); err != nil {
		return fmt.Errorf("compact federation activity rollups: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM federation_activity_rollups WHERE bucket_seconds = 300 AND bucket_start < $1`, cutoff); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM federation_activity_rollups WHERE bucket_seconds = 3600 AND bucket_start < $1`, now.Add(-90*24*time.Hour)); err != nil {
		return err
	}
	return tx.Commit()
}
