package commands

import (
	"context"
	"log"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
	"github.com/datallboy/gonzb/internal/infra/platform"
	"github.com/datallboy/gonzb/internal/runtime/wiring"
)

type Runner struct {
	ConfigPath string
}

func New(configPath string) *Runner {
	return &Runner{ConfigPath: configPath}
}

func (r *Runner) loadRuntimeConfig() (*config.Config, *logger.Logger) {
	cfg, err := config.Load(r.ConfigPath)
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	appLogger, err := logger.New(cfg.Log.Path, logger.ParseLevel(cfg.Log.Level), cfg.Log.IncludeStdout)
	if err != nil {
		log.Fatalf("Could not initialize logger %v\n", err)
	}

	if cfg.Log.Level == "debug" {
		appLogger.Debug("Debug logging enabled")
	}

	return cfg, appLogger
}

func (r *Runner) setupApp(ctx context.Context) *app.Context {
	cfg, appLogger := r.loadRuntimeConfig()

	if cfg.Modules.Downloader.Enabled {
		if err := platform.ValidateDependencies(); err != nil {
			log.Fatalf("Missing dependencies. Please check your Dockerfile or local installation: %v", err)
		}
	}

	appCtx, err := app.NewContext(cfg, appLogger)
	if err != nil {
		appLogger.Fatal("Failed to initialize application context %v", err)
	}

	if err := wiring.BuildInitialRuntime(appCtx); err != nil {
		appLogger.Fatal("Failed to build runtime: %v", err)
	}

	return appCtx
}
