package downloader

import (
	"context"
	"fmt"
	"slices"

	"github.com/datallboy/gonzb/internal/domain"
)

type Queries struct {
	provider DependencyProvider
}

func NewQueries(provider DependencyProvider) *Queries {
	return &Queries{provider: provider}
}

func (q *Queries) ListActive() []*domain.QueueItem {
	queue := q.provider.Queue()
	if queue == nil {
		return []*domain.QueueItem{}
	}
	return queue.GetAllItems()
}

func (q *Queries) ListHistory(ctx context.Context, status string, limit, offset int) ([]*domain.QueueItem, int, error) {
	jobStore := q.provider.JobStore()
	if jobStore == nil {
		return nil, 0, fmt.Errorf("job store is unavailable")
	}

	items, err := jobStore.GetQueueItems(ctx)
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

func (q *Queries) GetActiveItem() *domain.QueueItem {
	queue := q.provider.Queue()
	if queue == nil {
		return nil
	}
	return queue.GetActiveItem()
}

func (q *Queries) GetItem(ctx context.Context, id string) (*domain.QueueItem, error) {
	queue := q.provider.Queue()
	if queue == nil {
		return nil, fmt.Errorf("downloader queue is unavailable")
	}

	item, ok := queue.GetItem(ctx, id)
	if !ok || item == nil {
		return nil, nil
	}
	return item, nil
}

func (q *Queries) GetItemFiles(ctx context.Context, id string) ([]*domain.DownloadFile, error) {
	queue := q.provider.Queue()
	queueFileStore := q.provider.QueueFileStore()
	if queue == nil || queueFileStore == nil {
		return nil, fmt.Errorf("queue file store is unavailable")
	}

	item, ok := queue.GetItem(ctx, id)
	if !ok || item == nil {
		return nil, nil
	}
	return queueFileStore.GetQueueItemFiles(ctx, item.ID)
}

func (q *Queries) GetItemEvents(ctx context.Context, id string) ([]*domain.QueueItemEvent, error) {
	queue := q.provider.Queue()
	jobStore := q.provider.JobStore()
	if queue == nil || jobStore == nil {
		return nil, fmt.Errorf("job store is unavailable")
	}

	item, ok := queue.GetItem(ctx, id)
	if !ok || item == nil {
		return nil, nil
	}
	return jobStore.GetQueueEvents(ctx, item.ID)
}

func (q *Queries) IsPaused() bool {
	queue := q.provider.Queue()
	if queue == nil {
		return false
	}
	return queue.IsPaused()
}
