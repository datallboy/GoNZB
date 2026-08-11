package wiring

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	connectice "github.com/datallboy/gonzb-connect/ice"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/peertransport"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func configureGoNZBNetPeerTransport(appCtx *app.Context) error {
	if appCtx == nil || appCtx.Config == nil {
		return fmt.Errorf("app context is required")
	}
	direct := observedFederationDirectTransport{base: http.DefaultTransport, basePath: appCtx.Config.GoNZBNet.HTTPBasePath, store: appCtx.PGIndexStore}
	dispatch := peertransport.Transport{Direct: direct}
	appCtx.GoNZBNetPeerTransport = dispatch
	cfg := appCtx.Config.GoNZBNet
	if !appCtx.Config.Modules.GoNZBNet.Enabled || !cfg.Traversal.Enabled {
		return nil
	}
	nodeIdentity, err := identity.LoadOrCreateWithPassword(cfg.KeysDir, cfg.KeyPassword)
	if err != nil {
		return err
	}
	coordinators := make([]connectice.CoordinatorConfig, 0, len(cfg.Traversal.Coordinators))
	for _, item := range cfg.Traversal.Coordinators {
		credential, err := readCoordinatorCredential(item.CredentialFile, item.CredentialEnv)
		if err != nil {
			return fmt.Errorf("read coordinator credential for %s: %w", item.URL, err)
		}
		coordinators = append(coordinators, connectice.CoordinatorConfig{
			URL: item.URL, Credential: credential, AllowedICEServerHosts: append([]string(nil), item.AllowedICEServerHosts...),
		})
	}
	manager, err := connectice.New(nodeIdentity, connectice.Config{
		Coordinators:       coordinators,
		AllowTURN:          cfg.Traversal.AllowTURN,
		GatherTimeout:      time.Duration(cfg.Traversal.GatherTimeoutSeconds) * time.Second,
		ConnectTimeout:     time.Duration(cfg.Traversal.ConnectTimeoutSeconds) * time.Second,
		IdleTimeout:        time.Duration(cfg.Traversal.IdleTimeoutMinutes) * time.Minute,
		MaxPeerConnections: cfg.Traversal.MaxPeerConnections,
		AllowedBasePath:    cfg.HTTPBasePath,
		MaxBodyBytes:       traversalMaxBodyBytes(cfg),
		VerifyPeerKey: func(ctx context.Context, nodeID string, publicKey ed25519.PublicKey) error {
			store, ok := appCtx.PGIndexStore.(interface {
				GetFederationNodeTransportPublicKey(context.Context, string) (ed25519.PublicKey, error)
			})
			if !ok {
				return fmt.Errorf("federation identity store is unavailable")
			}
			expected, err := store.GetFederationNodeTransportPublicKey(ctx, nodeID)
			if errors.Is(err, pgindex.ErrUnknownFederationNode) {
				return nil
			} // First contact is bound by node ID derived from the signed key.
			if err != nil {
				return fmt.Errorf("verify persistent traversal peer identity: %w", err)
			}
			if !expected.Equal(publicKey) {
				return fmt.Errorf("traversal signaling key does not match the persistent peer identity")
			}
			return nil
		},
		Observe: func(observation connectice.Observation) {
			if observation.PeerNodeID == "" || appCtx.PGIndexStore == nil {
				return
			}
			store, ok := appCtx.PGIndexStore.(interface {
				RecordFederationNodeEndpointResult(context.Context, string, string, bool, string, string, int64, int64, int64, int64, string) error
			})
			if !ok {
				return
			}
			parsed, err := url.Parse(observation.Coordinator)
			if err != nil {
				return
			}
			basePath := strings.TrimRight(strings.TrimSpace(cfg.HTTPBasePath), "/")
			if basePath == "" {
				basePath = "/gonzbnet/v1"
			}
			locator := "gonzb+ice://" + observation.PeerNodeID + "@" + strings.ToLower(parsed.Host) + basePath
			_ = store.RecordFederationNodeEndpointResult(context.Background(), observation.PeerNodeID, locator, observation.Failure == "", observation.PathType, observation.ICEState, observation.RTTMS, observation.Reconnects, observation.BytesSent, observation.BytesReceived, observation.Failure)
		},
	})
	if err != nil {
		return err
	}
	appCtx.GoNZBNetTraversal = manager
	appCtx.GoNZBNetPeerTransport = peertransport.Transport{Direct: direct, Traversal: manager}
	return nil
}

func traversalMaxBodyBytes(cfg config.GoNZBNetConfig) int64 {
	eventBytes := int64(cfg.MaxEventBytes)
	if eventBytes <= 0 {
		eventBytes = 256 << 10
	}
	batchSize := int64(max(cfg.MaxBatchEvents, cfg.GossipBatchSize, cfg.PushSyncBatchSize))
	if batchSize <= 0 {
		batchSize = 100
	}
	const hardLimit = int64(128 << 20)
	if eventBytes > hardLimit || batchSize > (hardLimit-(64<<10))/eventBytes {
		return hardLimit
	}
	limit := eventBytes*batchSize + 64<<10
	if manifestBytes := int64(cfg.MaxManifestBytes); manifestBytes > limit {
		limit = manifestBytes
	}
	if limit > hardLimit {
		return hardLimit
	}
	return limit
}

func configuredCoordinatorHosts(items []config.GoNZBNetCoordinatorConfig) []string {
	hosts := make([]string, 0, len(items))
	for _, item := range items {
		parsed, err := url.Parse(strings.TrimSpace(item.URL))
		if err == nil && parsed.Host != "" {
			hosts = append(hosts, strings.ToLower(parsed.Host))
		}
	}
	return hosts
}

type observedFederationDirectTransport struct {
	base     http.RoundTripper
	basePath string
	store    any
}

func (t observedFederationDirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, fmt.Errorf("federation request URL is required")
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	started := time.Now()
	response, err := base.RoundTrip(request)
	if request.URL.Scheme != "https" && request.URL.Scheme != "http" {
		return response, err
	}
	path := strings.TrimRight(strings.TrimSpace(t.basePath), "/")
	if path == "" {
		path = "/gonzbnet/v1"
	}
	locator := (&url.URL{Scheme: request.URL.Scheme, Host: request.URL.Host, Path: path}).String()
	bytesSent := max(request.ContentLength, 0)
	bytesReceived := int64(0)
	if response != nil {
		bytesReceived = max(response.ContentLength, 0)
	}
	errText := ""
	if err != nil {
		errText = "direct_transport_failed"
	}
	rttMS := time.Since(started).Milliseconds()
	if store, ok := t.store.(interface {
		RecordFederationEndpointResultByLocator(context.Context, string, bool, int64, int64, int64, string) error
	}); ok {
		go func() {
			_ = store.RecordFederationEndpointResultByLocator(context.Background(), locator, err == nil, rttMS, bytesSent, bytesReceived, errText)
		}()
	}
	return response, err
}

func readCoordinatorCredential(fileName, envName string) (string, error) {
	if fileName = strings.TrimSpace(fileName); fileName != "" {
		payload, err := os.ReadFile(fileName)
		if err != nil {
			return "", err
		}
		if value := strings.TrimSpace(string(payload)); value != "" {
			return value, nil
		}
		return "", fmt.Errorf("credential file is empty")
	}
	if envName = strings.TrimSpace(envName); envName != "" {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return value, nil
		}
		return "", fmt.Errorf("credential environment variable %s is empty", envName)
	}
	return "", fmt.Errorf("credential source is required")
}

func peerHTTPClient(appCtx *app.Context, timeout time.Duration) *http.Client {
	transport := http.RoundTripper(http.DefaultTransport)
	if appCtx != nil && appCtx.GoNZBNetPeerTransport != nil {
		transport = appCtx.GoNZBNetPeerTransport
	}
	return &http.Client{Transport: transport, Timeout: traversalAwareHTTPTimeout(appCtx, timeout)}
}

func traversalAwareHTTPTimeout(appCtx *app.Context, requestTimeout time.Duration) time.Duration {
	if appCtx == nil || appCtx.Config == nil || !appCtx.Config.GoNZBNet.Traversal.Enabled {
		return requestTimeout
	}
	connectTimeout := time.Duration(appCtx.Config.GoNZBNet.Traversal.ConnectTimeoutSeconds) * time.Second
	if connectTimeout <= 0 {
		return requestTimeout
	}
	return connectTimeout + requestTimeout
}

func BindGoNZBNetTraversalHandler(appCtx *app.Context, handler http.Handler) {
	if appCtx != nil && appCtx.GoNZBNetTraversal != nil {
		appCtx.GoNZBNetTraversal.SetHandler(handler)
	}
}
