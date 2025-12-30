package downloader

import (
	"fmt"
	"gonzb/internal/domain"
	"io"
	"os"
	"path/filepath"
)

func (s *Service) mergeFile(nzbFile domain.NZBFile, destPath string) error {
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	for _, seg := range nzbFile.Segments {
		tempPath := filepath.Join(s.cfg.Download.TempDir, seg.MessageID)

		if err := s.appendAndCleanup(tempPath, out); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) appendAndCleanup(srcPath string, dst io.Writer) error {
	src, err := os.Open(srcPath)
	if err != nil {
		// If a segment is missing, the whole file is corrupt.
		return fmt.Errorf("missing segment file %s: %w", srcPath, err)
	}

	// Stream the segment into the final file
	_, err = io.Copy(dst, src)
	src.Close() // Close before removing

	if err != nil {
		return err
	}

	// Clean up the temp segment immediately to free space
	return os.Remove(srcPath)
}
