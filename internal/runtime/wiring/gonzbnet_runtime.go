package wiring

import (
	"context"
	"fmt"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/publisher"
)

const moduleNameGoNZBNet = "gonzbnet"

type gonzbnetRuntimeModule struct {
	appCtx    *app.Context
	publisher *publisher.Service
	cancel    context.CancelFunc
	running   bool
}

func (m *gonzbnetRuntimeModule) Name() string { return moduleNameGoNZBNet }

func (m *gonzbnetRuntimeModule) Enabled() bool {
	return m.appCtx != nil && m.appCtx.Config != nil && m.appCtx.Config.Modules.GoNZBNet.Enabled
}

func (m *gonzbnetRuntimeModule) Build(context.Context) error {
	m.stop()
	m.publisher = nil
	if !m.Enabled() {
		return nil
	}
	if m.appCtx.PGIndexStore == nil {
		return fmt.Errorf("gonzbnet module requires pgindex store")
	}
	store, ok := m.appCtx.PGIndexStore.(publisher.Store)
	if !ok {
		return fmt.Errorf("pgindex store does not support gonzbnet publishing")
	}
	nodeIdentity, err := identity.LoadOrCreate(m.appCtx.Config.GoNZBNet.KeysDir)
	if err != nil {
		return err
	}
	m.publisher = publisher.New(nodeIdentity, store, m.appCtx.Config.GoNZBNet.LocalPoolID)
	return nil
}

func (m *gonzbnetRuntimeModule) Start(ctx context.Context) error {
	if !m.Enabled() || m.publisher == nil {
		return nil
	}
	if !m.appCtx.Config.GoNZBNet.PublishReleaseCardsEnabled {
		return nil
	}
	if m.running {
		return nil
	}
	childCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.running = true

	interval := time.Duration(m.appCtx.Config.GoNZBNet.PublishReleaseCardsIntervalMin * float64(time.Minute))
	batchSize := m.appCtx.Config.GoNZBNet.PublishReleaseCardsBatchSize
	m.appCtx.Logger.Info("starting gonzbnet release-card publisher interval=%s batch_size=%d", interval, batchSize)
	go func() {
		if err := m.publisher.Run(childCtx, interval, batchSize); err != nil && childCtx.Err() == nil {
			m.appCtx.Logger.Error("gonzbnet release-card publisher failed: %v", err)
		}
	}()
	return nil
}

func (m *gonzbnetRuntimeModule) Reload(ctx context.Context) error {
	wasRunning := m.running
	m.stop()
	if err := m.Build(ctx); err != nil {
		return err
	}
	if wasRunning {
		return m.Start(ctx)
	}
	return nil
}

func (m *gonzbnetRuntimeModule) Close() error {
	m.stop()
	return nil
}

func (m *gonzbnetRuntimeModule) ReadinessChecks(context.Context) []app.RuntimeCheck {
	if !m.Enabled() {
		return nil
	}
	checks := []app.RuntimeCheck{
		runtimeBoolCheck("gonzbnet_pgindex_store", m.appCtx.PGIndexStore != nil, "pgindex store is required"),
		runtimeBoolCheck("gonzbnet_publisher", m.publisher != nil, "gonzbnet publisher is required"),
	}
	return checks
}

func (m *gonzbnetRuntimeModule) stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.running = false
}
