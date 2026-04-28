package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
	settingsstore "github.com/datallboy/gonzb/internal/store/settings"
	"github.com/labstack/echo/v5"
)

func TestAuthSetupAndRBACFlow(t *testing.T) {
	e := echo.New()
	appCtx := newAuthTestAppContext(t)
	RegisterRoutes(e, appCtx)

	sessionResp := performJSONRequest(t, e, http.MethodGet, "/api/v1/auth/session", nil, nil, "")
	if sessionResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", sessionResp.Code)
	}
	var sessionBody struct {
		Session struct {
			Authenticated bool     `json:"authenticated"`
			SetupRequired bool     `json:"setup_required"`
			Permissions   []string `json:"permissions"`
		} `json:"session"`
	}
	mustDecodeJSON(t, sessionResp, &sessionBody)
	if sessionBody.Session.Authenticated || !sessionBody.Session.SetupRequired {
		t.Fatalf("unexpected initial session payload: %s", sessionResp.Body.String())
	}

	loginResp := performJSONRequest(t, e, http.MethodPost, "/api/v1/auth/session", map[string]string{
		"username": "admin",
		"password": "admin",
	}, nil, "")
	if loginResp.Code != http.StatusConflict {
		t.Fatalf("expected setup-required login conflict, got %d body=%s", loginResp.Code, loginResp.Body.String())
	}

	adminUsersResp := performJSONRequest(t, e, http.MethodGet, "/api/v1/admin/auth/users", nil, nil, "")
	if adminUsersResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated admin list, got %d", adminUsersResp.Code)
	}

	setupResp := performJSONRequest(t, e, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "owner",
		"password": "very-secure-pass",
	}, nil, "")
	if setupResp.Code != http.StatusCreated {
		t.Fatalf("expected 201 for initial setup, got %d body=%s", setupResp.Code, setupResp.Body.String())
	}
	setupCookies := cookieMap(setupResp.Result().Cookies())
	sessionCookie := setupCookies["gonzb_session"]
	csrfCookie := setupCookies["gonzb_csrf"]
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("expected session and csrf cookies after setup")
	}

	repeatSetupResp := performJSONRequest(t, e, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "owner2",
		"password": "another-secure-pass",
	}, nil, "")
	if repeatSetupResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 after setup completion, got %d", repeatSetupResp.Code)
	}

	usersResp := performJSONRequest(t, e, http.MethodGet, "/api/v1/admin/auth/users", nil, []*http.Cookie{sessionCookie}, "")
	if usersResp.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin users list, got %d body=%s", usersResp.Code, usersResp.Body.String())
	}

	createViewerResp := performJSONRequest(t, e, http.MethodPost, "/api/v1/admin/auth/users", map[string]any{
		"username": "viewer1",
		"password": "viewer-secure-pass",
		"enabled":  true,
		"role_ids": []string{"viewer"},
	}, []*http.Cookie{sessionCookie, csrfCookie}, csrfCookie.Value)
	if createViewerResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating viewer user, got %d body=%s", createViewerResp.Code, createViewerResp.Body.String())
	}

	createOperatorResp := performJSONRequest(t, e, http.MethodPost, "/api/v1/admin/auth/users", map[string]any{
		"username": "operator1",
		"password": "operator-secure-pass",
		"enabled":  true,
		"role_ids": []string{"operator"},
	}, []*http.Cookie{sessionCookie, csrfCookie}, csrfCookie.Value)
	if createOperatorResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating operator user, got %d body=%s", createOperatorResp.Code, createOperatorResp.Body.String())
	}

	viewerLoginResp := performJSONRequest(t, e, http.MethodPost, "/api/v1/auth/session", map[string]string{
		"username": "viewer1",
		"password": "viewer-secure-pass",
	}, nil, "")
	if viewerLoginResp.Code != http.StatusOK {
		t.Fatalf("expected viewer login success, got %d body=%s", viewerLoginResp.Code, viewerLoginResp.Body.String())
	}
	viewerCookies := cookieMap(viewerLoginResp.Result().Cookies())
	viewerSession := viewerCookies["gonzb_session"]
	viewerCSRF := viewerCookies["gonzb_csrf"]
	if viewerSession == nil || viewerCSRF == nil {
		t.Fatalf("expected viewer auth cookies")
	}

	viewerAdminResp := performJSONRequest(t, e, http.MethodGet, "/api/v1/admin/auth/users", nil, []*http.Cookie{viewerSession}, "")
	if viewerAdminResp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer on admin auth users, got %d body=%s", viewerAdminResp.Code, viewerAdminResp.Body.String())
	}

	viewerTokenResp := performJSONRequest(t, e, http.MethodPost, "/api/v1/auth/tokens", map[string]string{
		"name": "viewer-cli",
	}, []*http.Cookie{viewerSession, viewerCSRF}, viewerCSRF.Value)
	if viewerTokenResp.Code != http.StatusOK {
		t.Fatalf("expected viewer token create success, got %d body=%s", viewerTokenResp.Code, viewerTokenResp.Body.String())
	}
	var tokenBody struct {
		Secret string `json:"secret"`
	}
	mustDecodeJSON(t, viewerTokenResp, &tokenBody)
	if tokenBody.Secret == "" {
		t.Fatalf("expected token secret in response")
	}

	bearerResp := performJSONRequest(t, e, http.MethodGet, "/api/v1/auth/tokens", nil, nil, "Bearer "+tokenBody.Secret)
	if bearerResp.Code != http.StatusOK {
		t.Fatalf("expected bearer token auth success, got %d body=%s", bearerResp.Code, bearerResp.Body.String())
	}

	operatorLoginResp := performJSONRequest(t, e, http.MethodPost, "/api/v1/auth/session", map[string]string{
		"username": "operator1",
		"password": "operator-secure-pass",
	}, nil, "")
	if operatorLoginResp.Code != http.StatusOK {
		t.Fatalf("expected operator login success, got %d body=%s", operatorLoginResp.Code, operatorLoginResp.Body.String())
	}
	operatorCookies := cookieMap(operatorLoginResp.Result().Cookies())
	operatorAdminResp := performJSONRequest(t, e, http.MethodGet, "/api/v1/admin/auth/users", nil, []*http.Cookie{operatorCookies["gonzb_session"]}, "")
	if operatorAdminResp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for operator on admin auth users, got %d body=%s", operatorAdminResp.Code, operatorAdminResp.Body.String())
	}
}

func newAuthTestAppContext(t *testing.T) *app.Context {
	t.Helper()
	dir := t.TempDir()
	log, err := logger.New(filepath.Join(dir, "test.log"), logger.LevelError, false)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	store, err := settingsstore.NewStore(filepath.Join(dir, "settings.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &app.Context{
		Config: &config.Config{
			API: config.APIConfig{
				CORSAllowedOrigins: []string{"http://localhost:5173"},
			},
			Modules: config.ModulesConfig{
				API:   config.ModuleToggle{Enabled: true},
				WebUI: config.ModuleToggle{Enabled: true},
			},
		},
		BootstrapConfig: &config.Config{},
		Logger:          log,
		SettingsStore:   store,
	}
}

func performJSONRequest(t *testing.T, e *echo.Echo, method, path string, body any, cookies []*http.Cookie, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	var rawBody []byte
	if body != nil {
		var err error
		rawBody, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(rawBody))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		if len(bearer) > 7 && bearer[:7] == "Bearer " {
			req.Header.Set("Authorization", bearer)
		} else {
			req.Header.Set("X-CSRF-Token", bearer)
		}
	}
	for _, cookie := range cookies {
		if cookie != nil {
			req.AddCookie(cookie)
		}
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func mustDecodeJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode json: %v body=%s", err, rec.Body.String())
	}
}

func cookieMap(cookies []*http.Cookie) map[string]*http.Cookie {
	out := make(map[string]*http.Cookie, len(cookies))
	for _, cookie := range cookies {
		out[cookie.Name] = cookie
	}
	return out
}
