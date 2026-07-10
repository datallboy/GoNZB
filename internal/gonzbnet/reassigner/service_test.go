package reassigner

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestRunOnceSignsReplacementAssignmentForStaleRangeClaim(t *testing.T) {
	nodeIdentity, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store := &fakeStore{
		claims: []pgindex.CoverageClaimRecord{{
			ClaimID:    "claim-1",
			ClaimType:  "range",
			PoolID:     "pool.test",
			Group:      "alt.binaries.test",
			NodeID:     "node_stale",
			RangeStart: 10,
			RangeEnd:   20,
			Status:     "active",
		}},
		nodes: []pgindex.CoverageScannerNode{
			{NodeID: "node_stale", Weight: 100, LocalTrustScore: 1},
			{NodeID: "node_fresh", Weight: 1, LocalTrustScore: 1},
		},
	}
	svc, err := New(nodeIdentity, store, "pool.test", 0.65)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	result, err := svc.RunOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if result.StaleClaims != 1 || result.AssignmentsCreated != 1 || result.SkippedNoNode != 0 || result.SkippedDuplicate != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(store.events) != 1 || store.events[0].EventType != coverage.TypeCoverageAssignment {
		t.Fatalf("expected one coverage assignment event, got %+v", store.events)
	}
	var body coverage.CoverageAssignment
	if err := json.Unmarshal(store.events[0].Body, &body); err != nil {
		t.Fatalf("decode assignment: %v", err)
	}
	if body.AssignedNodeID != "node_fresh" || body.RangeStart != 10 || body.RangeEnd != 20 || body.AssignmentID == "" {
		t.Fatalf("unexpected assignment body: %+v", body)
	}
	if store.projected != 1 {
		t.Fatalf("expected projected assignment, got %d", store.projected)
	}
}

func TestRunOnceSkipsDuplicateReplacementAssignment(t *testing.T) {
	nodeIdentity, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	claim := pgindex.CoverageClaimRecord{
		ClaimID:    "claim-1",
		PoolID:     "pool.test",
		Group:      "alt.binaries.test",
		NodeID:     "node_stale",
		RangeStart: 10,
		RangeEnd:   20,
	}
	store := &fakeStore{
		claims:   []pgindex.CoverageClaimRecord{claim},
		nodes:    []pgindex.CoverageScannerNode{{NodeID: "node_fresh", Weight: 1, LocalTrustScore: 1}},
		existing: map[string]bool{replacementAssignmentID(claim): true},
	}
	svc, err := New(nodeIdentity, store, "pool.test", 0.65)
	if err != nil {
		t.Fatalf("service: %v", err)
	}

	result, err := svc.RunOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if result.AssignmentsCreated != 0 || result.SkippedDuplicate != 1 {
		t.Fatalf("expected duplicate skip, got %+v", result)
	}
	if len(store.events) != 0 {
		t.Fatalf("duplicate should not append events, got %d", len(store.events))
	}
}

type fakeStore struct {
	claims     []pgindex.CoverageClaimRecord
	nodes      []pgindex.CoverageScannerNode
	existing   map[string]bool
	sequence   int64
	events     []*events.SignedEvent
	projected  int
	localNode  string
	publicKey  ed25519.PublicKey
	minTrust   float64
	lastPoolID string
}

func (s *fakeStore) ListStaleCoverageRangeClaims(_ context.Context, poolID string, _ int) ([]pgindex.CoverageClaimRecord, error) {
	s.lastPoolID = poolID
	return s.claims, nil
}

func (s *fakeStore) ListCoverageScannerNodes(_ context.Context, _ string, minTrustScore float64) ([]pgindex.CoverageScannerNode, error) {
	s.minTrust = minTrustScore
	return s.nodes, nil
}

func (s *fakeStore) CoverageAssignmentExists(_ context.Context, assignmentID string) (bool, error) {
	return s.existing != nil && s.existing[assignmentID], nil
}

func (s *fakeStore) UpsertFederationNodeIdentity(_ context.Context, nodeID string, publicKey ed25519.PublicKey) error {
	s.localNode = nodeID
	s.publicKey = publicKey
	return nil
}

func (s *fakeStore) NextFederationEventSequence(context.Context, string) (int64, *string, error) {
	s.sequence++
	if len(s.events) == 0 {
		return s.sequence, nil, nil
	}
	previous := s.events[len(s.events)-1].EventID
	return s.sequence, &previous, nil
}

func (s *fakeStore) AppendVerifiedFederationEvent(_ context.Context, event *events.SignedEvent, validation *events.ValidationResult) error {
	if validation != nil && validation.OK {
		s.events = append(s.events, event)
	}
	return nil
}

func (s *fakeStore) ProjectCoverageEvent(context.Context, *events.SignedEvent) error {
	s.projected++
	return nil
}
