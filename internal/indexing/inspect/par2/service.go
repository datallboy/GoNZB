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
	ApplyPAR2InspectionBatch(ctx context.Context, rows []pgindex.PAR2InspectionBatchRecord) (*pgindex.PAR2InspectionBatchResult, error)
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
		"candidate_count":         len(candidates),
		"processed_count":         0,
		"submitted_count":         0,
		"batch_size":              s.opts.CandidateBatchSize,
		"effective_concurrency":   0,
		"candidate_selection_ms":  durationMillis(time.Since(selectionStarted)),
		"processing_ms":           float64(0),
		"run_budget_ms":           durationMillis(runBudget),
		"run_budget_exhausted":    false,
		"claim_ms":                float64(0),
		"workspace_ms":            float64(0),
		"prefix_fetch_ms":         float64(0),
		"parse_ms":                float64(0),
		"full_manifest_count":     0,
		"full_manifest_ms":        float64(0),
		"full_manifest_bytes":     int64(0),
		"artifact_write_ms":       float64(0),
		"set_write_ms":            float64(0),
		"target_write_ms":         float64(0),
		"coverage_write_ms":       float64(0),
		"completion_write_ms":     float64(0),
		"release_rollup_write_ms": float64(0),
		"skipped_write_ms":        float64(0),
		"result_flush_count":      0,
		"result_flush_rows":       int64(0),
		"result_flush_ms":         float64(0),
		"result_flush_failures":   0,
		"result_flush_max_size":   0,
		"result_flush_max_ms":     float64(0),
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
	results := make(chan par2CandidateResult, workerCount*2)
	processed := 0
	submitted := 0
	budgetExhausted := false
	processingStarted := time.Now()
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		flushWG  sync.WaitGroup
		firstErr error
	)
	recordResult := func(candidate pgindex.BinaryInspectionCandidate, candidateDuration time.Duration, candidateMetrics par2CandidateMetrics, err error) {
		mu.Lock()
		defer mu.Unlock()
		mergePAR2Metrics(metrics, candidateMetrics)
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
	flushWG.Add(1)
	go func() {
		defer flushWG.Done()
		s.flushCandidateResults(ctx, results, metrics, processingStarted, &mu, &processed, &firstErr)
	}()
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				if ctx.Err() != nil {
					recordResult(candidate, 0, par2CandidateMetrics{}, ctx.Err())
					continue
				}
				candidateStarted := time.Now()
				result, err := s.inspectCandidate(ctx, candidate)
				result.candidateDuration = time.Since(candidateStarted)
				if err != nil {
					recordResult(candidate, result.candidateDuration, result.metrics, err)
					continue
				}
				select {
				case results <- result:
				case <-ctx.Done():
					recordResult(candidate, result.candidateDuration, result.metrics, ctx.Err())
				}
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
	close(results)
	flushWG.Wait()
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

func (s *Service) flushCandidateResults(ctx context.Context, results <-chan par2CandidateResult, metrics map[string]any, processingStarted time.Time, mu *sync.Mutex, processed *int, firstErr *error) {
	flushSize := par2FlushSize(s.opts, metrics["effective_concurrency"].(int))
	batch := make([]par2CandidateResult, 0, flushSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		started := time.Now()
		rowCount := estimatePAR2FlushRows(batch)
		persistRows := make([]pgindex.PAR2InspectionBatchRecord, 0, len(batch))
		releaseUpdates := make([]pgindex.ReleaseInspectionUpdate, 0, len(batch))
		for _, item := range batch {
			persistRows = append(persistRows, item.persist)
			if item.releaseUpdate != nil {
				releaseUpdates = append(releaseUpdates, *item.releaseUpdate)
			}
		}
		out, err := s.repo.ApplyPAR2InspectionBatch(ctx, persistRows)
		flushDuration := time.Since(started)

		mu.Lock()
		defer mu.Unlock()
		addFloatMetric(metrics, "result_flush_ms", durationMillis(flushDuration))
		addIntMetric(metrics, "result_flush_count", 1)
		addInt64Metric(metrics, "result_flush_rows", rowCount)
		if size := len(batch); size > metrics["result_flush_max_size"].(int) {
			metrics["result_flush_max_size"] = size
		}
		if durationMillis(flushDuration) > metrics["result_flush_max_ms"].(float64) {
			metrics["result_flush_max_ms"] = durationMillis(flushDuration)
		}
		if err != nil {
			addIntMetric(metrics, "result_flush_failures", 1)
			if *firstErr == nil {
				*firstErr = err
			}
			return
		}
		if out != nil && out.RowsWritten > rowCount {
			metrics["result_flush_rows"] = metrics["result_flush_rows"].(int64) + (out.RowsWritten - rowCount)
		}
		for _, update := range releaseUpdates {
			releaseStarted := time.Now()
			err := s.repo.ApplyReleaseInspectionUpdate(ctx, update)
			addFloatMetric(metrics, "release_rollup_write_ms", durationMillis(time.Since(releaseStarted)))
			if err != nil {
				if pgindex.IsReleaseNotFound(err) {
					if s != nil && s.log != nil {
						s.log.Warn("inspect_par2: skipped stale release rollup release_id=%s", update.ReleaseID)
					}
					continue
				}
				addIntMetric(metrics, "result_flush_failures", 1)
				if *firstErr == nil {
					*firstErr = err
				}
				return
			}
		}
		for _, item := range batch {
			mergePAR2Metrics(metrics, item.metrics)
			*processed++
			metrics["processed_count"] = *processed
			metrics["processing_ms"] = durationMillis(time.Since(processingStarted))
			candidateDurationMS := durationMillis(item.candidateDuration)
			if s != nil && s.log != nil && (*processed == metrics["candidate_count"].(int) || *processed%10 == 0 || candidateDurationMS >= 5000) {
				s.log.Info("inspect_par2: progress processed=%d/%d binary_id=%d file=%s duration_ms=%.2f concurrency=%d",
					*processed,
					metrics["candidate_count"],
					item.candidate.BinaryID,
					item.candidate.FileName,
					candidateDurationMS,
					metrics["effective_concurrency"],
				)
			}
		}
		batch = batch[:0]
	}

	for item := range results {
		batch = append(batch, item)
		if len(batch) >= flushSize {
			flush()
		}
	}
	flush()
}

func par2WorkerCount(opts inspectpkg.Options, candidateCount int) int {
	if candidateCount <= 0 {
		return 0
	}
	workers := opts.Concurrency
	if workers <= 0 {
		workers = 1
	}
	if workers > 32 {
		workers = 32
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

type par2CandidateMetrics struct {
	Claim              time.Duration
	Workspace          time.Duration
	PrefixFetch        time.Duration
	Parse              time.Duration
	FullManifest       time.Duration
	FullManifestCount  int
	FullManifestBytes  int64
	ArtifactWrite      time.Duration
	SetWrite           time.Duration
	TargetWrite        time.Duration
	CoverageWrite      time.Duration
	CompletionWrite    time.Duration
	ReleaseRollupWrite time.Duration
	SkippedWrite       time.Duration
}

type par2CandidateResult struct {
	candidate         pgindex.BinaryInspectionCandidate
	metrics           par2CandidateMetrics
	persist           pgindex.PAR2InspectionBatchRecord
	releaseUpdate     *pgindex.ReleaseInspectionUpdate
	candidateDuration time.Duration
}

func mergePAR2Metrics(metrics map[string]any, in par2CandidateMetrics) {
	addFloatMetric(metrics, "claim_ms", durationMillis(in.Claim))
	addFloatMetric(metrics, "workspace_ms", durationMillis(in.Workspace))
	addFloatMetric(metrics, "prefix_fetch_ms", durationMillis(in.PrefixFetch))
	addFloatMetric(metrics, "parse_ms", durationMillis(in.Parse))
	addFloatMetric(metrics, "full_manifest_ms", durationMillis(in.FullManifest))
	addIntMetric(metrics, "full_manifest_count", in.FullManifestCount)
	addInt64Metric(metrics, "full_manifest_bytes", in.FullManifestBytes)
	addFloatMetric(metrics, "artifact_write_ms", durationMillis(in.ArtifactWrite))
	addFloatMetric(metrics, "set_write_ms", durationMillis(in.SetWrite))
	addFloatMetric(metrics, "target_write_ms", durationMillis(in.TargetWrite))
	addFloatMetric(metrics, "coverage_write_ms", durationMillis(in.CoverageWrite))
	addFloatMetric(metrics, "completion_write_ms", durationMillis(in.CompletionWrite))
	addFloatMetric(metrics, "release_rollup_write_ms", durationMillis(in.ReleaseRollupWrite))
	addFloatMetric(metrics, "skipped_write_ms", durationMillis(in.SkippedWrite))
}

func addFloatMetric(metrics map[string]any, key string, delta float64) {
	if delta <= 0 {
		return
	}
	current, _ := metrics[key].(float64)
	metrics[key] = current + delta
}

func addIntMetric(metrics map[string]any, key string, delta int) {
	if delta == 0 {
		return
	}
	current, _ := metrics[key].(int)
	metrics[key] = current + delta
}

func addInt64Metric(metrics map[string]any, key string, delta int64) {
	if delta == 0 {
		return
	}
	current, _ := metrics[key].(int64)
	metrics[key] = current + delta
}

func (s *Service) inspectCandidate(ctx context.Context, candidate pgindex.BinaryInspectionCandidate) (par2CandidateResult, error) {
	stageName := string(supervisor.StageInspectPAR2)
	result := par2CandidateResult{candidate: candidate}
	started := time.Now()
	if err := s.repo.StartBinaryInspection(ctx, stageName, candidate.BinaryID, candidate.ReleaseID, candidate.SourceUpdatedAt); err != nil {
		return result, err
	}
	result.metrics.Claim = time.Since(started)

	started = time.Now()
	workspace, err := s.workspace.PrepareBinaryWorkspace(ctx, stageName, candidate)
	result.metrics.Workspace = time.Since(started)
	if err != nil {
		_ = s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:       stageName,
			BinaryID:        candidate.BinaryID,
			ReleaseID:       candidate.ReleaseID,
			ErrorText:       err.Error(),
			SourceUpdatedAt: candidate.SourceUpdatedAt,
		})
		return result, fmt.Errorf("prepare par2 workspace: %w", err)
	}
	defer workspace.Cleanup()

	started = time.Now()
	sample, err := inspectpkg.SampleBinaryPrefix(ctx, s.repo, s.fetcher, candidate, minInt64PAR2(s.opts.MaxBytes, 256*1024))
	result.metrics.PrefixFetch = time.Since(started)
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
			return result, nil
		}
		started = time.Now()
		persist, persistErr := s.skippedInspectionRecord(candidate, err)
		result.metrics.SkippedWrite = time.Since(started)
		if persistErr != nil {
			return result, persistErr
		}
		result.persist = persist
		if s != nil && s.log != nil {
			s.log.Debug("inspect_par2: skipped binary_id=%d release_id=%s reason=%s", candidate.BinaryID, candidate.ReleaseID, par2ProbeSkipReason(err))
		}
		return result, nil
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
	started = time.Now()
	targets := parseTargetFiles(sample.Prefix)
	result.metrics.Parse = time.Since(started)
	usedFullMaterialization := false
	if shouldMaterializePAR2Manifest(candidate.FileName, isVolume, sample, targets) {
		result.metrics.FullManifestCount = 1
		started = time.Now()
		full, fullErr := inspectpkg.MaterializeBinaryToWorkspace(ctx, s.repo, s.fetcher, candidate, workspaceBinaryPath(workspace, candidate), s.opts.MaxBytes)
		result.metrics.FullManifest = time.Since(started)
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
				result.metrics.FullManifestBytes += full.BytesWritten
				started = time.Now()
				targets = parseTargetFiles(data)
				result.metrics.Parse += time.Since(started)
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

	result.persist = pgindex.PAR2InspectionBatchRecord{
		StageName:       stageName,
		BinaryID:        candidate.BinaryID,
		ReleaseID:       candidate.ReleaseID,
		SourceUpdatedAt: candidate.SourceUpdatedAt,
		ArtifactRows: []pgindex.BinaryInspectionArtifactRecord{{
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
		}},
		PAR2SetRows: []pgindex.BinaryPAR2SetRecord{{
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
		}},
		PAR2TargetRows:    targetRows,
		MaterializedBytes: workspace.MaterializedBytes + sample.BytesRead,
		ToolProvenance:    inspectpkg.ToolProvenance(s.opts, stageName),
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
	result.persist.Summary = summary

	hasPAR2 := true
	if strings.TrimSpace(candidate.ReleaseID) == "" {
		return result, nil
	}
	result.releaseUpdate = &pgindex.ReleaseInspectionUpdate{
		ReleaseID:         candidate.ReleaseID,
		HasPAR2:           &hasPAR2,
		MetadataUpdatedAt: ptrTime(time.Now().UTC()),
	}
	return result, nil
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

func (s *Service) skippedInspectionRecord(candidate pgindex.BinaryInspectionCandidate, cause error) (pgindex.PAR2InspectionBatchRecord, error) {
	stageName := string(supervisor.StageInspectPAR2)
	summary := map[string]any{
		"has_par2":           true,
		"file_name":          candidate.FileName,
		"probe_skip_reason":  par2ProbeSkipReason(cause),
		"probe_error_detail": strings.TrimSpace(cause.Error()),
	}
	return pgindex.PAR2InspectionBatchRecord{
		StageName:       stageName,
		BinaryID:        candidate.BinaryID,
		ReleaseID:       candidate.ReleaseID,
		SourceUpdatedAt: candidate.SourceUpdatedAt,
		ToolProvenance:  inspectpkg.ToolProvenance(s.opts, stageName),
		Summary:         summary,
	}, nil
}

func par2FlushSize(opts inspectpkg.Options, concurrency int) int {
	if concurrency <= 0 {
		concurrency = 1
	}
	size := concurrency * 2
	if size < 4 {
		size = 4
	}
	if size > 16 {
		size = 16
	}
	if opts.CandidateBatchSize > 0 && size > opts.CandidateBatchSize {
		size = opts.CandidateBatchSize
	}
	return size
}

func estimatePAR2FlushRows(batch []par2CandidateResult) int64 {
	var rows int64
	for _, item := range batch {
		rows += int64(len(item.persist.ArtifactRows))
		rows += int64(len(item.persist.PAR2SetRows))
		rows += int64(len(item.persist.PAR2TargetRows))
		rows++
		if item.releaseUpdate != nil {
			rows++
		}
	}
	return rows
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
