package downloader

import (
	"context"
	"fmt"
	"gonzb/internal/config"
	"gonzb/internal/domain"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		// Allocate a 1MB buffer (typical max size for a Usenet segment)
		return make([]byte, 1024*1024)
	},
}

type Service struct {
	cfg *config.Config
}

func NewService(c *config.Config) *Service {
	return &Service{cfg: c}
}

func (s *Service) DownloadNZB(ctx context.Context, nzb *domain.NZB) error {
	if err := s.prepareEnvironment(); err != nil {
		return err
	}

	writer := NewFileWriter()
	defer writer.CloseAll()

	if err := s.runWorkerPool(ctx, nzb, writer); err != nil {
		return err
	}

	return s.mergeAll(nzb)
}

func (s *Service) prepareEnvironment() error {
	// Create the output directory where finished files go
	if err := os.MkdirAll(s.cfg.Download.OutDir, 0755); err != nil {
		return fmt.Errorf("failed to create out_dir: %w", err)
	}

	// Create the temporary directory for segments
	if err := os.MkdirAll(s.cfg.Download.TempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp_dir: %w", err)
	}

	return nil
}

func (s *Service) mergeAll(nzb *domain.NZB) error {
	for _, file := range nzb.Files {
		log.Printf("Merging file: %s", file.Subject)

		// 1. Determine final filename (simplified sanitize)
		// Usually Usenet subjects look like: "FileName.rar [1/50]"
		// For now, we'll just use the subject string cleaned up.
		cleanName := strings.Map(func(r rune) rune {
			if r == '/' || r == '\\' || r == ':' {
				return '_'
			}
			return r
		}, file.Subject)

		finalPath := filepath.Join(s.cfg.Download.OutDir, cleanName)

		// 2. Perform the actual byte-copy merge
		if err := s.mergeFile(file, finalPath); err != nil {
			return fmt.Errorf("failed to merge %s: %w", cleanName, err)
		}
	}
	return nil
}

func (s *Service) dispatchJobs(nzb *domain.NZB, jobs chan<- domain.DownloadJob) {
	// Ensure we close the jobs channel when we are done sending,
	// otherwise workers will hang forever waiting for more.
	defer close(jobs)

	for _, file := range nzb.Files {
		var currentOffset int64 = 0
		cleanName := s.sanitizeFileName(file.Subject)
		finalPath := filepath.Join(s.cfg.Download.OutDir, cleanName)

		for _, seg := range file.Segments {
			jobs <- domain.DownloadJob{
				Segment:  seg,
				FilePath: finalPath,
				Offset:   currentOffset,
			}
			currentOffset += seg.Bytes
		}
	}
}

func (s *Service) sanitizeFileName(subject string) string {
	// 1. Remove characters that are illegal in Linux/Windows filenames
	badChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	res := subject
	for _, char := range badChars {
		res = strings.ReplaceAll(res, char, "_")
	}

	// 2. Optional: Usenet subjects often end with ' (1/50)' or similar metadata.
	// We can use a regex here if we want to be fancy, but a simple trim works for now.
	if idx := strings.LastIndex(res, "\""); idx != -1 {
		// Many NZBs put the filename in quotes inside the subject
		res = res[idx+1:]
	}

	return strings.TrimSpace(res)
}
