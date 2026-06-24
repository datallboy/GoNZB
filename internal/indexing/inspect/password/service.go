package password

import (
	"context"
	"encoding/json"
	"fmt"
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
	ListBinaryInspectionCandidatesWithOptions(ctx context.Context, stageName string, limit int, opts pgindex.BinaryInspectionCandidateOptions) ([]pgindex.BinaryInspectionCandidate, error)
	ListPasswordVerificationCandidates(ctx context.Context, limit int) ([]pgindex.PasswordVerificationCandidate, error)
	StartBinaryInspection(ctx context.Context, stageName string, binaryID int64, releaseID string, sourceUpdatedAt *time.Time) error
	CompleteBinaryInspection(ctx context.Context, in pgindex.BinaryInspectionRecord) error
	FailBinaryInspection(ctx context.Context, in pgindex.BinaryInspectionRecord) error
	ReplaceBinaryInspectionArtifacts(ctx context.Context, stageName string, binaryID int64, rows []pgindex.BinaryInspectionArtifactRecord) error
	UpdateReleasePasswordCandidateStatus(ctx context.Context, candidateID int64, status string, verifiedAt *time.Time, lastError string) error
	ApplyReleaseInspectionUpdate(ctx context.Context, in pgindex.ReleaseInspectionUpdate) error
	inspectpkg.CatalogReader
}

type Service struct {
	repo      repository
	workspace *inspectpkg.WorkspaceManager
	fetcher   inspectpkg.ArticleFetcher
	runner    inspectpkg.CommandRunner
	log       logger
	opts      inspectpkg.Options
}

func NewService(repo repository, workspace *inspectpkg.WorkspaceManager, fetcher inspectpkg.ArticleFetcher, runner inspectpkg.CommandRunner, log logger, opts inspectpkg.Options) *Service {
	return &Service{
		repo:      repo,
		workspace: workspace,
		fetcher:   fetcher,
		runner:    runner,
		log:       log,
		opts:      inspectpkg.DefaultOptions(opts),
	}
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	candidates, err := s.repo.ListBinaryInspectionCandidatesWithOptions(ctx, string(supervisor.StageInspectPassword), s.opts.CandidateBatchSize, pgindex.BinaryInspectionCandidateOptions{
		RequireExpectedFileCount: s.opts.RequireExpectedFileCount,
	})
	if err != nil {
		return nil, fmt.Errorf("list inspect_password candidates: %w", err)
	}
	passwords, err := s.repo.ListPasswordVerificationCandidates(ctx, s.opts.CandidateBatchSize*4)
	if err != nil {
		return nil, fmt.Errorf("list password verification candidates: %w", err)
	}
	metrics := map[string]any{
		"candidate_count":          len(candidates),
		"password_candidate_count": len(passwords),
		"processed_count":          0,
		"batch_size":               s.opts.CandidateBatchSize,
	}
	if len(candidates) == 0 && len(passwords) == 0 {
		if s != nil && s.log != nil {
			s.log.Debug("inspect_password: no password verification candidates available")
		}
		return metrics, nil
	}

	candidateByRelease := make(map[string]pgindex.BinaryInspectionCandidate, len(candidates))
	for _, candidate := range candidates {
		if !archiveSummaryEncrypted(candidate.ArchiveSummaryJSON) {
			continue
		}
		candidateByRelease[candidate.ReleaseID] = candidate
	}

	passwordsByRelease := make(map[string][]pgindex.PasswordVerificationCandidate, len(passwords))
	for _, candidate := range passwords {
		passwordsByRelease[candidate.ReleaseID] = append(passwordsByRelease[candidate.ReleaseID], candidate)
	}

	processed := 0
	for releaseID, candidate := range candidateByRelease {
		if err := ctx.Err(); err != nil {
			metrics["processed_count"] = processed
			return metrics, err
		}
		if err := s.inspectCandidate(ctx, candidate, passwordsByRelease[releaseID]); err != nil {
			metrics["processed_count"] = processed
			return metrics, err
		}
		processed++
	}

	metrics["processable_release_count"] = len(candidateByRelease)
	metrics["processed_count"] = processed
	return metrics, nil
}

func (s *Service) inspectCandidate(ctx context.Context, candidate pgindex.BinaryInspectionCandidate, passwords []pgindex.PasswordVerificationCandidate) error {
	stageName := string(supervisor.StageInspectPassword)
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
		return fmt.Errorf("prepare password workspace: %w", err)
	}
	defer workspace.Cleanup()

	probe, err := inspectpkg.PrepareArchiveProbe(ctx, workspace, s.repo, s.fetcher, s.runner, s.log, s.opts, candidate)
	if err != nil {
		_ = s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:       stageName,
			BinaryID:        candidate.BinaryID,
			ReleaseID:       candidate.ReleaseID,
			ErrorText:       err.Error(),
			SourceUpdatedAt: candidate.SourceUpdatedAt,
		})
		return fmt.Errorf("prepare archive password probe: %w", err)
	}
	if probe == nil {
		return nil
	}

	attempted := 0
	var preferredPasswordID *int64
	verified := false

	for _, password := range passwords {
		if err := ctx.Err(); err != nil {
			return err
		}
		attempted++

		output, runErr := s.runner.Run(ctx, s.opts.SevenZipPath, "l", "-slt", "-p"+strings.TrimSpace(password.PasswordValue), probe.ProbePath)
		if passwordVerified(output, runErr) {
			now := time.Now().UTC()
			if err := s.repo.UpdateReleasePasswordCandidateStatus(ctx, password.ID, "verified", &now, ""); err != nil {
				return err
			}
			preferredPasswordID = &password.ID
			verified = true
			break
		}

		lastError := strings.TrimSpace(string(output))
		if lastError == "" && runErr != nil {
			lastError = runErr.Error()
		}
		if lastError == "" {
			lastError = "password candidate rejected"
		}
		if err := s.repo.UpdateReleasePasswordCandidateStatus(ctx, password.ID, "rejected", nil, lastError); err != nil {
			return err
		}
	}

	if err := s.repo.ReplaceBinaryInspectionArtifacts(ctx, stageName, candidate.BinaryID, []pgindex.BinaryInspectionArtifactRecord{{
		BinaryID:     candidate.BinaryID,
		ReleaseID:    candidate.ReleaseID,
		StageName:    stageName,
		ArtifactRole: "password_probe",
		ArtifactName: candidate.FileName,
		BytesTotal:   workspace.MaterializedBytes + probe.MaterializedBytes,
		MIMEType:     "application/x-archive-probe",
		Signature:    "password_probe",
		SourceKind:   "inspect_password",
		Metadata: map[string]any{
			"attempted_candidates": attempted,
			"probe_strategy":       probe.Strategy,
			"encrypted":            probe.Encrypted,
		},
	}}); err != nil {
		return err
	}

	passworded := true
	passwordedKnown := verified
	passwordedUnknown := !verified
	passwordState := "passworded_unknown"
	if verified {
		passwordState = "passworded_known"
	}

	summary := map[string]any{
		"attempted_candidates": attempted,
		"verified_password":    verified,
		"candidate_verified":   verified,
	}
	if preferredPasswordID != nil {
		summary["preferred_password_id"] = *preferredPasswordID
	}

	if err := s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
		StageName:         stageName,
		BinaryID:          candidate.BinaryID,
		ReleaseID:         candidate.ReleaseID,
		Status:            "completed",
		MaterializedBytes: workspace.MaterializedBytes + probe.MaterializedBytes,
		ToolProvenance:    inspectpkg.ToolProvenance(s.opts, stageName),
		Summary:           summary,
		SourceUpdatedAt:   candidate.SourceUpdatedAt,
	}); err != nil {
		return err
	}

	if err := s.repo.ApplyReleaseInspectionUpdate(ctx, pgindex.ReleaseInspectionUpdate{
		ReleaseID:           candidate.ReleaseID,
		Passworded:          &passworded,
		PasswordedKnown:     &passwordedKnown,
		PasswordedUnknown:   &passwordedUnknown,
		PasswordState:       passwordState,
		PreferredPasswordID: preferredPasswordID,
		MetadataUpdatedAt:   ptrTime(time.Now().UTC()),
	}); err != nil {
		if pgindex.IsReleaseNotFound(err) {
			if s != nil && s.log != nil {
				s.log.Warn("inspect_password: skipped stale release rollup binary_id=%d release_id=%s", candidate.BinaryID, candidate.ReleaseID)
			}
			return nil
		}
		return err
	}
	return nil
}

func passwordVerified(output []byte, err error) bool {
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(output))
	return !strings.Contains(lower, "wrong password") &&
		!strings.Contains(lower, "can not open encrypted archive")
}

func archiveSummaryEncrypted(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}

	var summary map[string]any
	if err := json.Unmarshal(raw, &summary); err != nil {
		return false
	}

	encrypted, ok := summary["encrypted"]
	if !ok {
		return false
	}

	switch v := encrypted.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func ptrTime(v time.Time) *time.Time { return &v }
