package app

import (
	"context"
	"io"

	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
	"github.com/datallboy/gonzb/internal/nzb"
)

type NNTPManager interface {
	// This allows the engine to call the manager without importing the nntp package
	Fetch(ctx context.Context, msgID string, groups []string) (io.Reader, error)
	TotalCapacity() int
}

type Processor interface {
	// This allows the engine to trigger repair/extract without importing processor
	Prepare(nzbModel *nzb.Model) ([]*nzb.DownloadFile, error)
	Finalize(ctx context.Context, tasks []*nzb.DownloadFile) error
	PostProcess(ctx context.Context, tasks []*nzb.DownloadFile) error
}

// Context hold the core environment and shared resources for GoNZB.
// It acts as the "Single Source of Truth" for the application state.
type Context struct {
	Config *config.Config
	Logger *logger.Logger

	// High-level interfaces for services to use
	NNTP      NNTPManager
	Processor Processor

	ExtractionEnabled bool
}

// NewContext initializes the base environment.
func NewContext(cfg *config.Config, log *logger.Logger) *Context {
	return &Context{
		Config:            cfg,
		Logger:            log,
		ExtractionEnabled: true,
	}
}
