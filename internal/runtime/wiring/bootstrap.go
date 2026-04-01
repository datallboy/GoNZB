package wiring

import (
	"context"
	"fmt"
	"io"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
	blobstore "github.com/datallboy/gonzb/internal/store/blob"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	settingsstore "github.com/datallboy/gonzb/internal/store/settings"
	"github.com/datallboy/gonzb/internal/store/sqlitejob"
)

type blobCacheIndexer interface {
	MarkReleaseCached(ctx context.Context, releaseID string, blobSize int64, blobMtimeUnix int64) error
	MarkReleaseCacheMissing(ctx context.Context, releaseID, reason string) error
}

// allows filesystem payload cache without SQLite blob index state.
type noopBlobCacheIndexer struct{}

func (noopBlobCacheIndexer) MarkReleaseCached(context.Context, string, int64, int64) error {
	return nil
}

func (noopBlobCacheIndexer) MarkReleaseCacheMissing(context.Context, string, string) error {
	return nil
}

// BootstrapStores initializes long-lived stores and cache dependencies once.
func BootstrapStores(appCtx *app.Context) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}
	if appCtx.PayloadCacheStore != nil {
		return nil
	}
	if appCtx.BootstrapConfig == nil {
		return fmt.Errorf("bootstrap config is required")
	}

	cfg := appCtx.BootstrapConfig
	modules := cfg.Modules

	needsJobStore := modules.Downloader.Enabled || (modules.Aggregator.Enabled && cfg.Store.SearchPersistenceEnabled)
	needsSettingsStore := modules.Downloader.Enabled || modules.Aggregator.Enabled || modules.UsenetIndexer.Enabled

	closers := make([]io.Closer, 0, 3)
	closeCreated := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			_ = closers[i].Close()
		}
	}

	if needsJobStore {
		jobStore, err := sqlitejob.NewStore(cfg.Store.SQLitePath, cfg.Store.BlobDir)
		if err != nil {
			return fmt.Errorf("failed to initialize sqlite job store: %w", err)
		}
		appCtx.JobStore = jobStore
		appCtx.QueueFileStore = jobStore
		closers = append(closers, jobStore)
	}

	if needsSettingsStore {
		settingsStore, err := settingsstore.NewStore(cfg.Store.SQLitePath)
		if err != nil {
			closeCreated()
			return fmt.Errorf("failed to initialize settings store: %w", err)
		}
		appCtx.SettingsStore = settingsStore
		closers = append(closers, settingsStore)
	}

	payloadStore, err := newPayloadCacheStore(cfg, appCtx.JobStore)
	if err != nil {
		closeCreated()
		return fmt.Errorf("failed to initialize payload cache store: %w", err)
	}
	appCtx.BlobStore = payloadStore
	appCtx.PayloadCacheStore = payloadStore

	if modules.UsenetIndexer.Enabled && cfg.Store.PGDSN != "" {
		pgStore, err := pgindex.NewStore(cfg.Store.PGDSN)
		if err != nil {
			closeCreated()
			return fmt.Errorf("failed to initialize pg index store: %w", err)
		}
		appCtx.PGIndexStore = pgStore
		closers = append(closers, pgStore)
	}

	for _, closer := range closers {
		appCtx.AddCloser(closer)
	}

	return nil
}

func newPayloadCacheStore(cfg *config.Config, jobStore app.JobStore) (app.PayloadCacheStore, error) {
	if cfg.Store.PayloadCacheEnabled {
		cacheIndexer := blobCacheIndexer(noopBlobCacheIndexer{})
		if jobStore != nil {
			if storeIndexer, ok := jobStore.(blobCacheIndexer); ok {
				cacheIndexer = storeIndexer
			}
		}

		return blobstore.NewFSBlobStore(cfg.Store.BlobDir, cacheIndexer)
	}

	return blobstore.NewEphemeralBlobStore(), nil
}
