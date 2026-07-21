package pgindex

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifest"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
)

func TestFederationScanOutputManifestRepairSelection(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("GONZB_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set GONZB_TEST_PG_DSN to run scan-output repair integration test")
	}
	ctx := context.Background()
	store, err := NewStore(dsn)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	nodeID, _ := node.NodeID(ctx)
	scanID := "scan-repair-" + strings.TrimPrefix(nodeID, "node_")
	poolID := "pool.repair." + strings.TrimPrefix(nodeID, "node_")
	manifestID := "man_repair_" + strings.TrimPrefix(nodeID, "node_")
	releaseID := "rel_repair_" + strings.TrimPrefix(nodeID, "node_")

	cardEvent := createScanOutputRepairEvent(t, node, pools.EventTypeReleaseCard, releasecard.BodySchema, poolID, 1, nil, map[string]any{
		"type": "ReleaseCard", "release_id": releaseID, "manifest_id": manifestID,
	})
	cardValidation, _ := events.Verify(cardEvent)
	if err := store.AppendVerifiedFederationEvent(ctx, cardEvent, cardValidation); err != nil {
		t.Fatalf("append release card: %v", err)
	}
	if err := store.UpsertGoNZBNetScanOutput(ctx, releasecard.LocalRelease{LocalReleaseID: scanID, GUID: scanID, Title: "repair fixture"}); err != nil {
		t.Fatalf("store scan output: %v", err)
	}
	if err := store.MarkGoNZBNetScanOutputPublished(ctx, scanID, cardEvent.EventID, poolID); err != nil {
		t.Fatalf("mark scan output published: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO resolution_manifests (
			manifest_id, release_id, source_node_id, source_event_id, validation_status
		) VALUES ($1, $2, $3, $4, 'accepted')`, manifestID, releaseID, nodeID, cardEvent.EventID); err != nil {
		t.Fatalf("store stale manifest source: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM gonzbnet_scan_outputs WHERE scan_id = $1`, scanID)
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM resolution_manifests WHERE manifest_id = $1`, manifestID)
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM federation_events WHERE author_node_id = $1`, nodeID)
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM federation_nodes WHERE node_id = $1`, nodeID)
	})

	ordinary, err := store.ListGoNZBNetScanOutputCandidates(ctx, poolID, false, 10)
	if err != nil || len(ordinary) != 0 {
		t.Fatalf("non-builder should not revisit publication: candidates=%d err=%v", len(ordinary), err)
	}
	repair, err := store.ListGoNZBNetScanOutputCandidates(ctx, poolID, true, 10)
	if err != nil || len(repair) != 1 || repair[0].LocalReleaseID != scanID {
		t.Fatalf("builder should select stale manifest publication: candidates=%+v err=%v", repair, err)
	}

	previous := cardEvent.EventID
	manifestEvent := createScanOutputRepairEvent(t, node, manifest.Type, manifest.BodySchema, poolID, 2, &previous, map[string]any{
		"type": manifest.Type, "release_id": releaseID, "manifest_id": manifestID,
	})
	manifestValidation, _ := events.Verify(manifestEvent)
	if err := store.AppendVerifiedFederationEvent(ctx, manifestEvent, manifestValidation); err != nil {
		t.Fatalf("append manifest event: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE resolution_manifests SET source_event_id = $2 WHERE manifest_id = $1`, manifestID, manifestEvent.EventID); err != nil {
		t.Fatalf("repair manifest source: %v", err)
	}
	repaired, err := store.ListGoNZBNetScanOutputCandidates(ctx, poolID, true, 10)
	if err != nil || len(repaired) != 0 {
		t.Fatalf("repaired publication should be idempotent: candidates=%d err=%v", len(repaired), err)
	}
}

func createScanOutputRepairEvent(t *testing.T, node events.Identity, eventType, bodySchema, poolID string, sequence int64, previousEventID *string, body any) *events.SignedEvent {
	t.Helper()
	event, validation, err := events.Create(t.Context(), node, events.CreateOptions{
		EventType: eventType, Sequence: sequence, PreviousEventID: previousEventID,
		CreatedAt: time.Now().UTC(), PoolIDs: []string{poolID}, Visibility: "pool",
		BodySchema: bodySchema, Body: body,
	})
	if err != nil || validation == nil || !validation.OK {
		t.Fatalf("create %s event: validation=%+v err=%v", eventType, validation, err)
	}
	return event
}
