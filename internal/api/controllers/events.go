package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/labstack/echo/v5"
)

// Event message structure for UI
type DownloadEvent struct {
	Service downloadEventService
}

func NewDownloadEvent(queries app.DownloaderQueries) *DownloadEvent {
	return &DownloadEvent{
		Service: newDownloadEventService(queries),
	}
}

type EventStats struct {
	Bps        int64            `json:"bps"`
	Progress   float64          `json:"progress"`
	ActiveJobs int              `json:"active_jobs"`
	ActiveItem *ActiveItemStats `json:"active_item,omitempty"`
}

type ActiveItemStats struct {
	ID        string           `json:"id"`
	ReleaseID string           `json:"release_id"`
	Title     string           `json:"title"`
	Status    domain.JobStatus `json:"status"`
	Size      int64            `json:"size"`
	Bytes     int64            `json:"bytes"`
}

func (ctrl *DownloadEvent) HandleEvents(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}

	rw := c.Response()
	rc := http.NewResponseController(rw)

	rw.Header().Set(echo.HeaderContentType, "text/event-stream")
	rw.Header().Set(echo.HeaderCacheControl, "no-cache")
	rw.Header().Set(echo.HeaderConnection, "keep-alive")

	if _, err := fmt.Fprintf(rw, "retry: 3000\n\n"); err != nil {
		return err
	}
	if err := rc.Flush(); err != nil {
		return nil
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastBytes int64

	for {
		select {
		case <-c.Request().Context().Done():
			return nil

		case <-ticker.C:
			snapshot, err := ctrl.Service.Snapshot()
			if err != nil {
				return err
			}

			bps := snapshot.CurrentBytes - lastBytes
			if bps < 0 {
				bps = 0
			}
			lastBytes = snapshot.CurrentBytes

			stats := EventStats{
				Bps:        bps,
				Progress:   snapshot.Progress,
				ActiveJobs: snapshot.ActiveJobs,
				ActiveItem: snapshot.ActiveItem,
			}

			data, err := json.Marshal(stats)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(rw, "data: %s\n\n", string(data)); err != nil {
				return err
			}

			if err := rc.Flush(); err != nil {
				return nil
			}
		}
	}
}
