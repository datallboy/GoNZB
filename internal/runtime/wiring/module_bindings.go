package wiring

import (
	aggregatormodule "github.com/datallboy/gonzb/internal/aggregator"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/downloadclient"
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

	if appCtx.SettingsAdmin != nil && appCtx.Resolver != nil {
		appCtx.DownloadClient = downloadclient.New(appCtx.SettingsAdmin, appCtx.Resolver)
	} else {
		appCtx.DownloadClient = nil
	}
}
