package controllers

import (
	"errors"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
)

var errDownloadEventUnavailable = errors.New("download event stream is unavailable")

type downloadEventSnapshot struct {
	CurrentBytes int64
	Progress     float64
	ActiveJobs   int
	ActiveItem   *ActiveItemStats
}

type downloadEventService interface {
	Snapshot() (*downloadEventSnapshot, error)
}

type runtimeDownloadEventService struct {
	queries app.DownloaderQueries
}

func newDownloadEventService(queries app.DownloaderQueries) downloadEventService {
	if queries == nil {
		return &runtimeDownloadEventService{}
	}

	return &runtimeDownloadEventService{
		queries: queries,
	}
}

func (s *runtimeDownloadEventService) Snapshot() (*downloadEventSnapshot, error) {
	if s == nil || s.queries == nil {
		return nil, errDownloadEventUnavailable
	}

	activeItem := s.queries.GetActiveItem()
	activeJobs := 0

	for _, item := range s.queries.ListActive() {
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

	return &downloadEventSnapshot{
		CurrentBytes: currentBytes,
		Progress:     progress,
		ActiveJobs:   activeJobs,
		ActiveItem:   activePayload,
	}, nil
}
