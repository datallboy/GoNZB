package maintenance

import (
	"context"
	"errors"
	"testing"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type fakeRepo struct {
	result *pgindex.IndexerMaintenanceResult
	err    error
}

func (f fakeRepo) RunIndexerMaintenance(context.Context) (*pgindex.IndexerMaintenanceResult, error) {
	return f.result, f.err
}

type testLogger struct{}

func (testLogger) Debug(string, ...interface{}) {}
func (testLogger) Info(string, ...interface{})  {}
func (testLogger) Warn(string, ...interface{})  {}
func (testLogger) Error(string, ...interface{}) {}

func TestRunOnceWithMetricsIncludesWorkspaceCleanup(t *testing.T) {
	svc := NewService(fakeRepo{
		result: &pgindex.IndexerMaintenanceResult{
			PurgedOrphanReleases: 3,
		},
	}, testLogger{}, func(context.Context) (int, error) {
		return 7, nil
	})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("run once with metrics: %v", err)
	}
	if got := metrics["purged_inspect_workspaces"]; got != 7 {
		t.Fatalf("expected purged_inspect_workspaces=7, got %#v", got)
	}
}

func TestRunOnceWithMetricsFailsOnWorkspaceCleanupError(t *testing.T) {
	svc := NewService(fakeRepo{
		result: &pgindex.IndexerMaintenanceResult{},
	}, testLogger{}, func(context.Context) (int, error) {
		return 0, errors.New("boom")
	})

	if _, err := svc.RunOnceWithMetrics(context.Background()); err == nil {
		t.Fatal("expected workspace cleanup error")
	}
}
