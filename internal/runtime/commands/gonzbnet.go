package commands

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	gonzbnetsync "github.com/datallboy/gonzb/internal/gonzbnet/sync"
	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type goNZBNetStatus struct {
	Enabled         bool   `json:"enabled"`
	NodeID          string `json:"node_id"`
	Visibility      string `json:"visibility"`
	AdvertiseURL    string `json:"advertise_url"`
	Pools           int    `json:"pools"`
	Peers           int    `json:"peers"`
	Scanner         bool   `json:"scanner"`
	IndexProjection bool   `json:"index_projection"`
	Validator       bool   `json:"validator"`
	HealthChecker   bool   `json:"health_checker"`
	Relay           bool   `json:"relay"`
	Consumer        bool   `json:"consumer"`
}

func (r *Runner) ExecuteGoNZBNetStatus() {
	appCtx, store, nodeIdentity, ctx, cleanup := r.setupGoNZBNetCommand()
	defer cleanup()

	nodeID, err := nodeIdentity.NodeID(ctx)
	if err != nil {
		appCtx.Logger.Fatal("read gonzbnet node identity: %v", err)
	}
	pools, err := store.ListTrustPools(ctx)
	if err != nil {
		appCtx.Logger.Fatal("list gonzbnet pools: %v", err)
	}
	peers, err := store.ListEnabledFederationPeers(ctx)
	if err != nil {
		appCtx.Logger.Fatal("list gonzbnet peers: %v", err)
	}
	cfg := appCtx.Config.GoNZBNet
	writeGoNZBNetJSON(appCtx, goNZBNetStatus{
		Enabled:         appCtx.Config.Modules.GoNZBNet.Enabled,
		NodeID:          nodeID,
		Visibility:      cfg.Visibility,
		AdvertiseURL:    cfg.AdvertiseURL,
		Pools:           len(pools),
		Peers:           len(peers),
		Scanner:         cfg.ScannerEnabled,
		IndexProjection: cfg.IndexProjectionEnabled,
		Validator:       cfg.ValidatorEnabled,
		HealthChecker:   cfg.HealthCheckerEnabled,
		Relay:           cfg.RelayEnabled,
		Consumer:        cfg.ConsumerEnabled,
	})
}

func (r *Runner) ExecuteGoNZBNetPools() {
	appCtx, store, nodeIdentity, ctx, cleanup := r.setupGoNZBNetCommand()
	defer cleanup()

	nodeID, err := nodeIdentity.NodeID(ctx)
	if err != nil {
		appCtx.Logger.Fatal("read gonzbnet node identity: %v", err)
	}
	type poolStatus struct {
		Pool       pgindex.TrustPoolRecord   `json:"pool"`
		Membership *pgindex.PoolMemberRecord `json:"local_membership,omitempty"`
	}
	pools, err := store.ListTrustPools(ctx)
	if err != nil {
		appCtx.Logger.Fatal("list gonzbnet pools: %v", err)
	}
	out := make([]poolStatus, 0, len(pools))
	for _, pool := range pools {
		members, err := store.ListPoolMembers(ctx, pool.PoolID)
		if err != nil {
			appCtx.Logger.Fatal("list members for pool %s: %v", pool.PoolID, err)
		}
		item := poolStatus{Pool: pool}
		for index := range members {
			if members[index].NodeID == nodeID {
				member := members[index]
				item.Membership = &member
				break
			}
		}
		out = append(out, item)
	}
	writeGoNZBNetJSON(appCtx, out)
}

func (r *Runner) ExecuteGoNZBNetPeers() {
	appCtx, store, _, ctx, cleanup := r.setupGoNZBNetCommand()
	defer cleanup()

	peers, err := store.ListEnabledFederationPeers(ctx)
	if err != nil {
		appCtx.Logger.Fatal("list gonzbnet peers: %v", err)
	}
	writeGoNZBNetJSON(appCtx, peers)
}

func (r *Runner) ExecuteGoNZBNetSync(mode string, limit int) {
	appCtx, store, nodeIdentity, ctx, cleanup := r.setupGoNZBNetCommand()
	defer cleanup()

	cfg := appCtx.Config.GoNZBNet
	service := gonzbnetsync.NewWithOptions(nodeIdentity, store, appCtx.Logger, gonzbnetsync.Options{
		AllowInsecurePeerHTTP: cfg.AllowInsecurePeerHTTP,
		EventTimeTolerance:    time.Duration(cfg.TimeToleranceSeconds) * time.Second,
		MaxEventAge:           time.Duration(cfg.MaxEventAgeHours) * time.Hour,
	})
	if err := service.UpsertManualPeers(ctx, cfg.ManualPeers); err != nil {
		appCtx.Logger.Fatal("register configured gonzbnet peers: %v", err)
	}

	var (
		result gonzbnetsync.Result
		err    error
	)
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "pull":
		result, err = service.SyncOnce(ctx)
	case "push":
		result, err = service.PushOnce(ctx, limit)
	default:
		appCtx.Logger.Fatal("unsupported gonzbnet sync mode %q; use pull or push", mode)
	}
	if err != nil {
		appCtx.Logger.Fatal("gonzbnet %s sync failed: %v", mode, err)
	}
	writeGoNZBNetJSON(appCtx, result)
}

func (r *Runner) ExecuteGoNZBNetNNTPCheck(group, messageID string) {
	cfg, appLogger := r.loadRuntimeConfig()
	appCtx, err := app.NewContext(cfg, appLogger)
	if err != nil {
		appLogger.Fatal("initialize nntp check: %v", err)
	}
	manager, err := nntp.NewManager(appCtx)
	if err != nil {
		appLogger.Fatal("initialize nntp providers: %v", err)
	}
	defer func() {
		_ = manager.Close()
		_ = appLogger.Close()
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	stats, err := manager.GroupStats(ctx, group)
	if err != nil {
		appLogger.Fatal("read nntp group %s: %v", group, err)
	}
	headers, err := manager.XOver(ctx, group, stats.Low, stats.High)
	if err != nil {
		appLogger.Fatal("read nntp overview %s: %v", group, err)
	}
	body, err := manager.FetchBodyPrefix(ctx, messageID, []string{group}, 4096)
	if err != nil {
		appLogger.Fatal("read nntp body %s: %v", messageID, err)
	}
	writeGoNZBNetJSON(appCtx, map[string]any{
		"group":         stats.Group,
		"low":           stats.Low,
		"high":          stats.High,
		"count":         stats.Count,
		"overview_rows": len(headers),
		"message_id":    messageID,
		"body_bytes":    len(body),
	})
}

func (r *Runner) setupGoNZBNetCommand() (*app.Context, *pgindex.Store, *identity.Identity, context.Context, func()) {
	appCtx := r.setupApp(context.Background())
	if !appCtx.Config.Modules.GoNZBNet.Enabled {
		appCtx.Logger.Fatal("gonzbnet module is disabled")
	}
	store, ok := appCtx.PGIndexStore.(*pgindex.Store)
	if !ok || store == nil {
		appCtx.Logger.Fatal("gonzbnet requires the PostgreSQL index store")
	}
	nodeIdentity, err := identity.LoadOrCreateWithPassword(appCtx.Config.GoNZBNet.KeysDir, appCtx.Config.GoNZBNet.KeyPassword)
	if err != nil {
		appCtx.Logger.Fatal("load gonzbnet node identity: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return appCtx, store, nodeIdentity, ctx, func() {
		stop()
		appCtx.Close()
	}
}

func writeGoNZBNetJSON(appCtx *app.Context, value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		appCtx.Logger.Fatal("write gonzbnet command output: %v", err)
	}
}
