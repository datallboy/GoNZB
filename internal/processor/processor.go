package processor

import (
	"context"
	"fmt"
	"gonzb/internal/domain"
	"gonzb/internal/logger"
	"html"
	"os"
	"regexp"
	"strings"
)

type Closeable interface {
	CloseFile(path string, finalSize int64) error
	PreAllocate(path string, size int64) error
}

type FileProcessor struct {
	logger *logger.Logger
	writer Closeable
	outDir string
}

func NewFileProcessor(l *logger.Logger, w Closeable, outDir string) *FileProcessor {
	return &FileProcessor{logger: l, writer: w, outDir: outDir}
}

// Prepare sanitizes names and creates sparse files. Returns our internal Tasks.
func (p *FileProcessor) Prepare(nzb *domain.NZB) ([]*domain.DownloadFile, error) {
	var tasks []*domain.DownloadFile

	for _, rawFile := range nzb.Files {
		cleanName := p.sanitizeFileName(rawFile.Subject)

		// Create the Task (This calculates Size and Paths internally)
		task := domain.NewDownloadFile(rawFile, cleanName, p.outDir)

		// Skip if already exists
		if _, err := os.Stat(task.FinalPath); err == nil {
			p.logger.Info("Skipping: %s (already completed)", task.CleanName)
			continue
		}

		// Pre-allocate the .part file
		if err := p.writer.PreAllocate(task.PartPath, task.Size); err != nil {
			return nil, fmt.Errorf("failed to pre-allocate %s: %w", task.CleanName, err)
		}

		tasks = append(tasks, task)
	}
	return tasks, nil
}

// Finalize renames .part to final names
func (p *FileProcessor) Finalize(ctx context.Context, tasks []*domain.DownloadFile) error {
	for _, task := range tasks {

		actualSize := task.GetActualSize()

		// 1. Release the file handle from the FileWriter
		err := p.writer.CloseFile(task.PartPath, actualSize)
		if err != nil {
			p.logger.Error("Failed to close/truncate %s: %v", task.CleanName, err)
		}

		// 2. Quick Integrity Check
		info, err := os.Stat(task.PartPath)
		if err != nil {
			p.logger.Error("Could not stat file %s: %v", task.CleanName, err)
		}

		// Use actualSize for the check if we have it. fallback to task.Size (NZB size)
		targetSize := actualSize
		if targetSize == 0 {
			targetSize = task.Size
		}

		if info.Size() < targetSize {
			p.logger.Warn("File incomplete, (Size %d, Expected: %d), skipping finalize: %s", info.Size(), targetSize, task.CleanName)
			continue
		}

		// 3. Rename to final
		if err := os.Rename(task.PartPath, task.FinalPath); err != nil {
			p.logger.Error("Finalize failed for %s: %v", task.CleanName, err)
			continue
		}

		p.logger.Info("Completed: %s", task.CleanName)
	}
	return nil
}

func (p *FileProcessor) sanitizeFileName(subject string) string {
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
