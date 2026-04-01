package controllers

import (
	"net/http"
	"strings"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/labstack/echo/v5"
)

type QueueController struct {
	Commands app.DownloaderCommands
	Queries  app.DownloaderQueries
}

func NewQueueController(module app.DownloaderModule) *QueueController {
	if module == nil {
		return &QueueController{}
	}

	return &QueueController{
		Commands: module.Commands(),
		Queries:  module.Queries(),
	}
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
	if ctrl.Queries == nil {
		return jsonError(c, http.StatusServiceUnavailable, "downloader queue service is unavailable")
	}

	limit, offset, err := parsePaginationParams(c, defaultPageLimit, maxPageLimit)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	items := mapQueueItems(ctrl.Queries.ListActive())
	total := len(items)

	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}

	page := items[offset:end]
	return c.JSON(http.StatusOK, map[string]any{
		"items":    page,
		"count":    len(page),
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": end < total,
	})
}

func (ctrl *QueueController) ListHistory(c *echo.Context) error {
	if ctrl.Queries == nil {
		return jsonError(c, http.StatusServiceUnavailable, "downloader queue service is unavailable")
	}

	limit, offset, err := parsePaginationParams(c, defaultPageLimit, maxPageLimit)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	status := queryParamTrimmed(c, "status")
	items, total, err := ctrl.Queries.ListHistory(c.Request().Context(), status, limit, offset)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}

	resp := mapQueueItems(items)
	return c.JSON(http.StatusOK, map[string]any{
		"items":    resp,
		"count":    len(resp),
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"status":   status,
		"has_more": offset+len(resp) < total,
	})
}

func (ctrl *QueueController) GetItem(c *echo.Context) error {
	if ctrl.Queries == nil {
		return jsonError(c, http.StatusServiceUnavailable, "downloader queue service is unavailable")
	}

	id := pathParamTrimmed(c, "id")
	if id == "" {
		return jsonError(c, http.StatusBadRequest, "missing queue item id")
	}

	item, err := ctrl.Queries.GetItem(c.Request().Context(), id)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if item == nil {
		return jsonError(c, http.StatusNotFound, "queue item not found")
	}

	return c.JSON(http.StatusOK, mapQueueItem(item))
}

func (ctrl *QueueController) Add(c *echo.Context) error {
	if ctrl.Commands == nil {
		return jsonError(c, http.StatusServiceUnavailable, "downloader queue service is unavailable")
	}

	contentType := normalizeLowerTrimmed(c.Request().Header.Get(echo.HeaderContentType))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		fileHeader, err := c.FormFile("nzb")
		if err != nil || fileHeader == nil {
			return jsonError(c, http.StatusBadRequest, "missing nzb file in multipart form")
		}

		file, openErr := fileHeader.Open()
		if openErr != nil {
			return jsonError(c, http.StatusBadRequest, "failed to open uploaded nzb")
		}
		defer file.Close()

		filename := sanitizeUploadFilename(fileHeader.Filename, "manual.nzb")
		item, queueErr := ctrl.Commands.EnqueueNZB(c.Request().Context(), filename, file)
		if queueErr != nil {
			return jsonError(c, http.StatusBadRequest, queueErr.Error())
		}

		return c.JSON(http.StatusCreated, mapQueueItem(item))
	}

	var req enqueueRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	req.ReleaseID = normalizeTrimmed(req.ReleaseID)
	req.Title = normalizeTrimmed(req.Title)

	item, err := ctrl.Commands.EnqueueByReleaseID(c.Request().Context(), req.ReleaseID, req.Title)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	return c.JSON(http.StatusCreated, mapQueueItem(item))
}

func (ctrl *QueueController) GetItemFiles(c *echo.Context) error {
	if ctrl.Queries == nil {
		return jsonError(c, http.StatusServiceUnavailable, "downloader queue service is unavailable")
	}

	id := pathParamTrimmed(c, "id")
	if id == "" {
		return jsonError(c, http.StatusBadRequest, "missing queue item id")
	}

	files, err := ctrl.Queries.GetItemFiles(c.Request().Context(), id)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if files == nil {
		return jsonError(c, http.StatusNotFound, "queue item not found")
	}

	resp := mapQueueFileResponses(files)
	return c.JSON(http.StatusOK, map[string]any{
		"items": resp,
		"count": len(resp),
	})
}

func (ctrl *QueueController) GetItemEvents(c *echo.Context) error {
	if ctrl.Queries == nil {
		return jsonError(c, http.StatusServiceUnavailable, "downloader queue service is unavailable")
	}

	id := pathParamTrimmed(c, "id")
	if id == "" {
		return jsonError(c, http.StatusBadRequest, "missing queue item id")
	}

	events, err := ctrl.Queries.GetItemEvents(c.Request().Context(), id)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if events == nil {
		return jsonError(c, http.StatusNotFound, "queue item not found")
	}

	resp := mapQueueEventResponses(events)
	return c.JSON(http.StatusOK, map[string]any{
		"items": resp,
		"count": len(resp),
	})
}

func (ctrl *QueueController) Cancel(c *echo.Context) error {
	if ctrl.Commands == nil {
		return jsonError(c, http.StatusServiceUnavailable, "downloader queue service is unavailable")
	}

	id := pathParamTrimmed(c, "id")
	if id == "" {
		return jsonError(c, http.StatusBadRequest, "missing queue item id")
	}

	ok := ctrl.Commands.Cancel(id)
	if !ok {
		return jsonError(c, http.StatusNotFound, "unable to cancel queue item")
	}

	return c.JSON(http.StatusOK, map[string]any{
		"ok": true,
		"id": id,
	})
}

func (ctrl *QueueController) CancelMany(c *echo.Context) error {
	if ctrl.Commands == nil {
		return jsonError(c, http.StatusServiceUnavailable, "downloader queue service is unavailable")
	}

	var req bulkIDsRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	req.IDs = normalizeIDs(req.IDs)
	if len(req.IDs) == 0 {
		return jsonError(c, http.StatusBadRequest, "ids must contain at least one queue item id")
	}

	cancelled := ctrl.Commands.CancelMany(req.IDs)
	return c.JSON(http.StatusOK, map[string]any{
		"ok":        true,
		"requested": len(req.IDs),
		"cancelled": cancelled,
	})
}

func (ctrl *QueueController) DeleteMany(c *echo.Context) error {
	if ctrl.Commands == nil {
		return jsonError(c, http.StatusServiceUnavailable, "downloader queue service is unavailable")
	}

	var req bulkIDsRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	req.IDs = normalizeIDs(req.IDs)
	if len(req.IDs) == 0 {
		return jsonError(c, http.StatusBadRequest, "ids must contain at least one queue item id")
	}

	deleted, err := ctrl.Commands.DeleteMany(c.Request().Context(), req.IDs)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]any{
		"ok":        true,
		"requested": len(req.IDs),
		"deleted":   deleted,
	})
}

func (ctrl *QueueController) ClearHistory(c *echo.Context) error {
	if ctrl.Commands == nil {
		return jsonError(c, http.StatusServiceUnavailable, "downloader queue service is unavailable")
	}

	deleted, err := ctrl.Commands.ClearHistory(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]any{
		"ok":      true,
		"deleted": deleted,
	})
}

func normalizeIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))

	for _, id := range ids {
		id = normalizeTrimmed(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}

	return out
}
