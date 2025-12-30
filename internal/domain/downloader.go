package domain

type DownloadJob struct {
	Segment  NZBSegment
	FilePath string // Where to write this specific segment
	Offset   int64
}

type DownloadResult struct {
	Segment NZBSegment
	Error   error
}

type WriteRequest struct {
	FilePath string
	Offset   int64
	Data     []byte
	Done     chan error // Feedback loop for the worker
}
