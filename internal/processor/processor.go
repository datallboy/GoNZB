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

// CHANGED: Prepare now uses the queue item's job-specific work dir.
func (p *Processor) Prepare(ctx context.Context, item *domain.QueueItem, nzbModel *nzb.Model, nzbFilename string) (*domain.PreparationResult, error) {
	var tasks []*domain.DownloadFile
	var totalSize int64

	outDir := p.ctx.Config.Download.OutDir
	if item != nil && strings.TrimSpace(item.OutDir) != "" {
		outDir = item.OutDir
	}
	if outDir == "" {
		return nil, fmt.Errorf("missing output directory for queue item")
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
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

		task := domain.NewDownloadFile(cleanName, 0, i, domSegs, outDir, password)

		task.Subject = rawFile.Subject
		task.Date = rawFile.Date
		task.Groups = rawFile.Groups
		task.Poster = rawFile.Poster

		totalSize += task.Size

		if info, err := os.Stat(task.FinalPath); err == nil {
			if task.Size > 0 && info.Size() < task.Size {
				p.ctx.Logger.Warn("Existing file is smaller than expected; re-downloading %s (%d < %d)",
					task.FileName, info.Size(), task.Size)
			} else {
				task.IsComplete = true
			}
		} else {
			if err := p.writer.PreAllocate(task.PartPath, task.Size); err != nil {
				return nil, fmt.Errorf("failed to pre-allocate %s: %w", task.FileName, err)
			}
		}

		tasks = append(tasks, task)
	}

	return &domain.PreparationResult{
		Tasks:     tasks,
		TotalSize: totalSize,
		Password:  password,
	}, nil
}

func (p *Processor) Finalize(ctx context.Context, tasks []*domain.DownloadFile) error {
	for _, task := range tasks {
		if task.IsComplete {
			continue
		}

		info, err := os.Stat(task.PartPath)
		if err != nil {
			p.ctx.Logger.Debug("Finalize: No part file found for %s, skipping...", task.FileName)
			continue
		}

		actualSize := task.GetActualSize()

		err = p.writer.CloseFile(task.PartPath, actualSize)
		if err != nil {
			p.ctx.Logger.Error("Failed to close/truncate %s: %v", task.FileName, err)
		}

		targetSize := actualSize
		if targetSize == 0 {
			targetSize = task.Size
		}

		if info.Size() < targetSize {
			p.ctx.Logger.Warn("File incomplete, (Size %d, Expected: %d), skipping finalize: %s", info.Size(), targetSize, task.FileName)
			continue
		}

		if err := os.Rename(task.PartPath, task.FinalPath); err != nil {
			p.ctx.Logger.Error("Finalize failed for %s: %v", task.FileName, err)
			continue
		}

		task.IsComplete = true
		p.ctx.Logger.Debug("Completed: %s", task.FileName)
	}
	return nil
}

// CHANGED: PostProcess now receives the queue item and preserves per-job completed layout.
func (p *Processor) PostProcess(ctx context.Context, item *domain.QueueItem, tasks []*domain.DownloadFile) error {
	if len(tasks) == 0 {
		return nil
	}

	p.ctx.Logger.Info("Starting post-processing...")

	if primaryPar := findPrimaryPar(tasks); primaryPar != "" {
		if err := p.handleRepair(ctx, primaryPar); err != nil {
			p.ctx.Logger.Error("Post-repair health check failed: %v", err)
			return fmt.Errorf("Post-repair health check failed: %v", err)
		}
	}

	extractedTasks, err := p.extractArchives(ctx, tasks)
	if err != nil {
		p.ctx.Logger.Error("Archive extraction failed: %v", err)
		return fmt.Errorf("Archive extraction failed: %v", err)
	}

	// CHANGED: if extraction succeeded, do not move the original archive set forward.
	moveTasks := buildMoveTaskList(tasks, extractedTasks)

	if p.ctx.Config.Download.CompletedDir != "" {
		p.ctx.Logger.Info("Moving files to completed directory: %s", p.ctx.Config.Download.CompletedDir)

		originalWorkDir := ""
		if item != nil {
			originalWorkDir = item.OutDir
		}

		completedDir, err := p.moveToCompleted(item, moveTasks)
		if err != nil {
			return fmt.Errorf("failed to move files: %w", err)
		}

		// after a successful move, point the queue item at the final importable location.
		if item != nil && completedDir != "" {
			item.OutDir = completedDir
		}

		// clean up leftover files from the old work dir after a successful move.
		if err := p.cleanupWorkDir(originalWorkDir, completedDir); err != nil {
			p.ctx.Logger.Warn("Failed to cleanup work directory %s: %v", originalWorkDir, err)
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
			break
		}

		allNewTasks = append(allNewTasks, newTasks...)
		currentBatch = newTasks
		p.ctx.Logger.Debug("Depth %d complete, found %d new files to check", depth, len(newTasks))
	}

	return allNewTasks, nil
}

func (p *Processor) extractBatch(ctx context.Context, tasks []*domain.DownloadFile) ([]*domain.DownloadFile, error) {
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

	for task, archive := range archives {
		archiveName := filepath.Base(task.FinalPath)
		p.ctx.Logger.Debug("Extracting %s with %s", archiveName, archive.Name())

		destDir := filepath.Dir(task.FinalPath)
		extractedFile, err := archive.Extract(ctx, task.FinalPath, destDir, task.Password)
		if err != nil {
			p.ctx.Logger.Error("Extraction failed for %s: %v", task.FileName, err)
			continue
		}

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

// CHANGED: move files into completed/<category>/<job-folder>/..., preserving relative paths.
func (p *Processor) moveToCompleted(item *domain.QueueItem, tasks []*domain.DownloadFile) (string, error) {
	if err := os.MkdirAll(p.ctx.Config.Download.CompletedDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create completed directory: %w", err)
	}

	baseDestDir := p.completedBaseDir(item)
	if err := os.MkdirAll(baseDestDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create completed job directory: %w", err)
	}

	for _, task := range tasks {
		fileName := filepath.Base(task.FinalPath)

		if cleanupExtensions(fileName, p.cleanupMap) {
			p.ctx.Logger.Debug("Cleanup: Removing %s", fileName)
			_ = os.Remove(task.FinalPath)
			continue
		}

		relPath := fileName
		if item != nil && item.OutDir != "" {
			if rel, err := filepath.Rel(item.OutDir, task.FinalPath); err == nil && rel != "." &&
				rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				relPath = rel
			}
		}

		dest := filepath.Join(baseDestDir, relPath)
		p.ctx.Logger.Debug("Moving %s to completed folder", fileName)

		if err := moveFile(task.FinalPath, dest); err != nil {
			p.ctx.Logger.Error("Failed cross-device move for %s: %v", task.FileName, err)
			return "", err
		}
	}

	return baseDestDir, nil
}

func (p *Processor) completedBaseDir(item *domain.QueueItem) string {
	base := p.ctx.Config.Download.CompletedDir
	category := "uncategorized"
	jobFolder := "job"

	if item != nil {
		if item.Release != nil && strings.TrimSpace(item.Release.Category) != "" {
			category = sanitizeCompletedPathPart(item.Release.Category)
		}
		if item.OutDir != "" {
			jobFolder = sanitizeCompletedPathPart(filepath.Base(item.OutDir))
		} else if item.ReleaseTitle != "" {
			jobFolder = sanitizeCompletedPathPart(item.ReleaseTitle)
		}
	}

	if category == "" {
		category = "uncategorized"
	}
	if jobFolder == "" {
		jobFolder = "job"
	}

	return filepath.Join(base, category, jobFolder)
}

var completedPathUnsafeRE = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeCompletedPathPart(value string) string {
	value = strings.TrimSpace(value)
	value = completedPathUnsafeRE.ReplaceAllString(value, "_")
	value = strings.Trim(value, "._- ")
	for strings.Contains(value, "__") {
		value = strings.ReplaceAll(value, "__", "_")
	}
	return value
}

// after a successful extraction, keep extracted outputs and drop archive artifacts.
func buildMoveTaskList(tasks, extractedTasks []*domain.DownloadFile) []*domain.DownloadFile {
	if len(extractedTasks) == 0 {
		return tasks
	}

	moveTasks := make([]*domain.DownloadFile, 0, len(tasks)+len(extractedTasks))

	for _, task := range tasks {
		if task == nil {
			continue
		}
		if isArchiveArtifact(task.FinalPath) {
			continue
		}
		moveTasks = append(moveTasks, task)
	}

	moveTasks = append(moveTasks, extractedTasks...)
	return moveTasks
}

func (p *Processor) cleanupWorkDir(workDir, completedDir string) error {
	workDir = strings.TrimSpace(workDir)
	completedDir = strings.TrimSpace(completedDir)

	if workDir == "" {
		return nil
	}

	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("resolve work dir: %w", err)
	}

	absCompletedDir := ""
	if completedDir != "" {
		absCompletedDir, _ = filepath.Abs(completedDir)
	}

	downloadRoot := strings.TrimSpace(p.ctx.Config.Download.OutDir)
	if downloadRoot == "" {
		return nil
	}

	absDownloadRoot, err := filepath.Abs(downloadRoot)
	if err != nil {
		return fmt.Errorf("resolve download root: %w", err)
	}

	// Safety: only cleanup inside the configured download root.
	if absWorkDir != absDownloadRoot && !strings.HasPrefix(absWorkDir, absDownloadRoot+string(filepath.Separator)) {
		return fmt.Errorf("refusing to cleanup work dir outside download root: %s", absWorkDir)
	}

	// Safety: never remove the completed destination.
	if absCompletedDir != "" && (absWorkDir == absCompletedDir ||
		strings.HasPrefix(absCompletedDir, absWorkDir+string(filepath.Separator)) == false && strings.HasPrefix(absWorkDir, absCompletedDir+string(filepath.Separator))) {
		return fmt.Errorf("refusing to cleanup work dir overlapping completed dir: work=%s completed=%s", absWorkDir, absCompletedDir)
	}

	entries, err := os.ReadDir(absWorkDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		path := filepath.Join(absWorkDir, entry.Name())

		if entry.IsDir() {
			continue
		}

		// Remove known downloader leftovers from the original work dir.
		if isArchiveArtifact(path) || cleanupExtensions(entry.Name(), p.cleanupMap) {
			p.ctx.Logger.Debug("Cleanup leftover work file: %s", path)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}

	return removeEmptyDirsUp(absWorkDir, absDownloadRoot)
}

func removeEmptyDirsUp(startDir, stopDir string) error {
	current := startDir

	for {
		current = strings.TrimSpace(current)
		if current == "" {
			return nil
		}

		if samePath(current, stopDir) {
			return nil
		}

		err := os.Remove(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			// Directory not empty is expected once we hit a parent with retained content.
			return nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return aa == bb
}

var archiveArtifactRE = regexp.MustCompile(`(?i)(\.part\d+\.rar|\.rar|\.r\d{2,3}|\.par2|\.vol\d+\+\d+\.par2|\.sfv)$`)

func isArchiveArtifact(path string) bool {
	name := filepath.Base(path)
	return archiveArtifactRE.MatchString(name)
}

func findPrimaryPar(tasks []*domain.DownloadFile) string {
	for _, t := range tasks {
		if strings.HasSuffix(t.FinalPath, ".par2") && !strings.Contains(t.FinalPath, ".vol") {
			return t.FinalPath
		}
	}
	return ""
}

func extractPassword(input string) string {
	re := regexp.MustCompile(`\{\{(.*?)\}\}`)
	match := re.FindStringSubmatch(input)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}
