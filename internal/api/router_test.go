package api

import (
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
