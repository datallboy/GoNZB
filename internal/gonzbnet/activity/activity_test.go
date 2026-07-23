package activity

import (
	"errors"
	"testing"
	"time"
)

func TestEventDrivenComponentStaysReadyWithoutWork(t *testing.T) {
	registry := NewRegistry()
	registry.Configure([]Definition{{Key: ComponentManifestResolver, Job: JobConsume, ExecutionMode: OnDemand, Configured: true, Eligible: true}})

	snapshots := registry.Snapshot()
	if len(snapshots) != 1 || snapshots[0].Status != StatusReady {
		t.Fatalf("expected unused on-demand component to be ready, got %+v", snapshots)
	}
}

func TestScheduledComponentDegradesAfterRepeatedFailures(t *testing.T) {
	registry := NewRegistry()
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Configure([]Definition{{Key: ComponentPullSync, Job: JobConnection, ExecutionMode: Scheduled, Interval: time.Minute, Configured: true, Eligible: true}})
	for range 3 {
		registry.Record(ComponentPullSync, "pool-a", Result{Err: errors.New("peer unavailable")})
	}

	snapshot := registry.Snapshot()[0]
	if snapshot.Status != StatusDegraded || snapshot.ConsecutiveFailures != 3 {
		t.Fatalf("expected degraded component, got %+v", snapshot)
	}
	if snapshot.LastError != "peer unavailable" {
		t.Fatalf("expected sanitized error, got %q", snapshot.LastError)
	}
}

func TestDrainAndRestoreRollups(t *testing.T) {
	registry := NewRegistry()
	registry.Configure([]Definition{{Key: ComponentValidator, Job: JobVerify, ExecutionMode: Scheduled, Configured: true, Eligible: true}})
	registry.Record(ComponentValidator, "pool-a", Result{ItemsIn: 3, ItemsOut: 2})

	items := registry.DrainRollups("node-a")
	if len(items) != 1 || items[0].NodeID != "node-a" || items[0].Successes != 1 || items[0].ItemsIn != 3 {
		t.Fatalf("unexpected rollups: %+v", items)
	}
	registry.RestoreRollups(items)
	restored := registry.DrainRollups("node-a")
	if len(restored) != 1 || restored[0].Attempts != 1 {
		t.Fatalf("unexpected restored rollups: %+v", restored)
	}
}

func TestUsefulWorkRequiresProcessedData(t *testing.T) {
	registry := NewRegistry()
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Configure([]Definition{{Key: ComponentValidator, Job: JobVerify, ExecutionMode: Scheduled, Configured: true, Eligible: true}})

	registry.Record(ComponentValidator, "pool-a", Result{})
	snapshot := registry.Snapshot()[0]
	if snapshot.LastSuccessAt == nil {
		t.Fatal("expected an empty successful pass to record its check time")
	}
	if snapshot.LastUsefulAt != nil {
		t.Fatalf("expected an empty successful pass not to count as useful work, got %v", snapshot.LastUsefulAt)
	}

	now = now.Add(time.Minute)
	registry.Record(ComponentValidator, "pool-a", Result{ItemsIn: 1, ItemsOut: 1})
	snapshot = registry.Snapshot()[0]
	if snapshot.LastUsefulAt == nil || !snapshot.LastUsefulAt.Equal(now) {
		t.Fatalf("expected processed data to record useful work at %v, got %v", now, snapshot.LastUsefulAt)
	}
	rollups := registry.DrainRollups("node-a")
	if len(rollups) != 1 || rollups[0].LastUsefulAt == nil || !rollups[0].LastUsefulAt.Equal(now) {
		t.Fatalf("expected useful-work time in rollup, got %+v", rollups)
	}
}
