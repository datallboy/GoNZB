package archive

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
	ReplaceBinaryInspectionArtifacts(ctx context.Context, stageName string, binaryID int64, rows []pgindex.BinaryInspectionArtifactRecord) error
	ReplaceBinaryArchiveEntries(ctx context.Context, binaryID int64, rows []pgindex.BinaryArchiveEntryRecord) error
	UpsertReleasePasswordCandidate(ctx context.Context, in pgindex.ReleasePasswordCandidateRecord) (int64, error)
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
	candidates, err := s.repo.ListBinaryInspectionCandidates(ctx, string(supervisor.StageInspectArchive), s.opts.CandidateBatchSize)
	if err != nil {
		return fmt.Errorf("list inspect_archive candidates: %w", err)
	}
	if len(candidates) == 0 {
		if s != nil && s.log != nil {
			s.log.Debug("inspect_archive: no inspection candidates available")
		}
		return nil
	}
	candidates, err = s.dedupeCandidates(ctx, candidates)
	if err != nil {
		return fmt.Errorf("dedupe inspect_archive candidates: %w", err)
	}
	if len(candidates) == 0 {
		if s != nil && s.log != nil {
			s.log.Debug("inspect_archive: no deduped inspection candidates available")
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

func (s *Service) dedupeCandidates(ctx context.Context, candidates []pgindex.BinaryInspectionCandidate) ([]pgindex.BinaryInspectionCandidate, error) {
	if len(candidates) <= 1 {
		return candidates, nil
	}
	filesByRelease := make(map[string][]pgindex.CatalogReleaseFile, len(candidates))
	bestByGroup := make(map[string]pgindex.BinaryInspectionCandidate, len(candidates))

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		files, ok := filesByRelease[candidate.ReleaseID]
		if !ok {
			var err error
			files, err = s.repo.ListCatalogReleaseFiles(ctx, candidate.ReleaseID)
			if err != nil {
				return nil, fmt.Errorf("list catalog release files %s: %w", candidate.ReleaseID, err)
			}
			filesByRelease[candidate.ReleaseID] = files
		}
		family := inspectpkg.ArchiveFamilyFiles(candidate.FileName, files)
		groupKey := candidate.ReleaseID + "|" + strings.ToLower(strings.TrimSpace(candidate.FileName))
		if len(family) > 0 {
			groupKey = candidate.ReleaseID + "|" + strings.ToLower(strings.TrimSpace(family[0].FileName)) + fmt.Sprintf("|%d", len(family))
		}
		current, exists := bestByGroup[groupKey]
		if !exists || archiveCandidatePriority(candidate.FileName) < archiveCandidatePriority(current.FileName) {
			bestByGroup[groupKey] = candidate
		}
	}

	out := make([]pgindex.BinaryInspectionCandidate, 0, len(bestByGroup))
	for _, candidate := range bestByGroup {
		out = append(out, candidate)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := time.Time{}
		right := time.Time{}
		if out[i].SourceUpdatedAt != nil {
			left = out[i].SourceUpdatedAt.UTC()
		}
		if out[j].SourceUpdatedAt != nil {
			right = out[j].SourceUpdatedAt.UTC()
		}
		if !left.Equal(right) {
			return left.After(right)
		}
		return out[i].BinaryID > out[j].BinaryID
	})
	return out, nil
}

func archiveCandidatePriority(fileName string) int {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	switch {
	case strings.HasSuffix(lower, ".part01.rar"), strings.HasSuffix(lower, ".part1.rar"):
		return 0
	case strings.HasSuffix(lower, ".7z.001"), strings.HasSuffix(lower, ".zip.001"):
		return 0
	case strings.HasSuffix(lower, ".r00"):
		return 1
	case strings.HasSuffix(lower, ".7z"), strings.HasSuffix(lower, ".zip"), strings.HasSuffix(lower, ".rar"):
		return 2
	default:
		return 3
	}
}

func (s *Service) inspectCandidate(ctx context.Context, candidate pgindex.BinaryInspectionCandidate) error {
	stageName := string(supervisor.StageInspectArchive)
	if err := ctx.Err(); err != nil {
		return err
	}
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
		return fmt.Errorf("prepare archive workspace: %w", err)
	}
	defer workspace.Cleanup()

	encrypted := inspectpkg.InferEncrypted(candidate)
	if s != nil && s.log != nil {
		s.log.Debug(
			"inspect_archive: starting binary_id=%d release_id=%s file=%s",
			candidate.BinaryID,
			candidate.ReleaseID,
			candidate.FileName,
		)
	}
	probe, err := inspectpkg.PrepareArchiveProbe(ctx, workspace, s.repo, s.fetcher, s.runner, s.log, s.opts, candidate)
	if err != nil {
		if isRecoverableArchiveInspectionError(err) {
			_ = s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
				StageName:       stageName,
				BinaryID:        candidate.BinaryID,
				ReleaseID:       candidate.ReleaseID,
				ErrorText:       err.Error(),
				SourceUpdatedAt: candidate.SourceUpdatedAt,
			})
			if s != nil && s.log != nil {
				s.log.Warn("inspect_archive: candidate failed binary_id=%d release_id=%s err=%v", candidate.BinaryID, candidate.ReleaseID, err)
			}
			return nil
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if probe != nil {
		encrypted = encrypted || probe.Encrypted
		if s != nil && s.log != nil {
			s.log.Debug(
				"inspect_archive: binary_id=%d release_id=%s strategy=%s files=%d materialized_bytes=%d",
				candidate.BinaryID,
				candidate.ReleaseID,
				probe.Strategy,
				len(probe.FamilyFileNames),
				probe.MaterializedBytes,
			)
		}
	}
	if probe != nil && strings.TrimSpace(probe.ProbeError) != "" && isRecoverableArchiveInspectionError(fmt.Errorf("%s", probe.ProbeError)) {
		_ = s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:       stageName,
			BinaryID:        candidate.BinaryID,
			ReleaseID:       candidate.ReleaseID,
			ErrorText:       strings.TrimSpace(probe.ProbeError),
			SourceUpdatedAt: candidate.SourceUpdatedAt,
		})
		if s != nil && s.log != nil {
			s.log.Warn("inspect_archive: transient probe failure binary_id=%d release_id=%s err=%s", candidate.BinaryID, candidate.ReleaseID, strings.TrimSpace(probe.ProbeError))
		}
		return nil
	}
	artifactRows := []pgindex.BinaryInspectionArtifactRecord{{
		BinaryID:     candidate.BinaryID,
		ReleaseID:    candidate.ReleaseID,
		StageName:    stageName,
		ArtifactRole: "archive_probe",
		ArtifactName: candidate.FileName,
		BytesTotal:   materializedBytesForProbe(workspace, probe),
		MIMEType:     "application/x-archive-probe",
		Signature:    "archive_probe",
		SourceKind:   "inspect_archive",
		Metadata: map[string]any{
			"probe_strategy": probeStrategy(probe),
			"probe_error":    probeError(probe),
		},
	}}
	if err := s.repo.ReplaceBinaryInspectionArtifacts(ctx, stageName, candidate.BinaryID, artifactRows); err != nil {
		return err
	}

	entryRows := make([]pgindex.BinaryArchiveEntryRecord, 0)
	if probe != nil {
		entryRows = make([]pgindex.BinaryArchiveEntryRecord, 0, len(probe.Entries))
		for _, entry := range probe.Entries {
			entryRows = append(entryRows, pgindex.BinaryArchiveEntryRecord{
				BinaryID:          candidate.BinaryID,
				ReleaseID:         candidate.ReleaseID,
				EntryName:         entry.Name,
				IsDir:             entry.IsDir,
				UncompressedBytes: entry.UncompressedSize,
				CompressedBytes:   entry.CompressedSize,
				Encrypted:         entry.Encrypted,
				Comment:           entry.Comment,
				MediaType:         inspectpkg.DetectMIMEType(nil, entry.Name),
				Signature:         inspectpkg.DetectSignature(nil, entry.Name),
				Metadata:          map[string]any{},
			})
		}
	}
	if err := s.repo.ReplaceBinaryArchiveEntries(ctx, candidate.BinaryID, entryRows); err != nil {
		return err
	}
	passwords := inspectpkg.ExtractPasswordCandidates(candidate.ReleaseTitle, candidate.SourceTitle, candidate.DeobfuscatedTitle, candidate.FileName)
	for _, password := range passwords {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := s.repo.UpsertReleasePasswordCandidate(ctx, pgindex.ReleasePasswordCandidateRecord{
			ReleaseID:     candidate.ReleaseID,
			BinaryID:      candidate.BinaryID,
			PasswordValue: password,
			SourceKind:    "archive_hint",
			SourceRef:     candidate.FileName,
			Confidence:    0.60,
		}); err != nil {
			return err
		}
	}

	summary := map[string]any{
		"archive":             true,
		"encrypted":           encrypted,
		"candidate_passwords": passwords,
	}
	materializedBytes := workspace.MaterializedBytes
	if probe != nil {
		summary["probe_strategy"] = probe.Strategy
		summary["archive_entries"] = probe.EntryNames
		summary["family_files"] = probe.FamilyFileNames
		materializedBytes += probe.MaterializedBytes
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if probe != nil && strings.TrimSpace(probe.ProbeError) != "" {
		if isRecoverableArchiveInspectionError(fmt.Errorf("%s", probe.ProbeError)) {
			summary["probe_error"] = probe.ProbeError
			return s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
				StageName:         stageName,
				BinaryID:          candidate.BinaryID,
				ReleaseID:         candidate.ReleaseID,
				Status:            "failed",
				ErrorText:         probe.ProbeError,
				MaterializedBytes: materializedBytes,
				ToolProvenance:    inspectpkg.ToolProvenance(s.opts, stageName),
				Summary:           summary,
				SourceUpdatedAt:   candidate.SourceUpdatedAt,
			})
		}
		skipReason := archiveProbeSkipReason(probe.ProbeError)
		if skipReason == "" {
			skipReason = "not_archive_or_unsupported"
		}
		summary["probe_skip_reason"] = skipReason
		summary["probe_error_detail"] = probe.ProbeError
	}
	if err := s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
		StageName:         stageName,
		BinaryID:          candidate.BinaryID,
		ReleaseID:         candidate.ReleaseID,
		Status:            "completed",
		MaterializedBytes: materializedBytes,
		ToolProvenance:    inspectpkg.ToolProvenance(s.opts, stageName),
		Summary:           summary,
		SourceUpdatedAt:   candidate.SourceUpdatedAt,
	}); err != nil {
		return err
	}
	if s != nil && s.log != nil && ctx.Err() == nil {
		s.log.Debug(
			"inspect_archive: completed binary_id=%d release_id=%s encrypted=%t entries=%d",
			candidate.BinaryID,
			candidate.ReleaseID,
			encrypted,
			len(probe.EntryNames),
		)
	}
	if probe != nil && strings.TrimSpace(probe.ProbeError) != "" {
		return nil
	}

	archiveCount := 1
	passworded := encrypted
	passwordedUnknown := encrypted
	passwordState := "not_passworded"
	tags := []string{"archive"}
	if encrypted {
		passwordState = "passworded_unknown"
		tags = append(tags, "unresolved_password")
	}

	return s.repo.ApplyReleaseInspectionUpdate(ctx, pgindex.ReleaseInspectionUpdate{
		ReleaseID:         candidate.ReleaseID,
		Encrypted:         &encrypted,
		Passworded:        &passworded,
		PasswordedUnknown: &passwordedUnknown,
		PasswordState:     passwordState,
		ArchiveCount:      &archiveCount,
		MediaTags:         tags,
		MetadataUpdatedAt: ptrTime(time.Now().UTC()),
	})
}

func isRecoverableArchiveInspectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable")
}

func ptrTime(v time.Time) *time.Time { return &v }

func materializedBytesForProbe(workspace *inspectpkg.Workspace, probe *inspectpkg.ArchiveProbeResult) int64 {
	if workspace == nil && probe == nil {
		return 0
	}
	var total int64
	if workspace != nil {
		total += workspace.MaterializedBytes
	}
	if probe != nil {
		total += probe.MaterializedBytes
	}
	return total
}

func probeStrategy(probe *inspectpkg.ArchiveProbeResult) string {
	if probe == nil {
		return ""
	}
	return probe.Strategy
}

func probePath(probe *inspectpkg.ArchiveProbeResult) string {
	if probe == nil {
		return ""
	}
	return probe.ProbePath
}

func probeError(probe *inspectpkg.ArchiveProbeResult) string {
	if probe == nil {
		return ""
	}
	return probe.ProbeError
}

func archiveProbeSkipReason(probeError string) string {
	probeError = strings.TrimSpace(strings.ToLower(probeError))
	switch {
	case strings.Contains(probeError, "7z header declares archive size"):
		return "incomplete_archive_family"
	case strings.Contains(probeError, "insufficient bytes for 7z next header"):
		return "incomplete_archive_family"
	default:
		return ""
	}
}
