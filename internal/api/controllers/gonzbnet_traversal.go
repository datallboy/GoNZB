package controllers

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/infra/config"
)

func gonzbnetPeerHTTPClient(appCtx *app.Context, timeout time.Duration) *http.Client {
	transport := http.RoundTripper(http.DefaultTransport)
	if appCtx != nil && appCtx.GoNZBNetPeerTransport != nil {
		transport = appCtx.GoNZBNetPeerTransport
	}
	return &http.Client{Transport: transport, Timeout: gonzbnetTraversalAwareTimeout(appCtx, timeout)}
}

func gonzbnetTraversalAwareTimeout(appCtx *app.Context, requestTimeout time.Duration) time.Duration {
	if appCtx == nil || appCtx.Config == nil || !appCtx.Config.GoNZBNet.Traversal.Enabled {
		return requestTimeout
	}
	connectTimeout := time.Duration(appCtx.Config.GoNZBNet.Traversal.ConnectTimeoutSeconds) * time.Second
	if connectTimeout <= 0 {
		return requestTimeout
	}
	return connectTimeout + requestTimeout
}

func configuredTraversalLocators(ctx context.Context, cfg config.GoNZBNetConfig, nodeIdentity *identity.Identity) []string {
	if !cfg.Traversal.Enabled || nodeIdentity == nil {
		return nil
	}
	nodeID, err := nodeIdentity.NodeID(ctx)
	if err != nil {
		return nil
	}
	basePath := strings.TrimRight(strings.TrimSpace(cfg.HTTPBasePath), "/")
	if basePath == "" {
		basePath = "/gonzbnet/v1"
	}
	out := make([]string, 0, len(cfg.Traversal.Coordinators))
	for _, coordinator := range cfg.Traversal.Coordinators {
		parsed, err := url.Parse(strings.TrimSpace(coordinator.URL))
		if err != nil || parsed.Host == "" {
			continue
		}
		out = append(out, "gonzb+ice://"+nodeID+"@"+strings.ToLower(parsed.Host)+basePath)
	}
	return out
}
