package telemetry

import (
	"context"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
)

func TestReadinessUsesRuntimeModuleChecks(t *testing.T) {
	appCtx := &app.Context{
		Config: &config.Config{
			Modules: config.ModulesConfig{
				API:           config.ModuleToggle{Enabled: true},
				Downloader:    config.ModuleToggle{Enabled: true},
				Aggregator:    config.ModuleToggle{Enabled: true},
				UsenetIndexer: config.ModuleToggle{Enabled: false},
			},
		},
	}

	appCtx.RegisterRuntimeModules(
		fakeRuntimeModule{
			name:    "downloader",
			enabled: true,
			checks: []app.RuntimeCheck{
				{Name: "queue_manager", OK: true},
			},
		},
		fakeRuntimeModule{
			name:    "aggregator",
			enabled: true,
			checks: []app.RuntimeCheck{
				{Name: "aggregator_runtime", OK: false, Detail: "aggregator runtime is required"},
			},
		},
	)

	code, report := Readiness(context.Background(), appCtx)
	if code != 503 {
		t.Fatalf("expected service unavailable, got %d", code)
	}
	if report.Modules["downloader"].Status != "ready" {
		t.Fatalf("expected downloader ready, got %q", report.Modules["downloader"].Status)
	}
	if report.Modules["aggregator"].Status != "not_ready" {
		t.Fatalf("expected aggregator not_ready, got %q", report.Modules["aggregator"].Status)
	}
	if len(report.Modules["aggregator"].Checks) != 1 || report.Modules["aggregator"].Checks[0].Detail == "" {
		t.Fatalf("expected aggregator failure detail in checks, got %#v", report.Modules["aggregator"].Checks)
	}
}

type fakeRuntimeModule struct {
	name    string
	enabled bool
	checks  []app.RuntimeCheck
}

func (m fakeRuntimeModule) Name() string                                       { return m.name }
func (m fakeRuntimeModule) Enabled() bool                                      { return m.enabled }
func (m fakeRuntimeModule) Build(context.Context) error                        { return nil }
func (m fakeRuntimeModule) Start(context.Context) error                        { return nil }
func (m fakeRuntimeModule) Reload(context.Context) error                       { return nil }
func (m fakeRuntimeModule) Close() error                                       { return nil }
func (m fakeRuntimeModule) ReadinessChecks(context.Context) []app.RuntimeCheck { return m.checks }
