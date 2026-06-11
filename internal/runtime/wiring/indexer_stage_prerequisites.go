package wiring

import (
	"context"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
)

func newIndexerPrerequisiteGate(appCtx *app.Context) supervisor.StageGateFunc {
	if appCtx == nil || appCtx.SettingsStore == nil {
		return nil
	}
	return func(ctx context.Context, stage supervisor.Stage, trigger string) (supervisor.StageGateDecision, error) {
		_ = trigger
		runtime, err := appCtx.SettingsStore.GetRuntimeSettings(ctx, appCtx.BootstrapConfig)
		if err != nil {
			return supervisor.StageGateDecision{}, err
		}
		switch stage.Name {
		case supervisor.StageScrapeLatest, supervisor.StageScrapeBackfill:
			if len(app.IndexerNNTPServers(runtime)) == 0 {
				return supervisor.StageGateDecision{Allowed: false, Reason: "configure at least one NNTP server"}, nil
			}
			if runtime == nil || runtime.Indexing == nil || len(app.EffectiveNewsgroupNames(runtime.Indexing)) == 0 {
				return supervisor.StageGateDecision{Allowed: false, Reason: "configure at least one scrape group"}, nil
			}
		case supervisor.StageAssembleLaneA,
			supervisor.StageAssembleLaneB,
			supervisor.StageRecoverYEnc,
			supervisor.StageInspectDiscovery,
			supervisor.StageInspectPAR2,
			supervisor.StageInspectNFO,
			supervisor.StageInspectArchive,
			supervisor.StageInspectPassword,
			supervisor.StageInspectMedia:
			if len(app.IndexerNNTPServers(runtime)) == 0 {
				return supervisor.StageGateDecision{Allowed: false, Reason: "configure at least one NNTP server"}, nil
			}
		}
		return supervisor.StageGateDecision{Allowed: true}, nil
	}
}

func chainStageGates(gates ...supervisor.StageGateFunc) supervisor.StageGateFunc {
	filtered := make([]supervisor.StageGateFunc, 0, len(gates))
	for _, gate := range gates {
		if gate != nil {
			filtered = append(filtered, gate)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return func(ctx context.Context, stage supervisor.Stage, trigger string) (supervisor.StageGateDecision, error) {
		for _, gate := range filtered {
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
