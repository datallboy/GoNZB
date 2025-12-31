package downloader

import (
	"context"
	"fmt"
	"gonzb/internal/config"
	"gonzb/internal/domain"
	"gonzb/internal/provider"
	"html"
	"log"
	"os"
	"path/filepath"
	"regexp"
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
	res := html.UnescapeString(subject)

	// Try pattern A: Contents inside double quotes
	firstQuote := strings.Index(res, "\"")
	lastQuote := strings.LastIndex(res, "\"")
	if firstQuote != -1 && lastQuote != -1 && firstQuote < lastQuote {
		res = res[firstQuote+1 : lastQuote]
	} else {
		// Try pattern B: Strip Usenet metadata (fallback)
		//  Removes (1/14) or [01/14] and the "yenc" suffix

		// Remove yenc
		reYenc := regexp.MustCompile(`(?i)\s+yenc.*$`)
		res = reYenc.ReplaceAllString(res, "")

		// Remove leading counters like [1/14]
		reLead := regexp.MustCompile(`^\[\d+/\d+\]\s+`)
		res = reLead.ReplaceAllString(res, "")
	}

	// Final cleanup: remove OS characters
	// Windows/Linux/macOS safety
	badChars := regexp.MustCompile(`[\\/:*?"<>|]`)
	res = badChars.ReplaceAllString(res, "_")

	return strings.TrimSpace(res)
}
