package api

import (
	"context"
	"crypto/subtle"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/api/controllers"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/auth"
	"github.com/datallboy/gonzb/internal/gonzbnet/requestauth"
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
	settingsCtrl := controllers.NewSettingsController(appCtx.SettingsAdmin)
	indexerCtrl := controllers.NewIndexerController(appCtx)
	indexerAdminCtrl := controllers.NewIndexerAdminController(indexerCtrl.Service)
	indexerScrapeAdminCtrl := controllers.NewIndexerScrapeAdminController(appCtx)
	gonzbnetCtrl := controllers.NewGoNZBNetController(appCtx)
	gonzbnetAdminCtrl := controllers.NewGoNZBNetAdminController(appCtx)
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
	federationRateLimit := federationRateLimitMiddleware(appCtx.Config.GoNZBNet.RateLimitEventsPerMinute)

	// runtime settings admin API for modules with SQLite settings state.
	if modules.API.Enabled && appCtx.SettingsStore != nil {
		v1Admin := e.Group("/api/v1/admin", bodyLimitMiddleware(adminJSONBodyLimit, defaultMultipartBodyLimit))
		v1Admin.Use(csrfProtectionMiddleware())
		v1Admin.Use(auditLogMiddleware(appCtx, "admin.settings"))
		v1Admin.GET("/settings", settingsCtrl.GetSettings, authMiddleware(authSvc, false, auth.PermissionAdminSettingsRead))
		v1Admin.GET("/capabilities", settingsCtrl.GetCapabilities, authMiddleware(authSvc, false, auth.PermissionAdminSettingsRead))
		v1Admin.PUT("/settings", settingsCtrl.UpdateSettings, authMiddleware(authSvc, false, auth.PermissionAdminSettingsWrite))
	}

	if modules.API.Enabled && authSvc != nil {
		v1Auth := e.Group("/api/v1/auth", bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit))
		v1Auth.GET("/session", authCtrl.GetSession, authMiddleware(authSvc, true))
		v1Auth.GET("/setup", authCtrl.GetSetupStatus)
		v1Auth.POST("/setup", authCtrl.CreateInitialUser, authRateLimit)
		v1Auth.POST("/session", authCtrl.CreateSession, authRateLimit)
		v1Auth.DELETE("/session", authCtrl.DeleteSession, authMiddleware(authSvc, true), csrfProtectionMiddleware())
		v1Auth.GET("/tokens", authCtrl.ListCurrentUserTokens, authMiddleware(authSvc, false))
		v1Auth.POST("/tokens", authCtrl.CreateCurrentUserToken, authMiddleware(authSvc, false), csrfProtectionMiddleware())
		v1Auth.DELETE("/tokens/:id", authCtrl.RevokeCurrentUserToken, authMiddleware(authSvc, false), csrfProtectionMiddleware())

		v1AdminAuth := e.Group("/api/v1/admin/auth", bodyLimitMiddleware(adminJSONBodyLimit, defaultMultipartBodyLimit))
		v1AdminAuth.Use(csrfProtectionMiddleware())
		v1AdminAuth.Use(auditLogMiddleware(appCtx, "admin.auth"))
		v1AdminAuth.GET("/users", authCtrl.ListUsers, authMiddleware(authSvc, false, auth.PermissionAuthUsersRead))
		v1AdminAuth.GET("/users/:id", authCtrl.GetUser, authMiddleware(authSvc, false, auth.PermissionAuthUsersRead))
		v1AdminAuth.POST("/users", authCtrl.UpsertUser, authMiddleware(authSvc, false, auth.PermissionAuthUsersWrite))
		v1AdminAuth.DELETE("/users/:id", authCtrl.DeleteUser, authMiddleware(authSvc, false, auth.PermissionAuthUsersWrite))
		v1AdminAuth.GET("/roles", authCtrl.ListRoles, authMiddleware(authSvc, false, auth.PermissionAuthRolesRead))
		v1AdminAuth.POST("/roles", authCtrl.UpsertRole, authMiddleware(authSvc, false, auth.PermissionAuthRolesWrite))
		v1AdminAuth.DELETE("/roles/:id", authCtrl.DeleteRole, authMiddleware(authSvc, false, auth.PermissionAuthRolesWrite))
		v1AdminAuth.GET("/tokens", authCtrl.ListTokens, authMiddleware(authSvc, false, auth.PermissionAuthTokensRead))
		v1AdminAuth.POST("/tokens", authCtrl.CreateToken, authMiddleware(authSvc, false, auth.PermissionAuthTokensWrite))
		v1AdminAuth.DELETE("/tokens/:id", authCtrl.RevokeToken, authMiddleware(authSvc, false, auth.PermissionAuthTokensWrite))
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

	if modules.API.Enabled && modules.GoNZBNet.Enabled {
		if appCtx.Config.GoNZBNet.HTTPEnabled {
			e.GET("/.well-known/gonzbnet", gonzbnetCtrl.WellKnown)
			fed := e.Group(gonzbnetHTTPBasePath(appCtx.Config.GoNZBNet.HTTPBasePath), federationBodyLimitMiddleware(appCtx.Config.GoNZBNet))
			fed.GET("/node", gonzbnetCtrl.Node)
			fed.GET("/caps", gonzbnetCtrl.Caps)
			fed.POST("/handshake", gonzbnetCtrl.Handshake)
			fed.GET("/pools", gonzbnetCtrl.AdmissionPools)
			fed.POST("/pools/:pool_id/join-requests", gonzbnetCtrl.SubmitPoolJoin, federationRateLimit)
			fed.GET("/pools/:pool_id/admissions/:proposal_event_id", gonzbnetCtrl.AdmissionStatus, federationRateLimit)
			fed.POST("/pools/:pool_id/admissions/:proposal_event_id/approvals", gonzbnetCtrl.SubmitPoolApproval, federationRateLimit)
			fed.POST("/pools/:pool_id/admissions/:proposal_event_id/rejections", gonzbnetCtrl.SubmitPoolRejection, federationRateLimit)
			fed.GET("/outbox", gonzbnetCtrl.Outbox)
			fed.GET("/events/:event_id", gonzbnetCtrl.Event)
			fed.POST("/events/batch", gonzbnetCtrl.Inbox, federationRateLimit)
			fed.POST("/inbox", gonzbnetCtrl.Inbox, federationRateLimit)
			fed.POST("/manifests/:manifest_id/request", gonzbnetCtrl.RequestManifest, federationRateLimit)
			fed.GET("/manifests/:manifest_id", gonzbnetCtrl.GetManifest, federationRateLimit)
			fed.GET("/coverage/groups", gonzbnetCtrl.CoverageGroups)
			fed.GET("/coverage/plan", gonzbnetCtrl.CoveragePlan)
			fed.GET("/coverage/work", gonzbnetCtrl.CoverageWork)
			fed.POST("/coverage/claim", gonzbnetCtrl.CoverageClaim, federationRateLimit)
			fed.POST("/coverage/checkpoint", gonzbnetCtrl.CoverageCheckpoint, federationRateLimit)
			fed.POST("/validation/request", gonzbnetCtrl.ValidationRequest, federationRateLimit)
			fed.GET("/capabilities/nodes", gonzbnetCtrl.NodeCapabilities)
			fed.GET("/pools/:pool_id/checkpoint", gonzbnetCtrl.PoolCheckpoint)
			fed.GET("/pools/:pool_id/members", gonzbnetCtrl.PoolMembers)
			fed.GET("/peers", gonzbnetCtrl.Peers)
			fed.GET("/ws", gonzbnetCtrl.GossipWS)
		}

		v1AdminGoNZBNet := e.Group("/api/v1/admin/gonzbnet", bodyLimitMiddleware(adminJSONBodyLimit, defaultMultipartBodyLimit))
		v1AdminGoNZBNet.Use(authMiddleware(authSvc, false, auth.PermissionGoNZBNetAdminPools))
		v1AdminGoNZBNet.Use(csrfProtectionMiddleware())
		v1AdminGoNZBNet.Use(auditLogMiddleware(appCtx, "admin.gonzbnet"))
		v1AdminGoNZBNet.GET("/node/profile", gonzbnetAdminCtrl.NodeProfile)
		v1AdminGoNZBNet.GET("/config/validation", gonzbnetAdminCtrl.ConfigValidation)
		v1AdminGoNZBNet.GET("/overview", gonzbnetAdminCtrl.ReportingOverview)
		v1AdminGoNZBNet.GET("/roles", gonzbnetAdminCtrl.ReportingRoles)
		v1AdminGoNZBNet.GET("/activity", gonzbnetAdminCtrl.ReportingActivity)
		v1AdminGoNZBNet.GET("/metrics", gonzbnetAdminCtrl.Metrics)
		v1AdminGoNZBNet.GET("/metrics/prometheus", gonzbnetAdminCtrl.PrometheusMetrics)
		v1AdminGoNZBNet.GET("/pools", gonzbnetAdminCtrl.ListPools)
		v1AdminGoNZBNet.POST("/pools", gonzbnetAdminCtrl.UpsertPool)
		v1AdminGoNZBNet.POST("/pools/:pool_id/invitations", gonzbnetAdminCtrl.CreatePoolInvitation)
		v1AdminGoNZBNet.POST("/admission/discover", gonzbnetAdminCtrl.DiscoverAdmissionNode)
		v1AdminGoNZBNet.POST("/admission/join", gonzbnetAdminCtrl.JoinAdmissionPool)
		v1AdminGoNZBNet.GET("/admissions", gonzbnetAdminCtrl.ListAdmissions)
		v1AdminGoNZBNet.POST("/admissions/:proposal_event_id/refresh", gonzbnetAdminCtrl.RefreshAdmission)
		v1AdminGoNZBNet.POST("/admissions/:proposal_event_id/approve", gonzbnetAdminCtrl.ApproveAdmission)
		v1AdminGoNZBNet.POST("/admissions/:proposal_event_id/reject", gonzbnetAdminCtrl.RejectAdmission)
		v1AdminGoNZBNet.GET("/pools/:pool_id/members", gonzbnetAdminCtrl.ListPoolMembers)
		v1AdminGoNZBNet.GET("/pools/:pool_id/health", gonzbnetAdminCtrl.ReportingPoolHealth)
		v1AdminGoNZBNet.GET("/pools/:pool_id/control-events", gonzbnetAdminCtrl.ListPoolControlEvents)
		v1AdminGoNZBNet.GET("/pools/:pool_id/role-access", gonzbnetAdminCtrl.ListRolePoolAccess)
		v1AdminGoNZBNet.POST("/pools/:pool_id/role-access", gonzbnetAdminCtrl.UpsertRolePoolAccess)
		v1AdminGoNZBNet.DELETE("/pools/:pool_id/role-access/:role_id", gonzbnetAdminCtrl.DeleteRolePoolAccess)
		v1AdminGoNZBNet.POST("/pools/:pool_id/members", gonzbnetAdminCtrl.UpsertPoolMember)
		v1AdminGoNZBNet.POST("/pools/:pool_id/members/:node_id/approve", gonzbnetAdminCtrl.ApprovePoolMember)
		v1AdminGoNZBNet.POST("/pools/:pool_id/members/:node_id/revocations", gonzbnetAdminCtrl.CreatePoolMemberRevocation)
		v1AdminGoNZBNet.POST("/pools/:pool_id/members/:node_id/revoke", gonzbnetAdminCtrl.RevokePoolMember)
		v1AdminGoNZBNet.POST("/pools/:pool_id/join-requests", gonzbnetAdminCtrl.RequestPoolJoin)
		v1AdminGoNZBNet.GET("/nodes/capabilities", gonzbnetAdminCtrl.ListNodeCapabilities)
		v1AdminGoNZBNet.GET("/coverage", gonzbnetAdminCtrl.CoverageDashboard)
		v1AdminGoNZBNet.GET("/coverage/groups", gonzbnetAdminCtrl.CoverageGroupCatalog)
		v1AdminGoNZBNet.GET("/coverage/validation-gaps", gonzbnetAdminCtrl.ValidationGaps)
		v1AdminGoNZBNet.GET("/coverage/suggestions", gonzbnetAdminCtrl.CoverageSuggestions)
		v1AdminGoNZBNet.GET("/coverage/plan", gonzbnetAdminCtrl.CoverageSchedulerPlan)
		v1AdminGoNZBNet.GET("/diagnostics/peers", gonzbnetAdminCtrl.PeerDiagnostics)
		v1AdminGoNZBNet.GET("/diagnostics/events", gonzbnetAdminCtrl.EventDiagnostics)
		v1AdminGoNZBNet.GET("/diagnostics/rejected-events", gonzbnetAdminCtrl.RejectedEventDiagnostics)
		v1AdminGoNZBNet.GET("/diagnostics/deliveries", gonzbnetAdminCtrl.PeerDeliveryDiagnostics)
		v1AdminGoNZBNet.GET("/diagnostics/validation-tasks", gonzbnetAdminCtrl.ValidationTaskDiagnostics)
		v1AdminGoNZBNet.GET("/diagnostics/release-sources", gonzbnetAdminCtrl.ReleaseSourceDiagnostics)
		v1AdminGoNZBNet.GET("/diagnostics/manifest-sources", gonzbnetAdminCtrl.ManifestSourceDiagnostics)
		v1AdminGoNZBNet.GET("/diagnostics/article-availability", gonzbnetAdminCtrl.ArticleAvailabilityDiagnostics)
		v1AdminGoNZBNet.GET("/diagnostics/health", gonzbnetAdminCtrl.HealthDiagnostics)
		v1AdminGoNZBNet.GET("/diagnostics/reputation", gonzbnetAdminCtrl.ReputationDiagnostics)
		v1AdminGoNZBNet.POST("/manifests/resolve", gonzbnetAdminCtrl.ResolveManifest)
		v1AdminGoNZBNet.POST("/scores/recompute", gonzbnetAdminCtrl.RecomputeScores)
		v1AdminGoNZBNet.POST("/coverage/assignments", gonzbnetAdminCtrl.CreateCoverageAssignment)
		v1AdminGoNZBNet.POST("/coverage/claims", gonzbnetAdminCtrl.CreateCoverageClaim)
		v1AdminGoNZBNet.POST("/coverage/complete", gonzbnetAdminCtrl.CreateCoverageComplete)
		v1AdminGoNZBNet.POST("/coverage/failed", gonzbnetAdminCtrl.CreateCoverageFailed)
		v1AdminGoNZBNet.POST("/coverage/stale-penalties", gonzbnetAdminCtrl.MaterializeStaleClaimPenalties)
		v1AdminGoNZBNet.POST("/coverage/stale-reassignments", gonzbnetAdminCtrl.CreateStaleClaimReassignments)

		v1AdminGoNZBNetPeers := e.Group("/api/v1/admin/gonzbnet", bodyLimitMiddleware(adminJSONBodyLimit, defaultMultipartBodyLimit))
		v1AdminGoNZBNetPeers.Use(authMiddleware(authSvc, false, auth.PermissionGoNZBNetAdminPeers))
		v1AdminGoNZBNetPeers.Use(csrfProtectionMiddleware())
		v1AdminGoNZBNetPeers.Use(auditLogMiddleware(appCtx, "admin.gonzbnet.peers"))
		v1AdminGoNZBNetPeers.POST("/peers", gonzbnetAdminCtrl.UpsertPeer)
		v1AdminGoNZBNetPeers.POST("/peers/:peer_id/enable", gonzbnetAdminCtrl.EnablePeer)
		v1AdminGoNZBNetPeers.POST("/peers/:peer_id/disable", gonzbnetAdminCtrl.DisablePeer)
		v1AdminGoNZBNetPeers.DELETE("/peers/:peer_id", gonzbnetAdminCtrl.DeletePeer)
		v1AdminGoNZBNetPeers.POST("/nodes/:node_id/block", gonzbnetAdminCtrl.BlockNode)
		v1AdminGoNZBNetPeers.POST("/nodes/:node_id/unblock", gonzbnetAdminCtrl.UnblockNode)
		v1AdminGoNZBNetPeers.POST("/sync/pull", gonzbnetAdminCtrl.PullSync)
		v1AdminGoNZBNetPeers.POST("/sync/push", gonzbnetAdminCtrl.PushSync)
		v1AdminGoNZBNetPeers.POST("/sync/gossip", gonzbnetAdminCtrl.GossipSync)

		v1AdminGoNZBNetModeration := e.Group("/api/v1/admin/gonzbnet", bodyLimitMiddleware(adminJSONBodyLimit, defaultMultipartBodyLimit))
		v1AdminGoNZBNetModeration.Use(authMiddleware(authSvc, false, auth.PermissionGoNZBNetAdminModeration))
		v1AdminGoNZBNetModeration.Use(csrfProtectionMiddleware())
		v1AdminGoNZBNetModeration.Use(auditLogMiddleware(appCtx, "admin.gonzbnet.moderation"))
		v1AdminGoNZBNetModeration.GET("/moderation/tombstones", gonzbnetAdminCtrl.ListTombstones)
		v1AdminGoNZBNetModeration.POST("/moderation/tombstones", gonzbnetAdminCtrl.CreateTombstone)

		v1AdminGoNZBNetKeys := e.Group("/api/v1/admin/gonzbnet", bodyLimitMiddleware(adminJSONBodyLimit, defaultMultipartBodyLimit))
		v1AdminGoNZBNetKeys.Use(authMiddleware(authSvc, false, auth.PermissionGoNZBNetAdminKeys))
		v1AdminGoNZBNetKeys.Use(csrfProtectionMiddleware())
		v1AdminGoNZBNetKeys.Use(auditLogMiddleware(appCtx, "admin.gonzbnet.keys"))
		v1AdminGoNZBNetKeys.POST("/keys/export", gonzbnetAdminCtrl.ExportKey)
		v1AdminGoNZBNetKeys.POST("/keys/rotate", gonzbnetAdminCtrl.RotateKey)
	}

	var (
		nzbCtrl *controllers.NewznabController
		sabCtrl *controllers.SABController
	)

	// Aggregator-owned API surface.
	if modules.API.Enabled && modules.Aggregator.Enabled {
		nzbCtrl = controllers.NewNewznabController(appCtx.AggregatorModule)
		aggCtrl := controllers.NewAggregatorController(appCtx.AggregatorModule)

		v1Agg := e.Group("/api/v1", bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit), apiTokenMiddleware(authSvc, auth.PermissionAggregatorReleasesRead))
		v1Agg.GET("/releases/search", aggCtrl.SearchReleases)

		// Keep direct NZB download endpoint under aggregator ownership.
		e.GET("/nzb/:id", nzbCtrl.HandleDownload, apiTokenMiddleware(authSvc, auth.PermissionAggregatorReleasesRead))
	}

	// Indexer-owned API surface.
	if modules.API.Enabled && modules.UsenetIndexer.Enabled {
		v1Indexer := e.Group("/api/v1/indexer", bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit), authMiddleware(authSvc, false, auth.PermissionIndexerReleasesRead))
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

		v1AdminIndexer := e.Group("/api/v1/admin/indexer", bodyLimitMiddleware(adminJSONBodyLimit, defaultMultipartBodyLimit))
		v1AdminIndexer.Use(authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeRead))
		v1AdminIndexer.Use(csrfProtectionMiddleware())
		v1AdminIndexer.Use(auditLogMiddleware(appCtx, "admin.indexer"))
		v1AdminIndexer.GET("/overview", indexerAdminCtrl.GetOverview)
		v1AdminIndexer.GET("/overview/stream", indexerAdminCtrl.StreamOverview)
		v1AdminIndexer.GET("/overview/stats", indexerAdminCtrl.GetDashboardStats)
		v1AdminIndexer.POST("/overview/stats/actions/refresh", indexerAdminCtrl.RefreshDashboardStats, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.GET("/storage", indexerAdminCtrl.GetStorageStatus)
		v1AdminIndexer.GET("/overview/backfill-progress", indexerAdminCtrl.GetBackfillProgress)
		v1AdminIndexer.GET("/work/recovery-capacity", indexerAdminCtrl.GetRecoveryCapacity)
		v1AdminIndexer.GET("/work/group-profiles", indexerAdminCtrl.ListGroupProfiles)
		v1AdminIndexer.GET("/work/deferred-ranges", indexerAdminCtrl.ListDeferredArticleRanges)
		v1AdminIndexer.GET("/work/cohorts", indexerAdminCtrl.ListArticleCohorts)
		v1AdminIndexer.GET("/overview/throughput", indexerAdminCtrl.GetStageThroughput)
		v1AdminIndexer.GET("/overview/nntp", indexerAdminCtrl.GetNNTPStats)
		v1AdminIndexer.GET("/overview/backlog", indexerAdminCtrl.GetDashboardStats)
		v1AdminIndexer.POST("/overview/backlog/actions/refresh", indexerAdminCtrl.RefreshDashboardStats, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.GET("/scrape", indexerScrapeAdminCtrl.GetConfig)
		v1AdminIndexer.GET("/stages", indexerAdminCtrl.ListStages)
		v1AdminIndexer.GET("/stages/:stage", indexerAdminCtrl.GetStage)
		v1AdminIndexer.GET("/maintenance/storage-audit", indexerAdminCtrl.GetStorageAudit)
		v1AdminIndexer.GET("/maintenance/tasks", indexerAdminCtrl.ListMaintenanceTasks)
		v1AdminIndexer.GET("/attention", indexerAdminCtrl.ListAttention)
		v1AdminIndexer.GET("/releases", indexerAdminCtrl.ListReleases)
		v1AdminIndexer.GET("/releases/:id", indexerAdminCtrl.GetRelease)
		v1AdminIndexer.GET("/binaries", indexerAdminCtrl.ListBinaries)
		v1AdminIndexer.GET("/runs", indexerAdminCtrl.ListRuns)
		v1AdminIndexer.GET("/runs/:id", indexerAdminCtrl.GetRun)
		v1AdminIndexer.PATCH("/stages/:stage", indexerAdminCtrl.PatchStage, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeConfigure))
		v1AdminIndexer.PATCH("/maintenance/tasks/:task", indexerAdminCtrl.PatchMaintenanceTask, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeConfigure))
		v1AdminIndexer.POST("/maintenance/tasks/:task/dry-run", indexerAdminCtrl.DryRunMaintenanceTask, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.POST("/maintenance/tasks/:task/run", indexerAdminCtrl.RunMaintenanceTask, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.PUT("/scrape", indexerScrapeAdminCtrl.UpdateConfig, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeConfigure))
		v1AdminIndexer.POST("/scrape/actions/scan", indexerScrapeAdminCtrl.ScanProviders, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.GET("/scrape/provider-inventory", indexerScrapeAdminCtrl.ProviderInventory)
		v1AdminIndexer.GET("/scrape/preview", indexerScrapeAdminCtrl.PreviewWildcardGroups)
		v1AdminIndexer.GET("/scrape/crosspost-popularity", indexerScrapeAdminCtrl.CrosspostPopularity)
		v1AdminIndexer.POST("/scrape/actions/apply", indexerScrapeAdminCtrl.ApplyWildcardGroups, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeConfigure))
		v1AdminIndexer.POST("/stages/:stage/actions/run", indexerAdminCtrl.RunStage, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.POST("/stages/:stage/actions/pause", indexerAdminCtrl.PauseStage, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimePause))
		v1AdminIndexer.POST("/stages/:stage/actions/resume", indexerAdminCtrl.ResumeStage, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimePause))
		v1AdminIndexer.PATCH("/releases/:id", indexerAdminCtrl.PatchRelease, authMiddleware(authSvc, false, auth.PermissionIndexerReleasesOverride))
		v1AdminIndexer.POST("/releases/:id/actions/identify", indexerAdminCtrl.IdentifyRelease, authMiddleware(authSvc, false, auth.PermissionIndexerReleasesOverride))
		v1AdminIndexer.POST("/releases/:id/actions/reinspect", indexerAdminCtrl.ReinspectRelease, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.POST("/releases/:id/actions/reenrich", indexerAdminCtrl.ReenrichRelease, authMiddleware(authSvc, false, auth.PermissionIndexerRuntimeRun))
		v1AdminIndexer.POST("/releases/:id/actions/hide", indexerAdminCtrl.HideRelease, authMiddleware(authSvc, false, auth.PermissionIndexerReleasesHide))
		v1AdminIndexer.POST("/releases/:id/actions/unhide", indexerAdminCtrl.UnhideRelease, authMiddleware(authSvc, false, auth.PermissionIndexerReleasesHide))
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

		v1Queue := e.Group("/api/v1", bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit), authMiddleware(authSvc, false))
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
		e.GET("/api/sab", sabCtrl.Handle, bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit), apiTokenMiddleware(authSvc, auth.PermissionDownloaderRuntimeRead))
		e.POST("/api/sab", sabCtrl.Handle, bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit), apiTokenMiddleware(authSvc, auth.PermissionDownloaderRuntimeRead))
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
		e.GET("/api", compatCtrl.Handle, bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit), compatAPITokenMiddleware(authSvc))
		e.POST("/api", compatCtrl.Handle, bodyLimitMiddleware(defaultJSONBodyLimit, defaultMultipartBodyLimit), compatAPITokenMiddleware(authSvc))
	}

	// Web UI is served only when explicitly enabled.
	if modules.WebUI.Enabled {
		registerWebUIRoutes(e)
	}
}

func gonzbnetHTTPBasePath(configured string) string {
	path := strings.TrimRight(strings.TrimSpace(configured), "/")
	if path == "" {
		return "/gonzbnet/v1"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func federationRateLimitMiddleware(eventsPerMinute int) echo.MiddlewareFunc {
	if eventsPerMinute <= 0 {
		eventsPerMinute = 120
	}
	ratePerSecond := float64(eventsPerMinute) / 60.0
	if ratePerSecond <= 0 {
		ratePerSecond = 1
	}
	throttle := newFederationFloodThrottle(3, 15*time.Minute, 5*time.Minute)
	limiter := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{
			Rate:      ratePerSecond,
			Burst:     eventsPerMinute,
			ExpiresIn: 15 * time.Minute,
		}),
		IdentifierExtractor: func(c *echo.Context) (string, error) {
			return federationRateLimitIdentifier(c), nil
		},
		DenyHandler: func(c *echo.Context, identifier string, err error) error {
			if throttle.recordViolation(identifier, time.Now()) {
				return federationTransportError(c, http.StatusTooManyRequests, "temporarily_throttled", "temporary throttle active")
			}
			return federationTransportError(c, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
		},
	})
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		limited := limiter(next)
		return func(c *echo.Context) error {
			if throttle.isThrottled(federationRateLimitIdentifier(c), time.Now()) {
				return federationTransportError(c, http.StatusTooManyRequests, "temporarily_throttled", "temporary throttle active")
			}
			return limited(c)
		}
	}
}

type federationFloodThrottle struct {
	mu          sync.Mutex
	threshold   int
	window      time.Duration
	duration    time.Duration
	violations  map[string]int
	firstSeen   map[string]time.Time
	throttledTo map[string]time.Time
}

func newFederationFloodThrottle(threshold int, window, duration time.Duration) *federationFloodThrottle {
	if threshold <= 0 {
		threshold = 3
	}
	if window <= 0 {
		window = 15 * time.Minute
	}
	if duration <= 0 {
		duration = 5 * time.Minute
	}
	return &federationFloodThrottle{
		threshold:   threshold,
		window:      window,
		duration:    duration,
		violations:  map[string]int{},
		firstSeen:   map[string]time.Time{},
		throttledTo: map[string]time.Time{},
	}
}

func (t *federationFloodThrottle) isThrottled(identifier string, now time.Time) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	until, ok := t.throttledTo[identifier]
	if !ok {
		return false
	}
	if now.Before(until) {
		return true
	}
	delete(t.throttledTo, identifier)
	delete(t.violations, identifier)
	delete(t.firstSeen, identifier)
	return false
}

func (t *federationFloodThrottle) recordViolation(identifier string, now time.Time) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if until, ok := t.throttledTo[identifier]; ok && now.Before(until) {
		return true
	}
	first, ok := t.firstSeen[identifier]
	if !ok || now.Sub(first) > t.window {
		t.firstSeen[identifier] = now
		t.violations[identifier] = 0
	}
	t.violations[identifier]++
	if t.violations[identifier] < t.threshold {
		return false
	}
	t.throttledTo[identifier] = now.Add(t.duration)
	return true
}

func federationRateLimitIdentifier(c *echo.Context) string {
	if nodeID := federationAuthorizationNodeID(c); nodeID != "" {
		return "node:" + nodeID
	}
	return "ip:" + c.RealIP()
}

func federationAuthorizationNodeID(c *echo.Context) string {
	if c == nil || c.Request() == nil {
		return ""
	}
	values, err := requestauth.ParseAuthorization(c.Request().Header.Get("Authorization"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(values["node_id"])
}

func apiTokenMiddleware(authSvc *auth.Service, permissions ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			principal, err := authenticateAPIKeyPrincipal(c, authSvc)
			if err != nil {
				return c.String(http.StatusUnauthorized, "Unauthorized")
			}
			for _, permission := range permissions {
				if !principal.Has(permission) {
					return c.String(http.StatusForbidden, "Forbidden")
				}
			}
			controllers.SetPrincipal(c, principal)
			return next(c)
		}
	}
}

func compatAPITokenMiddleware(authSvc *auth.Service) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			permission := auth.PermissionAggregatorReleasesRead
			if strings.TrimSpace(c.QueryParam("mode")) != "" || strings.TrimSpace(c.FormValue("mode")) != "" {
				permission = auth.PermissionDownloaderRuntimeRead
			}
			return apiTokenMiddleware(authSvc, permission)(next)(c)
		}
	}
}

func authenticateAPIKeyPrincipal(c *echo.Context, authSvc *auth.Service) (*auth.Principal, error) {
	if authSvc == nil {
		return nil, auth.ErrUnauthorized
	}
	provided := strings.TrimSpace(c.QueryParam("apikey"))
	if provided == "" {
		provided = strings.TrimSpace(c.Request().Header.Get("X-API-Key"))
	}
	return authSvc.AuthenticateToken(c.Request().Context(), provided)
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
				authMode = "principal"
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

func authMiddleware(authSvc *auth.Service, optional bool, permissions ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			principal, err := authenticatePrincipal(c, authSvc)
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

func authenticatePrincipal(c *echo.Context, authSvc *auth.Service) (*auth.Principal, error) {
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
	return false
}
