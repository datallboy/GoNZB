package pgindex

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
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
	event, validation, err := events.Create(ctx, node, events.CreateOptions{
		EventType: "NodeProfile", Sequence: 1, CreatedAt: testIntegrationEventTime(),
		Visibility: "public", BodySchema: "gonzbnet.NodeProfile/1.0",
		Body: map[string]any{"schema_version": "1.0", "type": "NodeProfile", "node_id": nodeID},
	})
	if err != nil || validation == nil || !validation.OK {
		t.Fatalf("create event: validation=%+v err=%v", validation, err)
	}
	if err := store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
		t.Fatalf("append accepted event: %v", err)
	}
	var accepted string
	if err := store.DB().QueryRowContext(ctx, `SELECT validation_status FROM federation_events WHERE event_id = $1`, event.EventID).Scan(&accepted); err != nil {
		t.Fatalf("read accepted event: %v", err)
	}
	if accepted != "accepted" {
		t.Fatalf("expected accepted status, got %q", accepted)
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
