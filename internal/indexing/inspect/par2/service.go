package par2

import (
	"context"
	"fmt"
	"os"
	"regexp"
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
	ReplaceBinaryInspectionArtifacts(ctx context.Context, stageName string, binaryID int64, rows []pgindex.BinaryInspectionArtifactRecord) error
	ReplaceBinaryPAR2Sets(ctx context.Context, binaryID int64, rows []pgindex.BinaryPAR2SetRecord) error
	ReplaceBinaryPAR2Targets(ctx context.Context, binaryID int64, rows []pgindex.BinaryPAR2TargetRecord) error
	ApplyBinaryPAR2TargetCoverage(ctx context.Context, binaryID int64, rows []pgindex.BinaryPAR2TargetRecord) (*pgindex.BinaryPAR2TargetCoverageResult, error)
	ApplyReleaseInspectionUpdate(ctx context.Context, in pgindex.ReleaseInspectionUpdate) error
	inspectpkg.CatalogReader
}

type Service struct {
	repo      repository
	workspace *inspectpkg.WorkspaceManager
	fetcher   inspectpkg.ArticleFetcher
	log       logger
	opts      inspectpkg.Options
}

var parVolumePartsRE = regexp.MustCompile(`(?i)\.vol(\d+)\+(\d+)\.par2$`)

func NewService(repo repository, workspace *inspectpkg.WorkspaceManager, fetcher inspectpkg.ArticleFetcher, log logger, opts inspectpkg.Options) *Service {
	return &Service{
		repo:      repo,
		workspace: workspace,
		fetcher:   fetcher,
		log:       log,
		opts:      inspectpkg.DefaultOptions(opts),
	}
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	selectionStarted := time.Now()
	candidates, err := s.repo.ListBinaryInspectionCandidates(ctx, string(supervisor.StageInspectPAR2), s.opts.CandidateBatchSize)
	if err != nil {
		return nil, fmt.Errorf("list inspect_par2 candidates: %w", err)
	}
	runBudget := par2RunBudget(s.opts)
	metrics := map[string]any{
		"candidate_count":        len(candidates),
		"processed_count":        0,
		"submitted_count":        0,
		"batch_size":             s.opts.CandidateBatchSize,
		"effective_concurrency":  0,
		"candidate_selection_ms": durationMillis(time.Since(selectionStarted)),
		"processing_ms":          float64(0),
		"run_budget_ms":          durationMillis(runBudget),
		"run_budget_exhausted":   false,
	}
	if len(candidates) == 0 {
		if s != nil && s.log != nil {
			s.log.Debug("inspect_par2: no inspection candidates available")
		}
		return metrics, nil
	}

	workerCount := par2WorkerCount(s.opts, len(candidates))
	metrics["effective_concurrency"] = workerCount
	jobs := make(chan pgindex.BinaryInspectionCandidate)
	processed := 0
	submitted := 0
	budgetExhausted := false
	processingStarted := time.Now()
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
	)
	recordResult := func(candidate pgindex.BinaryInspectionCandidate, candidateDuration time.Duration, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			metrics["processed_count"] = processed
			metrics["processing_ms"] = durationMillis(time.Since(processingStarted))
			if s != nil && s.log != nil {
				s.log.Warn("inspect_par2: failed binary_id=%d release_id=%s file=%s processed=%d/%d duration_ms=%.2f err=%v",
					candidate.BinaryID,
					candidate.ReleaseID,
					candidate.FileName,
					processed,
					len(candidates),
					durationMillis(candidateDuration),
					err,
				)
			}
			return
		}
		processed++
		metrics["processed_count"] = processed
		metrics["processing_ms"] = durationMillis(time.Since(processingStarted))
		candidateDurationMS := durationMillis(candidateDuration)
		if s != nil && s.log != nil && (processed == len(candidates) || processed%10 == 0 || candidateDurationMS >= 5000) {
			s.log.Info("inspect_par2: progress processed=%d/%d binary_id=%d file=%s duration_ms=%.2f concurrency=%d",
				processed,
				len(candidates),
				candidate.BinaryID,
				candidate.FileName,
				candidateDurationMS,
				workerCount,
			)
		}
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				if ctx.Err() != nil {
					recordResult(candidate, 0, ctx.Err())
					continue
				}
				candidateStarted := time.Now()
				err := s.inspectCandidate(ctx, candidate)
				recordResult(candidate, time.Since(candidateStarted), err)
			}
		}()
	}

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			close(jobs)
			wg.Wait()
			metrics["processed_count"] = processed
			metrics["processing_ms"] = durationMillis(time.Since(processingStarted))
			return metrics, err
		}
		if runBudget > 0 && time.Since(processingStarted) >= runBudget && submitted < len(candidates) {
			budgetExhausted = true
			break
		}
		mu.Lock()
		err := firstErr
		mu.Unlock()
		if err != nil {
			break
		}
		jobs <- candidate
		submitted++
	}
	close(jobs)
	wg.Wait()
	metrics["processed_count"] = processed
	metrics["processing_ms"] = durationMillis(time.Since(processingStarted))
	metrics["submitted_count"] = submitted
	metrics["run_budget_exhausted"] = budgetExhausted
	if firstErr != nil {
		return metrics, firstErr
	}
	if budgetExhausted && s != nil && s.log != nil {
		s.log.Info("inspect_par2: run budget exhausted processed=%d/%d submitted=%d budget_ms=%.2f concurrency=%d",
			processed,
			len(candidates),
			submitted,
			durationMillis(runBudget),
			workerCount,
		)
	}
	return metrics, nil
}

func par2WorkerCount(opts inspectpkg.Options, candidateCount int) int {
	if candidateCount <= 0 {
		return 0
	}
	workers := opts.Concurrency
	if workers <= 0 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	if workers > candidateCount {
		workers = candidateCount
	}
	return workers
}

func par2RunBudget(opts inspectpkg.Options) time.Duration {
	if opts.ToolTimeout <= 0 {
		return 2 * time.Minute
	}
	budget := opts.ToolTimeout * 4
	if budget < 30*time.Second {
		return 30 * time.Second
	}
	if budget > 2*time.Minute {
		return 2 * time.Minute
	}
	return budget
}

func (s *Service) inspectCandidate(ctx context.Context, candidate pgindex.BinaryInspectionCandidate) error {
	stageName := string(supervisor.StageInspectPAR2)
	if err := s.repo.StartBinaryInspection(ctx, stageName, candidate.BinaryID, candidate.ReleaseID, candidate.SourceUpdatedAt); err != nil {
		return err
	}

	workspace, err := s.workspace.PrepareBinaryWorkspace(ctx, stageName, candidate)
	if err != nil {
		_ = s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:       stageName,
			BinaryID:        candidate.BinaryID,
			ReleaseID:       candidate.ReleaseID,
			ErrorText:       err.Error(),
			SourceUpdatedAt: candidate.SourceUpdatedAt,
		})
		return fmt.Errorf("prepare par2 workspace: %w", err)
	}
	defer workspace.Cleanup()

	sample, err := inspectpkg.SampleBinaryPrefix(ctx, s.repo, s.fetcher, candidate, minInt64PAR2(s.opts.MaxBytes, 256*1024))
	if err != nil {
		if isRecoverablePAR2InspectionError(err) {
			_ = s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
				StageName:       stageName,
				BinaryID:        candidate.BinaryID,
				ReleaseID:       candidate.ReleaseID,
				ErrorText:       err.Error(),
				SourceUpdatedAt: candidate.SourceUpdatedAt,
			})
			if s != nil && s.log != nil {
				s.log.Warn("inspect_par2: candidate failed binary_id=%d release_id=%s err=%v", candidate.BinaryID, candidate.ReleaseID, err)
			}
			return nil
		}
		if err := s.completeSkippedInspection(ctx, candidate, workspace.ManifestPath, err); err != nil {
			return err
		}
		if s != nil && s.log != nil {
			s.log.Debug("inspect_par2: skipped binary_id=%d release_id=%s reason=%s", candidate.BinaryID, candidate.ReleaseID, par2ProbeSkipReason(err))
		}
		return nil
	}

	base := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(candidate.FileName)), ".par2")
	setName := strings.TrimSpace(candidate.FileName)
	volumeNumber := 0
	recoveryBlocks := 0
	isVolume := false
	if match := parVolumePartsRE.FindStringSubmatch(strings.ToLower(strings.TrimSpace(candidate.FileName))); len(match) == 3 {
		isVolume = true
		base = parVolumePartsRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(candidate.FileName)), ".par2")
		volumeNumber = parseInt(match[1])
		recoveryBlocks = parseInt(match[2])
	}
	signatureOK := sample.Signature == "par2"
	targets := parseTargetFiles(sample.Prefix)
	usedFullMaterialization := false
	if shouldMaterializePAR2Manifest(candidate.FileName, isVolume, sample, targets) {
		full, fullErr := inspectpkg.MaterializeBinaryToWorkspace(ctx, s.repo, s.fetcher, candidate, workspaceBinaryPath(workspace, candidate), s.opts.MaxBytes)
		if fullErr != nil {
			if s != nil && s.log != nil {
				s.log.Warn("inspect_par2: manifest materialization fallback failed binary_id=%d release_id=%s err=%v", candidate.BinaryID, candidate.ReleaseID, fullErr)
			}
		} else {
			data, readErr := os.ReadFile(full.OutputPath)
			if readErr != nil {
				if s != nil && s.log != nil {
					s.log.Warn("inspect_par2: manifest materialization read failed binary_id=%d release_id=%s err=%v", candidate.BinaryID, candidate.ReleaseID, readErr)
				}
			} else {
				workspace.MaterializedBytes += full.BytesWritten
				targets = parseTargetFiles(data)
				usedFullMaterialization = len(targets) > 0
			}
		}
	}
	targetMetadata := make([]map[string]any, 0, len(targets))
	targetRows := make([]pgindex.BinaryPAR2TargetRecord, 0, len(targets))
	for _, target := range targets {
		targetMetadata = append(targetMetadata, map[string]any{
			"name": target.Name,
			"size": target.Size,
		})
		targetRows = append(targetRows, pgindex.BinaryPAR2TargetRecord{
			BinaryID:  candidate.BinaryID,
			ReleaseID: candidate.ReleaseID,
			FileName:  target.Name,
			FileSize:  int64(target.Size),
			Metadata: map[string]any{
				"source": "par2_file_description_packet",
			},
		})
	}

	if err := s.repo.ReplaceBinaryInspectionArtifacts(ctx, stageName, candidate.BinaryID, []pgindex.BinaryInspectionArtifactRecord{{
		BinaryID:     candidate.BinaryID,
		ReleaseID:    candidate.ReleaseID,
		StageName:    stageName,
		ArtifactRole: "prefix_sample",
		ArtifactName: sample.OutputName,
		BytesTotal:   sample.BytesRead,
		MIMEType:     sample.MIMEType,
		Signature:    sample.Signature,
		SourceKind:   "inspect_par2",
		Metadata: map[string]any{
			"bytes_sampled":          sample.BytesRead,
			"exact_size":             sample.ExactSize,
			"full_manifest_fallback": usedFullMaterialization,
		},
	}}); err != nil {
		return err
	}
	if err := s.repo.ReplaceBinaryPAR2Sets(ctx, candidate.BinaryID, []pgindex.BinaryPAR2SetRecord{{
		BinaryID:       candidate.BinaryID,
		ReleaseID:      candidate.ReleaseID,
		SetName:        setName,
		BaseName:       base,
		IsVolume:       isVolume,
		VolumeNumber:   volumeNumber,
		RecoveryBlocks: recoveryBlocks,
		SignatureOK:    signatureOK,
		Metadata: map[string]any{
			"file_name": candidate.FileName,
			"targets":   targetMetadata,
		},
	}}); err != nil {
		return err
	}
	if err := s.repo.ReplaceBinaryPAR2Targets(ctx, candidate.BinaryID, targetRows); err != nil {
		return err
	}
	coverage, err := s.repo.ApplyBinaryPAR2TargetCoverage(ctx, candidate.BinaryID, targetRows)
	if err != nil {
		return err
	}

	summary := map[string]any{
		"has_par2":               true,
		"file_name":              candidate.FileName,
		"base_name":              base,
		"set_name":               setName,
		"signature_ok":           signatureOK,
		"repairable_hint":        true,
		"target_count":           len(targets),
		"targets":                targetMetadata,
		"full_manifest_fallback": usedFullMaterialization,
	}
	if coverage != nil {
		summary["main_target_count"] = coverage.MainTargetCount
		summary["target_coverage_updates"] = coverage.UpdatedBinaryCount
	}

	if err := s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
		StageName:         stageName,
		BinaryID:          candidate.BinaryID,
		ReleaseID:         candidate.ReleaseID,
		Status:            "completed",
		MaterializedBytes: workspace.MaterializedBytes + sample.BytesRead,
		ToolProvenance:    inspectpkg.ToolProvenance(s.opts, stageName),
		Summary:           summary,
		SourceUpdatedAt:   candidate.SourceUpdatedAt,
	}); err != nil {
		return err
	}

	hasPAR2 := true
	if strings.TrimSpace(candidate.ReleaseID) == "" {
		return nil
	}
	if err := s.repo.ApplyReleaseInspectionUpdate(ctx, pgindex.ReleaseInspectionUpdate{
		ReleaseID:         candidate.ReleaseID,
		HasPAR2:           &hasPAR2,
		MetadataUpdatedAt: ptrTime(time.Now().UTC()),
	}); err != nil {
		if pgindex.IsReleaseNotFound(err) {
			if s != nil && s.log != nil {
				s.log.Warn("inspect_par2: skipped stale release rollup binary_id=%d release_id=%s", candidate.BinaryID, candidate.ReleaseID)
			}
			return nil
		}
		return err
	}
	return nil
}

func ptrTime(v time.Time) *time.Time { return &v }

func durationMillis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

func parseInt(v string) int {
	n := inspectpkg.ParseInt64(v)
	return int(n)
}

func minInt64PAR2(values ...int64) int64 {
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

func shouldMaterializePAR2Manifest(fileName string, isVolume bool, sample *inspectpkg.BinaryPrefixSample, targets []targetFile) bool {
	if isVolume {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(fileName))
	if lower == "" || !strings.HasSuffix(lower, ".par2") {
		return false
	}
	if sample == nil || sample.Signature != "par2" {
		return false
	}
	if len(targets) > 0 {
		return false
	}
	if sample.ExactSize <= 0 {
		return false
	}
	return sample.BytesRead < sample.ExactSize
}

func workspaceBinaryPath(workspace *inspectpkg.Workspace, candidate pgindex.BinaryInspectionCandidate) string {
	name := strings.TrimSpace(candidate.FileName)
	if name == "" {
		name = fmt.Sprintf("binary-%d.par2", candidate.BinaryID)
	}
	return workspace.Dir + string(os.PathSeparator) + name
}

func (s *Service) completeSkippedInspection(ctx context.Context, candidate pgindex.BinaryInspectionCandidate, _ string, cause error) error {
	stageName := string(supervisor.StageInspectPAR2)
	summary := map[string]any{
		"has_par2":           true,
		"file_name":          candidate.FileName,
		"probe_skip_reason":  par2ProbeSkipReason(cause),
		"probe_error_detail": strings.TrimSpace(cause.Error()),
	}
	if err := s.repo.ReplaceBinaryInspectionArtifacts(ctx, stageName, candidate.BinaryID, nil); err != nil {
		return err
	}
	if err := s.repo.ReplaceBinaryPAR2Sets(ctx, candidate.BinaryID, nil); err != nil {
		return err
	}
	if err := s.repo.ReplaceBinaryPAR2Targets(ctx, candidate.BinaryID, nil); err != nil {
		return err
	}
	return s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
		StageName:       stageName,
		BinaryID:        candidate.BinaryID,
		ReleaseID:       candidate.ReleaseID,
		Status:          "completed",
		ToolProvenance:  inspectpkg.ToolProvenance(s.opts, stageName),
		Summary:         summary,
		SourceUpdatedAt: candidate.SourceUpdatedAt,
	})
}

func par2ProbeSkipReason(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "has no file for binary"):
		return "release_file_missing"
	case strings.Contains(msg, "has no articles"):
		return "article_refs_missing"
	case strings.Contains(msg, "has no newsgroups"):
		return "newsgroups_missing"
	case strings.Contains(msg, "checksum mismatch"):
		return "article_checksum_mismatch"
	default:
		return "prefix_sample_failed"
	}
}

func isRecoverablePAR2InspectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable")
}
