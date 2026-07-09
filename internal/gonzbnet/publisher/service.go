package publisher

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
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

		existingEventID, err := s.store.FindFederationEventByBodyHash(ctx, nodeID, "ReleaseCard", bodyHash)
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
			EventType:       "ReleaseCard",
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

func validationReason(validation *events.ValidationResult) string {
	if validation == nil {
		return "missing validation"
	}
	return validation.Reason
}
