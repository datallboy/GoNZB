package pgindex

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/gonzbnet/validation"
)

func TestFederationAcceptedAndRejectedEventPersistence(t *testing.T) {
	ctx := context.Background()
	store := openPostgresTestStore(t)
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
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM federated_release_cards WHERE release_id = 'rel_pg_projection'`)
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM federation_rejected_events WHERE author_node_id = $1`, nodeID)
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
	if err := store.DB().QueryRowContext(ctx, `
		SELECT card.title, source.pool_id
		FROM federated_release_cards card
		JOIN federated_release_sources source ON source.release_id = card.release_id
		WHERE card.release_id = $1 AND source.source_node_id = $2 AND source.pool_id = $3`,
		card.ReleaseID, nodeID, "pool.test").Scan(&projectedTitle, &projectedPool); err != nil {
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
	var projectedCapacity validation.ValidatorCapacity
	if err := json.Unmarshal(capacityJSON, &projectedCapacity); err != nil {
		t.Fatalf("decode validator capacity projection: %v", err)
	}
	if projectedCapacity.MaxTasksPerHour != 10 {
		t.Fatalf("validator capacity projection missing expected value: %+v", projectedCapacity)
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
	attestation := validation.ArticleAvailabilityAttestation{
		SchemaVersion: "1.0", Type: validation.TypeArticleAvailabilityAttestation,
		ReleaseID: "rel_pg_projection", ManifestID: "man_pg_projection",
		CheckedAt: time.Now().UTC().Format(time.RFC3339), Status: validation.StatusAvailable,
		ArticlesTotal: 1, ArticlesAvailable: 1, Confidence: 1, Method: "nntp_fetch_body_prefix",
	}
	if err := store.ProjectArticleAvailabilityAttestation(ctx, ArticleAvailabilityProjection{
		Attestation: attestation, EventID: coverageEvent.EventID, AuthorNodeID: nodeID, PoolID: "pool.test",
	}); err != nil {
		t.Fatalf("project article availability: %v", err)
	}
	var projectedStatus string
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM article_availability_attestations WHERE manifest_id = $1 AND author_node_id = $2`, attestation.ManifestID, nodeID).Scan(&projectedStatus); err != nil {
		t.Fatalf("read article availability projection: %v", err)
	}
	if projectedStatus != validation.StatusAvailable {
		t.Fatalf("unexpected article availability status=%q", projectedStatus)
	}
	rejectedID := event.EventID + "-rejected"
	if err := store.AppendRejectedFederationEvent(ctx, rejectedID, nodeID, "NodeProfile", []byte(`{"bad":true}`), "malformed signature"); err != nil {
		t.Fatalf("append rejected event: %v", err)
	}
	var reason string
	if err := store.DB().QueryRowContext(ctx, `SELECT rejection_reason FROM federation_rejected_events WHERE event_id = $1 ORDER BY received_at DESC LIMIT 1`, rejectedID).Scan(&reason); err != nil {
		t.Fatalf("read rejected event: %v", err)
	}
	if reason != "malformed signature" {
		t.Fatalf("unexpected rejection reason=%q", reason)
	}
}

func TestFederationEventStorePreservesSignedTimestampPrecision(t *testing.T) {
	ctx := context.Background()
	store := openPostgresTestStore(t)
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
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM federation_events WHERE author_node_id = $1`, nodeID)
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM federation_nodes WHERE node_id = $1`, nodeID)
	})

	event, validation, err := events.Create(ctx, node, events.CreateOptions{
		EventType: "NodeProfile", Sequence: 1,
		CreatedAt:  testIntegrationEventTime().Add(853298405 * time.Nanosecond),
		Visibility: "public", BodySchema: "gonzbnet.NodeProfile/1.0",
		Body: map[string]any{"schema_version": "1.0", "type": "NodeProfile", "node_id": nodeID},
	})
	if err != nil || validation == nil || !validation.OK {
		t.Fatalf("create event: validation=%+v err=%v", validation, err)
	}
	if err := store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
		t.Fatalf("append event: %v", err)
	}
	storedEvent, err := store.GetFederationEvent(ctx, event.EventID)
	if err != nil {
		t.Fatalf("reload event: %v", err)
	}
	wireJSON, err := json.Marshal(storedEvent)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	var wireEvent events.SignedEvent
	if err := json.Unmarshal(wireJSON, &wireEvent); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	wireValidation, err := events.Verify(&wireEvent)
	if err != nil || wireValidation == nil || !wireValidation.OK {
		t.Fatalf("verify reloaded wire event: validation=%+v err=%v", wireValidation, err)
	}
}
