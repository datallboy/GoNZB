package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type ModuleStatus struct {
	Enabled bool    `json:"enabled"`
	Ready   bool    `json:"ready"`
	Status  string  `json:"status"`
	Checks  []Check `json:"checks"`
}

type ProbeResponse struct {
	Status    string                  `json:"status"`
	Timestamp string                  `json:"timestamp"`
	Modules   map[string]ModuleStatus `json:"modules"`
}

func Health(appCtx *app.Context) ProbeResponse {
	modules := map[string]ModuleStatus{}

	if appCtx != nil && appCtx.Config != nil {
		cfg := appCtx.Config.Modules
		modules["api"] = simpleHealthModule(cfg.API.Enabled)
		modules["web_ui"] = simpleHealthModule(cfg.WebUI.Enabled)
		modules["downloader"] = simpleHealthModule(cfg.Downloader.Enabled)
		modules["aggregator"] = simpleHealthModule(cfg.Aggregator.Enabled)
		modules["usenet_indexer"] = simpleHealthModule(cfg.UsenetIndexer.Enabled)
	}

	return ProbeResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Modules:   modules,
	}
}

func Readiness(ctx context.Context, appCtx *app.Context) (int, ProbeResponse) {
	modules := map[string]ModuleStatus{}
	overallReady := true

	if appCtx == nil || appCtx.Config == nil {
		return http.StatusServiceUnavailable, ProbeResponse{
			Status:    "not_ready",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Modules:   modules,
		}
	}

	cfg := appCtx.Config.Modules

	modules["api"] = simpleReadyModule(cfg.API.Enabled)
	modules["web_ui"] = simpleReadyModule(cfg.WebUI.Enabled)

	downloader := evaluateDownloader(ctx, appCtx)
	modules["downloader"] = downloader
	if downloader.Enabled && !downloader.Ready {
		overallReady = false
	}

	aggregator := evaluateAggregator(ctx, appCtx)
	modules["aggregator"] = aggregator
	if aggregator.Enabled && !aggregator.Ready {
		overallReady = false
	}

	usenetIndexer := evaluateUsenetIndexer(ctx, appCtx)
	modules["usenet_indexer"] = usenetIndexer
	if usenetIndexer.Enabled && !usenetIndexer.Ready {
		overallReady = false
	}

	status := "ready"
	code := http.StatusOK
	if !overallReady {
		status = "not_ready"
		code = http.StatusServiceUnavailable
	}

	return code, ProbeResponse{
		Status:    status,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Modules:   modules,
	}
}

func ValidateStartupReadiness(ctx context.Context, appCtx *app.Context) error {
	code, report := Readiness(ctx, appCtx)
	if code == http.StatusOK {
		return nil
	}

	failures := make([]string, 0)
	for name, module := range report.Modules {
		if !module.Enabled || module.Ready {
			continue
		}
		failures = append(failures, name)
	}

	if len(failures) == 0 {
		return fmt.Errorf("startup readiness failed")
	}

	return fmt.Errorf("startup readiness failed for modules: %s", strings.Join(failures, ", "))
}

func simpleHealthModule(enabled bool) ModuleStatus {
	if !enabled {
		return ModuleStatus{
			Enabled: false,
			Ready:   false,
			Status:  "disabled",
			Checks:  []Check{},
		}
	}

	return ModuleStatus{
		Enabled: true,
		Ready:   true,
		Status:  "up",
		Checks:  []Check{},
	}
}

func simpleReadyModule(enabled bool) ModuleStatus {
	if !enabled {
		return ModuleStatus{
			Enabled: false,
			Ready:   false,
			Status:  "disabled",
			Checks:  []Check{},
		}
	}

	return ModuleStatus{
		Enabled: true,
		Ready:   true,
		Status:  "ready",
		Checks:  []Check{},
	}
}

func evaluateDownloader(ctx context.Context, appCtx *app.Context) ModuleStatus {
	if !appCtx.Config.Modules.Downloader.Enabled {
		return simpleReadyModule(false)
	}

	checks := []Check{
		boolCheck("job_store", appCtx.JobStore != nil, "downloader job store is required"),
		boolCheck("queue_file_store", appCtx.QueueFileStore != nil, "queue file store is required"),
		boolCheck("queue_manager", appCtx.Queue != nil, "queue manager is required"),
		boolCheck("downloader_runtime", appCtx.Downloader != nil, "downloader runtime is required"),
		boolCheck("nntp_manager", appCtx.NNTP != nil, "NNTP manager is required"),
		boolCheck("nzb_parser", appCtx.NZBParser != nil, "NZB parser is required"),
	}

	ready := allChecksOK(checks)

	if appCtx.JobStore != nil {
		checks = append(checks, errorCheck("job_store_ping", appCtx.JobStore.Ping(ctx)))
		checks = append(checks, errorCheck("job_store_schema", appCtx.JobStore.ValidateSchema(ctx)))
		ready = ready && checks[len(checks)-1].Status == "ok" && checks[len(checks)-2].Status == "ok"
	}

	return ModuleStatus{
		Enabled: true,
		Ready:   ready,
		Status:  readyStatus(ready),
		Checks:  checks,
	}
}

func evaluateAggregator(ctx context.Context, appCtx *app.Context) ModuleStatus {
	if !appCtx.Config.Modules.Aggregator.Enabled {
		return simpleReadyModule(false)
	}

	checks := []Check{
		boolCheck("aggregator_runtime", appCtx.Aggregator != nil, "aggregator runtime is required"),
		boolCheck("indexer_sources", len(appCtx.Config.Indexers) > 0, "at least one indexer source must be configured"),
		boolCheck("payload_store", appCtx.BlobStore != nil, "payload store is required"),
	}

	ready := allChecksOK(checks)

	if appCtx.Config.Store.SearchPersistenceEnabled {
		if appCtx.JobStore == nil {
			checks = append(checks, Check{
				Name:   "aggregator_cache_store",
				Status: "fail",
				Detail: "job store is required when search persistence is enabled",
			})
			ready = false
		} else {
			checks = append(checks, errorCheck("aggregator_cache_ping", appCtx.JobStore.Ping(ctx)))
			checks = append(checks, errorCheck("aggregator_cache_schema", appCtx.JobStore.ValidateSchema(ctx)))
			ready = ready && checks[len(checks)-1].Status == "ok" && checks[len(checks)-2].Status == "ok"
		}
	}

	return ModuleStatus{
		Enabled: true,
		Ready:   ready,
		Status:  readyStatus(ready),
		Checks:  checks,
	}
}

func evaluateUsenetIndexer(ctx context.Context, appCtx *app.Context) ModuleStatus {
	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		return simpleReadyModule(false)
	}

	checks := []Check{
		boolCheck("usenet_indexer_runtime", appCtx.UsenetIndexer != nil, "usenet indexer runtime is required"),
		boolCheck("pgindex_store", appCtx.PGIndexStore != nil, "pgindex store is required"),
	}

	ready := allChecksOK(checks)

	if appCtx.PGIndexStore != nil {
		checks = append(checks, errorCheck("pgindex_ping", appCtx.PGIndexStore.Ping(ctx)))
		checks = append(checks, errorCheck("pgindex_schema", appCtx.PGIndexStore.ValidateSchema(ctx)))
		ready = ready && checks[len(checks)-1].Status == "ok" && checks[len(checks)-2].Status == "ok"
	}

	if appCtx.SettingsStore != nil {
		checks = append(checks, errorCheck("settings_ping", appCtx.SettingsStore.Ping(ctx)))
		checks = append(checks, errorCheck("settings_schema", appCtx.SettingsStore.ValidateSchema(ctx)))
		ready = ready && checks[len(checks)-1].Status == "ok" && checks[len(checks)-2].Status == "ok"
	}

	return ModuleStatus{
		Enabled: true,
		Ready:   ready,
		Status:  readyStatus(ready),
		Checks:  checks,
	}
}

func boolCheck(name string, ok bool, detail string) Check {
	if ok {
		return Check{Name: name, Status: "ok"}
	}
	return Check{Name: name, Status: "fail", Detail: detail}
}

func errorCheck(name string, err error) Check {
	if err == nil {
		return Check{Name: name, Status: "ok"}
	}
	return Check{Name: name, Status: "fail", Detail: err.Error()}
}

func allChecksOK(checks []Check) bool {
	for _, check := range checks {
		if check.Status != "ok" {
			return false
		}
	}
	return true
}

func readyStatus(ready bool) string {
	if ready {
		return "ready"
	}
	return "not_ready"
}
