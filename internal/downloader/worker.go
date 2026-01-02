package downloader

import (
	"context"
	"errors"
	"fmt"
	"gonzb/internal/decoding"
	"gonzb/internal/domain"
	"io"
	"math"
	"sync"
	"time"
)

// runWorkerPool orchestrates the lifecycle of the download process.
func (s *Service) runWorkerPool(ctx context.Context, tasks []*domain.DownloadFile) error {
	totalSegments := 0
	for _, f := range tasks {
		totalSegments += len(f.Segments)
	}

	// Ask the manager for the connection limit
	capacity := s.manager.TotalCapacity()
	if capacity <= 0 {
		return fmt.Errorf("no download capacity available: check server max_connections")
	}

	// Dynamically define workers and buffers based on max_connection capacity
	// Add 2 extra workers to ensure there's alyways a worker waiting for a slot
	workerCount := capacity + 2
	bufferSize := workerCount * 2

	jobs := make(chan domain.DownloadJob, bufferSize)
	results := make(chan domain.DownloadResult, bufferSize)

	// Start the Workers
	var wg sync.WaitGroup
	for w := 1; w <= workerCount; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.worker(ctx, jobs, results)
		}(w)
	}

	// Dispatch Jobs
	go s.dispatchJobs(tasks, jobs)

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
				isBusy := errors.Is(res.Error, domain.ErrProviderBusy)

				// If we have retires left, put it back in the pipeline
				if isBusy || res.Job.RetryCount < 3 {
					delay := 100 * time.Millisecond // quick retry for busy error

					if !isBusy {
						res.Job.RetryCount++

						// Calculate backoff: 2s, 4s, 8s...
						delay = time.Duration(math.Pow(2, float64(res.Job.RetryCount))) * time.Second

						s.logger.Warn("[Retry] Segment %s: Attempt %d/3 - Error: %v",
							res.Job.Segment.MessageID, res.Job.RetryCount, res.Error)
					}

					// Use a timer to re-queue the job so we don't block this loop
					time.AfterFunc(delay, func() {
						jobs <- res.Job
					})

					continue // Do not count as completed yet
				}
				// Permanent failure
				s.logger.Error("[FAIL] Segment %s permanently failed: %v", res.Job.Segment.MessageID, res.Error)
				finalErr = fmt.Errorf("one or more segments failed permanently")
			}
			completedCount++
		}
	}
	close(jobs)
	wg.Wait()
	return finalErr
}

// worker pulls jobs from the channel and executes them until channel is closed
func (s *Service) worker(ctx context.Context, jobs <-chan domain.DownloadJob, results chan<- domain.DownloadResult) {
	for job := range jobs {
		select {
		case <-ctx.Done():
			return
		default:
			err := s.processSegment(ctx, job)
			results <- domain.DownloadResult{Job: job, Error: err}
		}
	}
}

// processSegment handles the unique pipleine for a single Usenet article
func (s *Service) processSegment(ctx context.Context, job domain.DownloadJob) error {
	// Fetch from the Manager (handles priorities, auth, and connections)
	rawReader, err := s.manager.FetchArticle(ctx, job.Segment.MessageID, job.Groups)
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
	decoder := decoding.NewYencDecoder(rawReader)

	if err := decoder.DiscardHeader(); err != nil {
		return fmt.Errorf("header error: %w", err)
	}

	data := make([]byte, job.Segment.Bytes)

	// Read decoded data into buffer
	// Limit the read to the expected segment size
	_, err = io.ReadFull(decoder, data)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return fmt.Errorf("decode read failed: %w", err)
	}

	// Verify CRC32
	if err := decoder.Verify(); err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}

	// Write only the number of bytes actually read (n)
	err = s.writer.Write(job.File.PartPath, job.Offset, data)
	if err != nil {
		return fmt.Errorf("write error %w", err)
	}

	// Update progress bar / cli UI
	s.reportProgress(len(data))

	return nil
}

// dispatchJobs translates the NZB structure into individual segment jobs.
func (s *Service) dispatchJobs(tasks []*domain.DownloadFile, jobs chan<- domain.DownloadJob) {
	for _, task := range tasks {
		var currentOffset int64 = 0

		var groups []string
		if task.Source != nil {
			groups = task.Source.Groups
		}

		for _, seg := range task.Segments {
			jobs <- domain.DownloadJob{
				Segment:    seg,
				File:       task,
				Groups:     groups,
				Offset:     currentOffset,
				RetryCount: 0,
			}
			currentOffset += seg.Bytes
		}
	}
}
