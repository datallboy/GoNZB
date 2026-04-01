package settings

import (
	"encoding/json"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
)

// runtime-editable settings model for Milestone 8.X chunk 1.
// Bootstrap-only fields stay in config.yaml/env and are not represented here.
type RuntimeSettings = app.RuntimeSettings
type RuntimeSettingsPatch = app.RuntimeSettingsPatch
type ServerRuntimeSettings = app.ServerRuntimeSettings
type IndexerRuntimeSettings = app.IndexerRuntimeSettings
type DownloadRuntimeSettings = app.DownloadRuntimeSettings
type IndexingRuntimeSettings = app.IndexingRuntimeSettings
type ArrIntegrationRuntimeSettings = app.ArrIntegrationRuntimeSettings

// derive editable runtime state from current effective config.
func FromConfig(cfg *config.Config) *RuntimeSettings {
	return app.FromConfig(cfg)
}

// apply runtime-editable settings on top of bootstrap config.
func ApplyToConfig(base *config.Config, runtime *RuntimeSettings) *config.Config {
	return app.ApplyToConfig(base, runtime)
}

// exported patch helper for admin API preview/validation path.
func ApplyPatch(current *RuntimeSettings, patch *RuntimeSettingsPatch) *RuntimeSettings {
	return app.ApplyPatch(current, patch)
}

// explicit clone used when persisting a validated full snapshot.
func CloneRuntimeSettings(in *RuntimeSettings) *RuntimeSettings {
	return app.CloneRuntimeSettings(in)
}

// redact runtime secrets before returning settings through API.
func RedactedCopy(in *RuntimeSettings) *RuntimeSettings {
	return app.RedactedCopy(in)
}

func ValidateArrIntegrations(integrations []ArrIntegrationRuntimeSettings) error {
	return app.ValidateArrIntegrations(integrations)
}

func encodeRuntimeSettings(v *RuntimeSettings) ([]byte, error) {
	if v == nil {
		v = &RuntimeSettings{}
	}
	return json.Marshal(v)
}
