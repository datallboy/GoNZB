package sqlitejob

import (
	"context"

	"github.com/datallboy/gonzb/internal/domain"
)

func (s *Store) SaveQueueItem(ctx context.Context, item *domain.QueueItem) error {
	return s.legacy.SaveQueueItem(ctx, item)
}

func (s *Store) GetQueueItem(ctx context.Context, id string) (*domain.QueueItem, error) {
	return s.legacy.GetQueueItem(ctx, id)
}

func (s *Store) GetQueueItems(ctx context.Context) ([]*domain.QueueItem, error) {
	return s.legacy.GetQueueItems(ctx)
}

func (s *Store) GetActiveQueueItems(ctx context.Context) ([]*domain.QueueItem, error) {
	return s.legacy.GetActiveQueueItems(ctx)
}

func (s *Store) DeleteQueueItems(ctx context.Context, ids []string) (int64, error) {
	return s.legacy.DeleteQueueItems(ctx, ids)
}

func (s *Store) ClearQueueHistory(ctx context.Context, statuses []domain.JobStatus) (int64, error) {
	return s.legacy.ClearQueueHistory(ctx, statuses)
}

func (s *Store) ResetStuckQueueItems(ctx context.Context, newStatus domain.JobStatus, oldStatuses ...domain.JobStatus) error {
	return s.legacy.ResetStuckQueueItems(ctx, newStatus, oldStatuses...)
}

func (s *Store) SaveReleaseFiles(ctx context.Context, releaseID string, files []*domain.DownloadFile) error {
	return s.legacy.SaveReleaseFiles(ctx, releaseID, files)
}

func (s *Store) GetReleaseFiles(ctx context.Context, releaseID string) ([]*domain.DownloadFile, error) {
	return s.legacy.GetReleaseFiles(ctx, releaseID)
}
