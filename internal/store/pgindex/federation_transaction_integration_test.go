package pgindex

import (
	"context"
	"errors"
	"testing"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
)

func TestAppendVerifiedFederationEventWithProjectionIsAtomic(t *testing.T) {
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
	if err := store.UpsertFederationNode(ctx, FederationNodeRecord{NodeID: nodeID, PublicKey: publicKey, Alias: "before", Status: "known"}); err != nil {
		t.Fatalf("store node: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM federation_events WHERE author_node_id = $1`, nodeID)
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM federation_nodes WHERE node_id = $1`, nodeID)
	})

	event, validationResult, err := events.Create(ctx, node, events.CreateOptions{
		EventType: "NodeProfile", Sequence: 1, CreatedAt: testIntegrationEventTime(),
		Visibility: "public", BodySchema: "gonzbnet.NodeProfile/1.0",
		Body: map[string]any{"schema_version": "1.0", "type": "NodeProfile", "node_id": nodeID},
	})
	if err != nil || validationResult == nil || !validationResult.OK {
		t.Fatalf("create event: validation=%+v err=%v", validationResult, err)
	}

	projectionErr := errors.New("forced projection failure")
	err = store.AppendVerifiedFederationEventWithProjection(ctx, event, validationResult, func(projectCtx context.Context) error {
		if _, err := store.federationExecutor(projectCtx).ExecContext(projectCtx, `UPDATE federation_nodes SET alias = 'rolled-back' WHERE node_id = $1`, nodeID); err != nil {
			return err
		}
		return projectionErr
	})
	if !errors.Is(err, projectionErr) {
		t.Fatalf("expected projection failure, got %v", err)
	}
	var eventCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM federation_events WHERE event_id = $1`, event.EventID).Scan(&eventCount); err != nil {
		t.Fatalf("count rolled-back event: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("expected event append rollback, found %d rows", eventCount)
	}
	var alias string
	if err := store.DB().QueryRowContext(ctx, `SELECT alias FROM federation_nodes WHERE node_id = $1`, nodeID).Scan(&alias); err != nil {
		t.Fatalf("read node after rollback: %v", err)
	}
	if alias != "before" {
		t.Fatalf("expected projection rollback, alias=%q", alias)
	}

	if err := store.AppendVerifiedFederationEventWithProjection(ctx, event, validationResult, func(projectCtx context.Context) error {
		_, err := store.federationExecutor(projectCtx).ExecContext(projectCtx, `UPDATE federation_nodes SET alias = 'committed' WHERE node_id = $1`, nodeID)
		return err
	}); err != nil {
		t.Fatalf("commit event and projection: %v", err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT alias FROM federation_nodes WHERE node_id = $1`, nodeID).Scan(&alias); err != nil {
		t.Fatalf("read committed projection: %v", err)
	}
	if alias != "committed" {
		t.Fatalf("expected committed projection, alias=%q", alias)
	}
}
