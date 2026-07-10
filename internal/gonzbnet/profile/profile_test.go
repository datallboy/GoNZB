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
	if profile.Capabilities.HealthAttestations {
		t.Fatalf("validator-only node should not advertise health-attestation publication")
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

func TestNodeProfileDoesNotAdvertiseLiveQuery(t *testing.T) {
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}

	profile, err := NodeProfileFor(context.Background(), node, Config{
		AdvertiseURL:     "https://node.example/gonzbnet/v1",
		Consumer:         true,
		LiveQueryEnabled: true,
	}, time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("node profile: %v", err)
	}
	if profile.Policy.LiveQuerySupported {
		t.Fatalf("live query should not be advertised")
	}
}

func TestNodeProfileAdvertisesCapacityAndModuleStatus(t *testing.T) {
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}

	profile, err := NodeProfileFor(context.Background(), node, Config{
		AdvertiseURL:                  "https://node.example/gonzbnet/v1",
		Scanner:                       true,
		IndexProjection:               false,
		Validator:                     true,
		ManifestBuilder:               true,
		ScannerMaxGroups:              12,
		ScannerMaxArticlesPerHour:     345000,
		ValidationMaxManifestsPerHour: 40,
		ValidationTiers:               []string{"metadata", "article_stat"},
		ValidationAllowSamplePayload:  true,
		ValidationAllowPAR2:           false,
		ProviderDisclosure:            "hash_only",
	}, time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("node profile: %v", err)
	}
	if profile.ModuleStatus.Scanner != "enabled" || profile.ModuleStatus.IndexProjection != "disabled" {
		t.Fatalf("unexpected module status: %+v", profile.ModuleStatus)
	}
	if profile.ScannerCapacity == nil || profile.ScannerCapacity.MaxGroups != 12 || profile.ScannerCapacity.MaxArticlesPerHour != 345000 {
		t.Fatalf("unexpected scanner capacity: %+v", profile.ScannerCapacity)
	}
	if profile.ValidatorCapacity == nil || profile.ValidatorCapacity.MaxManifestsPerHour != 40 {
		t.Fatalf("unexpected validator capacity: %+v", profile.ValidatorCapacity)
	}
	if len(profile.ValidatorCapacity.ValidationTiers) != 1 || profile.ValidatorCapacity.ValidationTiers[0] != "metadata" || profile.ValidatorCapacity.SupportsYEncSampleValidation || profile.ValidatorCapacity.SupportsPAR2Validation {
		t.Fatalf("unexpected validator tiers/support: %+v", profile.ValidatorCapacity)
	}
	if profile.ProviderScope.ProviderDisclosure != "hash_only" || profile.ProviderScope.ArticleNumberScope != "provider_local" {
		t.Fatalf("unexpected provider scope: %+v", profile.ProviderScope)
	}
}

func TestNodeProfileOnlyAdvertisesActiveProductionPaths(t *testing.T) {
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	profile, err := NodeProfileFor(context.Background(), node, Config{
		AdvertiseURL:    "https://node.example/gonzbnet/v1",
		Scanner:         true,
		Indexer:         true,
		ManifestBuilder: true,
		HealthChecker:   true,
	}, time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("node profile: %v", err)
	}
	if profile.Capabilities.ReleaseCards || profile.Capabilities.HealthAttestations || profile.Capabilities.ManifestBuilder {
		t.Fatalf("disabled/unimplemented output paths must not be advertised: %+v", profile.Capabilities)
	}
	if profile.Endpoints.WS != "" {
		t.Fatalf("disabled WebSocket endpoint must be omitted, got %q", profile.Endpoints.WS)
	}
	if profile.ModuleStatus.ManifestBuilder != "enabled" {
		t.Fatalf("configured module status should remain visible: %+v", profile.ModuleStatus)
	}
}

func TestNodeProfileAdvertisesEnabledReleaseAndHealthPublishers(t *testing.T) {
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	profile, err := NodeProfileFor(context.Background(), node, Config{
		AdvertiseURL:              "https://node.example/gonzbnet/v1",
		Scanner:                   true,
		PublishReleaseCards:       true,
		HealthChecker:             true,
		PublishHealthAttestations: true,
		WebSocketGossip:           true,
	}, time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("node profile: %v", err)
	}
	if !profile.Capabilities.ReleaseCards || !profile.Capabilities.HealthAttestations {
		t.Fatalf("enabled output paths should be advertised: %+v", profile.Capabilities)
	}
	if profile.Endpoints.WS == "" {
		t.Fatalf("enabled WebSocket endpoint should be advertised")
	}
}

func TestCapsOnlyAdvertiseImplementedWireFeatures(t *testing.T) {
	caps := CapsFor(1, 2)
	if len(caps.Compressions) != 1 || caps.Compressions[0] != "none" {
		t.Fatalf("unexpected compression advertisement: %+v", caps.Compressions)
	}
	for _, eventType := range caps.EventTypes {
		if eventType == "NodeProfile" {
			t.Fatalf("signed NodeProfile events are not accepted and must not be advertised")
		}
	}
}

func TestProviderBackboneHashIsDeterministicAndNormalized(t *testing.T) {
	first := ProviderBackboneHash([]string{" News.Example:563 ", "backup.example:119"})
	second := ProviderBackboneHash([]string{"backup.example:119", "news.example:563"})
	if first == "" {
		t.Fatal("expected provider backbone hash")
	}
	if first != second {
		t.Fatalf("expected normalized hash, got %q and %q", first, second)
	}
	if first == ProviderBackboneHash([]string{"other.example:563"}) {
		t.Fatal("expected different provider scopes to hash differently")
	}
}
