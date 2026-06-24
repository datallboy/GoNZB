package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestRunIncludesMaterializerStagesByDefault(t *testing.T) {
	runs := make(chan StageName, 2)
	record := func(name StageName) RunnerFunc {
		return func(context.Context) error {
			runs <- name
			return nil
		}
	}

	stages := []Stage{
		{Name: StageScrapeLatest},
		{Name: StageScrapeBackfill},
		{Name: StagePosterMaterialize, Interval: time.Hour, Enabled: true, Runner: record(StagePosterMaterialize)},
		{Name: StageCrosspostPopularityRefresh, Interval: time.Hour, Enabled: true, Runner: record(StageCrosspostPopularityRefresh)},
		{Name: StageAssemble},
		{Name: StageRecoverYEnc},
		{Name: StageReleaseSummaryRefresh},
		{Name: StageRelease},
		{Name: StageReleaseGenerateNZB},
		{Name: StageReleaseArchiveNZB},
		{Name: StageReleasePurgeArchivedSources},
		{Name: StageInspectDiscoveryReadyRefresh},
		{Name: StageInspectPAR2ReadyRefresh},
		{Name: StageInspectArchiveReadyRefresh},
		{Name: StageInspectMediaReadyRefresh},
		{Name: StageInspectDiscovery},
		{Name: StageInspectPAR2},
		{Name: StageInspectNFO},
		{Name: StageInspectArchive},
		{Name: StageInspectPassword},
		{Name: StageInspectMedia},
		{Name: StageEnrichPreDB},
		{Name: StageEnrichTMDB},
		{Name: StageName("maintenance.dashboard_stats_refresh")},
		{Name: StageMaintenanceReleaseSourcePurge},
		{Name: StageName("maintenance.poster_queue_done_cleanup")},
		{Name: StageName("maintenance.inspect_ready_queue_cleanup")},
		{Name: StageName("maintenance.assembly_queue_stale_cleanup")},
		{Name: StageName("maintenance.readiness_cleanup")},
		{Name: StageName("maintenance.runtime_history_cleanup")},
		{Name: StageName("maintenance.grouping_evidence_cleanup")},
		{Name: StageName("maintenance.header_payload_purge")},
		{Name: StageMaintenance},
	}
	svc := New(nil, stages)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- svc.Run(ctx)
	}()

	seen := map[StageName]bool{}
	for len(seen) < 2 {
		select {
		case name := <-runs:
			seen[name] = true
		case <-time.After(250 * time.Millisecond):
			t.Fatalf("timed out waiting for materializer stage runs, seen=%v", seen)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed out waiting for supervisor shutdown")
	}
}

func TestRunPipelineIncludesReadyRefreshAndMaintenanceExcludesPipeline(t *testing.T) {
	runs := make(chan StageName, 4)
	record := func(name StageName) RunnerFunc {
		return func(context.Context) error {
			runs <- name
			return nil
		}
	}

	stages := []Stage{
		{Name: StageScrapeLatest},
		{Name: StageScrapeBackfill},
		{Name: StagePosterMaterialize},
		{Name: StageCrosspostPopularityRefresh},
		{Name: StageAssemble},
		{Name: StageRecoverYEnc},
		{Name: StageReleaseSummaryRefresh},
		{Name: StageRelease},
		{Name: StageReleaseGenerateNZB},
		{Name: StageReleaseArchiveNZB},
		{Name: StageInspectDiscoveryReadyRefresh, Interval: time.Hour, Enabled: true, Runner: record(StageInspectDiscoveryReadyRefresh)},
		{Name: StageInspectPAR2ReadyRefresh},
		{Name: StageInspectArchiveReadyRefresh},
		{Name: StageInspectMediaReadyRefresh},
		{Name: StageInspectDiscovery},
		{Name: StageInspectPAR2},
		{Name: StageInspectNFO},
		{Name: StageInspectArchive},
		{Name: StageInspectPassword},
		{Name: StageInspectMedia},
		{Name: StageEnrichPreDB},
		{Name: StageEnrichTMDB},
		{Name: StageName("maintenance.dashboard_stats_refresh"), Interval: time.Hour, Enabled: true, Runner: record(StageName("maintenance.dashboard_stats_refresh"))},
		{Name: StageMaintenanceReleaseSourcePurge},
		{Name: StageName("maintenance.poster_queue_done_cleanup")},
		{Name: StageName("maintenance.inspect_ready_queue_cleanup")},
		{Name: StageName("maintenance.assembly_queue_stale_cleanup")},
		{Name: StageName("maintenance.readiness_cleanup")},
		{Name: StageName("maintenance.runtime_history_cleanup")},
		{Name: StageName("maintenance.grouping_evidence_cleanup")},
		{Name: StageName("maintenance.header_payload_purge")},
		{Name: StageMaintenance},
	}
	svc := New(nil, stages)

	pipelineCtx, cancelPipeline := context.WithCancel(context.Background())
	pipelineDone := make(chan error, 1)
	go func() {
		pipelineDone <- svc.RunPipeline(pipelineCtx)
	}()

	select {
	case name := <-runs:
		if name != StageInspectDiscoveryReadyRefresh {
			t.Fatalf("expected pipeline ready-refresh stage, got %s", name)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed out waiting for pipeline stage")
	}
	cancelPipeline()
	if err := <-pipelineDone; err != nil {
		t.Fatalf("run pipeline: %v", err)
	}

	maintenanceCtx, cancelMaintenance := context.WithCancel(context.Background())
	maintenanceDone := make(chan error, 1)
	go func() {
		maintenanceDone <- svc.RunMaintenance(maintenanceCtx)
	}()

	select {
	case name := <-runs:
		if name != StageName("maintenance.dashboard_stats_refresh") {
			t.Fatalf("expected maintenance stage, got %s", name)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed out waiting for maintenance stage")
	}
	cancelMaintenance()
	if err := <-maintenanceDone; err != nil {
		t.Fatalf("run maintenance: %v", err)
	}

	select {
	case name := <-runs:
		t.Fatalf("unexpected extra stage run: %s", name)
	default:
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

func TestRunStageOnceSkipsWhenStageGateBlocks(t *testing.T) {
	tracker := &fakeTracker{
		claimResult: &pgindex.IndexerStageClaimResult{
			Claimed: true,
			Run:     &pgindex.IndexerStageRun{ID: 88, StageName: string(StageAssemble)},
		},
	}

	svc := New(nil, []Stage{
		{
			Name:     StageAssemble,
			Interval: time.Second,
			Enabled:  true,
			Runner: RunnerFunc(func(context.Context) error {
				t.Fatal("runner should not execute when the stage gate blocks")
				return nil
			}),
		},
	}, Options{
		Tracker: tracker,
		Owner:   "test-owner",
		StageGate: func(context.Context, Stage, string) (StageGateDecision, error) {
			return StageGateDecision{Allowed: false, Reason: "low space"}, nil
		},
	})

	if err := svc.RunStageOnce(context.Background(), StageAssemble); err != nil {
		t.Fatalf("run stage once: %v", err)
	}
	if tracker.claims != 0 {
		t.Fatalf("expected stage gate to block before claim, got %d claims", tracker.claims)
	}
}

func TestRunStageOnceSerializesBinarySourceWriters(t *testing.T) {
	startedAssemble := make(chan struct{})
	releaseAssemble := make(chan struct{})
	doneAssemble := make(chan error, 1)
	var ranPurge bool

	svc := New(nil, []Stage{
		{
			Name:     StageAssemble,
			Interval: time.Second,
			Enabled:  true,
			Runner: RunnerFunc(func(context.Context) error {
				close(startedAssemble)
				<-releaseAssemble
				return nil
			}),
		},
		{
			Name:     StageReleasePurgeArchivedSources,
			Interval: time.Second,
			Enabled:  true,
			Runner: RunnerFunc(func(context.Context) error {
				ranPurge = true
				return nil
			}),
		},
	})

	go func() {
		doneAssemble <- svc.RunStageOnce(context.Background(), StageAssemble)
	}()

	select {
	case <-startedAssemble:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed out waiting for assemble to start")
	}

	if err := svc.RunStageOnce(context.Background(), StageReleasePurgeArchivedSources); err != nil {
		t.Fatalf("run purge while assemble active: %v", err)
	}
	if ranPurge {
		t.Fatal("purge runner should not execute while assemble holds the binary source write lane")
	}

	close(releaseAssemble)
	select {
	case err := <-doneAssemble:
		if err != nil {
			t.Fatalf("assemble returned error: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed out waiting for assemble to finish")
	}
}

func TestRunStageOnceSuppressesRepeatedBlockedWarnings(t *testing.T) {
	log := &fakeSupervisorLogger{}
	svc := New(log, []Stage{
		{
			Name:     StageScrapeLatest,
			Interval: time.Second,
			Enabled:  true,
			Runner: RunnerFunc(func(context.Context) error {
				t.Fatal("runner should not execute when the stage gate blocks")
				return nil
			}),
		},
	}, Options{
		StageGate: func(context.Context, Stage, string) (StageGateDecision, error) {
			return StageGateDecision{Allowed: false, Reason: "same reason"}, nil
		},
	})

	if err := svc.RunStageOnce(context.Background(), StageScrapeLatest); err != nil {
		t.Fatalf("first run stage once: %v", err)
	}
	if err := svc.RunStageOnce(context.Background(), StageScrapeLatest); err != nil {
		t.Fatalf("second run stage once: %v", err)
	}
	if got := log.warnCountContaining("index stage blocked"); got != 1 {
		t.Fatalf("expected 1 blocked warning, got %d (%v)", got, log.warns)
	}
}

func TestRunStageOnceLogsBlockedWarningWhenReasonChanges(t *testing.T) {
	log := &fakeSupervisorLogger{}
	reasons := []string{"reason one", "reason two"}
	var idx int
	svc := New(log, []Stage{
		{
			Name:     StageScrapeLatest,
			Interval: time.Second,
			Enabled:  true,
			Runner: RunnerFunc(func(context.Context) error {
				t.Fatal("runner should not execute when the stage gate blocks")
				return nil
			}),
		},
	}, Options{
		StageGate: func(context.Context, Stage, string) (StageGateDecision, error) {
			reason := reasons[idx]
			if idx < len(reasons)-1 {
				idx++
			}
			return StageGateDecision{Allowed: false, Reason: reason}, nil
		},
	})

	if err := svc.RunStageOnce(context.Background(), StageScrapeLatest); err != nil {
		t.Fatalf("first run stage once: %v", err)
	}
	if err := svc.RunStageOnce(context.Background(), StageScrapeLatest); err != nil {
		t.Fatalf("second run stage once: %v", err)
	}
	if got := log.warnCountContaining("index stage blocked"); got != 2 {
		t.Fatalf("expected 2 blocked warnings after reason change, got %d (%v)", got, log.warns)
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

func TestRunStageOncePersistsMetricsFromResultRunner(t *testing.T) {
	tracker := &fakeTracker{
		claimResult: &pgindex.IndexerStageClaimResult{
			Claimed: true,
			Run:     &pgindex.IndexerStageRun{ID: 51, StageName: string(StageAssemble)},
		},
	}

	svc := New(nil, []Stage{
		{
			Name:     StageAssemble,
			Interval: time.Second,
			Enabled:  true,
			Runner: ResultRunnerFunc(func(context.Context) (json.RawMessage, error) {
				return json.RawMessage(`{"processed_headers":12}`), nil
			}),
		},
	}, Options{Tracker: tracker, Owner: "test-owner"})

	if err := svc.RunStageOnce(context.Background(), StageAssemble); err != nil {
		t.Fatalf("run stage once: %v", err)
	}
	if string(tracker.lastFinish.MetricsJSON) != `{"processed_headers":12}` {
		t.Fatalf("unexpected metrics payload: %s", string(tracker.lastFinish.MetricsJSON))
	}
}

func TestRunStageOnceFailsTrackedRunWhenRunnerErrors(t *testing.T) {
	tracker := &fakeTracker{
		claimResult: &pgindex.IndexerStageClaimResult{
			Claimed: true,
			Run: &pgindex.IndexerStageRun{
				ID:        77,
				StageName: string(StageInspectArchive),
			},
		},
	}

	svc := New(nil, []Stage{
		{
			Name:     StageInspectArchive,
			Interval: time.Second,
			Enabled:  true,
			Runner: RunnerFunc(func(context.Context) error {
				return context.DeadlineExceeded
			}),
		},
	}, Options{
		Tracker: tracker,
		Owner:   "test-owner",
	})

	err := svc.RunStageOnce(context.Background(), StageInspectArchive)
	if err == nil {
		t.Fatal("expected runner error, got nil")
	}
	if tracker.fails != 1 {
		t.Fatalf("expected 1 failed finish call, got %d", tracker.fails)
	}
	if tracker.completes != 0 {
		t.Fatalf("expected no completion calls, got %d", tracker.completes)
	}
}

type fakeTracker struct {
	mu          sync.Mutex
	claimResult *pgindex.IndexerStageClaimResult
	claims      int
	heartbeats  int
	completes   int
	fails       int
	lastFinish  pgindex.IndexerStageFinishRequest
}

type fakeSupervisorLogger struct {
	mu    sync.Mutex
	warns []string
}

func (f *fakeSupervisorLogger) Debug(string, ...interface{}) {}
func (f *fakeSupervisorLogger) Info(string, ...interface{})  {}
func (f *fakeSupervisorLogger) Error(string, ...interface{}) {}

func (f *fakeSupervisorLogger) Warn(format string, v ...interface{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warns = append(f.warns, fmt.Sprintf(format, v...))
}

func (f *fakeSupervisorLogger) warnCountContaining(fragment string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, entry := range f.warns {
		if strings.Contains(entry, fragment) {
			count++
		}
	}
	return count
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

func (f *fakeTracker) CompleteIndexerStageRun(_ context.Context, req pgindex.IndexerStageFinishRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completes++
	f.lastFinish = req
	return nil
}

func (f *fakeTracker) FailIndexerStageRun(_ context.Context, req pgindex.IndexerStageFinishRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fails++
	f.lastFinish = req
	return nil
}
