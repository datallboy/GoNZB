package indexer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

type store interface {
	UpsertReleases(ctx context.Context, results []*domain.Release) error
	GetRelease(ctx context.Context, id string) (*domain.Release, error)
	SearchReleases(ctx context.Context, query string) ([]*domain.Release, error)
	GetNZBReader(id string) (io.ReadCloser, error)
	SaveNZBAtomically(id string, data []byte) error
	Exists(id string) bool
}

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

// BaseManager is the concrete implementation of the Manager interface.
type BaseManager struct {
	mu       sync.RWMutex
	indexers map[string]Indexer
	store    store
	logger   logger
}

// NewManager initializes a new manager with a physical file store.
func NewManager(s store, l logger) *BaseManager {
	return &BaseManager{
		indexers: make(map[string]Indexer),
		store:    s,
		logger:   l,
	}
}

// AddIndexer registers a new indexer (usually a CachedIndexer) to the manager.
func (m *BaseManager) AddIndexer(idx Indexer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indexers[idx.Name()] = idx
}

// SearchAll queries all indexers loaded by the manager
func (m *BaseManager) SearchAll(ctx context.Context, query string) ([]*domain.Release, error) {
	var wg sync.WaitGroup
	resultsChan := make(chan []*domain.Release, len(m.indexers))

	m.mu.RLock()
	for _, idx := range m.indexers {
		wg.Add(1)
		go func(i Indexer) {
			defer wg.Done()

			// Create a per-indexer timeout context
			searchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			res, err := i.Search(searchCtx, query)
			if err != nil {
				m.logger.Error("Indexer %s error: %v", i.Name(), err)
				return
			}

			for _, r := range res {
				if r.ID == "" {
					r.ID = domain.GenerateCompositeID(r.Source, r.GUID)
				}
			}
			resultsChan <- res
		}(idx)
	}
	m.mu.RUnlock()

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var allResults []*domain.Release
	for res := range resultsChan {
		allResults = append(allResults, res...)
	}

	// Persist release records in database
	if len(allResults) > 0 {
		_ = m.store.UpsertReleases(ctx, allResults)
	}

	return allResults, nil
}

// GetNZB handles retrieving nzb from cache or downloading from an indexer.
// Returns io.ReaderCloser so it can returned as a HTTP response or parsed for download
func (m *BaseManager) GetNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error) {
	// Check the file store
	if m.store.Exists(res.ID) {
		return m.store.GetNZBReader(res.ID)
	}

	// Find the indexer that provided this result.
	m.mu.RLock()
	idx, ok := m.indexers[res.Source]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("indexer %s not found", res.Source)
	}

	// This calls either the raw DownloadNZB or the local store indexer.
	data, err := idx.DownloadNZB(ctx, res)
	if err != nil {
		return nil, fmt.Errorf("failed to download from inedxer: %w", err)
	}
	defer data.Close()

	// Read once so we can both cache atomically and still return data on
	// cache-write failures without re-downloading from upstream.
	payload, err := io.ReadAll(data)
	if err != nil {
		return nil, fmt.Errorf("failed reading nzb payload: %w", err)
	}

	if err := m.store.SaveNZBAtomically(res.ID, payload); err != nil {
		m.logger.Warn("Failed to persist cached NZB for %s: %v", res.ID, err)
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	return m.store.GetNZBReader(res.ID)
}

// GetResultByID looks up a search result in the manager's memory
func (m *BaseManager) GetResultByID(ctx context.Context, id string) (*domain.Release, error) {
	return m.store.GetRelease(ctx, id)
}
