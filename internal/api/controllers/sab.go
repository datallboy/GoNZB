package controllers

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	queuesvc "github.com/datallboy/gonzb/internal/queue"
	"github.com/labstack/echo/v5"
)

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

	if err := echo.BindQueryParams(c, &req); err != nil {
		return req, fmt.Errorf("invalid query parameters")
	}
	if err := echo.BindBody(c, &req); err != nil {
		return req, fmt.Errorf("invalid request body")
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

	target := strings.TrimSpace(req.Value)
	if target == "" {
		target = strings.TrimSpace(req.NZOID)
	}
	if target == "" {
		target = strings.TrimSpace(req.Name)
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

	resp, err := http.DefaultClient.Do(httpReq)
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
			// Queue fields
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

			// Status-only fields
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

func (ctrl *SABController) buildQueueData(req sabAPIRequest) sabQueueData {
	slots := ctrl.buildQueueSlots(req)
	summary := summarizeQueueSlots(slots)

	start := parseIntDefault(req.Start, 0)
	limit := parseIntDefault(req.Limit, len(slots))
	if limit <= 0 {
		limit = len(slots)
	}
	if start < 0 {
		start = 0
	}
	if start > len(slots) {
		start = len(slots)
	}
	end := start + limit
	if end > len(slots) {
		end = len(slots)
	}

	visibleSlots := slots[start:end]

	queueStatus := "Paused"
	if !ctrl.Service.IsPaused() {
		queueStatus = "Idle"
	}
	for _, slot := range slots {
		if slot.Status == "Downloading" {
			queueStatus = "Downloading"
			break
		}
		if slot.Status == "Queued" && queueStatus == "Idle" {
			queueStatus = "Queued"
		}
		if slot.Status == "Processing" {
			queueStatus = "Downloading"
		}
	}

	return sabQueueData{
		Status:          queueStatus,
		Speedlimit:      "0",
		SpeedlimitAbs:   "0",
		Paused:          ctrl.Service.IsPaused(),
		PausedAll:       ctrl.Service.IsPaused(),
		NoOfSlotsTotal:  len(slots),
		NoOfSlots:       len(visibleSlots),
		Limit:           limit,
		Start:           start,
		TimeLeft:        "0:00:00",
		Speed:           "0 B",
		KBPerSec:        "0.00",
		Size:            formatSize(summary.TotalBytes),
		SizeLeft:        formatSize(summary.LeftBytes),
		MB:              formatMB(summary.TotalBytes),
		MBLeft:          formatMB(summary.LeftBytes),
		Slots:           visibleSlots,
		DiskSpace1:      "0.00",
		DiskSpace2:      "0.00",
		DiskSpaceTotal1: "0.00",
		DiskSpaceTotal2: "0.00",
		DiskSpace1Norm:  "0.0 G",
		DiskSpace2Norm:  "0.0 G",
		HaveWarnings:    "0",
		PauseInt:        "0",
		LeftQuota:       "0 ",
		Version:         "4.5.0",
		Finish:          0,
		CacheArt:        "0",
		CacheSize:       "0 B",
		FinishAction:    nil,
		Quota:           "0 ",
		HaveQuota:       false,
	}
}

func (ctrl *SABController) buildSABConfig() sabConfigData {
	downloadDir := ""
	completeDir := ""

	if ctrl.App != nil && ctrl.App.Config != nil {
		downloadDir = strings.TrimSpace(ctrl.App.Config.Download.OutDir)
		completeDir = strings.TrimSpace(ctrl.App.Config.Download.CompletedDir)
	}

	categories := []sabConfigCategory{
		{
			Name:     "movies",
			Order:    1,
			PP:       "3",
			Script:   "None",
			Dir:      categoryDir(completeDir, "movies"),
			Newzbin:  "",
			Priority: "0",
		},
		{
			Name:     "tv",
			Order:    2,
			PP:       "3",
			Script:   "None",
			Dir:      categoryDir(completeDir, "tv"),
			Newzbin:  "",
			Priority: "0",
		},
	}

	return sabConfigData{
		Misc: sabConfigMisc{
			DownloadDir:  downloadDir,
			CompleteDir:  completeDir,
			TVSorting:    0,
			MovieSorting: 0,
		},
		Categories: categories,
	}
}

func categoryDir(baseDir, category string) string {
	baseDir = strings.TrimSpace(baseDir)
	category = strings.TrimSpace(category)

	if baseDir == "" {
		return category
	}
	if category == "" {
		return baseDir
	}
	return filepath.Clean(filepath.Join(baseDir, category))
}

func (ctrl *SABController) buildQueueSlots(req sabAPIRequest) []sabQueueSlot {
	items := ctrl.Service.ListActive()

	slots := make([]sabQueueSlot, 0, len(items))
	for idx, item := range items {
		if item == nil {
			continue
		}
		if item.Status == domain.StatusCompleted || item.Status == domain.StatusFailed {
			continue
		}
		if !matchesSABSearch(item, req.Search) {
			continue
		}
		if !matchesSABCategory(queueItemCategory(item), req.Category) {
			continue
		}

		totalBytes := queueItemSize(item)
		written := item.GetBytes()
		left := totalBytes - written
		if left < 0 {
			left = 0
		}

		slotStatus := mapSABQueueStatus(item.Status)
		if ctrl.Service.IsPaused() && slotStatus == "Queued" {
			slotStatus = "Paused"
		}

		slots = append(slots, sabQueueSlot{
			Status:       slotStatus,
			Index:        idx,
			Password:     "",
			AvgAge:       "",
			TimeAdded:    item.CreatedAt.UTC().Unix(),
			Script:       "None",
			DirectUnpack: nil,
			MB:           formatMB(totalBytes),
			MBLeft:       formatMB(left),
			MBMissing:    "0.0",
			Size:         formatSize(totalBytes),
			SizeLeft:     formatSize(left),
			Filename:     queueItemDisplayName(item),
			Labels:       []string{},
			Priority:     "Normal",
			Category:     queueItemCategory(item),
			TimeLeft:     "0:00:00",
			Percentage:   formatPercentage(written, totalBytes),
			NZOID:        item.ID,
			UnpackOpts:   "3",
			Bytes:        totalBytes,
		})
	}

	return slots
}

type queueSlotSummary struct {
	TotalBytes int64
	LeftBytes  int64
	DoneBytes  int64
}

func summarizeQueueSlots(slots []sabQueueSlot) queueSlotSummary {
	var summary queueSlotSummary

	for _, slot := range slots {
		summary.TotalBytes += slot.Bytes

		leftBytes := parseMBString(slot.MBLeft)
		if leftBytes < 0 {
			leftBytes = 0
		}
		summary.LeftBytes += leftBytes
	}

	summary.DoneBytes = summary.TotalBytes - summary.LeftBytes
	if summary.DoneBytes < 0 {
		summary.DoneBytes = 0
	}

	return summary
}

func parseMBString(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	mb, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return int64(mb * 1024 * 1024)
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

func queueItemDisplayName(item *domain.QueueItem) string {
	if item == nil {
		return ""
	}
	if item.Release != nil && item.Release.Title != "" {
		return item.Release.Title
	}
	if item.ReleaseTitle != "" {
		return item.ReleaseTitle
	}
	if item.ReleaseID != "" {
		return item.ReleaseID
	}
	return item.ID
}

func queueItemCategory(item *domain.QueueItem) string {
	if item != nil && item.Release != nil && strings.TrimSpace(item.Release.Category) != "" {
		return item.Release.Category
	}
	return "*"
}

func queueItemSize(item *domain.QueueItem) int64 {
	if item == nil {
		return 0
	}
	if item.Release != nil && item.Release.Size > 0 {
		return item.Release.Size
	}
	if item.ReleaseSize > 0 {
		return item.ReleaseSize
	}
	return 0
}

func matchesSABSearch(item *domain.QueueItem, search string) bool {
	search = strings.TrimSpace(strings.ToLower(search))
	if search == "" {
		return true
	}
	return strings.Contains(strings.ToLower(queueItemDisplayName(item)), search)
}

func matchesSABCategory(itemCategory, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}

	itemCategory = strings.TrimSpace(itemCategory)
	if itemCategory == "" {
		itemCategory = "*"
	}

	for _, part := range strings.Split(filter, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.EqualFold(part, itemCategory) {
			return true
		}
	}

	return false
}

func mapSABQueueStatus(status domain.JobStatus) string {
	switch status {
	case domain.StatusPending:
		return "Queued"
	case domain.StatusDownloading:
		return "Downloading"
	case domain.StatusProcessing:
		return "Processing"
	case domain.StatusCompleted:
		return "Completed"
	case domain.StatusFailed:
		return "Failed"
	default:
		return string(status)
	}
}

func mapSABHistoryStatus(status domain.JobStatus) string {
	switch status {
	case domain.StatusCompleted:
		return "Completed"
	case domain.StatusFailed:
		return "Failed"
	default:
		return string(status)
	}
}

func formatMB(bytes int64) string {
	if bytes <= 0 {
		return "0.00"
	}
	return fmt.Sprintf("%.2f", float64(bytes)/(1024*1024))
}

func formatPercentage(done, total int64) string {
	if total <= 0 {
		return "0"
	}
	pct := (float64(done) / float64(total)) * 100
	return strconv.FormatFloat(pct, 'f', 2, 64)
}

func formatSize(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}

	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)

	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
