package tmdb

import (
	"context"
	"testing"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type fakeRepo struct {
	candidates []pgindex.ReleaseEnrichmentCandidate
	tmdbRows   map[string][]pgindex.ReleaseTMDBMatchRecord
	tvdbRows   map[string][]pgindex.ReleaseTVDBMatchRecord
	updates    []pgindex.ReleaseEnrichmentUpdate
}

func (f *fakeRepo) ListReleaseEnrichmentCandidates(context.Context, string, int) ([]pgindex.ReleaseEnrichmentCandidate, error) {
	return append([]pgindex.ReleaseEnrichmentCandidate(nil), f.candidates...), nil
}

func (f *fakeRepo) ReplaceReleaseTMDBMatches(_ context.Context, releaseID string, rows []pgindex.ReleaseTMDBMatchRecord) error {
	if f.tmdbRows == nil {
		f.tmdbRows = map[string][]pgindex.ReleaseTMDBMatchRecord{}
	}
	f.tmdbRows[releaseID] = append([]pgindex.ReleaseTMDBMatchRecord(nil), rows...)
	return nil
}

func (f *fakeRepo) ReplaceReleaseTVDBMatches(_ context.Context, releaseID string, rows []pgindex.ReleaseTVDBMatchRecord) error {
	if f.tvdbRows == nil {
		f.tvdbRows = map[string][]pgindex.ReleaseTVDBMatchRecord{}
	}
	f.tvdbRows[releaseID] = append([]pgindex.ReleaseTVDBMatchRecord(nil), rows...)
	return nil
}

func (f *fakeRepo) ApplyReleaseEnrichmentUpdate(_ context.Context, in pgindex.ReleaseEnrichmentUpdate) error {
	f.updates = append(f.updates, in)
	return nil
}

type fakeTMDBClient struct {
	movie []externalMatch
	tv    []externalMatch
}

func (f fakeTMDBClient) SearchMovie(context.Context, string, int) ([]externalMatch, error) {
	return append([]externalMatch(nil), f.movie...), nil
}

func (f fakeTMDBClient) SearchTV(context.Context, string, int) ([]externalMatch, error) {
	return append([]externalMatch(nil), f.tv...), nil
}

type fakeTVDBClient struct {
	series []externalMatch
}

func (f fakeTVDBClient) SearchSeries(context.Context, string, int) ([]externalMatch, error) {
	return append([]externalMatch(nil), f.series...), nil
}

type fakeLogger struct{}

func (fakeLogger) Debug(string, ...interface{}) {}
func (fakeLogger) Info(string, ...interface{})  {}
func (fakeLogger) Warn(string, ...interface{})  {}
func (fakeLogger) Error(string, ...interface{}) {}

func TestDeriveReleaseQueryTV(t *testing.T) {
	query, ok := deriveReleaseQuery(pgindex.ReleaseEnrichmentCandidate{
		DeobfuscatedTitle: "Example.Show.S08E16.1080p.x265-GROUP",
	})
	if !ok {
		t.Fatal("expected release query")
	}
	if !query.IsTV || query.BaseTitle != "Example Show" || query.Season != 8 || query.Episode != 16 {
		t.Fatalf("unexpected query: %+v", query)
	}
}

func TestDeriveReleaseQueryMovie(t *testing.T) {
	query, ok := deriveReleaseQuery(pgindex.ReleaseEnrichmentCandidate{
		DeobfuscatedTitle: "Example.Feature.1963.1080p.BluRay.x265-GROUP",
	})
	if !ok {
		t.Fatal("expected release query")
	}
	if query.IsTV || query.BaseTitle != "Example Feature" || query.Year != 1963 {
		t.Fatalf("unexpected query: %+v", query)
	}
}

func TestRunOncePrefersTVDBForTV(t *testing.T) {
	repo := &fakeRepo{
		candidates: []pgindex.ReleaseEnrichmentCandidate{{
			ReleaseID:         "rel-tv",
			DeobfuscatedTitle: "Example.Show.S08E16.1080p.x265-GROUP",
			Classification:    "video_archive",
		}},
	}
	svc := &Service{
		repo: repo,
		log:  fakeLogger{},
		opts: DefaultOptions(Options{}),
		tvdb: fakeTVDBClient{series: []externalMatch{{
			Source:        "tvdb",
			ExternalID:    1234,
			MediaType:     "tv",
			Title:         "Example Show",
			OriginalTitle: "Example Show",
			Year:          2018,
		}}},
		tmdb: fakeTMDBClient{tv: []externalMatch{{
			Source:        "tmdb",
			ExternalID:    99,
			MediaType:     "tv",
			Title:         "Example Show",
			OriginalTitle: "Example Show",
			Year:          2018,
		}}},
	}

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("expected one update, got %d", len(repo.updates))
	}
	update := repo.updates[0]
	if update.TVDBID != 1234 {
		t.Fatalf("expected tvdb id 1234, got %+v", repo.updates[0])
	}
	if update.SeasonNumber != 8 || update.EpisodeNumber != 16 {
		t.Fatalf("expected season/episode 8/16, got %+v", update)
	}
	if update.SeasonEpisodeSource != "composite" {
		t.Fatalf("expected composite season/episode source, got %+v", update)
	}
	if update.SeasonEpisodeConfidence <= 0 {
		t.Fatalf("expected season/episode confidence, got %+v", update)
	}
}

func TestRunOnceUsesTMDBForMovie(t *testing.T) {
	repo := &fakeRepo{
		candidates: []pgindex.ReleaseEnrichmentCandidate{{
			ReleaseID:         "rel-movie",
			DeobfuscatedTitle: "Example.Feature.1963.1080p.BluRay.x265-GROUP",
			Classification:    "video_archive",
		}},
	}
	svc := &Service{
		repo: repo,
		log:  fakeLogger{},
		opts: DefaultOptions(Options{}),
		tmdb: fakeTMDBClient{movie: []externalMatch{{
			Source:        "tmdb",
			ExternalID:    657,
			MediaType:     "movie",
			Title:         "Example Feature",
			OriginalTitle: "Example Feature",
			Year:          1963,
		}}},
	}

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("expected one update, got %d", len(repo.updates))
	}
	if repo.updates[0].TMDBID != 657 || repo.updates[0].MatchedMediaTitle != "Example Feature" {
		t.Fatalf("unexpected update: %+v", repo.updates[0])
	}
}
