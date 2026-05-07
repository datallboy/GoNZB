package api

import (
	"context"
	"crypto/subtle"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/api/controllers"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/auth"
	"github.com/datallboy/gonzb/internal/telemetry"
	"github.com/datallboy/gonzb/internal/webui"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func RegisterRoutes(e *echo.Echo, appCtx *app.Context) {
	// CORS for browser-based UI (Vite/dev and optional external UI hosts).
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     appCtx.Config.API.CORSAllowedOrigins,
		AllowCredentials: true,
		AllowMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowHeaders: []string{
			echo.HeaderOrigin,
			echo.HeaderContentType,
			echo.HeaderAccept,
			echo.HeaderAuthorization,
			"X-API-Key",
		},
	}))

	e.Use(middleware.RequestID())
	e.Use(middleware.Recover())
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XFrameOptions:         "DENY",
		ContentTypeNosniff:    "nosniff",
		XSSProtection:         "1; mode=block",
		ReferrerPolicy:        "same-origin",
		ContentSecurityPolicy: "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self' data:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'",
	}))

	// Middleware: Request Logger
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:    true,
		LogURI:       true,
		LogMethod:    true,
		LogLatency:   true,
		LogRequestID: true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			appCtx.Logger.Info("request_id=%s method=%s uri=%s status=%d latency=%s",
				v.RequestID, v.Method, redactSensitiveURI(v.URI), v.Status, v.Latency)
			return nil
		},
	}))

	// route registration is now module-aware per Milestone 8.
	modules := appCtx.Config.Modules
	apiKeyMW := apiKeyMiddleware(appCtx.Config.API.Key)

	settingsCtrl := controllers.NewSettingsController(appCtx.SettingsAdmin)
	indexerCtrl := controllers.NewIndexerController(appCtx)
	indexerAdminCtrl := controllers.NewIndexerAdminController(indexerCtrl.Service)
	var authSvc *auth.Service
	if store, ok := any(appCtx.SettingsStore).(auth.Store); ok {
		authSvc = auth.NewService(store)
		_ = authSvc.Bootstrap(context.Background())
	}
	authCtrl := &controllers.AuthController{Service: authSvc}
	authRateLimit := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{
			Rate:      0.2,
			Burst:     5,
			ExpiresIn: 15 * time.Minute,
		}),
	})

	// runtime settings admin API for modules with SQLite settings state.
	if modules.API.Enabled && appCtx.SettingsStore != nil {
		v1Admin := e.Group("/api/v1/admin", bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
		v1Admin.Use(csrfProtectionMiddleware())
		v1Admin.Use(auditLogMiddleware(appCtx, "admin.settings"))
		v1Admin.GET("/settings", settingsCtrl.GetSettings, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAdminSettingsRead))
		v1Admin.GET("/capabilities", settingsCtrl.GetCapabilities, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAdminSettingsRead))
		v1Admin.PUT("/settings", settingsCtrl.UpdateSettings, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAdminSettingsWrite))
	}

	if modules.API.Enabled && authSvc != nil {
		v1Auth := e.Group("/api/v1/auth", bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
		v1Auth.GET("/session", authCtrl.GetSession, authMiddleware(authSvc, appCtx.Config.API.Key, true))
		v1Auth.GET("/setup", authCtrl.GetSetupStatus)
		v1Auth.POST("/setup", authCtrl.CreateInitialUser, authRateLimit)
		v1Auth.POST("/session", authCtrl.CreateSession, authRateLimit)
		v1Auth.DELETE("/session", authCtrl.DeleteSession, authMiddleware(authSvc, appCtx.Config.API.Key, true), csrfProtectionMiddleware())
		v1Auth.GET("/tokens", authCtrl.ListCurrentUserTokens, authMiddleware(authSvc, appCtx.Config.API.Key, false))
		v1Auth.POST("/tokens", authCtrl.CreateCurrentUserToken, authMiddleware(authSvc, appCtx.Config.API.Key, false), csrfProtectionMiddleware())
		v1Auth.DELETE("/tokens/:id", authCtrl.RevokeCurrentUserToken, authMiddleware(authSvc, appCtx.Config.API.Key, false), csrfProtectionMiddleware())

		v1AdminAuth := e.Group("/api/v1/admin/auth", bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
		v1AdminAuth.Use(csrfProtectionMiddleware())
		v1AdminAuth.Use(auditLogMiddleware(appCtx, "admin.auth"))
		v1AdminAuth.GET("/users", authCtrl.ListUsers, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAuthUsersRead))
		v1AdminAuth.GET("/users/:id", authCtrl.GetUser, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAuthUsersRead))
		v1AdminAuth.POST("/users", authCtrl.UpsertUser, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAuthUsersWrite))
		v1AdminAuth.DELETE("/users/:id", authCtrl.DeleteUser, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAuthUsersWrite))
		v1AdminAuth.GET("/roles", authCtrl.ListRoles, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAuthRolesRead))
		v1AdminAuth.POST("/roles", authCtrl.UpsertRole, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAuthRolesWrite))
		v1AdminAuth.DELETE("/roles/:id", authCtrl.DeleteRole, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAuthRolesWrite))
		v1AdminAuth.GET("/tokens", authCtrl.ListTokens, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAuthTokensRead))
		v1AdminAuth.POST("/tokens", authCtrl.CreateToken, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAuthTokensWrite))
		v1AdminAuth.DELETE("/tokens/:id", authCtrl.RevokeToken, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionAuthTokensWrite))
	}

	// Liveness/readiness endpoints stay unauthenticated for infrastructure probes.
	if modules.API.Enabled {
		e.GET("/healthz", func(c *echo.Context) error {
			return c.JSON(http.StatusOK, telemetry.Health(appCtx))
		})

		e.GET("/readyz", func(c *echo.Context) error {
			code, report := telemetry.Readiness(c.Request().Context(), appCtx)
			return c.JSON(code, report)
		})
	}

	var (
		nzbCtrl *controllers.NewznabController
		sabCtrl *controllers.SABController
	)

	// Aggregator-owned API surface.
	if modules.API.Enabled && modules.Aggregator.Enabled {
		nzbCtrl = controllers.NewNewznabController(appCtx.AggregatorModule)
		aggCtrl := controllers.NewAggregatorController(appCtx.AggregatorModule)

		v1Agg := e.Group("/api/v1", apiKeyMW, bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
		v1Agg.GET("/releases/search", aggCtrl.SearchReleases)

		// Keep direct NZB download endpoint under aggregator ownership.
		e.GET("/nzb/:id", nzbCtrl.HandleDownload, apiKeyMW)
	}

	// Indexer-owned API surface.
	if modules.API.Enabled && modules.UsenetIndexer.Enabled {
		v1Indexer := e.Group("/api/v1/indexer", bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit), authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerReleasesRead))
		v1Indexer.GET("/overview", indexerCtrl.GetOverview)
		v1Indexer.GET("/stages", indexerCtrl.ListStages)
		v1Indexer.GET("/runs", indexerCtrl.ListRuns)
		v1Indexer.POST("/stages/:stage/run", indexerCtrl.RunStage)
		v1Indexer.POST("/stages/:stage/pause", indexerCtrl.PauseStage)
		v1Indexer.POST("/stages/:stage/resume", indexerCtrl.ResumeStage)
		v1Indexer.GET("/releases", indexerCtrl.ListReleases)
		v1Indexer.GET("/releases/:id", indexerCtrl.GetRelease)
		v1Indexer.GET("/binaries/:id", indexerCtrl.GetBinary)
		v1Indexer.GET("/files/:id", indexerCtrl.GetFile)

		v1AdminIndexer := e.Group("/api/v1/admin/indexer", bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
		v1AdminIndexer.Use(authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerRuntimeRead))
		v1AdminIndexer.Use(csrfProtectionMiddleware())
		v1AdminIndexer.Use(auditLogMiddleware(appCtx, "admin.indexer"))
		v1AdminIndexer.GET("/overview", indexerAdminCtrl.GetOverview)
		v1AdminIndexer.GET("/overview/stats", indexerAdminCtrl.GetDashboardStats)
		v1AdminIndexer.POST("/overview/stats/actions/refresh", indexerAdminCtrl.RefreshDashboardStats, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.GET("/overview/backfill-progress", indexerAdminCtrl.GetBackfillProgress)
		v1AdminIndexer.GET("/overview/backlog", indexerAdminCtrl.GetDashboardStats)
		v1AdminIndexer.POST("/overview/backlog/actions/refresh", indexerAdminCtrl.RefreshDashboardStats, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.GET("/stages", indexerAdminCtrl.ListStages)
		v1AdminIndexer.GET("/stages/:stage", indexerAdminCtrl.GetStage)
		v1AdminIndexer.GET("/releases", indexerAdminCtrl.ListReleases)
		v1AdminIndexer.GET("/releases/:id", indexerAdminCtrl.GetRelease)
		v1AdminIndexer.GET("/runs", indexerAdminCtrl.ListRuns)
		v1AdminIndexer.GET("/runs/:id", indexerAdminCtrl.GetRun)
		v1AdminIndexer.PATCH("/stages/:stage", indexerAdminCtrl.PatchStage, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerRuntimeConfigure))
		v1AdminIndexer.POST("/stages/:stage/actions/run", indexerAdminCtrl.RunStage, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.POST("/stages/:stage/actions/pause", indexerAdminCtrl.PauseStage, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerRuntimePause))
		v1AdminIndexer.POST("/stages/:stage/actions/resume", indexerAdminCtrl.ResumeStage, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerRuntimePause))
		v1AdminIndexer.PATCH("/releases/:id", indexerAdminCtrl.PatchRelease, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerReleasesOverride))
		v1AdminIndexer.POST("/releases/:id/actions/reinspect", indexerAdminCtrl.ReinspectRelease, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.POST("/releases/:id/actions/reenrich", indexerAdminCtrl.ReenrichRelease, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.POST("/releases/:id/actions/hide", indexerAdminCtrl.HideRelease, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerReleasesHide))
		v1AdminIndexer.POST("/releases/:id/actions/unhide", indexerAdminCtrl.UnhideRelease, authMiddleware(authSvc, appCtx.Config.API.Key, false, auth.PermissionIndexerReleasesHide))
	}

	// Downloader-owned API surface.
	if modules.API.Enabled && modules.Downloader.Enabled {
		queueCtrl := controllers.NewQueueController(appCtx.DownloaderModule)
		var downloaderQueries app.DownloaderQueries
		if appCtx.DownloaderModule != nil {
			downloaderQueries = appCtx.DownloaderModule.Queries()
		}
		eventCtrl := controllers.NewDownloadEvent(downloaderQueries)
		sabCtrl = controllers.NewSABController(appCtx.DownloaderModule, appCtx.CurrentConfig)

		v1Queue := e.Group("/api/v1", bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit), authMiddleware(authSvc, appCtx.Config.API.Key, false))
		v1Queue.Use(csrfProtectionMiddleware())
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

func csrfProtectionMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if c == nil || isSafeMethod(c.Request().Method) || usesNonSessionAuth(c) {
				return next(c)
			}
			if _, err := c.Cookie(controllers.SessionCookieName()); err != nil {
				return next(c)
			}
			cookie, err := c.Cookie(controllers.CSRFCookieName())
			if err != nil || cookie == nil || strings.TrimSpace(cookie.Value) == "" {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "invalid csrf token"})
			}
			provided := strings.TrimSpace(c.Request().Header.Get(echo.HeaderXCSRFToken))
			if subtle.ConstantTimeCompare([]byte(provided), []byte(cookie.Value)) != 1 {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "invalid csrf token"})
			}
			return next(c)
		}
	}
}

func auditLogMiddleware(appCtx *app.Context, scope string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			err := next(c)
			if c == nil || appCtx == nil || appCtx.Logger == nil || isSafeMethod(c.Request().Method) {
				return err
			}
			username := "anonymous"
			authMode := "none"
			if principal, ok := controllers.PrincipalFromContext(c); ok && principal != nil {
				if principal.Username != "" {
					username = principal.Username
				}
				if principal.ByAPIKey {
					authMode = "api-key"
				} else {
					authMode = "principal"
				}
			}
			status := http.StatusOK
			if res, unwrapErr := echo.UnwrapResponse(c.Response()); unwrapErr == nil && res != nil && res.Status != 0 {
				status = res.Status
			}
			appCtx.Logger.Info(
				"audit scope=%s request_id=%s method=%s path=%s status=%d actor=%s auth_mode=%s",
				scope,
				c.Response().Header().Get(echo.HeaderXRequestID),
				c.Request().Method,
				c.Request().URL.Path,
				status,
				username,
				authMode,
			)
			return err
		}
	}
}

func authMiddleware(authSvc *auth.Service, apiKey string, optional bool, permissions ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			principal, err := authenticatePrincipal(c, authSvc, apiKey)
			if err != nil && !optional {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			}
			if principal != nil {
				controllers.SetPrincipal(c, principal)
			}
			if principal == nil {
				return next(c)
			}
			for _, permission := range permissions {
				if !principal.Has(permission) {
					return c.JSON(http.StatusForbidden, map[string]string{"error": "Forbidden"})
				}
			}
			return next(c)
		}
	}
}

func authenticatePrincipal(c *echo.Context, authSvc *auth.Service, apiKey string) (*auth.Principal, error) {
	if apiKey != "" {
		provided := c.QueryParam("apikey")
		if provided == "" {
			provided = c.Request().Header.Get("X-API-Key")
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(apiKey)) == 1 {
			return &auth.Principal{Username: "api-key", ByAPIKey: true}, nil
		}
	}
	if authSvc == nil {
		return nil, auth.ErrUnauthorized
	}
	if header := c.Request().Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		return authSvc.AuthenticateToken(c.Request().Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
	}
	if cookie, err := c.Cookie(controllers.SessionCookieName()); err == nil && cookie != nil {
		return authSvc.AuthenticateSession(c.Request().Context(), cookie.Value)
	}
	return nil, auth.ErrUnauthorized
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func usesNonSessionAuth(c *echo.Context) bool {
	if c == nil || c.Request() == nil {
		return false
	}
	if strings.HasPrefix(c.Request().Header.Get("Authorization"), "Bearer ") {
		return true
	}
	if strings.TrimSpace(c.Request().Header.Get("X-API-Key")) != "" || strings.TrimSpace(c.QueryParam("apikey")) != "" {
		return true
	}
	return false
}
