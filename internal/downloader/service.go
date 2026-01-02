package downloader

import (
	"context"
	"fmt"
	"gonzb/internal/config"
	"gonzb/internal/domain"
	"gonzb/internal/logger"
	"gonzb/internal/provider"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		// Allocate a 1MB buffer (typical max size for a Usenet segment)
		return make([]byte, 1024*1024)
	},
}

type Service struct {
	cfg          *config.Config
	manager      *provider.Manager
	logger       *logger.Logger
	writer       *FileWriter
	bytesWritten uint64
	totalBytes   uint64
}

func NewService(c *config.Config, mgr *provider.Manager, l *logger.Logger) *Service {
	return &Service{
		cfg:     c,
		manager: mgr,
		logger:  l,
		writer:  NewFileWriter(),
	}
}

func (s *Service) Download(ctx context.Context, nzb *domain.NZB) error {
	defer s.writer.CloseAll()

	if err := os.MkdirAll(s.cfg.Download.OutDir, 0755); err != nil {
		return fmt.Errorf("failed to create out_dir: %w", err)
	}

	var filesToProcess []domain.NZBFile

	// Pre-allocate Sparse Files (.part)
	for _, file := range nzb.Files {
		cleanName := s.sanitizeFileName(file.Subject)
		finalPath := filepath.Join(s.cfg.Download.OutDir, cleanName)
		pathPath := finalPath + ".part"

		// Check if the file is already 100% finished
		if _, err := os.Stat(finalPath); err == nil {
			s.logger.Info("Found finished file %s (Skipping)", cleanName)
			continue
		}

		// Create the sparse file so workers have a target
		if err := s.writer.PreAllocate(pathPath, file.TotalSize()); err != nil {
			return fmt.Errorf("failed to pre-allocate %s %w", cleanName, err)
		}

		filesToProcess = append(filesToProcess, file)
	}

	// Reset counters for new job
	s.bytesWritten = 0
	s.totalBytes = 0
	for _, f := range filesToProcess {
		s.totalBytes += uint64(f.TotalSize())
	}

	if len(filesToProcess) == 0 {
		s.logger.Info("All files are already present. No downloaded needed.")
		return nil
	}

	s.logger.Info("Starting download...")

	startTime := time.Now()
	monitorCtx, cancel := context.WithCancel(ctx)
	fmt.Print("\n\n")
	go s.startUI(monitorCtx, startTime)

	// Call worker pool with remaining files to process
	workNZB := &domain.NZB{Files: filesToProcess}

	err := s.runWorkerPool(ctx, workNZB, s.writer)

	cancel() // Stop the UI when workers are done
	s.renderUI(0, startTime, true)
	fmt.Print("\n\n") // Print newline after the progress bar finishes

	if err != nil {
		return err
	}

	// Finialize: Close handles and rename .part -> final
	for _, file := range nzb.Files {
		cleanName := s.sanitizeFileName(file.Subject)
		finalPath := filepath.Join(s.cfg.Download.OutDir, cleanName)
		partPath := finalPath + ".part"

		// Close handle so OS releases the lock for renaming
		if err := s.writer.CloseFile(partPath); err != nil {
			s.logger.Warn("Warning: failed to close %s: %v", partPath, err)
		}

		if err := os.Rename(partPath, finalPath); err != nil {
			return fmt.Errorf("failed to finalize %s: %w", cleanName, err)
		}
		s.logger.Info("Finished: %s", cleanName)
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

func (s *Service) startUI(ctx context.Context, startTime time.Time) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastBytes uint64

	for {
		select {
		case <-ticker.C:
			current := atomic.LoadUint64(&s.bytesWritten)
			delta := current - lastBytes
			lastBytes = current

			// Calculate instantaneous speed
			speedMbps := float64(delta) * 8 / (1024 * 1024)

			s.renderUI(speedMbps, startTime, false)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) renderUI(speedMbps float64, startTime time.Time, final bool) {
	current := atomic.LoadUint64(&s.bytesWritten)
	total := s.totalBytes
	if total == 0 {
		return
	}

	// Average Speed & ETA
	elapsed := time.Since(startTime)
	percent := float64(current) / float64(total) * 100

	displaySpeed := speedMbps
	etaStr := "calc..."

	if final {
		percent = 100.0

		// Guard against division by zero or sub-millisecond durations
		seconds := elapsed.Seconds()
		if seconds < 0.1 {
			seconds = 0.1
		}

		avgBytesPerSec := float64(current) / seconds
		displaySpeed = (avgBytesPerSec * 8) / (1024 * 1024)

		// If we didn't actually download anything (all skipped), show 0
		if current == 0 {
			displaySpeed = 0
		}
	} else {
		avgBytesPerSec := float64(current) / elapsed.Seconds()
		if avgBytesPerSec > 0 {
			remainingBytes := total - current
			etaSeconds := int(float64(remainingBytes) / avgBytesPerSec)
			etaStr = (time.Duration(etaSeconds) * time.Second).String()
		}
	}

	// Progress Bar go brrr [====>   ]
	const barWidth = 20
	completedWidth := int(percent / 100 * barWidth)
	bar := strings.Repeat("=", completedWidth)
	if completedWidth < barWidth {
		bar += ">" + strings.Repeat(" ", barWidth-completedWidth-1)
	}

	// Print UI: [Bar] 50% | Speed: 100 Mbps | ETA: 2m30s | 500/1000 MB
	speedLabel := "Speed"
	timeLabel := "ETA"
	if final {
		speedLabel = "Avg"
		timeLabel = "Time"
		etaStr = elapsed.Truncate(time.Second).String()
	}

	fmt.Printf("\r[%s] %5.1f%% | %s: %6.2f Mbps | %s: %-7s | %d/%d MB      ",
		bar, percent, speedLabel, displaySpeed, timeLabel, etaStr, current/1024/1024, total/1024/1024)
}

func (s *Service) reportProgress(n int) {
	atomic.AddUint64(&s.bytesWritten, uint64(n))
}
