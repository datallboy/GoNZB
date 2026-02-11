package domain

import (
	"context"
	"sync/atomic"
	"time"
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
	ID        string   // Unique KSUID for this job
	ReleaseID string   // Reference to the shared Release
	Release   *Release // Populated via JOIN from store
	Status    JobStatus
	OutDir    string

	// Tasks are only present in RAM. When loaded from queue_items,
	// this is nil until hydrated from BLOB store.
	Tasks []*DownloadFile

	BytesWritten atomic.Int64

	StartedAt time.Time
	Error     *string

	CancelFunc context.CancelFunc
}

func (q *QueueItem) AddBytes(n int64) {
	q.BytesWritten.Add(n)
}

func (q *QueueItem) GetBytes() int64 {
	return q.BytesWritten.Load()
}
