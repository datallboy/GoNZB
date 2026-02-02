package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/processor"

	"github.com/datallboy/gonzb/internal/nntp"
)

// Downloader is the concrete implementation of the download engine.
type Downloader struct {
	ctx       *app.Context
	nntp      *nntp.Manager
	processor *processor.Processor
	writer    *FileWriter
}

func NewDownloader(ctx *app.Context, writer *FileWriter) *Downloader {
	return &Downloader{
		ctx:       ctx,
		nntp:      ctx.NNTP.(*nntp.Manager),
		processor: ctx.Processor.(*processor.Processor),
		writer:    writer,
	}
}

// Download processes a QueueItem from start to finish
func (s *Downloader) Download(ctx context.Context, item *domain.QueueItem) error {
	defer s.writer.CloseAll()

	if err := os.MkdirAll(s.ctx.Config.Download.OutDir, 0755); err != nil {
		return fmt.Errorf("failed to create out_dir: %w", err)
	}

	// PREPARE: Sanitize names and pre-allocate .part files
	tasks, err := s.processor.Prepare(item.NZBModel, item.Name)
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		s.ctx.Logger.Info("All files are already present. No download needed.")
		return nil
	}

	item.Tasks = tasks

	// Reset counters for new job
	item.BytesWritten.Store(0)
	var totalSize uint64
	for _, t := range tasks {
		totalSize += uint64(t.Size)
	}
	item.TotalBytes = totalSize

	s.ctx.Logger.Info("Starting download for: %s (%d MB)", item.Name, item.TotalBytes/1024/1024)

	item.StartedAt = time.Now()

	err = s.runWorkerPool(ctx, item)
	if err != nil {
		s.writer.CloseAll()
		return err
	}

	// Finialize: Close handles and rename .part -> final
	if err := s.processor.Finalize(ctx, tasks); err != nil {
		return fmt.Errorf("post-processing failed: %w", err)
	}

	// Post Process: PAR2 verify, repair, unrar if needed
	// Update status to 'processing' so the WebUI shows we are working on the disk
	item.Status = domain.StatusProcessing
	if err := s.processor.PostProcess(ctx, tasks); err != nil {
		// Download is "done" but failed repair/verify
		// TODO: decide if should return an error or consider the file "good enough"
		s.ctx.Logger.Error("Post-processing failed: %v", err)
	}

	return nil
}

func (s *Downloader) StartCLIProgress(ctx context.Context, item *domain.QueueItem) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastBytes uint64

	for {
		select {
		case <-ticker.C:
			current := item.BytesWritten.Load()
			delta := current - lastBytes
			lastBytes = current

			// Calculate instantaneous speed
			speedMbps := float64(delta) * 8 / (1024 * 1024)

			s.renderCLIProgress(item, speedMbps, false)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Downloader) renderCLIProgress(item *domain.QueueItem, speedMbps float64, final bool) {
	current := item.BytesWritten.Load()
	total := item.TotalBytes
	if total == 0 {
		return
	}

	// Average Speed & ETA
	elapsed := time.Since(item.StartedAt)
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
