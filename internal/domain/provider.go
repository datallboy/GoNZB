package domain

import (
	"context"
	"io"
)

// Provider is the Domain's contract for what it needs to start a provider.
type ProviderConfig struct {
	ID            string
	Host          string
	Port          int
	Username      string
	Password      string
	TLS           bool
	MaxConnection int
	Priority      int
}

// Provider represents the contract for a Usenet server connection.
type Provider interface {
	ID() string
	Priority() int
	MaxConnection() int
	Fetch(ctx context.Context, msgID string) (io.Reader, error)
	Close() error
}
