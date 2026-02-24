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

	CreatedAt time.Time
	UpdatedAt time.Time
	StartedAt time.Time
	// CompletedAt is when the queue item reaches a terminal state.
	CompletedAt time.Time

	// Historical metrics persisted for queue history.
	DownloadedBytes    int64
	AvgBps             int64
	DownloadSeconds    int64
	PostProcessSeconds int64

	// Internal runtime markers used to compute durations.
	DownloadStartedAt   time.Time
	ProcessingStartedAt time.Time

	Error *string

	CancelFunc context.CancelFunc
}

type QueueItemEvent struct {
	ID        int64
	QueueID   string
	Stage     string
	Status    string
	Message   string
	MetaJSON  string
	CreatedAt time.Time
}

func (q *QueueItem) AddBytes(n int64) {
	q.BytesWritten.Add(n)
}

func (q *QueueItem) GetBytes() int64 {
	return q.BytesWritten.Load()
}
