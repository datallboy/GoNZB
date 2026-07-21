package controllers

import (
	"testing"

	"github.com/datallboy/gonzb/internal/app"
)

func TestHasAnySettingsPatchFieldAcceptsNNTPPoolOnlyPatch(t *testing.T) {
	patch := &settingsPatch{
		NNTPPool: &app.NNTPPoolRuntimeSettings{
			DemandWindowSeconds: 31,
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

func TestHasAnySettingsPatchFieldAcceptsScopedServerPatch(t *testing.T) {
	downloaderServers := []app.ServerRuntimeSettings{}
	indexerServers := []app.ServerRuntimeSettings{}

	for name, patch := range map[string]*settingsPatch{
		"downloader": {DownloaderServers: &downloaderServers},
		"indexer":    {IndexerServers: &indexerServers},
	} {
		if !hasAnySettingsPatchField(patch) {
			t.Fatalf("expected %s server patch to be treated as non-empty", name)
		}
	}
}
