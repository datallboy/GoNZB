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

	nzbCtrl := &controllers.NewznabController{App: app}
	queueCtrl := &controllers.QueueController{Service: queuesvc.NewService(app)}
	eventCtrl := &controllers.DownloadEvent{App: app}
	apiKeyMW := apiKeyMiddleware(app.Config.API.Key)

	// Newznab API Endpoint (for Prowlarr/Sonarr)
	e.GET("/api", nzbCtrl.Handle, apiKeyMW)

	// Direct NZB Download Endpoint
	e.GET("/nzb/:id", nzbCtrl.HandleDownload, apiKeyMW)

	v1 := e.Group("/api/v1", apiKeyMW)
	v1.GET("/queue", queueCtrl.ListActive)
	v1.GET("/queue/history", queueCtrl.ListHistory)
	v1.POST("/queue/bulk/cancel", queueCtrl.CancelMany)
	v1.POST("/queue/bulk/delete", queueCtrl.DeleteMany)
	v1.POST("/queue/history/clear", queueCtrl.ClearHistory)
	v1.GET("/queue/:id", queueCtrl.GetItem)
	v1.GET("/queue/:id/files", queueCtrl.GetItemFiles)
	v1.GET("/queue/:id/events", queueCtrl.GetItemEvents)
	v1.GET("/releases/search", queueCtrl.SearchReleases)
	v1.POST("/queue", queueCtrl.Add)
	v1.POST("/queue/:id/cancel", queueCtrl.Cancel)
	v1.GET("/events/queue", eventCtrl.HandleEvents)

	registerWebUIRoutes(e)
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
