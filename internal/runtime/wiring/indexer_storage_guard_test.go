package wiring

import (
	"testing"

	"github.com/datallboy/gonzb/internal/indexing/supervisor"
)

func TestShouldAlwaysAllowOnLowDBSpaceOnlyAllowsArchiveAndPurgeStages(t *testing.T) {
	allowed := []supervisor.StageName{
		supervisor.StageReleaseArchiveNZB,
		supervisor.StageReleasePurgeArchivedSources,
		supervisor.StageMaintenanceReleaseSourcePurge,
	}
	for _, stage := range allowed {
		if !shouldAlwaysAllowOnLowDBSpace(stage) {
			t.Fatalf("expected %s to be allowed during low DB space", stage)
		}
	}

	blocked := []supervisor.StageName{
		supervisor.StageReleaseGenerateNZB,
		supervisor.StageMaintenance,
		supervisor.StageAssemble,
		supervisor.StageRecoverYEnc,
	}
	for _, stage := range blocked {
		if shouldAlwaysAllowOnLowDBSpace(stage) {
			t.Fatalf("expected %s to be blocked by low DB space guard", stage)
		}
	}
}
