package processor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
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
func (p *Processor) Prepare(ctx context.Context, nzbModel *nzb.Model, nzbFilename string) ([]*domain.DownloadFile, error) {
	var tasks []*domain.DownloadFile

	if err := os.MkdirAll(p.ctx.Config.Download.OutDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create out_dir: %w", err)
	}

	// Try Meta tag for password
	password := nzbModel.GetPassword()

	// Try NZB Filename fallback
	if password == "" {
		password = extractPassword(nzbFilename)
	}

	// Fallback to subject line
	if password == "" && len(nzbModel.Files) > 0 {
		password = extractPassword(nzbModel.Files[0].Subject)
	}

	for i, rawFile := range nzbModel.Files {
		cleanName := sanitizeFileName(rawFile.Subject)

		domSegs := make([]domain.Segment, len(rawFile.Segments))
		for j, s := range rawFile.Segments {
			domSegs[j] = domain.Segment{Number: s.Number, Bytes: s.Bytes, MessageID: s.MessageID}
		}

		// Create the Task (This calculates Size and Paths internally)
		task := domain.NewDownloadFile(cleanName, 0, i, domSegs, p.ctx.Config.Download.OutDir, password)
		task.Subject = rawFile.Subject
		task.Date = rawFile.Date
		task.Groups = rawFile.Groups

		// Check if it exists, still append so PostProcess knows about the file
		if _, err := os.Stat(task.FinalPath); err == nil {
			task.IsComplete = true
		} else {
			// Only pre-allocate if we actually need to download it
			if err := p.writer.PreAllocate(task.PartPath, task.Size); err != nil {
				return nil, fmt.Errorf("failed to pre-allocate %s: %w", task.FileName, err)
			}
		}

		// Populate tasks either way so Download can determine how far along the download is
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// Finalize renames .part to final names
func (p *Processor) Finalize(ctx context.Context, tasks []*domain.DownloadFile) error {
	for _, task := range tasks {
		// If it was already complete from Prepare, skip
		if task.IsComplete {
			continue
		}

		// CHECK IF PART FILE EXISTS: If no part file and no final file, it's a fail.
		info, err := os.Stat(task.PartPath)
		if err != nil {
			p.ctx.Logger.Debug("Finalize: No part file found for %s, skipping...", task.FileName)
			continue
		}

		actualSize := task.GetActualSize()

		// Release the file handle from the FileWriter
		err = p.writer.CloseFile(task.PartPath, actualSize)
		if err != nil {
			p.ctx.Logger.Error("Failed to close/truncate %s: %v", task.FileName, err)
		}

		// Use actualSize for the check if we have it. fallback to task.Size (NZB size)
		targetSize := actualSize
		if targetSize == 0 {
			targetSize = task.Size
		}

		if info.Size() < targetSize {
			p.ctx.Logger.Warn("File incomplete, (Size %d, Expected: %d), skipping finalize: %s", info.Size(), targetSize, task.FileName)
			continue
		}

		// Rename to final
		if err := os.Rename(task.PartPath, task.FinalPath); err != nil {
			p.ctx.Logger.Error("Finalize failed for %s: %v", task.FileName, err)
			continue
		}

		//
		task.IsComplete = true
		p.ctx.Logger.Debug("Completed: %s", task.FileName)
	}
	return nil
}

// PostProcess handles the modular repair and extraction logic
func (p *Processor) PostProcess(ctx context.Context, tasks []*domain.DownloadFile) error {
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

func (p *Processor) extractArchives(ctx context.Context, tasks []*domain.DownloadFile) ([]*domain.DownloadFile, error) {

	if !p.ctx.ExtractionEnabled {
		return nil, nil
	}

	if !p.extractor.HasExtractors() {
		p.ctx.Logger.Warn("No extractors available, skipping archive extraction")
		return nil, nil
	}

	p.ctx.Logger.Debug("Available extractors: %v", p.extractor.AvailableExtractors())

	var allNewTasks []*domain.DownloadFile
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

func (p *Processor) extractBatch(ctx context.Context, tasks []*domain.DownloadFile) ([]*domain.DownloadFile, error) {
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

	var newTasks []*domain.DownloadFile

	// Extract each archive
	for task, archive := range archives {
		archiveName := filepath.Base(task.FinalPath)
		p.ctx.Logger.Debug("Extracting %s with %s", archiveName, archive.Name())

		// Extract to the same directory as the archive
		destDir := filepath.Dir(task.FinalPath)
		extractedFile, err := archive.Extract(ctx, task.FinalPath, destDir, task.Password)
		if err != nil {
			p.ctx.Logger.Error("Extraction failed for %s: %v", task.FileName, err)
			continue
		}

		// Convert strings to domain.DownloadFile objects
		for _, path := range extractedFile {
			newTasks = append(newTasks, &domain.DownloadFile{
				FinalPath: path,
				FileName:  filepath.Base(path),
			})

		}

		p.ctx.Logger.Debug("Successfully extracted: %s", archiveName)
	}
	return newTasks, nil

}

func (p *Processor) moveToCompleted(tasks []*domain.DownloadFile) error {

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
			p.ctx.Logger.Error("Failed cross-device move for %s: %v", task.FileName, err)
			return err
		}
	}
	return nil
}

func findPrimaryPar(tasks []*domain.DownloadFile) string {
	for _, t := range tasks {
		if strings.HasSuffix(t.FinalPath, ".par2") && !strings.Contains(t.FinalPath, ".vol") {
			return t.FinalPath
		}
	}
	return ""
}

// extractPassword searches a string for the {{password}} pattern
func extractPassword(input string) string {
	// Matches text inside double curly braces: {{mypassword}}
	re := regexp.MustCompile(`\{\{(.*?)\}\}`)
	match := re.FindStringSubmatch(input)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}
