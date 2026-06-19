package yencrecover

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/match"
	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type repository interface {
	ListYEncRecoveryCandidates(ctx context.Context, limit int) ([]pgindex.YEncRecoveryCandidate, error)
	ApplyYEncHeaderRecovery(ctx context.Context, in pgindex.YEncHeaderRecoveryRecord) (*pgindex.YEncHeaderRecoveryResult, error)
	RecordYEncRecoveryNotFound(ctx context.Context, articleHeaderID int64) error
	RecordYEncRecoveryNoop(ctx context.Context, articleHeaderID int64) error
	RecordYEncRecoveryTransientFailure(ctx context.Context, articleHeaderID int64) error
}

type repositoryWithSelectionOptions interface {
	ListYEncRecoveryCandidatesWithOptions(ctx context.Context, limit int, opts pgindex.YEncRecoverySelectionOptions) ([]pgindex.YEncRecoveryCandidate, error)
}

type repositoryWithBatchHeaderRecovery interface {
	ApplyYEncHeaderRecoveries(ctx context.Context, in []pgindex.YEncHeaderRecoveryRecord) ([]pgindex.YEncHeaderRecoveryResult, error)
}

type bodyPrefixFetcher interface {
	FetchBodyPrefix(ctx context.Context, msgID string, groups []string, maxBytes int64) ([]byte, error)
}

type matcher interface {
	Match(candidate match.Candidate) match.Result
}

type Options struct {
	BatchSize           int
	MaxHeaderBytes      int64
	FetchTimeout        time.Duration
	Concurrency         int
	TargetWindowEnabled bool
	TargetWindowStart   string
	TargetWindowEnd     string
	TargetWindowPercent int
	NewestPercent       int
}

type Service struct {
	repo    repository
	matcher matcher
	fetcher bodyPrefixFetcher
	log     logger
	opts    Options
}

func NewService(repo repository, matcher matcher, fetcher bodyPrefixFetcher, log logger, opts Options) *Service {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 100
	}
	if opts.MaxHeaderBytes <= 0 {
		opts.MaxHeaderBytes = 8192
	}
	if opts.FetchTimeout <= 0 {
		opts.FetchTimeout = 10 * time.Second
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	if opts.TargetWindowPercent <= 0 && opts.NewestPercent <= 0 {
		opts.TargetWindowPercent = 60
		opts.NewestPercent = 40
	}
	return &Service{repo: repo, matcher: matcher, fetcher: fetcher, log: log, opts: opts}
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	metrics := map[string]any{
		"batch_size":             s.opts.BatchSize,
		"max_header_bytes":       s.opts.MaxHeaderBytes,
		"concurrency":            s.opts.Concurrency,
		"effective_concurrency":  0,
		"batch_full":             false,
		"candidates":             0,
		"attempted":              0,
		"candidate_selection_ms": float64(0),
		"processing_ms":          float64(0),
		"fetch_ms":               float64(0),
		"parse_ms":               float64(0),
		"match_ms":               float64(0),
		"write_ms":               float64(0),
		"not_found_write_ms":     float64(0),
		"recovered":              0,
		"merged":                 0,
		"noops":                  0,
		"fetch_failures":         0,
		"not_found":              0,
		"parse_failures":         0,
		"stale_candidates":       0,
		"selected_newest":        0,
		"selected_fairness":      0,
		"selected_target_window": 0,
		"fairness_bucket_start":  "",
		"fairness_bucket_end":    "",
		"target_window_enabled":  s.opts.TargetWindowEnabled,
		"target_window_start":    s.opts.TargetWindowStart,
		"target_window_end":      s.opts.TargetWindowEnd,
		"target_window_pct":      s.opts.TargetWindowPercent,
		"newest_pct":             s.opts.NewestPercent,
	}
	if s == nil || s.repo == nil || s.matcher == nil || s.fetcher == nil {
		return metrics, fmt.Errorf("yenc recovery service is not configured")
	}

	selectionStarted := time.Now()
	selectionOpts, err := s.selectionOptions()
	if err != nil {
		metrics["candidate_selection_ms"] = durationMillis(time.Since(selectionStarted))
		return metrics, err
	}
	var candidates []pgindex.YEncRecoveryCandidate
	if repo, ok := s.repo.(repositoryWithSelectionOptions); ok {
		candidates, err = repo.ListYEncRecoveryCandidatesWithOptions(ctx, s.opts.BatchSize, selectionOpts)
	} else {
		candidates, err = s.repo.ListYEncRecoveryCandidates(ctx, s.opts.BatchSize)
	}
	metrics["candidate_selection_ms"] = durationMillis(time.Since(selectionStarted))
	if err != nil {
		return metrics, fmt.Errorf("list yenc recovery candidates: %w", err)
	}
	metrics["candidates"] = len(candidates)
	for _, candidate := range candidates {
		switch candidate.RecoveryLane {
		case "time_cohort_fairness":
			metrics["selected_fairness"] = metrics["selected_fairness"].(int) + 1
			if candidate.FairnessBucketStart != nil {
				metrics["fairness_bucket_start"] = candidate.FairnessBucketStart.Format(time.RFC3339)
			}
			if candidate.FairnessBucketEnd != nil {
				metrics["fairness_bucket_end"] = candidate.FairnessBucketEnd.Format(time.RFC3339)
			}
		case "target_window":
			metrics["selected_target_window"] = metrics["selected_target_window"].(int) + 1
		default:
			metrics["selected_newest"] = metrics["selected_newest"].(int) + 1
		}
	}
	if len(candidates) == 0 {
		if s.log != nil {
			s.log.Debug("recover_yenc: no recovery candidates available")
		}
		return metrics, nil
	}

	workerCount := s.opts.Concurrency
	if workerCount > len(candidates) {
		workerCount = len(candidates)
	}
	metrics["max_effective_concurrency"] = 0
	metrics["effective_concurrency"] = workerCount
	metrics["batch_full"] = len(candidates) >= s.opts.BatchSize
	batchRepo, batchWrites := s.repo.(repositoryWithBatchHeaderRecovery)
	metrics["batch_writes"] = batchWrites
	jobs := make(chan pgindex.YEncRecoveryCandidate)
	var (
		mu              sync.Mutex
		wg              sync.WaitGroup
		firstErr        error
		recoveryRecords []pgindex.YEncHeaderRecoveryRecord
	)
	recordResult := func(outcome *yencCandidateOutcome, kind string, timings yencCandidateTimings, err error) {
		mu.Lock()
		defer mu.Unlock()
		addYEncDurationMetric(metrics, "fetch_ms", timings.Fetch)
		addYEncDurationMetric(metrics, "parse_ms", timings.Parse)
		addYEncDurationMetric(metrics, "match_ms", timings.Match)
		addYEncDurationMetric(metrics, "write_ms", timings.Write)
		addYEncDurationMetric(metrics, "not_found_write_ms", timings.NotFoundWrite)
		metrics["attempted"] = metrics["attempted"].(int) + 1
		if outcome != nil && outcome.Record != nil {
			recoveryRecords = append(recoveryRecords, *outcome.Record)
		}
		switch kind {
		case "not_found":
			metrics["not_found"] = metrics["not_found"].(int) + 1
		case "fetch_failure":
			metrics["fetch_failures"] = metrics["fetch_failures"].(int) + 1
		case "parse_failure":
			metrics["parse_failures"] = metrics["parse_failures"].(int) + 1
		case "stale":
			metrics["stale_candidates"] = metrics["stale_candidates"].(int) + 1
		case "noop":
			metrics["noops"] = metrics["noops"].(int) + 1
		case "recovered":
			metrics["recovered"] = metrics["recovered"].(int) + 1
			if outcome != nil && outcome.Result != nil && outcome.Result.Merged {
				metrics["merged"] = metrics["merged"].(int) + 1
			}
		}
		attempted := metrics["attempted"].(int)
		if s.log != nil && (attempted == len(candidates) || attempted%100 == 0) {
			s.log.Info(
				"recover_yenc: progress attempted=%d/%d recovered=%d merged=%d noops=%d not_found=%d fetch_failures=%d parse_failures=%d stale_candidates=%d concurrency=%d",
				attempted,
				len(candidates),
				metrics["recovered"],
				metrics["merged"],
				metrics["noops"],
				metrics["not_found"],
				metrics["fetch_failures"],
				metrics["parse_failures"],
				metrics["stale_candidates"],
				workerCount,
			)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	processingStarted := time.Now()
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				if ctx.Err() != nil {
					recordResult(nil, "noop", yencCandidateTimings{}, ctx.Err())
					continue
				}
				outcome, kind, timings, err := s.recoverCandidate(ctx, candidate, batchWrites)
				recordResult(outcome, kind, timings, err)
			}
		}()
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			close(jobs)
			wg.Wait()
			return metrics, err
		}
		jobs <- candidate
	}
	close(jobs)
	wg.Wait()
	if firstErr == nil && batchWrites && len(recoveryRecords) > 0 {
		started := time.Now()
		results, err := batchRepo.ApplyYEncHeaderRecoveries(ctx, recoveryRecords)
		addYEncDurationMetric(metrics, "write_ms", time.Since(started))
		if err != nil {
			firstErr = fmt.Errorf("apply yenc recovery batch: %w", err)
		} else {
			metrics["recovered"] = metrics["recovered"].(int) + len(results)
			for _, result := range results {
				if result.Merged {
					metrics["merged"] = metrics["merged"].(int) + 1
				}
			}
		}
	}
	metrics["processing_ms"] = durationMillis(time.Since(processingStarted))
	if firstErr != nil {
		return metrics, firstErr
	}

	if s.log != nil {
		s.log.Info(
			"recover_yenc: candidates=%d attempted=%d recovered=%d merged=%d noops=%d not_found=%d fetch_failures=%d parse_failures=%d stale_candidates=%d max_header_bytes=%d concurrency=%d",
			metrics["candidates"],
			metrics["attempted"],
			metrics["recovered"],
			metrics["merged"],
			metrics["noops"],
			metrics["not_found"],
			metrics["fetch_failures"],
			metrics["parse_failures"],
			metrics["stale_candidates"],
			s.opts.MaxHeaderBytes,
			workerCount,
		)
	}
	return metrics, nil
}

func (s *Service) selectionOptions() (pgindex.YEncRecoverySelectionOptions, error) {
	if s == nil {
		return pgindex.YEncRecoverySelectionOptions{}, nil
	}
	if s.opts.TargetWindowPercent < 0 || s.opts.TargetWindowPercent > 100 || s.opts.NewestPercent < 0 || s.opts.NewestPercent > 100 || s.opts.TargetWindowPercent+s.opts.NewestPercent != 100 {
		return pgindex.YEncRecoverySelectionOptions{}, fmt.Errorf("recover_yenc target and newest percentages must be 0-100 and total 100")
	}
	if !s.opts.TargetWindowEnabled {
		return pgindex.YEncRecoverySelectionOptions{
			TargetWindowPercent: s.opts.TargetWindowPercent,
			NewestPercent:       s.opts.NewestPercent,
		}, nil
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(s.opts.TargetWindowStart))
	if err != nil {
		return pgindex.YEncRecoverySelectionOptions{}, fmt.Errorf("parse recover_yenc target window start: %w", err)
	}
	end, err := time.Parse(time.RFC3339, strings.TrimSpace(s.opts.TargetWindowEnd))
	if err != nil {
		return pgindex.YEncRecoverySelectionOptions{}, fmt.Errorf("parse recover_yenc target window end: %w", err)
	}
	if !start.Before(end) {
		return pgindex.YEncRecoverySelectionOptions{}, fmt.Errorf("recover_yenc target window start must be before end")
	}
	return pgindex.YEncRecoverySelectionOptions{
		TargetWindowStart:   &start,
		TargetWindowEnd:     &end,
		TargetWindowPercent: s.opts.TargetWindowPercent,
		NewestPercent:       s.opts.NewestPercent,
	}, nil
}

func durationMillis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

func addYEncDurationMetric(metrics map[string]any, key string, d time.Duration) {
	if d <= 0 {
		return
	}
	current, _ := metrics[key].(float64)
	metrics[key] = current + durationMillis(d)
}

type yencCandidateTimings struct {
	Fetch         time.Duration
	Parse         time.Duration
	Match         time.Duration
	Write         time.Duration
	NotFoundWrite time.Duration
}

type yencCandidateOutcome struct {
	Result *pgindex.YEncHeaderRecoveryResult
	Record *pgindex.YEncHeaderRecoveryRecord
}

func (s *Service) recoverCandidate(ctx context.Context, candidate pgindex.YEncRecoveryCandidate, batchWrites bool) (*yencCandidateOutcome, string, yencCandidateTimings, error) {
	var timings yencCandidateTimings
	groups := candidate.FetchGroups()
	if candidate.MessageID == "" || len(groups) == 0 {
		return nil, "noop", timings, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, s.opts.FetchTimeout)
	defer cancel()

	started := time.Now()
	prefix, err := s.fetcher.FetchBodyPrefix(fetchCtx, candidate.MessageID, groups, s.opts.MaxHeaderBytes)
	timings.Fetch = time.Since(started)
	if err != nil {
		if errors.Is(err, nntp.ErrArticleNotFound) {
			started = time.Now()
			if markErr := s.repo.RecordYEncRecoveryNotFound(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
				s.log.Warn("recover_yenc: failed to persist not_found backoff article=%d err=%v", candidate.ArticleHeaderID, markErr)
			}
			timings.NotFoundWrite = time.Since(started)
			return nil, "not_found", timings, nil
		}
		if s.log != nil {
			s.log.Warn("recover_yenc: fetch prefix failed article=%d binary=%d err=%v", candidate.ArticleHeaderID, candidate.BinaryID, err)
		}
		started = time.Now()
		if markErr := s.repo.RecordYEncRecoveryTransientFailure(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
			s.log.Warn("recover_yenc: failed to persist transient backoff article=%d err=%v", candidate.ArticleHeaderID, markErr)
		}
		timings.NotFoundWrite = time.Since(started)
		return nil, "fetch_failure", timings, nil
	}

	started = time.Now()
	header, err := nzb.ReadYencHeader(bytes.NewReader(prefix))
	timings.Parse = time.Since(started)
	if err != nil {
		started = time.Now()
		if markErr := s.repo.RecordYEncRecoveryNotFound(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
			s.log.Warn("recover_yenc: failed to persist parse backoff article=%d err=%v", candidate.ArticleHeaderID, markErr)
		}
		timings.NotFoundWrite = time.Since(started)
		return nil, "parse_failure", timings, nil
	}
	if strings.TrimSpace(header.FileName) == "" {
		started = time.Now()
		if markErr := s.repo.RecordYEncRecoveryNoop(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
			s.log.Warn("recover_yenc: failed to persist noop backoff article=%d err=%v", candidate.ArticleHeaderID, markErr)
		}
		timings.NotFoundWrite = time.Since(started)
		return nil, "noop", timings, nil
	}

	raw := candidate.CloneRawOverview()
	raw["name"] = header.FileName
	if header.PartNumber > 0 {
		raw["part"] = header.PartNumber
	}
	if header.TotalParts > 0 {
		raw["total"] = header.TotalParts
	}
	if header.FileSize > 0 {
		raw["size"] = header.FileSize
	}

	started = time.Now()
	matched := s.matcher.Match(match.Candidate{
		ArticleNumber: candidate.ArticleNumber,
		MessageID:     candidate.MessageID,
		Subject:       candidate.Subject,
		Poster:        candidate.Poster,
		PostedAt:      candidate.DateUTC,
		Bytes:         candidate.Bytes,
		Lines:         candidate.Lines,
		Xref:          candidate.Xref,
		RawOverview:   raw,
	})
	timings.Match = time.Since(started)
	if strings.TrimSpace(matched.FileName) == "" || strings.HasSuffix(strings.ToLower(matched.FileName), ".bin") {
		started = time.Now()
		if markErr := s.repo.RecordYEncRecoveryNoop(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
			s.log.Warn("recover_yenc: failed to persist noop backoff article=%d err=%v", candidate.ArticleHeaderID, markErr)
		}
		timings.NotFoundWrite = time.Since(started)
		return nil, "noop", timings, nil
	}

	started = time.Now()
	record := pgindex.YEncHeaderRecoveryRecord{
		BinaryID:          candidate.BinaryID,
		ArticleHeaderID:   candidate.ArticleHeaderID,
		SourceReleaseKey:  matched.SourceReleaseKey,
		ReleaseFamilyKey:  matched.ReleaseFamilyKey,
		FileSetKey:        matched.FileSetKey,
		FileFamilyKey:     matched.FileFamilyKey,
		IdentityStrength:  matched.IdentityStrength,
		IdentityReason:    matched.IdentityReason,
		SubjectSetToken:   matched.SubjectSetToken,
		SubjectSetKind:    matched.SubjectSetKind,
		FamilyKind:        matched.FamilyKind,
		BaseStem:          matched.BaseStem,
		IsAuxiliary:       matched.IsAuxiliary,
		IsMainPayload:     matched.IsMainPayload,
		ReleaseKey:        matched.ReleaseKey,
		ReleaseName:       matched.ReleaseName,
		BinaryKey:         matched.BinaryKey,
		BinaryName:        matched.BinaryName,
		FileName:          matched.FileName,
		FileIndex:         matched.FileIndex,
		ExpectedFileCount: matched.ExpectedFileCount,
		PartNumber:        matched.PartNumber,
		TotalParts:        matched.TotalParts,
		FileSize:          header.FileSize,
		MatchConfidence:   matched.MatchConfidence,
		MatchStatus:       matched.MatchStatus,
		GroupingEvidence:  matched.GroupingEvidence,
	}
	if batchWrites {
		return &yencCandidateOutcome{Record: &record}, "ready", timings, nil
	}

	result, err := s.repo.ApplyYEncHeaderRecovery(ctx, record)
	timings.Write = time.Since(started)
	if err != nil {
		if pgindex.IsBinaryNotFound(err) {
			if s.log != nil {
				s.log.Debug("recover_yenc: skipped stale binary article=%d binary=%d err=%v", candidate.ArticleHeaderID, candidate.BinaryID, err)
			}
			if markErr := s.repo.RecordYEncRecoveryTransientFailure(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
				s.log.Warn("recover_yenc: failed to release stale candidate article=%d err=%v", candidate.ArticleHeaderID, markErr)
			}
			return nil, "stale", timings, nil
		}
		if markErr := s.repo.RecordYEncRecoveryTransientFailure(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
			s.log.Warn("recover_yenc: failed to release failed candidate article=%d err=%v", candidate.ArticleHeaderID, markErr)
		}
		return nil, "", timings, fmt.Errorf("apply yenc recovery binary=%d article=%d: %w", candidate.BinaryID, candidate.ArticleHeaderID, err)
	}
	return &yencCandidateOutcome{Result: result}, "recovered", timings, nil
}

func DefaultStage() Options {
	return Options{BatchSize: 25, MaxHeaderBytes: 8192, FetchTimeout: 10 * time.Second}
}

func DefaultInterval() time.Duration {
	return 10 * time.Minute
}
