package controllers

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	queuesvc "github.com/datallboy/gonzb/internal/queue"
	"github.com/labstack/echo/v5"
)

var compatFetchClient = &http.Client{
	Timeout: 30 * time.Second,
}

type SABController struct {
	App     *app.Context
	Service *queuesvc.Service
}

func (ctrl *SABController) Handle(c *echo.Context) error {
	req, err := bindSABRequest(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, sabStatusResponse{
			Status: false,
			Error:  err.Error(),
		})
	}

	if req.Output != "json" {
		return c.JSON(http.StatusBadRequest, sabStatusResponse{
			Status: false,
			Error:  "only json output is currently supported",
		})
	}

	switch req.Mode {
	case "queue":
		return ctrl.handleQueue(c, req)
	case "history":
		return ctrl.handleHistory(c, req)
	case "addurl":
		return ctrl.handleAddURL(c, req)
	case "addfile":
		return ctrl.handleAddFile(c, req)
	case "delete":
		return ctrl.handleDelete(c, req)
	case "pause":
		return ctrl.handlePause(c, req)
	case "resume":
		return ctrl.handleResume(c, req)
	case "get_files":
		return ctrl.handleGetFiles(c, req)
	case "get_config":
		return ctrl.handleGetConfig(c, req)
	case "get_cats":
		return ctrl.handleGetCategories(c, req)
	case "version":
		return ctrl.handleVersion(c, req)
	case "fullstatus", "status":
		return ctrl.handleFullStatus(c, req)
	default:
		return c.JSON(http.StatusBadRequest, sabStatusResponse{
			Status: false,
			Error:  "unsupported or missing SAB mode",
		})
	}
}

func bindSABRequest(c *echo.Context) (sabAPIRequest, error) {
	var req sabAPIRequest

	if err := bindQueryAndBody(c, &req); err != nil {
		return req, err
	}

	req.normalize()
	return req, nil
}

func (ctrl *SABController) handleQueue(c *echo.Context, req sabAPIRequest) error {
	queueData := ctrl.buildQueueData(req)

	return c.JSON(http.StatusOK, sabQueueResponse{
		Queue: queueData,
	})
}

func (ctrl *SABController) handleHistory(c *echo.Context, req sabAPIRequest) error {
	if strings.EqualFold(req.Name, "delete") {
		return ctrl.handleHistoryDelete(c, req)
	}

	limit := parseIntDefault(req.Limit, 50)
	start := parseIntDefault(req.Start, 0)

	items, _, err := ctrl.Service.ListHistory(c.Request().Context(), "", limit, start)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, sabHistoryResponse{
			Status: false,
			Error:  err.Error(),
			History: sabHistoryData{
				Slots: []sabHistorySlot{},
			},
		})
	}

	slots := make([]sabHistorySlot, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		if item.Status != domain.StatusCompleted && item.Status != domain.StatusFailed {
			continue
		}
		if !matchesSABSearch(item, req.Search) {
			continue
		}
		if !matchesSABCategory(queueItemCategory(item), req.Category) {
			continue
		}

		slot := sabHistorySlot{
			NZOID:     item.ID,
			Name:      queueItemDisplayName(item),
			Status:    mapSABHistoryStatus(item.Status),
			Category:  queueItemCategory(item),
			SizeBytes: queueItemSize(item),
		}

		if item.Error != nil {
			slot.FailMessage = *item.Error
		}
		if !item.CompletedAt.IsZero() {
			slot.Completed = item.CompletedAt.UTC().Format(time.RFC3339)
		}
		if item.OutDir != "" {
			slot.Storage = item.OutDir
		}

		slots = append(slots, slot)
	}

	return c.JSON(http.StatusOK, sabHistoryResponse{
		Status: true,
		History: sabHistoryData{
			NoOfSlots: len(slots),
			Slots:     slots,
		},
	})
}

func (ctrl *SABController) handleHistoryDelete(c *echo.Context, req sabAPIRequest) error {
	ctx := c.Request().Context()

	target := normalizeTrimmed(req.Value)
	if target == "" {
		target = normalizeTrimmed(req.NZOID)
	}
	if target == "" {
		target = normalizeTrimmed(req.Name)
	}

	// SAB-style "history delete" can target a specific item or clear archived history.
	if target == "" || strings.EqualFold(target, "all") {
		_, err := ctrl.Service.ClearHistory(ctx)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, sabStatusResponse{
				Status: false,
				Error:  err.Error(),
			})
		}

		return c.JSON(http.StatusOK, sabStatusResponse{
			Status: true,
		})
	}

	deleted, err := ctrl.Service.DeleteMany(ctx, []string{target})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, sabStatusResponse{
			Status: false,
			Error:  err.Error(),
		})
	}
	if deleted == 0 {
		return c.JSON(http.StatusNotFound, sabStatusResponse{
			Status: false,
			Error:  "history item not found",
		})
	}

	return c.JSON(http.StatusOK, sabStatusResponse{
		Status: true,
	})
}

func (ctrl *SABController) handleAddURL(c *echo.Context, req sabAPIRequest) error {
	nzbURL := req.addURL()
	if nzbURL == "" {
		return c.JSON(http.StatusBadRequest, sabAddResponse{
			Status: false,
			Error:  "missing nzb url",
		})
	}

	filename := req.NZBName
	if filename == "" {
		if parsed, err := url.Parse(nzbURL); err == nil {
			base := path.Base(parsed.Path)
			if base != "" && base != "." && base != "/" {
				filename = base
			}
		}
	}
	if filename == "" {
		filename = "remote.nzb"
	}

	httpReq, err := http.NewRequestWithContext(c.Request().Context(), http.MethodGet, nzbURL, nil)
	if err != nil {
		return c.JSON(http.StatusBadRequest, sabAddResponse{
			Status: false,
			Error:  "invalid nzb url",
		})
	}

	resp, err := compatFetchClient.Do(httpReq)
	if err != nil {
		return c.JSON(http.StatusBadGateway, sabAddResponse{
			Status: false,
			Error:  fmt.Sprintf("failed to fetch nzb url: %v", err),
		})
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.JSON(http.StatusBadGateway, sabAddResponse{
			Status: false,
			Error:  fmt.Sprintf("upstream nzb url returned %d", resp.StatusCode),
		})
	}

	item, err := ctrl.Service.EnqueueNZBWithCategory(
		c.Request().Context(),
		filename,
		req.Category,
		resp.Body,
	)
	if err != nil {
		return c.JSON(http.StatusBadRequest, sabAddResponse{
			Status: false,
			Error:  err.Error(),
		})
	}

	return c.JSON(http.StatusOK, sabAddResponse{
		Status: true,
		NZOIDs: []string{item.ID},
	})
}

func (ctrl *SABController) handleAddFile(c *echo.Context, req sabAPIRequest) error {
	fileHeader, err := firstUploadedFile(c, "nzbfile", "name", "file", "nzb")
	if err != nil {
		return c.JSON(http.StatusBadRequest, sabAddResponse{
			Status: false,
			Error:  err.Error(),
		})
	}

	file, err := fileHeader.Open()
	if err != nil {
		return c.JSON(http.StatusBadRequest, sabAddResponse{
			Status: false,
			Error:  "failed to open uploaded nzb",
		})
	}
	defer file.Close()

	filename := req.NZBName
	if filename == "" {
		filename = fileHeader.Filename
	}
	if filename == "" {
		filename = "upload.nzb"
	}

	item, err := ctrl.Service.EnqueueNZBWithCategory(
		c.Request().Context(),
		filename,
		req.Category,
		file,
	)
	if err != nil {
		return c.JSON(http.StatusBadRequest, sabAddResponse{
			Status: false,
			Error:  err.Error(),
		})
	}

	return c.JSON(http.StatusOK, sabAddResponse{
		Status: true,
		NZOIDs: []string{item.ID},
	})
}

func (ctrl *SABController) handleDelete(c *echo.Context, req sabAPIRequest) error {
	id := req.deleteTarget()
	if id == "" {
		return c.JSON(http.StatusBadRequest, sabStatusResponse{
			Status: false,
			Error:  "missing nzo_id/value for delete",
		})
	}

	ctx := c.Request().Context()

	if ctrl.Service.Cancel(id) {
		return c.JSON(http.StatusOK, sabStatusResponse{Status: true})
	}

	deleted, err := ctrl.App.JobStore.DeleteQueueItems(ctx, []string{id})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, sabStatusResponse{
			Status: false,
			Error:  err.Error(),
		})
	}
	if deleted > 0 {
		return c.JSON(http.StatusOK, sabStatusResponse{Status: true})
	}

	return c.JSON(http.StatusNotFound, sabStatusResponse{
		Status: false,
		Error:  "queue item not found",
	})
}

func (ctrl *SABController) handlePause(c *echo.Context, req sabAPIRequest) error {
	if req.NZOID != "" || req.Value != "" || req.Name != "" {
		return c.JSON(http.StatusBadRequest, sabStatusResponse{
			Status: false,
			Error:  "per-job pause is not supported; use global pause",
		})
	}

	if !ctrl.Service.Pause() {
		return c.JSON(http.StatusInternalServerError, sabStatusResponse{
			Status: false,
			Error:  "failed to pause downloader queue",
		})
	}

	return c.JSON(http.StatusOK, sabStatusResponse{
		Status: true,
	})
}

func (ctrl *SABController) handleResume(c *echo.Context, req sabAPIRequest) error {
	if req.NZOID != "" || req.Value != "" || req.Name != "" {
		return c.JSON(http.StatusBadRequest, sabStatusResponse{
			Status: false,
			Error:  "per-job resume is not supported; use global resume",
		})
	}

	if !ctrl.Service.Resume() {
		return c.JSON(http.StatusInternalServerError, sabStatusResponse{
			Status: false,
			Error:  "failed to resume downloader queue",
		})
	}

	return c.JSON(http.StatusOK, sabStatusResponse{
		Status: true,
	})
}

func (ctrl *SABController) handleGetFiles(c *echo.Context, req sabAPIRequest) error {
	id := req.filesTarget()
	if id == "" {
		return c.JSON(http.StatusBadRequest, sabFilesResponse{
			Status: false,
			Error:  "missing nzo_id/value for get_files",
			Files:  []sabFileEntry{},
		})
	}

	files, err := ctrl.lookupQueueFiles(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, sabFilesResponse{
			Status: false,
			Error:  err.Error(),
			Files:  []sabFileEntry{},
		})
	}
	if files == nil {
		return c.JSON(http.StatusNotFound, sabFilesResponse{
			Status: false,
			Error:  "queue item not found",
			Files:  []sabFileEntry{},
		})
	}

	resp := make([]sabFileEntry, 0, len(files))
	for _, f := range files {
		resp = append(resp, sabFileEntry{
			Filename: f.FileName,
			Size:     f.Size,
			Subject:  f.Subject,
			Date:     f.Date,
		})
	}

	return c.JSON(http.StatusOK, sabFilesResponse{
		Status: true,
		Files:  resp,
	})
}

func (ctrl *SABController) handleVersion(c *echo.Context, _ sabAPIRequest) error {
	return c.JSON(http.StatusOK, sabVersionResponse{
		Version: "4.5.0",
	})
}

func (ctrl *SABController) handleGetCategories(c *echo.Context, _ sabAPIRequest) error {
	return c.JSON(http.StatusOK, sabCategoriesResponse{
		Categories: []string{"*", "movies", "tv"},
	})
}

func (ctrl *SABController) handleGetConfig(c *echo.Context, req sabAPIRequest) error {
	cfg := ctrl.buildSABConfig()

	switch req.Section {
	case "", "misc":
		if req.Section == "misc" {
			return c.JSON(http.StatusOK, sabConfigResponse{
				Config: sabConfigData{
					Misc: cfg.Misc,
				},
			})
		}
		return c.JSON(http.StatusOK, sabConfigResponse{Config: cfg})

	case "categories":
		return c.JSON(http.StatusOK, sabConfigResponse{
			Config: sabConfigData{
				Categories: cfg.Categories,
			},
		})

	default:
		return c.JSON(http.StatusOK, sabConfigResponse{
			Config: sabConfigData{},
		})
	}
}

func (ctrl *SABController) handleFullStatus(c *echo.Context, req sabAPIRequest) error {
	queueData := ctrl.buildQueueData(req)

	cfg := ctrl.App.Config
	downloadDir := ""
	completeDir := ""
	if cfg != nil {
		downloadDir = strings.TrimSpace(cfg.Download.OutDir)
		completeDir = strings.TrimSpace(cfg.Download.CompletedDir)
	}

	return c.JSON(http.StatusOK, sabFullStatusResponse{
		Status: sabFullStatusData{
			Status:          queueData.Status,
			Speedlimit:      queueData.Speedlimit,
			SpeedlimitAbs:   queueData.SpeedlimitAbs,
			Paused:          queueData.Paused,
			PausedAll:       queueData.PausedAll,
			NoOfSlotsTotal:  queueData.NoOfSlotsTotal,
			NoOfSlots:       queueData.NoOfSlots,
			Limit:           queueData.Limit,
			Start:           queueData.Start,
			TimeLeft:        queueData.TimeLeft,
			Speed:           queueData.Speed,
			KBPerSec:        queueData.KBPerSec,
			Size:            queueData.Size,
			SizeLeft:        queueData.SizeLeft,
			MB:              queueData.MB,
			MBLeft:          queueData.MBLeft,
			Slots:           queueData.Slots,
			DiskSpace1:      queueData.DiskSpace1,
			DiskSpace2:      queueData.DiskSpace2,
			DiskSpaceTotal1: queueData.DiskSpaceTotal1,
			DiskSpaceTotal2: queueData.DiskSpaceTotal2,
			DiskSpace1Norm:  queueData.DiskSpace1Norm,
			DiskSpace2Norm:  queueData.DiskSpace2Norm,
			HaveWarnings:    queueData.HaveWarnings,
			PauseInt:        queueData.PauseInt,
			LeftQuota:       queueData.LeftQuota,
			Version:         queueData.Version,
			Finish:          queueData.Finish,
			CacheArt:        queueData.CacheArt,
			CacheSize:       queueData.CacheSize,
			FinishAction:    queueData.FinishAction,
			Quota:           queueData.Quota,
			HaveQuota:       queueData.HaveQuota,

			LocalIPv4:        "127.0.0.1",
			IPv6:             nil,
			PublicIPv4:       nil,
			DNSLookup:        "OK",
			Folders:          []string{},
			CPUModel:         "",
			Pystone:          0,
			LoadAvg:          "",
			DownloadDir:      downloadDir,
			DownloadDirSpeed: 0,
			CompleteDir:      completeDir,
			CompleteDirSpeed: 0,
			LogLevel:         "0",
			LogFile:          "",
			ConfigFn:         "",
			NT:               false,
			Darwin:           false,
			ConfigHelpURI:    "https://sabnzbd.org/wiki/configuration/4.5/",
			Uptime:           "",
			ColorScheme:      "Default",
			WebDir:           "",
			ActiveLang:       "en",
			RestartReq:       false,
			PowerOptions:     false,
			PPPauseEvent:     false,
			PID:              0,
			WebLogFile:       nil,
			NewRelease:       false,
			NewRelURL:        nil,
			Warnings:         []map[string]any{},
			Servers:          []sabStatusServer{},
		},
	})
}

func (ctrl *SABController) lookupQueueFiles(ctx context.Context, id string) ([]*domain.DownloadFile, error) {
	item, err := ctrl.Service.GetItem(ctx, id)
	if err == nil && item != nil {
		return ctrl.Service.GetItemFiles(ctx, id)
	}

	queueItem, err := ctrl.App.JobStore.GetQueueItem(ctx, id)
	if err != nil {
		return nil, err
	}
	if queueItem == nil {
		return nil, nil
	}

	return ctrl.App.QueueFileStore.GetQueueItemFiles(ctx, queueItem.ID)
}

func firstUploadedFile(c *echo.Context, fieldNames ...string) (*multipart.FileHeader, error) {
	for _, fieldName := range fieldNames {
		fileHeader, err := c.FormFile(fieldName)
		if err == nil && fileHeader != nil {
			return fileHeader, nil
		}
	}
	return nil, fmt.Errorf("missing nzb file upload")
}
