package store

import (
	"database/sql"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

// releaseDBO maps to the releases table
type releaseDBO struct {
	ID              string         `db:"id"`
	FileHash        string         `db:"file_hash"`
	Title           string         `db:"title"`
	Size            int64          `db:"size"`
	Password        sql.NullString `db:"password"`
	GUID            sql.NullString `db:"guid"`
	Source          sql.NullString `db:"source"`
	DownloadURL     sql.NullString `db:"download_url"`
	PublishDate     int64          `db:"publish_date"`
	Category        sql.NullString `db:"category"`
	RedirectAllowed bool           `db:"redirect_allowed"`
	CreatedAt       time.Time      `db:"created_at"`
}

// Mapper: DBO to Domain Release
func (r *releaseDBO) ToDomain() *domain.Release {
	return &domain.Release{
		ID:          r.ID,
		FileHash:    r.FileHash,
		Title:       r.Title,
		Size:        r.Size,
		Password:    r.Password.String,
		GUID:        r.GUID.String,
		Source:      r.Source.String,
		DownloadURL: r.DownloadURL.String,
		PublishDate: time.Unix(r.PublishDate, 0),
		Category:    r.Category.String,
	}
}

// Mapper: Domain Release to DBO
func (r *releaseDBO) FromDomain(rel *domain.Release) {
	r.ID = rel.ID
	r.FileHash = rel.FileHash
	r.Title = rel.Title
	r.Size = rel.Size
	r.Password = sql.NullString{String: rel.Password, Valid: rel.Password != ""}
	r.GUID = sql.NullString{String: rel.GUID, Valid: rel.GUID != ""}
	r.Source = sql.NullString{String: rel.Source, Valid: rel.Source != ""}
	r.DownloadURL = sql.NullString{String: rel.DownloadURL, Valid: rel.DownloadURL != ""}

	if !rel.PublishDate.IsZero() {
		r.PublishDate = rel.PublishDate.Unix()
	} else {
		r.PublishDate = 0
	}

	r.Category = sql.NullString{String: rel.Category, Valid: rel.Category != ""}
	r.RedirectAllowed = rel.RedirectAllowed
}

// queueItemDBO maps to the queue_items table
type queueItemDBO struct {
	ID        string         `db:"id"`
	ReleaseID string         `db:"release_id"`
	Status    string         `db:"status"`
	OutDir    string         `db:"out_dir"`
	Error     sql.NullString `db:"error"`
	CreatedAt time.Time      `db:"created_at"`
}

// Mapper: DBO to Domain QueueItem
func (q *queueItemDBO) ToDomain(rel *domain.Release) *domain.QueueItem {
	item := &domain.QueueItem{
		ID:        q.ID,
		ReleaseID: q.ReleaseID,
		Release:   rel,
		Status:    domain.JobStatus(q.Status),
		OutDir:    q.OutDir,
	}
	if q.Error.Valid {
		errStr := q.Error.String
		item.Error = &errStr
	}
	return item
}
