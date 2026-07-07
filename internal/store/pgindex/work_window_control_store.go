package pgindex

import (
	"context"
	"fmt"
	"time"
)

type DeferredArticleRangeRecord struct {
	ProviderID               int64
	NewsgroupID              int64
	ArticleLow               int64
	ArticleHigh              int64
	PostedAtMin              *time.Time
	PostedAtMax              *time.Time
	EstimatedArticleCount    int64
	EstimatedObfuscatedCount int64
	Reason                   string
	PriorityScore            float64
}

func (s *Store) UpsertIndexerGroupProfile(ctx context.Context, providerID, newsgroupID int64, tier, reason string) error {
	if providerID <= 0 || newsgroupID <= 0 {
		return fmt.Errorf("provider id and newsgroup id are required")
	}
	tier = normalizeIndexerGroupTier(tier)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO indexer_group_profiles (
			provider_id,
			newsgroup_id,
			tier,
			score,
			updated_at
		)
		VALUES ($1, $2, $3, 0, NOW())
		ON CONFLICT (provider_id, newsgroup_id) DO UPDATE
		SET tier = CASE
				WHEN indexer_group_profiles.tier_override IS NULL THEN EXCLUDED.tier
				ELSE indexer_group_profiles.tier
			END,
		    updated_at = NOW()`,
		providerID,
		newsgroupID,
		tier,
	); err != nil {
		return fmt.Errorf("upsert indexer group profile: %w", err)
	}
	return nil
}

func (s *Store) UpsertDeferredArticleRange(ctx context.Context, in DeferredArticleRangeRecord) error {
	if in.ProviderID <= 0 || in.NewsgroupID <= 0 {
		return fmt.Errorf("provider id and newsgroup id are required")
	}
	if in.ArticleLow <= 0 || in.ArticleHigh < in.ArticleLow {
		return fmt.Errorf("valid article range is required")
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO deferred_article_ranges (
			provider_id,
			newsgroup_id,
			article_low,
			article_high,
			posted_at_min,
			posted_at_max,
			estimated_article_count,
			estimated_obfuscated_count,
			reason,
			priority_score,
			state,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'ready', NOW())
		ON CONFLICT (provider_id, newsgroup_id, article_low, article_high) DO UPDATE
		SET posted_at_min = COALESCE(deferred_article_ranges.posted_at_min, EXCLUDED.posted_at_min),
		    posted_at_max = COALESCE(deferred_article_ranges.posted_at_max, EXCLUDED.posted_at_max),
		    estimated_article_count = GREATEST(deferred_article_ranges.estimated_article_count, EXCLUDED.estimated_article_count),
		    estimated_obfuscated_count = GREATEST(deferred_article_ranges.estimated_obfuscated_count, EXCLUDED.estimated_obfuscated_count),
		    reason = EXCLUDED.reason,
		    priority_score = GREATEST(deferred_article_ranges.priority_score, EXCLUDED.priority_score),
		    state = CASE
		    	WHEN deferred_article_ranges.state = 'completed' THEN deferred_article_ranges.state
		    	ELSE 'ready'
		    END,
		    updated_at = NOW()`,
		in.ProviderID,
		in.NewsgroupID,
		in.ArticleLow,
		in.ArticleHigh,
		in.PostedAtMin,
		in.PostedAtMax,
		in.EstimatedArticleCount,
		in.EstimatedObfuscatedCount,
		in.Reason,
		in.PriorityScore,
	); err != nil {
		return fmt.Errorf("upsert deferred article range: %w", err)
	}
	return nil
}

func normalizeIndexerGroupTier(tier string) string {
	switch tier {
	case "hot", "warm", "cold", "disabled":
		return tier
	default:
		return "warm"
	}
}
