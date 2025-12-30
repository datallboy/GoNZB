package domain

type DownloadJob struct {
	Segment  NZBSegment
	FilePath string // Where to write this specific segment
}

type DownloadResult struct {
	Segment NZBSegment
	Error   error
}
