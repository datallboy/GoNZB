package pgindex

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
)

func TestFederationEventChainGapResolutionAndForkDetection(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("GONZB_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set GONZB_TEST_PG_DSN to run federation chain integration test")
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
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM federation_event_chain_issues WHERE author_node_id = $1`, nodeID)
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM federation_events WHERE author_node_id = $1`, nodeID)
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM federation_nodes WHERE node_id = $1`, nodeID)
	})

	first := createChainTestEvent(t, node, 1, nil, "first")
	second := createChainTestEvent(t, node, 2, &first.EventID, "second")
	secondValidation, _ := events.Verify(second)
	if err := store.AppendVerifiedFederationEvent(ctx, second, secondValidation); err != nil {
		t.Fatalf("append gapped second event: %v", err)
	}
	if got := openChainIssueCount(t, store, nodeID, "sequence_gap"); got != 1 {
		t.Fatalf("expected one open sequence gap, got %d", got)
	}

	firstValidation, _ := events.Verify(first)
	if err := store.AppendVerifiedFederationEvent(ctx, first, firstValidation); err != nil {
		t.Fatalf("append missing predecessor: %v", err)
	}
	if got := openChainIssueCount(t, store, nodeID, "sequence_gap"); got != 0 {
		t.Fatalf("expected sequence gap resolution, got %d open gaps", got)
	}

	fork := createChainTestEvent(t, node, 2, &first.EventID, "fork")
	forkValidation, _ := events.Verify(fork)
	if err := store.AppendVerifiedFederationEvent(ctx, fork, forkValidation); !errors.Is(err, ErrFederationSequenceConflict) {
		t.Fatalf("expected sequence conflict, got %v", err)
	}
	if got := openChainIssueCount(t, store, nodeID, "fork"); got != 1 {
		t.Fatalf("expected one open fork issue, got %d", got)
	}
	var status string
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM federation_nodes WHERE node_id = $1`, nodeID).Scan(&status); err != nil {
		t.Fatalf("read forked node status: %v", err)
	}
	if status != "forked" {
		t.Fatalf("expected forked node status, got %q", status)
	}
	var rawEventJSON string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT raw_event_json
		FROM federation_event_chain_issues
		WHERE author_node_id = $1 AND event_id = $2 AND issue_type = 'fork'`, nodeID, fork.EventID).Scan(&rawEventJSON); err != nil {
		t.Fatalf("read fork evidence: %v", err)
	}
	if !strings.Contains(rawEventJSON, fork.EventID) {
		t.Fatalf("fork evidence does not contain conflicting signed event")
	}
}

func createChainTestEvent(t *testing.T, node *identity.Identity, sequence int64, previousEventID *string, marker string) *events.SignedEvent {
	t.Helper()
	event, validation, err := events.Create(context.Background(), node, events.CreateOptions{
		EventType:       "NodeProfile",
		Sequence:        sequence,
		PreviousEventID: previousEventID,
		CreatedAt:       time.Date(2026, 7, 9, 12, 0, int(sequence), 0, time.UTC),
		Visibility:      "public",
		BodySchema:      "gonzbnet.NodeProfile/1.0",
		Body:            map[string]any{"schema_version": "1.0", "type": "NodeProfile", "marker": marker},
	})
	if err != nil || validation == nil || !validation.OK {
		t.Fatalf("create chain event: validation=%+v err=%v", validation, err)
	}
	return event
}

func openChainIssueCount(t *testing.T, store *Store, nodeID, issueType string) int {
	t.Helper()
	var count int
	if err := store.DB().QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM federation_event_chain_issues
		WHERE author_node_id = $1
		  AND issue_type = $2
		  AND resolved_at IS NULL`, nodeID, issueType).Scan(&count); err != nil {
		t.Fatalf("count open chain issues: %v", err)
	}
	return count
}
