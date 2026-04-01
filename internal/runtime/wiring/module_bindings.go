package wiring

import (
	aggregatormodule "github.com/datallboy/gonzb/internal/aggregator"
	"github.com/datallboy/gonzb/internal/app"
	downloadermodule "github.com/datallboy/gonzb/internal/downloader"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/settingsadmin"
)

func BindApplicationModules(appCtx *app.Context) {
	if appCtx == nil {
		return
	}

	if appCtx.Config != nil && appCtx.Config.Modules.Aggregator.Enabled {
		appCtx.AggregatorModule = aggregatormodule.NewModule(aggregatormodule.DependencyProvider{
			Aggregator: func() app.IndexerAggregator { return appCtx.Aggregator },
			BlobStore:  func() app.BlobStore { return appCtx.BlobStore },
			Logger: func() aggregatormodule.Logger {
				return appCtx.Logger
			},
		})
	} else {
		appCtx.AggregatorModule = nil
	}

	if appCtx.SettingsStore != nil {
		appCtx.SettingsAdmin = settingsadmin.NewService(settingsadmin.DependencyProvider{
			SettingsStore:   func() app.SettingsStore { return appCtx.SettingsStore },
			BootstrapConfig: func() *config.Config { return appCtx.BootstrapConfig },
		})
	} else {
		appCtx.SettingsAdmin = nil
	}

	if appCtx.Queue != nil {
		appCtx.DownloaderModule = downloadermodule.NewModule(downloadermodule.DependencyProvider{
			Queue:          func() app.QueueManager { return appCtx.Queue },
			Resolver:       func() app.ReleaseResolver { return appCtx.Resolver },
			BlobStore:      func() app.BlobStore { return appCtx.BlobStore },
			JobStore:       func() app.JobStore { return appCtx.JobStore },
			QueueFileStore: func() app.QueueFileStore { return appCtx.QueueFileStore },
		})
	} else {
		appCtx.DownloaderModule = nil
	}
}
