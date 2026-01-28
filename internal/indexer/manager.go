package indexer

import (
	"context"
	"fmt"
	"sync"
)

type store interface {
	Get(string) ([]byte, error)
	Put(string, []byte) error
}

// BaseManager is the concrete implementation of the Manager interface.
type BaseManager struct {
	mu       sync.RWMutex
	indexers map[string]Indexer

	// resultCache stores recent search results so we can find the
	// DownloadURL when a user requests an NZB by its ID.
	resultCache map[string]SearchResult

	store store
}

// NewManager initializes a new manager with a physical file store.
func NewManager(s store) *BaseManager {
	return &BaseManager{
		indexers:    make(map[string]Indexer),
		resultCache: make(map[string]SearchResult),
		store:       s,
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

		// Store these results in our "memory" so FetchNZB can find them later
		m.mu.Lock()
		for _, r := range res {
			m.resultCache[r.ID] = r
		}
		m.mu.Unlock()
	}

	return allResults, nil
}

// FetchNZB handles retrieving nzb from cache or downloading from an indexer
func (m *BaseManager) FetchNZB(ctx context.Context, id string) ([]byte, error) {
	// Check the file store
	if data, err := m.store.Get(id); err == nil {
		return data, nil
	}

	// Not in file store, find the metadata to get the URL and Source
	res, err := m.GetResultByID(id)
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
	return idx.DownloadNZB(ctx, id)
}

// GetResultByID looks up a search result in the manager's memory
func (m *BaseManager) GetResultByID(id string) (SearchResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res, ok := m.resultCache[id]
	if !ok {
		return SearchResult{}, fmt.Errorf("result %s not found in recent history", id)
	}
	return res, nil
}
