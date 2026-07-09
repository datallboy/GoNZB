package trust

import (
	"testing"
	"time"
)

func TestValidateTrustAttestation(t *testing.T) {
	item := Attestation{
		SubjectNodeID: "node_subject",
		PoolID:        "pool.private.movies",
		ScoreDelta:    10,
		Reason:        "valid_contributions",
		Evidence:      []byte(`{"event_ids":["evt_1"]}`),
		ExpiresAt:     "2026-08-07T12:00:00Z",
	}
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	if err := Validate(item, now); err != nil {
		t.Fatalf("expected valid attestation: %v", err)
	}
	if got := NormalizedDelta(item.ScoreDelta); got != 0.1 {
		t.Fatalf("normalized delta = %v, want 0.1", got)
	}

	item.ScoreDelta = 101
	if err := Validate(item, now); err == nil {
		t.Fatalf("expected out-of-range score_delta to fail")
	}

	item.ScoreDelta = 10
	item.ExpiresAt = "2026-07-01T12:00:00Z"
	if err := Validate(item, now); err == nil {
		t.Fatalf("expected expired attestation to fail")
	}
}
