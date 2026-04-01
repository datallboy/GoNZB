package app

import (
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
	"io"
)

// Context hold the core environment and shared resources for GoNZB.
// It acts as the "Single Source of Truth" for the application state.
type Context struct {
	BootstrapConfig *config.Config
	Config          *config.Config
	Logger          *logger.Logger

	// High-level interfaces for services to use
	NNTP              NNTPManager
	Aggregator        IndexerAggregator
	Resolver          ReleaseResolver
	UsenetIndexer     UsenetIndexerService
	Processor         Processor
	Downloader        Downloader
	Queue             QueueManager
	NZBParser         NZBParser
	JobStore          JobStore
	QueueFileStore    QueueFileStore
	BlobStore         BlobStore
	PayloadFetcher    PayloadFetcher
	PayloadCacheStore PayloadCacheStore
	SettingsStore     SettingsStore
	PGIndexStore      UsenetIndexStore
	ArrNotifier       ArrNotifier

	DownloaderModule DownloaderModule
	AggregatorModule AggregatorModule
	SettingsAdmin    SettingsAdmin

	ExtractionEnabled bool
	closers           []io.Closer
	runtimeModules    map[string]RuntimeModule
}

// NewContext returns the shared application container. Concrete runtime
// construction lives in internal/runtime/wiring.
func NewContext(cfg *config.Config, log *logger.Logger) (*Context, error) {
	return &Context{
		BootstrapConfig:   cfg,
		Config:            cfg,
		Logger:            log,
		ExtractionEnabled: true,
		closers:           make([]io.Closer, 0, 3),
		runtimeModules:    make(map[string]RuntimeModule),
	}, nil
}

// allow runtime wiring (from main) to register additional closers.
func (ctx *Context) AddCloser(c io.Closer) {
	if c == nil {
		return
	}
	ctx.closers = append(ctx.closers, c)
}

func (ctx *Context) Close() {
	for _, module := range ctx.RuntimeModules() {
		if module == nil {
			continue
		}
		if err := module.Close(); err != nil {
			ctx.Logger.Warn("Error closing runtime module %s: %v", module.Name(), err)
		}
	}

	for _, c := range ctx.closers {
		if c == nil {
			continue
		}

		if err := c.Close(); err != nil {
			ctx.Logger.Error("Error closing resource: %v", err)
		}
	}
}

func (ctx *Context) CurrentConfig() *config.Config {
	if ctx == nil {
		return nil
	}
	return ctx.Config
}

func (ctx *Context) RegisterRuntimeModules(modules ...RuntimeModule) {
	if ctx == nil {
		return
	}
	if ctx.runtimeModules == nil {
		ctx.runtimeModules = make(map[string]RuntimeModule)
	}

	for _, module := range modules {
		if module == nil {
			continue
		}
		ctx.runtimeModules[module.Name()] = module
	}
}

func (ctx *Context) RuntimeModule(name string) RuntimeModule {
	if ctx == nil || ctx.runtimeModules == nil {
		return nil
	}
	return ctx.runtimeModules[name]
}

func (ctx *Context) RuntimeModules() []RuntimeModule {
	if ctx == nil || len(ctx.runtimeModules) == 0 {
		return nil
	}

	modules := make([]RuntimeModule, 0, len(ctx.runtimeModules))
	for _, module := range ctx.runtimeModules {
		modules = append(modules, module)
	}
	return modules
}
