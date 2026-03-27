package api

import (
	"crypto/subtle"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"github.com/datallboy/gonzb/internal/api/controllers"
	"github.com/datallboy/gonzb/internal/app"
	queuesvc "github.com/datallboy/gonzb/internal/queue"
	"github.com/datallboy/gonzb/internal/telemetry"
	"github.com/datallboy/gonzb/internal/webui"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func RegisterRoutes(e *echo.Echo, app *app.Context) {
	// CORS for browser-based UI (Vite/dev and optional external UI hosts).
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: app.Config.API.CORSAllowedOrigins,
		AllowMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodOptions,
		},
		AllowHeaders: []string{
			echo.HeaderOrigin,
			echo.HeaderContentType,
			echo.HeaderAccept,
			"X-API-Key",
		},
	}))

	e.Use(middleware.RequestID())
	e.Use(middleware.Recover())

	// Middleware: Request Logger
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:    true,
		LogURI:       true,
		LogMethod:    true,
		LogLatency:   true,
		LogRequestID: true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			app.Logger.Info("request_id=%s method=%s uri=%s status=%d latency=%s",
				v.RequestID, v.Method, redactSensitiveURI(v.URI), v.Status, v.Latency)
			return nil
		},
	}))

	// route registration is now module-aware per Milestone 8.
	modules := app.Config.Modules
	apiKeyMW := apiKeyMiddleware(app.Config.API.Key)

	settingsCtrl := controllers.NewSettingsController(app)

	// runtime settings admin API for modules with SQLite settings state.
	if modules.API.Enabled && app.SettingsStore != nil {
		v1Admin := e.Group("/api/v1/admin", apiKeyMW, bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
		v1Admin.GET("/settings", settingsCtrl.GetSettings)
		v1Admin.PUT("/settings", settingsCtrl.UpdateSettings)
	}

	// Liveness/readiness endpoints stay unauthenticated for infrastructure probes.
	if modules.API.Enabled {
		e.GET("/healthz", func(c *echo.Context) error {
			return c.JSON(http.StatusOK, telemetry.Health(app))
		})

		e.GET("/readyz", func(c *echo.Context) error {
			code, report := telemetry.Readiness(c.Request().Context(), app)
			return c.JSON(code, report)
		})
	}

	var (
		nzbCtrl *controllers.NewznabController
		sabCtrl *controllers.SABController
	)

	// Aggregator-owned API surface.
	if modules.API.Enabled && modules.Aggregator.Enabled {
		nzbCtrl = controllers.NewNewznabController(app)
		aggCtrl := controllers.NewAggregatorController(app)

		v1Agg := e.Group("/api/v1", apiKeyMW, bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
		v1Agg.GET("/releases/search", aggCtrl.SearchReleases)

		// Keep direct NZB download endpoint under aggregator ownership.
		e.GET("/nzb/:id", nzbCtrl.HandleDownload, apiKeyMW)
	}

	// Downloader-owned API surface.
	if modules.API.Enabled && modules.Downloader.Enabled {
		queueService := queuesvc.NewService(app)
		queueCtrl := &controllers.QueueController{Service: queueService}
		eventCtrl := &controllers.DownloadEvent{App: app}
		sabCtrl = &controllers.SABController{
			App:     app,
			Service: queueService,
		}

		v1Queue := e.Group("/api/v1", apiKeyMW, bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
		v1Queue.GET("/queue", queueCtrl.ListActive)
		v1Queue.GET("/queue/history", queueCtrl.ListHistory)
		v1Queue.POST("/queue/bulk/cancel", queueCtrl.CancelMany)
		v1Queue.POST("/queue/bulk/delete", queueCtrl.DeleteMany)
		v1Queue.POST("/queue/history/clear", queueCtrl.ClearHistory)
		v1Queue.GET("/queue/:id", queueCtrl.GetItem)
		v1Queue.GET("/queue/:id/files", queueCtrl.GetItemFiles)
		v1Queue.GET("/queue/:id/events", queueCtrl.GetItemEvents)
		v1Queue.POST("/queue", queueCtrl.Add)
		v1Queue.POST("/queue/:id/cancel", queueCtrl.Cancel)
		v1Queue.GET("/events/queue", eventCtrl.HandleEvents)

		// Explicit SAB-compatible downloader surface.
		// Supported alongside the shared `/api` multiplexer.
		e.GET("/api/sab", sabCtrl.Handle, apiKeyMW, bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
		e.POST("/api/sab", sabCtrl.Handle, apiKeyMW, bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
	}

	// Shared compatibility multiplexer.
	// Supported contract:
	// - `/api?mode=...` => SAB-compatible downloader API
	// - `/api?t=...` => Newznab-compatible aggregator API
	if modules.API.Enabled && (modules.Aggregator.Enabled || modules.Downloader.Enabled) {
		compatCtrl := &controllers.CompatAPIController{
			SABEnabled:     modules.Downloader.Enabled,
			NewznabEnabled: modules.Aggregator.Enabled,
			SAB:            sabCtrl,
			Newznab:        nzbCtrl,
		}
		e.GET("/api", compatCtrl.Handle, apiKeyMW, bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
		e.POST("/api", compatCtrl.Handle, apiKeyMW, bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
	}

	// Web UI is served only when explicitly enabled.
	if modules.WebUI.Enabled {
		registerWebUIRoutes(e)
	}
}

func apiKeyMiddleware(requiredKey string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if requiredKey == "" {
				return next(c)
			}

			provided := c.QueryParam("apikey")
			if provided == "" {
				provided = c.Request().Header.Get("X-API-Key")
			}

			if subtle.ConstantTimeCompare([]byte(provided), []byte(requiredKey)) != 1 {
				return c.String(http.StatusUnauthorized, "Unauthorized")
			}

			return next(c)
		}
	}
}

func redactSensitiveURI(rawURI string) string {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return rawURI
	}

	query := parsed.Query()
	if query.Has("apikey") {
		query.Set("apikey", "REDACTED")
		parsed.RawQuery = query.Encode()
	}

	return parsed.String()
}

func registerWebUIRoutes(e *echo.Echo) {
	uiFS, err := webui.FS()
	if err != nil {
		return
	}

	e.GET("/", func(c *echo.Context) error {
		return c.FileFS("index.html", uiFS)
	})

	// SPA fallback for non-API paths.
	e.RouteNotFound("/*", func(c *echo.Context) error {
		p := c.Request().URL.Path
		if strings.HasPrefix(p, "/api") || strings.HasPrefix(p, "/nzb") {
			return c.NoContent(http.StatusNotFound)
		}

		clean := strings.TrimPrefix(p, "/")
		if clean != "" {
			if _, statErr := fs.Stat(uiFS, clean); statErr == nil {
				return c.FileFS(clean, uiFS)
			}
		}

		return c.FileFS("index.html", uiFS)
	})
}
