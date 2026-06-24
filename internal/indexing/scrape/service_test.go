package scrape

import (
	"context"
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
