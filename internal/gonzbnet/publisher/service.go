package publisher

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/health"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifest"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/gonzbnet/validation"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type Identity interface {
	events.Identity
	PublicKey(context.Context) (ed25519.PublicKey, error)
}

type Store interface {
	ListGoNZBNetLocalReleaseCandidates(ctx context.Context, limit int) ([]releasecard.LocalRelease, error)
	UpsertFederationNodeIdentity(ctx context.Context, nodeID string, publicKey ed25519.PublicKey) error
	NextFederationEventSequence(ctx context.Context, authorNodeID string) (int64, *string, error)
	FindFederationEventByBodyHash(ctx context.Context, authorNodeID, eventType, bodyHash string) (string, error)
	AppendVerifiedFederationEvent(ctx context.Context, event *events.SignedEvent, validation *events.ValidationResult) error
	UpsertFederatedReleaseCardProjection(ctx context.Context, projection releasecard.Projection) error
	ProjectHealthAttestation(ctx context.Context, projection pgindex.HealthAttestationProjection) error
	ClaimValidationTasks(ctx context.Context, nodeID string, limit int) ([]pgindex.ValidationTask, error)
	GetResolutionManifest(ctx context.Context, manifestID string) (*manifest.ResolutionManifest, error)
	ProjectValidatorCapacity(ctx context.Context, projection pgindex.ValidatorCapacityProjection) error
	ProjectArticleAvailabilityAttestation(ctx context.Context, projection pgindex.ArticleAvailabilityProjection) error
	ProjectChecksumAttestation(ctx context.Context, projection pgindex.ChecksumAttestationProjection) error
	CompleteValidationTask(ctx context.Context, taskID int64, status, message string) error
}

type Service struct {
	identity Identity
	store    Store
	poolID   string
	now      func() time.Time
}

type Result struct {
	Scanned   int
	Published int
	Skipped   int
	Projected int
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
		identity: identity,
		store:    store,
		poolID:   poolID,
		now:      time.Now,
	}
}

func (s *Service) PublishOnce(ctx context.Context, limit int) (Result, error) {
	var result Result
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

	candidates, err := s.store.ListGoNZBNetLocalReleaseCandidates(ctx, limit)
	if err != nil {
		return result, err
	}
	result.Scanned = len(candidates)

	for _, candidate := range candidates {
		card, err := releasecard.MapLocalRelease(candidate)
		if err != nil {
			return result, fmt.Errorf("map release %s: %w", candidate.LocalReleaseID, err)
		}
		bodyHash, err := releasecard.HashBody(card)
		if err != nil {
			return result, fmt.Errorf("hash release card %s: %w", card.ReleaseID, err)
		}

		existingEventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, pools.EventTypeReleaseCard, bodyHash)
		if err != nil {
			return result, err
		}
		if existingEventID != "" {
			result.Skipped++
			if err := s.store.UpsertFederatedReleaseCardProjection(ctx, releasecard.Projection{
				Card:         card,
				EventID:      existingEventID,
				SourceNodeID: nodeID,
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
			EventType:       pools.EventTypeReleaseCard,
			Sequence:        sequence,
			PreviousEventID: previousEventID,
			CreatedAt:       s.now().UTC(),
			PoolIDs:         []string{s.poolID},
			Visibility:      "pool",
			BodySchema:      releasecard.BodySchema,
			Body:            card,
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
		result.Published++
		if err := s.store.UpsertFederatedReleaseCardProjection(ctx, releasecard.Projection{
			Card:         card,
			EventID:      event.EventID,
			SourceNodeID: nodeID,
			PoolID:       s.poolID,
		}); err != nil {
			return result, err
		}
		result.Projected++
	}

	return result, nil
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
	tasks, err := s.store.ClaimValidationTasks(ctx, nodeID, limit)
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
	candidates, err := s.store.ListGoNZBNetLocalReleaseCandidates(ctx, limit)
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
		existingEventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, pools.EventTypeHealthAttestation, bodyHash)
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
	}
	bodyHash, err := validation.HashBody(capacity)
	if err != nil {
		return false, err
	}
	existingEventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, pools.EventTypeValidatorCapacity, bodyHash)
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
	existingEventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, pools.EventTypeArticleAvailabilityAttestation, bodyHash)
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

func (s *Service) publishChecksumAttestation(ctx context.Context, nodeID string, attestation validation.ChecksumAttestation, poolID string) (string, bool, error) {
	bodyHash, err := validation.HashBody(attestation)
	if err != nil {
		return "", false, err
	}
	existingEventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, pools.EventTypeChecksumAttestation, bodyHash)
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
