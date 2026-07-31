package pgindex

import (
	"context"
	"fmt"
	"time"
)

type FederationProtocolCompactionResult struct {
	ExpiredNonces       int64 `json:"expired_nonces"`
	RejectedEvents      int64 `json:"rejected_events"`
	StaleHandshakeNodes int64 `json:"stale_handshake_nodes"`
}

// CompactFederationProtocolState removes bounded-lifetime protocol records.
// Accepted events and their projections are intentionally retained.
func (s *Store) CompactFederationProtocolState(
	ctx context.Context,
	now time.Time,
	handshakeBefore time.Time,
	rejectedBefore time.Time,
) (FederationProtocolCompactionResult, error) {
	var result FederationProtocolCompactionResult
	if s == nil || s.db == nil {
		return result, fmt.Errorf("pgindex store is not initialized")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if handshakeBefore.IsZero() {
		handshakeBefore = now.Add(-24 * time.Hour)
	}
	if rejectedBefore.IsZero() {
		rejectedBefore = now.Add(-90 * 24 * time.Hour)
	}

	err := s.db.QueryRowContext(ctx, `
		WITH expired_nonces AS (
		  DELETE FROM federation_nonce_replay_cache
		  WHERE expires_at < $1
		  RETURNING 1
		),
		rejected_events AS (
		  DELETE FROM federation_rejected_events
		  WHERE received_at < $2
		  RETURNING 1
		),
		stale_handshakes AS (
		  DELETE FROM federation_nodes node
		  WHERE node.status = 'handshaken'
		    AND COALESCE(node.last_seen_at, node.created_at) < $3
		    AND NOT EXISTS (
		      SELECT 1 FROM federation_events event
		      WHERE event.author_node_id = node.node_id
		    )
		    AND NOT EXISTS (
		      SELECT 1 FROM federation_peers peer
		      WHERE peer.node_id = node.node_id
		    )
		    AND NOT EXISTS (
		      SELECT 1 FROM pool_members member
		      WHERE member.node_id = node.node_id
		    )
		    AND NOT EXISTS (
		      SELECT 1 FROM federation_pool_admissions admission
		      WHERE admission.candidate_node_id = node.node_id
		    )
		  RETURNING 1
		)
		SELECT
		  (SELECT COUNT(*) FROM expired_nonces),
		  (SELECT COUNT(*) FROM rejected_events),
		  (SELECT COUNT(*) FROM stale_handshakes)`,
		now.UTC(),
		rejectedBefore.UTC(),
		handshakeBefore.UTC(),
	).Scan(&result.ExpiredNonces, &result.RejectedEvents, &result.StaleHandshakeNodes)
	if err != nil {
		return FederationProtocolCompactionResult{}, fmt.Errorf("compact federation protocol state: %w", err)
	}
	return result, nil
}
