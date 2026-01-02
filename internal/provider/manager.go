package provider

import (
	"context"
	"fmt"
	"gonzb/internal/config"
	"gonzb/internal/domain"
	"gonzb/internal/logger"
	"gonzb/internal/nntp"
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

type Manager struct {
	providers []*managedProvider
	logger    *logger.Logger
}

func NewManager(configs []config.ServerConfig, l *logger.Logger) (*Manager, error) {
	var managed []*managedProvider

	for _, cfg := range configs {
		p := nntp.NewNNTPProvider(cfg)

		l.Info("Validating provider: %s", p.ID())
		if err := p.TestConnection(); err != nil {
			return nil, fmt.Errorf("connection test failed for %s: %w", p.ID(), err)
		}

		managed = append(managed, &managedProvider{
			Provider:  p,
			semaphore: make(chan struct{}, p.MaxConnection()),
		})
	}

	// Sort providers by priority (0 = highest)
	sort.Slice(managed, func(i, j int) bool {
		return managed[i].Priority() < managed[j].Priority()
	})
	return &Manager{providers: managed, logger: l}, nil
}

func (m *Manager) FetchArticle(ctx context.Context, msgID string, groups []string) (io.Reader, error) {
	var lastErr error

	for _, mp := range m.providers {
		select {
		case mp.semaphore <- struct{}{}:
			// Got a slot!
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		reader, err := m.tryFetch(ctx, mp, msgID, groups)
		if err != nil {
			// Release the slot if the fetch fails so
			// the next worker can try this provider for a different article.
			<-mp.semaphore
			lastErr = err
			continue
		}

		if reader == nil {
			<-mp.semaphore
			continue
		}

		// Return a reader that releases the semaphore ONLY when the
		// worker is finished reading the body.
		return &releaseReader{
			Reader: reader,
			onClose: func() {
				<-mp.semaphore
			},
		}, nil
	}

	return nil, fmt.Errorf("article not found on any provider: %w", lastErr)
}

// try fetch will attempt to fetch an article with some logic to check missing articles or retry
func (m *Manager) tryFetch(ctx context.Context, p *managedProvider, msgID string, groups []string) (io.Reader, error) {
	// Simple interneal retry for network blips
	for i := 0; i < FETCH_RETRY_COUNT; i++ {
		reader, err := p.Fetch(ctx, msgID, groups)
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

type releaseReader struct {
	io.Reader
	onClose func()
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

func (r *releaseReader) Close() error {
	// 1. Guard against nil reader
	if r.Reader != nil {
		if c, ok := r.Reader.(io.ReadCloser); ok {
			c.Close()
		}
	}

	// 2. Guard against nil onClose function
	// and ensure it only runs once
	if r.onClose != nil {
		r.onClose()
		r.onClose = nil // Prevent double-release if Close is called twice
	}
	return nil
}
