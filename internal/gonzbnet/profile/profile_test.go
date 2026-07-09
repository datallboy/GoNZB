package profile

import (
	"context"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
)

func TestNodeProfileAdvertisesValidatorOnlyCapabilities(t *testing.T) {
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}

	profile, err := NodeProfileFor(context.Background(), node, Config{
		AdvertiseURL:  "https://node.example/gonzbnet/v1",
		Consumer:      false,
		Scanner:       false,
		Indexer:       false,
		ManifestCache: true,
		Validator:     true,
	}, time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("node profile: %v", err)
	}
	if profile.Capabilities.ReleaseCards {
		t.Fatalf("validator-only node should not advertise release card production")
	}
	if !profile.Capabilities.ResolutionManifests {
		t.Fatalf("manifest cache node should advertise resolution manifest support")
	}
	if !profile.Capabilities.HealthAttestations {
		t.Fatalf("validator node should advertise health attestation support")
	}
	if !profile.Capabilities.Validator || profile.Capabilities.Scanner || profile.Capabilities.Indexer {
		t.Fatalf("unexpected module capabilities: %+v", profile.Capabilities)
	}
}

func TestNodeProfileAdvertisesConsumerOnlyCapabilities(t *testing.T) {
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}

	profile, err := NodeProfileFor(context.Background(), node, Config{
		AdvertiseURL: "https://node.example/gonzbnet/v1",
		Consumer:     true,
	}, time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("node profile: %v", err)
	}
	if !profile.Capabilities.Consumer {
		t.Fatalf("consumer-only node should advertise consumer capability")
	}
	if profile.Capabilities.ReleaseCards || profile.Capabilities.ResolutionManifests || profile.Capabilities.HealthAttestations {
		t.Fatalf("consumer-only node should not advertise contribution capabilities: %+v", profile.Capabilities)
	}
}
