package par2

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
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
	ReplaceBinaryInspectionArtifacts(ctx context.Context, stageName string, binaryID int64, rows []pgindex.BinaryInspectionArtifactRecord) error
	ReplaceBinaryPAR2Sets(ctx context.Context, binaryID int64, rows []pgindex.BinaryPAR2SetRecord) error
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
	candidates, err := s.repo.ListBinaryInspectionCandidates(ctx, string(supervisor.StageInspectPAR2), s.opts.CandidateBatchSize)
	if err != nil {
		return fmt.Errorf("list inspect_par2 candidates: %w", err)
	}
	if len(candidates) == 0 {
		if s != nil && s.log != nil {
			s.log.Debug("inspect_par2: no inspection candidates available")
		}
		return nil
	}

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.inspectCandidate(ctx, candidate); err != nil {
			return err
		}
	}
	return nil
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

	stagePath := filepath.Join(workspace.Dir, filepath.Base(candidate.FileName))
	materialized, err := inspectpkg.MaterializeBinaryToWorkspace(ctx, s.repo, s.fetcher, candidate, stagePath, s.opts.MaxBytes)
	if err != nil {
		_ = s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:       stageName,
			BinaryID:        candidate.BinaryID,
			ReleaseID:       candidate.ReleaseID,
			ErrorText:       err.Error(),
			SourceUpdatedAt: candidate.SourceUpdatedAt,
		})
		if isRecoverablePAR2InspectionError(err) {
			if s != nil && s.log != nil {
				s.log.Warn("inspect_par2: candidate failed binary_id=%d release_id=%s err=%v", candidate.BinaryID, candidate.ReleaseID, err)
			}
			return nil
		}
		return fmt.Errorf("materialize par2 binary: %w", err)
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
	signatureOK := materialized.Signature == "par2"

	if err := s.repo.ReplaceBinaryInspectionArtifacts(ctx, stageName, candidate.BinaryID, []pgindex.BinaryInspectionArtifactRecord{{
		BinaryID:     candidate.BinaryID,
		ReleaseID:    candidate.ReleaseID,
		StageName:    stageName,
		ArtifactRole: "decoded_file",
		ArtifactName: candidate.FileName,
		ArtifactPath: materialized.OutputPath,
		BytesTotal:   materialized.ExactSize,
		MIMEType:     materialized.MIMEType,
		Signature:    materialized.Signature,
		SourceKind:   "inspect_par2",
		Metadata: map[string]any{
			"yenc_file_size": materialized.ExactSize,
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
		},
	}}); err != nil {
		return err
	}

	summary := map[string]any{
		"has_par2":        true,
		"file_name":       candidate.FileName,
		"base_name":       base,
		"set_name":        setName,
		"signature_ok":    signatureOK,
		"repairable_hint": true,
		"workspace_path":  workspace.ManifestPath,
	}

	if err := s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
		StageName:         stageName,
		BinaryID:          candidate.BinaryID,
		ReleaseID:         candidate.ReleaseID,
		Status:            "completed",
		MaterializedBytes: workspace.MaterializedBytes + materialized.BytesWritten,
		ToolProvenance:    inspectpkg.ToolProvenance(s.opts, stageName),
		Summary:           summary,
		SourceUpdatedAt:   candidate.SourceUpdatedAt,
	}); err != nil {
		return err
	}

	hasPAR2 := true
	return s.repo.ApplyReleaseInspectionUpdate(ctx, pgindex.ReleaseInspectionUpdate{
		ReleaseID:         candidate.ReleaseID,
		HasPAR2:           &hasPAR2,
		MetadataUpdatedAt: ptrTime(time.Now().UTC()),
	})
}

func ptrTime(v time.Time) *time.Time { return &v }

func parseInt(v string) int {
	n := inspectpkg.ParseInt64(v)
	return int(n)
}

func isRecoverablePAR2InspectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "checksum mismatch") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable")
}
