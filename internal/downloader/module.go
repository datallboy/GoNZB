package downloader

import "github.com/datallboy/gonzb/internal/app"

type DependencyProvider struct {
	Queue          func() app.QueueManager
	Resolver       func() app.ReleaseResolver
	BlobStore      func() app.BlobStore
	JobStore       func() app.JobStore
	QueueFileStore func() app.QueueFileStore
}

type Module struct {
	commands *Commands
	queries  *Queries
}

func NewModule(provider DependencyProvider) *Module {
	return &Module{
		commands: NewCommands(provider),
		queries:  NewQueries(provider),
	}
}

func (m *Module) Commands() app.DownloaderCommands {
	if m == nil {
		return nil
	}
	return m.commands
}

func (m *Module) Queries() app.DownloaderQueries {
	if m == nil {
		return nil
	}
	return m.queries
}
