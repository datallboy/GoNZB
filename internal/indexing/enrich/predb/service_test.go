package predb

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type fakeRepo struct {
	candidates  []pgindex.ReleaseEnrichmentCandidate
	rows        map[string][]pgindex.ReleasePredbMatchRecord
	updates     []pgindex.ReleasePredbUpdate
	upserts     []pgindex.PredbEntryRecord
	entries     []pgindex.PredbEntrySummary
	entryLimit  int
	category    string
	window      *pgindex.PredbBackfillWindow
	entryWindow *pgindex.PredbBackfillWindow
	checkpoint  *pgindex.PredbBackfillCheckpoint
}

func (f *fakeRepo) ListReleaseEnrichmentCandidates(context.Context, string, int) ([]pgindex.ReleaseEnrichmentCandidate, error) {
	return append([]pgindex.ReleaseEnrichmentCandidate(nil), f.candidates...), nil
}

func (f *fakeRepo) GetPredbBackfillWindow(context.Context) (*pgindex.PredbBackfillWindow, error) {
	return f.window, nil
}

func (f *fakeRepo) GetPredbEntryWindow(context.Context) (*pgindex.PredbBackfillWindow, error) {
	return f.entryWindow, nil
}

func (f *fakeRepo) GetPredbBackfillCheckpoint(context.Context, string) (*pgindex.PredbBackfillCheckpoint, error) {
	return f.checkpoint, nil
}

func (f *fakeRepo) UpsertPredbEntries(_ context.Context, rows []pgindex.PredbEntryRecord) error {
	f.upserts = append(f.upserts, rows...)
	return nil
}

func (f *fakeRepo) UpsertPredbBackfillCheckpoint(_ context.Context, in pgindex.PredbBackfillCheckpoint) error {
	copy := in
	f.checkpoint = &copy
	return nil
}

func (f *fakeRepo) ListPredbEntriesForWindow(_ context.Context, _ *time.Time, _ *time.Time, categoryHint string, limit int) ([]pgindex.PredbEntrySummary, error) {
	f.category = categoryHint
	f.entryLimit = limit
	return append([]pgindex.PredbEntrySummary(nil), f.entries...), nil
}

func (f *fakeRepo) ReplaceReleasePredbMatches(_ context.Context, releaseID string, rows []pgindex.ReleasePredbMatchRecord) error {
	if f.rows == nil {
		f.rows = map[string][]pgindex.ReleasePredbMatchRecord{}
	}
	f.rows[releaseID] = append([]pgindex.ReleasePredbMatchRecord(nil), rows...)
	return nil
}

func (f *fakeRepo) ApplyReleasePredbUpdate(_ context.Context, in pgindex.ReleasePredbUpdate) error {
	f.updates = append(f.updates, in)
	return nil
}

type fakeProvider struct {
	matches []Match
	err     error
}

func (f fakeProvider) ProviderName() string { return "predb.club" }

func (f fakeProvider) Search(context.Context, Query) ([]Match, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]Match(nil), f.matches...), nil
}

type fakeLogger struct{}

func (fakeLogger) Debug(string, ...interface{}) {}
func (fakeLogger) Info(string, ...interface{})  {}
func (fakeLogger) Warn(string, ...interface{})  {}
func (fakeLogger) Error(string, ...interface{}) {}

type fakeBackfillProvider struct {
	pages [][]pgindex.PredbEntryRecord
}

func (f fakeBackfillProvider) ProviderName() string { return "predb.club" }

func (f fakeBackfillProvider) FetchPage(_ context.Context, offset, limit int) ([]pgindex.PredbEntryRecord, bool, error) {
	if limit <= 0 {
		limit = 1
	}
	index := 0
	if limit > 0 {
		index = offset / limit
	}
	if index < 0 || index >= len(f.pages) {
		return nil, false, nil
	}
	more := index < len(f.pages)-1
	return append([]pgindex.PredbEntryRecord(nil), f.pages[index]...), more, nil
}

func TestDeriveQueryUsesCanonicalTVIdentity(t *testing.T) {
	query, ok := deriveQuery(pgindex.ReleaseEnrichmentCandidate{
		Title:             "xYzObfuscated.vol03+01",
		MatchedMediaTitle: "Example Show",
		ExternalMediaType: "tv",
		SeasonNumber:      8,
		EpisodeNumber:     16,
	})
	if !ok {
		t.Fatal("expected query")
	}
	if query.Text != "Example Show S08E16" {
		t.Fatalf("unexpected query text: %+v", query)
	}
}

func TestRunOnceAppliesPredbTitleAsFallback(t *testing.T) {
	repo := &fakeRepo{
		candidates: []pgindex.ReleaseEnrichmentCandidate{{
			ReleaseID:         "rel-1",
			Title:             "xYzObfuscated.vol03+01",
			SourceTitle:       "xYzObfuscated.vol03+01",
			TitleSource:       "source",
			MatchedMediaTitle: "Example Feature",
			ExternalYear:      1963,
		}},
	}
	svc := &Service{
		repo:           repo,
		log:            fakeLogger{},
		opts:           DefaultOptions(Options{}),
		searchProvider: fakeProvider{matches: []Match{{Title: "Example.Feature.1963.1080p.BluRay.x265-GROUP", Source: "predb.club"}}},
	}

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("expected one update, got %d", len(repo.updates))
	}
	update := repo.updates[0]
	if update.TitleSource != "predb" {
		t.Fatalf("expected title source predb, got %+v", update)
	}
	if update.Title != "Example Feature 1963 1080p BluRay x265-GROUP" {
		t.Fatalf("unexpected display title: %+v", update)
	}
	if update.DeobfuscatedTitle != "Example.Feature.1963.1080p.BluRay.x265-GROUP" {
		t.Fatalf("unexpected release title: %+v", update)
	}
}

func TestRunOnceAppliesPredbTitleForSourceObfuscatedRelease(t *testing.T) {
	repo := &fakeRepo{
		candidates: []pgindex.ReleaseEnrichmentCandidate{{
			ReleaseID:         "rel-obfuscated",
			Title:             "unknown-release",
			SourceTitle:       "tVXofScxpPrnpZg9WDP3g2uxRzTFfwbK.vol01+02",
			TitleSource:       "source_obfuscated",
			MatchedMediaTitle: "Example Feature",
			ExternalYear:      1963,
		}},
	}
	svc := &Service{
		repo:           repo,
		log:            fakeLogger{},
		opts:           DefaultOptions(Options{}),
		searchProvider: fakeProvider{matches: []Match{{Title: "Example.Feature.1963.720p.HDTV.x264-GRP", Source: "predb.club"}}},
	}

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("expected one update, got %d", len(repo.updates))
	}
	update := repo.updates[0]
	if update.TitleSource != "predb" {
		t.Fatalf("expected source_obfuscated release to accept predb title, got %+v", update)
	}
	if update.Title != "Example Feature 1963 720p HDTV x264-GRP" {
		t.Fatalf("unexpected display title: %+v", update)
	}
}

func TestRunOnceSkipsRecoverablePredbRateLimit(t *testing.T) {
	repo := &fakeRepo{
		candidates: []pgindex.ReleaseEnrichmentCandidate{{
			ReleaseID:         "rel-1",
			Title:             "unknown-release",
			SourceTitle:       "unknown-release",
			TitleSource:       "source",
			MatchedMediaTitle: "Example Feature",
			ExternalYear:      1963,
		}},
	}
	svc := &Service{
		repo:           repo,
		log:            fakeLogger{},
		opts:           DefaultOptions(Options{}),
		searchProvider: fakeProvider{err: fmt.Errorf("predb.club returned status 429")},
	}

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if len(repo.updates) != 0 {
		t.Fatalf("expected no updates during rate limit, got %d", len(repo.updates))
	}
}

func TestMetadataFallbackUsesPayloadSizeAsPrimaryEvidence(t *testing.T) {
	postedAt := time.Date(2026, 6, 26, 18, 52, 47, 0, time.UTC)
	repo := &fakeRepo{
		candidates: []pgindex.ReleaseEnrichmentCandidate{{
			ReleaseID:         "opaque-media",
			Title:             "tVXofScxpPrnpZg9WDP3g2uxRzTFfwbK vol01+02",
			TitleSource:       "source_obfuscated",
			PostedAt:          &postedAt,
			PayloadSizeBytes:  571_231_197,
			RuntimeSeconds:    1397,
			PrimaryResolution: "720p",
			PrimaryVideoCodec: "h264",
			PrimaryAudioCodec: "eac3",
		}},
		entries: []pgindex.PredbEntrySummary{{
			EntryID:         1,
			ExternalID:      100,
			NormalizedTitle: "example.feature.2026.720p.web.h264-grp",
			Title:           "Example.Feature.2026.720p.WEB.H264-GRP",
			Category:        "TV",
			Source:          "predb.club",
			SizeKB:          float64(571_231_197) / 1024,
			PostedAt:        &postedAt,
		}},
	}
	svc := &Service{repo: repo, log: fakeLogger{}, opts: DefaultOptions(Options{})}

	if err := svc.RunMetadataFallbackOnce(context.Background()); err != nil {
		t.Fatalf("RunMetadataFallbackOnce failed: %v", err)
	}
	if repo.entryLimit != 1000 {
		t.Fatalf("expected wider local predb window limit, got %d", repo.entryLimit)
	}
	if repo.category != "" {
		t.Fatalf("runtime alone should not force movie category hint, got %q", repo.category)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("expected one PreDB update, got %d", len(repo.updates))
	}
	if repo.updates[0].Title != "Example Feature 2026 720p WEB H264-GRP" {
		t.Fatalf("unexpected title update: %+v", repo.updates[0])
	}
	if repo.updates[0].TitleConfidence < 0.90 {
		t.Fatalf("expected high-confidence metadata fallback, got %.3f", repo.updates[0].TitleConfidence)
	}
}

func TestMetadataFallbackRejectsTimeOnlyMatch(t *testing.T) {
	postedAt := time.Date(2026, 6, 26, 18, 52, 47, 0, time.UTC)
	query, ok := deriveMetadataFallbackQuery(pgindex.ReleaseEnrichmentCandidate{
		PostedAt: &postedAt,
	})
	if !ok {
		t.Fatal("expected fallback query")
	}
	score := scoreMetadataFallback(query, pgindex.PredbEntrySummary{
		Title:    "Unrelated.Release.2026-GRP",
		PostedAt: &postedAt,
	})
	if score >= 0.90 {
		t.Fatalf("time-only match should not be high confidence, got %.3f", score)
	}
}

func TestMetadataFallbackScoresLegacyPredbClubMegabyteSize(t *testing.T) {
	postedAt := time.Date(2026, 6, 26, 18, 52, 47, 0, time.UTC)
	query, ok := deriveMetadataFallbackQuery(pgindex.ReleaseEnrichmentCandidate{
		PostedAt:          &postedAt,
		PayloadSizeBytes:  571_231_197,
		PrimaryResolution: "720p",
		PrimaryVideoCodec: "h264",
	})
	if !ok {
		t.Fatal("expected fallback query")
	}
	score := scoreMetadataFallback(query, pgindex.PredbEntrySummary{
		Title:    "Example.Feature.2026.720p.WEB.H264-GRP",
		Category: "TV-720P",
		SizeKB:   float64(571_231_197) / 1024 / 1024,
		PostedAt: &postedAt,
	})
	if score < 0.90 {
		t.Fatalf("expected legacy MB-valued size to score as high confidence, got %.3f", score)
	}
}

func TestMetadataFallbackRecordsButDoesNotApplyFarTimeSizeMatch(t *testing.T) {
	postedAt := time.Date(2026, 6, 26, 18, 52, 47, 0, time.UTC)
	predbAt := postedAt.Add(-51 * time.Minute)
	repo := &fakeRepo{
		candidates: []pgindex.ReleaseEnrichmentCandidate{{
			ReleaseID:         "opaque-media",
			Title:             "opaque-token",
			TitleSource:       "source_obfuscated",
			PostedAt:          &postedAt,
			PayloadSizeBytes:  571_231_197,
			PrimaryResolution: "720p",
			PrimaryVideoCodec: "h264",
		}},
		entries: []pgindex.PredbEntrySummary{{
			EntryID:         1,
			ExternalID:      100,
			NormalizedTitle: "wrong.but.same.size.720p.h264",
			Title:           "Wrong.But.Same.Size.720p.H264-GRP",
			Category:        "TV-720P",
			Source:          "predb.club",
			SizeKB:          float64(571_231_197) / 1024 / 1024,
			PostedAt:        &predbAt,
		}},
	}
	svc := &Service{repo: repo, log: fakeLogger{}, opts: DefaultOptions(Options{})}

	if err := svc.RunMetadataFallbackOnce(context.Background()); err != nil {
		t.Fatalf("RunMetadataFallbackOnce failed: %v", err)
	}
	if len(repo.updates) != 0 {
		t.Fatalf("expected no auto-applied update for far time match, got %d", len(repo.updates))
	}
	rows := repo.rows["opaque-media"]
	if len(rows) != 1 {
		t.Fatalf("expected manual-review PreDB candidate to be recorded, got %d", len(rows))
	}
	if rows[0].Chosen {
		t.Fatalf("far time match should not be marked chosen")
	}
}

func TestRunSyncBackfillOnceStopsAfterWindowCovered(t *testing.T) {
	from := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	repo := &fakeRepo{
		window: &pgindex.PredbBackfillWindow{From: &from, To: &to},
	}
	page1Time := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	page2Time := time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)
	svc := &Service{
		repo: repo,
		log:  fakeLogger{},
		opts: DefaultOptions(Options{
			BackfillPageSize: 2,
			MaxBackfillPages: 10,
		}),
		backfill: fakeBackfillProvider{pages: [][]pgindex.PredbEntryRecord{
			{
				{Title: "Recent.One", PostedAt: &page1Time, Source: "predb.club"},
				{Title: "Recent.Two", PostedAt: &page1Time, Source: "predb.club"},
			},
			{
				{Title: "Older.One", PostedAt: &page2Time, Source: "predb.club"},
			},
		}},
	}

	if err := svc.RunSyncBackfillOnce(context.Background()); err != nil {
		t.Fatalf("RunSyncBackfillOnce failed: %v", err)
	}
	if got := len(repo.upserts); got != 3 {
		t.Fatalf("expected 3 backfilled rows, got %d", got)
	}
}

func TestRunSyncBackfillOnceSeeksFromOldestLocalEntry(t *testing.T) {
	from := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	localOldest := time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC)
	repo := &fakeRepo{
		window:      &pgindex.PredbBackfillWindow{From: &from, To: &to},
		entryWindow: &pgindex.PredbBackfillWindow{From: &localOldest, To: &to},
	}
	page1Newest := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	page1Oldest := time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)
	page2Newest := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	page2Oldest := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	svc := &Service{
		repo: repo,
		log:  fakeLogger{},
		opts: DefaultOptions(Options{
			BackfillPageSize: 2,
			MaxBackfillPages: 10,
		}),
		backfill: fakeBackfillProvider{pages: [][]pgindex.PredbEntryRecord{
			{
				{Title: "Recent.One", PostedAt: &page1Newest, Source: "predb.club"},
				{Title: "Recent.Two", PostedAt: &page1Oldest, Source: "predb.club"},
			},
			{
				{Title: "Older.One", PostedAt: &page2Newest, Source: "predb.club"},
				{Title: "Older.Two", PostedAt: &page2Oldest, Source: "predb.club"},
			},
		}},
	}

	if err := svc.RunSyncBackfillOnce(context.Background()); err != nil {
		t.Fatalf("RunSyncBackfillOnce failed: %v", err)
	}
	if got := len(repo.upserts); got != 2 {
		t.Fatalf("expected only boundary-and-older rows to upsert, got %d", got)
	}
	if repo.upserts[0].Title != "Older.One" {
		t.Fatalf("expected backfill to skip newest page and start at older boundary, got %+v", repo.upserts)
	}
}

func TestRunSyncBackfillOnceUsesCheckpointOffsetHint(t *testing.T) {
	from := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	anchorTime := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	repo := &fakeRepo{
		window:      &pgindex.PredbBackfillWindow{From: &from, To: &to},
		entryWindow: &pgindex.PredbBackfillWindow{From: &anchorTime, To: &to},
		checkpoint: &pgindex.PredbBackfillCheckpoint{
			Provider:              "predb.club",
			OffsetHint:            8,
			OldestPostedAt:        &anchorTime,
			OldestNormalizedTitle: "anchor.release",
		},
	}
	t1 := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	t4 := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	svc := &Service{
		repo: repo,
		log:  fakeLogger{},
		opts: DefaultOptions(Options{
			BackfillPageSize: 2,
			MaxBackfillPages: 10,
		}),
		backfill: fakeBackfillProvider{pages: [][]pgindex.PredbEntryRecord{
			{{Title: "Recent.One", PostedAt: &t1, Source: "predb.club"}, {Title: "Recent.Two", PostedAt: &t1, Source: "predb.club"}},
			{{Title: "Mid.One", PostedAt: &t2, Source: "predb.club"}, {Title: "Mid.Two", PostedAt: &t2, Source: "predb.club"}},
			{{Title: "Anchor.Release", PostedAt: &t3, Source: "predb.club"}, {Title: "Older.One", PostedAt: &t4, Source: "predb.club"}},
		}},
	}

	if err := svc.RunSyncBackfillOnce(context.Background()); err != nil {
		t.Fatalf("RunSyncBackfillOnce failed: %v", err)
	}
	if got := len(repo.upserts); got != 2 {
		t.Fatalf("expected checkpoint resume to begin at anchor page, got %d rows", got)
	}
	if repo.upserts[0].Title != "Anchor.Release" {
		t.Fatalf("expected checkpoint anchor page to be first upserted page, got %+v", repo.upserts)
	}
}
