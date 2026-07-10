package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/labstack/echo/v5"
)

func TestRegisterRoutesDownloaderOnly(t *testing.T) {
	e := echo.New()
	appCtx := &app.Context{
		Config: &config.Config{
			API: config.APIConfig{
				CORSAllowedOrigins: []string{"http://localhost:5173"},
			},
			Modules: config.ModulesConfig{
				API:        config.ModuleToggle{Enabled: true},
				Downloader: config.ModuleToggle{Enabled: true},
			},
		},
	}

	RegisterRoutes(e, appCtx)
	routes := routePaths(e)

	assertRoutePresent(t, routes, "/api/v1/queue")
	assertRoutePresent(t, routes, "/api/sab")
	assertRoutePresent(t, routes, "/api/v1/events/queue")
	assertRouteMissing(t, routes, "/api/v1/releases/search")
	assertRouteMissing(t, routes, "/nzb/:id")
}

func TestRegisterRoutesAggregatorOnly(t *testing.T) {
	e := echo.New()
	appCtx := &app.Context{
		Config: &config.Config{
			API: config.APIConfig{
				CORSAllowedOrigins: []string{"http://localhost:5173"},
			},
			Modules: config.ModulesConfig{
				API:        config.ModuleToggle{Enabled: true},
				Aggregator: config.ModuleToggle{Enabled: true},
			},
		},
	}

	RegisterRoutes(e, appCtx)
	routes := routePaths(e)

	assertRoutePresent(t, routes, "/api/v1/releases/search")
	assertRoutePresent(t, routes, "/nzb/:id")
	assertRouteMissing(t, routes, "/api/v1/queue")
	assertRouteMissing(t, routes, "/api/sab")
}

func TestRegisterRoutesIndexerOnly(t *testing.T) {
	e := echo.New()
	appCtx := &app.Context{
		Config: &config.Config{
			API: config.APIConfig{
				CORSAllowedOrigins: []string{"http://localhost:5173"},
			},
			Modules: config.ModulesConfig{
				API:           config.ModuleToggle{Enabled: true},
				UsenetIndexer: config.ModuleToggle{Enabled: true},
			},
		},
	}

	RegisterRoutes(e, appCtx)
	routes := routePaths(e)

	assertRoutePresent(t, routes, "/api/v1/indexer/overview")
	assertRoutePresent(t, routes, "/api/v1/indexer/stages")
	assertRoutePresent(t, routes, "/api/v1/indexer/runs")
	assertRoutePresent(t, routes, "/api/v1/indexer/stages/:stage/run")
	assertRoutePresent(t, routes, "/api/v1/indexer/stages/:stage/pause")
	assertRoutePresent(t, routes, "/api/v1/indexer/stages/:stage/resume")
	assertRoutePresent(t, routes, "/api/v1/indexer/releases")
	assertRoutePresent(t, routes, "/api/v1/indexer/releases/:id")
	assertRoutePresent(t, routes, "/api/v1/indexer/binaries/:id")
	assertRoutePresent(t, routes, "/api/v1/indexer/files/:id")
	assertRoutePresent(t, routes, "/api/v1/admin/indexer/overview")
	assertRoutePresent(t, routes, "/api/v1/admin/indexer/stages")
	assertRoutePresent(t, routes, "/api/v1/admin/indexer/stages/:stage")
	assertRoutePresent(t, routes, "/api/v1/admin/indexer/runs")
	assertRoutePresent(t, routes, "/api/v1/admin/indexer/attention")
	assertRoutePresent(t, routes, "/api/v1/admin/indexer/stages/:stage/actions/run")
	assertRoutePresent(t, routes, "/api/v1/admin/indexer/stages/:stage/actions/pause")
	assertRoutePresent(t, routes, "/api/v1/admin/indexer/stages/:stage/actions/resume")
	assertRouteMissing(t, routes, "/api/v1/releases/search")
	assertRouteMissing(t, routes, "/api/v1/queue")
}

func TestRegisterRoutesGoNZBNetOnly(t *testing.T) {
	e := echo.New()
	appCtx := &app.Context{
		Config: &config.Config{
			API: config.APIConfig{
				CORSAllowedOrigins: []string{"http://localhost:5173"},
			},
			Modules: config.ModulesConfig{
				API:      config.ModuleToggle{Enabled: true},
				GoNZBNet: config.ModuleToggle{Enabled: true},
			},
			GoNZBNet: config.GoNZBNetConfig{
				HTTPEnabled: true,
			},
		},
	}

	RegisterRoutes(e, appCtx)
	routes := routePaths(e)

	assertRoutePresent(t, routes, "/.well-known/gonzbnet")
	assertRoutePresent(t, routes, "/gonzbnet/v1/node")
	assertRoutePresent(t, routes, "/gonzbnet/v1/inbox")
	assertRoutePresent(t, routes, "/gonzbnet/v1/events/batch")
	assertRoutePresent(t, routes, "/gonzbnet/v1/coverage/groups")
	assertRoutePresent(t, routes, "/gonzbnet/v1/coverage/plan")
	assertRoutePresent(t, routes, "/gonzbnet/v1/coverage/work")
	assertRoutePresent(t, routes, "/gonzbnet/v1/coverage/claim")
	assertRoutePresent(t, routes, "/gonzbnet/v1/coverage/checkpoint")
	assertRoutePresent(t, routes, "/gonzbnet/v1/validation/request")
	assertRoutePresent(t, routes, "/gonzbnet/v1/capabilities/nodes")
	assertRoutePresent(t, routes, "/gonzbnet/v1/pools/:pool_id/checkpoint")
	assertRoutePresent(t, routes, "/gonzbnet/v1/pools/:pool_id/members")
	assertRoutePresent(t, routes, "/gonzbnet/v1/peers")
	assertRoutePresent(t, routes, "/api/v1/admin/gonzbnet/node/profile")
	assertRoutePresent(t, routes, "/api/v1/admin/gonzbnet/config/validation")
	assertRouteMissing(t, routes, "/api/v1/releases/search")
	assertRouteMissing(t, routes, "/api/v1/queue")
}

func TestRegisterRoutesGoNZBNetHTTPDisabledKeepsLocalAdmin(t *testing.T) {
	e := echo.New()
	appCtx := &app.Context{
		Config: &config.Config{
			API: config.APIConfig{
				CORSAllowedOrigins: []string{"http://localhost:5173"},
			},
			Modules: config.ModulesConfig{
				API:      config.ModuleToggle{Enabled: true},
				GoNZBNet: config.ModuleToggle{Enabled: true},
			},
			GoNZBNet: config.GoNZBNetConfig{
				HTTPEnabled: false,
			},
		},
	}

	RegisterRoutes(e, appCtx)
	routes := routePaths(e)

	assertRouteMissing(t, routes, "/.well-known/gonzbnet")
	assertRouteMissing(t, routes, "/gonzbnet/v1/node")
	assertRouteMissing(t, routes, "/gonzbnet/v1/inbox")
	assertRouteMissing(t, routes, "/gonzbnet/v1/events/batch")
	assertRouteMissing(t, routes, "/gonzbnet/v1/coverage/groups")
	assertRouteMissing(t, routes, "/gonzbnet/v1/coverage/plan")
	assertRouteMissing(t, routes, "/gonzbnet/v1/coverage/work")
	assertRouteMissing(t, routes, "/gonzbnet/v1/coverage/claim")
	assertRouteMissing(t, routes, "/gonzbnet/v1/coverage/checkpoint")
	assertRouteMissing(t, routes, "/gonzbnet/v1/validation/request")
	assertRouteMissing(t, routes, "/gonzbnet/v1/capabilities/nodes")
	assertRouteMissing(t, routes, "/gonzbnet/v1/pools/:pool_id/checkpoint")
	assertRouteMissing(t, routes, "/gonzbnet/v1/pools/:pool_id/members")
	assertRouteMissing(t, routes, "/gonzbnet/v1/peers")
	assertRoutePresent(t, routes, "/api/v1/admin/gonzbnet/node/profile")
	assertRoutePresent(t, routes, "/api/v1/admin/gonzbnet/config/validation")
}

func TestRegisterRoutesGoNZBNetDisabledOmitsFederationRoutes(t *testing.T) {
	e := echo.New()
	appCtx := &app.Context{
		Config: &config.Config{
			API: config.APIConfig{
				CORSAllowedOrigins: []string{"http://localhost:5173"},
			},
			Modules: config.ModulesConfig{
				API:      config.ModuleToggle{Enabled: true},
				GoNZBNet: config.ModuleToggle{Enabled: false},
			},
			GoNZBNet: config.GoNZBNetConfig{
				HTTPEnabled: true,
			},
		},
	}

	RegisterRoutes(e, appCtx)
	routes := routePaths(e)

	assertRouteMissing(t, routes, "/.well-known/gonzbnet")
	assertRouteMissing(t, routes, "/gonzbnet/v1/node")
	assertRouteMissing(t, routes, "/gonzbnet/v1/inbox")
	assertRouteMissing(t, routes, "/gonzbnet/v1/events/batch")
	assertRouteMissing(t, routes, "/gonzbnet/v1/coverage/groups")
	assertRouteMissing(t, routes, "/gonzbnet/v1/coverage/plan")
	assertRouteMissing(t, routes, "/gonzbnet/v1/coverage/work")
	assertRouteMissing(t, routes, "/gonzbnet/v1/coverage/claim")
	assertRouteMissing(t, routes, "/gonzbnet/v1/coverage/checkpoint")
	assertRouteMissing(t, routes, "/gonzbnet/v1/validation/request")
	assertRouteMissing(t, routes, "/gonzbnet/v1/capabilities/nodes")
	assertRouteMissing(t, routes, "/gonzbnet/v1/pools/:pool_id/checkpoint")
	assertRouteMissing(t, routes, "/gonzbnet/v1/pools/:pool_id/members")
	assertRouteMissing(t, routes, "/gonzbnet/v1/peers")
	assertRouteMissing(t, routes, "/api/v1/admin/gonzbnet/node/profile")
	assertRouteMissing(t, routes, "/api/v1/admin/gonzbnet/config/validation")
}

func TestFederationRateLimitReturnsStableErrorCode(t *testing.T) {
	e := echo.New()
	mw := federationRateLimitMiddleware(1)
	handler := mw(func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/gonzbnet/v1/inbox", strings.NewReader(`{}`))
	req.RemoteAddr = "192.0.2.1:12345"
	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("first request returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected first request status 200, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/gonzbnet/v1/inbox", strings.NewReader(`{}`))
	req.RemoteAddr = "192.0.2.1:12345"
	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("second request returned error: %v", err)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"rate_limited"`) {
		t.Fatalf("expected rate_limited code, got %s", rec.Body.String())
	}
}

func TestFederationRateLimitTemporarilyThrottlesFlooder(t *testing.T) {
	e := echo.New()
	mw := federationRateLimitMiddleware(1)
	handler := mw(func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	for i := 0; i < 4; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/gonzbnet/v1/inbox", strings.NewReader(`{}`))
		req.RemoteAddr = "192.0.2.2:12345"
		if err := handler(e.NewContext(req, rec)); err != nil {
			t.Fatalf("request %d returned error: %v", i+1, err)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/gonzbnet/v1/inbox", strings.NewReader(`{}`))
	req.RemoteAddr = "192.0.2.2:12345"
	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("throttled request returned error: %v", err)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"temporarily_throttled"`) {
		t.Fatalf("expected temporarily_throttled code, got %s", rec.Body.String())
	}
}

func TestFederationBodyLimitReturnsStableErrorCode(t *testing.T) {
	e := echo.New()
	mw := federationBodyLimitMiddleware(config.GoNZBNetConfig{
		MaxEventBytes:    1,
		MaxManifestBytes: 1,
		MaxBatchEvents:   1,
	})
	handler := mw(func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	body := strings.NewReader(strings.Repeat("a", int(defaultJSONBodyLimit)+1))
	req := httptest.NewRequest(http.MethodPost, "/gonzbnet/v1/inbox", body)
	rec := httptest.NewRecorder()

	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("request returned error: %v", err)
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status 413, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"payload_too_large"`) {
		t.Fatalf("expected payload_too_large code, got %s", rec.Body.String())
	}
}

func routePaths(e *echo.Echo) map[string]struct{} {
	out := map[string]struct{}{}
	for _, route := range e.Router().Routes() {
		out[route.Path] = struct{}{}
	}
	return out
}

func assertRoutePresent(t *testing.T, routes map[string]struct{}, path string) {
	t.Helper()
	if _, ok := routes[path]; !ok {
		t.Fatalf("expected route %s to be registered", path)
	}
}

func assertRouteMissing(t *testing.T, routes map[string]struct{}, path string) {
	t.Helper()
	if _, ok := routes[path]; ok {
		t.Fatalf("expected route %s to be absent", path)
	}
}
