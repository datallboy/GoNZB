package nfo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	ReplaceBinaryTextEvidence(ctx context.Context, stageName string, binaryID int64, rows []pgindex.BinaryTextEvidenceRecord) error
	UpsertReleasePasswordCandidate(ctx context.Context, in pgindex.ReleasePasswordCandidateRecord) (int64, error)
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

func NewService(repo repository, workspace *inspectpkg.WorkspaceManager, fetcher inspectpkg.ArticleFetcher, log logger, opts inspectpkg.Options) *Service {
	return &Service{repo: repo, workspace: workspace, fetcher: fetcher, log: log, opts: inspectpkg.DefaultOptions(opts)}
}

func (s *Service) RunOnce(ctx context.Context) error {
	candidates, err := s.repo.ListBinaryInspectionCandidates(ctx, string(supervisor.StageInspectNFO), s.opts.CandidateBatchSize)
	if err != nil {
		return fmt.Errorf("list inspect_nfo candidates: %w", err)
	}
	if len(candidates) == 0 {
		if s != nil && s.log != nil {
			s.log.Debug("inspect_nfo: no inspection candidates available")
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
	stageName := string(supervisor.StageInspectNFO)
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
		return fmt.Errorf("prepare nfo workspace: %w", err)
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
		return fmt.Errorf("materialize nfo binary: %w", err)
	}

	textBytes, err := os.ReadFile(materialized.OutputPath)
	if err != nil {
		return fmt.Errorf("read materialized nfo %s: %w", materialized.OutputPath, err)
	}
	textValue := string(textBytes)
	passwords := inspectpkg.ExtractPasswordCandidates(
		candidate.ReleaseTitle,
		candidate.SourceTitle,
		candidate.DeobfuscatedTitle,
		candidate.FileName,
		textValue,
	)
	for _, password := range passwords {
		if _, err := s.repo.UpsertReleasePasswordCandidate(ctx, pgindex.ReleasePasswordCandidateRecord{
			ReleaseID:     candidate.ReleaseID,
			BinaryID:      candidate.BinaryID,
			PasswordValue: password,
			SourceKind:    "nfo_hint",
			SourceRef:     candidate.FileName,
			Confidence:    0.45,
		}); err != nil {
			return err
		}
	}

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
		SourceKind:   "inspect_nfo",
		Metadata: map[string]any{
			"yenc_file_size": materialized.ExactSize,
		},
	}}); err != nil {
		return err
	}
	if err := s.repo.ReplaceBinaryTextEvidence(ctx, stageName, candidate.BinaryID, []pgindex.BinaryTextEvidenceRecord{{
		BinaryID:     candidate.BinaryID,
		ReleaseID:    candidate.ReleaseID,
		StageName:    stageName,
		EvidenceKind: "nfo_text",
		TextValue:    textValue,
		Tokens:       inspectpkg.ExtractTextTokens(textValue),
		Metadata: map[string]any{
			"file_name": candidate.FileName,
		},
	}}); err != nil {
		return err
	}

	hasNFO := true
	summary := map[string]any{
		"has_nfo":             true,
		"candidate_passwords": passwords,
		"text_length":         len(textValue),
		"signature":           materialized.Signature,
		"workspace_path":      workspace.ManifestPath,
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

	return s.repo.ApplyReleaseInspectionUpdate(ctx, pgindex.ReleaseInspectionUpdate{
		ReleaseID:         candidate.ReleaseID,
		HasNFO:            &hasNFO,
		MetadataUpdatedAt: ptrTime(time.Now().UTC()),
	})
}

func ptrTime(v time.Time) *time.Time { return &v }
