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
		return c.JSON(http.StatusNotImplemented, map[string]string{"error": "runtime settings are not configured"})
	}

	runtime, err := ctrl.App.SettingsStore.GetRuntimeSettings(c.Request().Context(), ctrl.App.BootstrapConfig)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, settingsstore.RedactedCopy(runtime))
}

func (ctrl *SettingsController) UpdateSettings(c *echo.Context) error {
	if ctrl.App == nil || ctrl.App.SettingsStore == nil {
		return c.JSON(http.StatusNotImplemented, map[string]string{"error": "runtime settings are not configured"})
	}

	var patch settingsstore.RuntimeSettingsPatch
	if err := c.Bind(&patch); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid settings patch"})
	}

	current, err := ctrl.App.SettingsStore.GetRuntimeSettings(c.Request().Context(), ctrl.App.BootstrapConfig)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// preview the merged runtime settings before persisting, so we
	// validate the effective config and reject bad runtime patches.
	next := settingsstore.ApplyPatch(current, &patch)
	effective := settingsstore.ApplyToConfig(ctrl.App.BootstrapConfig, next)
	if effective == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to build effective config"})
	}
	if err := effective.ValidateEffective(); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	if err := ctrl.App.SettingsStore.UpdateSettings(c.Request().Context(), &patch); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// Return the redacted effective runtime settings state that was just accepted.
	return c.JSON(http.StatusOK, settingsstore.RedactedCopy(next))
}
