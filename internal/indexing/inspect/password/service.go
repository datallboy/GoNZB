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
	ListBinaryInspectionCandidates(ctx context.Context, stageName string, limit int) ([]pgindex.BinaryInspectionCandidate, error)
	ListPasswordVerificationCandidates(ctx context.Context, limit int) ([]pgindex.PasswordVerificationCandidate, error)
	StartBinaryInspection(ctx context.Context, stageName string, binaryID int64, releaseID string, sourceUpdatedAt *time.Time) error
	CompleteBinaryInspection(ctx context.Context, in pgindex.BinaryInspectionRecord) error
	FailBinaryInspection(ctx context.Context, in pgindex.BinaryInspectionRecord) error
	UpdateReleasePasswordCandidateStatus(ctx context.Context, candidateID int64, status string, verifiedAt *time.Time, lastError string) error
	ApplyReleaseInspectionUpdate(ctx context.Context, in pgindex.ReleaseInspectionUpdate) error
}

type Service struct {
	repo repository
	log  logger
	opts inspectpkg.Options
}

func NewService(repo repository, log logger, opts inspectpkg.Options) *Service {
	return &Service{repo: repo, log: log, opts: inspectpkg.DefaultOptions(opts)}
}

func (s *Service) RunOnce(ctx context.Context) error {
	candidates, err := s.repo.ListBinaryInspectionCandidates(ctx, string(supervisor.StageInspectPassword), s.opts.CandidateBatchSize)
	if err != nil {
		return fmt.Errorf("list inspect_password candidates: %w", err)
	}
	passwords, err := s.repo.ListPasswordVerificationCandidates(ctx, s.opts.CandidateBatchSize*4)
	if err != nil {
		return fmt.Errorf("list password verification candidates: %w", err)
	}
	if len(candidates) == 0 && len(passwords) == 0 {
		if s != nil && s.log != nil {
			s.log.Debug("inspect_password: no password verification candidates available")
		}
		return nil
	}

	candidateByRelease := make(map[string]pgindex.BinaryInspectionCandidate, len(candidates))
	for _, candidate := range candidates {
		candidateByRelease[candidate.ReleaseID] = candidate
	}

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !archiveSummaryEncrypted(candidate.ArchiveSummaryJSON) {
			continue
		}
		if err := s.repo.StartBinaryInspection(ctx, string(supervisor.StageInspectPassword), candidate.BinaryID, candidate.ReleaseID, candidate.SourceUpdatedAt); err != nil {
			return err
		}
	}

	verifiedByRelease := make(map[string]bool)
	for _, candidate := range passwords {
		if err := ctx.Err(); err != nil {
			return err
		}

		expected := firstNonEmpty(
			firstPassword(inspectpkg.ExtractPasswordCandidates(candidate.Title)),
			firstPassword(inspectpkg.ExtractPasswordCandidates(candidate.SourceTitle)),
			firstPassword(inspectpkg.ExtractPasswordCandidates(candidate.DeobfuscatedTitle)),
			firstPassword(inspectpkg.ExtractPasswordCandidates(candidate.SourceRef)),
		)

		status := "failed"
		lastError := "password candidate not verified"
		var verifiedAt *time.Time
		if expected != "" && strings.EqualFold(strings.TrimSpace(candidate.PasswordValue), expected) {
			status = "verified"
			lastError = ""
			now := time.Now().UTC()
			verifiedAt = &now
			verifiedByRelease[candidate.ReleaseID] = true
		}

		if err := s.repo.UpdateReleasePasswordCandidateStatus(ctx, candidate.ID, status, verifiedAt, lastError); err != nil {
			return err
		}
	}

	for releaseID, candidate := range candidateByRelease {
		if !archiveSummaryEncrypted(candidate.ArchiveSummaryJSON) {
			continue
		}
		known := verifiedByRelease[releaseID]
		unknown := !known
		passworded := true
		passwordState := "passworded_unknown"
		statusText := "completed"
		summary := map[string]any{
			"verified_password":  known,
			"candidate_verified": known,
		}
		if known {
			passwordState = "passworded_known"
			unknown = false
		}

		if err := s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:       string(supervisor.StageInspectPassword),
			BinaryID:        candidate.BinaryID,
			ReleaseID:       candidate.ReleaseID,
			Status:          statusText,
			ToolProvenance:  inspectpkg.ToolProvenance(s.opts, string(supervisor.StageInspectPassword)),
			Summary:         summary,
			SourceUpdatedAt: candidate.SourceUpdatedAt,
		}); err != nil {
			return err
		}

		if err := s.repo.ApplyReleaseInspectionUpdate(ctx, pgindex.ReleaseInspectionUpdate{
			ReleaseID:         candidate.ReleaseID,
			Passworded:        &passworded,
			PasswordedKnown:   &known,
			PasswordedUnknown: &unknown,
			PasswordState:     passwordState,
			MetadataUpdatedAt: ptrTime(time.Now().UTC()),
		}); err != nil {
			return err
		}
	}

	return nil
}

func firstPassword(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
