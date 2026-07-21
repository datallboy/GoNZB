package activity

import (
	"strings"
	"time"
)

type Configuration struct {
	ModuleEnabled               bool
	StoreReady                  bool
	ConsumerEnabled             bool
	ScannerEnabled              bool
	IndexProjectionEnabled      bool
	ManifestCacheEnabled        bool
	ValidatorEnabled            bool
	HealthCheckerEnabled        bool
	CoverageEnabled             bool
	SchedulerEnabled            bool
	PublishReleaseCardsEnabled  bool
	HealthAttestationsEnabled   bool
	PullSyncEnabled             bool
	PushSyncEnabled             bool
	WebSocketGossipEnabled      bool
	RelayEnabled                bool
	PeerExchangeEnabled         bool
	CoverageMode                string
	PublishReleaseCardsInterval time.Duration
	HealthAttestationsInterval  time.Duration
	ValidationInterval          time.Duration
	PullSyncInterval            time.Duration
	PushSyncInterval            time.Duration
	GossipInterval              time.Duration
	CoverageSchedulerInterval   time.Duration
}

func Definitions(cfg Configuration) []Definition {
	definition := func(key, job, label, description string, mode ExecutionMode, interval time.Duration, configured bool) Definition {
		reason := ""
		if configured && !cfg.StoreReady {
			reason = "PostgreSQL federation store is unavailable"
		}
		return Definition{
			Key: key, Job: job, Label: label, Description: description,
			ExecutionMode: mode, Interval: interval,
			Configured: cfg.ModuleEnabled && configured,
			Eligible:   cfg.ModuleEnabled && cfg.StoreReady,
			Reason:     reason,
		}
	}
	return []Definition{
		definition(ComponentAdmissionPoller, JobConnection, "Pool admissions", "Refreshes pending pool admission state.", Scheduled, 30*time.Second, true),
		definition(ComponentReleasePublisher, JobContribute, "Release publisher", "Publishes eligible release cards and manifests.", Scheduled, cfg.PublishReleaseCardsInterval, cfg.ScannerEnabled && cfg.PublishReleaseCardsEnabled),
		definition(ComponentHealthPublisher, JobVerify, "Health publisher", "Publishes signed release-health evidence.", Scheduled, cfg.HealthAttestationsInterval, cfg.HealthCheckerEnabled && cfg.HealthAttestationsEnabled),
		definition(ComponentValidator, JobVerify, "Article validator", "Checks manifests and article reachability.", Scheduled, cfg.ValidationInterval, cfg.ValidatorEnabled),
		definition(ComponentPullSync, JobConnection, "Pull synchronization", "Retrieves signed pool events from peers.", Scheduled, cfg.PullSyncInterval, cfg.PullSyncEnabled),
		definition(ComponentPushSync, JobConnection, "Push synchronization", "Delivers signed pool events to peers.", Scheduled, cfg.PushSyncInterval, cfg.PushSyncEnabled),
		definition(ComponentGossip, JobConnection, "WebSocket gossip", "Exchanges recent signed events with peers.", Scheduled, cfg.GossipInterval, cfg.WebSocketGossipEnabled),
		definition(ComponentCoverageScheduler, JobCoordinate, "Coverage scheduler", "Reassigns stale coordinated scanning work.", Scheduled, cfg.CoverageSchedulerInterval, cfg.CoverageEnabled && cfg.SchedulerEnabled && strings.EqualFold(cfg.CoverageMode, "automatic")),
		definition(ComponentScanner, JobCoordinate, "Scanner coordinator", "Claims and reports coordinated NNTP scanning work.", EventDriven, 0, cfg.ScannerEnabled && cfg.CoverageEnabled),
		definition(ComponentIndexProjection, JobConsume, "Index projection", "Projects accepted pool releases into local search.", EventDriven, 0, cfg.IndexProjectionEnabled),
		definition(ComponentManifestResolver, JobConsume, "Manifest resolver", "Fetches and verifies a manifest when a local grab needs it.", OnDemand, 0, cfg.ConsumerEnabled),
		definition(ComponentManifestCache, JobConsume, "Manifest cache", "Stores and serves verified manifests under local policy.", OnDemand, 0, cfg.ManifestCacheEnabled),
		definition(ComponentRelay, JobConnection, "Event relay", "Forwards eligible signed events without changing their authorship.", EventDriven, 0, cfg.RelayEnabled),
		definition(ComponentPeerExchange, JobConnection, "Peer exchange", "Learns authenticated peer endpoints during gossip.", EventDriven, 0, cfg.PeerExchangeEnabled),
	}
}
