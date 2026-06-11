package wiring

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/app"
)

type nntpSnapshotStore interface {
	UpsertNNTPSnapshot(ctx context.Context, publisherID, moduleName, scope string, payload []byte) error
}

type runtimeSnapshotPublisher struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func (p *runtimeSnapshotPublisher) Close() error {
	if p == nil {
		return nil
	}
	p.once.Do(func() {
		if p.cancel != nil {
			p.cancel()
		}
		if p.done != nil {
			<-p.done
		}
	})
	return nil
}

func startIndexerNNTPSnapshotPublisher(parent context.Context, log interface {
	Warn(format string, v ...interface{})
}, store nntpSnapshotStore, publisherID string, statsFn func() app.NNTPRuntimeStats) io.Closer {
	if store == nil || statsFn == nil || publisherID == "" {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		publish := func() {
			stats := statsFn()
			payload, err := json.Marshal(stats)
			if err != nil {
				if log != nil {
					log.Warn("failed to marshal nntp runtime snapshot: %v", err)
				}
				return
			}
			if err := store.UpsertNNTPSnapshot(ctx, publisherID, "indexer", stats.Scope, payload); err != nil && ctx.Err() == nil && log != nil {
				log.Warn("failed to persist nntp runtime snapshot: %v", err)
			}
		}

		publish()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				publish()
			}
		}
	}()
	return &runtimeSnapshotPublisher{cancel: cancel, done: done}
}

func runtimeNNTPStatsFunc(service app.UsenetIndexerService) func() app.NNTPRuntimeStats {
	if service == nil {
		return nil
	}
	return func() app.NNTPRuntimeStats {
		stats, err := service.NNTPStats(context.Background())
		if err != nil || stats == nil {
			return app.NNTPRuntimeStats{Scope: "indexer"}
		}
		return *stats
	}
}

type closerGroup []io.Closer

func (g closerGroup) Close() error {
	var firstErr error
	for _, closer := range g {
		if closer == nil {
			continue
		}
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func multiCloser(closers ...io.Closer) io.Closer {
	group := make(closerGroup, 0, len(closers))
	for _, closer := range closers {
		if closer != nil {
			group = append(group, closer)
		}
	}
	if len(group) == 0 {
		return nil
	}
	if len(group) == 1 {
		return group[0]
	}
	return group
}
