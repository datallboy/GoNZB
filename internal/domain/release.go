package domain

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Release represents the NZB itself, whether from an indexer or a file upload.
type Release struct {
	ID              string    `json:"id"`
	FileHash        string    `json:"hash"`
	Title           string    `json:"title"`
	Password        string    `json:"password"`
	GUID            string    `json:"guid"`
	Source          string    `json:"source"`
	DownloadURL     string    `json:"downloadUrl"`
	Size            int64     `json:"size"`
	PublishDate     time.Time `json:"publishDate"`
	Category        string    `json:"category"`
	RedirectAllowed bool
	Poster          string
}

// Segment represents an individual article to be fetched from Usenet
type Segment struct {
	Number      int
	Bytes       int64
	MessageID   string
	MissingFrom map[string]bool
}

// DownloadFile represents an individual file within a Release.
type DownloadFile struct {
	// Persistent fields (saved in release_files table)
	ID        int64
	ReleaseID string
	FileName  string // Sanitized filename
	Size      int64  // Expected total size from NZB
	Index     int    // Original order in the NZB
	IsPars    bool   // True if the file is a repar volume
	Subject   string
	Date      int64
	Groups    []string
	Poster    string

	// App State (calulated at hydration)
	PartPath   string
	FinalPath  string
	IsComplete bool
	Password   string

	// Progress tracking
	actualSize atomic.Int64
	Segments   []Segment
}

// NewDownloadFile is the constructor for creating a live download task.
// If size is 0, it calculates the total size from the segment list.
func NewDownloadFile(fileName string, size int64, index int, segments []Segment, outDir string, password string) *DownloadFile {

	if size <= 0 {
		for _, s := range segments {
			size += s.Bytes
		}
	}

	f := &DownloadFile{
		FileName: fileName,
		Size:     size,
		Index:    index,
		Segments: segments,
		Password: password,
		IsPars:   strings.HasSuffix(strings.ToLower(fileName), ".par2"),
	}
	f.Prepare(outDir)
	return f
}

func (f *DownloadFile) Prepare(outDir string) {
	final := filepath.Join(outDir, f.FileName)
	f.PartPath = final + ".part"
	f.FinalPath = final
}

func (f *DownloadFile) SetActualSize(size int64) {
	f.actualSize.Store(size)
}

func (f *DownloadFile) GetActualSize() int64 {
	return f.actualSize.Load()
}

type PreparationResult struct {
	Tasks     []*DownloadFile
	TotalSize int64
	Password  string
}
