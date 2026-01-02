package domain

import "path/filepath"

type DownloadFile struct {
	Source    *NZBFile
	Segments  []NZBSegment
	CleanName string
	PartPath  string
	FinalPath string
	Size      int64
}

func NewDownloadFile(raw NZBFile, cleanName, outDir string) *DownloadFile {
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
		Size:      total,
	}
}
