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
		if err := rows.Scan(&item.ProviderID, &item.ProviderKey, &item.NewsgroupID, &item.GroupName, &item.BucketDay, &item.Tier, &item.ScrapeProgressKnown, &item.LowerBoundaryCrossed, &item.UpperBoundaryCrossed, &item.BucketArticleLow, &item.BucketArticleHigh, &item.ScrapeCursorLow, &item.ScrapeCursorHigh, &item.HeadersStaged, &item.UnassembledHeaders, &item.YEncReady, &item.YEncRunning, &item.YEncDone, &item.BinariesTotal, &item.BinariesComplete, &item.BinariesWeak, &item.ReleasesCreated, &item.BlockerCount, &item.LastRefreshedAt); err != nil {
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
	var refreshed int64
	if err := s.db.QueryRowContext(ctx, `
		WITH bounds AS (
			SELECT (CURRENT_DATE - (($1::int - 1) * INTERVAL '1 day'))::date AS cutoff_day
		),
		source_days AS (
			SELECT
				ah.provider_id,
				ah.newsgroup_id,
				ah.date_utc::date AS bucket_day,
				MIN(ah.article_number) AS article_low,
				MAX(ah.article_number) AS article_high,
				COUNT(*) AS headers_staged,
				COUNT(*) FILTER (WHERE ah.assembled_at IS NULL) AS unassembled_headers
			FROM article_headers ah, bounds b
			WHERE ah.date_utc >= b.cutoff_day
			GROUP BY ah.provider_id, ah.newsgroup_id, ah.date_utc::date
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
				bc.provider_id,
				bc.newsgroup_id,
				COALESCE(bos.posted_at, bc.created_at)::date AS bucket_day,
				COUNT(*) AS binaries_total,
				COUNT(*) FILTER (WHERE COALESCE(bos.total_parts, 0) > 0 AND COALESCE(bos.observed_parts, 0) >= COALESCE(bos.total_parts, 0)) AS binaries_complete,
				COUNT(*) FILTER (WHERE LOWER(COALESCE(bic.identity_strength, '')) IN ('weak', 'provisional')) AS binaries_weak
			FROM binary_core bc
			JOIN binary_observation_stats bos ON bos.binary_id = bc.binary_id
			LEFT JOIN binary_identity_current bic ON bic.binary_id = bc.binary_id
			JOIN bounds b ON COALESCE(bos.posted_at, bc.created_at) >= b.cutoff_day
			GROUP BY bc.provider_id, bc.newsgroup_id, COALESCE(bos.posted_at, bc.created_at)::date
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
			SELECT provider_id, newsgroup_id, bucket_day FROM source_days
			UNION
			SELECT provider_id, newsgroup_id, bucket_day FROM yenc_days
			UNION
			SELECT provider_id, newsgroup_id, bucket_day FROM binary_days
			UNION
			SELECT provider_id, newsgroup_id, bucket_day FROM release_days
		),
		upserted AS (
			INSERT INTO indexer_daily_bucket_stats (
				provider_id,
				newsgroup_id,
				bucket_day,
				tier,
				scrape_progress_known,
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
				false,
				false,
				false,
				COALESCE(sd.article_low, 0),
				COALESCE(sd.article_high, 0),
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
			ON CONFLICT (provider_id, newsgroup_id, bucket_day)
			DO UPDATE SET
				tier = EXCLUDED.tier,
				scrape_progress_known = EXCLUDED.scrape_progress_known,
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
