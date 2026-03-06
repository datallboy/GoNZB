package aggregator

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

type Manager struct {
	mu                       sync.RWMutex
	sources                  map[string]catalogSource
	store                    store
	logger                   logger
	cacheEnabled             bool
	searchPersistenceEnabled bool
	recentResults            map[string]*domain.Release
}

func NewManager(s store, l logger, cacheEnabled bool, searchPersistenceEnabled bool) *Manager {
	return &Manager{
		sources:                  make(map[string]catalogSource),
		store:                    s,
		logger:                   l,
		cacheEnabled:             cacheEnabled,
		searchPersistenceEnabled: searchPersistenceEnabled,
		recentResults:            make(map[string]*domain.Release),
	}
}

// AddSource registers a new nzb source to the manager.
func (m *Manager) AddSource(src catalogSource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sources[src.Name()] = src
}

// SearchAll queries all sources loaded by the manager
func (m *Manager) SearchAll(ctx context.Context, query string) ([]*domain.Release, error) {
	merged := make(map[string]*domain.Release, 256)
	order := make([]string, 0, 256)

	addOrMerge := func(rel *domain.Release, preferIncoming bool) {
		if rel == nil {
			return
		}
		if rel.ID == "" {
			rel.ID = domain.GenerateCompositeID(rel.Source, rel.GUID)
		}
		if rel.ID == "" {
			return
		}

		in := cloneRelease(rel)

		if existing, ok := merged[in.ID]; ok {
			merged[in.ID] = mergeRelease(existing, in, preferIncoming)
			return
		}
		merged[in.ID] = in
		order = append(order, in.ID)
	}

	// optional fast local cache read (SQLite aggregator_release_cache).
	if m.searchPersistenceEnabled {
		cacheResults, err := m.store.SearchAggregatorReleaseCache(ctx, query, 100)
		if err != nil {
			m.logger.Warn("Failed to search aggregator_release_cache: %v", err)
		} else {
			for _, rel := range cacheResults {
				addOrMerge(rel, false)
			}
		}
	}

	var wg sync.WaitGroup
	resultsChan := make(chan []*domain.Release, len(m.sources))

	m.mu.RLock()
	for _, src := range m.sources {
		wg.Add(1)
		go func(s catalogSource) {
			defer wg.Done()

			// Create a per-indexer timeout context
			searchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			res, err := s.Search(searchCtx, query)
			if err != nil {
				m.logger.Error("Indexer %s error: %v", s.Name(), err)
				return
			}

			for _, r := range res {
				if r.ID == "" {
					r.ID = domain.GenerateCompositeID(r.Source, r.GUID)
				}
			}
			resultsChan <- res
		}(src)
	}
	m.mu.RUnlock()

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	allRemote := make([]*domain.Release, 0, 256)
	for res := range resultsChan {
		allRemote = append(allRemote, res...)
		for _, rel := range res {
			// remote results override cached fields on collision.
			addOrMerge(rel, true)
		}
	}

	allResults := make([]*domain.Release, 0, len(order))
	for _, id := range order {
		if rel := merged[id]; rel != nil {
			allResults = append(allResults, rel)
		}
	}

	// always refresh in-memory index (supports stateless lookup path).
	m.mu.Lock()
	m.recentResults = make(map[string]*domain.Release, len(allResults))
	for _, rel := range allResults {
		if rel == nil || rel.ID == "" {
			continue
		}
		m.recentResults[rel.ID] = cloneRelease(rel)
	}
	m.mu.Unlock()

	// persistence is gated by config
	if m.searchPersistenceEnabled && len(allResults) > 0 {
		if err := m.store.UpsertAggregatorReleaseCache(ctx, allResults); err != nil {
			m.logger.Warn("Failed to persist aggregator_release_cache: %v", err)
		}
	}

	return allResults, nil
}

// GetNZB handles retrieving nzb from cache or downloading from an indexer.
// Returns io.ReaderCloser so it can returned as a HTTP response or parsed for download
func (m *Manager) GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error) {

	if rel == nil {
		return nil, fmt.Errorf("release is required")
	}

	// Check the file store
	if m.cacheEnabled && m.store.Exists(rel.ID) {
		// ensure in-memory/current response reflects cached state.
		rel.CachePresent = true

		// keep aggregator_release_cache.nzb_cached in sync for UI/search.
		if m.searchPersistenceEnabled {
			if err := m.store.UpsertAggregatorReleaseCache(ctx, []*domain.Release{rel}); err != nil {
				m.logger.Warn("Failed to persist cached-state for %s: %v", rel.ID, err)
			}
		}

		return m.store.GetNZBReader(rel.ID)
	}

	// Find the indexer that provided this result.
	m.mu.RLock()
	src, ok := m.sources[rel.Source]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("aggregator source %s not found", rel.Source)
	}

	// This calls either the raw DownloadNZB or the local store indexer.
	body, err := src.GetNZB(ctx, rel)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch nzb from source: %w", err)
	}

	if !m.cacheEnabled {
		// pass-through mode for payload cache disabled.
		return body, nil
	}

	defer body.Close()

	// Read once so we can both cache atomically and still return data on
	// cache-write failures without re-downloading from upstream.
	payload, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed reading nzb payload: %w", err)
	}

	if err := m.store.SaveNZBAtomically(rel.ID, payload); err != nil {
		m.logger.Warn("Failed to persist cached NZB for %s: %v", rel.ID, err)
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	// cache write succeeded, update release cached state.
	rel.CachePresent = true

	// persist nzb_cached flag for later searches/UI state.
	if m.searchPersistenceEnabled {
		if err := m.store.UpsertAggregatorReleaseCache(ctx, []*domain.Release{rel}); err != nil {
			m.logger.Warn("Failed to persist cached-state for %s: %v", rel.ID, err)
		}
	}

	return m.store.GetNZBReader(rel.ID)
}

// GetResultByID resolves a release by id.
// checks in-memory recent results first, then persistent store if enabled.
func (m *Manager) GetResultByID(ctx context.Context, id string) (*domain.Release, error) {
	m.mu.RLock()
	if rel, ok := m.recentResults[id]; ok && rel != nil {
		cp := *rel
		m.mu.RUnlock()
		return &cp, nil
	}
	enabled := m.searchPersistenceEnabled
	m.mu.RUnlock()

	if !enabled {
		return nil, nil
	}

	return m.store.GetAggregatorReleaseCacheByID(ctx, id)
}

func cloneRelease(in *domain.Release) *domain.Release {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

// merge helper; preferIncoming=true means incoming overwrites most fields.
// CachePresent is OR'd so cache signal is not lost.
func mergeRelease(existing, incoming *domain.Release, preferIncoming bool) *domain.Release {
	if existing == nil {
		return cloneRelease(incoming)
	}
	if incoming == nil {
		return cloneRelease(existing)
	}

	if preferIncoming {
		out := cloneRelease(incoming)
		out.CachePresent = incoming.CachePresent || existing.CachePresent
		return out
	}

	out := cloneRelease(existing)
	if out.Title == "" {
		out.Title = incoming.Title
	}
	if out.Source == "" {
		out.Source = incoming.Source
	}
	if out.GUID == "" {
		out.GUID = incoming.GUID
	}
	if out.Category == "" {
		out.Category = incoming.Category
	}
	if out.Size == 0 {
		out.Size = incoming.Size
	}
	if out.PublishDate.IsZero() {
		out.PublishDate = incoming.PublishDate
	}
	if out.DownloadURL == "" {
		out.DownloadURL = incoming.DownloadURL
	}
	out.CachePresent = existing.CachePresent || incoming.CachePresent
	return out
}
