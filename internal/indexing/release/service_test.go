package release

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestRunOnceSplitsSourceReleaseKeyIntoMultipleGroups(t *testing.T) {
	baseTime := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{
			{
				ProviderID:  1,
				NewsgroupID: 2,
				ReleaseKey:  "shared source key",
			},
		},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"shared source key": {
				{
					BinaryID:        1,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "shared source key",
					ReleaseName:     "Movie One 2026",
					FileName:        "movie.one.2026.r00",
					Poster:          "poster-a",
					PostedAt:        ptrTime(baseTime),
					TotalParts:      10,
					ObservedParts:   10,
					TotalBytes:      700_000_000,
					MatchConfidence: 0.92,
				},
				{
					BinaryID:        2,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "shared source key",
					ReleaseName:     "Movie One 2026",
					FileName:        "movie.one.2026.r01",
					Poster:          "poster-a",
					PostedAt:        ptrTime(baseTime.Add(45 * time.Minute)),
					TotalParts:      10,
					ObservedParts:   9,
					TotalBytes:      710_000_000,
					MatchConfidence: 0.88,
				},
				{
					BinaryID:        3,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "shared source key",
					ReleaseName:     "Other Show S01E01",
					FileName:        "other.show.s01e01.r00",
					Poster:          "poster-b",
					PostedAt:        ptrTime(baseTime.Add(30 * time.Hour)),
					TotalParts:      8,
					ObservedParts:   8,
					TotalBytes:      550_000_000,
					MatchConfidence: 0.90,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
			2: {{ArticleHeaderID: 102, PartNumber: 2}},
			3: {{ArticleHeaderID: 103, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 2 {
		t.Fatalf("expected 2 release groups, got %d", len(repo.upsertedReleases))
	}
	if len(repo.deletedStaleCalls) != 1 {
		t.Fatalf("expected one stale-delete call, got %d", len(repo.deletedStaleCalls))
	}
	if len(repo.deletedStaleCalls[0].keepGroupNames) != 2 {
		t.Fatalf("expected 2 kept group names, got %v", repo.deletedStaleCalls[0].keepGroupNames)
	}
}

func TestRunOnceSkipsLowConfidenceReleaseBelowThreshold(t *testing.T) {
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{
			{
				ProviderID:  1,
				NewsgroupID: 2,
				ReleaseKey:  "weak source key",
			},
		},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"weak source key": {
				{
					BinaryID:        1,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "weak source key",
					FileName:        "abcd1234.txt",
					Poster:          "poster-a",
					TotalParts:      1,
					ObservedParts:   1,
					TotalBytes:      2048,
					MatchConfidence: 0.10,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.60})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 0 {
		t.Fatalf("expected no releases to be formed, got %d", len(repo.upsertedReleases))
	}
	if len(repo.deletedStaleCalls) != 1 {
		t.Fatalf("expected one stale-delete call, got %d", len(repo.deletedStaleCalls))
	}
	if len(repo.deletedStaleCalls[0].keepGroupNames) != 0 {
		t.Fatalf("expected no kept group names, got %v", repo.deletedStaleCalls[0].keepGroupNames)
	}
}

func TestRunOnceBuildsReleaseSummaryState(t *testing.T) {
	baseTime := time.Date(2026, 4, 2, 18, 0, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{
			{
				ProviderID:  1,
				NewsgroupID: 2,
				ReleaseKey:  "show source key",
				ReleaseName: "Show.S01E01.1080p.WEB-DL.x265",
			},
		},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"show source key": {
				{
					BinaryID:        1,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "show source key",
					ReleaseName:     "Show.S01E01.1080p.WEB-DL.x265",
					FileName:        "show.s01e01.1080p.web-dl.x265.mkv",
					Poster:          "poster-a",
					PostedAt:        ptrTime(baseTime),
					TotalParts:      20,
					ObservedParts:   18,
					TotalBytes:      1_500_000_000,
					MatchConfidence: 0.95,
				},
				{
					BinaryID:        2,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "show source key",
					ReleaseName:     "Show.S01E01.1080p.WEB-DL.x265",
					FileName:        "show.s01e01.1080p.web-dl.x265.nfo",
					Poster:          "poster-a",
					PostedAt:        ptrTime(baseTime.Add(10 * time.Minute)),
					TotalParts:      1,
					ObservedParts:   1,
					TotalBytes:      1024,
					MatchConfidence: 0.90,
				},
				{
					BinaryID:        3,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "show source key",
					ReleaseName:     "Show.S01E01.1080p.WEB-DL.x265",
					FileName:        "show.s01e01.1080p.web-dl.x265.par2",
					Poster:          "poster-a",
					PostedAt:        ptrTime(baseTime.Add(20 * time.Minute)),
					TotalParts:      1,
					ObservedParts:   1,
					TotalBytes:      2048,
					MatchConfidence: 0.90,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
			2: {{ArticleHeaderID: 102, PartNumber: 1}},
			3: {{ArticleHeaderID: 103, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 1 {
		t.Fatalf("expected one release, got %d", len(repo.upsertedReleases))
	}

	got := repo.upsertedReleases[0]
	if !got.HasPAR2 || !got.HasNFO {
		t.Fatalf("expected has_par2 and has_nfo to be true, got has_par2=%v has_nfo=%v", got.HasPAR2, got.HasNFO)
	}
	if got.Passworded || got.PasswordedKnown || got.PasswordedUnknown {
		t.Fatalf("expected password flags to remain false, got %+v", got)
	}
	if got.PasswordState != "unknown" {
		t.Fatalf("expected password_state unknown, got %q", got.PasswordState)
	}
	if got.PrimaryResolution != "1080p" {
		t.Fatalf("expected primary resolution 1080p, got %q", got.PrimaryResolution)
	}
	if got.PrimaryVideoCodec != "x265" {
		t.Fatalf("expected primary video codec x265, got %q", got.PrimaryVideoCodec)
	}
	if got.AvailabilityScore == got.CompletionPct {
		t.Fatalf("expected availability score to differ from completion pct, got %.2f", got.AvailabilityScore)
	}
}

func TestRunOnceCompletionRespectsExpectedFileCount(t *testing.T) {
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{
			{
				ProviderID:  1,
				NewsgroupID: 2,
				ReleaseKey:  "expected file count source key",
			},
		},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"expected file count source key": {
				{
					BinaryID:          1,
					ProviderID:        1,
					NewsgroupID:       2,
					ReleaseKey:        "expected file count source key",
					FileName:          "example.7z.001",
					FileIndex:         1,
					ExpectedFileCount: 4,
					TotalParts:        10,
					ObservedParts:     10,
					TotalBytes:        700_000_000,
					MatchConfidence:   0.95,
				},
				{
					BinaryID:          2,
					ProviderID:        1,
					NewsgroupID:       2,
					ReleaseKey:        "expected file count source key",
					FileName:          "example.par2",
					FileIndex:         2,
					ExpectedFileCount: 4,
					TotalParts:        1,
					ObservedParts:     1,
					TotalBytes:        1024,
					MatchConfidence:   0.90,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
			2: {{ArticleHeaderID: 102, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 1 {
		t.Fatalf("expected one release, got %d", len(repo.upsertedReleases))
	}
	if repo.upsertedReleases[0].ExpectedFileCount != 4 {
		t.Fatalf("expected expected_file_count 4, got %d", repo.upsertedReleases[0].ExpectedFileCount)
	}
	if repo.upsertedReleases[0].CompletionPct != 50 {
		t.Fatalf("expected completion_pct 50 due to file-count gate, got %.2f", repo.upsertedReleases[0].CompletionPct)
	}
}

func TestRunOnceSkipsReleaseBelowCompletionThreshold(t *testing.T) {
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{
			{
				ProviderID:  1,
				NewsgroupID: 2,
				ReleaseKey:  "sparse source key",
			},
		},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"sparse source key": {
				{
					BinaryID:        1,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "sparse source key",
					FileName:        "sparse.release.7z.001",
					Poster:          "poster-a",
					TotalParts:      10,
					ObservedParts:   2,
					TotalBytes:      700_000_000,
					MatchConfidence: 0.90,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{
		BatchSize:            10,
		ReleaseMinConfidence: 0.55,
		ReleaseMinCompletion: 25,
	})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 0 {
		t.Fatalf("expected no releases to be formed, got %d", len(repo.upsertedReleases))
	}
}

func TestSummarizeFilesCountsSplitArchiveFamilyOnce(t *testing.T) {
	hasPAR2, _, archiveCount, _, _, _ := summarizeFiles([]pgindex.BinarySummary{
		{FileName: "example.7z.001"},
		{FileName: "example.7z.002"},
		{FileName: "example.par2"},
	})

	if !hasPAR2 {
		t.Fatalf("expected PAR2 presence to be detected")
	}
	if archiveCount != 1 {
		t.Fatalf("expected one archive family, got %d", archiveCount)
	}
}

type fakeReleaseRepository struct {
	candidates         []pgindex.ReleaseCandidate
	binariesByKey      map[string][]pgindex.BinarySummary
	articlesByBinaryID map[int64][]pgindex.ReleaseFileArticleRecord
	upsertedReleases   []pgindex.ReleaseRecord
	replaceFileCalls   int
	newsgroupCalls     int
	nzbCalls           int
	deletedStaleCalls  []staleDeleteCall
}

type staleDeleteCall struct {
	providerID     int64
	releaseKey     string
	keepGroupNames []string
}

func (f *fakeReleaseRepository) ListReleaseCandidates(context.Context, int) ([]pgindex.ReleaseCandidate, error) {
	return f.candidates, nil
}

func (f *fakeReleaseRepository) ListBinariesForReleaseCandidate(_ context.Context, _ int64, _ int64, releaseKey string) ([]pgindex.BinarySummary, error) {
	return f.binariesByKey[releaseKey], nil
}

func (f *fakeReleaseRepository) ListBinaryPartArticles(_ context.Context, binaryID int64) ([]pgindex.ReleaseFileArticleRecord, error) {
	return f.articlesByBinaryID[binaryID], nil
}

func (f *fakeReleaseRepository) UpsertRelease(_ context.Context, in pgindex.ReleaseRecord) (string, error) {
	f.upsertedReleases = append(f.upsertedReleases, in)
	return fmt.Sprintf("rel-%d", len(f.upsertedReleases)), nil
}

func (f *fakeReleaseRepository) DeleteStaleReleasesForSourceKey(_ context.Context, providerID int64, releaseKey string, keepGroupNames []string) error {
	f.deletedStaleCalls = append(f.deletedStaleCalls, staleDeleteCall{
		providerID:     providerID,
		releaseKey:     releaseKey,
		keepGroupNames: append([]string(nil), keepGroupNames...),
	})
	return nil
}

func (f *fakeReleaseRepository) ReplaceReleaseFiles(context.Context, string, []pgindex.ReleaseFileRecord) error {
	f.replaceFileCalls++
	return nil
}

func (f *fakeReleaseRepository) ReplaceReleaseNewsgroups(context.Context, string, []int64) error {
	f.newsgroupCalls++
	return nil
}

func (f *fakeReleaseRepository) UpsertNZBCache(context.Context, string, string, string, string) error {
	f.nzbCalls++
	return nil
}

type testReleaseLogger struct{}

func (testReleaseLogger) Debug(string, ...interface{}) {}
func (testReleaseLogger) Info(string, ...interface{})  {}
func (testReleaseLogger) Warn(string, ...interface{})  {}
func (testReleaseLogger) Error(string, ...interface{}) {}

func ptrTime(v time.Time) *time.Time {
	return &v
}
