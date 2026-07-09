package events

import (
	"context"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
)

func TestSignedEventVerifiesAndEventIDIsDeterministic(t *testing.T) {
	ctx := context.Background()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}

	opts := testCreateOptions()
	first, firstValidation, err := Create(ctx, node, opts)
	if err != nil {
		t.Fatalf("create first event: %v", err)
	}
	if !firstValidation.OK {
		t.Fatalf("expected first event to verify: %s", firstValidation.Reason)
	}

	second, secondValidation, err := Create(ctx, node, opts)
	if err != nil {
		t.Fatalf("create second event: %v", err)
	}
	if !secondValidation.OK {
		t.Fatalf("expected second event to verify: %s", secondValidation.Reason)
	}

	if first.EventID == "" {
		t.Fatalf("expected event id")
	}
	if first.EventID != second.EventID {
		t.Fatalf("expected deterministic event id %q, got %q", first.EventID, second.EventID)
	}
	if first.Signature != second.Signature {
		t.Fatalf("expected deterministic ed25519 signature")
	}
	if string(firstValidation.CanonicalEventJSON) != string(secondValidation.CanonicalEventJSON) {
		t.Fatalf("expected deterministic canonical unsigned event")
	}
}

func TestSignedEventBodyTamperFailsVerification(t *testing.T) {
	ctx := context.Background()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}

	event, validation, err := Create(ctx, node, testCreateOptions())
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if !validation.OK {
		t.Fatalf("expected original event to verify: %s", validation.Reason)
	}

	tampered := *event
	tampered.Body = []byte(`{"schema_version":"1.0","title":"changed"}`)
	result, err := Verify(&tampered)
	if err != nil {
		t.Fatalf("verify tampered event: %v", err)
	}
	if result.OK {
		t.Fatalf("expected tampered body to fail verification")
	}
	if result.Reason != "body_hash mismatch" {
		t.Fatalf("expected body_hash mismatch, got %q", result.Reason)
	}
}

func TestSignedEventSignedFieldTamperFailsVerification(t *testing.T) {
	ctx := context.Background()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}

	event, validation, err := Create(ctx, node, testCreateOptions())
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if !validation.OK {
		t.Fatalf("expected original event to verify: %s", validation.Reason)
	}

	tampered := *event
	tampered.EventType = "HealthAttestation"
	result, err := Verify(&tampered)
	if err != nil {
		t.Fatalf("verify tampered event: %v", err)
	}
	if result.OK {
		t.Fatalf("expected signed field tamper to fail verification")
	}
	if result.Reason != "event_id mismatch" {
		t.Fatalf("expected event_id mismatch, got %q", result.Reason)
	}
}

func testCreateOptions() CreateOptions {
	return CreateOptions{
		EventType:  "NodeProfile",
		Sequence:   1,
		CreatedAt:  time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC),
		PoolIDs:    []string{"pool.private.movies"},
		Visibility: "pool",
		BodySchema: "gonzbnet.NodeProfile/1.0",
		Body: map[string]any{
			"schema_version": "1.0",
			"type":           "NodeProfile",
			"alias":          "phase-one-test",
			"capabilities": map[string]any{
				"release_cards":        true,
				"resolution_manifests": false,
			},
		},
	}
}
