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
