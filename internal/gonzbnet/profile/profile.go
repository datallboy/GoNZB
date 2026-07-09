package profile

import (
	"context"
	"crypto/ed25519"
	"fmt"
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
	SchemaVersion   string       `json:"schema_version"`
	Type            string       `json:"type"`
	NodeID          string       `json:"node_id"`
	Alias           string       `json:"alias,omitempty"`
	Software        string       `json:"software"`
	SoftwareVersion string       `json:"software_version"`
	Protocols       []string     `json:"protocols"`
	PublicKey       string       `json:"public_key"`
	Endpoints       Endpoints    `json:"endpoints"`
	Capabilities    Capabilities `json:"capabilities"`
	Limits          Limits       `json:"limits"`
	Policy          Policy       `json:"policy"`
	CreatedAt       string       `json:"created_at"`
	UpdatedAt       string       `json:"updated_at"`
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
	Alias            string
	AdvertiseURL     string
	HTTPBasePath     string
	PrivateNetwork   bool
	LiveQueryEnabled bool
	WebSocketGossip  bool
	PeerExchange     bool
	RelayMode        bool
	MaxEventBytes    int
	MaxManifestBytes int
	MaxBatchEvents   int
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
			WS:        baseURL + "/ws",
		},
		Capabilities: Capabilities{
			ReleaseCards:        true,
			ResolutionManifests: true,
			HealthAttestations:  true,
			TrustPools:          true,
			PoolWitness:         false,
			WebSocketGossip:     cfg.WebSocketGossip,
			PeerExchange:        cfg.PeerExchange,
			RelayMode:           cfg.RelayMode,
		},
		Limits: Limits{
			MaxEventBytes:         cfg.MaxEventBytes,
			MaxManifestBytes:      cfg.MaxManifestBytes,
			MaxBatchEvents:        cfg.MaxBatchEvents,
			RateLimitEventsPerMin: 120,
		},
		Policy: Policy{
			PrivateNetwork:                  cfg.PrivateNetwork,
			LiveQuerySupported:              cfg.LiveQueryEnabled,
			ManifestFetchRequiresMembership: true,
		},
		CreatedAt: ts,
		UpdatedAt: ts,
	}, nil
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
			"NodeProfile",
			"ReleaseCard",
			"ResolutionManifest",
			"HealthAttestation",
			"PoolGenesis",
			"PoolJoinRequest",
			"PoolMemberApproved",
			"PoolMemberRevoked",
			"TrustAttestation",
			"Tombstone",
			"PoolCheckpoint",
		},
		Encodings:        []string{"jcs-json"},
		Compressions:     []string{"none", "gzip", "zstd"},
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
