package moderation

import (
	"testing"
	"time"
)

func TestValidateTombstoneAcceptsReleaseReject(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	item := Tombstone{
		SchemaVersion: "1.0",
		Type:          Type,
		TargetType:    TargetRelease,
		TargetID:      "rel_1",
		PoolID:        "pool.local",
		Reason:        "malformed_manifest",
		Severity:      SeverityReject,
		EffectiveAt:   now.Format(time.RFC3339),
	}
	if err := Validate(item, now, time.Minute); err != nil {
		t.Fatalf("expected valid tombstone: %v", err)
	}
	hash1, err := HashBody(item)
	if err != nil {
		t.Fatalf("hash tombstone: %v", err)
	}
	hash2, err := HashBody(item)
	if err != nil {
		t.Fatalf("hash tombstone again: %v", err)
	}
	if hash1 != hash2 {
		t.Fatalf("expected deterministic hash")
	}
}

func TestValidateTombstoneRejectsInvalidValues(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	item := Tombstone{
		SchemaVersion: "1.0",
		Type:          Type,
		TargetType:    "file",
		TargetID:      "rel_1",
		Reason:        "bad",
		Severity:      SeverityReject,
		EffectiveAt:   now.Format(time.RFC3339),
	}
	if err := Validate(item, now, time.Minute); err == nil {
		t.Fatalf("expected invalid target type to fail")
	}
	item.TargetType = TargetRelease
	item.Severity = "banish"
	if err := Validate(item, now, time.Minute); err == nil {
		t.Fatalf("expected invalid severity to fail")
	}
	item.Severity = SeverityReject
	item.EffectiveAt = now.Add(10 * time.Minute).Format(time.RFC3339)
	if err := Validate(item, now, time.Minute); err == nil {
		t.Fatalf("expected future effective_at to fail")
	}
}
