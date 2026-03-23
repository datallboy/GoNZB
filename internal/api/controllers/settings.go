package controllers

import (
	"net/http"

	"github.com/datallboy/gonzb/internal/app"
	settingsstore "github.com/datallboy/gonzb/internal/store/settings"
	"github.com/labstack/echo/v5"
)

type SettingsController struct {
	App *app.Context
}

func (ctrl *SettingsController) GetSettings(c *echo.Context) error {
	if ctrl.App == nil || ctrl.App.SettingsStore == nil {
		return jsonError(c, http.StatusServiceUnavailable, "runtime settings are not configured")
	}

	runtime, err := ctrl.App.SettingsStore.GetRuntimeSettings(c.Request().Context(), ctrl.App.BootstrapConfig)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, settingsstore.RedactedCopy(runtime))
}

func (ctrl *SettingsController) UpdateSettings(c *echo.Context) error {
	if ctrl.App == nil || ctrl.App.SettingsStore == nil {
		return jsonError(c, http.StatusServiceUnavailable, "runtime settings are not configured")
	}

	var patch settingsstore.RuntimeSettingsPatch
	if err := decodeJSONBody(c, &patch); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	if !hasAnySettingsPatchField(&patch) {
		return jsonError(c, http.StatusBadRequest, "settings patch must include at least one field")
	}

	current, err := ctrl.App.SettingsStore.GetRuntimeSettings(c.Request().Context(), ctrl.App.BootstrapConfig)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}

	next := settingsstore.ApplyPatch(current, &patch)
	if err := settingsstore.ValidateArrIntegrations(next.ArrIntegrations); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	effective := settingsstore.ApplyToConfig(ctrl.App.BootstrapConfig, next)
	if effective == nil {
		return jsonError(c, http.StatusBadRequest, "failed to build effective config")
	}
	if err := effective.ValidateEffective(); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	if err := ctrl.App.SettingsStore.UpdateSettings(c.Request().Context(), next); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, settingsstore.RedactedCopy(next))
}

func hasAnySettingsPatchField(patch *settingsstore.RuntimeSettingsPatch) bool {
	return patch != nil && (patch.Servers != nil ||
		patch.Indexers != nil ||
		patch.Download != nil ||
		patch.Indexing != nil ||
		patch.ArrIntegrations != nil)
}
