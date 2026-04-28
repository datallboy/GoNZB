package supervisor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type StageName string

const (
	StageScrapeLatest     StageName = "scrape_latest"
	StageScrapeBackfill   StageName = "scrape_backfill"
	StageAssemble         StageName = "assemble"
	StageRelease          StageName = "release"
	StageInspectDiscovery StageName = "inspect_discovery"
	StageInspectPAR2      StageName = "inspect_par2"
	StageInspectNFO       StageName = "inspect_nfo"
	StageInspectArchive   StageName = "inspect_archive"
	StageInspectPassword  StageName = "inspect_password"
	StageInspectMedia     StageName = "inspect_media"
	StageEnrichPreDB      StageName = "enrich_predb"
	StageEnrichTMDB       StageName = "enrich_tmdb"
	StageMaintenance      StageName = "indexer_maintenance"
)

type Runner interface {
	Run(ctx context.Context) error
}

type RunnerFunc func(ctx context.Context) error

func (fn RunnerFunc) Run(ctx context.Context) error {
	return fn(ctx)
}

type Stage struct {
	Name        StageName
	Interval    time.Duration
	Enabled     bool
	BatchSize   int
	Concurrency int
	Backoff     time.Duration
	Runner      Runner
}

type Tracker interface {
	ClaimIndexerStage(ctx context.Context, req pgindex.IndexerStageClaimRequest) (*pgindex.IndexerStageClaimResult, error)
	HeartbeatIndexerStageRun(ctx context.Context, runID int64, owner string, leaseDuration time.Duration) error
	CompleteIndexerStageRun(ctx context.Context, req pgindex.IndexerStageFinishRequest) error
	FailIndexerStageRun(ctx context.Context, req pgindex.IndexerStageFinishRequest) error
}

type Options struct {
	Tracker           Tracker
	Owner             string
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
}

type Supervisor struct {
	log               logger
	stages            map[StageName]Stage
	tracker           Tracker
	owner             string
	leaseDuration     time.Duration
	heartbeatInterval time.Duration
}

func New(log logger, stages []Stage, options ...Options) *Supervisor {
	var opts Options
	if len(options) > 0 {
		opts = options[0]
	}
	if opts.Owner == "" {
		opts.Owner = fmt.Sprintf("indexer-supervisor-%d", time.Now().UnixNano())
	}
	if opts.LeaseDuration <= 0 {
		opts.LeaseDuration = 30 * time.Second
	}
	if opts.HeartbeatInterval <= 0 || opts.HeartbeatInterval >= opts.LeaseDuration {
		opts.HeartbeatInterval = opts.LeaseDuration / 2
		if opts.HeartbeatInterval <= 0 {
			opts.HeartbeatInterval = 10 * time.Second
		}
	}

	stageMap := make(map[StageName]Stage, len(stages))
	for _, stage := range stages {
		if stage.Interval <= 0 {
			stage.Interval = 10 * time.Minute
		}
		if stage.Concurrency <= 0 {
			stage.Concurrency = 1
		}
		stageMap[stage.Name] = stage
	}

	return &Supervisor{
		log:               log,
		stages:            stageMap,
		tracker:           opts.Tracker,
		owner:             opts.Owner,
		leaseDuration:     opts.LeaseDuration,
		heartbeatInterval: opts.HeartbeatInterval,
	}
}

func (s *Supervisor) Run(ctx context.Context) error {
	return s.RunSelected(
		ctx,
		StageScrapeLatest,
		StageScrapeBackfill,
		StageAssemble,
		StageRelease,
		StageInspectDiscovery,
		StageInspectPAR2,
		StageInspectNFO,
		StageInspectArchive,
		StageInspectPassword,
		StageInspectMedia,
		StageEnrichPreDB,
		StageEnrichTMDB,
		StageMaintenance,
	)
}

func (s *Supervisor) RunSelected(ctx context.Context, names ...StageName) error {
	stages, err := s.selectStages(names...)
	if err != nil {
		return err
	}
	if len(stages) == 0 {
		return fmt.Errorf("no enabled supervisor stages selected")
	}

	if s.log != nil {
		s.log.Info("index supervisor started stages=%s", joinStageNames(stages))
	}

	var wg sync.WaitGroup
	for _, stage := range stages {
		stage := stage
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.runStageLoop(ctx, stage)
		}()
	}

	<-ctx.Done()
	wg.Wait()

	if s.log != nil {
		s.log.Info("index supervisor stopped")
	}

	return nil
}

func (s *Supervisor) RunStageOnce(ctx context.Context, name StageName) error {
	stage, err := s.stage(name)
	if err != nil {
		return err
	}
	return s.executeStage(ctx, stage, "manual")
}

func (s *Supervisor) RunStagesOnce(ctx context.Context, names ...StageName) error {
	for _, name := range names {
		stage, err := s.stage(name)
		if err != nil {
			return err
		}
		if err := s.executeStage(ctx, stage, "manual"); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func (s *Supervisor) runStageLoop(ctx context.Context, stage Stage) {
	s.runStage(ctx, stage, "scheduled")

	ticker := time.NewTicker(stage.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runStage(ctx, stage, "scheduled")
		}
	}
}

func (s *Supervisor) runStage(ctx context.Context, stage Stage, trigger string) {
	if err := s.executeStage(ctx, stage, trigger); err != nil && ctx.Err() == nil && s.log != nil {
		s.log.Error("index stage failed stage=%s trigger=%s err=%v", stage.Name, trigger, err)
	}
}

func (s *Supervisor) executeStage(ctx context.Context, stage Stage, trigger string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if s.tracker == nil {
		return stage.Runner.Run(ctx)
	}

	claim, err := s.tracker.ClaimIndexerStage(ctx, pgindex.IndexerStageClaimRequest{
		StageName:     string(stage.Name),
		TriggerKind:   trigger,
		Owner:         s.owner,
		Enabled:       stage.Enabled,
		Interval:      stage.Interval,
		BatchSize:     stage.BatchSize,
		Concurrency:   stage.Concurrency,
		Backoff:       stage.Backoff,
		LeaseDuration: s.leaseDuration,
	})
	if err != nil {
		return err
	}
	if claim == nil || !claim.Claimed || claim.Run == nil {
		if s.log != nil && claim != nil && claim.Reason != "" {
			s.log.Debug("index stage skipped stage=%s trigger=%s reason=%s", stage.Name, trigger, claim.Reason)
		}
		return nil
	}

	heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
	defer cancelHeartbeat()

	heartbeatErrCh := make(chan error, 1)
	go s.heartbeatStageRun(heartbeatCtx, claim.Run.ID, heartbeatErrCh)

	runErr := stage.Runner.Run(ctx)

	cancelHeartbeat()
	heartbeatErr := <-heartbeatErrCh

	controlCtx, cancelControl := s.controlContext()
	defer cancelControl()

	if runErr != nil {
		finishErr := s.tracker.FailIndexerStageRun(controlCtx, pgindex.IndexerStageFinishRequest{
			RunID:     claim.Run.ID,
			Owner:     s.owner,
			ErrorText: runErr.Error(),
		})
		if finishErr != nil {
			return fmt.Errorf("%v (also failed to mark stage run failed: %w)", runErr, finishErr)
		}
		return runErr
	}

	if heartbeatErr != nil && s.log != nil {
		s.log.Warn("index stage heartbeat had errors stage=%s run_id=%d err=%v", stage.Name, claim.Run.ID, heartbeatErr)
	}

	if err := s.tracker.CompleteIndexerStageRun(controlCtx, pgindex.IndexerStageFinishRequest{
		RunID: claim.Run.ID,
		Owner: s.owner,
	}); err != nil {
		return err
	}

	return nil
}

func (s *Supervisor) heartbeatStageRun(ctx context.Context, runID int64, errCh chan<- error) {
	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()

	var heartbeatErr error
	for {
		select {
		case <-ctx.Done():
			errCh <- heartbeatErr
			return
		case <-ticker.C:
			controlCtx, cancel := s.controlContext()
			err := s.tracker.HeartbeatIndexerStageRun(controlCtx, runID, s.owner, s.leaseDuration)
			cancel()
			if err != nil {
				heartbeatErr = err
				errCh <- heartbeatErr
				return
			}
		}
	}
}

func (s *Supervisor) controlContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func (s *Supervisor) selectStages(names ...StageName) ([]Stage, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("at least one stage must be selected")
	}

	out := make([]Stage, 0, len(names))
	for _, name := range names {
		stage, ok, err := s.selectableStage(name)
		if err != nil {
			return nil, err
		}
		if !ok {
			if s.log != nil {
				s.log.Info("index supervisor skipping disabled stage=%s", name)
			}
			continue
		}
		out = append(out, stage)
	}
	return out, nil
}

func (s *Supervisor) stage(name StageName) (Stage, error) {
	stage, ok, err := s.selectableStage(name)
	if err != nil {
		return Stage{}, err
	}
	if !ok {
		return Stage{}, fmt.Errorf("stage %q is disabled", name)
	}
	return stage, nil
}

func (s *Supervisor) selectableStage(name StageName) (Stage, bool, error) {
	if s == nil {
		return Stage{}, false, fmt.Errorf("supervisor is not configured")
	}

	stage, ok := s.stages[name]
	if !ok {
		return Stage{}, false, fmt.Errorf("stage %q is not configured", name)
	}
	if !stage.Enabled {
		return Stage{}, false, nil
	}
	if stage.Runner == nil {
		return Stage{}, false, fmt.Errorf("stage %q has no runner", name)
	}
	return stage, true, nil
}

func joinStageNames(stages []Stage) string {
	names := make([]string, 0, len(stages))
	for _, stage := range stages {
		names = append(names, string(stage.Name))
	}
	return strings.Join(names, ",")
}
