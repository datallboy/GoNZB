package controllers

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/datallboy/gonzb/internal/domain"
)

// Keep SAB transport handlers in sab.go and move
// queue/config/status projection helpers into this file.

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
	if search == "" {
		return true
	}
	return strings.Contains(normalizeLowerTrimmed(queueItemDisplayName(item)), search)
}

func matchesSABCategory(itemCategory, filter string) bool {
	if filter == "" {
		return true
	}

	itemCategory = normalizeTrimmed(itemCategory)
	if itemCategory == "" {
		itemCategory = "*"
	}

	for _, part := range strings.Split(filter, ",") {
		part = normalizeTrimmed(part)
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
