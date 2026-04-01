package controllers

import (
	"context"
	"errors"
	"net/http"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/settingsadmin"
)

type settingsView = app.RuntimeSettings
type settingsPatch = app.RuntimeSettingsPatch

type settingsService interface {
	Get(ctx context.Context) (*settingsView, error)
	Update(ctx context.Context, patch *settingsPatch) (*settingsView, error)
}

type runtimeSettingsService struct {
	admin app.SettingsAdmin
}

func newSettingsService(admin app.SettingsAdmin) settingsService {
	return &runtimeSettingsService{admin: admin}
}

func (s *runtimeSettingsService) Get(ctx context.Context) (*settingsView, error) {
	if s == nil || s.admin == nil {
		return nil, settingsadmin.ErrUnavailable
	}
	return s.admin.Get(ctx)
}

func (s *runtimeSettingsService) Update(ctx context.Context, patch *settingsPatch) (*settingsView, error) {
	if s == nil || s.admin == nil {
		return nil, settingsadmin.ErrUnavailable
	}
	return s.admin.Update(ctx, patch)
}

func redactedSettingsCopy(runtime *settingsView) *settingsView {
	return app.RedactedCopy(runtime)
}

func settingsErrorStatus(err error) int {
	switch {
	case errors.Is(err, settingsadmin.ErrUnavailable):
		return http.StatusServiceUnavailable
	default:
		var validationErr settingsadmin.ValidationError
		if errors.As(err, &validationErr) {
			return http.StatusBadRequest
		}
		return http.StatusInternalServerError
	}
}
