package supervisor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestRunStageOnceRejectsDisabledStage(t *testing.T) {
	svc := New(nil, []Stage{
		{
			Name:     StageRelease,
			Interval: time.Second,
			Enabled:  false,
			Runner: RunnerFunc(func(context.Context) error {
				t.Fatal("disabled stage runner should not be called")
				return nil
			}),
		},
	})

	err := svc.RunStageOnce(context.Background(), StageRelease)
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled stage error, got %v", err)
	}
}

func TestRunStagesOnceHonorsRequestedOrder(t *testing.T) {
	var (
		mu    sync.Mutex
		order []StageName
	)

	record := func(name StageName) RunnerFunc {
		return func(context.Context) error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}
	}

	svc := New(nil, []Stage{
		{Name: StageScrapeLatest, Interval: time.Second, Enabled: true, Runner: record(StageScrapeLatest)},
		{Name: StageAssemble, Interval: time.Second, Enabled: true, Runner: record(StageAssemble)},
		{Name: StageRelease, Interval: time.Second, Enabled: true, Runner: record(StageRelease)},
	})

	if err := svc.RunStagesOnce(context.Background(), StageRelease, StageScrapeLatest, StageAssemble); err != nil {
		t.Fatalf("run stages once: %v", err)
	}

	want := []StageName{StageRelease, StageScrapeLatest, StageAssemble}
	if len(order) != len(want) {
		t.Fatalf("expected %d stage calls, got %d (%v)", len(want), len(order), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("expected order %v, got %v", want, order)
		}
	}
}

func TestRunSelectedRunsStageOnStartupAndInterval(t *testing.T) {
	runs := make(chan struct{}, 4)

	svc := New(nil, []Stage{
		{
			Name:     StageAssemble,
			Interval: 10 * time.Millisecond,
			Enabled:  true,
			Runner: RunnerFunc(func(context.Context) error {
				runs <- struct{}{}
				return nil
			}),
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- svc.RunSelected(ctx, StageAssemble)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-runs:
		case <-time.After(250 * time.Millisecond):
			t.Fatalf("timed out waiting for run %d", i+1)
		}
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run selected returned error: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed out waiting for supervisor shutdown")
	}
}

func TestRunStageOnceSkipsWhenTrackerDoesNotClaim(t *testing.T) {
	tracker := &fakeTracker{
		claimResult: &pgindex.IndexerStageClaimResult{
			Claimed: false,
			Reason:  "paused",
		},
	}

	svc := New(nil, []Stage{
		{
			Name:     StageInspectMedia,
			Interval: time.Second,
			Enabled:  true,
			Runner: RunnerFunc(func(context.Context) error {
				t.Fatal("runner should not execute when claim is skipped")
				return nil
			}),
		},
	}, Options{
		Tracker: tracker,
		Owner:   "test-owner",
	})

	if err := svc.RunStageOnce(context.Background(), StageInspectMedia); err != nil {
		t.Fatalf("run stage once: %v", err)
	}
	if tracker.claims != 1 {
		t.Fatalf("expected 1 claim, got %d", tracker.claims)
	}
}

func TestRunStageOnceCompletesTrackedRun(t *testing.T) {
	tracker := &fakeTracker{
		claimResult: &pgindex.IndexerStageClaimResult{
			Claimed: true,
			Run: &pgindex.IndexerStageRun{
				ID:        42,
				StageName: string(StageRelease),
			},
		},
	}

	svc := New(nil, []Stage{
		{
			Name:     StageRelease,
			Interval: time.Second,
			Enabled:  true,
			Runner: RunnerFunc(func(context.Context) error {
				time.Sleep(15 * time.Millisecond)
				return nil
			}),
		},
	}, Options{
		Tracker:           tracker,
		Owner:             "test-owner",
		LeaseDuration:     20 * time.Millisecond,
		HeartbeatInterval: 5 * time.Millisecond,
	})

	if err := svc.RunStageOnce(context.Background(), StageRelease); err != nil {
		t.Fatalf("run stage once: %v", err)
	}
	if tracker.completes != 1 {
		t.Fatalf("expected 1 completion, got %d", tracker.completes)
	}
	if tracker.heartbeats == 0 {
		t.Fatalf("expected at least one heartbeat, got %d", tracker.heartbeats)
	}
	if tracker.fails != 0 {
		t.Fatalf("expected no failures, got %d", tracker.fails)
	}
}

type fakeTracker struct {
	mu          sync.Mutex
	claimResult *pgindex.IndexerStageClaimResult
	claims      int
	heartbeats  int
	completes   int
	fails       int
}

func (f *fakeTracker) ClaimIndexerStage(context.Context, pgindex.IndexerStageClaimRequest) (*pgindex.IndexerStageClaimResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	return f.claimResult, nil
}

func (f *fakeTracker) HeartbeatIndexerStageRun(context.Context, int64, string, time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeats++
	return nil
}

func (f *fakeTracker) CompleteIndexerStageRun(context.Context, pgindex.IndexerStageFinishRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completes++
	return nil
}

func (f *fakeTracker) FailIndexerStageRun(context.Context, pgindex.IndexerStageFinishRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fails++
	return nil
}
