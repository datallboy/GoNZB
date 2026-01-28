package indexer

import (
	"context"
	"fmt"
	"sync"
)

type store interface {
	SaveReleases(ctx context.Context, results []SearchResult) error
	GetRelease(ctx context.Context, id string) (SearchResult, error)
	GetNZB(id string) ([]byte, error)
	PutNZB(id string, data []byte) error
	Exists(id string) bool
}

// BaseManager is the concrete implementation of the Manager interface.
type BaseManager struct {
	mu       sync.RWMutex
	indexers map[string]Indexer
	store    store
}

// NewManager initializes a new manager with a physical file store.
func NewManager(s store) *BaseManager {
	return &BaseManager{
		indexers: make(map[string]Indexer),
		store:    s,
	}
}

// AddIndexer registers a new indexer (usually a CachedIndexer) to the manager.
func (m *BaseManager) AddIndexer(idx Indexer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indexers[idx.Name()] = idx
}

// SearchAll queries all indexers loaded by the manager
func (m *BaseManager) SearchAll(ctx context.Context, query string) ([]SearchResult, error) {
	var wg sync.WaitGroup
	resultsChan := make(chan []SearchResult, len(m.indexers))

	m.mu.RLock()
	for _, idx := range m.indexers {
		wg.Add(1)
		go func(i Indexer) {
			defer wg.Done()
			res, err := i.Search(ctx, query)
			if err == nil {
				resultsChan <- res
			}
		}(idx)
	}
	m.mu.RUnlock()

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var allResults []SearchResult
	for res := range resultsChan {
		allResults = append(allResults, res...)
	}

	// Persist release records in database
	if len(allResults) > 0 {
		_ = m.store.SaveReleases(ctx, allResults)
	}

	return allResults, nil
}

// FetchNZB handles retrieving nzb from cache or downloading from an indexer
func (m *BaseManager) FetchNZB(ctx context.Context, id string) ([]byte, error) {
	// Check the file store
	if m.store.Exists(id) {
		return m.store.GetNZB(id)
	}

	// Not on disk? Looks where to download it from in SQLite
	res, err := m.store.GetRelease(ctx, id)
	if err != nil {
		return nil, err
	}

	// Find the indexer that provided this result.
	m.mu.RLock()
	idx, ok := m.indexers[res.Source]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("indexer not available: %s", res.Source)
	}

	// This calls either the raw DownloadNZB or the Cached one!
	data, err := idx.DownloadNZB(ctx, res)
	if err != nil {
		return nil, err
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Cache file to disk
	_ = m.store.PutNZB(id, data)

	return data, nil
}

// GetResultByID looks up a search result in the manager's memory
func (m *BaseManager) GetResultByID(ctx context.Context, id string) (SearchResult, error) {
	return m.store.GetRelease(ctx, id)
}
