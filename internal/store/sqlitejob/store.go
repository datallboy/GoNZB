package sqlitejob

import (
	"context"

	"github.com/datallboy/gonzb/internal/domain"
)

type legacyStore interface {
	SaveQueueItem(ctx context.Context, item *domain.QueueItem) error
	GetQueueItem(ctx context.Context, id string) (*domain.QueueItem, error)
	GetQueueItems(ctx context.Context) ([]*domain.QueueItem, error)
	GetActiveQueueItems(ctx context.Context) ([]*domain.QueueItem, error)
	DeleteQueueItems(ctx context.Context, ids []string) (int64, error)
	ClearQueueHistory(ctx context.Context, statuses []domain.JobStatus) (int64, error)
	SaveQueueEvent(ctx context.Context, ev *domain.QueueItemEvent) error
	GetQueueEvents(ctx context.Context, queueID string) ([]*domain.QueueItemEvent, error)
	ResetStuckQueueItems(ctx context.Context, newStatus domain.JobStatus, oldStatuses ...domain.JobStatus) error
	SaveReleaseFiles(ctx context.Context, releaseID string, files []*domain.DownloadFile) error
	GetReleaseFiles(ctx context.Context, releaseID string) ([]*domain.DownloadFile, error)
}

type Store struct {
	legacy legacyStore
}

func New(legacy legacyStore) *Store {
	return &Store{legacy: legacy}
}
