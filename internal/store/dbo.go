package store

import (
	"database/sql"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

// releaseDBO maps to the releases table
type releaseDBO struct {
	ID              string         `db:"id"`
	Title           string         `db:"title"`
	Size            int64          `db:"size"`
	Password        sql.NullString `db:"password"`
	GUID            sql.NullString `db:"guid"`
	Source          sql.NullString `db:"source"`
	DownloadURL     sql.NullString `db:"download_url"`
	PublishDate     int64          `db:"publish_date"`
	Category        sql.NullString `db:"category"`
	RedirectAllowed bool           `db:"redirect_allowed"`
	CachePresent    bool           `db:"cache_present"`
	CacheBlobSize   int64          `db:"cache_blob_size"`
	CacheVerifiedAt int64          `db:"cache_verified_at_unix"`
	CreatedAt       time.Time      `db:"created_at"`
}

// Mapper: DBO to Domain Release
func (r *releaseDBO) ToDomain() *domain.Release {
	var verifiedAt time.Time
	if r.CacheVerifiedAt > 0 {
		verifiedAt = time.Unix(r.CacheVerifiedAt, 0)
	}

	return &domain.Release{
		ID:              r.ID,
		Title:           r.Title,
		Size:            r.Size,
		Password:        r.Password.String,
		GUID:            r.GUID.String,
		Source:          r.Source.String,
		DownloadURL:     r.DownloadURL.String,
		PublishDate:     time.Unix(r.PublishDate, 0),
		Category:        r.Category.String,
		RedirectAllowed: r.RedirectAllowed,
		CachePresent:    r.CachePresent,
		CacheBlobSize:   r.CacheBlobSize,
		CacheVerifiedAt: verifiedAt,
	}
}

// Mapper: Domain Release to DBO
func (r *releaseDBO) FromDomain(rel *domain.Release) {
	r.ID = rel.ID
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
