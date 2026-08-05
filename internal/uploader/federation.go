package uploader

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/publicationstate"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/nzb"
)

type FederationBackend interface {
	EligiblePools(context.Context) ([]string, error)
	Publish(context.Context, string, releasecard.LocalRelease, string) (PublicationOutcome, error)
	PublishState(context.Context, string, FederationPublication, string, string) (PublicationOutcome, error)
}

type FederationService struct {
	uploader *Service
	backend  FederationBackend
}

func NewFederationService(service *Service, backend FederationBackend) *FederationService {
	return &FederationService{uploader: service, backend: backend}
}

func (s *FederationService) EligiblePools(ctx context.Context) ([]string, error) {
	if s == nil || s.backend == nil {
		return nil, fmt.Errorf("GoNZBNet publication is unavailable")
	}
	return s.backend.EligiblePools(ctx)
}

func (s *FederationService) List(ctx context.Context, submissionID string) ([]FederationPublication, error) {
	if s == nil || s.uploader == nil || s.uploader.store == nil {
		return nil, fmt.Errorf("uploader federation service is unavailable")
	}
	return s.uploader.store.ListFederationPublications(ctx, strings.TrimSpace(submissionID))
}

func (s *FederationService) Request(ctx context.Context, submissionID string, poolIDs []string, actor string) ([]FederationPublication, error) {
	eligible, err := s.EligiblePools(ctx)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(eligible))
	for _, poolID := range eligible {
		allowed[poolID] = struct{}{}
	}
	poolIDs = normalizedPoolIDs(poolIDs)
	if len(poolIDs) == 0 {
		return nil, fmt.Errorf("at least one pool_id is required")
	}
	for _, poolID := range poolIDs {
		if _, ok := allowed[poolID]; !ok {
			return nil, fmt.Errorf("pool %q is not eligible for release and manifest publication", poolID)
		}
	}
	items := make([]FederationPublication, 0, len(poolIDs))
	for _, poolID := range poolIDs {
		item, err := s.uploader.store.RequestFederationPublication(ctx, submissionID, poolID, actor)
		if err != nil {
			return items, err
		}
		if item.State == PublicationPublished {
			items = append(items, *item)
			continue
		}
		item, err = s.process(ctx, *item)
		if err != nil {
			items = append(items, *item)
			return items, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *FederationService) Withdraw(ctx context.Context, submissionID, poolID, actor, reason string) (*FederationPublication, error) {
	if s == nil || s.backend == nil || s.uploader == nil {
		return nil, fmt.Errorf("GoNZBNet publication is unavailable")
	}
	item, err := s.uploader.store.RequestFederationWithdrawal(ctx, submissionID, poolID, actor, reason)
	if err != nil || item.State == PublicationWithdrawn {
		return item, err
	}
	return s.process(ctx, *item)
}

func (s *FederationService) RetryDue(ctx context.Context, limit int) (int, error) {
	if s == nil || s.backend == nil || s.uploader == nil {
		return 0, nil
	}
	items, err := s.uploader.store.ListDueFederationPublications(ctx, limit)
	if err != nil {
		return 0, err
	}
	completed := 0
	for _, item := range items {
		if _, err := s.process(ctx, item); err == nil {
			completed++
		}
	}
	return completed, nil
}

func (s *FederationService) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, _ = s.RetryDue(ctx, 25)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *FederationService) process(ctx context.Context, publication FederationPublication) (*FederationPublication, error) {
	var outcome PublicationOutcome
	var err error
	if publication.State == PublicationWithdrawalRequested {
		outcome, err = s.backend.PublishState(ctx, publication.PoolID, publication, publicationstate.StateWithdrawn, "local submission returned to pending or withdrawn")
		if err == nil {
			return s.uploader.store.CompleteFederationPublication(ctx, publication.SubmissionID, publication.PoolID, PublicationWithdrawn, outcome)
		}
	} else {
		candidate, candidateErr := s.candidate(ctx, publication.SubmissionID)
		if candidateErr != nil {
			err = candidateErr
		} else {
			outcome, err = s.backend.Publish(ctx, publication.PoolID, candidate, publication.PublicationStateEventID)
			if err == nil {
				return s.uploader.store.CompleteFederationPublication(ctx, publication.SubmissionID, publication.PoolID, PublicationPublished, outcome)
			}
		}
	}
	failed, storeErr := s.uploader.store.FailFederationPublication(ctx, publication.SubmissionID, publication.PoolID, err)
	if storeErr != nil {
		return nil, storeErr
	}
	return failed, err
}

func (s *FederationService) candidate(ctx context.Context, submissionID string) (releasecard.LocalRelease, error) {
	submission, err := s.uploader.store.GetSubmission(ctx, submissionID)
	if err != nil {
		return releasecard.LocalRelease{}, err
	}
	if submission.State != StateApproved {
		return releasecard.LocalRelease{}, ErrInvalidTransition
	}
	reader, err := s.uploader.store.OpenNZB(ctx, submission.ID)
	if err != nil {
		return releasecard.LocalRelease{}, err
	}
	defer reader.Close()
	maxBytes := s.uploader.limits.MaxBytes
	if maxBytes <= 0 {
		maxBytes = nzb.DefaultMaxNZBBytes
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return releasecard.LocalRelease{}, err
	}
	doc, err := nzb.ValidateBytes(data, s.uploader.limits)
	if err != nil {
		return releasecard.LocalRelease{}, err
	}
	files := make([]releasecard.LocalFile, 0, len(doc.Model.Files))
	for i, file := range doc.Model.Files {
		postedAt := time.Unix(file.Date, 0).UTC()
		if file.Date <= 0 {
			postedAt = submission.PostedAt
		}
		segments := make([]releasecard.LocalSegment, 0, len(file.Segments))
		var size int64
		for _, segment := range file.Segments {
			segments = append(segments, releasecard.LocalSegment{Number: segment.Number, Bytes: segment.Bytes, MessageID: segment.MessageID})
			size += segment.Bytes
		}
		name := nzb.SubjectFilename(file.Subject)
		files = append(files, releasecard.LocalFile{
			Name: name, Subject: file.Subject, Poster: file.Poster, PostedAt: &postedAt,
			SizeBytes: size, FileIndex: i + 1, IsPars: strings.EqualFold(filepath.Ext(name), ".par2"),
			ArticleCount: len(segments), TotalParts: len(segments), Segments: segments,
		})
	}
	passwordState := "not_passworded"
	if submission.HasPassword {
		passwordState = "passworded"
	}
	postedAt := submission.PostedAt
	return releasecard.LocalRelease{
		LocalReleaseID: submission.ReleaseID, GUID: submission.ReleaseID, Title: submission.Title,
		Category: submission.Category, CategoryID: submission.CategoryID, SizeBytes: submission.SizeBytes,
		PostedAt: &postedAt, AddedAt: &submission.CreatedAt, FileCount: submission.FileCount,
		CompletionPct: 100, Groups: append([]string(nil), submission.Groups...), Files: files,
		HasPAR2: submission.HasPAR2, HasNFO: submission.HasNFO, PasswordState: passwordState,
		ArchivePassword: submission.Password, Availability: 1, TMDBID: submission.TMDBID,
		TVDBID: submission.TVDBID, IMDBID: submission.IMDBID, ExternalMedia: submission.MediaSource,
		ExternalYear: submission.Year, PrimaryResolution: submission.Resolution,
		PrimaryVideoCodec: submission.VideoCodec, PrimaryAudioCodec: submission.AudioCodec,
		SourceKind: "local_uploader",
	}, nil
}

func normalizedPoolIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
