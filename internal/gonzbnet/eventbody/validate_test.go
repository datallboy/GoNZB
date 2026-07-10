package eventbody

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/gonzbnet/validation"
)

func TestValidateAcceptsReleaseCardAndRejectsPrivateField(t *testing.T) {
	node := testIdentity(t)
	card, err := releasecard.MapLocalRelease(testRelease())
	if err != nil {
		t.Fatalf("map release: %v", err)
	}
	event := testEvent(t, node, pools.EventTypeReleaseCard, releasecard.BodySchema, card)
	if err := Validate(event, testNow(), 2*time.Minute); err != nil {
		t.Fatalf("validate release card event: %v", err)
	}

	privateBody := map[string]any{
		"schema_version": "1.0",
		"type":           pools.EventTypeReleaseCard,
		"user_id":        "local-user-must-not-federate",
	}
	privateEvent := testEvent(t, node, pools.EventTypeReleaseCard, releasecard.BodySchema, privateBody)
	if err := Validate(privateEvent, testNow(), 2*time.Minute); err == nil || !strings.Contains(err.Error(), "private field") {
		t.Fatalf("expected private field rejection, got %v", err)
	}
}

func TestValidateRejectsAuthorBoundNodeMismatch(t *testing.T) {
	node := testIdentity(t)
	body := validation.ValidatorCapacity{
		SchemaVersion:           "1.0",
		Type:                    validation.TypeValidatorCapacity,
		NodeID:                  "node_other",
		PublishedAt:             testNow().Format(time.RFC3339),
		AcceptedManifestSchemas: []string{"gonzbnet.ResolutionManifest/1.0"},
	}
	event := testEvent(t, node, pools.EventTypeValidatorCapacity, validation.ValidatorCapacityBodySchema, body)
	if err := Validate(event, testNow(), 2*time.Minute); err == nil || !strings.Contains(err.Error(), "event author") {
		t.Fatalf("expected author mismatch rejection, got %v", err)
	}
}

func TestValidateRejectsBodySchemaMismatch(t *testing.T) {
	node := testIdentity(t)
	card, err := releasecard.MapLocalRelease(testRelease())
	if err != nil {
		t.Fatalf("map release: %v", err)
	}
	event := testEvent(t, node, pools.EventTypeReleaseCard, "gonzbnet.Other/1.0", card)
	if err := Validate(event, testNow(), 2*time.Minute); err == nil || !strings.Contains(err.Error(), "body_schema") {
		t.Fatalf("expected body schema rejection, got %v", err)
	}
}

func testEvent(t *testing.T, node *identity.Identity, eventType, bodySchema string, body any) *events.SignedEvent {
	t.Helper()
	event, result, err := events.Create(context.Background(), node, events.CreateOptions{
		EventType:  eventType,
		Sequence:   1,
		CreatedAt:  testNow(),
		PoolIDs:    []string{"pool.local"},
		Visibility: "pool",
		BodySchema: bodySchema,
		Body:       body,
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if result == nil || !result.OK {
		t.Fatalf("created event did not verify")
	}
	return event
}

func testIdentity(t *testing.T) *identity.Identity {
	t.Helper()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	return node
}

func testNow() time.Time {
	return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
}

func testRelease() releasecard.LocalRelease {
	posted := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	return releasecard.LocalRelease{
		Title:        "Example.Release.2026.1080p.WEB-DL",
		Category:     "movies",
		CategoryID:   2040,
		SizeBytes:    1000,
		PostedAt:     &posted,
		FileCount:    1,
		Groups:       []string{"alt.binaries.example"},
		Availability: 1,
		Files: []releasecard.LocalFile{{
			Name:         "example.mkv",
			Subject:      "Example.Release.2026.1080p.WEB-DL example.mkv yEnc",
			PostedAt:     &posted,
			SizeBytes:    1000,
			FileIndex:    1,
			ArticleCount: 1,
			TotalParts:   1,
			Segments: []releasecard.LocalSegment{{
				Number:    1,
				Bytes:     1000,
				MessageID: "<seg1@example.invalid>",
			}},
		}},
	}
}
