package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type ScrapeTimeframeProgress struct {
	TimeframeID   string     `json:"timeframe_id"`
	ProviderID    int64      `json:"provider_id"`
	NewsgroupID   int64      `json:"newsgroup_id"`
	WindowStart   time.Time  `json:"window_start"`
	WindowEnd     time.Time  `json:"window_end"`
	ArticleLow    int64      `json:"article_low"`
	ArticleHigh   int64      `json:"article_high"`
	NextArticle   int64      `json:"next_article"`
	State         string     `json:"state"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	LastError     string     `json:"last_error"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type ScrapeTimeframeProgressSummary struct {
	ScrapeTimeframeProgress
	ProviderKey string `json:"provider_key"`
	GroupName   string `json:"group_name"`
}

func (s *Store) EnsureScrapeTimeframeProgress(ctx context.Context, timeframeID string, providerID, newsgroupID int64, windowStart, windowEnd time.Time) (*ScrapeTimeframeProgress, error) {
	timeframeID = strings.TrimSpace(timeframeID)
	windowStart = windowStart.UTC()
	windowEnd = windowEnd.UTC()
	if timeframeID == "" || providerID <= 0 || newsgroupID <= 0 || !windowEnd.After(windowStart) {
		return nil, fmt.Errorf("valid timeframe id, provider, newsgroup, and window are required")
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO indexer_scrape_timeframe_progress (
			timeframe_id, provider_id, newsgroup_id, window_start, window_end, state, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, 'pending', NOW())
		ON CONFLICT (timeframe_id, provider_id, newsgroup_id) DO UPDATE
		SET window_start = EXCLUDED.window_start,
		    window_end = EXCLUDED.window_end,
		    article_low = CASE
		      WHEN indexer_scrape_timeframe_progress.window_start IS DISTINCT FROM EXCLUDED.window_start
		        OR indexer_scrape_timeframe_progress.window_end IS DISTINCT FROM EXCLUDED.window_end THEN 0
		      ELSE indexer_scrape_timeframe_progress.article_low END,
		    article_high = CASE
		      WHEN indexer_scrape_timeframe_progress.window_start IS DISTINCT FROM EXCLUDED.window_start
		        OR indexer_scrape_timeframe_progress.window_end IS DISTINCT FROM EXCLUDED.window_end THEN 0
		      ELSE indexer_scrape_timeframe_progress.article_high END,
		    next_article = CASE
		      WHEN indexer_scrape_timeframe_progress.window_start IS DISTINCT FROM EXCLUDED.window_start
		        OR indexer_scrape_timeframe_progress.window_end IS DISTINCT FROM EXCLUDED.window_end THEN 0
		      ELSE indexer_scrape_timeframe_progress.next_article END,
		    state = CASE
		      WHEN indexer_scrape_timeframe_progress.window_start IS DISTINCT FROM EXCLUDED.window_start
		        OR indexer_scrape_timeframe_progress.window_end IS DISTINCT FROM EXCLUDED.window_end THEN 'pending'
		      ELSE indexer_scrape_timeframe_progress.state END,
		    resolved_at = CASE
		      WHEN indexer_scrape_timeframe_progress.window_start IS DISTINCT FROM EXCLUDED.window_start
		        OR indexer_scrape_timeframe_progress.window_end IS DISTINCT FROM EXCLUDED.window_end THEN NULL
		      ELSE indexer_scrape_timeframe_progress.resolved_at END,
		    completed_at = CASE
		      WHEN indexer_scrape_timeframe_progress.window_start IS DISTINCT FROM EXCLUDED.window_start
		        OR indexer_scrape_timeframe_progress.window_end IS DISTINCT FROM EXCLUDED.window_end THEN NULL
		      ELSE indexer_scrape_timeframe_progress.completed_at END,
		    last_error = CASE
		      WHEN indexer_scrape_timeframe_progress.window_start IS DISTINCT FROM EXCLUDED.window_start
		        OR indexer_scrape_timeframe_progress.window_end IS DISTINCT FROM EXCLUDED.window_end THEN ''
		      ELSE indexer_scrape_timeframe_progress.last_error END,
		    updated_at = NOW()`, timeframeID, providerID, newsgroupID, windowStart, windowEnd); err != nil {
		return nil, fmt.Errorf("ensure scrape timeframe progress %s: %w", timeframeID, err)
	}
	return s.loadScrapeTimeframeProgress(ctx, timeframeID, providerID, newsgroupID)
}

func (s *Store) ResolveScrapeTimeframeProgress(ctx context.Context, timeframeID string, providerID, newsgroupID, articleLow, articleHigh int64, empty bool) error {
	state := "active"
	nextArticle := articleLow
	var completedAt any
	if empty {
		state = "empty"
		articleLow = 0
		articleHigh = 0
		nextArticle = 0
		completedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE indexer_scrape_timeframe_progress
		SET article_low = $4, article_high = $5, next_article = $6,
		    state = $7, resolved_at = NOW(), completed_at = $8,
		    last_attempt_at = NOW(), last_error = '', updated_at = NOW()
		WHERE timeframe_id = $1 AND provider_id = $2 AND newsgroup_id = $3`,
		timeframeID, providerID, newsgroupID, articleLow, articleHigh, nextArticle, state, completedAt)
	if err != nil {
		return fmt.Errorf("resolve scrape timeframe progress %s: %w", timeframeID, err)
	}
	return nil
}

func (s *Store) AdvanceScrapeTimeframeProgress(ctx context.Context, timeframeID string, providerID, newsgroupID, nextArticle int64, completed bool) error {
	state := "active"
	var completedAt any
	if completed {
		state = "completed"
		completedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE indexer_scrape_timeframe_progress
		SET next_article = $4, state = $5, completed_at = $6,
		    last_attempt_at = NOW(), last_error = '', updated_at = NOW()
		WHERE timeframe_id = $1 AND provider_id = $2 AND newsgroup_id = $3`,
		timeframeID, providerID, newsgroupID, nextArticle, state, completedAt)
	if err != nil {
		return fmt.Errorf("advance scrape timeframe progress %s: %w", timeframeID, err)
	}
	return nil
}

func (s *Store) FailScrapeTimeframeProgress(ctx context.Context, timeframeID string, providerID, newsgroupID int64, cause string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE indexer_scrape_timeframe_progress
		SET state = 'failed', last_attempt_at = NOW(), last_error = $4, updated_at = NOW()
		WHERE timeframe_id = $1 AND provider_id = $2 AND newsgroup_id = $3`,
		timeframeID, providerID, newsgroupID, strings.TrimSpace(cause))
	if err != nil {
		return fmt.Errorf("fail scrape timeframe progress %s: %w", timeframeID, err)
	}
	return nil
}

func (s *Store) loadScrapeTimeframeProgress(ctx context.Context, timeframeID string, providerID, newsgroupID int64) (*ScrapeTimeframeProgress, error) {
	var item ScrapeTimeframeProgress
	var resolvedAt, completedAt, lastAttemptAt sql.NullTime
	if err := s.db.QueryRowContext(ctx, `
		SELECT timeframe_id, provider_id, newsgroup_id, window_start, window_end,
		       article_low, article_high, next_article, state,
		       resolved_at, completed_at, last_attempt_at, last_error, updated_at
		FROM indexer_scrape_timeframe_progress
		WHERE timeframe_id = $1 AND provider_id = $2 AND newsgroup_id = $3`,
		timeframeID, providerID, newsgroupID).Scan(
		&item.TimeframeID, &item.ProviderID, &item.NewsgroupID, &item.WindowStart, &item.WindowEnd,
		&item.ArticleLow, &item.ArticleHigh, &item.NextArticle, &item.State,
		&resolvedAt, &completedAt, &lastAttemptAt, &item.LastError, &item.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("load scrape timeframe progress %s: %w", timeframeID, err)
	}
	if resolvedAt.Valid {
		item.ResolvedAt = ptrUTC(resolvedAt.Time)
	}
	if completedAt.Valid {
		item.CompletedAt = ptrUTC(completedAt.Time)
	}
	if lastAttemptAt.Valid {
		item.LastAttemptAt = ptrUTC(lastAttemptAt.Time)
	}
	return &item, nil
}

func (s *Store) ListScrapeTimeframeProgress(ctx context.Context, limit int) ([]ScrapeTimeframeProgressSummary, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.timeframe_id, p.provider_id, p.newsgroup_id,
		       p.window_start, p.window_end, p.article_low, p.article_high,
		       p.next_article, p.state, p.resolved_at, p.completed_at,
		       p.last_attempt_at, p.last_error, p.updated_at,
		       up.provider_key, ng.group_name
		FROM indexer_scrape_timeframe_progress p
		JOIN usenet_providers up ON up.id = p.provider_id
		JOIN newsgroups ng ON ng.id = p.newsgroup_id
		ORDER BY CASE p.state
		           WHEN 'active' THEN 0
		           WHEN 'failed' THEN 1
		           WHEN 'pending' THEN 2
		           ELSE 3
		         END,
		         p.updated_at DESC,
		         p.timeframe_id
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list scrape timeframe progress: %w", err)
	}
	defer rows.Close()

	out := make([]ScrapeTimeframeProgressSummary, 0, limit)
	for rows.Next() {
		var item ScrapeTimeframeProgressSummary
		var resolvedAt, completedAt, lastAttemptAt sql.NullTime
		if err := rows.Scan(
			&item.TimeframeID, &item.ProviderID, &item.NewsgroupID,
			&item.WindowStart, &item.WindowEnd, &item.ArticleLow, &item.ArticleHigh,
			&item.NextArticle, &item.State, &resolvedAt, &completedAt,
			&lastAttemptAt, &item.LastError, &item.UpdatedAt,
			&item.ProviderKey, &item.GroupName,
		); err != nil {
			return nil, fmt.Errorf("scan scrape timeframe progress: %w", err)
		}
		if resolvedAt.Valid {
			item.ResolvedAt = ptrUTC(resolvedAt.Time)
		}
		if completedAt.Valid {
			item.CompletedAt = ptrUTC(completedAt.Time)
		}
		if lastAttemptAt.Valid {
			item.LastAttemptAt = ptrUTC(lastAttemptAt.Time)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scrape timeframe progress: %w", err)
	}
	return out, nil
}
