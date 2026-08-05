package publicationstate

import (
	"testing"
	"time"
)

func TestValidateReleasePublicationState(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	item := State{
		SchemaVersion: "1.0", Type: Type, PoolID: "pool.synthetic", ReleaseID: "rel_synthetic",
		ManifestID: "man_synthetic", State: StateWithdrawn, ChangedAt: now.Format(time.RFC3339),
	}
	if err := Validate(item, now, 0); err != nil {
		t.Fatalf("validate publication state: %v", err)
	}
	item.State = "deleted"
	if err := Validate(item, now, 0); err == nil {
		t.Fatal("expected invalid state rejection")
	}
}
