package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/labstack/echo/v5"
)

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
