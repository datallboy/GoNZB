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
}

type bodyPrefixFetcher interface {
	FetchBodyPrefix(ctx context.Context, msgID string, groups []string, maxBytes int64) ([]byte, error)
}

type matcher interface {
	Match(candidate match.Candidate) match.Result
}

type Options struct {
	BatchSize      int
	MaxHeaderBytes int64
	FetchTimeout   time.Duration
	Concurrency    int
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
	return &Service{repo: repo, matcher: matcher, fetcher: fetcher, log: log, opts: opts}
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	metrics := map[string]any{
		"batch_size":       s.opts.BatchSize,
		"max_header_bytes": s.opts.MaxHeaderBytes,
		"candidates":       0,
		"attempted":        0,
		"recovered":        0,
		"merged":           0,
		"noops":            0,
		"fetch_failures":   0,
		"not_found":        0,
		"parse_failures":   0,
	}
	if s == nil || s.repo == nil || s.matcher == nil || s.fetcher == nil {
		return metrics, fmt.Errorf("yenc recovery service is not configured")
	}

	candidates, err := s.repo.ListYEncRecoveryCandidates(ctx, s.opts.BatchSize)
	if err != nil {
		return metrics, fmt.Errorf("list yenc recovery candidates: %w", err)
	}
	metrics["candidates"] = len(candidates)
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
	jobs := make(chan pgindex.YEncRecoveryCandidate)
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
	)
	recordResult := func(result *pgindex.YEncHeaderRecoveryResult, kind string, err error) {
		mu.Lock()
		defer mu.Unlock()
		metrics["attempted"] = metrics["attempted"].(int) + 1
		switch kind {
		case "not_found":
			metrics["not_found"] = metrics["not_found"].(int) + 1
		case "fetch_failure":
			metrics["fetch_failures"] = metrics["fetch_failures"].(int) + 1
		case "parse_failure":
			metrics["parse_failures"] = metrics["parse_failures"].(int) + 1
		case "noop":
			metrics["noops"] = metrics["noops"].(int) + 1
		case "recovered":
			metrics["recovered"] = metrics["recovered"].(int) + 1
			if result != nil && result.Merged {
				metrics["merged"] = metrics["merged"].(int) + 1
			}
		}
		attempted := metrics["attempted"].(int)
		if s.log != nil && (attempted == len(candidates) || attempted%100 == 0) {
			s.log.Info(
				"recover_yenc: progress attempted=%d/%d recovered=%d merged=%d noops=%d not_found=%d fetch_failures=%d parse_failures=%d concurrency=%d",
				attempted,
				len(candidates),
				metrics["recovered"],
				metrics["merged"],
				metrics["noops"],
				metrics["not_found"],
				metrics["fetch_failures"],
				metrics["parse_failures"],
				workerCount,
			)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				if ctx.Err() != nil {
					recordResult(nil, "noop", ctx.Err())
					continue
				}
				result, kind, err := s.recoverCandidate(ctx, candidate)
				recordResult(result, kind, err)
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
	if firstErr != nil {
		return metrics, firstErr
	}

	if s.log != nil {
		s.log.Info(
			"recover_yenc: candidates=%d attempted=%d recovered=%d merged=%d noops=%d not_found=%d fetch_failures=%d parse_failures=%d max_header_bytes=%d concurrency=%d",
			metrics["candidates"],
			metrics["attempted"],
			metrics["recovered"],
			metrics["merged"],
			metrics["noops"],
			metrics["not_found"],
			metrics["fetch_failures"],
			metrics["parse_failures"],
			s.opts.MaxHeaderBytes,
			workerCount,
		)
	}
	return metrics, nil
}

func (s *Service) recoverCandidate(ctx context.Context, candidate pgindex.YEncRecoveryCandidate) (*pgindex.YEncHeaderRecoveryResult, string, error) {
	groups := candidate.FetchGroups()
	if candidate.MessageID == "" || len(groups) == 0 {
		return nil, "noop", nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, s.opts.FetchTimeout)
	defer cancel()

	prefix, err := s.fetcher.FetchBodyPrefix(fetchCtx, candidate.MessageID, groups, s.opts.MaxHeaderBytes)
	if err != nil {
		if errors.Is(err, nntp.ErrArticleNotFound) {
			if markErr := s.repo.RecordYEncRecoveryNotFound(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
				s.log.Warn("recover_yenc: failed to persist not_found backoff article=%d err=%v", candidate.ArticleHeaderID, markErr)
			}
			return nil, "not_found", nil
		}
		if s.log != nil {
			s.log.Warn("recover_yenc: fetch prefix failed article=%d binary=%d err=%v", candidate.ArticleHeaderID, candidate.BinaryID, err)
		}
		return nil, "fetch_failure", nil
	}

	header, err := nzb.ReadYencHeader(bytes.NewReader(prefix))
	if err != nil {
		if markErr := s.repo.RecordYEncRecoveryNotFound(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
			s.log.Warn("recover_yenc: failed to persist parse backoff article=%d err=%v", candidate.ArticleHeaderID, markErr)
		}
		return nil, "parse_failure", nil
	}
	if strings.TrimSpace(header.FileName) == "" {
		return nil, "noop", nil
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
	if strings.TrimSpace(matched.FileName) == "" || strings.HasSuffix(strings.ToLower(matched.FileName), ".bin") {
		return nil, "noop", nil
	}

	result, err := s.repo.ApplyYEncHeaderRecovery(ctx, pgindex.YEncHeaderRecoveryRecord{
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
		TotalParts:        matched.TotalParts,
		MatchConfidence:   matched.MatchConfidence,
		MatchStatus:       matched.MatchStatus,
		GroupingEvidence:  matched.GroupingEvidence,
	})
	if err != nil {
		return nil, "", fmt.Errorf("apply yenc recovery binary=%d article=%d: %w", candidate.BinaryID, candidate.ArticleHeaderID, err)
	}
	return result, "recovered", nil
}

func DefaultStage() Options {
	return Options{BatchSize: 25, MaxHeaderBytes: 8192, FetchTimeout: 10 * time.Second}
}

func DefaultInterval() time.Duration {
	return 10 * time.Minute
}
