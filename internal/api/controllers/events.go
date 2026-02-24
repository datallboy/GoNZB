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
	App *app.Context
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
			activeItem := ctrl.App.Queue.GetActiveItem()
			activeJobs := 0
			for _, item := range ctrl.App.Queue.GetAllItems() {
				if item.Status == domain.StatusDownloading || item.Status == domain.StatusProcessing {
					activeJobs++
				}
			}

			var currentBytes int64
			var progress float64
			var activePayload *ActiveItemStats
			if activeItem != nil {
				currentBytes = activeItem.BytesWritten.Load()
				if activeItem.Release != nil && activeItem.Release.Size > 0 {
					progress = float64(currentBytes) / float64(activeItem.Release.Size) * 100
				}

				activePayload = &ActiveItemStats{
					ID:        activeItem.ID,
					ReleaseID: activeItem.ReleaseID,
					Status:    activeItem.Status,
					Bytes:     currentBytes,
				}
				if activeItem.Release != nil {
					activePayload.Title = activeItem.Release.Title
					activePayload.Size = activeItem.Release.Size
				}
			}

			bps := currentBytes - lastBytes
			if bps < 0 {
				bps = 0
			}
			lastBytes = currentBytes

			stats := EventStats{
				Bps:        bps,
				Progress:   progress,
				ActiveJobs: activeJobs,
				ActiveItem: activePayload,
			}

			data, err := json.Marshal(stats)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(rw, "data: %s\n\n", string(data))
			if err != nil {
				return err
			}

			err = rc.Flush()
			if err != nil {
				return nil
			}
		}
	}
}
