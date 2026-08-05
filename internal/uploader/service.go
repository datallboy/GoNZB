package uploader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/datallboy/gonzb/internal/categories/newsnab"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/segmentio/ksuid"
)

type Service struct {
	store              Store
	limits             nzb.Limits
	maxArtifactBytes   int64
	maxSubmissionBytes int64
	now                func() time.Time
}

type IntakeLimits struct {
	MaxArtifactBytes   int64
	MaxSubmissionBytes int64
}

func NewService(store Store, limits nzb.Limits, intake ...IntakeLimits) *Service {
	artifactBytes := int64(32 << 20)
	submissionBytes := int64(128 << 20)
	if len(intake) > 0 {
		if intake[0].MaxArtifactBytes > 0 {
			artifactBytes = intake[0].MaxArtifactBytes
		}
		if intake[0].MaxSubmissionBytes > 0 {
			submissionBytes = intake[0].MaxSubmissionBytes
		}
	}
	return &Service{store: store, limits: limits, maxArtifactBytes: artifactBytes, maxSubmissionBytes: submissionBytes, now: time.Now}
}

func (s *Service) Submit(ctx context.Context, in SubmitInput) (CreateResult, error) {
	if s == nil || s.store == nil {
		return CreateResult{}, fmt.Errorf("uploader store is unavailable")
	}
	doc, err := nzb.ValidateBytes(in.NZBBytes, s.limits)
	if err != nil {
		return CreateResult{}, err
	}
	if in.IntakeKind == "" {
		in.IntakeKind = IntakeHTTP
	}
	if err := validateMetadata(in.Metadata); err != nil {
		return CreateResult{}, err
	}
	if len(strings.TrimSpace(in.IdempotencyKey)) > 4096 {
		return CreateResult{}, fmt.Errorf("idempotency key exceeds field limit")
	}
	artifacts, err := s.buildArtifacts(in.Artifacts, in.Metadata.Artifacts)
	if err != nil {
		return CreateResult{}, err
	}
	totalBytes := int64(len(in.NZBBytes))
	for _, artifact := range artifacts {
		if totalBytes > s.maxSubmissionBytes-artifact.SizeBytes {
			return CreateResult{}, fmt.Errorf("submission exceeds %d byte limit", s.maxSubmissionBytes)
		}
		totalBytes += artifact.SizeBytes
	}

	sum := sha256.Sum256(in.NZBBytes)
	hash := hex.EncodeToString(sum[:])
	title := firstNonBlank(in.Metadata.Title, doc.Facts.Title, filenameTitle(in.OriginalFilename))
	if title == "" {
		title = "NZB " + hash[:12]
	}
	categoryID := in.Metadata.CategoryID
	if categoryID == 0 {
		categoryID = newsnab.OtherMisc
	}
	postedAt := doc.Facts.PostedAt
	if strings.TrimSpace(in.Metadata.PostedAt) != "" {
		postedAt, err = time.Parse(time.RFC3339, strings.TrimSpace(in.Metadata.PostedAt))
		if err != nil {
			return CreateResult{}, fmt.Errorf("metadata.posted_at must be RFC3339: %w", err)
		}
		postedAt = postedAt.UTC()
	}
	if postedAt.IsZero() {
		postedAt = s.now().UTC()
	}
	password := in.Metadata.Password
	if password == "" {
		password = doc.Facts.Password
	}
	now := s.now().UTC()
	submission := Submission{
		ID:                   ksuid.New().String(),
		State:                StatePendingReview,
		ReleaseID:            domain.GenerateCompositeID(SourceName, hash),
		Title:                strings.TrimSpace(title),
		NormalizedTitle:      NormalizeTitle(title),
		CategoryID:           categoryID,
		Category:             newsnab.DisplayName(categoryID),
		SizeBytes:            doc.Facts.SizeBytes,
		PostedAt:             postedAt,
		Poster:               doc.Facts.Poster,
		Groups:               append([]string(nil), doc.Facts.Groups...),
		FileCount:            doc.Facts.FileCount,
		SegmentCount:         doc.Facts.SegmentCount,
		Password:             password,
		HasPassword:          password != "",
		HasPAR2:              doc.Facts.HasPAR2 || in.Metadata.Flags.HasPAR2,
		HasNFO:               doc.Facts.HasNFO,
		ObfuscatedSubjects:   in.Metadata.Flags.ObfuscatedSubjects,
		EncryptedNames:       in.Metadata.Flags.EncryptedNames,
		IMDBID:               strings.TrimSpace(in.Metadata.ExternalIDs.IMDBID),
		TMDBID:               in.Metadata.ExternalIDs.TMDBID,
		TVDBID:               in.Metadata.ExternalIDs.TVDBID,
		Year:                 in.Metadata.Media.Year,
		Resolution:           strings.TrimSpace(in.Metadata.Media.Resolution),
		MediaSource:          strings.TrimSpace(in.Metadata.Media.Source),
		VideoCodec:           strings.TrimSpace(in.Metadata.Media.VideoCodec),
		AudioCodec:           strings.TrimSpace(in.Metadata.Media.AudioCodec),
		NZBSHA256:            hash,
		IdempotencyKey:       strings.TrimSpace(in.IdempotencyKey),
		IntakeKind:           in.IntakeKind,
		ProvenanceTool:       strings.TrimSpace(in.Metadata.Provenance.Tool),
		ProvenanceVersion:    strings.TrimSpace(in.Metadata.Provenance.Version),
		ProvenanceExternalID: strings.TrimSpace(in.Metadata.Provenance.ExternalID),
		OriginalFilename:     filepath.Base(strings.TrimSpace(in.OriginalFilename)),
		SubmittedBy:          strings.TrimSpace(in.SubmittedBy),
		Files:                append([]nzb.FileFacts(nil), doc.Facts.Files...),
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	return s.store.CreateSubmission(ctx, submission, in.NZBBytes, artifacts)
}

func (s *Service) Get(ctx context.Context, id string, revealPassword bool) (*Submission, error) {
	item, err := s.store.GetSubmission(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if !revealPassword {
		item.Password = ""
	}
	return item, nil
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]Submission, error) {
	items, err := s.store.ListSubmissions(ctx, filter)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Password = ""
		items[i].Files = nil
	}
	return items, nil
}

func (s *Service) Update(ctx context.Context, id string, update Update, revealPassword bool) (*Submission, error) {
	if update.Title != nil {
		value := strings.TrimSpace(*update.Title)
		if value == "" {
			return nil, fmt.Errorf("title must not be blank")
		}
		if len(value) > 16<<10 {
			return nil, fmt.Errorf("title exceeds field limit")
		}
		update.Title = &value
	}
	if update.CategoryID != nil {
		if _, ok := newsnab.Lookup(*update.CategoryID); !ok {
			return nil, fmt.Errorf("category_id is invalid")
		}
	}
	if update.Password != nil && len(*update.Password) > 16<<10 {
		return nil, fmt.Errorf("password exceeds field limit")
	}
	if update.IMDBID != nil {
		value := strings.TrimSpace(*update.IMDBID)
		if len(value) > 16<<10 {
			return nil, fmt.Errorf("imdb_id exceeds field limit")
		}
		update.IMDBID = &value
	}
	if update.TMDBID != nil && *update.TMDBID < 0 {
		return nil, fmt.Errorf("tmdb_id must not be negative")
	}
	if update.TVDBID != nil && *update.TVDBID < 0 {
		return nil, fmt.Errorf("tvdb_id must not be negative")
	}
	if update.Year != nil && (*update.Year < 0 || *update.Year > 9999) {
		return nil, fmt.Errorf("year is invalid")
	}
	for name, value := range map[string]*string{
		"resolution": update.Resolution, "media_source": update.MediaSource,
		"video_codec": update.VideoCodec, "audio_codec": update.AudioCodec,
	} {
		if value != nil && len(*value) > 16<<10 {
			return nil, fmt.Errorf("%s exceeds field limit", name)
		}
	}
	item, err := s.store.UpdateSubmission(ctx, strings.TrimSpace(id), update)
	if err != nil {
		return nil, err
	}
	if !revealPassword {
		item.Password = ""
	}
	return item, nil
}

func (s *Service) Transition(ctx context.Context, id string, next State, actor, note string, revealPassword bool) (*Submission, error) {
	switch next {
	case StatePendingReview, StateApproved, StateRejected:
	default:
		return nil, fmt.Errorf("unsupported uploader state %q", next)
	}
	item, err := s.store.TransitionSubmission(ctx, strings.TrimSpace(id), next, strings.TrimSpace(actor), strings.TrimSpace(note))
	if err != nil {
		return nil, err
	}
	if !revealPassword {
		item.Password = ""
	}
	return item, nil
}

func (s *Service) Events(ctx context.Context, id string) ([]Event, error) {
	return s.store.ListEvents(ctx, strings.TrimSpace(id))
}

func (s *Service) OpenNZB(ctx context.Context, id string, requireApproved bool) (io.ReadCloser, error) {
	item, err := s.store.GetSubmission(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if requireApproved && item.State != StateApproved {
		return nil, ErrNotFound
	}
	return s.store.OpenNZB(ctx, item.ID)
}

func (s *Service) Store() Store { return s.store }

func (s *Service) OpenArtifact(ctx context.Context, submissionID, artifactID string) (*Artifact, io.ReadCloser, error) {
	return s.store.OpenArtifact(ctx, strings.TrimSpace(submissionID), strings.TrimSpace(artifactID))
}

func (s *Service) buildArtifacts(inputs []ArtifactInput, descriptors []ArtifactDescriptor) ([]Artifact, error) {
	if len(inputs) == 0 && len(descriptors) == 0 {
		return nil, nil
	}
	if len(inputs) != len(descriptors) {
		return nil, fmt.Errorf("each artifact must have exactly one metadata descriptor")
	}
	descriptorByName := make(map[string]ArtifactDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		name := strings.TrimSpace(descriptor.Filename)
		if name == "" || filepath.Base(name) != name || name == "." {
			return nil, fmt.Errorf("artifact descriptor filename is invalid")
		}
		if !validArtifactKind(descriptor.Kind) {
			return nil, fmt.Errorf("artifact kind %q is invalid", descriptor.Kind)
		}
		if len(descriptor.Label) > 4096 {
			return nil, fmt.Errorf("artifact label exceeds field limit")
		}
		if _, exists := descriptorByName[name]; exists {
			return nil, fmt.Errorf("artifact descriptor filename %q is duplicated", name)
		}
		descriptorByName[name] = descriptor
	}
	now := s.now().UTC()
	total := int64(0)
	artifacts := make([]Artifact, 0, len(inputs))
	for order, input := range inputs {
		name := strings.TrimSpace(input.Filename)
		if name == "" || filepath.Base(name) != name || name == "." {
			return nil, fmt.Errorf("artifact filename is invalid")
		}
		descriptor, ok := descriptorByName[name]
		if !ok {
			return nil, fmt.Errorf("artifact %q has no metadata descriptor", name)
		}
		if int64(len(input.Payload)) > s.maxArtifactBytes {
			return nil, fmt.Errorf("artifact %q exceeds %d byte limit", name, s.maxArtifactBytes)
		}
		if total > s.maxSubmissionBytes-int64(len(input.Payload)) {
			return nil, fmt.Errorf("submission artifacts exceed %d byte limit", s.maxSubmissionBytes)
		}
		total += int64(len(input.Payload))
		sum := sha256.Sum256(input.Payload)
		id := ksuid.New().String()
		artifacts = append(artifacts, Artifact{
			ID: id, Kind: descriptor.Kind, OriginalFilename: name, Label: strings.TrimSpace(descriptor.Label),
			DeclaredMediaType: strings.TrimSpace(input.DeclaredMediaType), DetectedMediaType: http.DetectContentType(input.Payload),
			SizeBytes: int64(len(input.Payload)), SHA256: hex.EncodeToString(sum[:]), DisplayOrder: order,
			BlobKey: filepath.ToSlash(filepath.Join("artifacts", id)), Payload: input.Payload, CreatedAt: now,
		})
	}
	return artifacts, nil
}

func validArtifactKind(kind ArtifactKind) bool {
	switch kind {
	case ArtifactNFO, ArtifactScreenshot, ArtifactSample, ArtifactSubtitle, ArtifactMetadata, ArtifactOther:
		return true
	default:
		return false
	}
}

func validateMetadata(metadata Metadata) error {
	if metadata.CategoryID != 0 {
		if _, ok := newsnab.Lookup(metadata.CategoryID); !ok {
			return fmt.Errorf("metadata.category_id is invalid")
		}
	}
	if metadata.Media.Year < 0 || metadata.Media.Year > 9999 {
		return fmt.Errorf("metadata.media.year is invalid")
	}
	if metadata.ExternalIDs.TMDBID < 0 || metadata.ExternalIDs.TVDBID < 0 {
		return fmt.Errorf("metadata external IDs must not be negative")
	}
	if strings.TrimSpace(metadata.Provenance.ExternalID) != "" && strings.TrimSpace(metadata.Provenance.Tool) == "" {
		return fmt.Errorf("metadata.provenance.tool is required with external_id")
	}
	for name, value := range map[string]string{
		"title": metadata.Title, "password": metadata.Password,
		"tool": metadata.Provenance.Tool, "version": metadata.Provenance.Version,
		"external_id": metadata.Provenance.ExternalID, "imdb_id": metadata.ExternalIDs.IMDBID,
		"resolution": metadata.Media.Resolution, "source": metadata.Media.Source,
		"video_codec": metadata.Media.VideoCodec, "audio_codec": metadata.Media.AudioCodec,
	} {
		if len(value) > 16<<10 {
			return fmt.Errorf("metadata.%s exceeds field limit", name)
		}
	}
	return nil
}

func filenameTitle(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	ext := filepath.Ext(name)
	if strings.EqualFold(ext, ".nzb") {
		name = strings.TrimSuffix(name, ext)
	}
	return strings.TrimSpace(name)
}

func NormalizeTitle(value string) string {
	var out strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			out.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(out.String())
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
