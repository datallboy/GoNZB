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
			dbs.provider_id,
			up.provider_key,
			dbs.newsgroup_id,
			ng.group_name,
			dbs.bucket_day::text,
			dbs.tier,
			dbs.scrape_progress_known,
			dbs.lower_boundary_crossed,
			dbs.upper_boundary_crossed,
			dbs.bucket_article_low,
			dbs.bucket_article_high,
			dbs.scrape_cursor_low,
			dbs.scrape_cursor_high,
			dbs.headers_staged,
			dbs.unassembled_headers,
			dbs.yenc_ready,
			dbs.yenc_running,
			dbs.yenc_done,
			dbs.binaries_total,
			dbs.binaries_complete,
			dbs.binaries_weak,
			dbs.releases_created,
			dbs.blocker_count,
			dbs.last_refreshed_at
		FROM indexer_daily_bucket_stats dbs
		JOIN usenet_providers up ON up.id = dbs.provider_id
		JOIN newsgroups ng ON ng.id = dbs.newsgroup_id
		ORDER BY dbs.bucket_day DESC, dbs.last_refreshed_at DESC
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
