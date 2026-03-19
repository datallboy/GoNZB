package sqlitejob

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

// queueItemDBO maps to the queue_items table
type queueItemDBO struct {
	ID                  string         `db:"id"`
	Status              string         `db:"status"`
	OutDir              string         `db:"out_dir"`
	Error               sql.NullString `db:"error"`
	SourceKind          string         `db:"source_kind"`
	SourceReleaseID     sql.NullString `db:"source_release_id"`
	ReleaseTitle        string         `db:"release_title"`
	ReleaseSize         int64          `db:"release_size"`
	ReleaseSnapshotJSON string         `db:"release_snapshot_json"`
	PayloadMode         string         `db:"payload_mode"`
	Resumable           bool           `db:"resumable"`
	CreatedAt           time.Time      `db:"created_at"`
	UpdatedAt           time.Time      `db:"updated_at"`
	StartedAtUnix       int64          `db:"started_at_unix"`
	CompletedAtUnix     int64          `db:"completed_at_unix"`
	DownloadSeconds     int64          `db:"download_seconds"`
	PostProcessSeconds  int64          `db:"postprocess_seconds"`
	AvgBps              int64          `db:"avg_bps"`
	DownloadedBytes     int64          `db:"downloaded_bytes"`
}

// Mapper: DBO to Domain QueueItem
func (q *queueItemDBO) ToDomain() *domain.QueueItem {
	item := &domain.QueueItem{
		ID:                  q.ID,
		Status:              domain.JobStatus(q.Status),
		OutDir:              q.OutDir,
		SourceKind:          q.SourceKind,
		SourceReleaseID:     q.SourceReleaseID.String,
		ReleaseTitle:        q.ReleaseTitle,
		ReleaseSize:         q.ReleaseSize,
		ReleaseSnapshotJSON: q.ReleaseSnapshotJSON,
		PayloadMode:         q.PayloadMode,
		Resumable:           q.Resumable,
		CreatedAt:           q.CreatedAt,
		UpdatedAt:           q.UpdatedAt,
		DownloadSeconds:     q.DownloadSeconds,
		PostProcessSeconds:  q.PostProcessSeconds,
		AvgBps:              q.AvgBps,
		DownloadedBytes:     q.DownloadedBytes,
	}

	if item.PayloadMode == "" {
		item.PayloadMode = domain.PayloadModeCached
	}
	if item.PayloadMode == domain.PayloadModeCached && !q.Resumable {
		item.Resumable = false
	} else if item.PayloadMode == domain.PayloadModeCached && q.Resumable {
		item.Resumable = true
	} else if item.PayloadMode == domain.PayloadModeEphemeral {
		item.Resumable = false
	}

	item.ReleaseID = item.SourceReleaseID

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

	// restore as much release metadata as possible from the persisted snapshot.
	item.Release = releaseFromSnapshot(
		item.SourceReleaseID,
		item.SourceKind,
		item.ReleaseTitle,
		item.ReleaseSize,
		item.ReleaseSnapshotJSON,
	)

	return item
}

func releaseFromSnapshot(id, sourceKind, title string, size int64, snapshotJSON string) *domain.Release {
	var rel domain.Release

	if snapshotJSON != "" && snapshotJSON != "{}" {
		if err := json.Unmarshal([]byte(snapshotJSON), &rel); err == nil {
			if rel.ID == "" {
				rel.ID = id
			}
			if rel.GUID == "" {
				rel.GUID = id
			}
			if rel.Source == "" {
				rel.Source = sourceKind
			}
			if rel.Title == "" {
				rel.Title = title
			}
			if rel.Size == 0 {
				rel.Size = size
			}
			return &rel
		}
	}

	if id != "" || title != "" || size > 0 {
		return &domain.Release{
			ID:     id,
			GUID:   id,
			Title:  title,
			Size:   size,
			Source: sourceKind,
		}
	}

	return nil
}
