package downloader

import (
	"context"
	"gonzb/internal/decoding"
	"gonzb/internal/domain"
	"gonzb/internal/nntp"
	"io"
	"log"
	"os"
	"sync"
)

func (s *Service) runWorkerPool(ctx context.Context, nzb *domain.NZB) error {
	jobs := make(chan domain.DownloadJob)
	results := make(chan domain.DownloadResult)
	var wg sync.WaitGroup

	// Start workers
	for w := 1; w <= s.cfg.Download.MaxWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.worker(ctx, id, jobs, results)
		}(w)
	}

	// Feed jobs and collect results in separate goroutines to avoid deadlocks
	go s.dispatchJobs(nzb, jobs)

	// Result collector
	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		if res.Error != nil {
			log.Printf("Segment error: %v", res.Error)
			// Implementation of a "Stop on first error" or "Retry" would go here
		}
	}
	return nil
}

func (s *Service) worker(ctx context.Context, id int, jobs <-chan domain.DownloadJob, results chan<- domain.DownloadResult) {
	repo := nntp.NewRepository(s.cfg.Server)
	if err := repo.Authenticate(); err != nil {
		return
	}

	// Ensure we tell the server "QUIT" when the worker exits
	defer func() {
		// repo.Close() should ideally send the NNTP 'QUIT' command
		// before closing the TCP socket.
		repo.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled (Ctrl+C), exit worker
			return
		case job, ok := <-jobs:
			if !ok {
				return // No more jobs
			}

			err := s.processSegment(repo, job)
			results <- domain.DownloadResult{Segment: job.Segment, Error: err}
		}
	}
}

func (s *Service) processSegment(repo domain.ArticleRespository, job domain.DownloadJob) error {
	rawReader, err := repo.FetchBody(job.Segment.MessageID)
	if err != nil {
		return err
	}

	// Wrap with yEnc Decoder
	decoder := decoding.NewYencDecoder(rawReader)
	if err := decoder.DiscardHeader(); err != nil {
		return err
	}

	// Create Temp File
	f, err := os.Create(job.FilePath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Stream through decoder to disk
	if _, err := io.Copy(f, decoder); err != nil {
		return err
	}

	// Verify CRC32
	return decoder.Verify()
}
