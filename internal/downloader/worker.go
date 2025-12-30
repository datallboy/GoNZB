package downloader

import (
	"context"
	"fmt"
	"gonzb/internal/decoding"
	"gonzb/internal/domain"
	"io"
	"log"
	"math"
	"path/filepath"
	"sync"
	"time"
)

// runWorkerPool orchestrates the lifecycle of the download process.
func (s *Service) runWorkerPool(ctx context.Context, nzb *domain.NZB, writer *FileWriter) error {
	totalSegments := 0
	for _, f := range nzb.Files {
		totalSegments += len(f.Segments)
	}

	jobs := make(chan domain.DownloadJob, totalSegments*2)
	results := make(chan domain.DownloadResult, totalSegments)

	// Start the Workers
	var wg sync.WaitGroup
	for w := 1; w <= s.cfg.Download.MaxWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.worker(ctx, jobs, results, writer)
		}(w)
	}

	// Dispatch Jobs
	go s.dispatchJobs(nzb, jobs)

	// Collect Results
	completedCount := 0
	var finalErr error

	for completedCount < totalSegments {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case res := <-results:
			if res.Error != nil {
				// If we have retires left, put it back in the pipeline
				if res.Job.RetryCount < 3 {
					res.Job.RetryCount++

					// Calculate backoff: 2s, 4s, 8s...
					delay := time.Duration(math.Pow(2, float64(res.Job.RetryCount))) * time.Second

					log.Printf("[Retry] Segment %s (Attempt %d)", res.Segment.MessageID, res.Job.RetryCount)

					// Use a timer to re-queue the job so we don't block this loop
					time.AfterFunc(delay, func() {
						jobs <- res.Job
					})

					continue // Do not count as completed yet
				}
				// Permanent failure
				log.Printf("[FAIL] Segment %s permanently failed: %v", res.Segment.MessageID, res.Error)
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
func (s *Service) worker(ctx context.Context, jobs <-chan domain.DownloadJob, results chan<- domain.DownloadResult, writer *FileWriter) {
	for job := range jobs {
		select {
		case <-ctx.Done():
			return
		default:
			err := s.processSegment(ctx, job, writer)
			results <- domain.DownloadResult{Segment: job.Segment, Job: job, Error: err}
		}
	}
}

// processSegment handles the unique pipleine for a single Usenet article
func (s *Service) processSegment(ctx context.Context, job domain.DownloadJob, writer *FileWriter) error {
	// Fetch from the Manager (handles priorities, auth, and connections)
	rawReader, err := s.manager.FetchArticle(ctx, job.Segment.MessageID)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	// The manager returns io.Reader. We must ensure it's closed (or read to EOF)
	// to release the connection slot back to the provider.
	if closer, ok := rawReader.(io.ReadCloser); ok {
		defer closer.Close()
	}

	// Get a buffer from the pool
	buf := bufferPool.Get().([]byte)
	defer bufferPool.Put(buf) // Put it back when we are done

	// Decode yEnc stream
	decoder := decoding.NewYencDecoder(rawReader)
	if err := decoder.DiscardHeader(); err != nil {
		return fmt.Errorf("yenc header error: %w", err)
	}

	// Read decoded data into buffer
	// Limit the read to the expected segment size
	n, err := io.ReadFull(decoder, buf[:job.Segment.Bytes])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return fmt.Errorf("decode read failed: %w", err)
	}

	// Verify CRC32
	if err := decoder.Verify(); err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}

	// Write only the number of bytes actually read (n)
	return writer.Write(job.FilePath, job.Offset, buf[:n])
}

// dispatchJobs translates the NZB structure into individual segment jobs.
func (s *Service) dispatchJobs(nzb *domain.NZB, jobs chan<- domain.DownloadJob) {
	for _, file := range nzb.Files {
		var currentOffset int64 = 0
		cleanName := s.sanitizeFileName(file.Subject)
		// Write the the .part files during download
		partPath := filepath.Join(s.cfg.Download.OutDir, cleanName+".part")

		for _, seg := range file.Segments {
			jobs <- domain.DownloadJob{
				Segment:  seg,
				FilePath: partPath,
				Offset:   currentOffset,
			}
			currentOffset += int64(seg.Bytes)
		}
	}
}
