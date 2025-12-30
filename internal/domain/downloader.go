package domain

type DownloadJob struct {
	Segment    NZBSegment
	Groups     []string
	FilePath   string // Where to write this specific segment
	Offset     int64
	RetryCount int
}

type DownloadResult struct {
	Segment NZBSegment
	Job     DownloadJob
	Error   error
}

type WriteRequest struct {
	FilePath string
	Offset   int64
	Data     []byte
	Done     chan error // Feedback loop for the worker
}
