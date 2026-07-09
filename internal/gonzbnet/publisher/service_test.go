package publisher

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/health"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestPublishOnceSignsStoresAndSkipsUnchangedReleaseCards(t *testing.T) {
	ctx := context.Background()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	store := &fakeStore{
		candidates:       []releasecard.LocalRelease{testPublisherRelease()},
		eventsByBodyHash: make(map[string]string),
	}
	svc := New(node, store, "pool.local")
	svc.now = func() time.Time { return time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC) }

	first, err := svc.PublishOnce(ctx, 10)
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if first.Scanned != 1 || first.Published != 1 || first.Projected != 1 || first.Skipped != 0 {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if len(store.events) != 1 {
		t.Fatalf("expected one stored event, got %d", len(store.events))
	}
	if store.events[0].EventType != "ReleaseCard" {
		t.Fatalf("expected ReleaseCard event, got %q", store.events[0].EventType)
	}

	second, err := svc.PublishOnce(ctx, 10)
	if err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if second.Scanned != 1 || second.Published != 0 || second.Projected != 1 || second.Skipped != 1 {
		t.Fatalf("unexpected second result: %+v", second)
	}
	if len(store.events) != 1 {
		t.Fatalf("expected unchanged card to reuse stored event, got %d events", len(store.events))
	}
}

func TestPublishHealthOnceSignsCompleteAndIncompleteAttestations(t *testing.T) {
	ctx := context.Background()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	complete := testPublisherRelease()
	incomplete := testPublisherRelease()
	incomplete.LocalReleaseID = "release-2"
	incomplete.Title = "Example.Release.2026.720p.WEB-DL"
	incomplete.Files[0].ArticleCount = 1
	incomplete.Files[0].TotalParts = 3
	incomplete.Files[0].Segments = incomplete.Files[0].Segments[:1]

	store := &fakeStore{
		candidates:       []releasecard.LocalRelease{complete, incomplete},
		eventsByBodyHash: make(map[string]string),
	}
	svc := New(node, store, "pool.local")
	svc.now = func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) }

	result, err := svc.PublishHealthOnce(ctx, 10)
	if err != nil {
		t.Fatalf("publish health: %v", err)
	}
	if result.Scanned != 2 || result.Published != 2 || result.Projected != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(store.healthProjections) != 2 {
		t.Fatalf("expected two health projections, got %d", len(store.healthProjections))
	}
	if store.healthProjections[0].Attestation.Status != health.StatusComplete {
		t.Fatalf("expected complete status, got %q", store.healthProjections[0].Attestation.Status)
	}
	if store.healthProjections[1].Attestation.Status != health.StatusIncomplete {
		t.Fatalf("expected incomplete status, got %q", store.healthProjections[1].Attestation.Status)
	}
	for _, event := range store.events {
		if event.EventType != "HealthAttestation" {
			t.Fatalf("expected HealthAttestation event, got %q", event.EventType)
		}
	}
}

type fakeStore struct {
	candidates        []releasecard.LocalRelease
	events            []*events.SignedEvent
	eventsByBodyHash  map[string]string
	projections       []releasecard.Projection
	healthProjections []pgindex.HealthAttestationProjection
	nodeID            string
	publicKey         ed25519.PublicKey
}

func (s *fakeStore) ListGoNZBNetLocalReleaseCandidates(context.Context, int) ([]releasecard.LocalRelease, error) {
	return s.candidates, nil
}

func (s *fakeStore) UpsertFederationNodeIdentity(_ context.Context, nodeID string, publicKey ed25519.PublicKey) error {
	s.nodeID = nodeID
	s.publicKey = publicKey
	return nil
}

func (s *fakeStore) NextFederationEventSequence(context.Context, string) (int64, *string, error) {
	if len(s.events) == 0 {
		return 1, nil, nil
	}
	previous := s.events[len(s.events)-1].EventID
	return int64(len(s.events) + 1), &previous, nil
}

func (s *fakeStore) FindFederationEventByBodyHash(_ context.Context, _, _, bodyHash string) (string, error) {
	return s.eventsByBodyHash[bodyHash], nil
}

func (s *fakeStore) AppendVerifiedFederationEvent(_ context.Context, event *events.SignedEvent, validation *events.ValidationResult) error {
	s.events = append(s.events, event)
	s.eventsByBodyHash[event.BodyHash] = event.EventID
	return nil
}

func (s *fakeStore) UpsertFederatedReleaseCardProjection(_ context.Context, projection releasecard.Projection) error {
	s.projections = append(s.projections, projection)
	return nil
}

func (s *fakeStore) ProjectHealthAttestation(_ context.Context, projection pgindex.HealthAttestationProjection) error {
	s.healthProjections = append(s.healthProjections, projection)
	return nil
}

func testPublisherRelease() releasecard.LocalRelease {
	posted := time.Date(2026, 7, 7, 10, 55, 0, 0, time.UTC)
	return releasecard.LocalRelease{
		LocalReleaseID: "release-1",
		Title:          "Example.Release.2026.1080p.WEB-DL",
		Category:       "movies",
		CategoryID:     2040,
		SizeBytes:      1500,
		PostedAt:       &posted,
		FileCount:      1,
		Groups:         []string{"alt.binaries.example"},
		PasswordState:  "not_passworded",
		Availability:   0.9,
		Files: []releasecard.LocalFile{
			{
				Name:         "example.mkv",
				Subject:      "Example.Release.2026.1080p.WEB-DL example.mkv yEnc",
				PostedAt:     &posted,
				SizeBytes:    1500,
				FileIndex:    1,
				ArticleCount: 1,
				TotalParts:   1,
				Segments: []releasecard.LocalSegment{
					{Number: 1, Bytes: 1500, MessageID: "<seg1@example.invalid>"},
				},
			},
		},
	}
}
