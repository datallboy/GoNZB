package arr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/infra/logger"
	settingsstore "github.com/datallboy/gonzb/internal/store/settings"
)

type Notifier struct {
	client       *http.Client
	logger       *logger.Logger
	integrations []settingsstore.ArrIntegrationRuntimeSettings
}

type refreshCommandRequest struct {
	Name string `json:"name"`
}

func New(log *logger.Logger, integrations []settingsstore.ArrIntegrationRuntimeSettings) *Notifier {
	filtered := make([]settingsstore.ArrIntegrationRuntimeSettings, 0, len(integrations))
	for _, integration := range integrations {
		if !integration.Enabled {
			continue
		}
		filtered = append(filtered, normalizeIntegration(integration))
	}

	if len(filtered) == 0 {
		return nil
	}

	return &Notifier{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger:       log,
		integrations: filtered,
	}
}

func (n *Notifier) NotifyQueueTerminal(ctx context.Context, item *domain.QueueItem) error {
	if n == nil || item == nil {
		return nil
	}
	if item.Status != domain.StatusCompleted && item.Status != domain.StatusFailed {
		return nil
	}

	var errs []string

	for _, integration := range n.integrations {
		if !shouldNotifyIntegration(integration, item) {
			continue
		}

		if err := n.refreshMonitoredDownloads(ctx, integration); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", integration.ID, err))
			continue
		}

		if n.logger != nil {
			n.logger.Info(
				"Triggered %s monitored-download refresh for queue item %s (%s)",
				integration.Kind,
				item.ID,
				item.Status,
			)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
	}

	return nil
}

func (n *Notifier) refreshMonitoredDownloads(ctx context.Context, integration settingsstore.ArrIntegrationRuntimeSettings) error {
	endpoint := strings.TrimRight(integration.BaseURL, "/") + "/api/v3/command"

	payload, err := json.Marshal(refreshCommandRequest{
		Name: "RefreshMonitoredDownloads",
	})
	if err != nil {
		return fmt.Errorf("marshal refresh command: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", integration.APIKey)
	req.Header.Set("User-Agent", buildUserAgent(integration))

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("request refresh command: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("refresh command returned status %d", resp.StatusCode)
	}

	return nil
}

func normalizeIntegration(integration settingsstore.ArrIntegrationRuntimeSettings) settingsstore.ArrIntegrationRuntimeSettings {
	integration.ID = strings.TrimSpace(integration.ID)
	integration.Kind = strings.ToLower(strings.TrimSpace(integration.Kind))
	integration.BaseURL = strings.TrimSpace(integration.BaseURL)
	integration.APIKey = strings.TrimSpace(integration.APIKey)
	integration.ClientName = strings.TrimSpace(integration.ClientName)
	integration.Category = strings.TrimSpace(integration.Category)
	return integration
}

func shouldNotifyIntegration(integration settingsstore.ArrIntegrationRuntimeSettings, item *domain.QueueItem) bool {
	if integration.Category == "" {
		return true
	}

	var itemCategory string
	if item.Release != nil {
		itemCategory = strings.TrimSpace(item.Release.Category)
	}

	return strings.EqualFold(integration.Category, itemCategory)
}

func buildUserAgent(integration settingsstore.ArrIntegrationRuntimeSettings) string {
	clientName := "GoNZB"
	if integration.ClientName != "" {
		clientName = integration.ClientName
	}
	if integration.Kind == "" {
		return clientName
	}
	return fmt.Sprintf("%s/%s", clientName, integration.Kind)
}
