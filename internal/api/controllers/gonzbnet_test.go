package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
