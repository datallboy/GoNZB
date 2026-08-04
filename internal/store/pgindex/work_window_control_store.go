package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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

type DeferredArticleRangeClaim struct {
	ID            int64
	ProviderID    int64
	ProviderKey   string
	NewsgroupID   int64
	GroupName     string
	ArticleLow    int64
	ArticleHigh   int64
	Reason        string
	PriorityScore float64
	Attempts      int
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
		    last_error = '',
		    state = CASE
		        WHEN deferred_article_ranges.state = 'completed' THEN deferred_article_ranges.state
		        WHEN deferred_article_ranges.state = 'running' THEN deferred_article_ranges.state
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

func (s *Store) ClaimDeferredArticleRange(ctx context.Context, owner string, lease time.Duration) (*DeferredArticleRangeClaim, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("deferred range claim owner is required")
	}
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	var item DeferredArticleRangeClaim
	err := s.db.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT id
			FROM deferred_article_ranges
			WHERE state = 'ready'
			   OR (state = 'running' AND (claim_until IS NULL OR claim_until < NOW()))
			ORDER BY priority_score DESC, posted_at_max DESC NULLS LAST, updated_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		), claimed AS (
			UPDATE deferred_article_ranges dar
			SET state = 'running',
			    claim_owner = $1,
			    claim_until = NOW() + $2::interval,
			    attempts = attempts + 1,
			    last_attempt_at = NOW(),
			    last_error = '',
			    updated_at = NOW()
			FROM candidate
			WHERE dar.id = candidate.id
			RETURNING dar.id, dar.provider_id, dar.newsgroup_id,
			          dar.article_low, dar.article_high, dar.reason, dar.priority_score, dar.attempts
		)
		SELECT claimed.id, claimed.provider_id, up.provider_key,
		       claimed.newsgroup_id, ng.group_name,
		       claimed.article_low, claimed.article_high,
		       claimed.reason, claimed.priority_score, claimed.attempts
		FROM claimed
		JOIN usenet_providers up ON up.id = claimed.provider_id
		JOIN newsgroups ng ON ng.id = claimed.newsgroup_id`, owner, lease.String()).Scan(
		&item.ID, &item.ProviderID, &item.ProviderKey,
		&item.NewsgroupID, &item.GroupName,
		&item.ArticleLow, &item.ArticleHigh,
		&item.Reason, &item.PriorityScore, &item.Attempts,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim deferred article range: %w", err)
	}
	return &item, nil
}

func (s *Store) CompleteDeferredArticleRange(ctx context.Context, id int64, owner string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE deferred_article_ranges
		SET state = 'completed', claim_owner = '', claim_until = NULL,
		    updated_at = NOW()
		WHERE id = $1 AND state = 'running' AND claim_owner = $2`, id, strings.TrimSpace(owner))
	if err != nil {
		return fmt.Errorf("complete deferred article range %d: %w", id, err)
	}
	return requireDeferredRangeClaimUpdate(result, id, "complete")
}

func (s *Store) FailDeferredArticleRange(ctx context.Context, id int64, owner, cause string, maxAttempts int) error {
	if maxAttempts <= 0 {
		maxAttempts = 8
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE deferred_article_ranges
		SET state = CASE WHEN attempts >= $4 THEN 'abandoned' ELSE 'ready' END,
		    claim_owner = '', claim_until = NULL,
		    last_error = $3, updated_at = NOW()
		WHERE id = $1 AND state = 'running' AND claim_owner = $2`, id, strings.TrimSpace(owner), strings.TrimSpace(cause), maxAttempts)
	if err != nil {
		return fmt.Errorf("fail deferred article range %d: %w", id, err)
	}
	return requireDeferredRangeClaimUpdate(result, id, "fail")
}

func requireDeferredRangeClaimUpdate(result sql.Result, id int64, action string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("cannot %s deferred article range %d: claim is no longer owned", action, id)
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
