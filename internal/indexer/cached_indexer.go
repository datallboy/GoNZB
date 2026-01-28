package indexer

import (
	"context"
	"fmt"
)

// IndexerCache is a simple interface for storage, making it swappable (File vs SQLite)
type IndexerCache interface {
	Get(id string) ([]byte, error)
	Put(id string, data []byte) error
}

// CachedIndexer "Decorates" a standard indexer with caching logic
type CachedIndexer struct {
	inner Indexer
	cache IndexerCache
}

func NewCachedIndexer(inner Indexer, cache IndexerCache) *CachedIndexer {
	return &CachedIndexer{inner: inner, cache: cache}
}

func (c *CachedIndexer) Name() string { return c.inner.Name() }

func (c *CachedIndexer) Search(ctx context.Context, query string) ([]SearchResult, error) {
	// We typically don't cache search results at the file level,
	// but we could in a database later. For now, pass through.
	return c.inner.Search(ctx, query)
}

func (c *CachedIndexer) DownloadNZB(ctx context.Context, id string) ([]byte, error) {
	// 1. Check the cache first
	if data, err := c.cache.Get(id); err == nil {
		fmt.Printf("Cache hit for NZB: %s\n", id)
		return data, nil
	}

	// 2. Cache miss: Call the actual indexer (Newznab, Scraper, etc)
	data, err := c.inner.DownloadNZB(ctx, id)
	if err != nil {
		return nil, err
	}

	// 3. Save to cache for next time
	_ = c.cache.Put(id, data)
	return data, nil
}
