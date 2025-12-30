package provider

import (
	"context"
	"fmt"
	"gonzb/internal/domain"
	"io"
	"sort"
	"strings"
	"time"
)

var FETCH_RETRY_COUNT = 3

type managedProvider struct {
	domain.Provider
	semaphore chan struct{}
}

type releaseReader struct {
	io.Reader
	onClose func()
}

type Manager struct {
	providers []*managedProvider
}

func NewManager(providers []domain.Provider) *Manager {
	var managed []*managedProvider
	for _, p := range providers {
		managed = append(managed, &managedProvider{
			Provider:  p,
			semaphore: make(chan struct{}, p.MaxConnection()),
		})
	}

	// Sort providers by priority (0 = highest)
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].Priority() < providers[j].Priority()
	})
	return &Manager{providers: managed}
}

func (m *Manager) FetchArticle(ctx context.Context, msgID string) (io.Reader, error) {
	var lastErr error

	for _, mp := range m.providers {
		select {
		case mp.semaphore <- struct{}{}:
			// Try to fetch with a small internal retry for network blips
			reader, err := m.tryFetch(ctx, mp, msgID)
			if err != nil {
				<-mp.semaphore // Release immediately if fetch failed
				lastErr = err
				continue // Try the next provider
			}

			// 2. Wrap the reader to release the slot only when Close() is called
			return &releaseReader{
				Reader: reader,
				onClose: func() {
					<-mp.semaphore
				},
			}, nil

		default:
			// No connections available for this provider right now, try next...
			continue
		}
	}
	return nil, fmt.Errorf("article %s not found on any provider (last error: %v)", msgID, lastErr)
}

func (r *releaseReader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	// If we hit EOF or an error, we can trigger the release
	if err != nil {
		if r.onClose != nil {
			r.onClose()
			r.onClose = nil
		}
	}

	return n, err
}

// try fetch will attempt to fetch an article with some logic to check missing articles or retry
func (m *Manager) tryFetch(ctx context.Context, p *managedProvider, msgID string) (io.Reader, error) {
	// Simple interneal retry for network blips
	for i := 0; i < FETCH_RETRY_COUNT; i++ {
		reader, err := p.Fetch(ctx, msgID)
		if err == nil {
			return reader, nil
		}

		// If the error is specifically "430 No Such Article", don't retry this provider
		if strings.Contains(err.Error(), "430") {
			return nil, err
		}

		// wait a moment before retrying a network timeout
		time.Sleep(100 * time.Millisecond)
	}

	return nil, fmt.Errorf("provider %s failed after retries", p.ID())
}
