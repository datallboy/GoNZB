package scrape

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestRunBackfillRespectsUntilDateBeforeInsert(t *testing.T) {
	cutoff := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	before := cutoff.Add(-time.Hour)
	onCutoff := cutoff
	after := cutoff.Add(time.Hour)
	repo := &fakeScrapeRepo{backfillCheckpoint: 3}
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 3},
		headers: []OverviewHeader{
			{ArticleNumber: 1, MessageID: "<before>", DateUTC: &before},
			{ArticleNumber: 2, MessageID: "<on-cutoff>", DateUTC: &onCutoff},
			{ArticleNumber: 3, MessageID: "<after>", DateUTC: &after},
		},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups:               []string{"alt.binaries.test"},
		BatchSize:                3,
		BackfillUntilDateByGroup: map[string]time.Time{"alt.binaries.test": cutoff},
	})

	metrics, err := svc.RunBackfillOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunBackfillOnceWithMetrics() error = %v", err)
	}
	if len(repo.insertedHeaders) != 2 {
		t.Fatalf("expected two inserted headers at/after cutoff, got %+v", repo.insertedHeaders)
	}
	for _, header := range repo.insertedHeaders {
		if header.DateUTC == nil || header.DateUTC.Before(cutoff) {
			t.Fatalf("inserted header before cutoff: %+v", header)
		}
	}
	if !repo.cutoffReached {
		t.Fatalf("expected cutoff to be marked reached")
	}
	if got := metrics["cutoff_filtered"]; got != int64(1) {
		t.Fatalf("expected cutoff_filtered=1, got %+v", got)
	}
	if repo.backfillCheckpointUpdated {
		t.Fatalf("expected checkpoint not to move past cutoff")
	}
}

func TestRunLatestDefersSourceDaysBeyondPerPassLimit(t *testing.T) {
	const sourceDays = 35
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	headers := make([]OverviewHeader, 0, sourceDays)
	for i := 0; i < sourceDays; i++ {
		postedAt := base.AddDate(0, 0, i)
		headers = append(headers, OverviewHeader{
			ArticleNumber: int64(i + 1),
			MessageID:     fmt.Sprintf("<article-%d>", i+1),
			DateUTC:       &postedAt,
		})
	}
	repo := &fakeScrapeRepo{}
	svc := NewService(repo, fakeScrapeProvider{
		stats:   GroupStats{Low: 1, High: sourceDays},
		headers: headers,
	}, testScrapeLogger{}, Options{
		Newsgroups:              []string{"alt.binaries.sparse"},
		BatchSize:               sourceDays,
		MaxNewSourceDaysPerPass: 32,
	})

	metrics, err := svc.RunLatestOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	}
	if len(repo.insertedHeaders) != 32 {
		t.Fatalf("expected newest 32 source days to be inserted, got %d", len(repo.insertedHeaders))
	}
	if got := repo.insertedHeaders[0].ArticleNumber; got != 4 {
		t.Fatalf("expected oldest admitted article to be 4, got %d", got)
	}
	if len(repo.deferredRanges) != 1 {
		t.Fatalf("expected one durable deferred range, got %+v", repo.deferredRanges)
	}
	deferred := repo.deferredRanges[0]
	if deferred.ArticleLow != 1 || deferred.ArticleHigh != 3 || deferred.Reason != "partition_source_day_cap" {
		t.Fatalf("unexpected deferred range: %+v", deferred)
	}
	if !repo.latestCheckpointUpdated || repo.latestCheckpointValue != sourceDays {
		t.Fatalf("expected checkpoint to advance only after durable deferral, got updated=%v value=%d", repo.latestCheckpointUpdated, repo.latestCheckpointValue)
	}
	if len(repo.observedRanges) != 1 || len(repo.observedRanges[0].observations) != 32 {
		t.Fatalf("expected observations only for admitted headers, got %+v", repo.observedRanges)
	}
	if got := metrics["new_source_days"]; got != 32 {
		t.Fatalf("expected new_source_days=32, got %v", got)
	}
}

func TestRunLatestExistingSourceDayDoesNotConsumeNewDayLimit(t *testing.T) {
	base := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	headers := make([]OverviewHeader, 0, 3)
	for i := 0; i < 3; i++ {
		postedAt := base.AddDate(0, 0, i)
		headers = append(headers, OverviewHeader{ArticleNumber: int64(i + 1), MessageID: fmt.Sprintf("<existing-%d>", i), DateUTC: &postedAt})
	}
	repo := &fakeScrapeRepo{existingScrapeDays: map[string]bool{base.Format("2006-01-02"): true}}
	svc := NewService(repo, fakeScrapeProvider{stats: GroupStats{Low: 1, High: 3}, headers: headers}, testScrapeLogger{}, Options{
		Newsgroups:              []string{"alt.binaries.existing"},
		BatchSize:               3,
		MaxNewSourceDaysPerPass: 2,
	})

	metrics, err := svc.RunLatestOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	}
	if len(repo.insertedHeaders) != 3 || len(repo.deferredRanges) != 0 {
		t.Fatalf("expected existing plus two new days to be admitted, inserted=%d deferred=%+v", len(repo.insertedHeaders), repo.deferredRanges)
	}
	if got := metrics["new_source_days"]; got != 2 {
		t.Fatalf("expected two newly admitted days, got %v", got)
	}
}

func TestRunTimeframesKeepsIndependentProgressForSameGroup(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeScrapeRepo{}
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 20},
		xoverFn: func(_ context.Context, _ string, from, to int64) ([]OverviewHeader, error) {
			rows := make([]OverviewHeader, 0, to-from+1)
			for article := from; article <= to; article++ {
				postedAt := base.AddDate(0, 0, int(article-1))
				rows = append(rows, OverviewHeader{
					ArticleNumber: article,
					MessageID:     fmt.Sprintf("<timeframe-%d>", article),
					DateUTC:       &postedAt,
				})
			}
			return rows, nil
		},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		BatchSize:  10,
		MaxBatches: 1,
		Timeframes: []Timeframe{
			{ID: "march-week", Group: "alt.binaries.history", Start: base.AddDate(0, 0, 2), End: base.AddDate(0, 0, 4)},
			{ID: "older-week", Group: "alt.binaries.history", Start: base.AddDate(0, 0, 9), End: base.AddDate(0, 0, 11)},
		},
	})

	metrics, err := svc.RunTimeframesOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunTimeframesOnceWithMetrics() error = %v", err)
	}
	if got := metrics["groups_scheduled"]; got != 1 {
		t.Fatalf("expected one timeframe to use the first pass budget, got groups_scheduled=%v", got)
	}
	if _, err := svc.RunTimeframesOnceWithMetrics(context.Background()); err != nil {
		t.Fatalf("second RunTimeframesOnceWithMetrics() error = %v", err)
	}
	if len(repo.timeframeProgress) != 2 {
		t.Fatalf("expected two progress records, got %+v", repo.timeframeProgress)
	}
	for _, id := range []string{"march-week", "older-week"} {
		progress := repo.timeframeProgress[timeframeProgressKey(id, 1, 1)]
		if progress == nil || progress.State != "completed" {
			t.Fatalf("expected %s to complete independently, got %+v", id, progress)
		}
	}
	if len(repo.insertedHeaders) != 4 {
		t.Fatalf("expected two articles from each date window, got %+v", repo.insertedHeaders)
	}
}

func TestRunBackfillSkipsGroupWhenCutoffReachedForAnotherProvider(t *testing.T) {
	cutoff := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeScrapeRepo{
		backfillCheckpoint: 10,
		groupCutoffReached: true,
	}
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 10},
		headers: []OverviewHeader{
			{ArticleNumber: 10, MessageID: "<ten>", DateUTC: &cutoff},
		},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups:               []string{"alt.binaries.test"},
		BatchSize:                10,
		BackfillUntilDateByGroup: map[string]time.Time{"alt.binaries.test": cutoff},
	})

	metrics, err := svc.RunBackfillOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunBackfillOnceWithMetrics() error = %v", err)
	}
	if len(repo.insertedHeaders) != 0 {
		t.Fatalf("expected no inserted headers once group cutoff already reached, got %+v", repo.insertedHeaders)
	}
	if repo.backfillCheckpointUpdated {
		t.Fatalf("expected no checkpoint update after group-wide cutoff skip")
	}
	if repo.cutoffReached {
		t.Fatalf("expected no per-provider cutoff rewrite when group-wide cutoff already reached")
	}
	if got := metrics["groups_with_work"]; got != 0 {
		t.Fatalf("expected groups_with_work=0, got %+v", got)
	}
}

func TestRunLatestUsesProviderThatServedGroupStatsForCheckpointsAndInsert(t *testing.T) {
	repo := &fakeScrapeRepo{providerIDsByKey: map[string]int64{"fake": 1, "newshosting": 2}}
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 10, ProviderID: "newshosting"},
		headers: []OverviewHeader{
			{ArticleNumber: 10, MessageID: "<ten>"},
		},
		xoverProviderID: "newshosting",
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups: []string{"alt.binaries.test"},
		BatchSize:  1,
	})

	if _, err := svc.RunLatestOnceWithMetrics(context.Background()); err != nil {
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	}
	if len(repo.insertProviderIDs) != 1 || repo.insertProviderIDs[0] != 2 {
		t.Fatalf("expected insert under provider 2, got %#v", repo.insertProviderIDs)
	}
	if len(repo.latestProviderIDs) != 1 || repo.latestProviderIDs[0] != 2 {
		t.Fatalf("expected checkpoint under provider 2, got %#v", repo.latestProviderIDs)
	}
}

func TestRunLatestUsesProviderThatServedXOverWhenItDiffersFromStats(t *testing.T) {
	repo := &fakeScrapeRepo{providerIDsByKey: map[string]int64{"fake": 1, "easynews": 2, "newshosting": 3}}
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 10, ProviderID: "easynews"},
		headers: []OverviewHeader{
			{ArticleNumber: 10, MessageID: "<ten>"},
		},
		xoverProviderID: "newshosting",
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups: []string{"alt.binaries.test"},
		BatchSize:  1,
	})

	if _, err := svc.RunLatestOnceWithMetrics(context.Background()); err != nil {
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	}
	if len(repo.insertProviderIDs) != 1 || repo.insertProviderIDs[0] != 3 {
		t.Fatalf("expected insert under xover provider 3, got %#v", repo.insertProviderIDs)
	}
	if len(repo.latestProviderIDs) != 1 || repo.latestProviderIDs[0] != 3 {
		t.Fatalf("expected checkpoint under xover provider 3, got %#v", repo.latestProviderIDs)
	}
}

func TestRunLatestOnceFailsWhenCriticalIndexIntegrityFails(t *testing.T) {
	svc := NewService(&fakeScrapeRepo{
		integrityReport: &pgindex.IndexerIntegrityReport{
			AmcheckAvailable: true,
			Checks: []pgindex.IndexerIntegrityCheck{{
				Relation:   "public.article_headers_newsgroup_id_message_id_key",
				MetadataOK: true,
				AmcheckRan: true,
				OK:         false,
				Detail:     "btree corruption detected",
			}},
		},
	}, fakeScrapeProvider{}, testScrapeLogger{}, Options{
		Newsgroups: []string{"alt.binaries.test"},
		BatchSize:  10,
	})

	_, err := svc.RunLatestOnceWithMetrics(context.Background())
	if err == nil {
		t.Fatal("expected integrity failure")
	}
	if !strings.Contains(err.Error(), "critical ingest index integrity failed") {
		t.Fatalf("expected integrity failure error, got %v", err)
	}
}

func TestRunLatestAdvancesSingleBatchPerRun(t *testing.T) {
	repo := &fakeScrapeRepo{latestCheckpoint: 100}
	var gotFrom, gotTo int64
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 110},
		xoverFn: func(_ context.Context, _ string, from, to int64) ([]OverviewHeader, error) {
			gotFrom, gotTo = from, to
			headers := make([]OverviewHeader, 0, to-from+1)
			for n := from; n <= to; n++ {
				headers = append(headers, OverviewHeader{
					ArticleNumber: n,
					MessageID:     "<msg>",
				})
			}
			return headers, nil
		},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups: []string{"alt.binaries.test"},
		BatchSize:  10,
	})

	metrics, err := svc.RunLatestOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	}
	if gotFrom != 101 || gotTo != 110 {
		t.Fatalf("expected XOVER range 101-110, got %d-%d", gotFrom, gotTo)
	}
	if !repo.latestCheckpointUpdated {
		t.Fatalf("expected latest checkpoint update")
	}
	if repo.latestCheckpointValue != 110 {
		t.Fatalf("expected latest checkpoint value 110, got %d", repo.latestCheckpointValue)
	}
	if got := metrics["ranges_fetched"]; got != 1 {
		t.Fatalf("expected ranges_fetched=1, got %+v", got)
	}
	if got := metrics["articles_inserted"]; got != int64(10) {
		t.Fatalf("expected articles_inserted=10, got %+v", got)
	}
}

func TestRunLatestSkipsCoordinatedActiveClaimWithoutAdvancing(t *testing.T) {
	repo := &fakeScrapeRepo{latestCheckpoint: 100}
	var xoverCalls int
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 110},
		xoverFn: func(context.Context, string, int64, int64) ([]OverviewHeader, error) {
			xoverCalls++
			return nil, nil
		},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups: []string{"alt.binaries.test"},
		BatchSize:  10,
		RangeCoordinator: &fakeRangeCoordinator{beginDecision: RangeDecision{
			Skipped:           true,
			AdvanceCheckpoint: false,
			Reason:            "active_claim",
		}},
	})

	metrics, err := svc.RunLatestOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	}
	if xoverCalls != 0 {
		t.Fatalf("expected coordinated skip to avoid XOVER, got %d calls", xoverCalls)
	}
	if repo.latestCheckpointUpdated {
		t.Fatalf("expected active claim skip not to advance latest checkpoint")
	}
	if got := metrics["ranges_skipped"]; got != 1 {
		t.Fatalf("expected ranges_skipped=1, got %+v", got)
	}
}

func TestRunLatestSkipsCompletedCoordinatedRangeAndAdvances(t *testing.T) {
	repo := &fakeScrapeRepo{latestCheckpoint: 100}
	var xoverCalls int
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 110},
		xoverFn: func(context.Context, string, int64, int64) ([]OverviewHeader, error) {
			xoverCalls++
			return nil, nil
		},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups: []string{"alt.binaries.test"},
		BatchSize:  10,
		RangeCoordinator: &fakeRangeCoordinator{beginDecision: RangeDecision{
			Skipped:           true,
			AdvanceCheckpoint: true,
			Reason:            "completed_range",
		}},
	})

	metrics, err := svc.RunLatestOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	}
	if xoverCalls != 0 {
		t.Fatalf("expected coordinated skip to avoid XOVER, got %d calls", xoverCalls)
	}
	if repo.latestCheckpointValue != 110 {
		t.Fatalf("expected completed skip to advance latest checkpoint to 110, got %d", repo.latestCheckpointValue)
	}
	if got := metrics["checkpoint_updates"]; got != 1 {
		t.Fatalf("expected checkpoint_updates=1, got %+v", got)
	}
}

func TestRunLatestCompletesCoordinatedRangeAfterFetch(t *testing.T) {
	repo := &fakeScrapeRepo{latestCheckpoint: 100}
	coord := &fakeRangeCoordinator{beginDecision: RangeDecision{ClaimID: "claim-1"}}
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 101},
		headers: []OverviewHeader{{
			ArticleNumber: 101,
			MessageID:     "<msg-101@test>",
		}},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups:       []string{"alt.binaries.test"},
		BatchSize:        1,
		RangeCoordinator: coord,
	})

	if _, err := svc.RunLatestOnceWithMetrics(context.Background()); err != nil {
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	}
	if len(coord.completed) != 1 {
		t.Fatalf("expected one completed range, got %+v", coord.completed)
	}
	got := coord.completed[0]
	if got.Group != "alt.binaries.test" || got.RangeStart != 101 || got.RangeEnd != 101 || got.ArticlesInserted != 1 {
		t.Fatalf("unexpected completed range: %+v", got)
	}
}

func TestRunLatestProcessesAssignedRangeWithoutConfiguredGroups(t *testing.T) {
	repo := &fakeScrapeRepo{}
	coord := &fakeRangeCoordinator{
		beginDecision: RangeDecision{ClaimID: "claim-1", AssignmentID: "assignment-1"},
		assigned: []RangeRequest{{
			AssignmentID: "assignment-1",
			Group:        "alt.binaries.assigned",
			RangeStart:   50,
			RangeEnd:     52,
		}},
	}
	var gotGroup string
	var gotFrom, gotTo int64
	provider := fakeScrapeProvider{
		xoverFn: func(_ context.Context, group string, from, to int64) ([]OverviewHeader, error) {
			gotGroup = group
			gotFrom = from
			gotTo = to
			return []OverviewHeader{
				{ArticleNumber: 50, MessageID: "<50@test>"},
				{ArticleNumber: 51, MessageID: "<51@test>"},
				{ArticleNumber: 52, MessageID: "<52@test>"},
			}, nil
		},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		BatchSize:        3,
		RangeCoordinator: coord,
	})

	metrics, err := svc.RunLatestOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	}
	if gotGroup != "alt.binaries.assigned" || gotFrom != 50 || gotTo != 52 {
		t.Fatalf("expected assigned XOVER range, got group=%s range=%d-%d", gotGroup, gotFrom, gotTo)
	}
	if len(coord.completed) != 1 || coord.completed[0].RangeStart != 50 || coord.completed[0].RangeEnd != 52 {
		t.Fatalf("expected assigned range completion, got %+v", coord.completed)
	}
	if repo.latestCheckpointUpdated || repo.backfillCheckpointUpdated {
		t.Fatalf("assigned range should not update scrape cursors")
	}
	if got := metrics["assigned_ranges"]; got != 1 {
		t.Fatalf("expected assigned_ranges=1, got %+v", got)
	}
	if got := metrics["articles_inserted"]; got != int64(3) {
		t.Fatalf("expected articles_inserted=3, got %+v", got)
	}
}

func TestRunLatestProcessesAssignedTimeWindowWithoutConfiguredGroups(t *testing.T) {
	repo := &fakeScrapeRepo{}
	windowStart := time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 7, 9, 5, 0, 0, 0, time.UTC)
	coord := &fakeRangeCoordinator{
		beginDecision: RangeDecision{ClaimID: "claim-window-1", AssignmentID: "assignment-window-1"},
		assigned: []RangeRequest{{
			AssignmentID: "assignment-window-1",
			Group:        "alt.binaries.window",
			WindowStart:  &windowStart,
			WindowEnd:    &windowEnd,
		}},
	}
	base := time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)
	headers := make([]OverviewHeader, 0, 10)
	for n := int64(100); n <= 109; n++ {
		postedAt := base.Add(time.Duration(n-100) * time.Hour)
		headers = append(headers, OverviewHeader{
			ArticleNumber: n,
			MessageID:     fmt.Sprintf("<%d@test>", n),
			DateUTC:       &postedAt,
		})
	}
	var gotGroup string
	var gotFrom, gotTo int64
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 100, High: 109},
		xoverFn: func(_ context.Context, group string, from, to int64) ([]OverviewHeader, error) {
			gotGroup = group
			gotFrom = from
			gotTo = to
			out := make([]OverviewHeader, 0, len(headers))
			for _, header := range headers {
				if header.ArticleNumber >= from && header.ArticleNumber <= to {
					out = append(out, header)
				}
			}
			return out, nil
		},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		BatchSize:        3,
		RangeCoordinator: coord,
	})

	metrics, err := svc.RunLatestOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	}
	if gotGroup != "alt.binaries.window" || gotFrom != 102 || gotTo != 104 {
		t.Fatalf("expected resolved window XOVER range, got group=%s range=%d-%d", gotGroup, gotFrom, gotTo)
	}
	if len(coord.requests) != 1 {
		t.Fatalf("expected one coordinated claim request, got %+v", coord.requests)
	}
	if coord.requests[0].RangeStart != 102 || coord.requests[0].RangeEnd != 104 || coord.requests[0].WindowStart == nil || coord.requests[0].WindowEnd == nil {
		t.Fatalf("expected resolved window claim request, got %+v", coord.requests[0])
	}
	if len(coord.completed) != 1 || coord.completed[0].RangeStart != 102 || coord.completed[0].RangeEnd != 104 {
		t.Fatalf("expected resolved window completion, got %+v", coord.completed)
	}
	if repo.latestCheckpointUpdated || repo.backfillCheckpointUpdated {
		t.Fatalf("assigned time window should not update scrape cursors")
	}
	if got := metrics["assigned_ranges"]; got != 1 {
		t.Fatalf("expected assigned_ranges=1, got %+v", got)
	}
	if got := metrics["articles_inserted"]; got != int64(3) {
		t.Fatalf("expected articles_inserted=3, got %+v", got)
	}
}

func TestRunLatestJumpsToHeadAndSeedsBackfillForLargeGap(t *testing.T) {
	repo := &fakeScrapeRepo{latestCheckpoint: 100, backfillCheckpoint: 25}
	var gotFrom, gotTo int64
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 1000},
		xoverFn: func(_ context.Context, _ string, from, to int64) ([]OverviewHeader, error) {
			gotFrom, gotTo = from, to
			headers := make([]OverviewHeader, 0, to-from+1)
			for n := from; n <= to; n++ {
				headers = append(headers, OverviewHeader{
					ArticleNumber: n,
					MessageID:     fmt.Sprintf("<msg-%d@test>", n),
				})
			}
			return headers, nil
		},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups: []string{"alt.binaries.test"},
		BatchSize:  100,
	})

	metrics, err := svc.RunLatestOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	}
	if gotFrom != 901 || gotTo != 1000 {
		t.Fatalf("expected latest to fetch head range 901-1000, got %d-%d", gotFrom, gotTo)
	}
	if repo.latestCheckpointValue != 1000 {
		t.Fatalf("expected latest checkpoint value 1000, got %d", repo.latestCheckpointValue)
	}
	if got := repo.backfillCheckpointByGroup["alt.binaries.test"]; got != 900 {
		t.Fatalf("expected backfill checkpoint seeded to gap high 900, got %d", got)
	}
	if len(repo.deferredRanges) != 1 {
		t.Fatalf("expected one deferred latest gap record, got %+v", repo.deferredRanges)
	}
	gap := repo.deferredRanges[0]
	if gap.ArticleLow != 101 || gap.ArticleHigh != 900 || gap.Reason != "latest_gap" {
		t.Fatalf("unexpected latest gap record: %+v", gap)
	}
	if got := metrics["ranges_fetched"]; got != 1 {
		t.Fatalf("expected ranges_fetched=1, got %+v", got)
	}
}

func TestRunLatestSanitizesEmbeddedNULsBeforeRepoInsert(t *testing.T) {
	repo := &fakeScrapeRepo{}
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 1},
		headers: []OverviewHeader{{
			ArticleNumber: 1,
			MessageID:     "<bad\x00msg@test>",
			Subject:       "bad\x00subject",
			Poster:        "bad\x00poster@test",
			DateUTC:       &now,
			Xref:          "xref\x00value",
			RawOverview: map[string]any{
				"Subject": "bad\x00subject",
			},
		}},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups: []string{"alt.binaries.test"},
		BatchSize:  10,
	})

	if _, err := svc.RunLatestOnceWithMetrics(context.Background()); err != nil {
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	}
	if len(repo.insertedHeaders) != 1 {
		t.Fatalf("expected 1 inserted header, got %d", len(repo.insertedHeaders))
	}
	header := repo.insertedHeaders[0]
	for _, value := range []string{header.MessageID, header.Subject, header.Poster, header.Xref} {
		if strings.ContainsRune(value, '\x00') {
			t.Fatalf("expected NUL stripped from %q", value)
		}
	}
	if subject, _ := header.RawOverview["Subject"].(string); strings.ContainsRune(subject, '\x00') {
		t.Fatalf("expected NUL stripped from raw overview subject %q", subject)
	}
}

func TestRunLatestRotatesAcrossGroupsByRunBudget(t *testing.T) {
	repo := &fakeScrapeRepo{}
	var mu sync.Mutex
	seen := make([]string, 0, 3)
	provider := fakeScrapeProvider{
		statsByGroup: map[string]GroupStats{
			"alt.one":   {Low: 1, High: 5},
			"alt.two":   {Low: 1, High: 5},
			"alt.three": {Low: 1, High: 5},
		},
		xoverFn: func(_ context.Context, group string, from, to int64) ([]OverviewHeader, error) {
			mu.Lock()
			seen = append(seen, group)
			mu.Unlock()
			return []OverviewHeader{{ArticleNumber: from, MessageID: "<msg>"}}, nil
		},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups:  []string{"alt.one", "alt.two", "alt.three"},
		BatchSize:   1,
		Concurrency: 1,
		MaxBatches:  1,
	})

	for range 3 {
		if _, err := svc.RunLatestOnceWithMetrics(context.Background()); err != nil {
			t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
		}
	}

	if !slices.Equal(seen, []string{"alt.one", "alt.two", "alt.three"}) {
		t.Fatalf("expected round-robin order [alt.one alt.two alt.three], got %+v", seen)
	}
}

func TestRunLatestUsesConcurrentWorkersWithinBudget(t *testing.T) {
	repo := &fakeScrapeRepo{}
	var current atomic.Int32
	var maxInFlight atomic.Int32
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	provider := fakeScrapeProvider{
		statsByGroup: map[string]GroupStats{
			"alt.one":   {Low: 1, High: 5},
			"alt.two":   {Low: 1, High: 5},
			"alt.three": {Low: 1, High: 5},
		},
		xoverFn: func(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error) {
			inFlight := current.Add(1)
			for {
				prev := maxInFlight.Load()
				if inFlight <= prev || maxInFlight.CompareAndSwap(prev, inFlight) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				current.Add(-1)
				return nil, ctx.Err()
			}
			current.Add(-1)
			return []OverviewHeader{{ArticleNumber: from, MessageID: "<msg>"}}, nil
		},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups:  []string{"alt.one", "alt.two", "alt.three"},
		BatchSize:   1,
		Concurrency: 2,
		MaxBatches:  3,
	})

	done := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		metrics, err := svc.RunLatestOnceWithMetrics(context.Background())
		if err != nil {
			errCh <- err
			return
		}
		done <- metrics
	}()

	<-started
	<-started
	close(release)

	select {
	case err := <-errCh:
		t.Fatalf("RunLatestOnceWithMetrics() error = %v", err)
	case metrics := <-done:
		if got := metrics["workers_used"]; got != 2 {
			t.Fatalf("expected workers_used=2, got %+v", got)
		}
		if got := metrics["groups_scheduled"]; got != 3 {
			t.Fatalf("expected groups_scheduled=3, got %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for concurrent scrape run")
	}

	if maxInFlight.Load() < 2 {
		t.Fatalf("expected at least 2 concurrent XOVER calls, got %d", maxInFlight.Load())
	}
}

func TestRunLatestCachesIntegrityPreflight(t *testing.T) {
	repo := &fakeScrapeRepo{}
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 1},
		headers: []OverviewHeader{{
			ArticleNumber: 1,
			MessageID:     "<msg@test>",
		}},
	}
	svc := NewService(repo, provider, testScrapeLogger{}, Options{
		Newsgroups: []string{"alt.binaries.test"},
		BatchSize:  10,
	})

	if _, err := svc.RunLatestOnceWithMetrics(context.Background()); err != nil {
		t.Fatalf("first RunLatestOnceWithMetrics() error = %v", err)
	}
	if _, err := svc.RunLatestOnceWithMetrics(context.Background()); err != nil {
		t.Fatalf("second RunLatestOnceWithMetrics() error = %v", err)
	}
	if repo.integrityCalls != 1 {
		t.Fatalf("expected 1 integrity preflight call, got %d", repo.integrityCalls)
	}
}

func TestRunLatestWarnsOnceWhenAmcheckUnavailable(t *testing.T) {
	repo := &fakeScrapeRepo{integrityReport: &pgindex.IndexerIntegrityReport{AmcheckAvailable: false}}
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 1},
		headers: []OverviewHeader{{
			ArticleNumber: 1,
			MessageID:     "<msg@test>",
		}},
	}
	log := &countingScrapeLogger{}
	svc := NewService(repo, provider, log, Options{
		Newsgroups: []string{"alt.binaries.test"},
		BatchSize:  10,
	})

	if _, err := svc.RunLatestOnceWithMetrics(context.Background()); err != nil {
		t.Fatalf("first RunLatestOnceWithMetrics() error = %v", err)
	}
	if _, err := svc.RunLatestOnceWithMetrics(context.Background()); err != nil {
		t.Fatalf("second RunLatestOnceWithMetrics() error = %v", err)
	}
	if log.warns != 1 {
		t.Fatalf("expected 1 amcheck warning, got %d", log.warns)
	}
}

type fakeScrapeRepo struct {
	backfillCheckpoint        int64
	latestCheckpoint          int64
	insertedHeaders           []pgindex.ArticleHeader
	integrityReport           *pgindex.IndexerIntegrityReport
	integrityErr              error
	integrityCalls            int
	cutoffReached             bool
	backfillCheckpointUpdated bool
	latestCheckpointUpdated   bool
	latestCheckpointValue     int64
	groupCutoffReached        bool
	nextNewsgroupID           int64
	providerIDsByKey          map[string]int64
	insertProviderIDs         []int64
	latestProviderIDs         []int64
	groupNamesByID            map[int64]string
	latestCheckpointByGroup   map[string]int64
	backfillCheckpointByGroup map[string]int64
	admissionSnapshot         *pgindex.YEncRecoveryAdmissionSnapshot
	deferredRanges            []pgindex.DeferredArticleRangeRecord
	observedRanges            []observedScrapeRange
	existingScrapeDays        map[string]bool
	timeframeProgress         map[string]*pgindex.ScrapeTimeframeProgress
}

type observedScrapeRange struct {
	providerID   int64
	newsgroupID  int64
	from         int64
	to           int64
	observations []pgindex.ScrapeRangeObservation
}

type fakeRangeCoordinator struct {
	beginDecision RangeDecision
	beginErr      error
	completeErr   error
	failErr       error
	assigned      []RangeRequest
	requests      []RangeRequest
	completed     []RangeResult
	failed        []error
}

func (f *fakeRangeCoordinator) AssignedScrapeRanges(context.Context, string, int) ([]RangeRequest, error) {
	return f.assigned, nil
}

func (f *fakeRangeCoordinator) BeginScrapeRange(_ context.Context, request RangeRequest) (RangeDecision, error) {
	f.requests = append(f.requests, request)
	return f.beginDecision, f.beginErr
}

func (f *fakeRangeCoordinator) CompleteScrapeRange(_ context.Context, _ RangeDecision, result RangeResult) error {
	f.completed = append(f.completed, result)
	return f.completeErr
}

func (f *fakeRangeCoordinator) FailScrapeRange(_ context.Context, _ RangeDecision, cause error) error {
	f.failed = append(f.failed, cause)
	return f.failErr
}

func (f *fakeScrapeRepo) EnsureProvider(_ context.Context, providerKey, _ string) (int64, error) {
	if f.providerIDsByKey == nil {
		f.providerIDsByKey = map[string]int64{"fake": 1}
	}
	if id := f.providerIDsByKey[providerKey]; id > 0 {
		return id, nil
	}
	id := int64(len(f.providerIDsByKey) + 1)
	f.providerIDsByKey[providerKey] = id
	return id, nil
}

func (f *fakeScrapeRepo) CheckCriticalIndexerIntegrity(context.Context, bool) (*pgindex.IndexerIntegrityReport, error) {
	f.integrityCalls++
	return f.integrityReport, f.integrityErr
}

func (f *fakeScrapeRepo) EnsureNewsgroup(_ context.Context, group string) (int64, error) {
	if f.groupNamesByID == nil {
		f.groupNamesByID = map[int64]string{}
	}
	for id, existing := range f.groupNamesByID {
		if existing == group {
			return id, nil
		}
	}
	f.nextNewsgroupID++
	if f.nextNewsgroupID <= 0 {
		f.nextNewsgroupID = 1
	}
	f.groupNamesByID[f.nextNewsgroupID] = group
	return f.nextNewsgroupID, nil
}

func (f *fakeScrapeRepo) StartScrapeRun(context.Context, int64) (int64, error) {
	return 1, nil
}

func (f *fakeScrapeRepo) FinishScrapeRun(context.Context, int64, string, string) error {
	return nil
}

func (f *fakeScrapeRepo) GetLatestCheckpoint(_ context.Context, _ int64, newsgroupID int64) (int64, error) {
	if len(f.latestCheckpointByGroup) == 0 {
		return f.latestCheckpoint, nil
	}
	return f.latestCheckpointByGroup[f.groupName(newsgroupID)], nil
}

func (f *fakeScrapeRepo) UpsertLatestCheckpoint(_ context.Context, providerID int64, newsgroupID int64, lastArticleNumber int64) error {
	f.latestCheckpointUpdated = true
	f.latestCheckpointValue = lastArticleNumber
	f.latestProviderIDs = append(f.latestProviderIDs, providerID)
	if f.latestCheckpointByGroup == nil {
		f.latestCheckpointByGroup = map[string]int64{}
	}
	f.latestCheckpointByGroup[f.groupName(newsgroupID)] = lastArticleNumber
	return nil
}

func (f *fakeScrapeRepo) GetBackfillCheckpoint(_ context.Context, _ int64, newsgroupID int64) (int64, error) {
	if len(f.backfillCheckpointByGroup) == 0 {
		return f.backfillCheckpoint, nil
	}
	return f.backfillCheckpointByGroup[f.groupName(newsgroupID)], nil
}

func (f *fakeScrapeRepo) UpsertBackfillCheckpoint(_ context.Context, _ int64, newsgroupID int64, backfillArticleNumber int64) error {
	f.backfillCheckpointUpdated = true
	if f.backfillCheckpointByGroup == nil {
		f.backfillCheckpointByGroup = map[string]int64{}
	}
	f.backfillCheckpointByGroup[f.groupName(newsgroupID)] = backfillArticleNumber
	return nil
}

func (f *fakeScrapeRepo) GetBackfillCheckpointState(context.Context, int64, int64) (*pgindex.BackfillCheckpointState, error) {
	return nil, nil
}

func (f *fakeScrapeRepo) HasBackfillCutoffReachedForGroup(context.Context, int64, time.Time) (bool, error) {
	return f.groupCutoffReached, nil
}

func (f *fakeScrapeRepo) SetBackfillCheckpointState(_ context.Context, _ int64, _ int64, _ *time.Time, cutoffReached bool, _ string) error {
	f.cutoffReached = cutoffReached
	return nil
}

func (f *fakeScrapeRepo) InsertArticleHeaders(_ context.Context, providerID int64, _ int64, headers []pgindex.ArticleHeader) (int64, error) {
	f.insertProviderIDs = append(f.insertProviderIDs, providerID)
	f.insertedHeaders = append(f.insertedHeaders, headers...)
	return int64(len(headers)), nil
}

func (f *fakeScrapeRepo) ObserveScrapeRange(_ context.Context, providerID, newsgroupID int64, from, to int64, observations []pgindex.ScrapeRangeObservation) error {
	f.observedRanges = append(f.observedRanges, observedScrapeRange{
		providerID:   providerID,
		newsgroupID:  newsgroupID,
		from:         from,
		to:           to,
		observations: append([]pgindex.ScrapeRangeObservation(nil), observations...),
	})
	return nil
}

func (f *fakeScrapeRepo) RefreshYEncRecoveryAdmissionSnapshot(context.Context) (*pgindex.YEncRecoveryAdmissionSnapshot, error) {
	if f.admissionSnapshot != nil {
		return f.admissionSnapshot, nil
	}
	return &pgindex.YEncRecoveryAdmissionSnapshot{
		ProbesPerHourEWMA: 25000,
		SoftCap:           100000,
		HardCap:           200000,
		RemainingToHard:   200000,
	}, nil
}

func (f *fakeScrapeRepo) UpsertIndexerGroupProfile(context.Context, int64, int64, string, string) error {
	return nil
}

func (f *fakeScrapeRepo) UpsertDeferredArticleRange(_ context.Context, in pgindex.DeferredArticleRangeRecord) error {
	f.deferredRanges = append(f.deferredRanges, in)
	return nil
}

func (f *fakeScrapeRepo) ExistingScrapeSourceDays(_ context.Context, sourcePostedAt []time.Time) (map[string]bool, error) {
	out := make(map[string]bool, len(sourcePostedAt))
	for _, postedAt := range sourcePostedAt {
		dayKey := postedAt.UTC().Format("2006-01-02")
		out[dayKey] = f.existingScrapeDays[dayKey]
	}
	return out, nil
}

func timeframeProgressKey(timeframeID string, providerID, newsgroupID int64) string {
	return fmt.Sprintf("%s/%d/%d", timeframeID, providerID, newsgroupID)
}

func (f *fakeScrapeRepo) EnsureScrapeTimeframeProgress(_ context.Context, timeframeID string, providerID, newsgroupID int64, windowStart, windowEnd time.Time) (*pgindex.ScrapeTimeframeProgress, error) {
	if f.timeframeProgress == nil {
		f.timeframeProgress = make(map[string]*pgindex.ScrapeTimeframeProgress)
	}
	key := timeframeProgressKey(timeframeID, providerID, newsgroupID)
	progress := f.timeframeProgress[key]
	if progress == nil || !progress.WindowStart.Equal(windowStart) || !progress.WindowEnd.Equal(windowEnd) {
		progress = &pgindex.ScrapeTimeframeProgress{
			TimeframeID: timeframeID,
			ProviderID:  providerID,
			NewsgroupID: newsgroupID,
			WindowStart: windowStart,
			WindowEnd:   windowEnd,
			State:       "pending",
		}
		f.timeframeProgress[key] = progress
	}
	copy := *progress
	return &copy, nil
}

func (f *fakeScrapeRepo) ResolveScrapeTimeframeProgress(_ context.Context, timeframeID string, providerID, newsgroupID, articleLow, articleHigh int64, empty bool) error {
	progress := f.timeframeProgress[timeframeProgressKey(timeframeID, providerID, newsgroupID)]
	progress.ArticleLow = articleLow
	progress.ArticleHigh = articleHigh
	progress.NextArticle = articleLow
	progress.State = "active"
	if empty {
		progress.State = "empty"
	}
	return nil
}

func (f *fakeScrapeRepo) AdvanceScrapeTimeframeProgress(_ context.Context, timeframeID string, providerID, newsgroupID, nextArticle int64, completed bool) error {
	progress := f.timeframeProgress[timeframeProgressKey(timeframeID, providerID, newsgroupID)]
	progress.NextArticle = nextArticle
	progress.State = "active"
	if completed {
		progress.State = "completed"
	}
	return nil
}

func (f *fakeScrapeRepo) FailScrapeTimeframeProgress(_ context.Context, timeframeID string, providerID, newsgroupID int64, cause string) error {
	progress := f.timeframeProgress[timeframeProgressKey(timeframeID, providerID, newsgroupID)]
	progress.State = "failed"
	progress.LastError = cause
	return nil
}

func (f *fakeScrapeRepo) groupName(newsgroupID int64) string {
	if f.groupNamesByID == nil {
		return ""
	}
	return f.groupNamesByID[newsgroupID]
}

type fakeScrapeProvider struct {
	stats           GroupStats
	statsByGroup    map[string]GroupStats
	headers         []OverviewHeader
	xoverFn         func(context.Context, string, int64, int64) ([]OverviewHeader, error)
	xoverProviderID string
}

func (f fakeScrapeProvider) ID() string {
	return "fake"
}

func (f fakeScrapeProvider) GroupStats(_ context.Context, group string) (GroupStats, error) {
	if f.statsByGroup != nil {
		if stats, ok := f.statsByGroup[group]; ok {
			return stats, nil
		}
	}
	return f.stats, nil
}

func (f fakeScrapeProvider) XOver(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error) {
	if f.xoverFn != nil {
		return f.xoverFn(ctx, group, from, to)
	}
	return append([]OverviewHeader(nil), f.headers...), nil
}

func (f fakeScrapeProvider) XOverWithProvider(ctx context.Context, group string, from, to int64) ([]OverviewHeader, string, error) {
	rows, err := f.XOver(ctx, group, from, to)
	providerID := f.xoverProviderID
	if providerID == "" {
		providerID = f.ID()
	}
	return rows, providerID, err
}

type testScrapeLogger struct{}

func (testScrapeLogger) Debug(string, ...interface{}) {}
func (testScrapeLogger) Info(string, ...interface{})  {}
func (testScrapeLogger) Warn(string, ...interface{})  {}
func (testScrapeLogger) Error(string, ...interface{}) {}

type countingScrapeLogger struct {
	warns int
}

func (*countingScrapeLogger) Debug(string, ...interface{}) {}
func (*countingScrapeLogger) Info(string, ...interface{})  {}
func (l *countingScrapeLogger) Warn(string, ...interface{}) {
	l.warns++
}
func (*countingScrapeLogger) Error(string, ...interface{}) {}
