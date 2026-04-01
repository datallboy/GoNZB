package wiring

import (
	"context"
	"strings"

	"github.com/datallboy/gonzb/internal/app"
	arrintegration "github.com/datallboy/gonzb/internal/integrations/arr"
)

func BuildArrNotifier(ctx context.Context, appCtx *app.Context) error {
	if appCtx == nil || appCtx.SettingsStore == nil {
		return nil
	}

	runtime, err := appCtx.SettingsStore.GetRuntimeSettings(ctx, appCtx.BootstrapConfig)
	if err != nil {
		return err
	}

	integrations := make([]app.ArrIntegrationRuntimeSettings, 0, len(runtime.ArrIntegrations))
	for _, integration := range runtime.ArrIntegrations {
		if !integration.Enabled {
			continue
		}
		if !isValidArrIntegration(integration) {
			appCtx.Logger.Warn("Skipping invalid Arr integration settings entry: id=%s kind=%s", integration.ID, integration.Kind)
			continue
		}
		integrations = append(integrations, integration)
	}

	appCtx.ArrNotifier = arrintegration.New(appCtx.Logger, integrations)
	return nil
}

func isValidArrIntegration(integration app.ArrIntegrationRuntimeSettings) bool {
	kind := strings.ToLower(strings.TrimSpace(integration.Kind))
	if kind != "radarr" && kind != "sonarr" {
		return false
	}
	if strings.TrimSpace(integration.ID) == "" {
		return false
	}
	if strings.TrimSpace(integration.BaseURL) == "" {
		return false
	}
	if strings.TrimSpace(integration.APIKey) == "" {
		return false
	}
	return true
}
