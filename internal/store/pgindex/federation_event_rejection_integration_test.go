package pgindex

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/gonzbnet/validation"
)

func TestFederationAcceptedAndRejectedEventPersistence(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("GONZB_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set GONZB_TEST_PG_DSN to run pgindex integration tests")
	}
	ctx := context.Background()
	store, err := NewStore(dsn)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	nodeID, err := node.NodeID(ctx)
	if err != nil {
		t.Fatalf("node id: %v", err)
	}
	publicKey, err := node.PublicKey(ctx)
	if err != nil {
		t.Fatalf("public key: %v", err)
	}
	if err := store.UpsertFederationNode(ctx, FederationNodeRecord{NodeID: nodeID, PublicKey: publicKey, Status: "known"}); err != nil {
		t.Fatalf("store node: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM federation_events WHERE author_node_id = $1`, nodeID)
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM federation_nodes WHERE node_id = $1`, nodeID)
	})
	event, eventValidation, err := events.Create(ctx, node, events.CreateOptions{
		EventType: "NodeProfile", Sequence: 1, CreatedAt: testIntegrationEventTime(),
		Visibility: "public", BodySchema: "gonzbnet.NodeProfile/1.0",
		Body: map[string]any{"schema_version": "1.0", "type": "NodeProfile", "node_id": nodeID},
	})
	if err != nil || eventValidation == nil || !eventValidation.OK {
		t.Fatalf("create event: validation=%+v err=%v", eventValidation, err)
	}
	if err := store.AppendVerifiedFederationEvent(ctx, event, eventValidation); err != nil {
		t.Fatalf("append accepted event: %v", err)
	}
	var accepted string
	if err := store.DB().QueryRowContext(ctx, `SELECT validation_status FROM federation_events WHERE event_id = $1`, event.EventID).Scan(&accepted); err != nil {
		t.Fatalf("read accepted event: %v", err)
	}
	if accepted != "accepted" {
		t.Fatalf("expected accepted status, got %q", accepted)
	}
	card := releasecard.ReleaseCard{
		SchemaVersion: "1.0", Type: "ReleaseCard", ReleaseID: "rel_pg_projection",
		Title: "PG projection", NormalizedTitle: "pg projection", Groups: []string{"alt.test"},
		FileCount: 1, SegmentCount: 1, SubjectFingerprint: "subject", FileFingerprint: "file",
		NZBGUID: func() *string { value := "guid"; return &value }(),
	}
	if err := store.UpsertFederatedReleaseCardProjection(ctx, releasecard.Projection{
		Card: card, EventID: event.EventID, SourceNodeID: nodeID, PoolID: "pool.test",
	}); err != nil {
		t.Fatalf("project release card: %v", err)
	}
	var projectedTitle, projectedPool string
	if err := store.DB().QueryRowContext(ctx, `SELECT title, pool_id FROM federated_release_cards WHERE release_id = $1`, card.ReleaseID).Scan(&projectedTitle, &projectedPool); err != nil {
		t.Fatalf("read release-card projection: %v", err)
	}
	if projectedTitle != card.Title || projectedPool != "pool.test" {
		t.Fatalf("unexpected release-card projection title=%q pool=%q", projectedTitle, projectedPool)
	}
	capacity := validation.ValidatorCapacity{
		SchemaVersion: "1.0", Type: validation.TypeValidatorCapacity, NodeID: nodeID,
		PublishedAt: time.Now().UTC().Format(time.RFC3339), MaxTasksPerHour: 10,
	}
	if err := store.ProjectValidatorCapacity(ctx, ValidatorCapacityProjection{Capacity: capacity, EventID: event.EventID, AuthorNodeID: nodeID}); err != nil {
		t.Fatalf("project validator capacity: %v", err)
	}
	var capacityJSON []byte
	if err := store.DB().QueryRowContext(ctx, `SELECT validator_capacity FROM federation_node_capabilities WHERE node_id = $1`, nodeID).Scan(&capacityJSON); err != nil {
		t.Fatalf("read validator capacity projection: %v", err)
	}
	if !strings.Contains(string(capacityJSON), `"max_tasks_per_hour":10`) {
		t.Fatalf("validator capacity projection missing expected value: %s", capacityJSON)
	}
	coverageBody := coverage.ScannerCapacity{
		SchemaVersion: "1.0", Type: coverage.TypeScannerCapacity, NodeID: nodeID,
		PoolID: "pool.test", CreatedAt: time.Now().UTC().Format(time.RFC3339), MaxGroups: 3,
	}
	coverageEvent, coverageValidation, err := events.Create(ctx, node, events.CreateOptions{
		EventType: coverage.TypeScannerCapacity, Sequence: 2, PreviousEventID: &event.EventID,
		CreatedAt: time.Now().UTC(), PoolIDs: []string{"pool.test"}, Visibility: "pool",
		BodySchema: coverage.ScannerCapacityBodySchema, Body: coverageBody,
	})
	if err != nil || coverageValidation == nil || !coverageValidation.OK {
		t.Fatalf("create coverage event: validation=%+v err=%v", coverageValidation, err)
	}
	if err := store.AppendVerifiedFederationEvent(ctx, coverageEvent, coverageValidation); err != nil {
		t.Fatalf("append coverage event: %v", err)
	}
	if err := store.ProjectCoverageEvent(ctx, coverageEvent); err != nil {
		t.Fatalf("project coverage event: %v", err)
	}
	var projectedMaxGroups int
	if err := store.DB().QueryRowContext(ctx, `SELECT max_groups FROM scanner_capacities WHERE node_id = $1`, nodeID).Scan(&projectedMaxGroups); err != nil {
		t.Fatalf("read scanner capacity projection: %v", err)
	}
	if projectedMaxGroups != 3 {
		t.Fatalf("unexpected scanner capacity max_groups=%d", projectedMaxGroups)
	}
	rejectedID := event.EventID + "-rejected"
	if err := store.AppendRejectedFederationEvent(ctx, rejectedID, nodeID, "NodeProfile", []byte(`{"bad":true}`), "malformed signature"); err != nil {
		t.Fatalf("append rejected event: %v", err)
	}
	var rejected, reason string
	if err := store.DB().QueryRowContext(ctx, `SELECT validation_status, rejection_reason FROM federation_events WHERE event_id = $1`, rejectedID).Scan(&rejected, &reason); err != nil {
		t.Fatalf("read rejected event: %v", err)
	}
	if rejected != "rejected" || reason != "malformed signature" {
		t.Fatalf("unexpected rejection state status=%q reason=%q", rejected, reason)
	}
}
