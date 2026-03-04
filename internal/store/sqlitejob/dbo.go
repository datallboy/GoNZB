package sqlitejob

import (
	"database/sql"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

// queueItemDBO maps to the queue_items table
type queueItemDBO struct {
	ID                 string         `db:"id"`
	ReleaseID          string         `db:"release_id"`
	Status             string         `db:"status"`
	OutDir             string         `db:"out_dir"`
	Error              sql.NullString `db:"error"`
	CreatedAt          time.Time      `db:"created_at"`
	UpdatedAt          time.Time      `db:"updated_at"`
	StartedAtUnix      int64          `db:"started_at_unix"`
	CompletedAtUnix    int64          `db:"completed_at_unix"`
	DownloadSeconds    int64          `db:"download_seconds"`
	PostProcessSeconds int64          `db:"postprocess_seconds"`
	AvgBps             int64          `db:"avg_bps"`
	DownloadedBytes    int64          `db:"downloaded_bytes"`
}

// Mapper: DBO to Domain QueueItem
func (q *queueItemDBO) ToDomain(rel *domain.Release) *domain.QueueItem {
	item := &domain.QueueItem{
		ID:                 q.ID,
		ReleaseID:          q.ReleaseID,
		Release:            rel,
		Status:             domain.JobStatus(q.Status),
		OutDir:             q.OutDir,
		CreatedAt:          q.CreatedAt,
		UpdatedAt:          q.UpdatedAt,
		DownloadSeconds:    q.DownloadSeconds,
		PostProcessSeconds: q.PostProcessSeconds,
		AvgBps:             q.AvgBps,
		DownloadedBytes:    q.DownloadedBytes,
	}
	if q.StartedAtUnix > 0 {
		item.StartedAt = time.Unix(q.StartedAtUnix, 0).UTC()
	}
	if q.CompletedAtUnix > 0 {
		item.CompletedAt = time.Unix(q.CompletedAtUnix, 0).UTC()
	}
	item.BytesWritten.Store(q.DownloadedBytes)
	if q.Error.Valid {
		errStr := q.Error.String
		item.Error = &errStr
	}
	return item
}
