package indexer

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"time"
)

// SearchResult is a normalized view of an entry from any indexer
type SearchResult struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	GUID            string    `json:"guid"`
	DownloadURL     string    `json:"downloadUrl"`
	Size            int64     `json:"size"`
	Source          string    `json:"source"`
	PublishDate     time.Time `json:"publishDate"`
	Category        string    `json:"category"`
	RedirectAllowed bool
}

// Indexer is the contract any source (Newznab, Local, Scraper) must fulfill
type Indexer interface {
	Name() string
	Search(ctx context.Context, query string) ([]SearchResult, error)
	DownloadNZB(ctx context.Context, res SearchResult) (io.ReadCloser, error)
}

func (r *SearchResult) SetCompositeID() {
	data := fmt.Sprintf("%s|%s", r.Source, r.GUID)
	h := sha1.New()
	h.Write([]byte(data))
	r.ID = fmt.Sprintf("%x", h.Sum(nil))
}
