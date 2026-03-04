package sqlitejob

import (
	"context"

	"github.com/datallboy/gonzb/internal/domain"
)

func (s *Store) SaveQueueEvent(ctx context.Context, ev *domain.QueueItemEvent) error {
	return s.legacy.SaveQueueEvent(ctx, ev)
}

func (s *Store) GetQueueEvents(ctx context.Context, queueID string) ([]*domain.QueueItemEvent, error) {
	return s.legacy.GetQueueEvents(ctx, queueID)
}
