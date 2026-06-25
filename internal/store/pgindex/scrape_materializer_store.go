package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

type posterMaterializationQueueRow struct {
	ArticleHeaderID int64
	PosterName      string
	SourcePostedAt  *time.Time
}

type IndexerPosterMaterializationResult struct {
	Claimed      int64 `json:"claimed"`
	Posters      int64 `json:"posters"`
	RefsUpserted int64 `json:"refs_upserted"`
}

type IndexerCrosspostPopularityRefreshResult struct {
	Claimed                  int64 `json:"claimed"`
	GroupsRefreshed          int64 `json:"groups_refreshed"`
	DistinctMessagesObserved int64 `json:"distinct_messages_observed"`
	DistinctSourcesObserved  int64 `json:"distinct_sources_observed"`

	// Deprecated: exact distinct helper rows are no longer materialized.
	MessagesUpserted int64 `json:"messages_upserted"`
	// Deprecated: exact distinct helper rows are no longer materialized.
	SourcesUpserted int64 `json:"sources_upserted"`
}

func normalizePosterKey(posterName string) string {
	return strings.ToLower(strings.TrimSpace(posterName))
}

func upsertPosterMaterializationQueueBatch(ctx context.Context, tx *sql.Tx, rows []posterMaterializationQueueRow) error {
	if len(rows) == 0 {
		return nil
	}
	type preparedRow struct {
		articleHeaderID int64
		posterName      string
		posterKey       string
		sourcePostedAt  *time.Time
	}
	prepared := make([]preparedRow, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		if row.ArticleHeaderID <= 0 {
			continue
		}
		posterName := sanitizeUTF8(row.PosterName)
		posterKey := normalizePosterKey(posterName)
		if posterKey == "" {
			continue
		}
		if _, ok := seen[row.ArticleHeaderID]; ok {
			continue
		}
		seen[row.ArticleHeaderID] = struct{}{}
		prepared = append(prepared, preparedRow{
			articleHeaderID: row.ArticleHeaderID,
			posterName:      posterName,
			posterKey:       posterKey,
			sourcePostedAt:  row.SourcePostedAt,
		})
	}
	if len(prepared) == 0 {
		return nil
	}

	var query strings.Builder
	query.WriteString(`
		INSERT INTO poster_materialization_queue (
			article_header_id,
			poster_name,
			poster_key,
			source_posted_at,
			status,
			ready_at,
			created_at,
			updated_at
		)
		VALUES `)
	args := make([]any, 0, len(prepared)*4)
	for idx, row := range prepared {
		if idx > 0 {
			query.WriteString(",")
		}
		fmt.Fprintf(&query, "($%d::bigint,$%d::text,$%d::text,COALESCE($%d::timestamptz, NOW()),'pending',NOW(),NOW(),NOW())",
			len(args)+1,
			len(args)+2,
			len(args)+3,
			len(args)+4,
		)
		args = append(args, row.articleHeaderID, row.posterName, row.posterKey, row.sourcePostedAt)
	}
	query.WriteString(`
		ON CONFLICT (source_posted_at, article_header_id) DO UPDATE
		SET poster_name = EXCLUDED.poster_name,
		    poster_key = EXCLUDED.poster_key,
		    source_posted_at = COALESCE(EXCLUDED.source_posted_at, poster_materialization_queue.source_posted_at),
		    status = CASE
			    WHEN poster_materialization_queue.poster_key IS DISTINCT FROM EXCLUDED.poster_key THEN 'pending'
			    WHEN poster_materialization_queue.status = 'done' THEN poster_materialization_queue.status
			    ELSE 'pending'
		    END,
		    ready_at = CASE
			    WHEN poster_materialization_queue.poster_key IS DISTINCT FROM EXCLUDED.poster_key THEN NOW()
			    WHEN poster_materialization_queue.status = 'done' THEN poster_materialization_queue.ready_at
			    ELSE LEAST(poster_materialization_queue.ready_at, NOW())
		    END,
		    lease_owner = CASE
			    WHEN poster_materialization_queue.poster_key IS DISTINCT FROM EXCLUDED.poster_key THEN ''
			    ELSE poster_materialization_queue.lease_owner
		    END,
		    lease_expires_at = CASE
			    WHEN poster_materialization_queue.poster_key IS DISTINCT FROM EXCLUDED.poster_key THEN NULL
			    ELSE poster_materialization_queue.lease_expires_at
		    END,
		    updated_at = NOW()`)
	if _, err := tx.ExecContext(ctx, query.String(), args...); err != nil {
		return fmt.Errorf("upsert poster materialization queue batch: %w", err)
	}
	return nil
}

type claimedPosterMaterializationRow struct {
	ArticleHeaderID int64
	PosterName      string
	PosterKey       string
	SourcePostedAt  *time.Time
}

func (s *Store) MaterializeArticleHeaderPosters(ctx context.Context, limit int) (*IndexerPosterMaterializationResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 {
		limit = 10000
	}
	out := &IndexerPosterMaterializationResult{}
	err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		claimed, err := claimPosterMaterializationRows(ctx, tx, limit)
		if err != nil {
			return err
		}
		out.Claimed = int64(len(claimed))
		if len(claimed) == 0 {
			return tx.Commit()
		}

		posterIDs, postersUpserted, err := upsertPosterDimensionRows(ctx, tx, claimed)
		if err != nil {
			return err
		}
		out.Posters = postersUpserted

		refsUpserted, err := upsertArticleHeaderPosterRefs(ctx, tx, claimed, posterIDs)
		if err != nil {
			return err
		}
		out.RefsUpserted = refsUpserted

		if err := finishPosterMaterializationRows(ctx, tx, claimed); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func claimPosterMaterializationRows(ctx context.Context, tx *sql.Tx, limit int) ([]claimedPosterMaterializationRow, error) {
	rows, err := tx.QueryContext(ctx, `
		WITH next_rows AS (
			SELECT article_header_id
			FROM poster_materialization_queue
			WHERE status IN ('pending', 'failed')
			  AND ready_at <= NOW()
			  AND BTRIM(COALESCE(poster_key, '')) <> ''
			ORDER BY ready_at, article_header_id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		),
		claimed AS (
			UPDATE poster_materialization_queue q
			SET status = 'processing',
			    lease_owner = 'poster_materialize',
			    lease_expires_at = NOW() + INTERVAL '5 minutes',
			    attempt_count = q.attempt_count + 1,
			    updated_at = NOW()
			FROM next_rows n
			WHERE q.article_header_id = n.article_header_id
			RETURNING q.article_header_id, q.poster_name, q.poster_key, q.source_posted_at
		)
		SELECT article_header_id, poster_name, poster_key, source_posted_at
		FROM claimed
		ORDER BY article_header_id`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim poster materialization rows: %w", err)
	}
	defer rows.Close()

	out := make([]claimedPosterMaterializationRow, 0, limit)
	for rows.Next() {
		var row claimedPosterMaterializationRow
		var sourcePostedAt sql.NullTime
		if err := rows.Scan(&row.ArticleHeaderID, &row.PosterName, &row.PosterKey, &sourcePostedAt); err != nil {
			return nil, fmt.Errorf("scan claimed poster materialization row: %w", err)
		}
		if sourcePostedAt.Valid {
			t := sourcePostedAt.Time.UTC()
			row.SourcePostedAt = &t
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed poster materialization rows: %w", err)
	}
	return out, nil
}

func upsertPosterDimensionRows(ctx context.Context, tx *sql.Tx, rows []claimedPosterMaterializationRow) (map[string]int64, int64, error) {
	posterNames := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.PosterName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		posterNames = append(posterNames, name)
	}
	sort.Strings(posterNames)
	if len(posterNames) == 0 {
		return map[string]int64{}, 0, nil
	}

	var query strings.Builder
	query.WriteString("INSERT INTO posters (poster_name) VALUES ")
	args := make([]any, 0, len(posterNames))
	for idx, name := range posterNames {
		if idx > 0 {
			query.WriteString(",")
		}
		fmt.Fprintf(&query, "($%d::text)", len(args)+1)
		args = append(args, name)
	}
	query.WriteString(" ON CONFLICT (poster_name) DO NOTHING")
	res, err := tx.ExecContext(ctx, query.String(), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("upsert poster dimension rows: %w", err)
	}
	inserted, _ := res.RowsAffected()

	query.Reset()
	args = args[:0]
	query.WriteString("SELECT id, poster_name FROM posters WHERE poster_name IN (")
	for idx, name := range posterNames {
		if idx > 0 {
			query.WriteString(",")
		}
		fmt.Fprintf(&query, "$%d::text", len(args)+1)
		args = append(args, name)
	}
	query.WriteString(")")
	foundRows, err := tx.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("select poster dimension rows: %w", err)
	}
	defer foundRows.Close()

	out := make(map[string]int64, len(posterNames))
	for foundRows.Next() {
		var id int64
		var name string
		if err := foundRows.Scan(&id, &name); err != nil {
			return nil, 0, fmt.Errorf("scan poster dimension row: %w", err)
		}
		out[name] = id
	}
	if err := foundRows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate poster dimension rows: %w", err)
	}
	return out, inserted, nil
}

func upsertArticleHeaderPosterRefs(ctx context.Context, tx *sql.Tx, rows []claimedPosterMaterializationRow, posterIDs map[string]int64) (int64, error) {
	const posterRefUpsertChunkSize = 2500
	var total int64
	for start := 0; start < len(rows); start += posterRefUpsertChunkSize {
		end := start + posterRefUpsertChunkSize
		if end > len(rows) {
			end = len(rows)
		}
		affected, err := upsertArticleHeaderPosterRefsChunk(ctx, tx, rows[start:end], posterIDs)
		if err != nil {
			return total, err
		}
		total += affected
	}
	return total, nil
}

func upsertArticleHeaderPosterRefsChunk(ctx context.Context, tx *sql.Tx, rows []claimedPosterMaterializationRow, posterIDs map[string]int64) (int64, error) {
	var query strings.Builder
	args := make([]any, 0, len(rows)*5)
	query.WriteString(`
		INSERT INTO article_header_poster_refs (
			article_header_id,
			poster_id,
			poster_name,
			poster_key,
			source_posted_at,
			created_at,
			updated_at
		)
		VALUES `)
	written := 0
	for _, row := range rows {
		posterID := posterIDs[row.PosterName]
		if posterID <= 0 {
			continue
		}
		if written > 0 {
			query.WriteString(",")
		}
		fmt.Fprintf(&query, "($%d::bigint,$%d::bigint,$%d::text,$%d::text,COALESCE($%d::timestamptz, NOW()),NOW(),NOW())",
			len(args)+1,
			len(args)+2,
			len(args)+3,
			len(args)+4,
			len(args)+5,
		)
		args = append(args, row.ArticleHeaderID, posterID, row.PosterName, row.PosterKey, row.SourcePostedAt)
		written++
	}
	if written == 0 {
		return 0, nil
	}
	query.WriteString(`
		ON CONFLICT (source_posted_at, article_header_id) DO UPDATE
		SET poster_id = EXCLUDED.poster_id,
		    poster_name = EXCLUDED.poster_name,
		    poster_key = EXCLUDED.poster_key,
		    source_posted_at = COALESCE(EXCLUDED.source_posted_at, article_header_poster_refs.source_posted_at),
		    updated_at = NOW()`)
	res, err := tx.ExecContext(ctx, query.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("upsert article header poster refs: %w", err)
	}
	affected, _ := res.RowsAffected()
	return affected, nil
}

func finishPosterMaterializationRows(ctx context.Context, tx *sql.Tx, rows []claimedPosterMaterializationRow) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([]any, 0, len(rows))
	var query strings.Builder
	query.WriteString("UPDATE poster_materialization_queue SET status = 'done', lease_owner = '', lease_expires_at = NULL, last_error = '', updated_at = NOW() WHERE article_header_id IN (")
	for idx, row := range rows {
		if idx > 0 {
			query.WriteString(",")
		}
		fmt.Fprintf(&query, "$%d::bigint", len(args)+1)
		args = append(args, row.ArticleHeaderID)
	}
	query.WriteString(")")
	if _, err := tx.ExecContext(ctx, query.String(), args...); err != nil {
		return fmt.Errorf("finish poster materialization rows: %w", err)
	}
	return nil
}

func (s *Store) RefreshCrosspostPopularity(ctx context.Context, limit int) (*IndexerCrosspostPopularityRefreshResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 {
		limit = 1000
	}
	out := &IndexerCrosspostPopularityRefreshResult{}
	err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		groups, err := claimCrosspostPopularityGroups(ctx, tx, limit)
		if err != nil {
			return err
		}
		out.Claimed = int64(len(groups))
		if len(groups) == 0 {
			return tx.Commit()
		}
		refreshed, messages, sources, err := refreshCrosspostPopularityGroups(ctx, tx, groups)
		if err != nil {
			return err
		}
		out.GroupsRefreshed = refreshed
		out.DistinctMessagesObserved = messages
		out.DistinctSourcesObserved = sources
		if err := finishCrosspostPopularityGroups(ctx, tx, groups); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func claimCrosspostPopularityGroups(ctx context.Context, tx *sql.Tx, limit int) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		WITH next_rows AS (
			SELECT observed_group_name
			FROM crosspost_popularity_refresh_queue
			WHERE status IN ('pending', 'failed')
			  AND ready_at <= NOW()
			  AND BTRIM(COALESCE(observed_group_name, '')) <> ''
			ORDER BY ready_at, observed_group_name
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		),
		claimed AS (
			UPDATE crosspost_popularity_refresh_queue q
			SET status = 'processing',
			    lease_owner = 'crosspost_popularity_refresh',
			    lease_expires_at = NOW() + INTERVAL '5 minutes',
			    attempt_count = q.attempt_count + 1,
			    updated_at = NOW()
			FROM next_rows n
			WHERE q.observed_group_name = n.observed_group_name
			RETURNING q.observed_group_name
		)
		SELECT observed_group_name
		FROM claimed
		ORDER BY observed_group_name`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim crosspost popularity groups: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0, limit)
	for rows.Next() {
		var groupName string
		if err := rows.Scan(&groupName); err != nil {
			return nil, fmt.Errorf("scan claimed crosspost popularity group: %w", err)
		}
		out = append(out, groupName)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed crosspost popularity groups: %w", err)
	}
	return out, nil
}

func refreshCrosspostPopularityGroups(ctx context.Context, tx *sql.Tx, groups []string) (int64, int64, int64, error) {
	var query strings.Builder
	args := make([]any, 0, len(groups))
	query.WriteString(`
		WITH requested (observed_group_name) AS (
			VALUES `)
	for idx, group := range groups {
		if idx > 0 {
			query.WriteString(",")
		}
		fmt.Fprintf(&query, "($%d::text)", len(args)+1)
		args = append(args, group)
	}
	query.WriteString(`
		),
		current_summary AS MATERIALIZED (
			SELECT
				r.observed_group_name,
				COALESCE(s.observed_article_count, 0)::bigint AS observed_article_count,
				COALESCE(s.distinct_message_count, 0)::bigint AS distinct_message_count,
				COALESCE(s.distinct_source_group_count, 0)::bigint AS distinct_source_group_count,
				s.last_seen_at,
				COALESCE(s.last_refreshed_article_header_id, 0)::bigint AS last_refreshed_article_header_id
			FROM requested r
			LEFT JOIN article_header_crosspost_group_summary s
			  ON s.observed_group_name = r.observed_group_name
		),
		raw_delta AS MATERIALIZED (
			SELECT
				g.observed_group_name,
				g.article_header_id,
				NULLIF(BTRIM(g.message_id), '') AS message_id,
				g.source_newsgroup_id,
				g.observed_at
			FROM article_header_crosspost_groups g
			JOIN current_summary s
			  ON s.observed_group_name = g.observed_group_name
			 AND g.article_header_id > s.last_refreshed_article_header_id
			WHERE BTRIM(g.observed_group_name) <> ''
		),
		raw_agg AS (
			SELECT
				observed_group_name,
				COUNT(*)::bigint AS observed_article_count,
				COUNT(DISTINCT message_id) FILTER (WHERE message_id IS NOT NULL)::bigint AS distinct_message_count,
				COUNT(DISTINCT source_newsgroup_id)::bigint AS distinct_source_group_count,
				MAX(observed_at) AS last_seen_at,
				MAX(article_header_id)::bigint AS last_refreshed_article_header_id
			FROM raw_delta
			GROUP BY observed_group_name
		),
		summary_upsert AS (
			INSERT INTO article_header_crosspost_group_summary (
				observed_group_name,
				observed_article_count,
				distinct_message_count,
				distinct_source_group_count,
				last_seen_at,
				last_refreshed_article_header_id,
				updated_at
			)
			SELECT
				r.observed_group_name,
				s.observed_article_count + COALESCE(a.observed_article_count, 0),
				s.distinct_message_count + COALESCE(a.distinct_message_count, 0),
				s.distinct_source_group_count + COALESCE(a.distinct_source_group_count, 0),
				GREATEST(COALESCE(s.last_seen_at, TIMESTAMPTZ 'epoch'), COALESCE(a.last_seen_at, TIMESTAMPTZ 'epoch')),
				GREATEST(s.last_refreshed_article_header_id, COALESCE(a.last_refreshed_article_header_id, 0)),
				NOW()
			FROM requested r
			JOIN current_summary s ON s.observed_group_name = r.observed_group_name
			JOIN raw_agg a ON a.observed_group_name = r.observed_group_name
			WHERE COALESCE(a.observed_article_count, 0) > 0
			ON CONFLICT (observed_group_name) DO UPDATE
			SET observed_article_count = EXCLUDED.observed_article_count,
			    distinct_message_count = EXCLUDED.distinct_message_count,
			    distinct_source_group_count = EXCLUDED.distinct_source_group_count,
			    last_seen_at = NULLIF(EXCLUDED.last_seen_at, TIMESTAMPTZ 'epoch'),
			    last_refreshed_article_header_id = GREATEST(
			        article_header_crosspost_group_summary.last_refreshed_article_header_id,
			        EXCLUDED.last_refreshed_article_header_id
			    ),
			    updated_at = NOW()
			RETURNING 1
		)
		SELECT
			COALESCE((SELECT COUNT(*)::bigint FROM summary_upsert), 0),
			COALESCE((SELECT SUM(distinct_message_count)::bigint FROM raw_agg), 0),
			COALESCE((SELECT SUM(distinct_source_group_count)::bigint FROM raw_agg), 0)`)
	var refreshed, messages, sources int64
	if err := tx.QueryRowContext(ctx, query.String(), args...).Scan(&refreshed, &messages, &sources); err != nil {
		return 0, 0, 0, fmt.Errorf("refresh crosspost popularity groups: %w", err)
	}
	return refreshed, messages, sources, nil
}

func finishCrosspostPopularityGroups(ctx context.Context, tx *sql.Tx, groups []string) error {
	if len(groups) == 0 {
		return nil
	}
	args := make([]any, 0, len(groups))
	var query strings.Builder
	query.WriteString("UPDATE crosspost_popularity_refresh_queue SET status = 'done', lease_owner = '', lease_expires_at = NULL, last_error = '', updated_at = NOW() WHERE observed_group_name IN (")
	for idx, group := range groups {
		if idx > 0 {
			query.WriteString(",")
		}
		fmt.Fprintf(&query, "$%d::text", len(args)+1)
		args = append(args, group)
	}
	query.WriteString(")")
	if _, err := tx.ExecContext(ctx, query.String(), args...); err != nil {
		return fmt.Errorf("finish crosspost popularity groups: %w", err)
	}
	return nil
}

func queueCrosspostPopularityRefreshRows(ctx context.Context, tx *sql.Tx, groups []string) error {
	if len(groups) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(groups))
	normalized := make([]string, 0, len(groups))
	for _, group := range groups {
		group = normalizeCrosspostGroupName(group)
		if group == "" {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		normalized = append(normalized, group)
	}
	if len(normalized) == 0 {
		return nil
	}
	sort.Strings(normalized)

	var query strings.Builder
	args := make([]any, 0, len(normalized))
	query.WriteString(`
		INSERT INTO crosspost_popularity_refresh_queue (
			observed_group_name,
			status,
			ready_at,
			created_at,
			updated_at
		)
		VALUES `)
	for idx, group := range normalized {
		if idx > 0 {
			query.WriteString(",")
		}
		fmt.Fprintf(&query, "($%d::text,'pending',NOW(),NOW(),NOW())", len(args)+1)
		args = append(args, group)
	}
	query.WriteString(`
		ON CONFLICT (observed_group_name) DO UPDATE
		SET status = CASE
			    WHEN crosspost_popularity_refresh_queue.status = 'processing' THEN crosspost_popularity_refresh_queue.status
			    ELSE 'pending'
		    END,
		    ready_at = CASE
			    WHEN crosspost_popularity_refresh_queue.status = 'processing' THEN crosspost_popularity_refresh_queue.ready_at
			    ELSE LEAST(crosspost_popularity_refresh_queue.ready_at, NOW())
		    END,
		    updated_at = CASE
			    WHEN crosspost_popularity_refresh_queue.status = 'processing' THEN crosspost_popularity_refresh_queue.updated_at
			    ELSE NOW()
		    END`)
	if _, err := tx.ExecContext(ctx, query.String(), args...); err != nil {
		return fmt.Errorf("queue crosspost popularity refresh rows: %w", err)
	}
	return nil
}
