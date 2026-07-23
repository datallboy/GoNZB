package pgindex

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
)

func testIntegrationEventTime() time.Time {
	return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
}

func TestPendingFederationProjectionLifecycle(t *testing.T) {
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
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM federation_pending_projections WHERE event_id IN (SELECT event_id FROM federation_events WHERE author_node_id = $1)`, nodeID)
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM federation_events WHERE author_node_id = $1`, nodeID)
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM federation_nodes WHERE node_id = $1`, nodeID)
	})
	event, validation, err := events.Create(ctx, node, events.CreateOptions{
		EventType: "ReleaseCard", Sequence: 1, CreatedAt: testIntegrationEventTime(),
		Visibility: "public", BodySchema: "gonzbnet.ReleaseCard/1.0",
		Body: map[string]any{"schema_version": "1.0", "type": "ReleaseCard", "release_id": "rel_pending"},
	})
	if err != nil || validation == nil || !validation.OK {
		t.Fatalf("create event: validation=%+v err=%v", validation, err)
	}
	if err := store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
		t.Fatalf("append event: %v", err)
	}
	if err := store.RecordFederationProjectionFailure(ctx, event.EventID, event.EventType, "release_card", os.ErrInvalid); err != nil {
		t.Fatalf("record pending projection: %v", err)
	}
	if err := store.RecordFederationProjectionFailure(ctx, event.EventID, event.EventType, "release_card", os.ErrPermission); err != nil {
		t.Fatalf("increment pending projection: %v", err)
	}
	var status string
	var attempts int
	if err := store.DB().QueryRowContext(ctx, `SELECT status, attempts FROM federation_pending_projections WHERE event_id = $1`, event.EventID).Scan(&status, &attempts); err != nil {
		t.Fatalf("read pending projection: %v", err)
	}
	if status != "pending" || attempts != 2 {
		t.Fatalf("unexpected pending state status=%q attempts=%d", status, attempts)
	}
	if err := store.ResolveFederationProjection(ctx, event.EventID); err != nil {
		t.Fatalf("resolve pending projection: %v", err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM federation_pending_projections WHERE event_id = $1`, event.EventID).Scan(&status); err != nil {
		t.Fatalf("read resolved projection: %v", err)
	}
	if status != "resolved" {
		t.Fatalf("expected resolved projection, got %q", status)
	}
}
