package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type xrefGroupRef struct {
	GroupName     string
	ArticleNumber int64
}

type IndexerCrosspostPopularityItem struct {
	GroupName                string     `json:"group_name"`
	ObservedArticleCount     int64      `json:"observed_article_count"`
	DistinctMessageCount     int64      `json:"distinct_message_count"`
	DistinctSourceGroupCount int64      `json:"distinct_source_group_count"`
	LastSeenAt               *time.Time `json:"last_seen_at,omitempty"`
}

type IndexerCrosspostBackfillResult struct {
	BatchesProcessed int   `json:"batches_processed"`
	SourceHeaders    int64 `json:"source_headers"`
	RowsUpserted     int64 `json:"rows_upserted"`
	LastArticleID    int64 `json:"last_article_id"`
}

func normalizeCrosspostGroupName(groupName string) string {
	return strings.ToLower(strings.TrimSpace(groupName))
}

func parseXrefGroupRefs(xref string) []xrefGroupRef {
	fields := strings.Fields(strings.TrimSpace(xref))
	if len(fields) == 0 {
		return nil
	}
	if strings.EqualFold(strings.TrimSuffix(fields[0], ":"), "xref") {
		fields = fields[1:]
	}
	if len(fields) > 0 && !strings.Contains(fields[0], ":") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return nil
	}

	out := make([]xrefGroupRef, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		idx := strings.LastIndex(field, ":")
		if idx <= 0 || idx >= len(field)-1 {
			continue
		}
		groupName := strings.TrimSpace(field[:idx])
		if groupName == "" {
			continue
		}
		articleNumber, err := parsePositiveInt64(field[idx+1:])
		if err != nil {
			continue
		}
		key := strings.ToLower(groupName)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, xrefGroupRef{
			GroupName:     groupName,
			ArticleNumber: articleNumber,
		})
	}
	return out
}

func parsePositiveInt64(raw string) (int64, error) {
	var n int64
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not numeric")
		}
		n = (n * 10) + int64(r-'0')
	}
	if n <= 0 {
		return 0, fmt.Errorf("not positive")
	}
	return n, nil
}

func upsertArticleHeaderCrosspostGroupsBatch(ctx context.Context, tx *sql.Tx, providerID, sourceNewsgroupID int64, batch []preparedArticleHeaderInsert, resolvedIDs []int64) error {
	if len(batch) == 0 || len(resolvedIDs) == 0 {
		return nil
	}
	type row struct {
		articleHeaderID    int64
		messageID          string
		observedGroupName  string
		observedArticleNum int64
	}
	rows := make([]row, 0, len(batch)*2)
	seen := make(map[string]struct{}, len(batch)*2)
	for idx, item := range batch {
		if idx >= len(resolvedIDs) || resolvedIDs[idx] <= 0 {
			continue
		}
		refs := parseXrefGroupRefs(item.Xref)
		if len(refs) == 0 {
			continue
		}
		for _, ref := range refs {
			groupName := normalizeCrosspostGroupName(ref.GroupName)
			if groupName == "" {
				continue
			}
			key := fmt.Sprintf("%d\x00%s", resolvedIDs[idx], groupName)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			rows = append(rows, row{
				articleHeaderID:    resolvedIDs[idx],
				messageID:          item.MessageID,
				observedGroupName:  groupName,
				observedArticleNum: ref.ArticleNumber,
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}

	var query strings.Builder
	query.WriteString(`
		WITH input_rows (
			article_header_id,
			provider_id,
			source_newsgroup_id,
			message_id,
			observed_group_name,
			observed_article_number,
			observed_at
		) AS (
			VALUES `)
	args := make([]any, 0, len(rows)*6)
	for idx, row := range rows {
		if idx > 0 {
			query.WriteString(",")
		}
		query.WriteString("(")
		fmt.Fprintf(&query, "$%d::bigint,", len(args)+1)
		args = append(args, row.articleHeaderID)
		fmt.Fprintf(&query, "$%d::bigint,", len(args)+1)
		args = append(args, providerID)
		fmt.Fprintf(&query, "$%d::bigint,", len(args)+1)
		args = append(args, sourceNewsgroupID)
		fmt.Fprintf(&query, "$%d::text,", len(args)+1)
		args = append(args, row.messageID)
		fmt.Fprintf(&query, "$%d::text,", len(args)+1)
		args = append(args, row.observedGroupName)
		fmt.Fprintf(&query, "$%d::bigint,", len(args)+1)
		args = append(args, row.observedArticleNum)
		query.WriteString("NOW())")
	}
	query.WriteString(`
		)
		INSERT INTO article_header_crosspost_groups (
			article_header_id,
			provider_id,
			source_newsgroup_id,
			message_id,
			observed_group_name,
			observed_article_number,
			observed_at
		)
			SELECT
				article_header_id,
				provider_id,
				source_newsgroup_id,
				COALESCE(BTRIM(message_id), ''),
				observed_group_name,
				observed_article_number,
				observed_at
			FROM input_rows
			WHERE BTRIM(COALESCE(observed_group_name, '')) <> ''
			ON CONFLICT (article_header_id, observed_group_name) DO NOTHING`)
	if _, err := tx.ExecContext(ctx, query.String(), args...); err != nil {
		return fmt.Errorf("upsert article header crosspost groups batch: %w", err)
	}
	groups := make([]string, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, row.observedGroupName)
	}
	if err := queueCrosspostPopularityRefreshRows(ctx, tx, groups); err != nil {
		return err
	}
	return nil
}

func (s *Store) GetIndexerCrosspostNewsgroupPopularity(ctx context.Context, limit int) ([]IndexerCrosspostPopularityItem, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.observed_group_name,
			s.observed_article_count,
			s.distinct_message_count,
			s.distinct_source_group_count,
			s.last_seen_at
		FROM article_header_crosspost_group_summary s
		WHERE BTRIM(COALESCE(s.observed_group_name, '')) <> ''
		ORDER BY distinct_message_count DESC, observed_article_count DESC, last_seen_at DESC NULLS LAST, s.observed_group_name ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("get indexer crosspost popularity: %w", err)
	}
	defer rows.Close()

	out := make([]IndexerCrosspostPopularityItem, 0, limit)
	for rows.Next() {
		var item IndexerCrosspostPopularityItem
		var lastSeen sql.NullTime
		if err := rows.Scan(
			&item.GroupName,
			&item.ObservedArticleCount,
			&item.DistinctMessageCount,
			&item.DistinctSourceGroupCount,
			&lastSeen,
		); err != nil {
			return nil, fmt.Errorf("scan indexer crosspost popularity: %w", err)
		}
		if lastSeen.Valid {
			ts := lastSeen.Time.UTC()
			item.LastSeenAt = &ts
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexer crosspost popularity: %w", err)
	}
	return out, nil
}

func (s *Store) BackfillIndexerCrosspostGroups(ctx context.Context, batchSize, maxBatches int) (*IndexerCrosspostBackfillResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if batchSize <= 0 {
		batchSize = 5000
	}
	if maxBatches <= 0 {
		maxBatches = 1
	}

	out := &IndexerCrosspostBackfillResult{}
	var cursor int64
	for range maxBatches {
		headers, upserted, lastID, err := s.backfillIndexerCrosspostGroupsBatch(ctx, cursor, batchSize)
		if err != nil {
			return nil, err
		}
		if headers == 0 {
			break
		}
		out.BatchesProcessed++
		out.SourceHeaders += headers
		out.RowsUpserted += upserted
		out.LastArticleID = lastID
		cursor = lastID
	}
	return out, nil
}

func (s *Store) backfillIndexerCrosspostGroupsBatch(ctx context.Context, afterArticleID int64, batchSize int) (int64, int64, int64, error) {
	var headers, rows, lastID int64
	if err := s.db.QueryRowContext(ctx, `
		WITH batch AS (
			SELECT
				ah.id AS article_header_id,
				ah.provider_id,
				ah.newsgroup_id AS source_newsgroup_id,
				ah.message_id,
				aip.xref
			FROM article_headers ah
			JOIN article_header_ingest_payloads aip ON aip.article_header_id = ah.id
			WHERE ah.id > $1
			  AND BTRIM(COALESCE(aip.xref, '')) <> ''
			ORDER BY ah.id
			LIMIT $2
		),
		exploded AS (
			SELECT DISTINCT
				b.article_header_id,
				b.provider_id,
				b.source_newsgroup_id,
				b.message_id,
				LOWER(BTRIM(SPLIT_PART(tok.token, ':', 1))) AS observed_group_name,
				COALESCE(NULLIF(SPLIT_PART(tok.token, ':', 2), ''), '0')::bigint AS observed_article_number
			FROM batch b
			CROSS JOIN LATERAL regexp_split_to_table(
				regexp_replace(b.xref, '^\s*Xref:\s*', '', 'i'),
				E'\\s+'
			) AS tok(token)
			WHERE POSITION(':' IN tok.token) > 0
			  AND BTRIM(SPLIT_PART(tok.token, ':', 1)) <> ''
		),
		ins AS (
			INSERT INTO article_header_crosspost_groups (
				article_header_id,
				provider_id,
				source_newsgroup_id,
				message_id,
				observed_group_name,
				observed_article_number,
				observed_at
			)
			SELECT
				e.article_header_id,
				e.provider_id,
				e.source_newsgroup_id,
				e.message_id,
				e.observed_group_name,
				e.observed_article_number,
				NOW()
			FROM exploded e
			ON CONFLICT (article_header_id, observed_group_name) DO NOTHING
			RETURNING observed_group_name
		),
		queue_upsert AS (
			INSERT INTO crosspost_popularity_refresh_queue (
				observed_group_name,
				status,
				ready_at,
				created_at,
				updated_at
			)
			SELECT
				observed_group_name,
				'pending',
				NOW(),
				NOW(),
				NOW()
			FROM (SELECT DISTINCT observed_group_name FROM ins) d
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
			    END
			RETURNING 1
		)
		SELECT
			COALESCE((SELECT COUNT(*)::bigint FROM batch), 0),
			COALESCE((SELECT COUNT(*)::bigint FROM ins), 0),
			COALESCE((SELECT MAX(article_header_id)::bigint FROM batch), 0)`,
		afterArticleID,
		batchSize,
	).Scan(&headers, &rows, &lastID); err != nil {
		return 0, 0, 0, fmt.Errorf("backfill indexer crosspost groups batch: %w", err)
	}
	return headers, rows, lastID, nil
}
