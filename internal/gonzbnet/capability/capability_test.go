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
	if !HasAny([]string{Validator}, RequiredForEvent("ArticleAvailabilityAttestation")...) {
		t.Fatalf("expected validator to satisfy ArticleAvailabilityAttestation")
	}
	if !HasAny([]string{Scanner}, RequiredForEvent("ManifestAvailability")...) {
		t.Fatalf("expected scanner to satisfy ManifestAvailability")
	}
}

func TestNormalizeDeduplicates(t *testing.T) {
	got := Normalize([]string{" scanner ", "scanner", "", "validator"})
	if len(got) != 2 || got[0] != Scanner || got[1] != Validator {
		t.Fatalf("unexpected normalized capabilities: %#v", got)
	}
}
