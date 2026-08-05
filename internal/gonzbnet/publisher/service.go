package publisher

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/health"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifest"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifestavailability"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/publicationstate"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/gonzbnet/validation"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type Identity interface {
	events.Identity
	PublicKey(context.Context) (ed25519.PublicKey, error)
}

type Store interface {
	ListGoNZBNetScanOutputCandidates(ctx context.Context, poolID string, requireSignedManifest bool, limit int) ([]releasecard.LocalRelease, error)
	ListGoNZBNetLocalReleaseCandidates(ctx context.Context, limit int, policy pgindex.ReleaseReadyPolicy) ([]releasecard.LocalRelease, error)
	MarkGoNZBNetScanOutputPublished(ctx context.Context, scanID, eventID, poolID string) error
	UpsertFederationNodeIdentity(ctx context.Context, nodeID string, publicKey ed25519.PublicKey) error
	NextFederationEventSequence(ctx context.Context, authorNodeID string) (int64, *string, error)
	FindFederationEventByBodyHash(ctx context.Context, authorNodeID, eventType, bodyHash, poolID string) (string, error)
	AppendVerifiedFederationEvent(ctx context.Context, event *events.SignedEvent, validation *events.ValidationResult) error
	UpsertFederatedReleaseCardProjection(ctx context.Context, projection releasecard.Projection) error
	ProjectReleasePublicationState(ctx context.Context, projection publicationstate.Projection) error
	StoreResolutionManifest(ctx context.Context, record pgindex.ResolutionManifestRecord) error
	ProjectManifestAvailability(ctx context.Context, projection pgindex.ManifestAvailabilityProjection) error
	ProjectHealthAttestation(ctx context.Context, projection pgindex.HealthAttestationProjection) error
	ClaimValidationTasks(ctx context.Context, nodeID, poolID string, limit int) ([]pgindex.ValidationTask, error)
	GetResolutionManifest(ctx context.Context, manifestID string) (*manifest.ResolutionManifest, error)
	ProjectValidatorCapacity(ctx context.Context, projection pgindex.ValidatorCapacityProjection) error
	ProjectArticleAvailabilityAttestation(ctx context.Context, projection pgindex.ArticleAvailabilityProjection) error
	ProjectChecksumAttestation(ctx context.Context, projection pgindex.ChecksumAttestationProjection) error
	CompleteValidationTask(ctx context.Context, taskID int64, status, message string) error
}

type transactionalProjectionStore interface {
	AppendVerifiedFederationEventWithProjection(context.Context, *events.SignedEvent, *events.ValidationResult, func(context.Context) error) error
}

type Service struct {
	identity                    Identity
	store                       Store
	poolID                      string
	now                         func() time.Time
	publishManifestAvailability bool
	buildManifests              bool
	articleChecker              func(context.Context, string, []string) error
	releaseReadyPolicy          pgindex.ReleaseReadyPolicy
}

type Result struct {
	Scanned   int
	Published int
	Skipped   int
	Projected int
}

// CandidateResult identifies the durable events created (or reused) while
// publishing one caller-supplied release into this service's pool.
type CandidateResult struct {
	Card               releasecard.ReleaseCard
	ReleaseCardEventID string
	ManifestEventID    string
	Published          bool
}

type PublicationStateResult struct {
	EventID string
	State   publicationstate.State
}

type HealthResult struct {
	Scanned   int
	Published int
	Skipped   int
	Projected int
}

type ValidationOptions struct {
	ChecksumEnabled bool
	MaxTasksPerHour int
}

type ValidationResult struct {
	Claimed           int
	CapacityPublished int
	Published         int
	Skipped           int
	Projected         int
	Failed            int
}

func New(identity Identity, store Store, poolID string) *Service {
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		poolID = "pool.local"
	}
	return &Service{
		identity:           identity,
		store:              store,
		poolID:             poolID,
		now:                time.Now,
		releaseReadyPolicy: pgindex.DefaultReleaseReadyPolicy(),
	}
}

func (s *Service) SetReleaseReadyPolicy(policy pgindex.ReleaseReadyPolicy) {
	if s != nil {
		s.releaseReadyPolicy = pgindex.NormalizeReleaseReadyPolicy(policy)
	}
}

func (s *Service) SetManifestAvailabilityPublishing(enabled bool) {
	if s != nil {
		s.publishManifestAvailability = enabled
	}
}

func (s *Service) SetManifestBuilding(enabled bool) {
	if s != nil {
		s.buildManifests = enabled
	}
}

func (s *Service) SetArticleChecker(checker func(context.Context, string, []string) error) {
	if s != nil {
		s.articleChecker = checker
	}
}

func (s *Service) PublishOnce(ctx context.Context, limit int) (Result, error) {
	var result Result
	nodeID, err := s.prepare(ctx)
	if err != nil {
		return result, err
	}

	scanCandidates, err := s.store.ListGoNZBNetScanOutputCandidates(ctx, s.poolID, s.buildManifests, limit)
	if err != nil {
		return result, err
	}
	remaining := limit - len(scanCandidates)
	if remaining < 0 {
		remaining = 0
	}
	indexerCandidates, err := s.store.ListGoNZBNetLocalReleaseCandidates(ctx, remaining, s.releaseReadyPolicy)
	if err != nil {
		return result, err
	}
	candidates := append(scanCandidates, indexerCandidates...)
	result.Scanned = len(candidates)

	for _, candidate := range candidates {
		published, err := s.publishCandidate(ctx, nodeID, candidate)
		if err != nil {
			return result, err
		}
		if published.Published {
			result.Published++
		} else {
			result.Skipped++
		}
		result.Projected++
	}

	return result, nil
}

// PublishCandidate publishes a supplied release without requiring it to be a
// PostgreSQL indexer candidate. This is the explicit uploader integration
// boundary and does not inspect torrents, magnets, or source files.
func (s *Service) PublishCandidate(ctx context.Context, candidate releasecard.LocalRelease) (CandidateResult, error) {
	nodeID, err := s.prepare(ctx)
	if err != nil {
		return CandidateResult{}, err
	}
	return s.publishCandidate(ctx, nodeID, candidate)
}

// PublishReleaseState emits an author-scoped withdrawal or restoration event
// and applies it locally before federation delivery.
func (s *Service) PublishReleaseState(ctx context.Context, releaseID, manifestID, state, supersedesEventID, reason string) (PublicationStateResult, error) {
	nodeID, err := s.prepare(ctx)
	if err != nil {
		return PublicationStateResult{}, err
	}
	body := publicationstate.State{
		SchemaVersion: "1.0", Type: publicationstate.Type, PoolID: s.poolID,
		ReleaseID: strings.TrimSpace(releaseID), ManifestID: strings.TrimSpace(manifestID),
		State: strings.TrimSpace(state), Reason: strings.TrimSpace(reason),
		ChangedAt: s.now().UTC().Format(time.RFC3339), SupersedesEventID: strings.TrimSpace(supersedesEventID),
	}
	if err := publicationstate.Validate(body, s.now().UTC(), 0); err != nil {
		return PublicationStateResult{}, err
	}
	sequence, previousEventID, err := s.store.NextFederationEventSequence(ctx, nodeID)
	if err != nil {
		return PublicationStateResult{}, err
	}
	event, validation, err := events.Create(ctx, s.identity, events.CreateOptions{
		EventType: publicationstate.Type, Sequence: sequence, PreviousEventID: previousEventID,
		CreatedAt: s.now().UTC(), PoolIDs: []string{s.poolID}, Visibility: "pool",
		BodySchema: publicationstate.BodySchema, Body: body,
	})
	if err != nil {
		return PublicationStateResult{}, err
	}
	if validation == nil || !validation.OK {
		return PublicationStateResult{}, fmt.Errorf("signed release publication state did not verify: %s", validationReason(validation))
	}
	projection := publicationstate.Projection{
		Publication: body, EventID: event.EventID, AuthorNodeID: nodeID, Sequence: event.Sequence,
	}
	if store, ok := s.store.(transactionalProjectionStore); ok {
		if err := store.AppendVerifiedFederationEventWithProjection(ctx, event, validation, func(projectCtx context.Context) error {
			return s.store.ProjectReleasePublicationState(projectCtx, projection)
		}); err != nil {
			return PublicationStateResult{}, err
		}
	} else {
		if err := s.store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
			return PublicationStateResult{}, err
		}
		if err := s.store.ProjectReleasePublicationState(ctx, projection); err != nil {
			return PublicationStateResult{}, err
		}
	}
	return PublicationStateResult{EventID: event.EventID, State: body}, nil
}

func (s *Service) prepare(ctx context.Context) (string, error) {
	if s == nil || s.identity == nil || s.store == nil {
		return "", fmt.Errorf("publisher dependencies are required")
	}
	nodeID, err := s.identity.NodeID(ctx)
	if err != nil {
		return "", err
	}
	publicKey, err := s.identity.PublicKey(ctx)
	if err != nil {
		return "", err
	}
	if err := s.store.UpsertFederationNodeIdentity(ctx, nodeID, publicKey); err != nil {
		return "", err
	}
	return nodeID, nil
}

func (s *Service) publishCandidate(ctx context.Context, nodeID string, candidate releasecard.LocalRelease) (CandidateResult, error) {
	card, err := releasecard.MapLocalRelease(candidate)
	if err != nil {
		return CandidateResult{}, fmt.Errorf("map release %s: %w", candidate.LocalReleaseID, err)
	}
	result := CandidateResult{Card: card}
	bodyHash, err := releasecard.HashBody(card)
	if err != nil {
		return result, fmt.Errorf("hash release card %s: %w", card.ReleaseID, err)
	}

	eventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, pools.EventTypeReleaseCard, bodyHash, s.poolID)
	if err != nil {
		return result, err
	}
	if eventID == "" {
		sequence, previousEventID, err := s.store.NextFederationEventSequence(ctx, nodeID)
		if err != nil {
			return result, err
		}
		event, validation, err := events.Create(ctx, s.identity, events.CreateOptions{
			EventType: pools.EventTypeReleaseCard, Sequence: sequence, PreviousEventID: previousEventID,
			CreatedAt: s.now().UTC(), PoolIDs: []string{s.poolID}, Visibility: "pool",
			BodySchema: releasecard.BodySchema, Body: card,
		})
		if err != nil {
			return result, fmt.Errorf("sign release card %s: %w", card.ReleaseID, err)
		}
		if validation == nil || !validation.OK {
			return result, fmt.Errorf("signed release card %s did not verify: %s", card.ReleaseID, validationReason(validation))
		}
		if err := s.store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
			return result, err
		}
		eventID = event.EventID
		result.Published = true
	}
	result.ReleaseCardEventID = eventID
	if err := s.store.UpsertFederatedReleaseCardProjection(ctx, releasecard.Projection{
		Card: card, EventID: eventID, SourceNodeID: nodeID, PoolID: s.poolID,
	}); err != nil {
		return result, err
	}
	if candidate.SourceKind == "local_scan_output" {
		if err := s.store.MarkGoNZBNetScanOutputPublished(ctx, candidate.LocalReleaseID, eventID, s.poolID); err != nil {
			return result, err
		}
	}
	if s.buildManifests && strings.TrimSpace(card.ManifestID) != "" {
		result.ManifestEventID, err = s.buildAndStoreManifest(ctx, candidate, card, nodeID)
		if err != nil {
			return result, err
		}
	}
	if result.Published && s.publishManifestAvailability && strings.TrimSpace(card.ManifestID) != "" {
		if err := s.publishManifestAvailabilityOnce(ctx, nodeID, card); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (s *Service) buildAndStoreManifest(ctx context.Context, candidate releasecard.LocalRelease, card releasecard.ReleaseCard, nodeID string) (string, error) {
	item, canonicalCore, generatedNZB, err := BuildLocalManifest(candidate)
	if err != nil {
		return "", fmt.Errorf("build local manifest for %s: %w", card.ReleaseID, err)
	}
	if item.ManifestID != card.ManifestID {
		return "", fmt.Errorf("manifest ID mismatch for release %s: card=%s built=%s", card.ReleaseID, card.ManifestID, item.ManifestID)
	}
	item.ReleaseID = card.ReleaseID
	bodyHash, _, err := canonical.BodyHash(item)
	if err != nil {
		return "", fmt.Errorf("hash local manifest for %s: %w", card.ReleaseID, err)
	}
	manifestEventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, manifest.Type, bodyHash, s.poolID)
	if err != nil {
		return "", err
	}
	if manifestEventID == "" {
		sequence, previousEventID, err := s.store.NextFederationEventSequence(ctx, nodeID)
		if err != nil {
			return "", err
		}
		event, validation, err := events.Create(ctx, s.identity, events.CreateOptions{
			EventType:       manifest.Type,
			Sequence:        sequence,
			PreviousEventID: previousEventID,
			CreatedAt:       s.now().UTC(),
			PoolIDs:         []string{s.poolID},
			Visibility:      "pool",
			BodySchema:      manifest.BodySchema,
			Body:            item,
		})
		if err != nil {
			return "", fmt.Errorf("sign local manifest for %s: %w", card.ReleaseID, err)
		}
		if err := s.store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
			return "", err
		}
		manifestEventID = event.EventID
	}
	err = s.store.StoreResolutionManifest(ctx, pgindex.ResolutionManifestRecord{
		Manifest:              item,
		SourceNodeID:          nodeID,
		SourceEventID:         manifestEventID,
		PoolID:                s.poolID,
		CanonicalManifestJSON: canonicalCore,
		GeneratedNZB:          generatedNZB,
	})
	return manifestEventID, err
}

func BuildLocalManifest(candidate releasecard.LocalRelease) (manifest.ResolutionManifest, []byte, []byte, error) {
	core, err := releasecard.ManifestCoreForLocalRelease(candidate)
	if err != nil {
		return manifest.ResolutionManifest{}, nil, nil, err
	}
	if len(core.Files) == 0 {
		return manifest.ResolutionManifest{}, nil, nil, fmt.Errorf("complete file segments are required")
	}
	manifestID, canonicalCore, err := manifest.ComputeID(core)
	if err != nil {
		return manifest.ResolutionManifest{}, nil, nil, err
	}
	item := manifest.ResolutionManifest{
		SchemaVersion: "1.0",
		Type:          manifest.Type,
		ManifestID:    manifestID,
		ReleaseID:     firstNonBlank(candidate.LocalReleaseID, candidate.GUID),
		ManifestCore:  core,
		Compression:   "none",
		Encrypted:     false,
	}
	if item.ReleaseID == "" {
		return manifest.ResolutionManifest{}, nil, nil, fmt.Errorf("release ID is required")
	}
	generatedNZB, err := manifest.GenerateNZB(item)
	if err != nil {
		return manifest.ResolutionManifest{}, nil, nil, err
	}
	return item, canonicalCore, generatedNZB, nil
}

func (s *Service) PublishValidationOnce(ctx context.Context, limit int, opts ValidationOptions) (ValidationResult, error) {
	var result ValidationResult
	if s == nil || s.identity == nil || s.store == nil {
		return result, fmt.Errorf("publisher dependencies are required")
	}
	nodeID, err := s.identity.NodeID(ctx)
	if err != nil {
		return result, err
	}
	publicKey, err := s.identity.PublicKey(ctx)
	if err != nil {
		return result, err
	}
	if err := s.store.UpsertFederationNodeIdentity(ctx, nodeID, publicKey); err != nil {
		return result, err
	}
	capacityPublished, err := s.publishValidatorCapacity(ctx, nodeID, opts)
	if err != nil {
		return result, err
	}
	if capacityPublished {
		result.CapacityPublished = 1
	}
	tasks, err := s.store.ClaimValidationTasks(ctx, nodeID, s.poolID, limit)
	if err != nil {
		return result, err
	}
	result.Claimed = len(tasks)
	for _, task := range tasks {
		manifestBody, err := s.store.GetResolutionManifest(ctx, task.ManifestID)
		if err != nil {
			result.Failed++
			_ = s.store.CompleteValidationTask(ctx, task.TaskID, "failed", err.Error())
			continue
		}
		if manifestBody == nil {
			result.Failed++
			_ = s.store.CompleteValidationTask(ctx, task.TaskID, "failed", "manifest not found")
			continue
		}
		availability := articleAvailabilityFromManifest(*manifestBody, s.now().UTC())
		if s.articleChecker != nil {
			availability = s.articleAvailabilityFromNNTP(ctx, *manifestBody)
		}
		eventID, published, err := s.publishArticleAvailability(ctx, nodeID, availability, firstNonBlank(task.PoolID, s.poolID))
		if err != nil {
			result.Failed++
			_ = s.store.CompleteValidationTask(ctx, task.TaskID, "failed", err.Error())
			continue
		}
		if published {
			result.Published++
		} else {
			result.Skipped++
		}
		if err := s.store.ProjectArticleAvailabilityAttestation(ctx, pgindex.ArticleAvailabilityProjection{
			Attestation:  availability,
			EventID:      eventID,
			AuthorNodeID: nodeID,
			PoolID:       firstNonBlank(task.PoolID, s.poolID),
		}); err != nil {
			result.Failed++
			_ = s.store.CompleteValidationTask(ctx, task.TaskID, "failed", err.Error())
			continue
		}
		result.Projected++
		if opts.ChecksumEnabled {
			checksum := checksumAttestationFromManifest(*manifestBody, s.now().UTC())
			eventID, published, err := s.publishChecksumAttestation(ctx, nodeID, checksum, firstNonBlank(task.PoolID, s.poolID))
			if err != nil {
				result.Failed++
				_ = s.store.CompleteValidationTask(ctx, task.TaskID, "failed", err.Error())
				continue
			}
			if published {
				result.Published++
			} else {
				result.Skipped++
			}
			if err := s.store.ProjectChecksumAttestation(ctx, pgindex.ChecksumAttestationProjection{
				Attestation:  checksum,
				EventID:      eventID,
				AuthorNodeID: nodeID,
				PoolID:       firstNonBlank(task.PoolID, s.poolID),
			}); err != nil {
				result.Failed++
				_ = s.store.CompleteValidationTask(ctx, task.TaskID, "failed", err.Error())
				continue
			}
			result.Projected++
		}
		_ = s.store.CompleteValidationTask(ctx, task.TaskID, "completed", "")
	}
	return result, nil
}

func (s *Service) articleAvailabilityFromNNTP(ctx context.Context, item manifest.ResolutionManifest) validation.ArticleAvailabilityAttestation {
	checkedAt := s.now().UTC()
	total := 0
	available := 0
	missing := 0
	for _, file := range item.ManifestCore.Files {
		for _, segment := range file.Segments {
			total++
			groups := append([]string(nil), item.ManifestCore.Groups...)
			if err := s.articleChecker(ctx, segment.MessageID, groups); err != nil {
				missing++
				continue
			}
			available++
		}
	}
	status := validation.StatusAvailable
	if available == 0 {
		status = validation.StatusMissing
	} else if missing > 0 {
		status = validation.StatusPartial
	}
	confidence := 1.0
	if total == 0 {
		confidence = 0
	}
	return validation.ArticleAvailabilityAttestation{
		SchemaVersion: "1.0", Type: validation.TypeArticleAvailabilityAttestation,
		ReleaseID: item.ReleaseID, ManifestID: item.ManifestID,
		CheckedAt: checkedAt.Format(time.RFC3339), Status: status,
		ArticlesTotal: total, ArticlesAvailable: available, MissingArticles: missing,
		Confidence: confidence, Method: "nntp_fetch_body_prefix",
	}
}

func (s *Service) PublishHealthOnce(ctx context.Context, limit int) (HealthResult, error) {
	var result HealthResult
	if s == nil || s.identity == nil || s.store == nil {
		return result, fmt.Errorf("publisher dependencies are required")
	}
	nodeID, err := s.identity.NodeID(ctx)
	if err != nil {
		return result, err
	}
	publicKey, err := s.identity.PublicKey(ctx)
	if err != nil {
		return result, err
	}
	if err := s.store.UpsertFederationNodeIdentity(ctx, nodeID, publicKey); err != nil {
		return result, err
	}
	candidates, err := s.store.ListGoNZBNetLocalReleaseCandidates(ctx, limit, s.releaseReadyPolicy)
	if err != nil {
		return result, err
	}
	result.Scanned = len(candidates)
	for _, candidate := range candidates {
		card, err := releasecard.MapLocalRelease(candidate)
		if err != nil {
			return result, fmt.Errorf("map release %s: %w", candidate.LocalReleaseID, err)
		}
		attestation := healthAttestationFromLocalRelease(card, candidate, s.now().UTC())
		bodyHash, err := health.HashBody(attestation)
		if err != nil {
			return result, fmt.Errorf("hash health attestation %s: %w", attestation.ReleaseID, err)
		}
		existingEventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, pools.EventTypeHealthAttestation, bodyHash, s.poolID)
		if err != nil {
			return result, err
		}
		if existingEventID != "" {
			result.Skipped++
			if err := s.store.ProjectHealthAttestation(ctx, pgindex.HealthAttestationProjection{
				Attestation:  attestation,
				EventID:      existingEventID,
				AuthorNodeID: nodeID,
				PoolID:       s.poolID,
			}); err != nil {
				return result, err
			}
			result.Projected++
			continue
		}
		sequence, previousEventID, err := s.store.NextFederationEventSequence(ctx, nodeID)
		if err != nil {
			return result, err
		}
		event, validation, err := events.Create(ctx, s.identity, events.CreateOptions{
			EventType:       pools.EventTypeHealthAttestation,
			Sequence:        sequence,
			PreviousEventID: previousEventID,
			CreatedAt:       s.now().UTC(),
			PoolIDs:         []string{s.poolID},
			Visibility:      "pool",
			BodySchema:      health.BodySchema,
			Body:            attestation,
		})
		if err != nil {
			return result, fmt.Errorf("sign health attestation %s: %w", attestation.ReleaseID, err)
		}
		if validation == nil || !validation.OK {
			return result, fmt.Errorf("signed health attestation %s did not verify: %s", attestation.ReleaseID, validationReason(validation))
		}
		if err := s.store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
			return result, err
		}
		result.Published++
		if err := s.store.ProjectHealthAttestation(ctx, pgindex.HealthAttestationProjection{
			Attestation:  attestation,
			EventID:      event.EventID,
			AuthorNodeID: nodeID,
			PoolID:       s.poolID,
		}); err != nil {
			return result, err
		}
		result.Projected++
	}
	return result, nil
}

func (s *Service) Run(ctx context.Context, interval time.Duration, limit int) error {
	if interval <= 0 {
		return s.runUntilDone(ctx, limit)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.PublishOnce(ctx, limit); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) runUntilDone(ctx context.Context, limit int) error {
	_, err := s.PublishOnce(ctx, limit)
	return err
}

func (s *Service) RunHealth(ctx context.Context, interval time.Duration, limit int) error {
	if interval <= 0 {
		_, err := s.PublishHealthOnce(ctx, limit)
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.PublishHealthOnce(ctx, limit); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) RunValidation(ctx context.Context, interval time.Duration, limit int, opts ValidationOptions) error {
	if interval <= 0 {
		_, err := s.PublishValidationOnce(ctx, limit, opts)
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.PublishValidationOnce(ctx, limit, opts); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) publishValidatorCapacity(ctx context.Context, nodeID string, opts ValidationOptions) (bool, error) {
	capacity := validation.ValidatorCapacity{
		SchemaVersion:           "1.0",
		Type:                    validation.TypeValidatorCapacity,
		NodeID:                  nodeID,
		PublishedAt:             s.now().UTC().Format(time.RFC3339),
		MaxTasksPerHour:         opts.MaxTasksPerHour,
		ArticleAvailability:     true,
		ChecksumValidation:      opts.ChecksumEnabled,
		ProviderScope:           validation.ProviderScope{},
		AcceptedManifestSchemas: []string{manifest.BodySchema},
		ManifestFeatures:        []string{"manifest_archive_password"},
	}
	bodyHash, err := validation.HashBody(capacity)
	if err != nil {
		return false, err
	}
	existingEventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, pools.EventTypeValidatorCapacity, bodyHash, s.poolID)
	if err != nil {
		return false, err
	}
	eventID := existingEventID
	published := false
	if eventID == "" {
		sequence, previousEventID, err := s.store.NextFederationEventSequence(ctx, nodeID)
		if err != nil {
			return false, err
		}
		event, validationResult, err := events.Create(ctx, s.identity, events.CreateOptions{
			EventType:       pools.EventTypeValidatorCapacity,
			Sequence:        sequence,
			PreviousEventID: previousEventID,
			CreatedAt:       s.now().UTC(),
			PoolIDs:         []string{s.poolID},
			Visibility:      "pool",
			BodySchema:      validation.ValidatorCapacityBodySchema,
			Body:            capacity,
		})
		if err != nil {
			return false, err
		}
		if validationResult == nil || !validationResult.OK {
			return false, fmt.Errorf("signed validator capacity did not verify: %s", validationReason(validationResult))
		}
		if err := s.store.AppendVerifiedFederationEvent(ctx, event, validationResult); err != nil {
			return false, err
		}
		eventID = event.EventID
		published = true
	}
	if err := s.store.ProjectValidatorCapacity(ctx, pgindex.ValidatorCapacityProjection{
		Capacity:     capacity,
		EventID:      eventID,
		AuthorNodeID: nodeID,
	}); err != nil {
		return false, err
	}
	return published, nil
}

func (s *Service) publishArticleAvailability(ctx context.Context, nodeID string, attestation validation.ArticleAvailabilityAttestation, poolID string) (string, bool, error) {
	bodyHash, err := validation.HashBody(attestation)
	if err != nil {
		return "", false, err
	}
	existingEventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, pools.EventTypeArticleAvailabilityAttestation, bodyHash, poolID)
	if err != nil {
		return "", false, err
	}
	if existingEventID != "" {
		return existingEventID, false, nil
	}
	sequence, previousEventID, err := s.store.NextFederationEventSequence(ctx, nodeID)
	if err != nil {
		return "", false, err
	}
	event, validationResult, err := events.Create(ctx, s.identity, events.CreateOptions{
		EventType:       pools.EventTypeArticleAvailabilityAttestation,
		Sequence:        sequence,
		PreviousEventID: previousEventID,
		CreatedAt:       s.now().UTC(),
		PoolIDs:         []string{poolID},
		Visibility:      "pool",
		BodySchema:      validation.ArticleAvailabilityAttestationBodySchema,
		Body:            attestation,
	})
	if err != nil {
		return "", false, err
	}
	if validationResult == nil || !validationResult.OK {
		return "", false, fmt.Errorf("signed article availability attestation did not verify: %s", validationReason(validationResult))
	}
	if err := s.store.AppendVerifiedFederationEvent(ctx, event, validationResult); err != nil {
		return "", false, err
	}
	return event.EventID, true, nil
}

func (s *Service) publishManifestAvailabilityOnce(ctx context.Context, nodeID string, card releasecard.ReleaseCard) error {
	attestation := manifestavailability.Attestation{
		SchemaVersion:       "1.0",
		Type:                manifestavailability.Type,
		ReleaseID:           card.ReleaseID,
		ManifestID:          card.ManifestID,
		SourceNodeID:        nodeID,
		PoolID:              s.poolID,
		Available:           true,
		FetchPolicy:         firstNonBlank(card.Resolution.FetchPolicy, manifestavailability.FetchPolicyTrustedPeers),
		CompressedSizeBytes: card.Resolution.CompressedSizeBytes,
		UpdatedAt:           s.now().UTC().Format(time.RFC3339),
	}
	bodyHash, err := manifestavailability.HashBody(attestation)
	if err != nil {
		return err
	}
	existingEventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, pools.EventTypeManifestAvailability, bodyHash, s.poolID)
	if err != nil {
		return err
	}
	eventID := existingEventID
	if eventID == "" {
		sequence, previousEventID, err := s.store.NextFederationEventSequence(ctx, nodeID)
		if err != nil {
			return err
		}
		event, validationResult, err := events.Create(ctx, s.identity, events.CreateOptions{
			EventType:       pools.EventTypeManifestAvailability,
			Sequence:        sequence,
			PreviousEventID: previousEventID,
			CreatedAt:       s.now().UTC(),
			PoolIDs:         []string{s.poolID},
			Visibility:      "pool",
			BodySchema:      manifestavailability.BodySchema,
			Body:            attestation,
		})
		if err != nil {
			return err
		}
		if validationResult == nil || !validationResult.OK {
			return fmt.Errorf("signed manifest availability did not verify: %s", validationReason(validationResult))
		}
		if err := s.store.AppendVerifiedFederationEvent(ctx, event, validationResult); err != nil {
			return err
		}
		eventID = event.EventID
	}
	return s.store.ProjectManifestAvailability(ctx, pgindex.ManifestAvailabilityProjection{
		Attestation:  attestation,
		EventID:      eventID,
		AuthorNodeID: nodeID,
		PoolID:       s.poolID,
	})
}

func (s *Service) publishChecksumAttestation(ctx context.Context, nodeID string, attestation validation.ChecksumAttestation, poolID string) (string, bool, error) {
	bodyHash, err := validation.HashBody(attestation)
	if err != nil {
		return "", false, err
	}
	existingEventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, pools.EventTypeChecksumAttestation, bodyHash, poolID)
	if err != nil {
		return "", false, err
	}
	if existingEventID != "" {
		return existingEventID, false, nil
	}
	sequence, previousEventID, err := s.store.NextFederationEventSequence(ctx, nodeID)
	if err != nil {
		return "", false, err
	}
	event, validationResult, err := events.Create(ctx, s.identity, events.CreateOptions{
		EventType:       pools.EventTypeChecksumAttestation,
		Sequence:        sequence,
		PreviousEventID: previousEventID,
		CreatedAt:       s.now().UTC(),
		PoolIDs:         []string{poolID},
		Visibility:      "pool",
		BodySchema:      validation.ChecksumAttestationBodySchema,
		Body:            attestation,
	})
	if err != nil {
		return "", false, err
	}
	if validationResult == nil || !validationResult.OK {
		return "", false, fmt.Errorf("signed checksum attestation did not verify: %s", validationReason(validationResult))
	}
	if err := s.store.AppendVerifiedFederationEvent(ctx, event, validationResult); err != nil {
		return "", false, err
	}
	return event.EventID, true, nil
}

func healthAttestationFromLocalRelease(card releasecard.ReleaseCard, release releasecard.LocalRelease, checkedAt time.Time) health.Attestation {
	total, available := localArticleCounts(release.Files)
	missing := total - available
	if missing < 0 {
		missing = 0
	}
	status := health.StatusUnverified
	switch {
	case total > 0 && available >= total:
		status = health.StatusComplete
	case total > 0 && available == 0:
		status = health.StatusMissing
	case total > 0 && release.HasPAR2:
		status = health.StatusRepairable
	case total > 0:
		status = health.StatusIncomplete
	}
	confidence := release.Availability
	if confidence <= 0 && total > 0 {
		confidence = float64(available) / float64(total)
	}
	repairConfidence := 0.0
	if release.HasPAR2 {
		repairConfidence = 0.8
	}
	return health.Attestation{
		SchemaVersion:     "1.0",
		Type:              health.Type,
		ReleaseID:         card.ReleaseID,
		ManifestID:        card.ManifestID,
		CheckedAt:         checkedAt.UTC().Format(time.RFC3339),
		Status:            status,
		ArticlesTotal:     total,
		ArticlesAvailable: available,
		MissingArticles:   missing,
		RepairAvailable:   release.HasPAR2,
		RepairConfidence:  repairConfidence,
		ProviderScope:     health.ProviderScope{},
		Confidence:        clamp01(confidence),
		Method:            "local_indexer_projection",
	}
}

func articleAvailabilityFromManifest(item manifest.ResolutionManifest, checkedAt time.Time) validation.ArticleAvailabilityAttestation {
	total := 0
	for _, file := range item.ManifestCore.Files {
		total += len(file.Segments)
	}
	return validation.ArticleAvailabilityAttestation{
		SchemaVersion:     "1.0",
		Type:              validation.TypeArticleAvailabilityAttestation,
		ReleaseID:         item.ReleaseID,
		ManifestID:        item.ManifestID,
		CheckedAt:         checkedAt.UTC().Format(time.RFC3339),
		Status:            validation.StatusUnverified,
		ArticlesTotal:     total,
		ArticlesAvailable: 0,
		MissingArticles:   0,
		ProviderScope:     validation.ProviderScope{},
		Confidence:        0.2,
		Method:            "manifest_structure_validation",
	}
}

func checksumAttestationFromManifest(item manifest.ResolutionManifest, checkedAt time.Time) validation.ChecksumAttestation {
	total := 0
	if strings.TrimSpace(item.ManifestCore.Hashes.FileListHash) != "" {
		total++
	}
	if strings.TrimSpace(item.ManifestCore.Hashes.SegmentListHash) != "" {
		total++
	}
	return validation.ChecksumAttestation{
		SchemaVersion:     "1.0",
		Type:              validation.TypeChecksumAttestation,
		ReleaseID:         item.ReleaseID,
		ManifestID:        item.ManifestID,
		CheckedAt:         checkedAt.UTC().Format(time.RFC3339),
		Status:            validation.StatusSkipped,
		ChecksumsTotal:    total,
		ChecksumsVerified: 0,
		ChecksumsFailed:   0,
		Confidence:        0.1,
		Method:            "checksum_validation_disabled",
	}
}

func localArticleCounts(files []releasecard.LocalFile) (int, int) {
	total := 0
	available := 0
	for _, file := range files {
		fileTotal := file.TotalParts
		if fileTotal <= 0 {
			fileTotal = file.ArticleCount
		}
		if fileTotal <= 0 {
			fileTotal = len(file.Segments)
		}
		fileAvailable := file.ArticleCount
		if fileAvailable <= 0 {
			fileAvailable = len(file.Segments)
		}
		total += fileTotal
		available += fileAvailable
	}
	return total, available
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func validationReason(validation *events.ValidationResult) string {
	if validation == nil {
		return "missing validation"
	}
	return validation.Reason
}
