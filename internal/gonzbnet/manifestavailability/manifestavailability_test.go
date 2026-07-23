package manifestavailability

import (
	"testing"
	"time"
)

func TestValidateAndHashManifestAvailability(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	item := Attestation{
		SchemaVersion:       "1.0",
		Type:                Type,
		ReleaseID:           "rel_1",
		ManifestID:          "man_1",
		SourceNodeID:        "node_1",
		PoolID:              "pool.local",
		Available:           true,
		FetchPolicy:         FetchPolicyTrustedPeers,
		CompressedSizeBytes: 81234,
		UpdatedAt:           now.Format(time.RFC3339),
	}
	if err := Validate(item, now, 2*time.Minute); err != nil {
		t.Fatalf("validate manifest availability: %v", err)
	}
	first, err := HashBody(item)
	if err != nil {
		t.Fatalf("hash manifest availability: %v", err)
	}
	second, err := HashBody(item)
	if err != nil {
		t.Fatalf("hash manifest availability again: %v", err)
	}
	if first == "" || first != second {
		t.Fatalf("hash should be deterministic: %q %q", first, second)
	}
	item.FetchPolicy = "bad"
	if err := Validate(item, now, 2*time.Minute); err == nil {
		t.Fatalf("expected invalid fetch policy to fail")
	}
}
