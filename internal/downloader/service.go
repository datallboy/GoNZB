package downloader

import (
	"context"
	"fmt"
	"gonzb/internal/config"
	"gonzb/internal/domain"
	"gonzb/internal/provider"
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
	cfg     *config.Config
	manager *provider.Manager
	writer  *FileWriter
}

func NewService(c *config.Config, mgr *provider.Manager) *Service {
	return &Service{
		cfg:     c,
		manager: mgr,
		writer:  NewFileWriter(),
	}
}

func (s *Service) Download(ctx context.Context, nzb *domain.NZB) error {
	defer s.writer.CloseAll()

	if err := os.MkdirAll(s.cfg.Download.OutDir, 0755); err != nil {
		return fmt.Errorf("failed to create out_dir: %w", err)
	}

	// Pre-allocate Sparse Files (.part)
	for _, file := range nzb.Files {
		cleanName := s.sanitizeFileName(file.Subject)
		finalPath := filepath.Join(s.cfg.Download.OutDir, cleanName)

		// Create the sparse file so workers have a target
		if err := s.writer.PreAllocate(finalPath+".part", file.TotalSize()); err != nil {
			return fmt.Errorf("failed to pre-allocate %s %w", cleanName, err)
		}
	}

	// Call worker pool
	if err := s.runWorkerPool(ctx, nzb, s.writer); err != nil {
		return err
	}

	// Finialize: Close handles and rename .part -> final
	for _, file := range nzb.Files {
		cleanName := s.sanitizeFileName(file.Subject)
		finalPath := filepath.Join(s.cfg.Download.OutDir, cleanName)
		partPath := finalPath + ".part"

		// Close handle so OS releases the lock for renaming
		if err := s.writer.CloseFile(partPath); err != nil {
			log.Printf("Warning: failed to close %s: %v", partPath, err)
		}

		if err := os.Rename(partPath, finalPath); err != nil {
			return fmt.Errorf("failed to finalize %s: %w", cleanName, err)
		}
		log.Printf("Finished: %s", cleanName)
	}

	return nil
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
