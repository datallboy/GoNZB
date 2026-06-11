package discovery

import (
	"context"
	"fmt"
	"sort"
	"strings"
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

type Service struct {
	repo    repository
	fetcher inspectpkg.ArticleFetcher
	log     logger
	opts    inspectpkg.Options
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
	metrics := map[string]any{"candidate_count": len(candidates), "processed_count": 0, "batch_size": s.opts.CandidateBatchSize}
	if len(candidates) == 0 {
		if s != nil && s.log != nil {
			s.log.Debug("inspect_discovery: no opaque binary candidates available")
		}
		return metrics, nil
	}

	processed := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			metrics["processed_count"] = processed
			return metrics, err
		}
		if err := s.inspectCandidate(ctx, candidate); err != nil {
			metrics["processed_count"] = processed
			return metrics, err
		}
		processed++
	}
	metrics["processed_count"] = processed
	return metrics, nil
}

func (s *Service) inspectCandidate(ctx context.Context, candidate pgindex.BinaryInspectionCandidate) error {
	stageName := string(supervisor.StageInspectDiscovery)
	if err := s.repo.StartBinaryInspection(ctx, stageName, candidate.BinaryID, candidate.ReleaseID, candidate.SourceUpdatedAt); err != nil {
		return err
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
		return fmt.Errorf("build discovery targets: %w", err)
	}

	var (
		bestTarget      = candidate
		bestSample      *inspectpkg.BinaryPrefixSample
		kind            string
		ext             string
		confidence      float64
		sampledBinaries int
	)
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		sample, sampleErr := inspectpkg.SampleBinaryPrefix(ctx, s.repo, s.fetcher, target, minInt64(s.opts.MaxBytes, 4096))
		if sampleErr != nil {
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
		_ = s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:       stageName,
			BinaryID:        candidate.BinaryID,
			ReleaseID:       candidate.ReleaseID,
			ErrorText:       "no materializable opaque binaries found for discovery",
			SourceUpdatedAt: candidate.SourceUpdatedAt,
		})
		return fmt.Errorf("no materializable opaque binaries found for release %s", candidate.ReleaseID)
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
		summary["content_filtered"] = true
		summary["content_filter_reason"] = decision.Reason
		summary["content_filter_rule"] = decision.Rule
		return s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:         stageName,
			BinaryID:          candidate.BinaryID,
			ReleaseID:         candidate.ReleaseID,
			Status:            "completed",
			MaterializedBytes: int64(sampledBinaries) * bestSample.BytesRead,
			ToolProvenance:    inspectpkg.ToolProvenance(s.opts, stageName),
			Summary:           summary,
			SourceUpdatedAt:   candidate.SourceUpdatedAt,
		})
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
			return fmt.Errorf("apply binary recovery %d: %w", bestTarget.BinaryID, err)
		}
		if s != nil && s.log != nil {
			s.log.Info("inspect_discovery: recovered binary_id=%d release_id=%s kind=%s ext=%s confidence=%.2f sampled_files=%d", bestTarget.BinaryID, candidate.ReleaseID, kind, ext, confidence, sampledBinaries)
		}
	} else if s != nil && s.log != nil {
		s.log.Debug("inspect_discovery: no recovery release_id=%s sampled_files=%d last_binary_id=%d signature=%q", candidate.ReleaseID, sampledBinaries, bestTarget.BinaryID, bestSample.Signature)
	}

	return s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
		StageName:         stageName,
		BinaryID:          candidate.BinaryID,
		ReleaseID:         candidate.ReleaseID,
		Status:            "completed",
		MaterializedBytes: int64(sampledBinaries) * bestSample.BytesRead,
		ToolProvenance:    inspectpkg.ToolProvenance(s.opts, stageName),
		Summary:           summary,
		SourceUpdatedAt:   candidate.SourceUpdatedAt,
	})
}

func (s *Service) discoveryTargets(ctx context.Context, candidate pgindex.BinaryInspectionCandidate) ([]pgindex.BinaryInspectionCandidate, error) {
	if strings.TrimSpace(candidate.ReleaseID) == "" {
		return []pgindex.BinaryInspectionCandidate{candidate}, nil
	}

	files, err := s.repo.ListCatalogReleaseFiles(ctx, candidate.ReleaseID)
	if err != nil {
		return nil, fmt.Errorf("list catalog release files %s: %w", candidate.ReleaseID, err)
	}

	opaque := make([]pgindex.CatalogReleaseFile, 0, len(files))
	for _, file := range files {
		if file.IsPars {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(file.FileName)), ".bin") {
			continue
		}
		opaque = append(opaque, file)
	}
	if len(opaque) == 0 {
		return []pgindex.BinaryInspectionCandidate{candidate}, nil
	}

	sort.SliceStable(opaque, func(i, j int) bool {
		if opaque[i].FileIndex != opaque[j].FileIndex {
			return opaque[i].FileIndex < opaque[j].FileIndex
		}
		if opaque[i].SizeBytes != opaque[j].SizeBytes {
			return opaque[i].SizeBytes > opaque[j].SizeBytes
		}
		return opaque[i].BinaryID < opaque[j].BinaryID
	})

	selected := opaque
	const maxOpaqueSamples = 1024
	if len(selected) > maxOpaqueSamples {
		selected = evenlySampleReleaseFiles(selected, maxOpaqueSamples)
	}

	targets := make([]pgindex.BinaryInspectionCandidate, 0, len(selected))
	for _, file := range selected {
		target := candidate
		target.BinaryID = file.BinaryID
		target.FileName = file.FileName
		target.TotalBytes = file.SizeBytes
		targets = append(targets, target)
	}
	return targets, nil
}

func evenlySampleReleaseFiles(files []pgindex.CatalogReleaseFile, limit int) []pgindex.CatalogReleaseFile {
	if len(files) <= limit || limit <= 0 {
		return files
	}
	out := make([]pgindex.CatalogReleaseFile, 0, limit)
	step := float64(len(files)-1) / float64(limit-1)
	seen := make(map[int]struct{}, limit)
	for i := 0; i < limit; i++ {
		idx := int(float64(i) * step)
		if idx >= len(files) {
			idx = len(files) - 1
		}
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		out = append(out, files[idx])
	}
	return out
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
