package assemble

import (
	"context"
	"fmt"
	"io"
	"strings"
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

	svc := NewService(repo, matcher, nil, testLogger{}, Options{BatchSize: 10})
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

func TestRunOnceRecoversObfuscatedMultipartIdentityFromYEncHeader(t *testing.T) {
	postedAt := time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC)
	repo := &fakeRepository{
		headers: []pgindex.AssemblyCandidate{
			{
				ID:            21,
				ProviderID:    1,
				NewsgroupID:   2,
				NewsgroupName: "alt.binaries.test",
				ArticleNumber: 1001,
				MessageID:     "<opaque-a@test.example>",
				Subject:       "ZxZzCeWW8ECJExG13i891fyVBUCommbINJNQNqdTam9KYctnYSWQI7Q1JXWeOPwA",
				Poster:        `Poster <poster@example.com>`,
				DateUTC:       &postedAt,
				Bytes:         740189,
				Lines:         100,
				Xref:          `alt.binaries.test:1001`,
			},
			{
				ID:            22,
				ProviderID:    1,
				NewsgroupID:   2,
				NewsgroupName: "alt.binaries.test",
				ArticleNumber: 1002,
				MessageID:     "<opaque-b@test.example>",
				Subject:       "2SyfBuDdgET6VKdhFnWPdQcQfHIzOdmE2qRrhv43KiKF1YJWfRVJMCFVJcsYV9ue",
				Poster:        `Poster <poster@example.com>`,
				DateUTC:       &postedAt,
				Bytes:         740369,
				Lines:         100,
				Xref:          `alt.binaries.test:1002`,
			},
		},
	}
	matcher := &fakeMatcher{dynamic: true}
	fetcher := fakeArticleFetcher{
		payloads: map[string]string{
			"<opaque-a@test.example>": "=ybegin part=156 total=807 line=128 size=577954475 name=kuqn1sj0tdehymt5l4ba7u\r\n=ypart begin=111104001 end=111820800\r\n",
			"<opaque-b@test.example>": "=ybegin part=157 total=807 line=128 size=577954475 name=kuqn1sj0tdehymt5l4ba7u\r\n=ypart begin=111820801 end=112537600\r\n",
		},
	}

	svc := NewService(repo, matcher, fetcher, testLogger{}, Options{BatchSize: 10})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedBinaries) != 2 {
		t.Fatalf("expected two upserted binaries, got %d", len(repo.upsertedBinaries))
	}
	first := repo.upsertedBinaries[0]
	second := repo.upsertedBinaries[1]
	if first.BinaryKey != second.BinaryKey {
		t.Fatalf("expected same binary key after yEnc recovery, got %q vs %q", first.BinaryKey, second.BinaryKey)
	}
	if first.FileName != "kuqn1sj0tdehymt5l4ba7u" || second.FileName != "kuqn1sj0tdehymt5l4ba7u" {
		t.Fatalf("expected yEnc name-backed file name, got %q / %q", first.FileName, second.FileName)
	}
	if first.TotalParts != 807 || second.TotalParts != 807 {
		t.Fatalf("expected total parts 807, got %d / %d", first.TotalParts, second.TotalParts)
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
	dynamic       bool
}

func (f *fakeMatcher) Match(candidate match.Candidate) match.Result {
	f.lastCandidate = candidate
	if f.dynamic {
		name, _ := candidate.RawOverview["name"].(string)
		part := intValue(candidate.RawOverview["part"])
		total := intValue(candidate.RawOverview["total"])
		if name != "" {
			return match.Result{
				SourceReleaseKey: "poster example com-host-release-1000",
				ReleaseFamilyKey: "poster example com-host-release-1000",
				ReleaseKey:       "poster example com-host-release-1000",
				BinaryName:       name,
				BinaryKey:        "poster example com-host-release-1000::" + name,
				FileName:         name,
				PartNumber:       part,
				TotalParts:       total,
				MatchConfidence:  0.64,
				MatchStatus:      "probable",
			}
		}
		return match.Result{
			SourceReleaseKey: "poster example com-host-release-1000",
			ReleaseFamilyKey: "poster example com-host-release-1000",
			ReleaseKey:       "poster example com-host-release-1000",
			BinaryName:       candidate.Subject + ".bin",
			BinaryKey:        "poster example com-host-release-1000::" + candidate.Subject,
			FileName:         candidate.Subject + ".bin",
			PartNumber:       1,
			TotalParts:       1,
			MatchConfidence:  0.20,
			MatchStatus:      "low_confidence",
		}
	}
	return f.result
}

type fakeArticleFetcher struct {
	payloads map[string]string
}

func (f fakeArticleFetcher) Fetch(_ context.Context, msgID string, _ []string) (io.Reader, error) {
	value, ok := f.payloads[msgID]
	if !ok {
		return nil, fmt.Errorf("missing payload for %s", msgID)
	}
	return strings.NewReader(value), nil
}

func intValue(v any) int {
	switch tv := v.(type) {
	case int:
		return tv
	case int64:
		return int(tv)
	case float64:
		return int(tv)
	default:
		return 0
	}
}

type testLogger struct{}

func (testLogger) Debug(string, ...interface{}) {}
func (testLogger) Info(string, ...interface{})  {}
func (testLogger) Warn(string, ...interface{})  {}
func (testLogger) Error(string, ...interface{}) {}
