package controllers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/aggregator/sources/newznab"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/labstack/echo/v5"
)

type SettingsConnectionController struct {
	appCtx *app.Context
}

type settingsConnectionTestRequest struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

func NewSettingsConnectionController(appCtx *app.Context) *SettingsConnectionController {
	return &SettingsConnectionController{appCtx: appCtx}
}

func (ctrl *SettingsConnectionController) Test(c *echo.Context) error {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.SettingsAdmin == nil {
		return jsonError(c, http.StatusServiceUnavailable, "runtime settings are not configured")
	}
	var req settingsConnectionTestRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	req.Kind = strings.ToLower(strings.TrimSpace(req.Kind))
	req.ID = strings.TrimSpace(req.ID)

	ctx, cancel := context.WithTimeout(c.Request().Context(), 30*time.Second)
	defer cancel()
	started := time.Now()
	if err := ctrl.test(ctx, req); err != nil {
		return c.JSON(http.StatusOK, map[string]any{
			"ok":         false,
			"kind":       req.Kind,
			"id":         req.ID,
			"latency_ms": time.Since(started).Milliseconds(),
			"message":    err.Error(),
		})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"ok":         true,
		"kind":       req.Kind,
		"id":         req.ID,
		"latency_ms": time.Since(started).Milliseconds(),
		"message":    "connection successful",
	})
}

func (ctrl *SettingsConnectionController) test(ctx context.Context, req settingsConnectionTestRequest) error {
	switch req.Kind {
	case "postgres":
		if ctrl.appCtx.PGIndexStore == nil {
			return fmt.Errorf("PostgreSQL index store is not configured")
		}
		return ctrl.appCtx.PGIndexStore.Ping(ctx)
	case "nntp":
		runtime, err := ctrl.appCtx.SettingsAdmin.Get(ctx)
		if err != nil {
			return err
		}
		server, ok := findRuntimeServer(runtime, req.ID)
		if !ok {
			return fmt.Errorf("NNTP provider %q was not found", req.ID)
		}
		provider := nntp.NewNNTPProvider(runtimeServerConfig(server))
		defer provider.Close()
		return provider.TestConnection()
	case "newznab":
		runtime, err := ctrl.appCtx.SettingsAdmin.Get(ctx)
		if err != nil {
			return err
		}
		indexer, ok := findRuntimeIndexer(runtime, req.ID)
		if !ok {
			return fmt.Errorf("Newznab source %q was not found", req.ID)
		}
		client := newznab.New(indexer.ID, indexer.BaseURL, indexer.APIPath, indexer.APIKey, indexer.Redirect, newznab.OutboundPolicy{
			AllowPrivateAddresses: indexer.AllowPrivateAddresses,
			AllowedCIDRs:          indexer.AllowedCIDRs,
		})
		return client.TestConnection(ctx)
	default:
		return fmt.Errorf("connection kind must be postgres, nntp, or newznab")
	}
}

func findRuntimeServer(runtime *app.RuntimeSettings, id string) (app.ServerRuntimeSettings, bool) {
	for _, server := range app.RuntimeServersForCompatibility(runtime) {
		if strings.EqualFold(strings.TrimSpace(server.ID), id) {
			return server, true
		}
	}
	return app.ServerRuntimeSettings{}, false
}

func findRuntimeIndexer(runtime *app.RuntimeSettings, id string) (app.IndexerRuntimeSettings, bool) {
	if runtime != nil {
		for _, indexer := range runtime.Indexers {
			if strings.EqualFold(strings.TrimSpace(indexer.ID), id) {
				return indexer, true
			}
		}
	}
	return app.IndexerRuntimeSettings{}, false
}

func runtimeServerConfig(server app.ServerRuntimeSettings) config.ServerConfig {
	return config.ServerConfig{
		ID:                     server.ID,
		Host:                   server.Host,
		Port:                   server.Port,
		Username:               server.Username,
		Password:               server.Password,
		TLS:                    server.TLS,
		MaxConnection:          server.MaxConnection,
		Priority:               server.Priority,
		DialTimeoutSeconds:     server.DialTimeoutSeconds,
		TCPKeepAliveSeconds:    server.TCPKeepAliveSeconds,
		PoolIdleTimeoutSeconds: server.PoolIdleTimeoutSeconds,
		PoolMaxAgeSeconds:      server.PoolMaxAgeSeconds,
		EnablePoolLogging:      false,
		Roles:                  append([]string(nil), server.Roles...),
	}
}
