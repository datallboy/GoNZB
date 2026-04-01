package settingsadmin

import (
	"context"
	"errors"
	"fmt"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
)

var ErrUnavailable = errors.New("runtime settings are not configured")

type ValidationError struct {
	message string
}

func (e ValidationError) Error() string {
	return e.message
}

type DependencyProvider struct {
	SettingsStore   func() app.SettingsStore
	BootstrapConfig func() *config.Config
}

type Service struct {
	provider DependencyProvider
}

func NewService(provider DependencyProvider) *Service {
	return &Service{provider: provider}
}

func (s *Service) Get(ctx context.Context) (*app.RuntimeSettings, error) {
	store := s.provider.SettingsStore()
	if store == nil {
		return nil, ErrUnavailable
	}

	runtime, err := store.GetRuntimeSettings(ctx, s.provider.BootstrapConfig())
	if err != nil {
		return nil, fmt.Errorf("load runtime settings: %w", err)
	}

	return runtime, nil
}

func (s *Service) Update(ctx context.Context, patch *app.RuntimeSettingsPatch) (*app.RuntimeSettings, error) {
	store := s.provider.SettingsStore()
	if store == nil {
		return nil, ErrUnavailable
	}
	if patch == nil {
		return nil, ValidationError{message: "settings patch is required"}
	}

	base := s.provider.BootstrapConfig()
	current, err := store.GetRuntimeSettings(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("load runtime settings: %w", err)
	}

	next := app.ApplyPatch(current, patch)
	if err := app.ValidateArrIntegrations(next.ArrIntegrations); err != nil {
		return nil, ValidationError{message: err.Error()}
	}

	effective := app.ApplyToConfig(base, next)
	if effective == nil {
		return nil, ValidationError{message: "failed to build effective config"}
	}
	if err := effective.ValidateEffective(); err != nil {
		return nil, ValidationError{message: err.Error()}
	}

	if err := store.UpdateSettings(ctx, next); err != nil {
		return nil, fmt.Errorf("persist runtime settings: %w", err)
	}

	return next, nil
}
