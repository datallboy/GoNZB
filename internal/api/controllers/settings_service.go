package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/datallboy/gonzb/internal/app"
)

var errSettingsUnavailable = errors.New("runtime settings are not configured")

type settingsValidationError struct {
	message string
}

func (e settingsValidationError) Error() string {
	return e.message
}

type settingsView = app.RuntimeSettings
type settingsPatch = app.RuntimeSettingsPatch

type settingsService interface {
	Get(ctx context.Context) (*settingsView, error)
	Update(ctx context.Context, patch *settingsPatch) (*settingsView, error)
}

type runtimeSettingsService struct {
	app *app.Context
}

func newSettingsService(appCtx *app.Context) settingsService {
	return &runtimeSettingsService{app: appCtx}
}

func (s *runtimeSettingsService) Get(ctx context.Context) (*settingsView, error) {
	if s == nil || s.app == nil || s.app.SettingsStore == nil {
		return nil, errSettingsUnavailable
	}

	runtime, err := s.app.SettingsStore.GetRuntimeSettings(ctx, s.app.BootstrapConfig)
	if err != nil {
		return nil, fmt.Errorf("load runtime settings: %w", err)
	}

	return runtime, nil
}

func (s *runtimeSettingsService) Update(ctx context.Context, patch *settingsPatch) (*settingsView, error) {
	if s == nil || s.app == nil || s.app.SettingsStore == nil {
		return nil, errSettingsUnavailable
	}
	if patch == nil {
		return nil, settingsValidationError{message: "settings patch is required"}
	}

	current, err := s.app.SettingsStore.GetRuntimeSettings(ctx, s.app.BootstrapConfig)
	if err != nil {
		return nil, fmt.Errorf("load runtime settings: %w", err)
	}

	next := app.ApplyPatch(current, patch)
	if err := app.ValidateArrIntegrations(next.ArrIntegrations); err != nil {
		return nil, settingsValidationError{message: err.Error()}
	}

	effective := app.ApplyToConfig(s.app.BootstrapConfig, next)
	if effective == nil {
		return nil, settingsValidationError{message: "failed to build effective config"}
	}
	if err := effective.ValidateEffective(); err != nil {
		return nil, settingsValidationError{message: err.Error()}
	}

	if err := s.app.SettingsStore.UpdateSettings(ctx, next); err != nil {
		return nil, fmt.Errorf("persist runtime settings: %w", err)
	}

	return next, nil
}

func redactedSettingsCopy(runtime *settingsView) *settingsView {
	return app.RedactedCopy(runtime)
}

func settingsErrorStatus(err error) int {
	switch {
	case errors.Is(err, errSettingsUnavailable):
		return http.StatusServiceUnavailable
	default:
		var validationErr settingsValidationError
		if errors.As(err, &validationErr) {
			return http.StatusBadRequest
		}
		return http.StatusInternalServerError
	}
}
