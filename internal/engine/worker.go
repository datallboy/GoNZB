package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/datallboy/gonzb/internal/nzb"
)

// runWorkerPool orchestrates the lifecycle of the download process.
func (s *Downloader) runWorkerPool(ctx context.Context, item *domain.QueueItem) error {
	totalSegments := 0
	for _, f := range item.Tasks {
		// Only count segments for files that aren't already finished
		if !f.IsComplete {
			totalSegments += len(f.Segments)
		}
	}

	// If everything is already downloaded, exit early!
	if totalSegments == 0 {
		return nil
	}

	// Create context for the workers that we can cancel
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	// Ask the manager for the connection limit
	capacity := s.ctx.NNTP.TotalCapacity()
	if capacity <= 0 {
		return fmt.Errorf("no download capacity available: check server max_connections")
	}

	// Dynamically define workers and buffers based on max_connection capacity
	// Add 2 extra workers to ensure there's alyways a worker waiting for a slot
	workerCount := capacity + 2
	bufferSize := workerCount * 2

	jobs := make(chan DownloadJob, bufferSize)
	results := make(chan DownloadResult, bufferSize)

	// Start the Workers
	var wg sync.WaitGroup

	for w := 1; w <= workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.worker(workerCtx, item, jobs, results)
		}()
	}

	// Dispatch Jobs
	go s.dispatchJobs(workerCtx, item.Tasks, jobs)

	// Collect Results
	completedCount := 0
	var finalErr error

	for completedCount < totalSegments {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case res := <-results:
			if res.Error != nil {
				// Identify the error type
				isBusy := errors.Is(res.Error, nntp.ErrProviderBusy)
				isMissing := errors.Is(res.Error, nntp.ErrArticleNotFound)

				// If we have retires left, put it back in the pipeline
				if (isBusy || isMissing) && res.Job.RetryCount < 3 {
					delay := 100 * time.Millisecond // quick retry for busy error

					if !isBusy {
						res.Job.RetryCount++
						delay = time.Duration(math.Pow(2, float64(res.Job.RetryCount))) * time.Second

						s.ctx.Logger.Debug("[Retry] Segment %s: Attempt %d/3 - Error: %v",
							res.Job.Segment.MessageID, res.Job.RetryCount, res.Error)

					}
					go func(j DownloadJob, d time.Duration) {
						time.Sleep(d)
						select {
						case <-workerCtx.Done():
						case jobs <- j:
						}
					}(res.Job, delay)

					continue // Do not count as completed yet
				}
				// Permanent failure
				s.ctx.Logger.Error("[FAIL] Segment %s permanently failed: %v", res.Job.Segment.MessageID, res.Error)
				finalErr = fmt.Errorf("one or more segments failed permanently")
			}
			completedCount++
		}
	}

	cancelWorkers()
	wg.Wait()
	return finalErr
}

// worker pulls jobs from the channel and executes them until channel is closed
func (s *Downloader) worker(ctx context.Context, item *domain.QueueItem, jobs <-chan DownloadJob, results chan<- DownloadResult) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}
			err := s.processSegment(ctx, item, job)
			results <- DownloadResult{Job: job, Error: err}
		}
	}
}

// processSegment handles the unique pipleine for a single Usenet article
func (s *Downloader) processSegment(ctx context.Context, item *domain.QueueItem, job DownloadJob) error {
	// Fetch from the Manager (handles priorities, auth, and connections)
	rawReader, err := s.ctx.NNTP.Fetch(ctx, job.Segment.MessageID, job.Groups)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	if rawReader == nil {
		return fmt.Errorf("manager returned nil reader for %s", job.Segment.MessageID)
	}

	// The manager returns io.Reader. We must ensure it's closed (or read to EOF)
	// to release the connection slot back to the provider.
	if closer, ok := rawReader.(io.ReadCloser); ok {
		defer closer.Close()
	}

	// Decode yEnc stream
	decoder := nzb.NewYencDecoder(rawReader)

	if err := decoder.DiscardHeader(); err != nil {
		return fmt.Errorf("header error: %w", err)
	}

	if decoder.FileSize > 0 {
		job.File.SetActualSize(decoder.FileSize)
	}

	// use the yEnc header offset if it exists
	// If partOffset is 0, likely a single-seg file. So just use job.Offset.
	writeOffset := decoder.PartOffset
	if writeOffset == 0 && job.Offset != 0 {
		writeOffset = job.Offset
	}

	data := make([]byte, job.Segment.Bytes)

	// Read decoded data into buffer
	// Limit the read to the expected segment size
	n, err := io.ReadFull(decoder, data)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return fmt.Errorf("decode read failed: %w", err)
	}

	// Verify CRC32
	if err := decoder.Verify(); err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}

	// Write only the number of bytes actually read (n)
	if n > 0 {
		err = s.writer.WriteAt(job.File.PartPath, data[:n], writeOffset)
		if err != nil {
			return fmt.Errorf("write error %w", err)
		}

		// Update progress
		item.BytesWritten.Add(int64(n))
	}

	return nil
}

// dispatchJobs translates the NZB structure into individual segment jobs.
func (s *Downloader) dispatchJobs(ctx context.Context, tasks []*domain.DownloadFile, jobs chan<- DownloadJob) {
	for _, task := range tasks {
		if task.IsComplete {
			s.ctx.Logger.Debug("Skipping segment dispatch: %s (already on disk)", task.FileName)
			continue
		}

		var currentOffset int64 = 0

		groups := task.Groups

		for _, seg := range task.Segments {
			select {
			case <-ctx.Done():
				return // stop dispatching if job is cancelled
			case jobs <- DownloadJob{
				Segment:    seg,
				File:       task,
				Groups:     groups,
				Offset:     currentOffset,
				RetryCount: 0,
			}:
				currentOffset += seg.Bytes

			}

		}
	}
}
