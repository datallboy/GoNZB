package engine

import "github.com/datallboy/gonzb/internal/nzb"

type DownloadJob struct {
	Segment    nzb.Segment
	File       *nzb.DownloadFile
	Groups     []string
	Offset     int64
	RetryCount int
}

type DownloadResult struct {
	Job   DownloadJob
	Error error
}

type WriteRequest struct {
	FilePath string
	Offset   int64
	Data     []byte
	Done     chan error // Feedback loop for the worker
}
