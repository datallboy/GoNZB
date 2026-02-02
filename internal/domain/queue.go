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
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Status JobStatus `json:"status"`

	NZBModel *nzb.Model          `json:"-"`
	Tasks    []*nzb.DownloadFile `json:"-"`

	BytesWritten atomic.Uint64 `json:"bytes_written"`
	TotalBytes   uint64        `json:"total_bytes"`

	StartedAt time.Time `json:"started_at"`
	Error     string    `json:"error,omitempty"`

	CancelFunc context.CancelFunc `json:"-"`
}
