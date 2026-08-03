package domain

import (
	"time"
)

// Release represents a searchable NZB release.
type Release struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Password        string    `json:"password"`
	GUID            string    `json:"guid"`
	Source          string    `json:"source"`
	DownloadURL     string    `json:"downloadUrl"`
	Size            int64     `json:"size"`
	PublishDate     time.Time `json:"publishDate"`
	Category        string    `json:"category"`
	CachePresent    bool      `json:"cache_present"`
	CacheBlobSize   int64     `json:"cache_blob_size"`
	CacheVerifiedAt time.Time `json:"cache_verified_at"`
	RedirectAllowed bool
	Poster          string
}

// Segment represents an individual article to be fetched from Usenet
type Segment struct {
	Number      int
	Bytes       int64
	MessageID   string
	MissingFrom map[string]bool
}
