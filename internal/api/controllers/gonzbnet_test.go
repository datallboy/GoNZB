package controllers

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/admission"
	"github.com/datallboy/gonzb/internal/gonzbnet/capability"
	"github.com/datallboy/gonzb/internal/gonzbnet/evidence"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/requestauth"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

type invitationAdminStore struct {
	active bool
}

type evidenceEndpointStore struct {
	keys       map[string]ed25519.PublicKey
	members    map[string]bool
	nonces     map[string]struct{}
	headers    []pgindex.YEncEvidenceRecord
	diagnostic pgindex.BinaryEvidenceDiagnostic
}

func (s *evidenceEndpointStore) GetFederationNodePublicKey(_ context.Context, nodeID string) (ed25519.PublicKey, error) {
	key := s.keys[nodeID]
	if len(key) == 0 {
		return nil, fmt.Errorf("unknown node")
	}
	return key, nil
}

func (s *evidenceEndpointStore) StoreFederationNonce(_ context.Context, nodeID, nonce string, _ time.Time) (bool, error) {
	key := nodeID + "\x00" + nonce
	if _, exists := s.nonces[key]; exists {
		return false, nil
	}
	s.nonces[key] = struct{}{}
	return true, nil
}

func (*evidenceEndpointStore) GetTrustPoolPolicy(context.Context, string) (pools.PoolPolicy, error) {
	return pools.PoolPolicy{AllowBinaryEvidenceExchange: true}, nil
}

func (s *evidenceEndpointStore) IsActivePoolMemberWithCapability(_ context.Context, _ string, nodeID, required string) (bool, error) {
	return required == capability.BinaryEvidenceExchange && s.members[nodeID], nil
}

func (s *evidenceEndpointStore) FindAcceptedYEncEvidence(context.Context, []string, bool, int) ([]pgindex.YEncEvidenceRecord, error) {
	return append([]pgindex.YEncEvidenceRecord(nil), s.headers...), nil
}

func (*evidenceEndpointStore) RefreshBinaryExchangeIdentities(context.Context, int) (int, error) {
	return 0, nil
}

func (*evidenceEndpointStore) FindLocalBinarySegments(context.Context, string, string, []int, []string, int) ([]evidence.Segment, error) {
	return nil, nil
}

func (s *evidenceEndpointStore) RecordBinaryEvidenceDiagnostic(_ context.Context, item pgindex.BinaryEvidenceDiagnostic) error {
	s.diagnostic = item
	return nil
}

func (s invitationAdminStore) IsActivePoolAdmin(context.Context, string, string) (bool, error) {
	return s.active, nil
}

func TestPoolInvitationAuthorizesOnlyMatchingActiveAdmin(t *testing.T) {
	pool := pgindex.TrustPoolRecord{PoolID: "pool.private", GenesisEventID: "evt_genesis"}
	invite := &admission.Invitation{
		PoolID: "pool.private", GenesisEventID: "evt_genesis",
		RelayURL: "https://relay.example/gonzbnet/v1", CreatedByNode: "node_admin",
	}
	if !poolInvitationAuthorizes(t.Context(), invitationAdminStore{active: true}, invite, pool, "https://relay.example/gonzbnet/v1/") {
		t.Fatal("expected matching invitation from active admin to authorize private descriptor")
	}
	invite.GenesisEventID = "evt_other"
	if poolInvitationAuthorizes(t.Context(), invitationAdminStore{active: true}, invite, pool, "https://relay.example/gonzbnet/v1") {
		t.Fatal("expected mismatched pool fingerprint to fail")
	}
	invite.GenesisEventID = pool.GenesisEventID
	if poolInvitationAuthorizes(t.Context(), invitationAdminStore{active: false}, invite, pool, "https://relay.example/gonzbnet/v1") {
		t.Fatal("expected inactive invitation signer to fail")
	}
}

func TestDistinctPoolMemberCountCountsActiveNodesOnce(t *testing.T) {
	members := []pgindex.PoolMemberRecord{
		{NodeID: "node_a", Role: pools.RoleAdmin, Status: pools.StatusActive},
		{NodeID: "node_a", Role: pools.RoleWitness, Status: pools.StatusActive},
		{NodeID: "node_b", Role: pools.RoleMember, Status: pools.StatusActive},
		{NodeID: "node_c", Role: pools.RoleMember, Status: pools.StatusRevoked},
	}
	if got := distinctPoolMemberCount(members); got != 2 {
		t.Fatalf("expected two distinct active nodes, got %d", got)
	}
}

func TestGoNZBNetHandshakeInvalidJSONUsesStableErrorCode(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/gonzbnet/v1/handshake", strings.NewReader(`{`))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	ctrl := NewGoNZBNetController(&app.Context{
		Config: &config.Config{
			GoNZBNet: config.GoNZBNetConfig{
				KeysDir: t.TempDir(),
			},
		},
	})

	if err := ctrl.Handshake(c); err != nil {
		t.Fatalf("Handshake returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
	var body federationErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != "invalid_json" || body.Error != "invalid_json" {
		t.Fatalf("expected invalid_json response, got %+v", body)
	}
}

func TestGoNZBNetHandshakeRejectsStaleTimestampBeforePersistence(t *testing.T) {
	e := echo.New()
	body := fmt.Sprintf(`{
		"schema_version":"1.0",
		"type":"HandshakeRequest",
		"node_id":"node_stale",
		"public_key":"ignored",
		"nonce":"ignored",
		"supported_versions":["gonzbnet/1.0"],
		"requested_pools":[],
		"created_at":%q,
		"signature":"present"
	}`, time.Now().UTC().Add(-10*time.Minute).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodPost, "/gonzbnet/v1/handshake", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ctrl := NewGoNZBNetController(&app.Context{
		Config: &config.Config{GoNZBNet: config.GoNZBNetConfig{
			KeysDir:              t.TempDir(),
			TimeToleranceSeconds: 30,
		}},
	})

	if err := ctrl.Handshake(e.NewContext(req, rec)); err != nil {
		t.Fatalf("Handshake returned error: %v", err)
	}
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"code":"expired_event"`) {
		t.Fatalf("expected expired_event status 401, got status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFederationGossipReadLimitIsBoundedFromConfiguration(t *testing.T) {
	cfg := config.GoNZBNetConfig{MaxEventBytes: 1024, GossipBatchSize: 5}
	if got, want := federationGossipReadLimit(cfg), int64(5*1024+64*1024); got != want {
		t.Fatalf("expected read limit %d, got %d", want, got)
	}
}

func TestYEncEvidenceEndpointExchangesSignedDataAndRejectsReplay(t *testing.T) {
	ctx := context.Background()
	keysDir := t.TempDir()
	local, err := identity.LoadOrCreate(keysDir)
	if err != nil {
		t.Fatal(err)
	}
	requester, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	localID, _ := local.NodeID(ctx)
	requesterID, _ := requester.NodeID(ctx)
	requesterKey, _ := requester.PublicKey(ctx)
	now := time.Now().UTC()
	store := &evidenceEndpointStore{
		keys:    map[string]ed25519.PublicKey{requesterID: requesterKey},
		members: map[string]bool{localID: true, requesterID: true},
		nonces:  map[string]struct{}{},
		headers: []pgindex.YEncEvidenceRecord{{
			SourcePostedAt: now, MessageID: "<part@example>",
			FileName: "movie.mkv", PartNumber: 2, TotalParts: 10, FileSize: 1234,
		}},
	}
	ctrl := NewGoNZBNetController(&app.Context{
		Config: &config.Config{GoNZBNet: config.GoNZBNetConfig{
			KeysDir:                        keysDir,
			BinaryEvidenceServeEnabled:     true,
			BinaryEvidenceYEncBatchSize:    100,
			BinaryEvidenceMaxResponseBytes: 1024 * 1024,
		}},
	})
	ctrl.evidenceStoreOverride = store
	query := evidence.YEncQuery{
		SchemaVersion: evidence.SchemaVersion, Type: evidence.YEncQueryType,
		RequestID: "req_endpoint_test", PoolID: "pool.test",
		RequestingNodeID: requesterID, MessageIDs: []string{"<part@example>"},
		CreatedAt: now.Format(time.RFC3339),
	}
	payload, err := json.Marshal(query)
	if err != nil {
		t.Fatal(err)
	}
	const path = "/gonzbnet/v1/evidence/yenc/query"
	authorization, err := requestauth.Sign(ctx, requester, http.MethodPost, path, "", payload, now)
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(body []byte, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
		req.Header.Set("Authorization", auth)
		rec := httptest.NewRecorder()
		if err := ctrl.QueryYEncEvidence(echo.New().NewContext(req, rec)); err != nil {
			t.Fatalf("query evidence: %v", err)
		}
		return rec
	}

	rec := invoke(payload, authorization)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected evidence response 200, got status=%d body=%s", rec.Code, rec.Body.String())
	}
	var bundle evidence.Bundle
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	localKey, _ := local.PublicKey(ctx)
	if err := evidence.VerifyBundle(bundle, localKey, query.PoolID, query.RequestID, requesterID, time.Now().UTC()); err != nil {
		t.Fatalf("verify response bundle: %v", err)
	}
	if len(bundle.YEncHeaders) != 1 || bundle.YEncHeaders[0].MessageID != "<part@example>" {
		t.Fatalf("unexpected evidence bundle: %+v", bundle)
	}
	if store.diagnostic.HitCount != 1 || store.diagnostic.PeerNodeID != requesterID {
		t.Fatalf("unexpected serve diagnostic: %+v", store.diagnostic)
	}

	rec = invoke(payload, authorization)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"code":"replayed_nonce"`) {
		t.Fatalf("expected replay rejection, got status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestYEncEvidenceEndpointRequiresPoolCapability(t *testing.T) {
	ctx := context.Background()
	keysDir := t.TempDir()
	local, err := identity.LoadOrCreate(keysDir)
	if err != nil {
		t.Fatal(err)
	}
	requester, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	localID, _ := local.NodeID(ctx)
	requesterID, _ := requester.NodeID(ctx)
	requesterKey, _ := requester.PublicKey(ctx)
	store := &evidenceEndpointStore{
		keys: map[string]ed25519.PublicKey{requesterID: requesterKey},
		// The local node may serve evidence, but the requester lacks the
		// explicit pool capability.
		members: map[string]bool{localID: true},
		nonces:  map[string]struct{}{},
	}
	ctrl := NewGoNZBNetController(&app.Context{
		Config: &config.Config{GoNZBNet: config.GoNZBNetConfig{
			KeysDir: keysDir, BinaryEvidenceServeEnabled: true,
		}},
	})
	ctrl.evidenceStoreOverride = store
	query := evidence.YEncQuery{
		SchemaVersion: evidence.SchemaVersion, Type: evidence.YEncQueryType,
		RequestID: "req_forbidden", PoolID: "pool.test",
		RequestingNodeID: requesterID, MessageIDs: []string{"<part@example>"},
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	payload, _ := json.Marshal(query)
	const path = "/gonzbnet/v1/evidence/yenc/query"
	authorization, err := requestauth.Sign(ctx, requester, http.MethodPost, path, "", payload, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(payload)))
	req.Header.Set("Authorization", authorization)
	rec := httptest.NewRecorder()
	if err := ctrl.QueryYEncEvidence(echo.New().NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"evidence_exchange_disabled"`) {
		t.Fatalf("expected capability rejection, got status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoNZBNetPeersDisabledReturnsEmptyListWithoutStore(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/gonzbnet/v1/peers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	ctrl := NewGoNZBNetController(&app.Context{
		Config: &config.Config{
			GoNZBNet: config.GoNZBNetConfig{
				PeerExchangeEnabled: false,
			},
		},
	})

	if err := ctrl.Peers(c); err != nil {
		t.Fatalf("Peers returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body peersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Type != "PeerList" || len(body.Peers) != 0 {
		t.Fatalf("expected empty PeerList, got %+v", body)
	}
}
