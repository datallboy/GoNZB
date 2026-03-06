package controllers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
	queuesvc "github.com/datallboy/gonzb/internal/queue"
	"github.com/labstack/echo/v5"
)

type QueueController struct {
	Service *queuesvc.Service
}

type enqueueRequest struct {
	ReleaseID string `json:"release_id"`
	Title     string `json:"title"`
}

type bulkIDsRequest struct {
	IDs []string `json:"ids"`
}

type queueItemResponse struct {
	ID          string                `json:"id"`
	ReleaseID   string                `json:"release_id"`
	Status      domain.JobStatus      `json:"status"`
	OutDir      string                `json:"out_dir"`
	Error       *string               `json:"error,omitempty"`
	CreatedAt   string                `json:"created_at,omitempty"`
	UpdatedAt   string                `json:"updated_at,omitempty"`
	StartedAt   string                `json:"started_at,omitempty"`
	CompletedAt string                `json:"completed_at,omitempty"`
	Release     *queueReleaseResponse `json:"release,omitempty"`
	Progress    queueProgressResponse `json:"progress"`
	Metrics     queueMetricsResponse  `json:"metrics"`
}

type queueReleaseResponse struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Size        int64  `json:"size"`
	Category    string `json:"category"`
	Source      string `json:"source"`
	PublishDate string `json:"publish_date,omitempty"`
}

type queueProgressResponse struct {
	BytesWritten int64 `json:"bytes_written"`
}

type queueMetricsResponse struct {
	DownloadedBytes    int64 `json:"downloaded_bytes"`
	AvgBps             int64 `json:"avg_bps"`
	DownloadSeconds    int64 `json:"download_seconds"`
	PostProcessSeconds int64 `json:"postprocess_seconds"`
}

type queueFileResponse struct {
	ID       int64    `json:"id"`
	FileName string   `json:"filename"`
	Size     int64    `json:"size"`
	Index    int      `json:"index"`
	IsPars   bool     `json:"is_pars"`
	Subject  string   `json:"subject"`
	Date     int64    `json:"date"`
	Groups   []string `json:"groups"`
}

type queueEventResponse struct {
	ID        int64  `json:"id"`
	Stage     string `json:"stage"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	MetaJSON  string `json:"meta_json"`
	CreatedAt string `json:"created_at"`
}

func (ctrl *QueueController) ListActive(c *echo.Context) error {
	items := ctrl.Service.ListActive()
	resp := mapQueueItems(items)
	return c.JSON(http.StatusOK, map[string]any{
		"items": resp,
		"count": len(resp),
	})
}

func (ctrl *QueueController) ListHistory(c *echo.Context) error {
	limit := parseIntDefault(c.QueryParam("limit"), 50)
	offset := parseIntDefault(c.QueryParam("offset"), 0)
	status := c.QueryParam("status")

	items, total, err := ctrl.Service.ListHistory(c.Request().Context(), status, limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	resp := mapQueueItems(items)
	return c.JSON(http.StatusOK, map[string]any{
		"items":    resp,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"status":   status,
		"has_more": offset+len(resp) < total,
	})
}

func (ctrl *QueueController) GetItem(c *echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing queue item id"})
	}

	item, err := ctrl.Service.GetItem(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if item == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "queue item not found"})
	}

	return c.JSON(http.StatusOK, mapQueueItem(item))
}

func (ctrl *QueueController) Add(c *echo.Context) error {
	contentType := c.Request().Header.Get(echo.HeaderContentType)
	if strings.HasPrefix(strings.ToLower(contentType), "multipart/form-data") {
		fileHeader, err := c.FormFile("nzb")
		if err != nil || fileHeader == nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing nzb file in multipart form"})
		}
		file, openErr := fileHeader.Open()
		if openErr != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to open uploaded nzb"})
		}
		defer file.Close()

		item, queueErr := ctrl.Service.EnqueueNZB(c.Request().Context(), fileHeader.Filename, file)
		if queueErr != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": queueErr.Error()})
		}

		return c.JSON(http.StatusCreated, mapQueueItem(item))
	}

	req := enqueueRequest{}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	item, err := ctrl.Service.EnqueueByReleaseID(c.Request().Context(), req.ReleaseID, req.Title)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, mapQueueItem(item))
}

func (ctrl *QueueController) GetItemFiles(c *echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing queue item id"})
	}

	files, err := ctrl.Service.GetItemFiles(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if files == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "queue item not found"})
	}

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

	return c.JSON(http.StatusOK, map[string]any{
		"items": resp,
		"count": len(resp),
	})
}

func (ctrl *QueueController) GetItemEvents(c *echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing queue item id"})
	}

	events, err := ctrl.Service.GetItemEvents(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if events == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "queue item not found"})
	}

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

	return c.JSON(http.StatusOK, map[string]any{
		"items": resp,
		"count": len(resp),
	})
}

func (ctrl *QueueController) Cancel(c *echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing queue item id"})
	}

	ok := ctrl.Service.Cancel(id)
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "unable to cancel queue item"})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"ok": true,
		"id": id,
	})
}

func (ctrl *QueueController) CancelMany(c *echo.Context) error {
	req := bulkIDsRequest{}
	if err := c.Bind(&req); err != nil || len(req.IDs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid ids payload"})
	}

	cancelled := ctrl.Service.CancelMany(req.IDs)
	return c.JSON(http.StatusOK, map[string]any{
		"ok":        true,
		"requested": len(req.IDs),
		"cancelled": cancelled,
	})
}

func (ctrl *QueueController) DeleteMany(c *echo.Context) error {
	req := bulkIDsRequest{}
	if err := c.Bind(&req); err != nil || len(req.IDs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid ids payload"})
	}

	deleted, err := ctrl.Service.DeleteMany(c.Request().Context(), req.IDs)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"ok":        true,
		"requested": len(req.IDs),
		"deleted":   deleted,
	})
}

func (ctrl *QueueController) ClearHistory(c *echo.Context) error {
	deleted, err := ctrl.Service.ClearHistory(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"ok":      true,
		"deleted": deleted,
	})
}

func parseIntDefault(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

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
