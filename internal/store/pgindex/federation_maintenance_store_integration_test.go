package pgindex

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestCompactFederationProtocolStateRemovesOnlyExpiredEphemera(t *testing.T) {
	store := openPostgresTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)

	for _, nodeID := range []string{"node_stale_handshake", "node_recent_handshake"} {
		if err := store.UpsertFederationNode(ctx, FederationNodeRecord{
			NodeID: nodeID, PublicKey: publicKey, Status: "handshaken",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE federation_nodes
		SET last_seen_at = CASE
		  WHEN node_id = 'node_stale_handshake' THEN $1::timestamptz
		  ELSE $2::timestamptz
		END
		WHERE node_id IN ('node_stale_handshake', 'node_recent_handshake')`,
		now.Add(-48*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if inserted, err := store.StoreFederationNonce(ctx, "node_stale_handshake", "expired", now.Add(-time.Minute)); err != nil || !inserted {
		t.Fatalf("insert expired nonce: inserted=%t err=%v", inserted, err)
	}
	if inserted, err := store.StoreFederationNonce(ctx, "node_recent_handshake", "active", now.Add(time.Hour)); err != nil || !inserted {
		t.Fatalf("insert active nonce: inserted=%t err=%v", inserted, err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO federation_rejected_events (event_id, raw_event_json, rejection_reason, received_at)
		VALUES
		  ('evt_old_rejection', '{}', 'old', $1),
		  ('evt_recent_rejection', '{}', 'recent', $2)`,
		now.Add(-100*24*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	result, err := store.CompactFederationProtocolState(
		ctx, now, now.Add(-24*time.Hour), now.Add(-90*24*time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExpiredNonces != 1 || result.RejectedEvents != 1 || result.StaleHandshakeNodes != 1 {
		t.Fatalf("unexpected compaction result: %+v", result)
	}

	for query, want := range map[string]int{
		`SELECT COUNT(*) FROM federation_nodes WHERE node_id = 'node_recent_handshake'`:           1,
		`SELECT COUNT(*) FROM federation_nodes WHERE node_id = 'node_stale_handshake'`:            0,
		`SELECT COUNT(*) FROM federation_nonce_replay_cache WHERE nonce = 'active'`:               1,
		`SELECT COUNT(*) FROM federation_nonce_replay_cache WHERE nonce = 'expired'`:              0,
		`SELECT COUNT(*) FROM federation_rejected_events WHERE event_id = 'evt_recent_rejection'`: 1,
		`SELECT COUNT(*) FROM federation_rejected_events WHERE event_id = 'evt_old_rejection'`:    0,
	} {
		var got int
		if err := store.DB().QueryRowContext(ctx, query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("query %q: expected %d, got %d", query, want, got)
		}
	}
}
