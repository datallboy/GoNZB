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

	downloader := evaluateRuntimeModule(ctx, appCtx.RuntimeModule("downloader"))
	modules["downloader"] = downloader
	if downloader.Enabled && !downloader.Ready {
		overallReady = false
	}

	aggregator := evaluateRuntimeModule(ctx, appCtx.RuntimeModule("aggregator"))
	modules["aggregator"] = aggregator
	if aggregator.Enabled && !aggregator.Ready {
		overallReady = false
	}

	usenetIndexer := evaluateRuntimeModule(ctx, appCtx.RuntimeModule("usenet_indexer"))
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

func evaluateRuntimeModule(ctx context.Context, module app.RuntimeModule) ModuleStatus {
	if module == nil || !module.Enabled() {
		return simpleReadyModule(false)
	}

	runtimeChecks := module.ReadinessChecks(ctx)
	checks := make([]Check, 0, len(runtimeChecks))
	ready := true

	for _, runtimeCheck := range runtimeChecks {
		status := "ok"
		if !runtimeCheck.OK {
			status = "fail"
			ready = false
		}
		checks = append(checks, Check{
			Name:   runtimeCheck.Name,
			Status: status,
			Detail: runtimeCheck.Detail,
		})
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
