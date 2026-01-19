package nzb

import (
	"path/filepath"
	"sync/atomic"
)

type DownloadFile struct {
	Source     *File
	Segments   []Segment
	CleanName  string
	PartPath   string
	FinalPath  string
	Password   string
	Size       int64
	actualSize int64
}

func NewDownloadFile(raw File, cleanName, outDir string, password string) *DownloadFile {
	var total int64
	for _, s := range raw.Segments {
		total += s.Bytes
	}

	final := filepath.Join(outDir, cleanName)

	return &DownloadFile{
		Source:    &raw,
		Segments:  raw.Segments,
		CleanName: cleanName,
		PartPath:  final + ".part",
		FinalPath: final,
		Password:  password,
		Size:      total,
	}
}

func (f *DownloadFile) SetActualSize(size int64) {
	atomic.StoreInt64(&f.actualSize, size)
}

func (f *DownloadFile) GetActualSize() int64 {
	return atomic.LoadInt64(&f.actualSize)
}
