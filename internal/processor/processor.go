package processor

import (
	"context"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/datallboy/gonzb/internal/config"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/logger"
	"github.com/datallboy/gonzb/internal/repair"
)

type Closeable interface {
	CloseFile(path string, finalSize int64) error
	PreAllocate(path string, size int64) error
}

type FileProcessor struct {
	logger       *logger.Logger
	writer       Closeable
	outDir       string
	completedDir string
	cleanupMap   map[string]struct{}
}

func NewFileProcessor(l *logger.Logger, w Closeable, downloadCfg *config.DownloadConfig) *FileProcessor {
	fp := &FileProcessor{logger: l, writer: w, outDir: downloadCfg.OutDir, completedDir: downloadCfg.CompletedDir, cleanupMap: make(map[string]struct{})}

	for _, ext := range downloadCfg.CleanupExtensions {
		normalized := strings.ToLower(ext)
		if !strings.HasPrefix(normalized, ".") {
			normalized = "." + normalized
		}
		fp.cleanupMap[normalized] = struct{}{}
	}

	return fp
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

		p.logger.Debug("Completed: %s", task.CleanName)
	}
	return nil
}

// PostProcess handles the modular repair and extraction logic
func (p *FileProcessor) PostProcess(ctx context.Context, tasks []*domain.DownloadFile) error {
	if len(tasks) == 0 {
		return nil
	}

	p.logger.Info("Starting post-processing...")

	// Find the primary PAR2 file among the finalized tasks
	var primaryPar string
	for _, t := range tasks {
		if strings.HasSuffix(t.FinalPath, ".par2") && !strings.Contains(t.FinalPath, ".vol") {
			primaryPar = t.FinalPath
			break
		}
	}

	// Perform Repair if PAR2 exists
	if primaryPar != "" {
		p.logger.Debug("PAR2 Index found: %s. Verifying...", filepath.Base(primaryPar))

		repairer, err := repair.NewCLIPar2()
		if err != nil {
			return fmt.Errorf("cannot initialize repair engine: %w", err)
		}
		healthy, err := repairer.Verify(ctx, primaryPar)

		if err != nil {
			// Check for Exit Code 1 (Damanged but repairable)
			p.logger.Warn("Files are damanged. Attemting repair...")
			if repairErr := repairer.Repair(ctx, primaryPar); repairErr != nil {
				return fmt.Errorf("PAR2 repair failed: %w", repairErr)
			}
			p.logger.Info("Repair complete.")
		} else if healthy {
			p.logger.Info("All files verified healthy via PAR2.")
		}
	}

	// Move to Completed Directory
	if p.completedDir != "" {
		p.logger.Info("Moving files to completed directory: %s", p.completedDir)
		if err := p.moveToCompleted(tasks); err != nil {
			return fmt.Errorf("failed to move files: %w", err)
		}
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

func (p *FileProcessor) moveToCompleted(tasks []*domain.DownloadFile) error {

	// Ensure destination subdir exists if using nested folders
	if err := os.MkdirAll(p.completedDir, 0755); err != nil {
		return fmt.Errorf("failed to create completed directory: %w", err)
	}

	for _, task := range tasks {
		fileName := filepath.Base(task.FinalPath)

		if p.cleanupExtensions(fileName) {
			p.logger.Debug("Cleanup: Removing %s", fileName)
			// It's safe to ignore the error here if the file is already gone
			_ = os.Remove(task.FinalPath)
			continue
		}

		dest := filepath.Join(p.completedDir, filepath.Base(task.FinalPath))
		p.logger.Debug("Moving %s to completed folder", fileName)

		// Try rename
		err := os.Rename(task.FinalPath, dest)
		if err != nil {
			// Fallback to Copy + Delete
			if err := p.moveCrossDevice(task.FinalPath, dest); err != nil {
				p.logger.Error("Failed cross-device move for %s: %v", task.CleanName, err)
				return domain.ErrArticleNotFound
			}
		}
	}
	return nil
}

func (p *FileProcessor) cleanupExtensions(fileName string) bool {
	filenameLower := strings.ToLower(fileName)

	ext := filepath.Ext(filenameLower)
	_, exists := p.cleanupMap[ext]

	return exists
}

// moveCrossDevice handles moving files between different mount points/filesystems
func (p *FileProcessor) moveCrossDevice(sourcePath, destPath string) error {
	src, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer src.Close()

	// Create the destination file
	dst, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	// Copy the contents. io.Copy is efficient as it uses small buffers
	// or sendfile(2) where available.
	_, err = io.Copy(dst, src)
	if err != nil {
		return err
	}

	// Explicitly close before deleting the source
	src.Close()
	dst.Close()

	// Remove the original file only after copy success
	return os.Remove(sourcePath)
}
