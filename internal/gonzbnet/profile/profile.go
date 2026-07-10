package profile

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
)

const SpecVersion = "gonzbnet/1.0"

type Identity interface {
	NodeID(context.Context) (string, error)
	PublicKey(context.Context) (ed25519.PublicKey, error)
}

type WellKnown struct {
	SpecVersion string `json:"spec_version"`
	NodeURL     string `json:"node_url"`
	BaseURL     string `json:"base_url"`
	PublicKey   string `json:"public_key"`
	NodeID      string `json:"node_id"`
}

type NodeProfile struct {
	SchemaVersion     string             `json:"schema_version"`
	Type              string             `json:"type"`
	NodeID            string             `json:"node_id"`
	Alias             string             `json:"alias,omitempty"`
	Software          string             `json:"software"`
	SoftwareVersion   string             `json:"software_version"`
	Protocols         []string           `json:"protocols"`
	PublicKey         string             `json:"public_key"`
	Endpoints         Endpoints          `json:"endpoints"`
	Capabilities      Capabilities       `json:"capabilities"`
	ModuleStatus      ModuleStatus       `json:"module_status"`
	ScannerCapacity   *ScannerCapacity   `json:"scanner_capacity,omitempty"`
	ValidatorCapacity *ValidatorCapacity `json:"validator_capacity,omitempty"`
	ProviderScope     ProviderScope      `json:"provider_scope"`
	Limits            Limits             `json:"limits"`
	Policy            Policy             `json:"policy"`
	CreatedAt         string             `json:"created_at"`
	UpdatedAt         string             `json:"updated_at"`
}

type Endpoints struct {
	Base      string `json:"base"`
	Inbox     string `json:"inbox"`
	Outbox    string `json:"outbox"`
	Events    string `json:"events"`
	Manifests string `json:"manifests"`
	WS        string `json:"ws,omitempty"`
}

type Capabilities struct {
	ReleaseCards        bool `json:"release_cards"`
	ResolutionManifests bool `json:"resolution_manifests"`
	HealthAttestations  bool `json:"health_attestations"`
	TrustPools          bool `json:"trust_pools"`
	PoolWitness         bool `json:"pool_witness"`
	WebSocketGossip     bool `json:"websocket_gossip"`
	PeerExchange        bool `json:"peer_exchange"`
	RelayMode           bool `json:"relay_mode"`
	Consumer            bool `json:"consumer"`
	Scanner             bool `json:"scanner"`
	Indexer             bool `json:"indexer"`
	ManifestBuilder     bool `json:"manifest_builder"`
	ManifestCache       bool `json:"manifest_cache"`
	Validator           bool `json:"validator"`
	HealthChecker       bool `json:"health_checker"`
	Coverage            bool `json:"coverage"`
	Scheduler           bool `json:"scheduler"`
}

type ModuleStatus struct {
	Scanner         string `json:"scanner"`
	IndexProjection string `json:"index_projection"`
	ManifestBuilder string `json:"manifest_builder"`
	ManifestCache   string `json:"manifest_cache"`
	Validator       string `json:"validator"`
	HealthChecker   string `json:"health_checker"`
	Coverage        string `json:"coverage"`
	Scheduler       string `json:"scheduler"`
	Relay           string `json:"relay"`
}

type ScannerCapacity struct {
	MaxGroups                int   `json:"max_groups"`
	MaxArticlesPerHour       int64 `json:"max_articles_per_hour"`
	MaxHeaderBytesPerHour    int64 `json:"max_header_bytes_per_hour,omitempty"`
	SupportsArticleRangeScan bool  `json:"supports_article_range_scan"`
	SupportsTimeWindowScan   bool  `json:"supports_time_window_scan"`
}

type ValidatorCapacity struct {
	MaxManifestsPerHour          int      `json:"max_manifests_per_hour"`
	ValidationTiers              []string `json:"validation_tiers"`
	SupportsYEncSampleValidation bool     `json:"supports_yenc_sample_validation"`
	SupportsPAR2Validation       bool     `json:"supports_par2_validation"`
}

type ProviderScope struct {
	ProviderDisclosure string `json:"provider_disclosure"`
	BackboneHash       string `json:"backbone_hash,omitempty"`
	ArticleNumberScope string `json:"article_number_scope"`
}

type Limits struct {
	MaxEventBytes         int `json:"max_event_bytes"`
	MaxManifestBytes      int `json:"max_manifest_bytes"`
	MaxBatchEvents        int `json:"max_batch_events"`
	RateLimitEventsPerMin int `json:"rate_limit_events_per_minute"`
}

type Policy struct {
	PrivateNetwork                  bool `json:"private_network"`
	LiveQuerySupported              bool `json:"live_query_supported"`
	ManifestFetchRequiresMembership bool `json:"manifest_fetch_requires_pool_membership"`
}

type Caps struct {
	SpecVersions     []string `json:"spec_versions"`
	EventTypes       []string `json:"event_types"`
	Encodings        []string `json:"encodings"`
	Compressions     []string `json:"compressions"`
	Transports       []string `json:"transports"`
	MaxEventBytes    int      `json:"max_event_bytes"`
	MaxManifestBytes int      `json:"max_manifest_bytes"`
}

type Config struct {
	Alias                         string
	AdvertiseURL                  string
	HTTPBasePath                  string
	PrivateNetwork                bool
	LiveQueryEnabled              bool
	WebSocketGossip               bool
	PeerExchange                  bool
	RelayMode                     bool
	Consumer                      bool
	Scanner                       bool
	Indexer                       bool
	IndexProjection               bool
	PublishReleaseCards           bool
	PublishHealthAttestations     bool
	ManifestBuilder               bool
	ManifestCache                 bool
	Validator                     bool
	HealthChecker                 bool
	Coverage                      bool
	Scheduler                     bool
	ScannerMaxGroups              int
	ScannerMaxArticlesPerHour     int64
	ValidationMaxManifestsPerHour int
	ValidationTiers               []string
	ValidationAllowSamplePayload  bool
	ValidationAllowPAR2           bool
	ProviderDisclosure            string
	ProviderBackboneHash          string
	MaxEventBytes                 int
	MaxManifestBytes              int
	MaxBatchEvents                int
	RateLimitEventsPerMin         int
}

func WellKnownFor(ctx context.Context, identity Identity, baseURL string) (WellKnown, error) {
	nodeID, publicKey, err := identityParts(ctx, identity)
	if err != nil {
		return WellKnown{}, err
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return WellKnown{
		SpecVersion: SpecVersion,
		NodeURL:     baseURL + "/node",
		BaseURL:     baseURL,
		PublicKey:   canonical.Base64URL(publicKey),
		NodeID:      nodeID,
	}, nil
}

func NodeProfileFor(ctx context.Context, identity Identity, cfg Config, now time.Time) (NodeProfile, error) {
	nodeID, publicKey, err := identityParts(ctx, identity)
	if err != nil {
		return NodeProfile{}, err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.AdvertiseURL), "/")
	if baseURL == "" {
		return NodeProfile{}, fmt.Errorf("advertise url is required")
	}
	if cfg.MaxEventBytes <= 0 {
		cfg.MaxEventBytes = 262144
	}
	if cfg.MaxManifestBytes <= 0 {
		cfg.MaxManifestBytes = 10485760
	}
	if cfg.MaxBatchEvents <= 0 {
		cfg.MaxBatchEvents = 100
	}
	if cfg.RateLimitEventsPerMin <= 0 {
		cfg.RateLimitEventsPerMin = 120
	}
	if cfg.ScannerMaxGroups < 0 {
		cfg.ScannerMaxGroups = 0
	}
	if cfg.ScannerMaxArticlesPerHour < 0 {
		cfg.ScannerMaxArticlesPerHour = 0
	}
	if cfg.ValidationMaxManifestsPerHour < 0 {
		cfg.ValidationMaxManifestsPerHour = 0
	}
	providerDisclosure := strings.TrimSpace(cfg.ProviderDisclosure)
	if providerDisclosure == "" {
		providerDisclosure = "hash_only"
	}
	var scannerCapacity *ScannerCapacity
	if cfg.Scanner {
		scannerCapacity = &ScannerCapacity{
			MaxGroups:                cfg.ScannerMaxGroups,
			MaxArticlesPerHour:       cfg.ScannerMaxArticlesPerHour,
			SupportsArticleRangeScan: true,
			SupportsTimeWindowScan:   true,
		}
	}
	var validatorCapacity *ValidatorCapacity
	if cfg.Validator {
		validatorCapacity = &ValidatorCapacity{
			MaxManifestsPerHour:          cfg.ValidationMaxManifestsPerHour,
			ValidationTiers:              []string{"metadata"},
			SupportsYEncSampleValidation: false,
			SupportsPAR2Validation:       false,
		}
	}
	webSocketEndpoint := ""
	if cfg.WebSocketGossip {
		webSocketEndpoint = baseURL + "/ws"
	}
	ts := now.UTC().Format(time.RFC3339)
	return NodeProfile{
		SchemaVersion:   "1.0",
		Type:            "NodeProfile",
		NodeID:          nodeID,
		Alias:           strings.TrimSpace(cfg.Alias),
		Software:        "GoNZB",
		SoftwareVersion: "0.8.0",
		Protocols:       []string{SpecVersion},
		PublicKey:       canonical.Base64URL(publicKey),
		Endpoints: Endpoints{
			Base:      baseURL,
			Inbox:     baseURL + "/inbox",
			Outbox:    baseURL + "/outbox",
			Events:    baseURL + "/events/{event_id}",
			Manifests: baseURL + "/manifests/{manifest_id}",
			WS:        webSocketEndpoint,
		},
		Capabilities: Capabilities{
			ReleaseCards:        cfg.Scanner && cfg.PublishReleaseCards,
			ResolutionManifests: cfg.ManifestCache,
			HealthAttestations:  cfg.HealthChecker && cfg.PublishHealthAttestations,
			TrustPools:          true,
			PoolWitness:         false,
			WebSocketGossip:     cfg.WebSocketGossip,
			PeerExchange:        cfg.PeerExchange,
			RelayMode:           cfg.RelayMode,
			Consumer:            cfg.Consumer,
			Scanner:             cfg.Scanner,
			Indexer:             cfg.Indexer,
			ManifestBuilder:     false,
			ManifestCache:       cfg.ManifestCache,
			Validator:           cfg.Validator,
			HealthChecker:       cfg.HealthChecker,
			Coverage:            cfg.Coverage,
			Scheduler:           cfg.Scheduler,
		},
		ModuleStatus: ModuleStatus{
			Scanner:         enabledStatus(cfg.Scanner),
			IndexProjection: enabledStatus(cfg.IndexProjection),
			ManifestBuilder: enabledStatus(cfg.ManifestBuilder),
			ManifestCache:   enabledStatus(cfg.ManifestCache),
			Validator:       enabledStatus(cfg.Validator),
			HealthChecker:   enabledStatus(cfg.HealthChecker),
			Coverage:        enabledStatus(cfg.Coverage),
			Scheduler:       enabledStatus(cfg.Scheduler),
			Relay:           enabledStatus(cfg.RelayMode),
		},
		ScannerCapacity:   scannerCapacity,
		ValidatorCapacity: validatorCapacity,
		ProviderScope: ProviderScope{
			ProviderDisclosure: providerDisclosure,
			BackboneHash:       strings.TrimSpace(cfg.ProviderBackboneHash),
			ArticleNumberScope: "provider_local",
		},
		Limits: Limits{
			MaxEventBytes:         cfg.MaxEventBytes,
			MaxManifestBytes:      cfg.MaxManifestBytes,
			MaxBatchEvents:        cfg.MaxBatchEvents,
			RateLimitEventsPerMin: cfg.RateLimitEventsPerMin,
		},
		Policy: Policy{
			PrivateNetwork:                  cfg.PrivateNetwork,
			LiveQuerySupported:              false,
			ManifestFetchRequiresMembership: true,
		},
		CreatedAt: ts,
		UpdatedAt: ts,
	}, nil
}

func enabledStatus(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func ProviderBackboneHash(parts []string) string {
	values := make([]string, 0, len(parts))
	for _, raw := range parts {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return ""
	}
	sort.Strings(values)
	sum := sha256.Sum256([]byte(strings.Join(values, "\n")))
	return hex.EncodeToString(sum[:])
}

func CapsFor(maxEventBytes, maxManifestBytes int) Caps {
	if maxEventBytes <= 0 {
		maxEventBytes = 262144
	}
	if maxManifestBytes <= 0 {
		maxManifestBytes = 10485760
	}
	return Caps{
		SpecVersions: []string{SpecVersion},
		EventTypes: []string{
			"ReleaseCard",
			"ResolutionManifest",
			"HealthAttestation",
			"TrustAttestation",
			"ValidatorCapacity",
			"ArticleAvailabilityAttestation",
			"ChecksumAttestation",
			"ManifestAvailability",
			"ScannerCapacity",
			"ScannerHeartbeat",
			"GroupObservation",
			"CoveragePlan",
			"CoverageAssignment",
			"RangeClaim",
			"TimeWindowClaim",
			"CoverageCheckpoint",
			"RangeComplete",
			"RangeFailed",
			"PoolGenesis",
			"PoolJoinRequest",
			"PoolMemberApproved",
			"PoolMemberRevoked",
			"PoolCheckpoint",
			"Tombstone",
		},
		Encodings:        []string{"jcs-json"},
		Compressions:     []string{"none"},
		Transports:       []string{"https"},
		MaxEventBytes:    maxEventBytes,
		MaxManifestBytes: maxManifestBytes,
	}
}

func identityParts(ctx context.Context, identity Identity) (string, ed25519.PublicKey, error) {
	if identity == nil {
		return "", nil, fmt.Errorf("identity is required")
	}
	nodeID, err := identity.NodeID(ctx)
	if err != nil {
		return "", nil, err
	}
	publicKey, err := identity.PublicKey(ctx)
	if err != nil {
		return "", nil, err
	}
	return nodeID, publicKey, nil
}
