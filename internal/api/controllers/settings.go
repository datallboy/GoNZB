package controllers

import (
	"net/http"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/labstack/echo/v5"
)

type SettingsController struct {
	Service settingsService
}

func NewSettingsController(admin app.SettingsAdmin) *SettingsController {
	return &SettingsController{
		Service: newSettingsService(admin),
	}
}

func (ctrl *SettingsController) GetSettings(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "runtime settings are not configured")
	}

	runtime, err := ctrl.Service.Get(c.Request().Context())
	if err != nil {
		return jsonError(c, settingsErrorStatus(err), err.Error())
	}

	return c.JSON(http.StatusOK, redactedSettingsCopy(runtime))
}

func (ctrl *SettingsController) UpdateSettings(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "runtime settings are not configured")
	}

	var patch settingsPatch
	if err := decodeJSONBody(c, &patch); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	if !hasAnySettingsPatchField(&patch) {
		return jsonError(c, http.StatusBadRequest, "settings patch must include at least one field")
	}

	next, err := ctrl.Service.Update(c.Request().Context(), &patch)
	if err != nil {
		return jsonError(c, settingsErrorStatus(err), err.Error())
	}

	return c.JSON(http.StatusOK, redactedSettingsCopy(next))
}

func hasAnySettingsPatchField(patch *settingsPatch) bool {
	return patch != nil && (patch.Servers != nil ||
		patch.Indexers != nil ||
		patch.Download != nil ||
		patch.Indexing != nil ||
		patch.ArrIntegrations != nil)
}
