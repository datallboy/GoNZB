package pgindex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/capability"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
)

func TestFederationPoolAuthorizationIntegration(t *testing.T) {
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
	poolID := "pool_auth_" + strings.ReplaceAll(nodeID, "-", "_")
	if err := store.UpsertFederationNode(ctx, FederationNodeRecord{NodeID: nodeID, PublicKey: publicKey, Status: "known"}); err != nil {
		t.Fatalf("store node: %v", err)
	}
	if err := store.UpsertTrustPool(ctx, TrustPoolRecord{PoolID: poolID, DisplayName: "Authorization Test", PolicyJSON: json.RawMessage(`{}`), MembershipThreshold: 1, ModerationThreshold: 1, CheckpointWitnessThreshold: 1, AcceptMode: "pool_member", AcceptedEventTypes: []string{"ReleaseCard"}, Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("store pool: %v", err)
	}
	if err := store.UpsertPoolMember(ctx, PoolMemberRecord{PoolID: poolID, NodeID: nodeID, Role: "member", Status: "active", AllowedCapabilities: []string{capability.Scanner, capability.Indexer}}); err != nil {
		t.Fatalf("store member: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO pool_members (pool_id, node_id, role, status, allowed_capabilities)
		VALUES ($1, $2, 'witness', 'active', 'null'::jsonb)
		ON CONFLICT (pool_id, node_id, role) DO UPDATE SET allowed_capabilities = 'null'::jsonb`, poolID, nodeID); err != nil {
		t.Fatalf("store legacy null-capability witness: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM pool_members WHERE pool_id = $1`, poolID)
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM trust_pools WHERE pool_id = $1`, poolID)
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM federation_nodes WHERE node_id = $1`, nodeID)
	})
	allowed, err := store.CanAcceptFederationEventForPools(ctx, nodeID, []string{poolID}, "ReleaseCard")
	if err != nil {
		t.Fatalf("authorize active member: %v", err)
	}
	if !allowed.Allowed {
		t.Fatalf("expected active capable member to be allowed, reason=%s", allowed.Reason)
	}
	scannerPools, err := store.ListActivePoolIDsForNodeCapabilities(ctx, nodeID, []string{capability.Scanner})
	if err != nil || len(scannerPools) != 1 || scannerPools[0] != poolID {
		t.Fatalf("expected scanner-capable pool selection, pools=%v err=%v", scannerPools, err)
	}
	validatorPools, err := store.ListActivePoolIDsForNodeCapabilities(ctx, nodeID, []string{capability.Validator})
	if err != nil || len(validatorPools) != 0 {
		t.Fatalf("expected validator pool selection to be empty, pools=%v err=%v", validatorPools, err)
	}
	unknown, err := store.CanAcceptFederationEventForPools(ctx, nodeID, []string{"pool.unknown"}, "ReleaseCard")
	if err != nil {
		t.Fatalf("authorize unknown pool: %v", err)
	}
	if unknown.Allowed || unknown.Reason != "unknown_pool" {
		t.Fatalf("expected unknown pool rejection, result=%+v", unknown)
	}
	multiple, err := store.CanAcceptFederationEventForPools(ctx, nodeID, []string{poolID, "pool.other"}, "ReleaseCard")
	if err != nil {
		t.Fatalf("authorize multiple pools: %v", err)
	}
	if multiple.Allowed || multiple.Reason != "multiple_pools_not_supported" {
		t.Fatalf("expected v1 multiple-pool rejection, result=%+v", multiple)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE pool_members SET status = 'revoked' WHERE pool_id = $1 AND node_id = $2`, poolID, nodeID); err != nil {
		t.Fatalf("revoke member: %v", err)
	}
	revoked, err := store.CanAcceptFederationEventForPools(ctx, nodeID, []string{poolID}, "ReleaseCard")
	if err != nil {
		t.Fatalf("authorize revoked member: %v", err)
	}
	if revoked.Allowed || revoked.Reason == "" {
		t.Fatalf("expected revoked member rejection, result=%+v", revoked)
	}
}

func TestFederationRevokedMemberCanReceiveOwnRevocation(t *testing.T) {
	ctx := context.Background()
	store := openPostgresTestStore(t)

	author := loadFederationTestIdentity(t)
	revoked := loadFederationTestIdentity(t)
	other := loadFederationTestIdentity(t)
	authorID, _ := author.NodeID(ctx)
	revokedID, _ := revoked.NodeID(ctx)
	otherID, _ := other.NodeID(ctx)
	poolID := "pool_revocation_" + strings.TrimPrefix(authorID, "node_")
	peerURL := "https://" + strings.TrimPrefix(revokedID, "node_") + ".example/gonzbnet/v1"

	for _, node := range []*identity.Identity{author, revoked, other} {
		nodeID, _ := node.NodeID(ctx)
		publicKey, _ := node.PublicKey(ctx)
		baseURL := "https://" + strings.TrimPrefix(nodeID, "node_") + ".example/gonzbnet/v1"
		if err := store.UpsertFederationNode(ctx, FederationNodeRecord{NodeID: nodeID, PublicKey: publicKey, BaseURL: baseURL, Status: "known"}); err != nil {
			t.Fatalf("store node %s: %v", nodeID, err)
		}
	}
	otherPublicKey, _ := other.PublicKey(ctx)
	if err := store.UpsertFederationNodeIdentity(ctx, otherID, otherPublicKey); err != nil {
		t.Fatalf("refresh other member identity: %v", err)
	}
	if err := store.UpsertTrustPool(ctx, TrustPoolRecord{
		PoolID: poolID, DisplayName: "Revocation Delivery Test", PolicyJSON: json.RawMessage(`{}`),
		MembershipThreshold: 1, ModerationThreshold: 1, CheckpointWitnessThreshold: 1,
		AcceptMode: "pool_member", AcceptedEventTypes: []string{pools.EventTypePoolMemberRevoked}, Enabled: true,
	}); err != nil {
		t.Fatalf("store pool: %v", err)
	}
	if err := store.UpsertPoolMember(ctx, PoolMemberRecord{PoolID: poolID, NodeID: revokedID, Role: pools.RoleMember, Status: pools.StatusRevoked}); err != nil {
		t.Fatalf("store revoked member: %v", err)
	}
	if err := store.UpsertPoolMember(ctx, PoolMemberRecord{PoolID: poolID, NodeID: otherID, Role: pools.RoleMember, Status: pools.StatusActive}); err != nil {
		t.Fatalf("store other member: %v", err)
	}
	endpoints, err := store.ListActivePoolMemberEndpoints(ctx, poolID)
	if err != nil {
		t.Fatalf("list active pool member endpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].NodeID != otherID {
		t.Fatalf("expected only active member endpoint, got %+v", endpoints)
	}
	peerID, err := store.UpsertFederationPeerURL(ctx, peerURL)
	if err != nil {
		t.Fatalf("store revoked member peer: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE federation_peers SET node_id = $2 WHERE id = $1`, peerID, revokedID); err != nil {
		t.Fatalf("bind peer identity: %v", err)
	}

	var previous *string
	ownRevocation := createRevocationDeliveryTestEvent(t, author, poolID, revokedID, 1, previous)
	previous = &ownRevocation.EventID
	otherRevocation := createRevocationDeliveryTestEvent(t, author, poolID, otherID, 2, previous)
	for _, event := range []*events.SignedEvent{ownRevocation, otherRevocation} {
		validation, _ := events.Verify(event)
		if err := store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
			t.Fatalf("append %s: %v", event.EventID, err)
		}
	}

	t.Cleanup(func() {
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM federation_peer_deliveries WHERE peer_id = $1`, peerID)
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM federation_peers WHERE id = $1`, peerID)
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM federation_events WHERE author_node_id = $1`, authorID)
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM pool_members WHERE pool_id = $1`, poolID)
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM trust_pools WHERE pool_id = $1`, poolID)
		_, _ = store.DB().ExecContext(context.Background(), `DELETE FROM federation_nodes WHERE node_id = ANY($1)`, []string{authorID, revokedID, otherID})
	})

	historical, err := store.IsFederationPoolMember(ctx, revokedID)
	if err != nil || !historical {
		t.Fatalf("expected historical membership, historical=%v err=%v", historical, err)
	}
	page, err := store.ListFederationOutboxEvents(ctx, FederationOutboxParams{
		PoolID: poolID, EventType: pools.EventTypePoolMemberRevoked, RequestingNodeID: revokedID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("list revoked member outbox: %v", err)
	}
	if !eventListContains(page.Events, ownRevocation.EventID) {
		t.Fatalf("own revocation was not readable by revoked member")
	}
	if eventListContains(page.Events, otherRevocation.EventID) {
		t.Fatalf("revoked member could read another member's revocation")
	}

	deliveries, err := store.ListUndeliveredFederationEvents(ctx, peerID, revokedID, 100)
	if err != nil {
		t.Fatalf("list directed deliveries: %v", err)
	}
	if !eventListContains(deliveries, ownRevocation.EventID) {
		t.Fatalf("own revocation was not selected for directed delivery")
	}
	if eventListContains(deliveries, otherRevocation.EventID) {
		t.Fatalf("another member's revocation was selected for directed delivery")
	}
}

func loadFederationTestIdentity(t *testing.T) *identity.Identity {
	t.Helper()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	return node
}

func createRevocationDeliveryTestEvent(t *testing.T, author *identity.Identity, poolID, subjectNodeID string, sequence int64, previousEventID *string) *events.SignedEvent {
	t.Helper()
	event, validation, err := events.Create(context.Background(), author, events.CreateOptions{
		EventType: pools.EventTypePoolMemberRevoked, Sequence: sequence, PreviousEventID: previousEventID,
		CreatedAt: time.Now().UTC().Add(time.Duration(sequence) * time.Millisecond), PoolIDs: []string{poolID}, Visibility: "pool",
		BodySchema: pools.BodySchema(pools.EventTypePoolMemberRevoked),
		Body:       map[string]any{"schema_version": "1.0", "type": pools.EventTypePoolMemberRevoked, "pool_id": poolID, "subject_node_id": subjectNodeID},
	})
	if err != nil || validation == nil || !validation.OK {
		t.Fatalf("create revocation event: validation=%+v err=%v", validation, err)
	}
	return event
}

func eventListContains(items []*events.SignedEvent, eventID string) bool {
	for _, event := range items {
		if event != nil && event.EventID == eventID {
			return true
		}
	}
	return false
}
