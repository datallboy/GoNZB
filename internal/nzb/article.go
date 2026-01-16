package nzb

import "io"

// Article respresents the metadata for a single Usenet post.
type Article struct {
	MessageID string
	Size      int
}

// ArticleRepository defines the contract for fetching data from Usenet.
type ArticleRespository interface {
	FetchBody(messageID string) (io.Reader, error)
	Authenticate() error
	Close() error
}
