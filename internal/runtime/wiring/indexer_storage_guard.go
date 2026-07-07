package wiring

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type databaseStorageStatusReader interface {
	DatabaseStorageStatus(ctx context.Context) (*pgindex.DatabaseStorageStatus, error)
}

type storageGuardSettingsReader interface {
	GetRuntimeSettings(ctx context.Context, base ...*config.Config) (*app.RuntimeSettings, error)
}

type cachedStorageGuard struct {
	repo            databaseStorageStatusReader
	settingsStore   storageGuardSettingsReader
	bootstrapConfig *config.Config
	config          pgindex.DatabaseStorageGuardConfig
	lastCheck       time.Time
	lastResult      supervisor.StageGateDecision
	mu              sync.Mutex
}

func newIndexerStageResourceGuard(repo databaseStorageStatusReader, storageCfg pgindex.DatabaseStorageGuardConfig, memoryCfg IndexerMemoryGuardConfig, settingsStore storageGuardSettingsReader, bootstrapConfig *config.Config) supervisor.StageGateFunc {
	var gates []supervisor.StageGateFunc
	if repo != nil {
		guard := &cachedStorageGuard{
			repo:            repo,
			settingsStore:   settingsStore,
			bootstrapConfig: bootstrapConfig,
			config:          storageCfg,
		}
		gates = append(gates, guard.allowStage)
	}
	if memoryCfg.Enabled {
		guard := &cachedMemoryGuard{config: memoryCfg}
		gates = append(gates, guard.allowStage)
	}
	if len(gates) == 0 {
		return nil
	}
	return func(ctx context.Context, stage supervisor.Stage, trigger string) (supervisor.StageGateDecision, error) {
		for _, gate := range gates {
			decision, err := gate(ctx, stage, trigger)
			if err != nil {
				return supervisor.StageGateDecision{}, err
			}
			if !decision.Allowed {
				return decision, nil
			}
		}
		return supervisor.StageGateDecision{Allowed: true}, nil
	}
}

func (g *cachedStorageGuard) allowStage(ctx context.Context, stage supervisor.Stage, trigger string) (supervisor.StageGateDecision, error) {
	if shouldAlwaysAllowOnLowDBSpace(stage.Name) {
		return supervisor.StageGateDecision{Allowed: true}, nil
	}

	g.mu.Lock()
	if time.Since(g.lastCheck) < 15*time.Second {
		result := g.lastResult
		g.mu.Unlock()
		return result, nil
	}
	g.mu.Unlock()

	status, err := g.repo.DatabaseStorageStatus(ctx)
	if err != nil {
		return supervisor.StageGateDecision{}, fmt.Errorf("check postgres storage status: %w", err)
	}
	cfg, err := g.currentConfig(ctx)
	if err != nil {
		return supervisor.StageGateDecision{}, err
	}
	if dataDirectory := strings.TrimSpace(cfg.DataDirectory); dataDirectory != "" {
		if err := pgindex.PopulateDatabaseStorageFilesystemStatus(status, dataDirectory); err != nil {
			return supervisor.StageGateDecision{}, fmt.Errorf("check configured postgres storage path: %w", err)
		}
	}
	evaluation := pgindex.EvaluateDatabaseStorageGuard(*status, cfg)
	decision := supervisor.StageGateDecision{
		Allowed: !evaluation.Blocked,
		Reason:  evaluation.Reason,
	}

	g.mu.Lock()
	g.lastCheck = time.Now()
	g.lastResult = decision
	g.mu.Unlock()
	return decision, nil
}

func (g *cachedStorageGuard) currentConfig(ctx context.Context) (pgindex.DatabaseStorageGuardConfig, error) {
	cfg := g.config
	if g.settingsStore == nil {
		return cfg, nil
	}
	runtime, err := g.settingsStore.GetRuntimeSettings(ctx, g.bootstrapConfig)
	if err != nil {
		return cfg, fmt.Errorf("load runtime storage guard settings: %w", err)
	}
	if runtime == nil || runtime.Indexing == nil {
		return cfg, nil
	}
	storage := runtime.Indexing.StorageGuard
	cfg.Enabled = storage.Enabled
	cfg.DataDirectory = storage.DataDirectory
	cfg.MinFreeBytes = storage.MinFreeBytes
	cfg.MinFreePercent = storage.MinFreePercent
	return cfg, nil
}

func shouldAlwaysAllowOnLowDBSpace(name supervisor.StageName) bool {
	switch name {
	case supervisor.StageReleaseArchiveNZB,
		supervisor.StageReleasePurgeArchivedSources,
		supervisor.StageMaintenanceReleaseSourcePurge:
		return true
	default:
		return false
	}
}
