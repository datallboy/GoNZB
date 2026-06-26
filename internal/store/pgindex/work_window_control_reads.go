package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type IndexerGroupProfileSummary struct {
	ProviderID      int64      `json:"provider_id"`
	ProviderKey     string     `json:"provider_key"`
	NewsgroupID     int64      `json:"newsgroup_id"`
	GroupName       string     `json:"group_name"`
	Tier            string     `json:"tier"`
	TierOverride    string     `json:"tier_override"`
	Score           float64    `json:"score"`
	RecoveryQueued  int64      `json:"recovery_queued_1d"`
	ReleasesCreated int64      `json:"releases_created_1d"`
	LastScoredAt    *time.Time `json:"last_scored_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type DeferredArticleRangeSummary struct {
	ID                    int64      `json:"id"`
	ProviderID            int64      `json:"provider_id"`
	ProviderKey           string     `json:"provider_key"`
	NewsgroupID           int64      `json:"newsgroup_id"`
	GroupName             string     `json:"group_name"`
	ArticleLow            int64      `json:"article_low"`
	ArticleHigh           int64      `json:"article_high"`
	PostedAtMin           *time.Time `json:"posted_at_min,omitempty"`
	PostedAtMax           *time.Time `json:"posted_at_max,omitempty"`
	EstimatedArticleCount int64      `json:"estimated_article_count"`
	Reason                string     `json:"reason"`
	PriorityScore         float64    `json:"priority_score"`
	State                 string     `json:"state"`
	Attempts              int        `json:"attempts"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type IndexerDailyBucketSummary struct {
	ProviderID           int64     `json:"provider_id"`
	ProviderKey          string    `json:"provider_key"`
	NewsgroupID          int64     `json:"newsgroup_id"`
	GroupName            string    `json:"group_name"`
	BucketDay            string    `json:"bucket_day"`
	Tier                 string    `json:"tier"`
	ScrapeProgressKnown  bool      `json:"scrape_progress_known"`
	ScrapeProgressPct    float64   `json:"scrape_progress_pct"`
	LowerBoundaryCrossed bool      `json:"lower_boundary_crossed"`
	UpperBoundaryCrossed bool      `json:"upper_boundary_crossed"`
	BucketArticleLow     int64     `json:"bucket_article_low"`
	BucketArticleHigh    int64     `json:"bucket_article_high"`
	ScrapeCursorLow      int64     `json:"scrape_cursor_low"`
	ScrapeCursorHigh     int64     `json:"scrape_cursor_high"`
	HeadersStaged        int64     `json:"headers_staged"`
	UnassembledHeaders   int64     `json:"unassembled_headers"`
	YEncReady            int64     `json:"yenc_ready"`
	YEncRunning          int64     `json:"yenc_running"`
	YEncDone             int64     `json:"yenc_done"`
	BinariesTotal        int64     `json:"binaries_total"`
	BinariesComplete     int64     `json:"binaries_complete"`
	BinariesWeak         int64     `json:"binaries_weak"`
	ReleasesCreated      int64     `json:"releases_created"`
	BlockerCount         int64     `json:"blocker_count"`
	LastRefreshedAt      time.Time `json:"last_refreshed_at"`
}

func (s *Store) ListIndexerGroupProfiles(ctx context.Context, limit int) ([]IndexerGroupProfileSummary, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			igp.provider_id,
			up.provider_key,
			igp.newsgroup_id,
			ng.group_name,
			igp.tier,
			COALESCE(igp.tier_override, ''),
			igp.score,
			igp.recovery_queued_1d,
			igp.releases_created_1d,
			igp.last_scored_at,
			igp.updated_at
		FROM indexer_group_profiles igp
		JOIN usenet_providers up ON up.id = igp.provider_id
		JOIN newsgroups ng ON ng.id = igp.newsgroup_id
		ORDER BY COALESCE(igp.tier_override, igp.tier), igp.score DESC, igp.updated_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list indexer group profiles: %w", err)
	}
	defer rows.Close()
	out := make([]IndexerGroupProfileSummary, 0, limit)
	for rows.Next() {
		var item IndexerGroupProfileSummary
		var lastScored sql.NullTime
		if err := rows.Scan(&item.ProviderID, &item.ProviderKey, &item.NewsgroupID, &item.GroupName, &item.Tier, &item.TierOverride, &item.Score, &item.RecoveryQueued, &item.ReleasesCreated, &lastScored, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan indexer group profile: %w", err)
		}
		if lastScored.Valid {
			t := lastScored.Time.UTC()
			item.LastScoredAt = &t
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListDeferredArticleRanges(ctx context.Context, state string, limit int) ([]DeferredArticleRangeSummary, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			dar.id,
			dar.provider_id,
			up.provider_key,
			dar.newsgroup_id,
			ng.group_name,
			dar.article_low,
			dar.article_high,
			dar.posted_at_min,
			dar.posted_at_max,
			dar.estimated_article_count,
			dar.reason,
			dar.priority_score,
			dar.state,
			dar.attempts,
			dar.updated_at
		FROM deferred_article_ranges dar
		JOIN usenet_providers up ON up.id = dar.provider_id
		JOIN newsgroups ng ON ng.id = dar.newsgroup_id
		WHERE ($1 = '' OR dar.state = $1)
		ORDER BY dar.priority_score DESC, dar.updated_at DESC
		LIMIT $2`, state, limit)
	if err != nil {
		return nil, fmt.Errorf("list deferred article ranges: %w", err)
	}
	defer rows.Close()
	out := make([]DeferredArticleRangeSummary, 0, limit)
	for rows.Next() {
		var item DeferredArticleRangeSummary
		var postedMin sql.NullTime
		var postedMax sql.NullTime
		if err := rows.Scan(&item.ID, &item.ProviderID, &item.ProviderKey, &item.NewsgroupID, &item.GroupName, &item.ArticleLow, &item.ArticleHigh, &postedMin, &postedMax, &item.EstimatedArticleCount, &item.Reason, &item.PriorityScore, &item.State, &item.Attempts, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan deferred article range: %w", err)
		}
		if postedMin.Valid {
			t := postedMin.Time.UTC()
			item.PostedAtMin = &t
		}
		if postedMax.Valid {
			t := postedMax.Time.UTC()
			item.PostedAtMax = &t
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListIndexerDailyBucketStats(ctx context.Context, limit int) ([]IndexerDailyBucketSummary, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			db.provider_id,
			up.provider_key,
			db.newsgroup_id,
			ng.group_name,
			db.bucket_day::text,
			db.tier,
			db.scrape_progress_known,
			db.scrape_progress_pct,
			db.lower_boundary_crossed,
			db.upper_boundary_crossed,
			db.bucket_article_low,
			db.bucket_article_high,
			db.scrape_cursor_low,
			db.scrape_cursor_high,
			db.headers_staged,
			db.unassembled_headers,
			db.yenc_ready,
			db.yenc_running,
			db.yenc_done,
			db.binaries_total,
			db.binaries_complete,
			db.binaries_weak,
			db.releases_created,
			db.blocker_count,
			db.last_refreshed_at
		FROM indexer_daily_bucket_stats db
		JOIN usenet_providers up ON up.id = db.provider_id
		JOIN newsgroups ng ON ng.id = db.newsgroup_id
		ORDER BY db.bucket_day DESC, db.headers_staged DESC, db.yenc_ready DESC, db.last_refreshed_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list indexer daily bucket stats: %w", err)
	}
	defer rows.Close()
	out := make([]IndexerDailyBucketSummary, 0, limit)
	for rows.Next() {
		var item IndexerDailyBucketSummary
		if err := rows.Scan(&item.ProviderID, &item.ProviderKey, &item.NewsgroupID, &item.GroupName, &item.BucketDay, &item.Tier, &item.ScrapeProgressKnown, &item.ScrapeProgressPct, &item.LowerBoundaryCrossed, &item.UpperBoundaryCrossed, &item.BucketArticleLow, &item.BucketArticleHigh, &item.ScrapeCursorLow, &item.ScrapeCursorHigh, &item.HeadersStaged, &item.UnassembledHeaders, &item.YEncReady, &item.YEncRunning, &item.YEncDone, &item.BinariesTotal, &item.BinariesComplete, &item.BinariesWeak, &item.ReleasesCreated, &item.BlockerCount, &item.LastRefreshedAt); err != nil {
			return nil, fmt.Errorf("scan indexer daily bucket stat: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) RefreshIndexerDailyBucketStats(ctx context.Context, days int) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("store is required")
	}
	if days <= 0 {
		days = 2
	}
	if days > 7 {
		days = 7
	}
	return s.refreshIndexerDailyBucketStatsByBoundary(ctx, days)
}

func (s *Store) refreshIndexerDailyBucketStatsByBoundary(ctx context.Context, days int) (int64, error) {
	cutoffDay := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -(days - 1))

	type bucket struct {
		ProviderID           int64
		NewsgroupID          int64
		BucketDay            time.Time
		LowerBoundaryCrossed bool
		UpperBoundaryCrossed bool
		BucketArticleLow     int64
		BucketArticleHigh    int64
		ScrapeProgressKnown  bool
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			provider_id,
			newsgroup_id,
			bucket_day,
			lower_boundary_crossed,
			upper_boundary_crossed,
			bucket_article_low,
			bucket_article_high,
			lower_boundary_crossed
				AND upper_boundary_crossed
				AND bucket_article_low > 0
				AND bucket_article_high >= bucket_article_low AS scrape_progress_known
		FROM indexer_scrape_day_boundaries
		WHERE bucket_day >= $1
		ORDER BY bucket_day DESC, provider_id, newsgroup_id`,
		cutoffDay,
	)
	if err != nil {
		return 0, fmt.Errorf("list daily bucket refresh boundaries: %w", err)
	}
	defer rows.Close()

	buckets := []bucket{}
	for rows.Next() {
		var item bucket
		if err := rows.Scan(
			&item.ProviderID,
			&item.NewsgroupID,
			&item.BucketDay,
			&item.LowerBoundaryCrossed,
			&item.UpperBoundaryCrossed,
			&item.BucketArticleLow,
			&item.BucketArticleHigh,
			&item.ScrapeProgressKnown,
		); err != nil {
			return 0, fmt.Errorf("scan daily bucket refresh boundary: %w", err)
		}
		buckets = append(buckets, item)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate daily bucket refresh boundaries: %w", err)
	}

	var refreshed int64
	for _, b := range buckets {
		var changed int64
		if err := s.db.QueryRowContext(ctx, `
			WITH source_stats AS (
				SELECT
					MIN(article_number) AS article_low,
					MAX(article_number) AS article_high,
					COUNT(article_number)::bigint AS headers_staged,
					COUNT(article_number) FILTER (WHERE assembled_at IS NULL)::bigint AS unassembled_headers
				FROM article_headers
				WHERE source_posted_at >= $3::date
				  AND source_posted_at < $3::date + INTERVAL '1 day'
				  AND provider_id = $1
				  AND newsgroup_id = $2
			),
			yenc_stats AS (
				SELECT
					COUNT(*) FILTER (WHERE status = 'ready')::bigint AS yenc_ready,
					COUNT(*) FILTER (WHERE status = 'running')::bigint AS yenc_running,
					COUNT(*) FILTER (WHERE status = 'done')::bigint AS yenc_done,
					COUNT(*) FILTER (WHERE status = 'stale')::bigint AS yenc_stale
				FROM yenc_recovery_work_items
				WHERE partition_day = $3::date
				  AND provider_id = $1
				  AND newsgroup_id = $2
			),
			binary_stats AS (
				SELECT
					COUNT(bos.binary_id)::bigint AS binaries_total,
					COUNT(bos.binary_id) FILTER (WHERE COALESCE(bos.total_parts, 0) > 0 AND COALESCE(bos.observed_parts, 0) >= COALESCE(bos.total_parts, 0))::bigint AS binaries_complete,
					COUNT(bos.binary_id) FILTER (WHERE LOWER(COALESCE(bic.identity_strength, '')) IN ('weak', 'provisional'))::bigint AS binaries_weak
				FROM binary_observation_stats bos
				LEFT JOIN binary_identity_current bic
				  ON bic.source_posted_at = bos.source_posted_at
				 AND bic.binary_id = bos.binary_id
				WHERE bos.source_posted_at >= $3::date
				  AND bos.source_posted_at < $3::date + INTERVAL '1 day'
				  AND bos.provider_id = $1
				  AND bos.newsgroup_id = $2
			),
			release_stats AS (
				SELECT COUNT(DISTINCT r.release_id)::bigint AS releases_created
				FROM releases r
				JOIN release_files rf ON rf.release_id = r.release_id
				JOIN binary_core bc ON bc.binary_id = rf.binary_id
				WHERE COALESCE(r.posted_at, r.created_at) >= $3::date
				  AND COALESCE(r.posted_at, r.created_at) < $3::date + INTERVAL '1 day'
				  AND bc.provider_id = $1
				  AND bc.newsgroup_id = $2
			),
			upserted AS (
				INSERT INTO indexer_daily_bucket_stats (
					provider_id,
					newsgroup_id,
					bucket_day,
					tier,
					scrape_progress_known,
					scrape_progress_pct,
					lower_boundary_crossed,
					upper_boundary_crossed,
					bucket_article_low,
					bucket_article_high,
					scrape_cursor_low,
					scrape_cursor_high,
					headers_staged,
					unassembled_headers,
					yenc_ready,
					yenc_running,
					yenc_done,
					yenc_stale,
					binaries_total,
					binaries_complete,
					binaries_weak,
					releases_created,
					blocker_count,
					last_refreshed_at
				)
				SELECT
					$1,
					$2,
					$3::date,
					COALESCE(NULLIF(igp.tier_override, ''), NULLIF(igp.tier, ''), 'warm'),
					$4,
					CASE
						WHEN $4
						 AND $8::bigint >= $7::bigint
						 AND $7::bigint > 0
						THEN LEAST(100::double precision, GREATEST(0::double precision, (COALESCE(ss.headers_staged, 0)::double precision / NULLIF(($8::bigint - $7::bigint + 1)::double precision, 0)) * 100))
						ELSE 0::double precision
					END,
					$5,
					$6,
					CASE
						WHEN $7::bigint > 0 AND COALESCE(ss.article_low, 0) > 0 THEN LEAST($7::bigint, ss.article_low)
						WHEN $7::bigint > 0 THEN $7::bigint
						ELSE COALESCE(ss.article_low, 0)
					END,
					GREATEST($8::bigint, COALESCE(ss.article_high, 0)),
					COALESCE(ss.article_low, 0),
					COALESCE(ss.article_high, 0),
					COALESCE(ss.headers_staged, 0),
					COALESCE(ss.unassembled_headers, 0),
					COALESCE(ys.yenc_ready, 0),
					COALESCE(ys.yenc_running, 0),
					COALESCE(ys.yenc_done, 0),
					COALESCE(ys.yenc_stale, 0),
					COALESCE(bs.binaries_total, 0),
					COALESCE(bs.binaries_complete, 0),
					COALESCE(bs.binaries_weak, 0),
					COALESCE(rs.releases_created, 0),
					0::bigint,
					NOW()
				FROM source_stats ss
				CROSS JOIN yenc_stats ys
				CROSS JOIN binary_stats bs
				CROSS JOIN release_stats rs
				LEFT JOIN indexer_group_profiles igp
				  ON igp.provider_id = $1
				 AND igp.newsgroup_id = $2
				ON CONFLICT (provider_id, newsgroup_id, bucket_day)
				DO UPDATE SET
					tier = EXCLUDED.tier,
					scrape_progress_known = EXCLUDED.scrape_progress_known,
					scrape_progress_pct = EXCLUDED.scrape_progress_pct,
					lower_boundary_crossed = EXCLUDED.lower_boundary_crossed,
					upper_boundary_crossed = EXCLUDED.upper_boundary_crossed,
					bucket_article_low = EXCLUDED.bucket_article_low,
					bucket_article_high = EXCLUDED.bucket_article_high,
					scrape_cursor_low = EXCLUDED.scrape_cursor_low,
					scrape_cursor_high = EXCLUDED.scrape_cursor_high,
					headers_staged = EXCLUDED.headers_staged,
					unassembled_headers = EXCLUDED.unassembled_headers,
					yenc_ready = EXCLUDED.yenc_ready,
					yenc_running = EXCLUDED.yenc_running,
					yenc_done = EXCLUDED.yenc_done,
					yenc_stale = EXCLUDED.yenc_stale,
					binaries_total = EXCLUDED.binaries_total,
					binaries_complete = EXCLUDED.binaries_complete,
					binaries_weak = EXCLUDED.binaries_weak,
					releases_created = EXCLUDED.releases_created,
					blocker_count = EXCLUDED.blocker_count,
					last_refreshed_at = NOW()
				RETURNING 1
			)
			SELECT COUNT(*) FROM upserted`,
			b.ProviderID,
			b.NewsgroupID,
			b.BucketDay,
			b.ScrapeProgressKnown,
			b.LowerBoundaryCrossed,
			b.UpperBoundaryCrossed,
			b.BucketArticleLow,
			b.BucketArticleHigh,
		).Scan(&changed); err != nil {
			return refreshed, fmt.Errorf("refresh daily bucket provider=%d group=%d day=%s: %w", b.ProviderID, b.NewsgroupID, b.BucketDay.Format("2006-01-02"), err)
		}
		refreshed += changed
	}

	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM indexer_daily_bucket_stats
		WHERE bucket_day < $1`,
		cutoffDay,
	); err != nil {
		return refreshed, fmt.Errorf("delete old daily bucket stats: %w", err)
	}
	return refreshed, nil
}

func (s *Store) refreshIndexerDailyBucketStatsLegacy(ctx context.Context, days int) (int64, error) {
	var refreshed int64
	if err := s.db.QueryRowContext(ctx, `
		WITH bounds AS (
			SELECT (CURRENT_DATE - (($1::int - 1) * INTERVAL '1 day'))::date AS cutoff_day
		),
		boundary_days AS (
			SELECT
				sb.provider_id,
				sb.newsgroup_id,
				sb.bucket_day,
				sb.lower_boundary_crossed,
				sb.upper_boundary_crossed,
				sb.bucket_article_low,
				sb.bucket_article_high,
				CASE
					WHEN sb.lower_boundary_crossed
					 AND sb.upper_boundary_crossed
					 AND sb.bucket_article_low > 0
					 AND sb.bucket_article_high >= sb.bucket_article_low
					THEN true
					ELSE false
				END AS scrape_progress_known
			FROM indexer_scrape_day_boundaries sb, bounds b
			WHERE sb.bucket_day >= b.cutoff_day
		),
		source_days AS (
			SELECT
				bnd.provider_id,
				bnd.newsgroup_id,
				bnd.bucket_day,
				MIN(ah.article_number) AS article_low,
				MAX(ah.article_number) AS article_high,
				COUNT(ah.article_number) AS headers_staged,
				COUNT(ah.article_number) FILTER (WHERE ah.assembled_at IS NULL) AS unassembled_headers
			FROM boundary_days bnd
			LEFT JOIN LATERAL (
				SELECT ah.article_number, ah.assembled_at
				FROM article_headers ah
				WHERE ah.source_posted_at >= bnd.bucket_day
				  AND ah.source_posted_at < bnd.bucket_day + INTERVAL '1 day'
				  AND ah.provider_id = bnd.provider_id
				  AND ah.newsgroup_id = bnd.newsgroup_id
			) ah ON TRUE
			GROUP BY bnd.provider_id, bnd.newsgroup_id, bnd.bucket_day
		),
		yenc_days AS (
			SELECT
				wi.provider_id,
				wi.newsgroup_id,
				wi.partition_day AS bucket_day,
				COUNT(*) FILTER (WHERE wi.status = 'ready') AS yenc_ready,
				COUNT(*) FILTER (WHERE wi.status = 'running') AS yenc_running,
				COUNT(*) FILTER (WHERE wi.status = 'done') AS yenc_done,
				COUNT(*) FILTER (WHERE wi.status = 'stale') AS yenc_stale
			FROM yenc_recovery_work_items wi, bounds b
			WHERE wi.partition_day >= b.cutoff_day
			GROUP BY wi.provider_id, wi.newsgroup_id, wi.partition_day
		),
		binary_days AS (
			SELECT
				bnd.provider_id,
				bnd.newsgroup_id,
				bnd.bucket_day,
				COUNT(bos.binary_id) AS binaries_total,
				COUNT(bos.binary_id) FILTER (WHERE COALESCE(bos.total_parts, 0) > 0 AND COALESCE(bos.observed_parts, 0) >= COALESCE(bos.total_parts, 0)) AS binaries_complete,
				COUNT(bos.binary_id) FILTER (WHERE LOWER(COALESCE(bic.identity_strength, '')) IN ('weak', 'provisional')) AS binaries_weak
			FROM boundary_days bnd
			LEFT JOIN binary_observation_stats bos
			  ON bos.source_posted_at >= bnd.bucket_day
			 AND bos.source_posted_at < bnd.bucket_day + INTERVAL '1 day'
			 AND bos.provider_id = bnd.provider_id
			 AND bos.newsgroup_id = bnd.newsgroup_id
			LEFT JOIN binary_identity_current bic
			  ON bic.source_posted_at = bos.source_posted_at
			 AND bic.binary_id = bos.binary_id
			GROUP BY bnd.provider_id, bnd.newsgroup_id, bnd.bucket_day
		),
		release_days AS (
			SELECT
				bc.provider_id,
				bc.newsgroup_id,
				COALESCE(r.posted_at, r.created_at)::date AS bucket_day,
				COUNT(DISTINCT r.release_id) AS releases_created
			FROM releases r
			JOIN release_files rf ON rf.release_id = r.release_id
			JOIN binary_core bc ON bc.binary_id = rf.binary_id
			JOIN bounds b ON COALESCE(r.posted_at, r.created_at) >= b.cutoff_day
			GROUP BY bc.provider_id, bc.newsgroup_id, COALESCE(r.posted_at, r.created_at)::date
		),
		keys AS (
			SELECT provider_id, newsgroup_id, bucket_day FROM yenc_days
			UNION
			SELECT provider_id, newsgroup_id, bucket_day FROM release_days
			UNION
			SELECT provider_id, newsgroup_id, bucket_day FROM boundary_days
		),
		upserted AS (
			INSERT INTO indexer_daily_bucket_stats (
				provider_id,
				newsgroup_id,
				bucket_day,
				tier,
				scrape_progress_known,
				scrape_progress_pct,
				lower_boundary_crossed,
				upper_boundary_crossed,
				bucket_article_low,
				bucket_article_high,
				scrape_cursor_low,
				scrape_cursor_high,
				headers_staged,
				unassembled_headers,
				yenc_ready,
				yenc_running,
				yenc_done,
				yenc_stale,
				binaries_total,
				binaries_complete,
				binaries_weak,
				releases_created,
				blocker_count,
				last_refreshed_at
			)
			SELECT
				k.provider_id,
				k.newsgroup_id,
				k.bucket_day,
				COALESCE(NULLIF(igp.tier_override, ''), NULLIF(igp.tier, ''), 'warm') AS tier,
				COALESCE(bnd.scrape_progress_known, false),
				CASE
					WHEN COALESCE(bnd.scrape_progress_known, false)
					 AND COALESCE(bnd.bucket_article_high, 0) >= COALESCE(bnd.bucket_article_low, 0)
					 AND COALESCE(bnd.bucket_article_low, 0) > 0
					THEN LEAST(100::double precision, GREATEST(0::double precision, (COALESCE(sd.headers_staged, 0)::double precision / NULLIF((bnd.bucket_article_high - bnd.bucket_article_low + 1)::double precision, 0)) * 100))
					ELSE 0::double precision
				END,
				COALESCE(bnd.lower_boundary_crossed, false),
				COALESCE(bnd.upper_boundary_crossed, false),
				CASE
					WHEN COALESCE(bnd.bucket_article_low, 0) > 0 AND COALESCE(sd.article_low, 0) > 0 THEN LEAST(bnd.bucket_article_low, sd.article_low)
					WHEN COALESCE(bnd.bucket_article_low, 0) > 0 THEN bnd.bucket_article_low
					ELSE COALESCE(sd.article_low, 0)
				END,
				GREATEST(COALESCE(bnd.bucket_article_high, 0), COALESCE(sd.article_high, 0)),
				COALESCE(sd.article_low, 0),
				COALESCE(sd.article_high, 0),
				COALESCE(sd.headers_staged, 0),
				COALESCE(sd.unassembled_headers, 0),
				COALESCE(yd.yenc_ready, 0),
				COALESCE(yd.yenc_running, 0),
				COALESCE(yd.yenc_done, 0),
				COALESCE(yd.yenc_stale, 0),
				COALESCE(bd.binaries_total, 0),
				COALESCE(bd.binaries_complete, 0),
				COALESCE(bd.binaries_weak, 0),
				COALESCE(rd.releases_created, 0),
				0::bigint,
				NOW()
			FROM keys k
			LEFT JOIN indexer_group_profiles igp ON igp.provider_id = k.provider_id AND igp.newsgroup_id = k.newsgroup_id
			LEFT JOIN source_days sd ON sd.provider_id = k.provider_id AND sd.newsgroup_id = k.newsgroup_id AND sd.bucket_day = k.bucket_day
			LEFT JOIN yenc_days yd ON yd.provider_id = k.provider_id AND yd.newsgroup_id = k.newsgroup_id AND yd.bucket_day = k.bucket_day
			LEFT JOIN binary_days bd ON bd.provider_id = k.provider_id AND bd.newsgroup_id = k.newsgroup_id AND bd.bucket_day = k.bucket_day
			LEFT JOIN release_days rd ON rd.provider_id = k.provider_id AND rd.newsgroup_id = k.newsgroup_id AND rd.bucket_day = k.bucket_day
			LEFT JOIN boundary_days bnd ON bnd.provider_id = k.provider_id AND bnd.newsgroup_id = k.newsgroup_id AND bnd.bucket_day = k.bucket_day
			ON CONFLICT (provider_id, newsgroup_id, bucket_day)
			DO UPDATE SET
				tier = EXCLUDED.tier,
				scrape_progress_known = EXCLUDED.scrape_progress_known,
				scrape_progress_pct = EXCLUDED.scrape_progress_pct,
				lower_boundary_crossed = EXCLUDED.lower_boundary_crossed,
				upper_boundary_crossed = EXCLUDED.upper_boundary_crossed,
				bucket_article_low = EXCLUDED.bucket_article_low,
				bucket_article_high = EXCLUDED.bucket_article_high,
				scrape_cursor_low = EXCLUDED.scrape_cursor_low,
				scrape_cursor_high = EXCLUDED.scrape_cursor_high,
				headers_staged = EXCLUDED.headers_staged,
				unassembled_headers = EXCLUDED.unassembled_headers,
				yenc_ready = EXCLUDED.yenc_ready,
				yenc_running = EXCLUDED.yenc_running,
				yenc_done = EXCLUDED.yenc_done,
				yenc_stale = EXCLUDED.yenc_stale,
				binaries_total = EXCLUDED.binaries_total,
				binaries_complete = EXCLUDED.binaries_complete,
				binaries_weak = EXCLUDED.binaries_weak,
				releases_created = EXCLUDED.releases_created,
				blocker_count = EXCLUDED.blocker_count,
				last_refreshed_at = NOW()
			RETURNING 1
		),
		deleted_old AS (
			DELETE FROM indexer_daily_bucket_stats db
			USING bounds b
			WHERE db.bucket_day < b.cutoff_day
			RETURNING 1
		)
		SELECT COUNT(*) FROM upserted`,
		days,
	).Scan(&refreshed); err != nil {
		return 0, fmt.Errorf("refresh indexer daily bucket stats: %w", err)
	}
	return refreshed, nil
}
