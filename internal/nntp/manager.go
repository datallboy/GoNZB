package nntp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
)

var FETCH_RETRY_COUNT = 3

type managedProvider struct {
	Provider
	semaphore chan struct{}
}

type Manager struct {
	ctx       *app.Context
	providers []*managedProvider
}

func NewManager(ctx *app.Context) (*Manager, error) {
	var managed []*managedProvider

	for _, cfg := range ctx.Config.Servers {
		p := NewNNTPProvider(cfg)

		ctx.Logger.Info("Validating provider: %s", p.ID())
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
	return &Manager{ctx: ctx, providers: managed}, nil
}

func (m *Manager) Fetch(ctx context.Context, seg *domain.Segment, groups []string) (io.Reader, error) {

	// Fast fail if user already cancelled
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if seg.MissingFrom == nil {
		seg.MissingFrom = make(map[string]bool)
	}

	var lastErr error

	for _, mp := range m.providers {
		// Skip if this provider already reported 430 for this article
		if seg.MissingFrom[mp.ID()] {
			continue
		}

		// If we already have some 430s for this segment, log that we are trying a failover
		if len(seg.MissingFrom) > 0 {
			m.ctx.Logger.Debug("[Failover] Segment %s missing on %d providers, trying %s (Priority %d)",
				seg.MessageID, len(seg.MissingFrom), mp.ID(), mp.Priority())
		}

		select {
		case mp.semaphore <- struct{}{}:
			m.ctx.Logger.Debug("Segment %s: Attempting fetch from %s", seg.MessageID, mp.ID())
			reader, err := m.tryFetch(ctx, mp, seg.MessageID, groups)
			if err != nil {
				// Release the slot if the fetch fails
				<-mp.semaphore

				if errors.Is(err, ErrArticleNotFound) {
					m.ctx.Logger.Debug("Provider %s: 430 Missing, marking as missing for segment %s...", mp.ID(), seg.MessageID)
					seg.MissingFrom[mp.ID()] = true
					
					// Small sleep before trying next provider in failover
					time.Sleep(100 * time.Millisecond)
					continue
				}

				// If it's a network/auth error, keep looking but save error
				m.ctx.Logger.Debug("Failover: %s error: %v", mp.ID(), err)
				lastErr = err
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
		default:
			// Provider is at MaxConnections, skip for now
			continue
		}
	}

	// If all providers are confirmed missing
	if len(seg.MissingFrom) == len(m.providers) {
		return nil, ErrArticleNotFound
	}

	// If we have a real error (not 430), return it to trigger a retry with backoff
	if lastErr != nil {
		return nil, lastErr
	}

	// Some providers were busy, so we tell the worker to wait and try again
	return nil, ErrProviderBusy
}

// try fetch will attempt to fetch an article from a provider
func (m *Manager) tryFetch(ctx context.Context, p *managedProvider, msgID string, groups []string) (io.Reader, error) {
	reader, err := p.Fetch(ctx, msgID, groups)
	if err != nil {
		return nil, err // Let the caller decide how to retry
	}

	if reader == nil {
		return nil, fmt.Errorf("provider returned no error but reader is nil")
	}
	return reader, nil
}

type releaseReader struct {
	io.Reader
	onClose func()
}

func (r *releaseReader) Read(p []byte) (n int, err error) {
	return r.Reader.Read(p)
}

func (r *releaseReader) Close() error {
	defer func() {
		if r.onClose != nil {
			r.onClose()
			r.onClose = nil
		}
	}()

	if r.Reader != nil {
		if c, ok := r.Reader.(io.ReadCloser); ok {
			return c.Close()
		}
	}
	return nil
}

// TotalCapacity returns the maximum number of concurrent connections
// allowed across all configured providers.
func (m *Manager) TotalCapacity() int {
	total := 0
	for _, mp := range m.providers {
		// cap() tells us the size of the semaphore buffer
		// which equals the MaxConnections for that provider.
		total += cap(mp.semaphore)
	}
	return total
}
