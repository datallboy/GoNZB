package yencrecover

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/match"
	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestRunOnceRecoversCandidateFromYEncHeaderPrefix(t *testing.T) {
	repo := &fakeRepo{
		candidates: []pgindex.YEncRecoveryCandidate{{
			BinaryID:        10,
			ArticleHeaderID: 20,
			NewsgroupName:   "alt.binaries.test",
			ArticleNumber:   1234,
			MessageID:       "abc@test",
			Subject:         `[01/60] opaque yEnc (1/1)`,
			Poster:          "poster",
			DateUTC:         ptrTime(time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)),
			Bytes:           1024,
			Lines:           10,
		}},
	}
	fetcher := &fakePrefixFetcher{body: []byte("=ybegin part=1 total=1 line=128 size=1024 name=Example.Release.vol001+002.par2\r\n=ypart begin=1 end=1024\r\n")}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{BatchSize: 10, MaxHeaderBytes: 256})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if metrics["recovered"] != 1 {
		t.Fatalf("expected one recovered candidate, got metrics=%v", metrics)
	}
	if metrics["effective_concurrency"] != 1 || metrics["batch_full"] != false {
		t.Fatalf("unexpected recovery saturation metrics: %v", metrics)
	}
	if _, ok := metrics["fetch_ms"].(float64); !ok {
		t.Fatalf("expected yEnc fetch timing metric, got %v", metrics)
	}
	if _, ok := metrics["parse_ms"].(float64); !ok {
		t.Fatalf("expected yEnc parse timing metric, got %v", metrics)
	}
	if _, ok := metrics["match_ms"].(float64); !ok {
		t.Fatalf("expected yEnc match timing metric, got %v", metrics)
	}
	if _, ok := metrics["write_ms"].(float64); !ok {
		t.Fatalf("expected yEnc step timing metrics, got %v", metrics)
	}
	if repo.applied.FileName != "Example.Release.vol001+002.par2" {
		t.Fatalf("expected recovered yEnc filename, got %q", repo.applied.FileName)
	}
	if repo.applied.BinaryID != 10 || repo.applied.ArticleHeaderID != 20 {
		t.Fatalf("expected binary/article ids to be preserved, got %+v", repo.applied)
	}
	if fetcher.maxBytes != 256 {
		t.Fatalf("expected prefix limit to be passed to fetcher, got %d", fetcher.maxBytes)
	}
}

func TestRunOncePersistsPlaceholderYEncFileNameAndSubjectFileCount(t *testing.T) {
	repo := &fakeRepo{
		candidates: []pgindex.YEncRecoveryCandidate{{
			BinaryID:        11,
			ArticleHeaderID: 21,
			NewsgroupName:   "alt.binaries.test",
			ArticleNumber:   4321,
			MessageID:       "placeholder@test",
			Subject:         `[03/12] opaque yEnc (1/1)`,
			Poster:          "poster",
			DateUTC:         ptrTime(time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)),
			Bytes:           2048,
			Lines:           20,
		}},
	}
	fetcher := &fakePrefixFetcher{body: []byte("=ybegin part=1 total=1 line=128 size=2048 name=payload.tmp\r\n=ypart begin=1 end=2048\r\n")}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{BatchSize: 10, MaxHeaderBytes: 256})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if metrics["recovered"] != 1 {
		t.Fatalf("expected one recovered candidate, got metrics=%v", metrics)
	}
	if repo.applied.FileName != "payload.tmp" {
		t.Fatalf("expected placeholder yEnc filename to persist, got %q", repo.applied.FileName)
	}
	if repo.applied.ExpectedFileCount != 12 || repo.applied.FileIndex != 3 {
		t.Fatalf("expected subject file counter 3/12 to be preserved, got %d/%d", repo.applied.FileIndex, repo.applied.ExpectedFileCount)
	}
}

func TestRunOnceBacksOffNotFoundArticle(t *testing.T) {
	repo := &fakeRepo{
		candidates: []pgindex.YEncRecoveryCandidate{{
			BinaryID:        10,
			ArticleHeaderID: 20,
			NewsgroupName:   "alt.binaries.test",
			MessageID:       "missing@test",
		}},
	}
	svc := NewService(repo, match.NewService(), &fakePrefixFetcher{err: nntp.ErrArticleNotFound}, nil, Options{BatchSize: 10})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if metrics["not_found"] != 1 {
		t.Fatalf("expected one not_found, got metrics=%v", metrics)
	}
	if repo.notFoundArticleID != 20 {
		t.Fatalf("expected backoff article 20, got %d", repo.notFoundArticleID)
	}
	if repo.applied.BinaryID != 0 {
		t.Fatalf("did not expect recovery apply, got %+v", repo.applied)
	}
}

func TestRunOnceBacksOffNoopArticle(t *testing.T) {
	repo := &fakeRepo{
		candidates: []pgindex.YEncRecoveryCandidate{{
			BinaryID:        10,
			ArticleHeaderID: 20,
			NewsgroupName:   "alt.binaries.test",
			MessageID:       "noop@test",
			Subject:         `"deadbeefdeadbeefdeadbeefdeadbeef.bin" yEnc (1/1)`,
		}},
	}
	fetcher := &fakePrefixFetcher{body: []byte("=ybegin part=1 total=1 line=128 size=1024 name=deadbeefdeadbeefdeadbeefdeadbeef.bin\r\n=ypart begin=1 end=1024\r\n")}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{BatchSize: 10, MaxHeaderBytes: 256})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if metrics["noops"] != 1 {
		t.Fatalf("expected one noop, got metrics=%v", metrics)
	}
	if repo.noopArticleID != 20 {
		t.Fatalf("expected noop backoff article 20, got %d", repo.noopArticleID)
	}
}

func TestRunOnceSkipsStaleBinaryRecoveryCandidate(t *testing.T) {
	repo := &fakeRepo{
		candidates: []pgindex.YEncRecoveryCandidate{{
			BinaryID:        10,
			ArticleHeaderID: 20,
			NewsgroupName:   "alt.binaries.test",
			MessageID:       "abc@test",
		}},
		applyErr: fmt.Errorf("%w: 10", pgindex.ErrBinaryNotFound),
	}
	fetcher := &fakePrefixFetcher{body: []byte("=ybegin part=1 total=1 line=128 size=1024 name=Example.Release.vol001+002.par2\r\n=ypart begin=1 end=1024\r\n")}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{BatchSize: 10, MaxHeaderBytes: 256})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if metrics["stale_candidates"] != 1 || metrics["attempted"] != 1 {
		t.Fatalf("expected stale candidate to be counted without failing, got metrics=%v", metrics)
	}
}

type fakeRepo struct {
	candidates        []pgindex.YEncRecoveryCandidate
	applied           pgindex.YEncHeaderRecoveryRecord
	notFoundArticleID int64
	noopArticleID     int64
	applyErr          error
}

func (f *fakeRepo) ListYEncRecoveryCandidates(context.Context, int) ([]pgindex.YEncRecoveryCandidate, error) {
	return append([]pgindex.YEncRecoveryCandidate(nil), f.candidates...), nil
}

func (f *fakeRepo) ApplyYEncHeaderRecovery(_ context.Context, in pgindex.YEncHeaderRecoveryRecord) (*pgindex.YEncHeaderRecoveryResult, error) {
	f.applied = in
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	return &pgindex.YEncHeaderRecoveryResult{BinaryID: in.BinaryID, TargetBinaryID: in.BinaryID}, nil
}

func (f *fakeRepo) RecordYEncRecoveryNotFound(_ context.Context, articleHeaderID int64) error {
	f.notFoundArticleID = articleHeaderID
	return nil
}

func (f *fakeRepo) RecordYEncRecoveryNoop(_ context.Context, articleHeaderID int64) error {
	f.noopArticleID = articleHeaderID
	return nil
}

type fakePrefixFetcher struct {
	body     []byte
	err      error
	maxBytes int64
}

func (f *fakePrefixFetcher) FetchBodyPrefix(_ context.Context, _ string, _ []string, maxBytes int64) ([]byte, error) {
	f.maxBytes = maxBytes
	if f.err != nil {
		return nil, f.err
	}
	if maxBytes > 0 && int64(len(f.body)) > maxBytes {
		return f.body[:int(maxBytes)], nil
	}
	return append([]byte(nil), f.body...), nil
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
