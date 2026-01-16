package processor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/nzb"
)

type Closeable interface {
	CloseFile(path string, finalSize int64) error
	PreAllocate(path string, size int64) error
}

type Processor struct {
	ctx       *app.Context
	writer    Closeable
	extractor *Manager

	cleanupMap map[string]struct{}
}

func New(ctx *app.Context, w Closeable) *Processor {
	p := &Processor{
		ctx:        ctx,
		writer:     w,
		extractor:  NewManager(),
		cleanupMap: make(map[string]struct{}),
	}

	for _, ext := range ctx.Config.Download.CleanupExtensions {
		normalized := strings.ToLower(ext)
		if !strings.HasPrefix(normalized, ".") {
			normalized = "." + normalized
		}
		p.cleanupMap[normalized] = struct{}{}
	}

	return p
}

// Prepare sanitizes names and creates sparse files. Returns our internal Tasks.
func (p *Processor) Prepare(nzbModel *nzb.Model) ([]*nzb.DownloadFile, error) {
	var tasks []*nzb.DownloadFile

	for _, rawFile := range nzbModel.Files {
		cleanName := sanitizeFileName(rawFile.Subject)

		// Create the Task (This calculates Size and Paths internally)
		task := nzb.NewDownloadFile(rawFile, cleanName, p.ctx.Config.Download.OutDir)

		// Skip if already exists
		if _, err := os.Stat(task.FinalPath); err == nil {
			p.ctx.Logger.Info("Skipping: %s (already completed)", task.CleanName)
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
func (p *Processor) Finalize(ctx context.Context, tasks []*nzb.DownloadFile) error {
	for _, task := range tasks {

		actualSize := task.GetActualSize()

		// 1. Release the file handle from the FileWriter
		err := p.writer.CloseFile(task.PartPath, actualSize)
		if err != nil {
			p.ctx.Logger.Error("Failed to close/truncate %s: %v", task.CleanName, err)
		}

		// 2. Quick Integrity Check
		info, err := os.Stat(task.PartPath)
		if err != nil {
			p.ctx.Logger.Error("Could not stat file %s: %v", task.CleanName, err)
		}

		// Use actualSize for the check if we have it. fallback to task.Size (NZB size)
		targetSize := actualSize
		if targetSize == 0 {
			targetSize = task.Size
		}

		if info.Size() < targetSize {
			p.ctx.Logger.Warn("File incomplete, (Size %d, Expected: %d), skipping finalize: %s", info.Size(), targetSize, task.CleanName)
			continue
		}

		// 3. Rename to final
		if err := os.Rename(task.PartPath, task.FinalPath); err != nil {
			p.ctx.Logger.Error("Finalize failed for %s: %v", task.CleanName, err)
			continue
		}

		p.ctx.Logger.Debug("Completed: %s", task.CleanName)
	}
	return nil
}

// PostProcess handles the modular repair and extraction logic
func (p *Processor) PostProcess(ctx context.Context, tasks []*nzb.DownloadFile) error {
	if len(tasks) == 0 {
		return nil
	}

	p.ctx.Logger.Info("Starting post-processing...")

	// Find the primary PAR2 file among the finalized tasks
	// Verify and Repair if needed
	if primaryPar := findPrimaryPar(tasks); primaryPar != "" {
		if err := p.handleRepair(ctx, primaryPar); err != nil {
			p.ctx.Logger.Error("Post-repair health check failed: %v", err)
			return fmt.Errorf("Post-repair health check failed: %v", err)
		}
	}

	// Extract RAR archives if present
	extractedTasks, err := p.extractArchives(ctx, tasks)
	if err != nil {
		p.ctx.Logger.Error("Archive extraction failed: %v", err)
		return fmt.Errorf("Archive extraction failed: %v", err)
		// Non-fatal: continue to move files even if extraction fails
	}
	// Adds extracted files to our list of things to move
	// tasks contains the .rar files, extractedTasks contains actual files
	tasks = append(tasks, extractedTasks...)

	// Move to Completed Directory
	if p.ctx.Config.Download.CompletedDir != "" {
		p.ctx.Logger.Info("Moving files to completed directory: %s", p.ctx.Config.Download.CompletedDir)
		if err := p.moveToCompleted(tasks); err != nil {
			return fmt.Errorf("failed to move files: %w", err)
		}
	}

	return nil
}

func (p *Processor) extractArchives(ctx context.Context, tasks []*nzb.DownloadFile) ([]*nzb.DownloadFile, error) {

	if !p.ctx.ExtractionEnabled {
		return nil, nil
	}

	if !p.extractor.HasExtractors() {
		p.ctx.Logger.Warn("No extractors available, skipping archive extraction")
		return nil, nil
	}

	p.ctx.Logger.Debug("Available extractors: %v", p.extractor.AvailableExtractors())

	var allNewTasks []*nzb.DownloadFile
	currentBatch := tasks
	maxDepth := 3

	for depth := 1; depth <= maxDepth; depth++ {
		newTasks, err := p.extractBatch(ctx, currentBatch)
		if err != nil {
			return allNewTasks, err
		}

		if len(newTasks) == 0 {
			break // No more archives found, we are finished
		}

		// Add to the master list of files we need to move later
		allNewTasks = append(allNewTasks, newTasks...)

		// Set the newly found files as the next batch to check
		currentBatch = newTasks
		p.ctx.Logger.Debug("Depth %d complete, found %d new files to check", depth, len(newTasks))
	}

	return allNewTasks, nil
}

func (p *Processor) extractBatch(ctx context.Context, tasks []*nzb.DownloadFile) ([]*nzb.DownloadFile, error) {
	// Detect which files are archives
	archives, err := p.extractor.DetectArchives(tasks)

	if err != nil {
		return nil, fmt.Errorf("failed to detect archives: %w", err)
	}

	if len(archives) == 0 {
		p.ctx.Logger.Debug("No archives detected, skipping extraction")
		return nil, nil
	}

	p.ctx.Logger.Info("Found %d archive(s) to extract", len(archives))

	var newTasks []*nzb.DownloadFile

	// Extract each archive
	for task, archive := range archives {
		archiveName := filepath.Base(task.FinalPath)
		p.ctx.Logger.Debug("Extracting %s with %s", archiveName, archive.Name())

		// Extract to the same directory as the archive
		destDir := filepath.Dir(task.FinalPath)
		extractedFile, err := archive.Extract(ctx, task.FinalPath, destDir)
		if err != nil {
			p.ctx.Logger.Error("Xxtraction failed for %s: %v", task.CleanName, err)
			continue
		}

		// Convert strings to nzb.DownloadFile objects
		for _, path := range extractedFile {
			newTasks = append(newTasks, &nzb.DownloadFile{
				FinalPath: path,
				CleanName: filepath.Base(path),
			})

		}

		p.ctx.Logger.Debug("Successfully extracted: %s", archiveName)
	}
	return newTasks, nil

}

func (p *Processor) moveToCompleted(tasks []*nzb.DownloadFile) error {

	// Ensure destination subdir exists if using nested folders
	if err := os.MkdirAll(p.ctx.Config.Download.CompletedDir, 0755); err != nil {
		return fmt.Errorf("failed to create completed directory: %w", err)
	}

	for _, task := range tasks {
		fileName := filepath.Base(task.FinalPath)

		if cleanupExtensions(fileName, p.cleanupMap) {
			p.ctx.Logger.Debug("Cleanup: Removing %s", fileName)
			// It's safe to ignore the error here if the file is already gone
			_ = os.Remove(task.FinalPath)
			continue
		}

		dest := filepath.Join(p.ctx.Config.Download.CompletedDir, filepath.Base(task.FinalPath))
		p.ctx.Logger.Debug("Moving %s to completed folder", fileName)

		err := moveFile(task.FinalPath, dest)
		if err != nil {
			p.ctx.Logger.Error("Failed cross-device move for %s: %v", task.CleanName, err)
			return err
		}
	}
	return nil
}

func findPrimaryPar(tasks []*nzb.DownloadFile) string {
	for _, t := range tasks {
		if strings.HasSuffix(t.FinalPath, ".par2") && !strings.Contains(t.FinalPath, ".vol") {
			return t.FinalPath
		}
	}
	return ""
}
