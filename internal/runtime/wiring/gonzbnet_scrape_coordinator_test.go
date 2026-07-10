package wiring

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/indexing/scrape"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestGoNZBNetScrapeCoordinatorPublishesClaimAndComplete(t *testing.T) {
	nodeIdentity, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store := &fakeScrapeCoordinatorStore{}
	coord, err := newGoNZBNetScrapeRangeCoordinator(nodeIdentity, store, "pool.test", time.Minute, 0.5, "scope-hash", true, true, true)
	if err != nil {
		t.Fatalf("coordinator: %v", err)
	}
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	coord.now = func() time.Time { return now }

	decision, err := coord.BeginScrapeRange(context.Background(), scrape.RangeRequest{
		Mode:         "latest",
		AssignmentID: "assignment-1",
		Group:        "alt.binaries.test",
		RangeStart:   10,
		RangeEnd:     20,
	})
	if err != nil {
		t.Fatalf("begin range: %v", err)
	}
	if decision.ClaimID == "" || decision.Skipped {
		t.Fatalf("expected local claim decision, got %+v", decision)
	}
	if err := coord.CompleteScrapeRange(context.Background(), decision, scrape.RangeResult{
		Mode:       "latest",
		Group:      "alt.binaries.test",
		RangeStart: 10,
		RangeEnd:   20,
	}); err != nil {
		t.Fatalf("complete range: %v", err)
	}

	if len(store.events) != 2 {
		t.Fatalf("expected claim and complete events, got %d", len(store.events))
	}
	if store.events[0].EventType != coverage.TypeRangeClaim || store.events[1].EventType != coverage.TypeRangeComplete {
		t.Fatalf("unexpected event types: %s, %s", store.events[0].EventType, store.events[1].EventType)
	}
	var claim coverage.RangeClaim
	if err := json.Unmarshal(store.events[0].Body, &claim); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claim.AssignmentID != "assignment-1" {
		t.Fatalf("expected assignment claim, got %+v", claim)
	}
	if store.projected != 2 {
		t.Fatalf("expected two projected events, got %d", store.projected)
	}
}

func TestGoNZBNetScrapeCoordinatorListsAssignedRanges(t *testing.T) {
	nodeIdentity, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store := &fakeScrapeCoordinatorStore{
		suggestions: []pgindex.CoverageWorkSuggestion{{
			Assignment: pgindex.CoverageAssignmentRecord{
				AssignmentID: "assignment-1",
				Group:        "alt.binaries.test",
				RangeStart:   10,
				RangeEnd:     20,
			},
		}},
	}
	coord, err := newGoNZBNetScrapeRangeCoordinator(nodeIdentity, store, "pool.test", time.Minute, 0.5, "scope-hash", true, false, true)
	if err != nil {
		t.Fatalf("coordinator: %v", err)
	}

	ranges, err := coord.AssignedScrapeRanges(context.Background(), "latest", 5)
	if err != nil {
		t.Fatalf("assigned ranges: %v", err)
	}
	if len(ranges) != 1 {
		t.Fatalf("expected one assigned range, got %+v", ranges)
	}
	if ranges[0].AssignmentID != "assignment-1" || ranges[0].Group != "alt.binaries.test" || ranges[0].RangeStart != 10 || ranges[0].RangeEnd != 20 {
		t.Fatalf("unexpected assigned range: %+v", ranges[0])
	}
	if store.lastSuggestionParams.NodeID == "" || store.lastSuggestionParams.PoolID != "pool.test" || !store.lastSuggestionParams.RequireArticleRange {
		t.Fatalf("expected node-scoped suggestion params, got %+v", store.lastSuggestionParams)
	}
}

func TestGoNZBNetScrapeCoordinatorSkipsBlockedRange(t *testing.T) {
	nodeIdentity, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store := &fakeScrapeCoordinatorStore{
		block: pgindex.CoverageRangeBlock{
			Blocked:           true,
			Reason:            "completed_range",
			AdvanceCheckpoint: true,
		},
	}
	coord, err := newGoNZBNetScrapeRangeCoordinator(nodeIdentity, store, "pool.test", time.Minute, 0.5, "scope-hash", true, true, true)
	if err != nil {
		t.Fatalf("coordinator: %v", err)
	}

	decision, err := coord.BeginScrapeRange(context.Background(), scrape.RangeRequest{
		Group:      "alt.binaries.test",
		RangeStart: 10,
		RangeEnd:   20,
	})
	if err != nil {
		t.Fatalf("begin range: %v", err)
	}
	if !decision.Skipped || !decision.AdvanceCheckpoint || decision.Reason != "completed_range" {
		t.Fatalf("expected completed-range skip, got %+v", decision)
	}
	if len(store.events) != 0 {
		t.Fatalf("expected skipped range not to publish local claim, got %d events", len(store.events))
	}
	if store.lastBlockParams.ProviderBackboneHash != "scope-hash" || !store.lastBlockParams.RequireProviderScope {
		t.Fatalf("expected provider scope block params, got %+v", store.lastBlockParams)
	}
}

type fakeScrapeCoordinatorStore struct {
	block                pgindex.CoverageRangeBlock
	lastBlockParams      pgindex.CoverageRangeBlockParams
	suggestions          []pgindex.CoverageWorkSuggestion
	lastSuggestionParams pgindex.CoverageWorkSuggestionParams
	sequence             int64
	events               []*events.SignedEvent
	projected            int
}

func (s *fakeScrapeCoordinatorStore) CheckCoverageRangeBlock(_ context.Context, params pgindex.CoverageRangeBlockParams) (pgindex.CoverageRangeBlock, error) {
	s.lastBlockParams = params
	return s.block, nil
}

func (s *fakeScrapeCoordinatorStore) SuggestCoverageWork(_ context.Context, params pgindex.CoverageWorkSuggestionParams) ([]pgindex.CoverageWorkSuggestion, error) {
	s.lastSuggestionParams = params
	return s.suggestions, nil
}

func (s *fakeScrapeCoordinatorStore) UpsertFederationNodeIdentity(context.Context, string, ed25519.PublicKey) error {
	return nil
}

func (s *fakeScrapeCoordinatorStore) NextFederationEventSequence(context.Context, string) (int64, *string, error) {
	s.sequence++
	if len(s.events) == 0 {
		return s.sequence, nil, nil
	}
	previous := s.events[len(s.events)-1].EventID
	return s.sequence, &previous, nil
}

func (s *fakeScrapeCoordinatorStore) AppendVerifiedFederationEvent(_ context.Context, event *events.SignedEvent, validation *events.ValidationResult) error {
	if validation == nil || !validation.OK {
		return nil
	}
	s.events = append(s.events, event)
	return nil
}

func (s *fakeScrapeCoordinatorStore) ProjectCoverageEvent(context.Context, *events.SignedEvent) error {
	s.projected++
	return nil
}
