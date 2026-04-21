package assemble

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/datallboy/gonzb/internal/indexing/match"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

// local interface only, scoped to assembly service.
type repository interface {
	ListUnassembledArticleHeaders(ctx context.Context, limit int) ([]pgindex.AssemblyCandidate, error)
	EnsurePoster(ctx context.Context, posterName string) (int64, error)
	UpsertBinary(ctx context.Context, in pgindex.BinaryRecord) (int64, error)
	UpsertBinaryPart(ctx context.Context, in pgindex.BinaryPartRecord) error
	RefreshBinaryStats(ctx context.Context, binaryID int64) error
}

// narrow matcher dependency.
type subjectMatcher interface {
	Match(candidate match.Candidate) match.Result
}

type articleFetcher interface {
	Fetch(ctx context.Context, msgID string, groups []string) (io.Reader, error)
}

type Options struct {
	BatchSize int
}

type Service struct {
	repo    repository
	matcher subjectMatcher
	fetcher articleFetcher
	log     logger
	opts    Options
}

func NewService(repo repository, matcher subjectMatcher, fetcher articleFetcher, log logger, opts Options) *Service {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 5000
	}

	return &Service{
		repo:    repo,
		matcher: matcher,
		fetcher: fetcher,
		log:     log,
		opts:    opts,
	}
}

// RunOnce assembles one batch of article headers into binaries + binary_parts.
func (s *Service) RunOnce(ctx context.Context) error {
	if s.repo == nil {
		return fmt.Errorf("assembly repo is required")
	}
	if s.matcher == nil {
		return fmt.Errorf("assembly matcher is required")
	}

	headers, err := s.repo.ListUnassembledArticleHeaders(ctx, s.opts.BatchSize)
	if err != nil {
		return fmt.Errorf("list unassembled article headers: %w", err)
	}
	if len(headers) == 0 {
		s.log.Debug("assemble: no unassembled article headers found")
		return nil
	}

	refreshed := make(map[int64]struct{}, len(headers))
	assembledCount := 0

	for _, header := range headers {
		if err := ctx.Err(); err != nil {
			return err
		}

		candidate := match.Candidate{
			ArticleNumber: header.ArticleNumber,
			MessageID:     header.MessageID,
			Subject:       header.Subject,
			Poster:        header.Poster,
			PostedAt:      header.DateUTC,
			Bytes:         header.Bytes,
			Lines:         header.Lines,
			Xref:          header.Xref,
			RawOverview:   header.RawOverview,
		}
		matched := s.matcher.Match(candidate)
		if s.shouldAttemptYEncRecovery(header, matched) {
			rematched, recovered, err := s.rematchFromYEncHeader(ctx, header, candidate)
			if err != nil {
				return fmt.Errorf("recover yenc metadata for article %d: %w", header.ID, err)
			}
			if recovered {
				matched = rematched
			}
		}

		posterID := header.PosterID
		if posterID <= 0 {
			var err error
			posterID, err = s.repo.EnsurePoster(ctx, header.Poster)
			if err != nil {
				return fmt.Errorf("ensure poster for article %d: %w", header.ID, err)
			}
		}

		binaryID, err := s.repo.UpsertBinary(ctx, pgindex.BinaryRecord{
			ProviderID:        header.ProviderID,
			NewsgroupID:       header.NewsgroupID,
			PosterID:          posterID,
			SourceReleaseKey:  matched.SourceReleaseKey,
			ReleaseFamilyKey:  matched.ReleaseFamilyKey,
			FileFamilyKey:     matched.FileFamilyKey,
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
			PostedAt:          header.DateUTC,
			MatchConfidence:   matched.MatchConfidence,
			MatchStatus:       matched.MatchStatus,
			GroupingEvidence:  matched.GroupingEvidence,
		})
		if err != nil {
			return fmt.Errorf("upsert binary for article %d: %w", header.ID, err)
		}

		if err := s.repo.UpsertBinaryPart(ctx, pgindex.BinaryPartRecord{
			BinaryID:        binaryID,
			ArticleHeaderID: header.ID,
			MessageID:       header.MessageID,
			PartNumber:      matched.PartNumber,
			TotalParts:      matched.TotalParts,
			SegmentBytes:    header.Bytes,
			FileName:        matched.FileName,
		}); err != nil {
			return fmt.Errorf("upsert binary part for article %d: %w", header.ID, err)
		}

		refreshed[binaryID] = struct{}{}
		assembledCount++
	}

	for binaryID := range refreshed {
		if err := s.repo.RefreshBinaryStats(ctx, binaryID); err != nil {
			return fmt.Errorf("refresh binary stats %d: %w", binaryID, err)
		}
	}

	s.log.Info(
		"assemble: processed_headers=%d binaries_refreshed=%d batch_size=%d",
		assembledCount,
		len(refreshed),
		s.opts.BatchSize,
	)

	return nil
}

func (s *Service) shouldAttemptYEncRecovery(header pgindex.AssemblyCandidate, matched match.Result) bool {
	if s == nil || s.fetcher == nil {
		return false
	}
	if header.MessageID == "" {
		return false
	}
	if matched.MatchConfidence >= 0.85 && matched.TotalParts > 1 && matched.FileName != "" && !strings.HasSuffix(strings.ToLower(matched.FileName), ".bin") {
		return false
	}
	if matched.TotalParts > 1 && matched.FileName != "" && matched.FileName == matched.BinaryName && !strings.HasSuffix(strings.ToLower(matched.FileName), ".bin") {
		return false
	}
	opaqueSubject := isOpaqueAssemblySubject(header.Subject)
	opaqueFile := matched.FileName == "" || strings.HasSuffix(strings.ToLower(strings.TrimSpace(matched.FileName)), ".bin")
	return opaqueSubject || opaqueFile || matched.TotalParts <= 1
}

func (s *Service) rematchFromYEncHeader(ctx context.Context, header pgindex.AssemblyCandidate, candidate match.Candidate) (match.Result, bool, error) {
	groups := assemblyFetchGroups(header)
	if len(groups) == 0 {
		return match.Result{}, false, nil
	}

	reader, err := s.fetcher.Fetch(ctx, header.MessageID, groups)
	if err != nil {
		return match.Result{}, false, nil
	}
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}

	yh, err := nzb.ReadYencHeader(reader)
	if err != nil {
		return match.Result{}, false, nil
	}
	if strings.TrimSpace(yh.FileName) == "" {
		return match.Result{}, false, nil
	}

	enrichedRaw := cloneRawOverview(candidate.RawOverview)
	enrichedRaw["name"] = yh.FileName
	if yh.PartNumber > 0 {
		enrichedRaw["part"] = yh.PartNumber
	}
	if yh.TotalParts > 0 {
		enrichedRaw["total"] = yh.TotalParts
	}
	if yh.FileSize > 0 {
		enrichedRaw["size"] = yh.FileSize
	}

	candidate.RawOverview = enrichedRaw
	rematched := s.matcher.Match(candidate)
	if rematched.FileName == "" || strings.HasSuffix(strings.ToLower(rematched.FileName), ".bin") {
		return match.Result{}, false, nil
	}
	return rematched, true, nil
}

func cloneRawOverview(in map[string]any) map[string]any {
	if len(in) == 0 {
		return make(map[string]any, 4)
	}
	out := make(map[string]any, len(in)+4)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isOpaqueAssemblySubject(subject string) bool {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return true
	}
	if strings.ContainsAny(subject, " []()/\"'") {
		return false
	}
	if len(subject) < 16 {
		return false
	}
	for _, r := range subject {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func assemblyFetchGroups(header pgindex.AssemblyCandidate) []string {
	groups := make([]string, 0, 3)
	seen := map[string]struct{}{}
	fields := strings.Fields(strings.TrimSpace(header.Xref))
	for idx, field := range fields {
		if idx == 0 && !strings.Contains(field, ":") {
			continue
		}
		group := field
		if idx := strings.IndexByte(group, ':'); idx >= 0 {
			group = group[:idx]
		}
		if idx := strings.IndexByte(group, ' '); idx >= 0 {
			group = group[:idx]
		}
		group = strings.TrimSpace(group)
		if group == "" || strings.EqualFold(group, "xref:") {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		groups = append(groups, group)
	}
	if strings.TrimSpace(header.NewsgroupName) != "" {
		if _, ok := seen[header.NewsgroupName]; !ok {
			groups = append(groups, header.NewsgroupName)
		}
	}
	return groups
}
