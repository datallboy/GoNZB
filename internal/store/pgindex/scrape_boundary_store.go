package pgindex

import (
	"context"
	"fmt"
	"time"
)

type scrapeDayBoundaryObservation struct {
	Day                  time.Time
	ArticleLow           int64
	ArticleHigh          int64
	ObservedArticleCount int64
	LowerCrossed         bool
	UpperCrossed         bool
}

func BuildScrapeDayBoundaryObservations(observations []ScrapeRangeObservation) []scrapeDayBoundaryObservation {
	type daySeen struct {
		low   int64
		high  int64
		count int64
	}
	byDay := map[time.Time]*daySeen{}
	var minDate *time.Time
	var maxDate *time.Time

	for _, observation := range observations {
		if observation.ArticleNumber <= 0 || observation.DateUTC == nil {
			continue
		}
		posted := observation.DateUTC.UTC()
		if minDate == nil || posted.Before(*minDate) {
			t := posted
			minDate = &t
		}
		if maxDate == nil || posted.After(*maxDate) {
			t := posted
			maxDate = &t
		}
		day := time.Date(posted.Year(), posted.Month(), posted.Day(), 0, 0, 0, 0, time.UTC)
		seen := byDay[day]
		if seen == nil {
			seen = &daySeen{low: observation.ArticleNumber, high: observation.ArticleNumber}
			byDay[day] = seen
		}
		if observation.ArticleNumber < seen.low {
			seen.low = observation.ArticleNumber
		}
		if observation.ArticleNumber > seen.high {
			seen.high = observation.ArticleNumber
		}
		seen.count++
	}
	if len(byDay) == 0 {
		return nil
	}

	out := make([]scrapeDayBoundaryObservation, 0, len(byDay))
	for day, seen := range byDay {
		dayEnd := day.Add(24 * time.Hour)
		item := scrapeDayBoundaryObservation{
			Day:                  day,
			ArticleLow:           seen.low,
			ArticleHigh:          seen.high,
			ObservedArticleCount: seen.count,
		}
		if minDate != nil && minDate.Before(day) {
			item.LowerCrossed = true
		}
		if maxDate != nil && !maxDate.Before(dayEnd) {
			item.UpperCrossed = true
		}
		out = append(out, item)
	}
	return out
}

func (s *Store) ObserveScrapeRange(ctx context.Context, providerID, newsgroupID int64, from, to int64, observations []ScrapeRangeObservation) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is required")
	}
	if providerID <= 0 || newsgroupID <= 0 {
		return fmt.Errorf("provider id and newsgroup id are required")
	}
	if from <= 0 || to < from || len(observations) == 0 {
		return nil
	}
	for _, item := range BuildScrapeDayBoundaryObservations(observations) {
		if err := upsertScrapeDayBoundaryObservation(ctx, s.db, providerID, newsgroupID, item); err != nil {
			return err
		}
	}
	return nil
}

func upsertScrapeDayBoundaryObservation(ctx context.Context, exec sqlExecQueryer, providerID, newsgroupID int64, item scrapeDayBoundaryObservation) error {
	if item.ArticleLow <= 0 || item.ArticleHigh < item.ArticleLow {
		return nil
	}
	_, err := exec.ExecContext(ctx, `
		INSERT INTO indexer_scrape_day_boundaries (
			provider_id,
			newsgroup_id,
			bucket_day,
			lower_boundary_crossed,
			upper_boundary_crossed,
			bucket_article_low,
			bucket_article_high,
			observed_article_count,
			first_observed_at,
			last_observed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		ON CONFLICT (provider_id, newsgroup_id, bucket_day)
		DO UPDATE SET
			lower_boundary_crossed = indexer_scrape_day_boundaries.lower_boundary_crossed OR EXCLUDED.lower_boundary_crossed,
			upper_boundary_crossed = indexer_scrape_day_boundaries.upper_boundary_crossed OR EXCLUDED.upper_boundary_crossed,
			bucket_article_low = CASE
				WHEN indexer_scrape_day_boundaries.bucket_article_low <= 0 THEN EXCLUDED.bucket_article_low
				ELSE LEAST(indexer_scrape_day_boundaries.bucket_article_low, EXCLUDED.bucket_article_low)
			END,
			bucket_article_high = GREATEST(indexer_scrape_day_boundaries.bucket_article_high, EXCLUDED.bucket_article_high),
			observed_article_count = indexer_scrape_day_boundaries.observed_article_count + EXCLUDED.observed_article_count,
			last_observed_at = NOW()`,
		providerID,
		newsgroupID,
		item.Day,
		item.LowerCrossed,
		item.UpperCrossed,
		item.ArticleLow,
		item.ArticleHigh,
		item.ObservedArticleCount,
	)
	if err != nil {
		return fmt.Errorf("upsert scrape day boundary observation: %w", err)
	}
	return nil
}
