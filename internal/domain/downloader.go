package domain

type DownloadJob struct {
	Segment    NZBSegment
	File       *DownloadFile
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
