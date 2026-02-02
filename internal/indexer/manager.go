package indexer

import (
	"context"
	"io"
	"net/http"
	"sync"

	"github.com/labstack/echo/v5"
)

type store interface {
	SaveReleases(ctx context.Context, results []SearchResult) error
	GetRelease(ctx context.Context, id string) (SearchResult, error)
	GetNZBReader(id string) (io.ReadCloser, error)
	CreateNZBWriter(id string) (io.WriteCloser, error)
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
func (m *BaseManager) FetchNZB(ctx context.Context, id string, c *echo.Context) error {
	// Check the file store
	if m.store.Exists(id) {
		data, err := m.store.GetNZBReader(id)
		if err != nil {
			return err
		}
		defer data.Close()
		return c.Stream(http.StatusOK, "application/x-nzb", data)
	}

	// Not on disk? Looks where to download it from in SQLite
	res, err := m.store.GetRelease(ctx, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get release from cache")
	}

	// Find the indexer that provided this result.
	m.mu.RLock()
	idx, ok := m.indexers[res.Source]
	m.mu.RUnlock()

	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "Indexer not found")
	}

	// This calls either the raw DownloadNZB or the Cached one!
	data, err := idx.DownloadNZB(ctx, res)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Indexer download failed")
	}
	defer data.Close()

	// Cache file to disk
	cacheFile, err := m.store.CreateNZBWriter(id)
	if err != nil {
		// If we can't create cache, just stream it from the web anyway
		return c.Stream(http.StatusOK, "application/x-nzb", data)
	}

	// Read from 'body', write to 'cacheFile', then pass through to 'w' (the HTTP response)
	tee := io.TeeReader(data, cacheFile)

	err = c.Stream(http.StatusOK, "application/x-nzb", tee)

	cacheFile.Close()
	return err
}

// GetResultByID looks up a search result in the manager's memory
func (m *BaseManager) GetResultByID(ctx context.Context, id string) (SearchResult, error) {
	return m.store.GetRelease(ctx, id)
}
