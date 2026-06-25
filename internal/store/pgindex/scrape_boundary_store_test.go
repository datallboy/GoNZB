package pgindex

import (
	"testing"
	"time"
)

func TestBuildScrapeDayBoundaryObservationsDetectsCrossedDayEdges(t *testing.T) {
	prev := time.Date(2026, 6, 23, 23, 59, 50, 0, time.UTC)
	dayA := time.Date(2026, 6, 24, 0, 0, 10, 0, time.UTC)
	dayB := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	next := time.Date(2026, 6, 25, 0, 0, 5, 0, time.UTC)

	items := BuildScrapeDayBoundaryObservations([]ScrapeRangeObservation{
		{ArticleNumber: 10, DateUTC: &prev},
		{ArticleNumber: 11, DateUTC: &dayA},
		{ArticleNumber: 20, DateUTC: &dayB},
		{ArticleNumber: 21, DateUTC: &next},
	})

	var target *scrapeDayBoundaryObservation
	for i := range items {
		if items[i].Day.Equal(time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)) {
			target = &items[i]
			break
		}
	}
	if target == nil {
		t.Fatalf("expected 2026-06-24 boundary observation, got %+v", items)
	}
	if !target.LowerCrossed || !target.UpperCrossed {
		t.Fatalf("expected both boundaries crossed, got %+v", *target)
	}
	if target.ArticleLow != 11 || target.ArticleHigh != 20 {
		t.Fatalf("expected day article bounds 11-20, got %d-%d", target.ArticleLow, target.ArticleHigh)
	}
	if target.ObservedArticleCount != 2 {
		t.Fatalf("expected two in-day observations, got %d", target.ObservedArticleCount)
	}
}
