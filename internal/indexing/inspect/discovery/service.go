package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	inspectpkg "github.com/datallboy/gonzb/internal/indexing/inspect"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type repository interface {
	ListBinaryInspectionCandidates(ctx context.Context, stageName string, limit int) ([]pgindex.BinaryInspectionCandidate, error)
	StartBinaryInspection(ctx context.Context, stageName string, binaryID int64, releaseID string, sourceUpdatedAt *time.Time) error
	CompleteBinaryInspection(ctx context.Context, in pgindex.BinaryInspectionRecord) error
	FailBinaryInspection(ctx context.Context, in pgindex.BinaryInspectionRecord) error
	ApplyBinaryRecovery(ctx context.Context, in pgindex.BinaryRecoveryRecord) error
	inspectpkg.CatalogReader
}

type repositoryWithBodyBudget interface {
	ReserveBodyRequestBudget(ctx context.Context, budgetKey string, limit int64, requested int) (pgindex.BodyRequestBudgetSnapshot, int, error)
}

type Service struct {
	repo    repository
	fetcher inspectpkg.ArticleFetcher
	log     logger
	opts    inspectpkg.Options
}

type inspectionOutcome struct {
	SignatureRecovered int
	Filtered           int
	TerminalSkip       int
	RetryableFailure   int
	SampledFiles       int
	MaterializedBytes  int64
}

func NewService(repo repository, fetcher inspectpkg.ArticleFetcher, log logger, opts inspectpkg.Options) *Service {
	return &Service{
		repo:    repo,
		fetcher: fetcher,
		log:     log,
		opts:    inspectpkg.DefaultOptions(opts),
	}
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	candidates, err := s.repo.ListBinaryInspectionCandidates(ctx, string(supervisor.StageInspectDiscovery), s.opts.CandidateBatchSize)
	if err != nil {
		return nil, fmt.Errorf("list inspect_discovery candidates: %w", err)
	}
	metrics := map[string]any{
		"eligible_candidate_count":   len(candidates),
		"candidate_count":            len(candidates),
		"processed_count":            0,
		"batch_size":                 s.opts.CandidateBatchSize,
		"signature_recovered_count":  0,
		"filtered_count":             0,
		"terminal_skip_count":        0,
		"retryable_failure_count":    0,
		"sampled_file_count":         0,
		"materialized_bytes":         int64(0),
		"body_budget_deferred_count": 0,
	}
	if budgetRepo, ok := s.repo.(repositoryWithBodyBudget); ok && len(candidates) > 0 {
		snapshot, granted, budgetErr := budgetRepo.ReserveBodyRequestBudget(
			ctx,
			"inspect_discovery",
			s.opts.BodyRequestsPerHour,
			len(candidates),
		)
		if budgetErr != nil {
			return metrics, fmt.Errorf("reserve inspect_discovery body budget: %w", budgetErr)
		}
		if granted < len(candidates) {
			metrics["body_budget_deferred_count"] = len(candidates) - granted
			candidates = candidates[:granted]
		}
		metrics["candidate_count"] = len(candidates)
		metrics["body_budget_limit"] = snapshot.Limit
		metrics["body_budget_used"] = snapshot.Used
		metrics["body_budget_remaining"] = snapshot.Remaining
	}
	if len(candidates) == 0 {
		if s != nil && s.log != nil {
			s.log.Debug("inspect_discovery: no opaque binary candidates available")
		}
		return metrics, nil
	}
	metrics["worker_count"] = s.opts.Concurrency

	if s.opts.Concurrency > 1 && len(candidates) > 1 {
		processed, outcome, err := s.inspectCandidatesConcurrently(ctx, candidates)
		metrics["processed_count"] = processed
		addInspectionOutcomeMetrics(metrics, outcome)
		return metrics, err
	}

	processed := 0
	var total inspectionOutcome
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			metrics["processed_count"] = processed
			return metrics, err
		}
		outcome, err := s.inspectCandidate(ctx, candidate)
		if err != nil {
			metrics["processed_count"] = processed
			return metrics, err
		}
		total.add(outcome)
		processed++
	}
	metrics["processed_count"] = processed
	addInspectionOutcomeMetrics(metrics, total)
	return metrics, nil
}

func (s *Service) inspectCandidatesConcurrently(ctx context.Context, candidates []pgindex.BinaryInspectionCandidate) (int, inspectionOutcome, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workerCount := s.opts.Concurrency
	if workerCount > len(candidates) {
		workerCount = len(candidates)
	}
	jobs := make(chan pgindex.BinaryInspectionCandidate)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	var mu sync.Mutex
	processed := 0
	var total inspectionOutcome

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				if err := ctx.Err(); err != nil {
					return
				}
				outcome, err := s.inspectCandidate(ctx, candidate)
				if err != nil {
					select {
					case errs <- err:
						cancel()
					default:
					}
					return
				}
				mu.Lock()
				processed++
				total.add(outcome)
				mu.Unlock()
			}
		}()
	}

queueCandidates:
	for _, candidate := range candidates {
		select {
		case <-ctx.Done():
			break queueCandidates
		case jobs <- candidate:
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errs:
		mu.Lock()
		defer mu.Unlock()
		return processed, total, err
	default:
	}
	if err := ctx.Err(); err != nil && err != context.Canceled {
		mu.Lock()
		defer mu.Unlock()
		return processed, total, err
	}
	mu.Lock()
	defer mu.Unlock()
	return processed, total, nil
}

func (s *Service) inspectCandidate(ctx context.Context, candidate pgindex.BinaryInspectionCandidate) (inspectionOutcome, error) {
	stageName := string(supervisor.StageInspectDiscovery)
	if err := s.repo.StartBinaryInspection(ctx, stageName, candidate.BinaryID, candidate.ReleaseID, candidate.SourceUpdatedAt); err != nil {
		return inspectionOutcome{}, err
	}

	targets, err := s.discoveryTargets(ctx, candidate)
	if err != nil {
		_ = s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:       stageName,
			BinaryID:        candidate.BinaryID,
			ReleaseID:       candidate.ReleaseID,
			ErrorText:       err.Error(),
			SourceUpdatedAt: candidate.SourceUpdatedAt,
		})
		return inspectionOutcome{}, fmt.Errorf("build discovery targets: %w", err)
	}

	var (
		bestTarget      = candidate
		bestSample      *inspectpkg.BinaryPrefixSample
		kind            string
		ext             string
		confidence      float64
		sampledBinaries int
		lastSampleErr   error
		sampleErrCount  int
		terminalErrs    int
	)
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return inspectionOutcome{}, err
		}
		sampleCtx, cancel := context.WithTimeout(ctx, discoverySampleTimeout(s.opts))
		sample, sampleErr := inspectpkg.SampleBinaryPrefix(sampleCtx, s.repo, s.fetcher, target, minInt64(s.opts.MaxBytes, 4096))
		cancel()
		if sampleErr != nil {
			lastSampleErr = sampleErr
			sampleErrCount++
			if discoveryTerminalSampleReason(sampleErr) != "" {
				terminalErrs++
			}
			continue
		}
		sampledBinaries++
		bestTarget = target
		bestSample = sample
		if decision := inspectpkg.EvaluateContentFilter(s.opts, sample); decision.Filtered {
			kind, ext, confidence = "filtered", "", 1
			if s != nil && s.log != nil {
				s.log.Info("inspect_discovery: filtered binary_id=%d release_id=%s reason=%s rule=%s", target.BinaryID, candidate.ReleaseID, decision.Reason, decision.Rule)
			}
			break
		}
		kind, ext, confidence = classifySample(sample)
		if kind != "" && ext != "" {
			break
		}
	}
	if bestSample == nil {
		if sampleErrCount > 0 && terminalErrs == sampleErrCount {
			err := s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
				StageName:       stageName,
				BinaryID:        candidate.BinaryID,
				ReleaseID:       candidate.ReleaseID,
				Status:          "completed",
				ToolProvenance:  inspectpkg.ToolProvenance(s.opts, stageName),
				SourceUpdatedAt: candidate.SourceUpdatedAt,
				Summary: map[string]any{
					"probe_skip_reason":  discoveryTerminalSampleReason(lastSampleErr),
					"probe_error_detail": lastSampleErr.Error(),
					"sampled_files":      0,
					"release_scan_mode":  "opaque_release_family",
				},
			})
			return inspectionOutcome{TerminalSkip: 1}, err
		}
		errorText := "no materializable opaque binaries found for discovery"
		if lastSampleErr != nil {
			errorText = fmt.Sprintf("sample opaque binary prefix: %v", lastSampleErr)
		}
		_ = s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:       stageName,
			BinaryID:        candidate.BinaryID,
			ReleaseID:       candidate.ReleaseID,
			ErrorText:       errorText,
			SourceUpdatedAt: candidate.SourceUpdatedAt,
		})
		return inspectionOutcome{RetryableFailure: 1}, nil
	}

	outcome := inspectionOutcome{
		SampledFiles:      sampledBinaries,
		MaterializedBytes: int64(sampledBinaries) * bestSample.BytesRead,
	}
	summary := map[string]any{
		"signature":         bestSample.Signature,
		"mime_type":         bestSample.MIMEType,
		"bytes_sampled":     bestSample.BytesRead,
		"detected_kind":     kind,
		"detected_ext":      ext,
		"confidence":        confidence,
		"sampled_files":     sampledBinaries,
		"sampled_binary_id": bestTarget.BinaryID,
		"sampled_file_name": bestTarget.FileName,
		"release_scan_mode": "opaque_release_family",
	}
	if decision := inspectpkg.EvaluateContentFilter(s.opts, bestSample); decision.Filtered {
		outcome.Filtered = 1
		summary["content_filtered"] = true
		summary["content_filter_reason"] = decision.Reason
		summary["content_filter_rule"] = decision.Rule
		err := s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:         stageName,
			BinaryID:          candidate.BinaryID,
			ReleaseID:         candidate.ReleaseID,
			Status:            "completed",
			MaterializedBytes: int64(sampledBinaries) * bestSample.BytesRead,
			ToolProvenance:    inspectpkg.ToolProvenance(s.opts, stageName),
			Summary:           summary,
			SourceUpdatedAt:   candidate.SourceUpdatedAt,
		})
		return outcome, err
	}

	if kind != "" && ext != "" {
		if err := s.repo.ApplyBinaryRecovery(ctx, pgindex.BinaryRecoveryRecord{
			BinaryID:     bestTarget.BinaryID,
			Kind:         kind,
			Extension:    ext,
			Source:       "byte_signature",
			Confidence:   confidence,
			Canonicalize: true,
		}); err != nil {
			return outcome, fmt.Errorf("apply binary recovery %d: %w", bestTarget.BinaryID, err)
		}
		outcome.SignatureRecovered = 1
		if s != nil && s.log != nil {
			s.log.Info("inspect_discovery: recovered binary_id=%d release_id=%s kind=%s ext=%s confidence=%.2f sampled_files=%d", bestTarget.BinaryID, candidate.ReleaseID, kind, ext, confidence, sampledBinaries)
		}
	}

	err = s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
		StageName:         stageName,
		BinaryID:          candidate.BinaryID,
		ReleaseID:         candidate.ReleaseID,
		Status:            "completed",
		MaterializedBytes: int64(sampledBinaries) * bestSample.BytesRead,
		ToolProvenance:    inspectpkg.ToolProvenance(s.opts, stageName),
		Summary:           summary,
		SourceUpdatedAt:   candidate.SourceUpdatedAt,
	})
	return outcome, err
}

func (o *inspectionOutcome) add(other inspectionOutcome) {
	if o == nil {
		return
	}
	o.SignatureRecovered += other.SignatureRecovered
	o.Filtered += other.Filtered
	o.TerminalSkip += other.TerminalSkip
	o.RetryableFailure += other.RetryableFailure
	o.SampledFiles += other.SampledFiles
	o.MaterializedBytes += other.MaterializedBytes
}

func addInspectionOutcomeMetrics(metrics map[string]any, outcome inspectionOutcome) {
	if metrics == nil {
		return
	}
	metrics["signature_recovered_count"] = outcome.SignatureRecovered
	metrics["filtered_count"] = outcome.Filtered
	metrics["terminal_skip_count"] = outcome.TerminalSkip
	metrics["retryable_failure_count"] = outcome.RetryableFailure
	metrics["sampled_file_count"] = outcome.SampledFiles
	metrics["materialized_bytes"] = outcome.MaterializedBytes
}

func discoveryTerminalSampleReason(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, " has no articles"):
		return "candidate_no_longer_materializable"
	case strings.Contains(message, " prefix starts at offset "):
		return "prefix_not_available"
	default:
		return ""
	}
}

func (s *Service) discoveryTargets(ctx context.Context, candidate pgindex.BinaryInspectionCandidate) ([]pgindex.BinaryInspectionCandidate, error) {
	_ = ctx
	return []pgindex.BinaryInspectionCandidate{candidate}, nil
}

func discoverySampleTimeout(opts inspectpkg.Options) time.Duration {
	timeout := opts.ToolTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	const maxDiscoverySampleTimeout = 10 * time.Second
	if timeout > maxDiscoverySampleTimeout {
		return maxDiscoverySampleTimeout
	}
	return timeout
}

func classifySample(sample *inspectpkg.BinaryPrefixSample) (string, string, float64) {
	if sample == nil {
		return "", "", 0
	}
	switch strings.TrimSpace(sample.Signature) {
	case "7z":
		return "archive", ".7z", 0.98
	case "rar":
		return "archive", ".rar", 0.98
	case "zip":
		return "archive", ".zip", 0.98
	case "par2":
		return "par2", ".par2", 0.99
	case "text":
		return "nfo", ".nfo", 0.70
	case "matroska":
		return "media", ".mkv", 0.96
	case "mp4":
		return "media", ".mp4", 0.96
	case "avi":
		return "media", ".avi", 0.94
	case "flac":
		return "media", ".flac", 0.96
	case "mp3":
		return "media", ".mp3", 0.90
	default:
		return "", "", 0
	}
}

func minInt64(values ...int64) int64 {
	out := int64(0)
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if out == 0 || value < out {
			out = value
		}
	}
	return out
}
