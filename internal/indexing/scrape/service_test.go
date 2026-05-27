package scrape

import (
	"context"
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

type fakeScrapeRepo struct {
	backfillCheckpoint        int64
	latestCheckpoint          int64
	insertedHeaders           []pgindex.ArticleHeader
	cutoffReached             bool
	backfillCheckpointUpdated bool
	latestCheckpointUpdated   bool
	latestCheckpointValue     int64
	groupCutoffReached        bool
}

func (f *fakeScrapeRepo) EnsureProvider(context.Context, string, string) (int64, error) {
	return 1, nil
}

func (f *fakeScrapeRepo) EnsureNewsgroup(context.Context, string) (int64, error) {
	return 1, nil
}

func (f *fakeScrapeRepo) StartScrapeRun(context.Context, int64) (int64, error) {
	return 1, nil
}

func (f *fakeScrapeRepo) FinishScrapeRun(context.Context, int64, string, string) error {
	return nil
}

func (f *fakeScrapeRepo) GetLatestCheckpoint(context.Context, int64, int64) (int64, error) {
	return f.latestCheckpoint, nil
}

func (f *fakeScrapeRepo) UpsertLatestCheckpoint(_ context.Context, _ int64, _ int64, lastArticleNumber int64) error {
	f.latestCheckpointUpdated = true
	f.latestCheckpointValue = lastArticleNumber
	return nil
}

func (f *fakeScrapeRepo) GetBackfillCheckpoint(context.Context, int64, int64) (int64, error) {
	return f.backfillCheckpoint, nil
}

func (f *fakeScrapeRepo) UpsertBackfillCheckpoint(context.Context, int64, int64, int64) error {
	f.backfillCheckpointUpdated = true
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

func (f *fakeScrapeRepo) InsertArticleHeaders(_ context.Context, _ int64, _ int64, headers []pgindex.ArticleHeader) (int64, error) {
	f.insertedHeaders = append(f.insertedHeaders, headers...)
	return int64(len(headers)), nil
}

type fakeScrapeProvider struct {
	stats   GroupStats
	headers []OverviewHeader
	xoverFn func(context.Context, string, int64, int64) ([]OverviewHeader, error)
}

func (f fakeScrapeProvider) ID() string {
	return "fake"
}

func (f fakeScrapeProvider) GroupStats(context.Context, string) (GroupStats, error) {
	return f.stats, nil
}

func (f fakeScrapeProvider) XOver(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error) {
	if f.xoverFn != nil {
		return f.xoverFn(ctx, group, from, to)
	}
	return append([]OverviewHeader(nil), f.headers...), nil
}

type testScrapeLogger struct{}

func (testScrapeLogger) Debug(string, ...interface{}) {}
func (testScrapeLogger) Info(string, ...interface{})  {}
func (testScrapeLogger) Warn(string, ...interface{})  {}
func (testScrapeLogger) Error(string, ...interface{}) {}

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

func TestRunLatestAdvancesSingleBatchPerRun(t *testing.T) {
	repo := &fakeScrapeRepo{latestCheckpoint: 100}
	var gotFrom, gotTo int64
	provider := fakeScrapeProvider{
		stats: GroupStats{Low: 1, High: 150},
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
