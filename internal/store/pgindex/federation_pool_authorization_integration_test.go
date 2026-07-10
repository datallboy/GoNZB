package pgindex

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/capability"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
)

func TestFederationPoolAuthorizationIntegration(t *testing.T) {
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
