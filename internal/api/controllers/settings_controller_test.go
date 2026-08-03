package controllers

import (
	"testing"

	"github.com/datallboy/gonzb/internal/app"
)

func TestHasAnySettingsPatchFieldAcceptsNNTPPoolOnlyPatch(t *testing.T) {
	patch := &settingsPatch{
		NNTPPool: &app.NNTPPoolRuntimeSettings{
			IndexerStageTargetPercent: 85,
		},
	}
	if !hasAnySettingsPatchField(patch) {
		t.Fatalf("expected nntp_pool-only patch to be treated as non-empty")
	}
}

func TestHasAnySettingsPatchFieldAcceptsGoNZBNetOnlyPatch(t *testing.T) {
	patch := &settingsPatch{GoNZBNet: &app.GoNZBNetRuntimeSettings{NodeAlias: "node-a"}}
	if !hasAnySettingsPatchField(patch) {
		t.Fatalf("expected gonzbnet-only patch to be treated as non-empty")
	}
}

func TestHasAnySettingsPatchFieldAcceptsIndexerServerPatch(t *testing.T) {
	indexerServers := []app.ServerRuntimeSettings{}
	if !hasAnySettingsPatchField(&settingsPatch{IndexerServers: &indexerServers}) {
		t.Fatal("expected indexer server patch to be treated as non-empty")
	}
}
