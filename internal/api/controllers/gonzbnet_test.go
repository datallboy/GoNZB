package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/admission"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

type invitationAdminStore struct {
	active bool
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
