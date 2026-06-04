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
