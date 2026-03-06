package queue

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
)

type Service struct {
	app *app.Context
}

func NewService(appCtx *app.Context) *Service {
	return &Service{app: appCtx}
}

func (s *Service) ListActive() []*domain.QueueItem {
	return s.app.Queue.GetAllItems()
}

func (s *Service) ListHistory(ctx context.Context, status string, limit, offset int) ([]*domain.QueueItem, int, error) {
	items, err := s.app.JobStore.GetQueueItems(ctx)
	if err != nil {
		return nil, 0, err
	}

	filtered := make([]*domain.QueueItem, 0, len(items))
	for _, item := range items {
		if status != "" && string(item.Status) != status {
			continue
		}
		filtered = append(filtered, item)
	}

	slices.Reverse(filtered)
	total := len(filtered)

	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	return filtered[start:end], total, nil
}

func (s *Service) GetItem(ctx context.Context, id string) (*domain.QueueItem, error) {
	item, ok := s.app.Queue.GetItem(ctx, id)
	if !ok || item == nil {
		return nil, nil
	}
	return item, nil
}

func (s *Service) GetItemFiles(ctx context.Context, id string) ([]*domain.DownloadFile, error) {
	item, ok := s.app.Queue.GetItem(ctx, id)
	if !ok || item == nil {
		return nil, nil
	}
	return s.app.QueueFileStore.GetQueueItemFiles(ctx, item.ID)
}

func (s *Service) GetItemEvents(ctx context.Context, id string) ([]*domain.QueueItemEvent, error) {
	item, ok := s.app.Queue.GetItem(ctx, id)
	if !ok || item == nil {
		return nil, nil
	}
	return s.app.JobStore.GetQueueEvents(ctx, item.ID)
}

func (s *Service) Cancel(id string) bool {
	return s.app.Queue.Cancel(id)
}

func (s *Service) CancelMany(ids []string) int {
	cancelled := 0
	for _, id := range ids {
		if s.app.Queue.Cancel(id) {
			cancelled++
		}
	}
	return cancelled
}

func (s *Service) DeleteMany(ctx context.Context, ids []string) (int64, error) {
	terminal := make([]string, 0, len(ids))
	for _, id := range ids {
		item, ok := s.app.Queue.GetItem(ctx, id)
		if !ok || item == nil {
			continue
		}
		if item.Status == domain.StatusCompleted || item.Status == domain.StatusFailed {
			terminal = append(terminal, id)
		}
	}

	return s.app.JobStore.DeleteQueueItems(ctx, terminal)
}

func (s *Service) ClearHistory(ctx context.Context) (int64, error) {
	return s.app.JobStore.ClearQueueHistory(ctx, []domain.JobStatus{domain.StatusCompleted, domain.StatusFailed})
}

func (s *Service) EnqueueByReleaseID(ctx context.Context, releaseID, title string) (*domain.QueueItem, error) {
	if releaseID == "" {
		return nil, fmt.Errorf("release_id is required")
	}

	rel, err := s.app.Resolver.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve release %s: %w", releaseID, err)
	}
	if rel == nil {
		return nil, fmt.Errorf("release %s not found", releaseID)
	}

	// request title can override display title if caller supplied one
	effectiveTitle := rel.Title
	if title != "" {
		effectiveTitle = title
	}

	item, err := s.app.Queue.Add(ctx, releaseID, effectiveTitle)
	if err != nil {
		return nil, err
	}

	// Fill response payload with release metadata when available.
	if item.Release == nil {
		item.Release = rel
	}

	return item, nil
}

func (s *Service) SearchReleases(ctx context.Context, query string) ([]*domain.Release, error) {
	if query == "" {
		return []*domain.Release{}, nil
	}
	return s.app.Resolver.SearchReleases(ctx, query)
}

func (s *Service) EnqueueNZB(ctx context.Context, filename string, file io.Reader) (*domain.QueueItem, error) {
	if filename == "" {
		filename = "manual.nzb"
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read uploaded nzb: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("uploaded nzb is empty")
	}

	releaseID, err := domain.CalculateFileHash(bytesReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to hash uploaded nzb: %w", err)
	}

	if err := s.app.BlobStore.SaveNZBAtomically(releaseID, data); err != nil {
		return nil, fmt.Errorf("failed to persist nzb in blob store: %w", err)
	}

	item, err := s.app.Queue.Add(ctx, releaseID, filename)
	if err != nil {
		return nil, err
	}

	// best-effort in-memory release shape for response payloads
	if item.Release == nil {
		item.Release = &domain.Release{
			ID:       releaseID,
			GUID:     releaseID,
			Title:    filename,
			Source:   "manual",
			Category: "Uncategorized",
		}
	}

	return item, nil
}

func bytesReader(b []byte) io.Reader {
	return bytes.NewReader(b)
}
