package downloadclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/app"
)

type Service struct {
	settings app.SettingsAdmin
	resolver app.ReleaseResolver
}

func New(settings app.SettingsAdmin, resolver app.ReleaseResolver) *Service {
	return &Service{settings: settings, resolver: resolver}
}

func (s *Service) SendRelease(ctx context.Context, submission app.DownloadClientSubmission) (*app.DownloadClientResult, error) {
	if s == nil || s.settings == nil || s.resolver == nil {
		return nil, fmt.Errorf("download client integration is unavailable")
	}

	runtime, err := s.settings.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("load download client settings: %w", err)
	}
	client, err := defaultClient(runtime.DownloadClients)
	if err != nil {
		return nil, err
	}

	release, err := s.resolver.GetRelease(ctx, submission.SourceKind, submission.ReleaseID)
	if err != nil {
		return nil, fmt.Errorf("resolve release: %w", err)
	}
	reader, err := s.resolver.GetNZB(ctx, submission.SourceKind, release)
	if err != nil {
		return nil, fmt.Errorf("resolve NZB: %w", err)
	}
	defer reader.Close()

	title := ""
	if release != nil {
		title = strings.TrimSpace(release.Title)
	}
	if title == "" {
		title = strings.TrimSpace(submission.ReleaseID)
	}

	jobID, err := sendNZB(ctx, client, title+".nzb", reader)
	if err != nil {
		return nil, fmt.Errorf("send NZB to %s: %w", clientDisplayName(client), err)
	}
	return &app.DownloadClientResult{ClientID: client.ID, JobID: jobID}, nil
}

func (s *Service) Test(ctx context.Context, client app.DownloadClientRuntimeSettings) error {
	return testClient(ctx, client)
}

func defaultClient(clients []app.DownloadClientRuntimeSettings) (app.DownloadClientRuntimeSettings, error) {
	var fallback *app.DownloadClientRuntimeSettings
	for i := range clients {
		client := clients[i]
		if !client.Enabled {
			continue
		}
		if fallback == nil {
			copy := client
			fallback = &copy
		}
		if client.Default {
			return client, nil
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return app.DownloadClientRuntimeSettings{}, fmt.Errorf("no enabled SAB-compatible download client is configured")
}

func clientDisplayName(client app.DownloadClientRuntimeSettings) string {
	if name := strings.TrimSpace(client.Name); name != "" {
		return name
	}
	return strings.TrimSpace(client.ID)
}
