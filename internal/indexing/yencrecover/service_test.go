package yencrecover

import (
	"context"
	"fmt"
	"sync"
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
	if repo.applied.PartNumber != 1 || repo.applied.TotalParts != 1 || repo.applied.FileSize != 1024 {
		t.Fatalf("expected yEnc part metadata to be preserved, got part=%d total=%d size=%d", repo.applied.PartNumber, repo.applied.TotalParts, repo.applied.FileSize)
	}
	if fetcher.maxBytes != 256 {
		t.Fatalf("expected prefix limit to be passed to fetcher, got %d", fetcher.maxBytes)
	}
}

func TestRunOncePersistsRecoveredYEncMultipartHeaderMetadata(t *testing.T) {
	repo := &fakeRepo{
		candidates: []pgindex.YEncRecoveryCandidate{{
			BinaryID:        12,
			ArticleHeaderID: 22,
			NewsgroupName:   "alt.binaries.test",
			ArticleNumber:   5678,
			MessageID:       "multipart@test",
			Subject:         `2cf281e3e9fa4a1f82d5c320fa444010`,
			Poster:          "poster",
			DateUTC:         ptrTime(time.Date(2026, 6, 18, 14, 24, 11, 0, time.UTC)),
			Bytes:           716800,
			Lines:           5693,
		}},
	}
	fetcher := &fakePrefixFetcher{body: []byte("=ybegin part=12 total=732 line=128 size=524288000 name=5AzyRS4rfbOyP5fZH.part2.rar\r\n=ypart begin=7884801 end=8601600\r\n")}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{BatchSize: 10, MaxHeaderBytes: 256})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if metrics["recovered"] != 1 {
		t.Fatalf("expected one recovered candidate, got metrics=%v", metrics)
	}
	if repo.applied.FileName != "5AzyRS4rfbOyP5fZH.part2.rar" {
		t.Fatalf("expected recovered yEnc filename, got %q", repo.applied.FileName)
	}
	if repo.applied.PartNumber != 12 || repo.applied.TotalParts != 732 || repo.applied.FileSize != 524288000 {
		t.Fatalf("expected yEnc part metadata 12/732 size=524288000, got part=%d total=%d size=%d", repo.applied.PartNumber, repo.applied.TotalParts, repo.applied.FileSize)
	}
}

func TestRunOnceReportsRecoverySelectionLanes(t *testing.T) {
	bucketStart := time.Date(2026, 6, 18, 14, 20, 0, 0, time.UTC)
	bucketEnd := bucketStart.Add(5 * time.Minute)
	repo := &fakeRepo{
		candidates: []pgindex.YEncRecoveryCandidate{{
			BinaryID:            12,
			ArticleHeaderID:     22,
			NewsgroupName:       "alt.binaries.test",
			MessageID:           "fairness@test",
			Subject:             `2cf281e3e9fa4a1f82d5c320fa444010`,
			RecoveryLane:        "time_cohort_fairness",
			PriorityRank:        0,
			AdmissionReason:     "opaque_near_time_cohort",
			GroupTier:           "hot",
			FairnessBucketStart: &bucketStart,
			FairnessBucketEnd:   &bucketEnd,
		}, {
			BinaryID:        13,
			ArticleHeaderID: 23,
			NewsgroupName:   "alt.binaries.test",
			MessageID:       "newest@test",
			Subject:         `3cf281e3e9fa4a1f82d5c320fa444011`,
			RecoveryLane:    "newest",
			PriorityRank:    1,
			AdmissionReason: "bounded_admission",
			GroupTier:       "warm",
		}},
	}
	fetcher := &fakePrefixFetcher{body: []byte("=ybegin part=1 total=1 line=128 size=1024 name=Example.Release.rar\r\n=ypart begin=1 end=1024\r\n")}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{BatchSize: 10, MaxHeaderBytes: 256})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if metrics["selected_fairness"] != 1 || metrics["selected_newest"] != 1 {
		t.Fatalf("expected one candidate from each recovery lane, got metrics=%v", metrics)
	}
	if metrics["fairness_bucket_start"] != bucketStart.Format(time.RFC3339) || metrics["fairness_bucket_end"] != bucketEnd.Format(time.RFC3339) {
		t.Fatalf("expected fairness bucket metrics, got metrics=%v", metrics)
	}
	if metrics["selected_priority_0"] != 1 || metrics["selected_priority_1"] != 1 {
		t.Fatalf("expected selected priority metrics, got metrics=%v", metrics)
	}
	if metrics["selected_admission_opaque_near_time_cohort"] != 1 || metrics["selected_admission_bounded_admission"] != 1 {
		t.Fatalf("expected selected admission metrics, got metrics=%v", metrics)
	}
	if metrics["recovered_tier_hot"] != 1 || metrics["recovered_tier_warm"] != 1 {
		t.Fatalf("expected recovered tier metrics, got metrics=%v", metrics)
	}
}

func TestRunOnceReportsSelectionFillMetrics(t *testing.T) {
	repo := &fakeRepo{
		candidates: []pgindex.YEncRecoveryCandidate{{
			BinaryID:        12,
			ArticleHeaderID: 22,
			NewsgroupName:   "alt.binaries.test",
			MessageID:       "windowed@test",
			Subject:         `2cf281e3e9fa4a1f82d5c320fa444010`,
			RecoveryLane:    "time_cohort_fairness",
		}},
		selectionStats: pgindex.YEncRecoverySelectionStats{
			BatchRequested:    4,
			BatchSelected:     1,
			ReadyCount:        12,
			Priority0Ready:    3,
			PrioritySeedLimit: 2,
			PrioritySeeded:    1,
			WindowedRequested: 4,
			NewestRequested:   0,
			SelectedWindowed:  1,
			SelectedNewest:    0,
			BucketsScanned:    3,
			EmptyBuckets:      2,
		},
	}
	fetcher := &fakePrefixFetcher{body: []byte("=ybegin part=1 total=1 line=128 size=1024 name=Example.Release.rar\r\n=ypart begin=1 end=1024\r\n")}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{BatchSize: 4, MaxHeaderBytes: 256})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if metrics["batch_requested"] != 4 || metrics["batch_selected"] != 1 {
		t.Fatalf("expected batch fill counts from selection stats, got metrics=%v", metrics)
	}
	if metrics["batch_fill_pct"] != float64(25) {
		t.Fatalf("expected 25%% batch fill, got metrics=%v", metrics)
	}
	if metrics["selection_buckets"] != 3 || metrics["selection_empty_buckets"] != 2 {
		t.Fatalf("expected selection bucket metrics, got metrics=%v", metrics)
	}
	if metrics["selection_windowed_requested"] != 4 || metrics["selection_newest_requested"] != 0 {
		t.Fatalf("expected selection lane request metrics, got metrics=%v", metrics)
	}
	if metrics["selection_ready_count"] != 12 || metrics["selection_priority0_ready"] != 3 || metrics["selection_priority_seeded"] != int64(1) {
		t.Fatalf("expected selection queue pressure metrics, got metrics=%v", metrics)
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

func TestRunOnceUsesConfiguredPrefixFetchConcurrency(t *testing.T) {
	candidates := make([]pgindex.YEncRecoveryCandidate, 12)
	for i := range candidates {
		candidates[i] = pgindex.YEncRecoveryCandidate{
			BinaryID:        int64(i + 1),
			ArticleHeaderID: int64(i + 100),
			NewsgroupName:   "alt.binaries.test",
			MessageID:       fmt.Sprintf("msg-%d@test", i),
			Subject:         `[01/01] capped yEnc (1/1)`,
		}
	}
	repo := &fakeRepo{candidates: candidates}
	fetcher := &fakePrefixFetcher{
		body:  []byte("=ybegin part=1 total=1 line=128 size=1024 name=Example.Release.mkv\r\n=ypart begin=1 end=1024\r\n"),
		block: make(chan struct{}),
	}
	const configuredConcurrency = 12
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{BatchSize: 12, MaxHeaderBytes: 256, Concurrency: configuredConcurrency})

	done := make(chan struct{})
	var (
		metrics map[string]any
		runErr  error
	)
	go func() {
		defer close(done)
		metrics, runErr = svc.RunOnceWithMetrics(context.Background())
	}()

	deadline := time.After(2 * time.Second)
	for {
		if fetcher.maxActiveCount() == configuredConcurrency {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for capped workers, max active=%d", fetcher.maxActiveCount())
		case <-time.After(10 * time.Millisecond):
		}
	}
	close(fetcher.block)
	<-done

	if runErr != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", runErr)
	}
	if metrics["effective_concurrency"] != configuredConcurrency {
		t.Fatalf("expected configured concurrency %d, got metrics=%v", configuredConcurrency, metrics)
	}
	if fetcher.maxActiveCount() != configuredConcurrency {
		t.Fatalf("prefix fetch concurrency did not use configured concurrency: %d", fetcher.maxActiveCount())
	}
}

func TestRunOnceStreamsRecoveredRecordsInFlushBatches(t *testing.T) {
	const candidateCount = yencRecoveryStreamFlushSize + 25
	candidates := make([]pgindex.YEncRecoveryCandidate, candidateCount)
	for i := range candidates {
		candidates[i] = pgindex.YEncRecoveryCandidate{
			BinaryID:        int64(i + 1),
			ArticleHeaderID: int64(i + 100),
			NewsgroupName:   "alt.binaries.test",
			MessageID:       fmt.Sprintf("stream-%d@test", i),
			Subject:         `[01/01] stream yEnc (1/1)`,
		}
	}
	repo := &fakeBatchRepo{fakeRepo: &fakeRepo{candidates: candidates}}
	fetcher := &fakePrefixFetcher{body: []byte("=ybegin part=1 total=1 line=128 size=1024 name=Example.Release.mkv\r\n=ypart begin=1 end=1024\r\n")}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{BatchSize: candidateCount, MaxHeaderBytes: 256, Concurrency: 16})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if metrics["write_flush_count"] != 2 {
		t.Fatalf("expected two streaming flushes, got metrics=%v", metrics)
	}
	if metrics["write_flush_rows"] != candidateCount || metrics["write_records_queued"] != candidateCount {
		t.Fatalf("expected all records to be queued/flushed, got metrics=%v", metrics)
	}
	if metrics["write_flush_max_size"] != yencRecoveryStreamFlushSize {
		t.Fatalf("expected max flush size %d, got metrics=%v", yencRecoveryStreamFlushSize, metrics)
	}
	if metrics["recovered"] != candidateCount {
		t.Fatalf("expected recovered count from committed stream batches, got metrics=%v", metrics)
	}
	if len(repo.batchSizes) != 2 || repo.batchSizes[0] != yencRecoveryStreamFlushSize || repo.batchSizes[1] != 25 {
		t.Fatalf("expected batch sizes %d and 25, got %+v", yencRecoveryStreamFlushSize, repo.batchSizes)
	}
}

func TestRunOnceAdmitsPrioritySiblingsForRecoveredStreamBatches(t *testing.T) {
	candidates := []pgindex.YEncRecoveryCandidate{{
		BinaryID:        10,
		ArticleHeaderID: 100,
		NewsgroupName:   "alt.binaries.test",
		MessageID:       "burst-1@test",
		Subject:         `opaque-one`,
	}, {
		BinaryID:        11,
		ArticleHeaderID: 101,
		NewsgroupName:   "alt.binaries.test",
		MessageID:       "burst-2@test",
		Subject:         `opaque-two`,
	}, {
		BinaryID:        10,
		ArticleHeaderID: 102,
		NewsgroupName:   "alt.binaries.test",
		MessageID:       "burst-3@test",
		Subject:         `opaque-three`,
	}}
	repo := &fakeBatchRepo{
		fakeRepo:                 &fakeRepo{candidates: candidates},
		siblingAdmissionUpserted: 42,
	}
	fetcher := &fakePrefixFetcher{body: []byte("=ybegin part=1 total=732 line=128 size=524288000 name=burst.part04.rar\r\n=ypart begin=1 end=716800\r\n")}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{BatchSize: len(candidates), MaxHeaderBytes: 256, Concurrency: 2})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if metrics["sibling_admission_attempts"] != 1 || metrics["sibling_admission_upserted"] != 42 {
		t.Fatalf("expected sibling admission metrics, got metrics=%v", metrics)
	}
	if len(repo.siblingAdmissionBatches) != 1 {
		t.Fatalf("expected one sibling admission batch, got %+v", repo.siblingAdmissionBatches)
	}
	got := repo.siblingAdmissionBatches[0]
	if len(got) != 2 || got[0] != 10 || got[1] != 11 {
		t.Fatalf("expected de-duplicated recovered binary ids [10 11], got %+v", got)
	}
	if len(repo.batchSizes) != 1 || repo.batchSizes[0] != len(candidates) {
		t.Fatalf("expected recovery records to still be applied, got batch sizes %+v", repo.batchSizes)
	}
}

func TestRunOncePassesTargetWindowSelectionOptions(t *testing.T) {
	repo := &fakeRepo{}
	fetcher := &fakePrefixFetcher{}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{
		BatchSize:           2000,
		TargetWindowEnabled: true,
		TargetWindowStart:   "2026-06-18T18:20:00Z",
		TargetWindowEnd:     "2026-06-18T18:40:00Z",
		TargetWindowPercent: 70,
		NewestPercent:       30,
	})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if repo.selectionLimit != 2000 {
		t.Fatalf("expected selection limit 2000, got %d", repo.selectionLimit)
	}
	if repo.selectionOpts.TargetWindowStart == nil || repo.selectionOpts.TargetWindowEnd == nil {
		t.Fatalf("expected target window options, got %+v", repo.selectionOpts)
	}
	if repo.selectionOpts.TargetWindowPercent != 70 || repo.selectionOpts.NewestPercent != 30 {
		t.Fatalf("expected 70/30 target split, got %+v", repo.selectionOpts)
	}
	if metrics["target_window_enabled"] != true || metrics["target_window_pct"] != 70 || metrics["newest_pct"] != 30 {
		t.Fatalf("expected target window metrics, got %v", metrics)
	}
}

func TestRunOnceAllowsTargetWindowOnlySelection(t *testing.T) {
	repo := &fakeRepo{}
	fetcher := &fakePrefixFetcher{}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{
		BatchSize:           2000,
		TargetWindowEnabled: true,
		TargetWindowStart:   "2026-06-18T18:20:00Z",
		TargetWindowEnd:     "2026-06-18T18:40:00Z",
		TargetWindowPercent: 100,
		NewestPercent:       0,
	})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if repo.selectionOpts.TargetWindowPercent != 100 || repo.selectionOpts.NewestPercent != 0 {
		t.Fatalf("expected target-only split, got %+v", repo.selectionOpts)
	}
	if metrics["target_window_pct"] != 100 || metrics["newest_pct"] != 0 {
		t.Fatalf("expected target-only metrics, got %v", metrics)
	}
}

func TestRunOncePassesFairnessNewestSplitWithoutExplicitTargetWindow(t *testing.T) {
	repo := &fakeRepo{}
	fetcher := &fakePrefixFetcher{}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{
		BatchSize:           5000,
		TargetWindowEnabled: false,
		TargetWindowPercent: 100,
		NewestPercent:       0,
	})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics failed: %v", err)
	}
	if repo.selectionLimit != 5000 {
		t.Fatalf("expected selection limit 5000, got %d", repo.selectionLimit)
	}
	if repo.selectionOpts.TargetWindowStart != nil || repo.selectionOpts.TargetWindowEnd != nil {
		t.Fatalf("did not expect literal target window bounds, got %+v", repo.selectionOpts)
	}
	if repo.selectionOpts.TargetWindowPercent != 100 || repo.selectionOpts.NewestPercent != 0 {
		t.Fatalf("expected 100/0 fairness split, got %+v", repo.selectionOpts)
	}
	if metrics["target_window_enabled"] != false || metrics["target_window_pct"] != 100 || metrics["newest_pct"] != 0 {
		t.Fatalf("expected fairness-only split metrics, got %v", metrics)
	}
}

func TestRunOnceRejectsInvalidTargetWindowSplit(t *testing.T) {
	repo := &fakeRepo{}
	fetcher := &fakePrefixFetcher{}
	svc := NewService(repo, match.NewService(), fetcher, nil, Options{
		BatchSize:           100,
		TargetWindowEnabled: true,
		TargetWindowStart:   "2026-06-18T18:20:00Z",
		TargetWindowEnd:     "2026-06-18T18:40:00Z",
		TargetWindowPercent: 60,
		NewestPercent:       60,
	})

	if _, err := svc.RunOnceWithMetrics(context.Background()); err == nil {
		t.Fatal("expected invalid target split to fail")
	}
}

type fakeRepo struct {
	candidates         []pgindex.YEncRecoveryCandidate
	selectionOpts      pgindex.YEncRecoverySelectionOptions
	selectionStats     pgindex.YEncRecoverySelectionStats
	selectionLimit     int
	applied            pgindex.YEncHeaderRecoveryRecord
	notFoundArticleID  int64
	noopArticleID      int64
	transientArticleID int64
	applyErr           error
}

type fakeBatchRepo struct {
	*fakeRepo
	mu                       sync.Mutex
	batchSizes               []int
	siblingAdmissionBatches  [][]int64
	siblingAdmissionUpserted int64
	siblingAdmissionErr      error
}

func (f *fakeRepo) ListYEncRecoveryCandidates(context.Context, int) ([]pgindex.YEncRecoveryCandidate, error) {
	return append([]pgindex.YEncRecoveryCandidate(nil), f.candidates...), nil
}

func (f *fakeRepo) ListYEncRecoveryCandidatesWithOptions(_ context.Context, limit int, opts pgindex.YEncRecoverySelectionOptions) ([]pgindex.YEncRecoveryCandidate, error) {
	f.selectionLimit = limit
	f.selectionOpts = opts
	return append([]pgindex.YEncRecoveryCandidate(nil), f.candidates...), nil
}

func (f *fakeRepo) LastYEncRecoverySelectionStats() pgindex.YEncRecoverySelectionStats {
	return f.selectionStats
}

func (f *fakeRepo) ApplyYEncHeaderRecovery(_ context.Context, in pgindex.YEncHeaderRecoveryRecord) (*pgindex.YEncHeaderRecoveryResult, error) {
	f.applied = in
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	return &pgindex.YEncHeaderRecoveryResult{BinaryID: in.BinaryID, TargetBinaryID: in.BinaryID}, nil
}

func (f *fakeBatchRepo) ApplyYEncHeaderRecoveries(_ context.Context, in []pgindex.YEncHeaderRecoveryRecord) ([]pgindex.YEncHeaderRecoveryResult, error) {
	f.mu.Lock()
	f.batchSizes = append(f.batchSizes, len(in))
	f.mu.Unlock()
	results := make([]pgindex.YEncHeaderRecoveryResult, 0, len(in))
	for _, record := range in {
		results = append(results, pgindex.YEncHeaderRecoveryResult{BinaryID: record.BinaryID, TargetBinaryID: record.BinaryID})
	}
	return results, nil
}

func (f *fakeBatchRepo) BackfillPriorityYEncRecoveryWorkItemsForBinaries(_ context.Context, binaryIDs []int64) (int64, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := append([]int64(nil), binaryIDs...)
	f.siblingAdmissionBatches = append(f.siblingAdmissionBatches, copied)
	if f.siblingAdmissionErr != nil {
		return 0, 0, f.siblingAdmissionErr
	}
	return f.siblingAdmissionUpserted, 0, nil
}

func (f *fakeRepo) RecordYEncRecoveryNotFound(_ context.Context, articleHeaderID int64) error {
	f.notFoundArticleID = articleHeaderID
	return nil
}

func (f *fakeRepo) RecordYEncRecoveryNoop(_ context.Context, articleHeaderID int64) error {
	f.noopArticleID = articleHeaderID
	return nil
}

func (f *fakeRepo) RecordYEncRecoveryTransientFailure(_ context.Context, articleHeaderID int64) error {
	f.transientArticleID = articleHeaderID
	return nil
}

func (f *fakeBatchRepo) RecordYEncRecoveryNotFoundBatch(_ context.Context, articleHeaderIDs []int64) error {
	if len(articleHeaderIDs) > 0 {
		f.notFoundArticleID = articleHeaderIDs[len(articleHeaderIDs)-1]
	}
	return nil
}

func (f *fakeBatchRepo) RecordYEncRecoveryNoopBatch(_ context.Context, articleHeaderIDs []int64) error {
	if len(articleHeaderIDs) > 0 {
		f.noopArticleID = articleHeaderIDs[len(articleHeaderIDs)-1]
	}
	return nil
}

func (f *fakeBatchRepo) RecordYEncRecoveryTransientFailureBatch(_ context.Context, articleHeaderIDs []int64) error {
	if len(articleHeaderIDs) > 0 {
		f.transientArticleID = articleHeaderIDs[len(articleHeaderIDs)-1]
	}
	return nil
}

type fakePrefixFetcher struct {
	body      []byte
	err       error
	maxBytes  int64
	block     chan struct{}
	mu        sync.Mutex
	active    int
	maxActive int
}

func (f *fakePrefixFetcher) FetchBodyPrefix(_ context.Context, _ string, _ []string, maxBytes int64) ([]byte, error) {
	f.mu.Lock()
	f.maxBytes = maxBytes
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	block := f.block
	f.mu.Unlock()

	if block != nil {
		<-block
	}

	f.mu.Lock()
	f.active--
	f.mu.Unlock()

	if f.err != nil {
		return nil, f.err
	}
	if maxBytes > 0 && int64(len(f.body)) > maxBytes {
		return f.body[:int(maxBytes)], nil
	}
	return append([]byte(nil), f.body...), nil
}

func (f *fakePrefixFetcher) maxActiveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxActive
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
