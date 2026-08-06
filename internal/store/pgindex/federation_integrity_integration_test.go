package pgindex

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/capability"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifest"
	"github.com/datallboy/gonzb/internal/gonzbnet/moderation"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
)

func TestFederationIntegrityLedgerAndCacheRepairIntegration(t *testing.T) {
	ctx := context.Background()
	store := openPostgresTestStore(t)
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nodeID, _ := node.NodeID(ctx)
	publicKey, _ := node.PublicKey(ctx)
	poolID := "pool.integrity"
	if err := store.UpsertFederationNode(ctx, FederationNodeRecord{
		NodeID: nodeID, PublicKey: publicKey, BaseURL: "https://integrity.example/gonzbnet/v1", Status: "known",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTrustPool(ctx, TrustPoolRecord{
		PoolID: poolID, DisplayName: "Integrity", PolicyJSON: json.RawMessage(`{}`),
		MembershipThreshold: 1, ModerationThreshold: 1, CheckpointWitnessThreshold: 1,
		AcceptMode: "pool_member", AcceptedEventTypes: []string{pools.EventTypeReleaseCard, manifest.Type, moderation.Type}, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertPoolMember(ctx, PoolMemberRecord{
		PoolID: poolID, NodeID: nodeID, Role: pools.RoleAdmin, Status: pools.StatusActive,
		AllowedCapabilities: []string{capability.ReleasePublisher, capability.ManifestBuilder, capability.Admin},
	}); err != nil {
		t.Fatal(err)
	}

	postedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	local := releasecard.LocalRelease{
		LocalReleaseID: "integrity-local", Title: "Integrity Release", SizeBytes: 1000,
		PostedAt: &postedAt, AddedAt: &postedAt, FileCount: 1,
		Groups: []string{"alt.binaries.integrity"}, SourceKind: "local_indexer_cache",
		Files: []releasecard.LocalFile{{
			Name: "integrity.rar", Subject: "Integrity Release integrity.rar yEnc",
			Poster: "poster@example.invalid", PostedAt: &postedAt, SizeBytes: 1000,
			FileIndex: 1, ArticleCount: 1, TotalParts: 1,
			Segments: []releasecard.LocalSegment{{Number: 1, Bytes: 1000, MessageID: "<integrity@example.invalid>"}},
		}},
	}
	card, err := releasecard.MapLocalRelease(local)
	if err != nil {
		t.Fatal(err)
	}
	cardEvent, cardValidation, err := events.Create(ctx, node, events.CreateOptions{
		EventType: pools.EventTypeReleaseCard, Sequence: 1, CreatedAt: time.Now().UTC(),
		PoolIDs: []string{poolID}, Visibility: "pool", BodySchema: releasecard.BodySchema, Body: card,
	})
	if err != nil || cardValidation == nil || !cardValidation.OK {
		t.Fatalf("create card event: validation=%+v err=%v", cardValidation, err)
	}
	if err := store.AppendVerifiedFederationEventWithProjection(ctx, cardEvent, cardValidation, func(projectCtx context.Context) error {
		return store.UpsertFederatedReleaseCardProjection(projectCtx, releasecard.Projection{
			Card: card, EventID: cardEvent.EventID, SourceNodeID: nodeID, PoolID: poolID,
		})
	}); err != nil {
		t.Fatal(err)
	}

	page, err := store.ListFederationReleaseLedger(ctx, FederationReleaseLedgerParams{PoolID: poolID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].EffectiveState != "active" || !page.Items[0].ProjectionMatchesSigned {
		t.Fatalf("unexpected active ledger: %+v", page)
	}
	searchItems, err := store.SearchFederatedReleaseCards(ctx, FederatedReleaseCardSearchParams{Pools: []string{poolID}, Limit: 10})
	if err != nil || len(searchItems) != 1 || searchItems[0].ReleaseID != card.ReleaseID {
		t.Fatalf("unexpected eligible search result: items=%+v err=%v", searchItems, err)
	}
	roleID := "integrity-reader"
	if err := store.UpsertFederationRolePoolAccess(ctx, FederationRolePoolAccessRecord{
		RoleID: roleID, PoolID: poolID, CanSearch: true, CanGet: true, CanResolveManifest: true,
	}); err != nil {
		t.Fatal(err)
	}
	canGet, err := store.CanGetFederatedReleaseForPrincipal(ctx, card.ReleaseID, "", []string{roleID})
	if err != nil || !canGet {
		t.Fatalf("expected eligible release get, allowed=%v err=%v", canGet, err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE federated_release_cards SET title = 'Tampered title' WHERE release_id = $1`, card.ReleaseID); err != nil {
		t.Fatal(err)
	}
	page, err = store.ListFederationReleaseLedger(ctx, FederationReleaseLedgerParams{PoolID: poolID, State: "projection_mismatch", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ProjectionMatchesSigned {
		t.Fatalf("expected projection mismatch, got %+v", page)
	}
	searchItems, err = store.SearchFederatedReleaseCards(ctx, FederatedReleaseCardSearchParams{Pools: []string{poolID}, Limit: 10})
	if err != nil || len(searchItems) != 0 {
		t.Fatalf("expected tampered title to be excluded from search, items=%+v err=%v", searchItems, err)
	}
	canGet, err = store.CanGetFederatedReleaseForPrincipal(ctx, card.ReleaseID, "", []string{roleID})
	if err != nil || canGet {
		t.Fatalf("expected tampered title to be excluded from get, allowed=%v err=%v", canGet, err)
	}
	manifestSource, err := store.FindFederatedManifestSource(ctx, card.ReleaseID)
	if err != nil || manifestSource != nil {
		t.Fatalf("expected tampered title to suppress manifest source, source=%+v err=%v", manifestSource, err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE federated_release_cards SET title = $2 WHERE release_id = $1`, card.ReleaseID, card.Title); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE federated_release_cards SET size_bytes = size_bytes + 1 WHERE release_id = $1`, card.ReleaseID); err != nil {
		t.Fatal(err)
	}
	page, err = store.ListFederationReleaseLedger(ctx, FederationReleaseLedgerParams{PoolID: poolID, State: "projection_mismatch", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected signed detail mismatch, got %+v", page)
	}
	searchItems, err = store.SearchFederatedReleaseCards(ctx, FederatedReleaseCardSearchParams{Pools: []string{poolID}, Limit: 10})
	if err != nil || len(searchItems) != 0 {
		t.Fatalf("expected tampered details to be excluded from search, items=%+v err=%v", searchItems, err)
	}
	canGet, err = store.CanGetFederatedReleaseForPrincipal(ctx, card.ReleaseID, "", []string{roleID})
	if err != nil || canGet {
		t.Fatalf("expected tampered details to be excluded from get, allowed=%v err=%v", canGet, err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE federated_release_cards SET size_bytes = $2 WHERE release_id = $1`, card.ReleaseID, card.SizeBytes); err != nil {
		t.Fatal(err)
	}
	searchItems, err = store.SearchFederatedReleaseCards(ctx, FederatedReleaseCardSearchParams{Pools: []string{poolID}, Limit: 10})
	if err != nil || len(searchItems) != 1 {
		t.Fatalf("expected restored signed projection in search, items=%+v err=%v", searchItems, err)
	}
	canGet, err = store.CanGetFederatedReleaseForPrincipal(ctx, card.ReleaseID, "", []string{roleID})
	if err != nil || !canGet {
		t.Fatalf("expected restored signed projection for get, allowed=%v err=%v", canGet, err)
	}

	core, err := releasecard.ManifestCoreForLocalRelease(local)
	if err != nil {
		t.Fatal(err)
	}
	manifestBody := manifest.ResolutionManifest{
		SchemaVersion: "1.0", Type: manifest.Type, ManifestID: card.ManifestID,
		ReleaseID: card.ReleaseID, ManifestCore: core,
	}
	manifestEvent, manifestValidation, err := events.Create(ctx, node, events.CreateOptions{
		EventType: manifest.Type, Sequence: 2, PreviousEventID: &cardEvent.EventID,
		CreatedAt: time.Now().UTC(), PoolIDs: []string{poolID}, Visibility: "pool",
		BodySchema: manifest.BodySchema, Body: manifestBody,
	})
	if err != nil || manifestValidation == nil || !manifestValidation.OK {
		t.Fatalf("create manifest event: validation=%+v err=%v", manifestValidation, err)
	}
	if err := store.AppendVerifiedFederationEvent(ctx, manifestEvent, manifestValidation); err != nil {
		t.Fatal(err)
	}
	nzb, err := manifest.GenerateNZB(manifestBody)
	if err != nil {
		t.Fatal(err)
	}
	tamperedManifest := manifestBody
	tamperedManifest.ReleaseID = "rel_tampered"
	if err := store.StoreResolutionManifest(ctx, ResolutionManifestRecord{
		Manifest: tamperedManifest, SourceNodeID: nodeID, FetchedFromNodeID: nodeID,
		SourceEventID: manifestEvent.EventID, PoolID: poolID, GeneratedNZB: nzb,
	}); err == nil {
		t.Fatal("expected manifest record that differs from its signed source event to be rejected")
	}
	if err := store.StoreResolutionManifest(ctx, ResolutionManifestRecord{
		Manifest: manifestBody, SourceNodeID: nodeID, FetchedFromNodeID: nodeID,
		SourceEventID: manifestEvent.EventID, PoolID: poolID, GeneratedNZB: nzb,
	}); err != nil {
		t.Fatal(err)
	}
	allowed, err := store.CanFetchResolutionManifestForSource(ctx, card.ManifestID, card.ReleaseID, poolID, nodeID)
	if err != nil || !allowed {
		t.Fatalf("expected exact manifest request bindings to be authorized, allowed=%v err=%v", allowed, err)
	}
	for _, mismatch := range []struct{ manifestID, releaseID, poolID string }{
		{card.ManifestID, "rel_wrong", poolID},
		{card.ManifestID, card.ReleaseID, "pool.wrong"},
	} {
		allowed, err = store.CanFetchResolutionManifestForSource(ctx, mismatch.manifestID, mismatch.releaseID, mismatch.poolID, nodeID)
		if err != nil || allowed {
			t.Fatalf("expected mismatched manifest request to be rejected, allowed=%v err=%v", allowed, err)
		}
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE federation_nodes SET status = 'blocked' WHERE node_id = $1`, nodeID); err != nil {
		t.Fatal(err)
	}
	searchItems, err = store.SearchFederatedReleaseCards(ctx, FederatedReleaseCardSearchParams{Pools: []string{poolID}, Limit: 10})
	if err != nil || len(searchItems) != 0 {
		t.Fatalf("expected blocked publisher to leave search immediately, items=%+v err=%v", searchItems, err)
	}
	if cached, ok, err := store.GetCachedFederatedNZBByReleaseID(ctx, card.ReleaseID); err != nil || ok || cached != nil {
		t.Fatalf("expected blocked publisher cache suppression, ok=%v bytes=%d err=%v", ok, len(cached), err)
	}
	page, err = store.ListFederationReleaseLedger(ctx, FederationReleaseLedgerParams{PoolID: poolID, State: "blocked", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("expected blocked publisher in release ledger, page=%+v err=%v", page, err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE federation_nodes SET status = 'known' WHERE node_id = $1`, nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE resolution_manifests SET generated_nzb = 'tampered'::bytea WHERE manifest_id = $1`, card.ManifestID); err != nil {
		t.Fatal(err)
	}
	repaired, ok, err := store.GetCachedFederatedNZBByReleaseID(ctx, card.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !bytes.Equal(repaired, nzb) {
		t.Fatalf("expected deterministic cache repair, ok=%v bytes=%d", ok, len(repaired))
	}
	var integrityStatus string
	if err := store.DB().QueryRowContext(ctx, `SELECT cache_integrity_status FROM resolution_manifests WHERE manifest_id = $1`, card.ManifestID).Scan(&integrityStatus); err != nil {
		t.Fatal(err)
	}
	if integrityStatus != "verified" {
		t.Fatalf("cache integrity status=%q", integrityStatus)
	}

	tombstoneBody := moderation.Tombstone{
		SchemaVersion: "1.0", Type: moderation.Type,
		TargetType: moderation.TargetEvent, TargetID: cardEvent.EventID, PoolID: poolID,
		Reason: "integrity test", Severity: moderation.SeverityReject,
		EffectiveAt: time.Now().UTC().Format(time.RFC3339),
	}
	tombstoneEvent, tombstoneValidation, err := events.Create(ctx, node, events.CreateOptions{
		EventType: moderation.Type, Sequence: 3, PreviousEventID: &manifestEvent.EventID,
		CreatedAt: time.Now().UTC(), PoolIDs: []string{poolID}, Visibility: "pool",
		BodySchema: moderation.BodySchema, Body: tombstoneBody,
	})
	if err != nil || tombstoneValidation == nil || !tombstoneValidation.OK {
		t.Fatalf("create tombstone event: validation=%+v err=%v", tombstoneValidation, err)
	}
	if err := store.AppendVerifiedFederationEventWithProjection(ctx, tombstoneEvent, tombstoneValidation, func(projectCtx context.Context) error {
		return store.ProjectTombstone(projectCtx, TombstoneProjection{
			Tombstone: tombstoneBody, EventID: tombstoneEvent.EventID, AuthorNodeID: nodeID,
		})
	}); err != nil {
		t.Fatal(err)
	}
	tombstoned := true
	eventItems, err := store.ListFederationEventDiagnostics(ctx, FederationEventDiagnosticParams{
		PoolID: poolID, NodeID: nodeID, EventType: pools.EventTypeReleaseCard,
		Tombstoned: &tombstoned, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(eventItems) != 1 || eventItems[0].EventID != cardEvent.EventID || !eventItems[0].Tombstoned {
		t.Fatalf("unexpected tombstoned event diagnostics: %+v", eventItems)
	}
	page, err = store.ListFederationReleaseLedger(ctx, FederationReleaseLedgerParams{PoolID: poolID, State: "tombstoned", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TombstoneSourceEventID != tombstoneEvent.EventID {
		t.Fatalf("unexpected tombstoned release ledger: %+v", page)
	}
	allowed, err = store.CanFetchResolutionManifestForSource(ctx, card.ManifestID, card.ReleaseID, poolID, nodeID)
	if err != nil || allowed {
		t.Fatalf("expected source-event tombstone to suppress manifest serving, allowed=%v err=%v", allowed, err)
	}
	searchItems, err = store.SearchFederatedReleaseCards(ctx, FederatedReleaseCardSearchParams{Pools: []string{poolID}, Limit: 10})
	if err != nil || len(searchItems) != 0 {
		t.Fatalf("expected tombstoned source to leave search immediately, items=%+v err=%v", searchItems, err)
	}
	canGet, err = store.CanGetFederatedReleaseForPrincipal(ctx, card.ReleaseID, "", []string{roleID})
	if err != nil || canGet {
		t.Fatalf("expected tombstoned source get rejection, allowed=%v err=%v", canGet, err)
	}
}
