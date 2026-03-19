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

	// Middleware: Request Logger
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:  true,
		LogURI:     true,
		LogMethod:  true,
		LogLatency: true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			app.Logger.Info("%s %s | %d | %s", v.Method, redactSensitiveURI(v.URI), v.Status, v.Latency)
			return nil
		},
	}))

	// route registration is now module-aware per Milestone 8.
	modules := app.Config.Modules
	apiKeyMW := apiKeyMiddleware(app.Config.API.Key)

	settingsCtrl := &controllers.SettingsController{App: app}

	// runtime settings admin API for modules with SQLite settings state.
	if modules.API.Enabled && app.SettingsStore != nil {
		v1Admin := e.Group("/api/v1/admin", apiKeyMW)
		v1Admin.GET("/settings", settingsCtrl.GetSettings)
		v1Admin.PUT("/settings", settingsCtrl.UpdateSettings)
	}

	var (
		nzbCtrl *controllers.NewznabController
		sabCtrl *controllers.SABController
	)

	// Aggregator-owned API surface.
	if modules.API.Enabled && modules.Aggregator.Enabled {
		nzbCtrl = &controllers.NewznabController{App: app}
		aggCtrl := &controllers.AggregatorController{App: app}

		v1Agg := e.Group("/api/v1", apiKeyMW)
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

		v1Queue := e.Group("/api/v1", apiKeyMW)
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

		// staging SAB compatibility route for Milestone 9 Chunk 1.
		// We keep this off `/api` for now to avoid colliding with the existing
		// Newznab compatibility route. A unified `/api` dispatcher comes next.
		e.GET("/api/sab", sabCtrl.Handle, apiKeyMW)
		e.POST("/api/sab", sabCtrl.Handle, apiKeyMW)
	}

	//  unified compatibility `/api` endpoint.
	// Dispatches to SAB or Newznab depending on query selector.
	if modules.API.Enabled && (modules.Aggregator.Enabled || modules.Downloader.Enabled) {
		compatCtrl := &controllers.CompatAPIController{
			SABEnabled:     modules.Downloader.Enabled,
			NewznabEnabled: modules.Aggregator.Enabled,
			SAB:            sabCtrl,
			Newznab:        nzbCtrl,
		}
		e.GET("/api", compatCtrl.Handle, apiKeyMW)
		e.POST("/api", compatCtrl.Handle, apiKeyMW)
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
