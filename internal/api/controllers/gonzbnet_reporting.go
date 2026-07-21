package controllers

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/activity"
	"github.com/datallboy/gonzb/internal/gonzbnet/capability"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

type gonzbnetActivityStore interface {
	ListFederationActivityRollups(context.Context, pgindex.FederationActivityQuery) ([]activity.Rollup, error)
	ListActivePoolIDsForNodeCapabilities(context.Context, string, []string) ([]string, error)
}

type gonzbnetPoolHealthStore interface {
	GetFederationPoolHealthReport(context.Context, string, time.Time) (pgindex.FederationPoolHealthReport, error)
}

type gonzbnetArticleAvailabilityStore interface {
	ListArticleAvailabilityDiagnostics(context.Context, string, int) ([]pgindex.ArticleAvailabilityDiagnostic, error)
}

type gonzbnetRoleJob struct {
	Key          string              `json:"key"`
	Label        string              `json:"label"`
	Description  string              `json:"description"`
	Status       activity.Status     `json:"status"`
	Configured   bool                `json:"configured"`
	Pools        []string            `json:"pools"`
	LastUsefulAt *time.Time          `json:"last_useful_at,omitempty"`
	Warnings     []string            `json:"warnings"`
	Components   []activity.Snapshot `json:"components"`
}

type gonzbnetRoleReport struct {
	GeneratedAt time.Time         `json:"generated_at"`
	NodeID      string            `json:"node_id"`
	Jobs        []gonzbnetRoleJob `json:"jobs"`
	Warnings    []string          `json:"warnings"`
}

type gonzbnetPoolOverview struct {
	PoolID      string `json:"pool_id"`
	DisplayName string `json:"display_name"`
	Enabled     bool   `json:"enabled"`
	Members     int    `json:"members"`
}

type gonzbnetOverviewResponse struct {
	GeneratedAt       time.Time                         `json:"generated_at"`
	NodeID            string                            `json:"node_id"`
	NodeAlias         string                            `json:"node_alias"`
	ModuleEnabled     bool                              `json:"module_enabled"`
	JobsHealthy       int                               `json:"jobs_healthy"`
	JobsConfigured    int                               `json:"jobs_configured"`
	PeersConnected    int                               `json:"peers_connected"`
	PeersTotal        int                               `json:"peers_total"`
	Pools             []gonzbnetPoolOverview            `json:"pools"`
	PendingAdmissions int                               `json:"pending_admissions"`
	ReleaseEvidence   pgindex.FederationEvidenceSummary `json:"release_evidence"`
	ArticleEvidence   pgindex.FederationEvidenceSummary `json:"article_evidence"`
	Warnings          []string                          `json:"warnings"`
	Jobs              []gonzbnetRoleJob                 `json:"jobs"`
}

type gonzbnetActivityResponse struct {
	GeneratedAt     time.Time         `json:"generated_at"`
	Window          string            `json:"window"`
	From            time.Time         `json:"from"`
	To              time.Time         `json:"to"`
	FiveMinuteUntil time.Time         `json:"five_minute_until"`
	RetainedUntil   time.Time         `json:"retained_until"`
	Partial         bool              `json:"partial"`
	Items           []activity.Rollup `json:"items"`
}

func (ctrl *GoNZBNetAdminController) ReportingOverview(c *echo.Context) error {
	report, err := ctrl.buildRoleReport(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet store is unavailable")
	}
	pools, err := store.ListTrustPools(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	poolItems := make([]gonzbnetPoolOverview, 0, len(pools))
	releaseEvidence := pgindex.FederationEvidenceSummary{Statuses: map[string]int64{}}
	articleEvidence := pgindex.FederationEvidenceSummary{Statuses: map[string]int64{}}
	healthStore, _ := any(store).(gonzbnetPoolHealthStore)
	for _, pool := range pools {
		members, listErr := store.ListPoolMembers(c.Request().Context(), pool.PoolID)
		if listErr != nil {
			return jsonError(c, http.StatusInternalServerError, listErr.Error())
		}
		poolItems = append(poolItems, gonzbnetPoolOverview{PoolID: pool.PoolID, DisplayName: pool.DisplayName, Enabled: pool.Enabled, Members: len(members)})
		if pool.Enabled && healthStore != nil {
			health, healthErr := healthStore.GetFederationPoolHealthReport(c.Request().Context(), pool.PoolID, report.GeneratedAt)
			if healthErr == nil {
				mergeEvidenceSummary(&releaseEvidence, health.ReleaseHealth)
				mergeEvidenceSummary(&articleEvidence, health.ArticleAvailability)
			}
		}
	}
	peers, err := store.ListFederationPeerDiagnostics(c.Request().Context(), 500)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	connected := 0
	for _, peer := range peers {
		if peer.Enabled && (peer.Status == "ready" || peer.Status == "connected" || peer.Status == "ok") {
			connected++
		}
	}
	pendingAdmissions := 0
	if admissionStore, ok := any(store).(interface {
		ListFederationAdmissions(context.Context, string, string, int) ([]pgindex.FederationAdmissionRecord, error)
	}); ok {
		if pending, listErr := admissionStore.ListFederationAdmissions(c.Request().Context(), "", "pending", 500); listErr == nil {
			pendingAdmissions = len(pending)
		}
	}
	healthy, configured := 0, 0
	for _, job := range report.Jobs {
		if job.Configured {
			configured++
		}
		if job.Status == activity.StatusReady || job.Status == activity.StatusWorking {
			healthy++
		}
	}
	return c.JSON(http.StatusOK, gonzbnetOverviewResponse{
		GeneratedAt: report.GeneratedAt, NodeID: report.NodeID,
		NodeAlias:     ctrl.appCtx.Config.GoNZBNet.NodeAlias,
		ModuleEnabled: ctrl.appCtx.Config.Modules.GoNZBNet.Enabled,
		JobsHealthy:   healthy, JobsConfigured: configured,
		PeersConnected: connected, PeersTotal: len(peers), Pools: poolItems,
		PendingAdmissions: pendingAdmissions, Warnings: report.Warnings, Jobs: report.Jobs,
		ReleaseEvidence: releaseEvidence, ArticleEvidence: articleEvidence,
	})
}

func (ctrl *GoNZBNetAdminController) ReportingRoles(c *echo.Context) error {
	report, err := ctrl.buildRoleReport(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, report)
}

func (ctrl *GoNZBNetAdminController) ReportingActivity(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet store is unavailable")
	}
	activityStore, ok := any(store).(gonzbnetActivityStore)
	if !ok {
		return jsonError(c, http.StatusNotImplemented, "gonzbnet activity reporting is unavailable")
	}
	now := time.Now().UTC()
	window, duration := reportingWindow(queryParamTrimmed(c, "window"))
	identity, err := ctrl.localIdentity()
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	nodeID, err := identity.NodeID(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	query := pgindex.FederationActivityQuery{
		PoolID: queryParamTrimmed(c, "pool_id"), NodeID: queryParamTrimmed(c, "node_id"),
		Job: queryParamTrimmed(c, "job"), Since: now.Add(-duration), Limit: 10000,
	}
	items, err := activityStore.ListFederationActivityRollups(c.Request().Context(), query)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	for _, item := range activity.Default.CurrentRollups(nodeID) {
		if item.BucketStart.Before(query.Since) || query.PoolID != "" && item.PoolID != query.PoolID || query.NodeID != "" && item.NodeID != query.NodeID || query.Job != "" && item.Job != query.Job {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].BucketStart.Before(items[j].BucketStart) })
	return c.JSON(http.StatusOK, gonzbnetActivityResponse{
		GeneratedAt: now, Window: window, From: query.Since, To: now,
		FiveMinuteUntil: now.Add(-48 * time.Hour), RetainedUntil: now.Add(-90 * 24 * time.Hour),
		Partial: len(items) == 0, Items: items,
	})
}

func (ctrl *GoNZBNetAdminController) ReportingPoolHealth(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet store is unavailable")
	}
	reportStore, ok := any(store).(gonzbnetPoolHealthStore)
	if !ok {
		return jsonError(c, http.StatusNotImplemented, "gonzbnet pool health reporting is unavailable")
	}
	report, err := reportStore.GetFederationPoolHealthReport(c.Request().Context(), strings.TrimSpace(c.Param("pool_id")), time.Now().UTC())
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, report)
}

func (ctrl *GoNZBNetAdminController) ArticleAvailabilityDiagnostics(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet store is unavailable")
	}
	reportStore, ok := any(store).(gonzbnetArticleAvailabilityStore)
	if !ok {
		return jsonError(c, http.StatusNotImplemented, "article availability diagnostics are unavailable")
	}
	items, err := reportStore.ListArticleAvailabilityDiagnostics(c.Request().Context(), queryParamTrimmed(c, "pool_id"), parseIntDefault(queryParamTrimmed(c, "limit"), 100))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) buildRoleReport(ctx context.Context) (gonzbnetRoleReport, error) {
	now := time.Now().UTC()
	definitions := reportingActivityDefinitions(ctrl)
	activity.Default.Configure(definitions)
	components := activity.Default.Snapshot()
	identity, err := ctrl.localIdentity()
	if err != nil {
		return gonzbnetRoleReport{}, err
	}
	nodeID, err := identity.NodeID(ctx)
	if err != nil {
		return gonzbnetRoleReport{}, err
	}
	store, _ := ctrl.store()
	poolStore, _ := any(store).(gonzbnetActivityStore)
	if poolStore != nil {
		history, historyErr := poolStore.ListFederationActivityRollups(ctx, pgindex.FederationActivityQuery{
			NodeID: nodeID, Since: now.Add(-90 * 24 * time.Hour), Limit: 10000,
		})
		if historyErr != nil {
			return gonzbnetRoleReport{}, historyErr
		}
		for index := range components {
			for _, item := range history {
				if item.Component != components[index].Key {
					continue
				}
				components[index].LastAttemptAt = latestReportingTime(components[index].LastAttemptAt, item.LastAttemptAt)
				components[index].LastSuccessAt = latestReportingTime(components[index].LastSuccessAt, item.LastSuccessAt)
				components[index].LastFailureAt = latestReportingTime(components[index].LastFailureAt, item.LastFailureAt)
				if item.LastFailureAt != nil && components[index].LastFailureAt != nil && item.LastFailureAt.Equal(*components[index].LastFailureAt) {
					components[index].LastError = item.LastError
				}
			}
		}
	}
	poolCache := map[string][]string{}
	for index := range components {
		if !components[index].Configured || poolStore == nil || components[index].Key == activity.ComponentAdmissionPoller {
			continue
		}
		required := requiredCapabilitiesForActivity(components[index].Key)
		cacheKey := strings.Join(required, ",")
		pools, exists := poolCache[cacheKey]
		if !exists {
			pools, err = poolStore.ListActivePoolIDsForNodeCapabilities(ctx, nodeID, required)
			if err != nil {
				return gonzbnetRoleReport{}, err
			}
			poolCache[cacheKey] = pools
		}
		components[index].Pools = pools
		if len(pools) == 0 {
			components[index].Eligible = false
			components[index].Reason = "No active pool membership grants the required capability"
		}
	}
	for index := range components {
		components[index].Status = activity.DeriveStatus(components[index], now)
	}
	jobs := make([]gonzbnetRoleJob, 0, 5)
	for _, metadata := range reportingJobMetadata() {
		job := gonzbnetRoleJob{Key: metadata.key, Label: metadata.label, Description: metadata.description, Status: activity.StatusOff, Pools: []string{}, Warnings: []string{}, Components: []activity.Snapshot{}}
		for _, component := range components {
			if component.Job != metadata.key {
				continue
			}
			job.Components = append(job.Components, component)
			job.Configured = job.Configured || component.Configured
			job.Pools = append(job.Pools, component.Pools...)
			job.LastUsefulAt = latestReportingTime(job.LastUsefulAt, component.LastSuccessAt)
			job.Status = mergeReportingStatus(job.Status, component.Status, component.Configured)
			if component.Configured && component.Reason != "" {
				job.Warnings = append(job.Warnings, component.Label+": "+component.Reason)
			}
		}
		job.Pools = sortedUniqueStrings(job.Pools)
		jobs = append(jobs, job)
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	if cfg.ConsumerEnabled && !cfg.PullSyncEnabled && !cfg.PushSyncEnabled && !cfg.WebSocketGossipEnabled {
		for index := range jobs {
			if jobs[index].Key == activity.JobConsume {
				jobs[index].Warnings = append(jobs[index].Warnings, "No automatic synchronization is configured; data changes only after manual sync")
			}
		}
	}
	if cfg.ConsumerEnabled && !ctrl.appCtx.Config.Aggregator.Sources.GoNZBNet.Enabled {
		for index := range jobs {
			if jobs[index].Key == activity.JobConsume {
				jobs[index].Warnings = append(jobs[index].Warnings, "The aggregator GoNZBNet source is disabled, so federated releases are not shown to users")
			}
		}
	}
	warnings := make([]string, 0)
	for _, job := range jobs {
		if job.Status == activity.StatusBlocked || job.Status == activity.StatusDegraded {
			warnings = append(warnings, job.Label+" needs attention")
		}
		warnings = append(warnings, job.Warnings...)
	}
	return gonzbnetRoleReport{GeneratedAt: now, NodeID: nodeID, Jobs: jobs, Warnings: sortedUniqueStrings(warnings)}, nil
}

func reportingActivityDefinitions(ctrl *GoNZBNetAdminController) []activity.Definition {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.Config == nil {
		return nil
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	return activity.Definitions(activity.Configuration{
		ModuleEnabled: ctrl.appCtx.Config.Modules.GoNZBNet.Enabled, StoreReady: ctrl.appCtx.PGIndexStore != nil || ctrl.storeOverride != nil,
		ConsumerEnabled: cfg.ConsumerEnabled, ScannerEnabled: cfg.ScannerEnabled, IndexProjectionEnabled: cfg.IndexProjectionEnabled,
		ManifestCacheEnabled: cfg.ManifestCacheEnabled, ValidatorEnabled: cfg.ValidatorEnabled,
		HealthCheckerEnabled: cfg.HealthCheckerEnabled, CoverageEnabled: cfg.CoverageEnabled,
		SchedulerEnabled: cfg.SchedulerEnabled, PublishReleaseCardsEnabled: cfg.PublishReleaseCardsEnabled,
		HealthAttestationsEnabled: cfg.HealthAttestationsEnabled, PullSyncEnabled: cfg.PullSyncEnabled,
		PushSyncEnabled: cfg.PushSyncEnabled, WebSocketGossipEnabled: cfg.WebSocketGossipEnabled,
		RelayEnabled: cfg.RelayEnabled, PeerExchangeEnabled: cfg.PeerExchangeEnabled, CoverageMode: cfg.CoverageMode,
		PublishReleaseCardsInterval: time.Duration(cfg.PublishReleaseCardsIntervalMin * float64(time.Minute)),
		HealthAttestationsInterval:  time.Duration(cfg.HealthAttestationsIntervalMin * float64(time.Minute)),
		ValidationInterval:          time.Duration(cfg.ValidationIntervalMin * float64(time.Minute)),
		PullSyncInterval:            time.Duration(cfg.PullSyncIntervalMin * float64(time.Minute)),
		PushSyncInterval:            time.Duration(cfg.PushSyncIntervalMin * float64(time.Minute)),
		GossipInterval:              time.Duration(cfg.GossipIntervalMin * float64(time.Minute)),
		CoverageSchedulerInterval:   time.Duration(cfg.ScannerCheckpointIntervalSecs) * time.Second,
	})
}

func requiredCapabilitiesForActivity(component string) []string {
	switch component {
	case activity.ComponentReleasePublisher:
		return []string{capability.Scanner, capability.Indexer}
	case activity.ComponentHealthPublisher:
		return []string{capability.Validator, capability.HealthChecker}
	case activity.ComponentValidator:
		return []string{capability.Validator}
	case activity.ComponentCoverageScheduler:
		return []string{capability.CoverageCoordinator}
	case activity.ComponentScanner:
		return []string{capability.Scanner, capability.Coverage}
	case activity.ComponentManifestCache:
		return []string{capability.ManifestCache}
	case activity.ComponentRelay:
		return []string{capability.Relay}
	default:
		return []string{capability.Consumer}
	}
}

type reportingJobInfo struct{ key, label, description string }

func reportingJobMetadata() []reportingJobInfo {
	return []reportingJobInfo{
		{activity.JobConsume, "Find and use releases", "Receives pool releases, makes them searchable, and resolves verified manifests for local grabs."},
		{activity.JobContribute, "Contribute releases", "Shares eligible releases and manifests produced by this node."},
		{activity.JobVerify, "Verify release health", "Checks manifests and article reachability, then shares signed health evidence."},
		{activity.JobCoordinate, "Coordinate scanning", "Coordinates scanner assignments, claims, checkpoints, and stale-work reassignment."},
		{activity.JobConnection, "Connection layer", "Moves signed data between peers and helps pool admission and relay traffic."},
	}
}

func mergeReportingStatus(current, next activity.Status, configured bool) activity.Status {
	if !configured {
		return current
	}
	priority := map[activity.Status]int{
		activity.StatusOff: 0, activity.StatusReady: 1, activity.StatusStarting: 2,
		activity.StatusWorking: 3, activity.StatusDegraded: 4, activity.StatusBlocked: 5,
	}
	if priority[next] > priority[current] || current == activity.StatusOff {
		return next
	}
	return current
}

func reportingWindow(value string) (string, time.Duration) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1h":
		return "1h", time.Hour
	case "7d":
		return "7d", 7 * 24 * time.Hour
	case "30d":
		return "30d", 30 * 24 * time.Hour
	default:
		return "24h", 24 * time.Hour
	}
}

func latestReportingTime(left, right *time.Time) *time.Time {
	if right == nil {
		return left
	}
	if left == nil || right.After(*left) {
		copy := right.UTC()
		return &copy
	}
	return left
}

func sortedUniqueStrings(values []string) []string {
	sort.Strings(values)
	out := values[:0]
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(out) > 0 && out[len(out)-1] == value {
			continue
		}
		out = append(out, value)
	}
	return out
}

func mergeEvidenceSummary(target *pgindex.FederationEvidenceSummary, item pgindex.FederationEvidenceSummary) {
	target.Total += item.Total
	target.Fresh += item.Fresh
	target.Aging += item.Aging
	target.Stale += item.Stale
	target.Reporters += item.Reporters
	target.LastChecked = latestReportingTime(target.LastChecked, item.LastChecked)
	for status, count := range item.Statuses {
		target.Statuses[status] += count
	}
}
