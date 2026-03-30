package controllers

import (
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

func mapQueueItems(items []*domain.QueueItem) []queueItemResponse {
	resp := make([]queueItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, mapQueueItem(item))
	}
	return resp
}

func mapQueueItem(item *domain.QueueItem) queueItemResponse {
	resp := queueItemResponse{
		ID:        item.ID,
		ReleaseID: item.ReleaseID,
		Status:    item.Status,
		OutDir:    item.OutDir,
		Error:     item.Error,
		Progress: queueProgressResponse{
			BytesWritten: item.GetBytes(),
		},
		Metrics: queueMetricsResponse{
			DownloadedBytes:    item.DownloadedBytes,
			AvgBps:             item.AvgBps,
			DownloadSeconds:    item.DownloadSeconds,
			PostProcessSeconds: item.PostProcessSeconds,
		},
	}

	if !item.CreatedAt.IsZero() {
		resp.CreatedAt = item.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !item.UpdatedAt.IsZero() {
		resp.UpdatedAt = item.UpdatedAt.UTC().Format(time.RFC3339)
	}
	if !item.StartedAt.IsZero() {
		resp.StartedAt = item.StartedAt.UTC().Format(time.RFC3339)
	}
	if !item.CompletedAt.IsZero() {
		resp.CompletedAt = item.CompletedAt.UTC().Format(time.RFC3339)
	}

	if item.Release != nil {
		rel := &queueReleaseResponse{
			ID:       item.Release.ID,
			Title:    item.Release.Title,
			Size:     item.Release.Size,
			Category: item.Release.Category,
			Source:   item.Release.Source,
		}
		if !item.Release.PublishDate.IsZero() {
			rel.PublishDate = item.Release.PublishDate.UTC().Format(time.RFC3339)
		}
		resp.Release = rel
	}

	return resp
}

func mapQueueFileResponses(files []*domain.DownloadFile) []queueFileResponse {
	resp := make([]queueFileResponse, 0, len(files))
	for _, f := range files {
		resp = append(resp, queueFileResponse{
			ID:       f.ID,
			FileName: f.FileName,
			Size:     f.Size,
			Index:    f.Index,
			IsPars:   f.IsPars,
			Subject:  f.Subject,
			Date:     f.Date,
			Groups:   f.Groups,
		})
	}
	return resp
}

func mapQueueEventResponses(events []*domain.QueueItemEvent) []queueEventResponse {
	resp := make([]queueEventResponse, 0, len(events))
	for _, ev := range events {
		resp = append(resp, queueEventResponse{
			ID:        ev.ID,
			Stage:     ev.Stage,
			Status:    ev.Status,
			Message:   ev.Message,
			MetaJSON:  ev.MetaJSON,
			CreatedAt: ev.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return resp
}
