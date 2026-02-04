package domain

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/datallboy/gonzb/internal/nzb"
)

type JobStatus string

const (
	StatusPending     JobStatus = "pending"
	StatusDownloading JobStatus = "downloading"
	StatusProcessing  JobStatus = "processing" // Post-processing (unrar/7z)
	StatusCompleted   JobStatus = "completed"
	StatusFailed      JobStatus = "failed"
)

// QueueItem represents the entire NZB download process
type QueueItem struct {
	ID       string
	Name     string
	Password string
	Status   JobStatus

	Tasks []*nzb.DownloadFile

	BytesWritten atomic.Uint64
	TotalBytes   uint64

	StartedAt time.Time
	Error     string

	CancelFunc context.CancelFunc
}

func (item *QueueItem) CalculateTotalSize() {
	var total int64
	for _, file := range item.Tasks {
		for _, seg := range file.Segments {
			total += seg.Bytes
		}
	}
	item.TotalBytes = uint64(total)
}
