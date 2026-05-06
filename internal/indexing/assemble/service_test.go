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

	if len(repo.upsertedBinaries) != 1 {
		t.Fatalf("expected one batch-local binary upsert, got %d", len(repo.upsertedBinaries))
	}
	first := repo.upsertedBinaries[0]
	if first.FileName != "kuqn1sj0tdehymt5l4ba7u" {
		t.Fatalf("expected yEnc name-backed file name, got %q", first.FileName)
	}
	if first.TotalParts != 807 {
		t.Fatalf("expected total parts 807, got %d", first.TotalParts)
	}
	if len(repo.upsertedParts) != 2 {
		t.Fatalf("expected two batch-upserted parts, got %d", len(repo.upsertedParts))
	}
	if repo.upsertedParts[0].BinaryID != repo.upsertedParts[1].BinaryID {
		t.Fatalf("expected parts to reuse the cached binary id, got %d / %d", repo.upsertedParts[0].BinaryID, repo.upsertedParts[1].BinaryID)
	}
}

func TestRunOnceSkipsYEncRecoveryWhenStructuredIdentityAlreadyMatchesExistingBinary(t *testing.T) {
	postedAt := time.Date(2026, 4, 21, 16, 0, 0, 0, time.UTC)
	repo := &fakeRepository{
		headers: []pgindex.AssemblyCandidate{
			{
				ID:                              31,
				ProviderID:                      1,
				NewsgroupID:                     2,
				NewsgroupName:                   "alt.binaries.test",
				ArticleNumber:                   2001,
				MessageID:                       "<stable@test.example>",
				Subject:                         `Stable Release [1/2] - "stable.release.r00" yEnc (7/20)`,
				Poster:                          `Poster <poster@example.com>`,
				DateUTC:                         &postedAt,
				Bytes:                           8192,
				Lines:                           100,
				Xref:                            `alt.binaries.test:2001`,
				FileName:                        "stable.release.r00",
				FileTotal:                       2,
				YEncPart:                        7,
				YEncTotal:                       20,
				StructuredIdentityBinaryMatched: true,
				RawOverview: map[string]any{
					"name":  "stable.release.r00",
					"part":  7,
					"total": 20,
				},
			},
		},
	}
	matcher := &fakeMatcher{
		result: match.Result{
			SourceReleaseKey: "stable-release",
			ReleaseFamilyKey: "stable-release",
			ReleaseKey:       "stable-release",
			BinaryName:       "stable.release.r00",
			BinaryKey:        "stable-release::stable.release.r00",
			FileName:         "stable.release.r00",
			PartNumber:       7,
			TotalParts:       20,
			MatchConfidence:  0.92,
			MatchStatus:      "matched",
		},
	}
	fetcher := &countingArticleFetcher{
		payloads: map[string]string{
			"<stable@test.example>": "=ybegin part=7 total=20 line=128 size=1234 name=stable.release.r00\r\n=ypart begin=1 end=2\r\n",
		},
	}

	svc := NewService(repo, matcher, fetcher, testLogger{}, Options{BatchSize: 10})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if fetcher.calls != 0 {
		t.Fatalf("expected yEnc recovery fetch to be skipped, got %d calls", fetcher.calls)
	}
}

func TestRunOnceSkipsYEncRecoveryWhenSubjectAlreadyExposesFileName(t *testing.T) {
	postedAt := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{
		headers: []pgindex.AssemblyCandidate{
			{
				ID:            41,
				ProviderID:    1,
				NewsgroupID:   2,
				NewsgroupName: "alt.binaries.test",
				ArticleNumber: 3001,
				MessageID:     "<subject-name@test.example>",
				Subject:       `Readable Subject - "readable.release.bin" yEnc`,
				Poster:        `Poster <poster@example.com>`,
				DateUTC:       &postedAt,
				Bytes:         2048,
				Lines:         100,
				Xref:          `alt.binaries.test:3001`,
				FileName:      "readable.release.bin",
			},
		},
	}
	matcher := &fakeMatcher{
		result: match.Result{
			SourceReleaseKey: "readable-release",
			ReleaseFamilyKey: "readable-release",
			ReleaseKey:       "readable-release",
			BinaryName:       "readable.release.bin",
			BinaryKey:        "readable-release::readable.release.bin",
			FileName:         "readable.release.bin",
			PartNumber:       1,
			TotalParts:       1,
			MatchConfidence:  0.20,
			MatchStatus:      "low_confidence",
		},
	}
	fetcher := &countingArticleFetcher{
		payloads: map[string]string{
			"<subject-name@test.example>": "=ybegin part=1 total=1 line=128 size=2048 name=readable.release.bin\r\n",
		},
	}

	svc := NewService(repo, matcher, fetcher, testLogger{}, Options{BatchSize: 10})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if fetcher.calls != 0 {
		t.Fatalf("expected yEnc recovery to stay off the hot path when subject already exposes file name, got %d calls", fetcher.calls)
	}
}

func TestRunOnceCapsYEncRecoveryAttemptsPerBatch(t *testing.T) {
	postedAt := time.Date(2026, 5, 6, 12, 30, 0, 0, time.UTC)
	repo := &fakeRepository{
		headers: []pgindex.AssemblyCandidate{
			{
				ID:            51,
				ProviderID:    1,
				NewsgroupID:   2,
				NewsgroupName: "alt.binaries.test",
				ArticleNumber: 4001,
				MessageID:     "<opaque-cap-a@test.example>",
				Subject:       "ABCDEF1234567890opaquecapaaaa",
				Poster:        `Poster <poster@example.com>`,
				DateUTC:       &postedAt,
				Bytes:         1024,
				Lines:         100,
				Xref:          `alt.binaries.test:4001`,
			},
			{
				ID:            52,
				ProviderID:    1,
				NewsgroupID:   2,
				NewsgroupName: "alt.binaries.test",
				ArticleNumber: 4002,
				MessageID:     "<opaque-cap-b@test.example>",
				Subject:       "ABCDEF1234567890opaquecapbbbb",
				Poster:        `Poster <poster@example.com>`,
				DateUTC:       &postedAt,
				Bytes:         1024,
				Lines:         100,
				Xref:          `alt.binaries.test:4002`,
			},
			{
				ID:            53,
				ProviderID:    1,
				NewsgroupID:   2,
				NewsgroupName: "alt.binaries.test",
				ArticleNumber: 4003,
				MessageID:     "<opaque-cap-c@test.example>",
				Subject:       "ABCDEF1234567890opaquecapcccc",
				Poster:        `Poster <poster@example.com>`,
				DateUTC:       &postedAt,
				Bytes:         1024,
				Lines:         100,
				Xref:          `alt.binaries.test:4003`,
			},
		},
	}
	matcher := &fakeMatcher{dynamic: true}
	fetcher := &countingArticleFetcher{
		payloads: map[string]string{
			"<opaque-cap-a@test.example>": "=ybegin part=1 total=10 line=128 size=1234 name=opaque.cap.file\r\n=ypart begin=1 end=2\r\n",
			"<opaque-cap-b@test.example>": "=ybegin part=2 total=10 line=128 size=1234 name=opaque.cap.file\r\n=ypart begin=3 end=4\r\n",
			"<opaque-cap-c@test.example>": "=ybegin part=3 total=10 line=128 size=1234 name=opaque.cap.file\r\n=ypart begin=5 end=6\r\n",
		},
	}

	svc := NewService(repo, matcher, fetcher, testLogger{}, Options{BatchSize: 10, MaxYEncRecoveryAttempts: 2})
	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("run once with metrics: %v", err)
	}

	if fetcher.calls != 2 {
		t.Fatalf("expected yEnc recovery attempts to be capped at 2, got %d", fetcher.calls)
	}
	if got := intValue(metrics["recovery_attempts"]); got != 2 {
		t.Fatalf("expected 2 recorded recovery attempts, got %d", got)
	}
	if got := intValue(metrics["recovery_skipped_by_cap"]); got != 1 {
		t.Fatalf("expected 1 skipped recovery due to cap, got %d", got)
	}
}

type fakeRepository struct {
	headers          []pgindex.AssemblyCandidate
	upsertedBinaries []pgindex.BinaryRecord
	upsertedParts    []pgindex.BinaryPartRecord
}

func (f *fakeRepository) ListUnassembledArticleHeaders(context.Context, int) ([]pgindex.AssemblyCandidate, error) {
	return f.headers, nil
}

func (f *fakeRepository) ClaimUnassembledArticleHeaders(_ context.Context, _ pgindex.AssemblyClaimRequest) ([]pgindex.AssemblyCandidate, error) {
	return f.headers, nil
}

func (f *fakeRepository) EnsurePoster(context.Context, string) (int64, error) {
	return 44, nil
}

func (f *fakeRepository) UpsertBinary(_ context.Context, in pgindex.BinaryRecord) (int64, error) {
	f.upsertedBinaries = append(f.upsertedBinaries, in)
	return 77, nil
}

func (f *fakeRepository) UpsertBinaryParts(_ context.Context, records []pgindex.BinaryPartRecord) error {
	f.upsertedParts = append(f.upsertedParts, records...)
	return nil
}

func (f *fakeRepository) RefreshBinaryStats(context.Context, int64) error {
	return nil
}

func (f *fakeRepository) RefreshBinaryStatsBatch(context.Context, []int64) error {
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

type countingArticleFetcher struct {
	payloads map[string]string
	calls    int
}

func (f *countingArticleFetcher) Fetch(_ context.Context, msgID string, _ []string) (io.Reader, error) {
	f.calls++
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
