package supervisor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type StageName string

const (
	StageScrapeLatest    StageName = "scrape_latest"
	StageScrapeBackfill  StageName = "scrape_backfill"
	StageAssemble        StageName = "assemble"
	StageRelease         StageName = "release"
	StageInspectPAR2     StageName = "inspect_par2"
	StageInspectNFO      StageName = "inspect_nfo"
	StageInspectArchive  StageName = "inspect_archive"
	StageInspectPassword StageName = "inspect_password"
	StageInspectMedia    StageName = "inspect_media"
	StageEnrichPreDB     StageName = "enrich_predb"
	StageEnrichTMDB      StageName = "enrich_tmdb"
)

type Runner interface {
	Run(ctx context.Context) error
}

type RunnerFunc func(ctx context.Context) error

func (fn RunnerFunc) Run(ctx context.Context) error {
	return fn(ctx)
}

type Stage struct {
	Name     StageName
	Interval time.Duration
	Enabled  bool
	Runner   Runner
}

type Supervisor struct {
	log    logger
	stages map[StageName]Stage
}

func New(log logger, stages []Stage) *Supervisor {
	stageMap := make(map[StageName]Stage, len(stages))
	for _, stage := range stages {
		if stage.Interval <= 0 {
			stage.Interval = 10 * time.Minute
		}
		stageMap[stage.Name] = stage
	}

	return &Supervisor{
		log:    log,
		stages: stageMap,
	}
}

func (s *Supervisor) Run(ctx context.Context) error {
	return s.RunSelected(
		ctx,
		StageScrapeLatest,
		StageScrapeBackfill,
		StageAssemble,
		StageRelease,
		StageInspectPAR2,
		StageInspectNFO,
		StageInspectArchive,
		StageInspectPassword,
		StageInspectMedia,
		StageEnrichPreDB,
		StageEnrichTMDB,
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
	return stage.Runner.Run(ctx)
}

func (s *Supervisor) RunStagesOnce(ctx context.Context, names ...StageName) error {
	for _, name := range names {
		if err := s.RunStageOnce(ctx, name); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func (s *Supervisor) runStageLoop(ctx context.Context, stage Stage) {
	s.runStage(ctx, stage, "startup")

	ticker := time.NewTicker(stage.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runStage(ctx, stage, "interval")
		}
	}
}

func (s *Supervisor) runStage(ctx context.Context, stage Stage, trigger string) {
	if err := ctx.Err(); err != nil {
		return
	}

	if err := stage.Runner.Run(ctx); err != nil && ctx.Err() == nil && s.log != nil {
		s.log.Error("index stage failed stage=%s trigger=%s err=%v", stage.Name, trigger, err)
	}
}

func (s *Supervisor) selectStages(names ...StageName) ([]Stage, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("at least one stage must be selected")
	}

	out := make([]Stage, 0, len(names))
	for _, name := range names {
		stage, err := s.stage(name)
		if err != nil {
			return nil, err
		}
		out = append(out, stage)
	}
	return out, nil
}

func (s *Supervisor) stage(name StageName) (Stage, error) {
	if s == nil {
		return Stage{}, fmt.Errorf("supervisor is not configured")
	}

	stage, ok := s.stages[name]
	if !ok {
		return Stage{}, fmt.Errorf("stage %q is not configured", name)
	}
	if !stage.Enabled {
		return Stage{}, fmt.Errorf("stage %q is disabled", name)
	}
	if stage.Runner == nil {
		return Stage{}, fmt.Errorf("stage %q has no runner", name)
	}
	return stage, nil
}

func joinStageNames(stages []Stage) string {
	names := make([]string, 0, len(stages))
	for _, stage := range stages {
		names = append(names, string(stage.Name))
	}
	return strings.Join(names, ",")
}
