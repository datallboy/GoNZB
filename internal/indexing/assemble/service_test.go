package assemble

import (
	"context"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/match"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestRunOncePassesRichMatchCandidateAndPersistsMatchFields(t *testing.T) {
	postedAt := time.Date(2026, 4, 2, 18, 0, 0, 0, time.UTC)
	repo := &fakeRepository{
		headers: []pgindex.AssemblyCandidate{
			{
				ID:            11,
				ProviderID:    1,
				NewsgroupID:   2,
				ArticleNumber: 300123,
				MessageID:     "<test@match.example>",
				Subject:       `Show.S01E01 "show.s01e01.r00" yEnc (1/15)`,
				Poster:        `Poster <poster@example.com>`,
				DateUTC:       &postedAt,
				Bytes:         4096,
				Lines:         100,
				Xref:          `news.example alt.binaries.tv:300123`,
				RawOverview: map[string]any{
					"size": 4096,
				},
			},
		},
	}
	matcher := &fakeMatcher{
		result: match.Result{
			ReleaseName:     "Show.S01E01",
			ReleaseKey:      "show s01e01",
			BinaryName:      "show.s01e01.r00",
			BinaryKey:       "show s01e01::show s01e01 r00",
			FileName:        "show.s01e01.r00",
			PartNumber:      1,
			TotalParts:      15,
			MatchConfidence: 0.91,
			MatchStatus:     "matched",
			GroupingEvidence: map[string]any{
				"summary": map[string]any{"confidence": 0.91},
			},
		},
	}

	svc := NewService(repo, matcher, testLogger{}, Options{BatchSize: 10})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if matcher.lastCandidate.ArticleNumber != 300123 {
		t.Fatalf("expected matcher to receive article number, got %d", matcher.lastCandidate.ArticleNumber)
	}
	if matcher.lastCandidate.Poster != `Poster <poster@example.com>` {
		t.Fatalf("expected matcher to receive poster, got %q", matcher.lastCandidate.Poster)
	}
	if matcher.lastCandidate.RawOverview["size"] != 4096 {
		t.Fatalf("expected matcher to receive raw overview, got %#v", matcher.lastCandidate.RawOverview)
	}
	if len(repo.upsertedBinaries) != 1 {
		t.Fatalf("expected one upserted binary, got %d", len(repo.upsertedBinaries))
	}

	got := repo.upsertedBinaries[0]
	if got.MatchStatus != "matched" {
		t.Fatalf("expected persisted match status, got %q", got.MatchStatus)
	}
	if got.MatchConfidence != 0.91 {
		t.Fatalf("expected persisted match confidence 0.91, got %f", got.MatchConfidence)
	}
	if _, ok := got.GroupingEvidence["summary"]; !ok {
		t.Fatalf("expected persisted grouping evidence, got %#v", got.GroupingEvidence)
	}
}

type fakeRepository struct {
	headers          []pgindex.AssemblyCandidate
	upsertedBinaries []pgindex.BinaryRecord
}

func (f *fakeRepository) ListUnassembledArticleHeaders(context.Context, int) ([]pgindex.AssemblyCandidate, error) {
	return f.headers, nil
}

func (f *fakeRepository) EnsurePoster(context.Context, string) (int64, error) {
	return 44, nil
}

func (f *fakeRepository) LinkArticlePoster(context.Context, int64, int64) error {
	return nil
}

func (f *fakeRepository) UpsertBinary(_ context.Context, in pgindex.BinaryRecord) (int64, error) {
	f.upsertedBinaries = append(f.upsertedBinaries, in)
	return 77, nil
}

func (f *fakeRepository) UpsertBinaryPart(context.Context, pgindex.BinaryPartRecord) error {
	return nil
}

func (f *fakeRepository) RefreshBinaryStats(context.Context, int64) error {
	return nil
}

type fakeMatcher struct {
	lastCandidate match.Candidate
	result        match.Result
}

func (f *fakeMatcher) Match(candidate match.Candidate) match.Result {
	f.lastCandidate = candidate
	return f.result
}

type testLogger struct{}

func (testLogger) Debug(string, ...interface{}) {}
func (testLogger) Info(string, ...interface{})  {}
func (testLogger) Warn(string, ...interface{})  {}
func (testLogger) Error(string, ...interface{}) {}
