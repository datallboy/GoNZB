package capability

import "testing"

func TestRequiredForEvent(t *testing.T) {
	if !HasAny([]string{Scanner}, RequiredForEvent("ReleaseCard")...) {
		t.Fatalf("expected scanner to satisfy ReleaseCard")
	}
	if HasAny([]string{Consumer}, RequiredForEvent("ReleaseCard")...) {
		t.Fatalf("expected consumer not to satisfy ReleaseCard")
	}
	if !HasAny([]string{ManifestCache}, RequiredForEvent("ResolutionManifest")...) {
		t.Fatalf("expected manifest_cache to satisfy ResolutionManifest")
	}
}

func TestNormalizeDeduplicates(t *testing.T) {
	got := Normalize([]string{" scanner ", "scanner", "", "validator"})
	if len(got) != 2 || got[0] != Scanner || got[1] != Validator {
		t.Fatalf("unexpected normalized capabilities: %#v", got)
	}
}
