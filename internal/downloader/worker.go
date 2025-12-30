package downloader

import (
	"context"
	"gonzb/internal/decoding"
	"gonzb/internal/domain"
	"gonzb/internal/nntp"
	"io"
	"log"
	"sync"
)

func (s *Service) runWorkerPool(ctx context.Context, nzb *domain.NZB, writer *FileWriter) error {
	jobs := make(chan domain.DownloadJob)
	results := make(chan domain.DownloadResult)
	var wg sync.WaitGroup

	// Start workers
	for w := 1; w <= s.cfg.Download.MaxWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.worker(ctx, id, jobs, results, writer)
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

func (s *Service) worker(ctx context.Context, id int, jobs <-chan domain.DownloadJob, results chan<- domain.DownloadResult, writer *FileWriter) {
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

			err := s.processSegment(repo, job, writer)
			results <- domain.DownloadResult{Segment: job.Segment, Error: err}
		}
	}
}

func (s *Service) processSegment(repo domain.ArticleRespository, job domain.DownloadJob, writer *FileWriter) error {
	rawReader, err := repo.FetchBody(job.Segment.MessageID)
	if err != nil {
		return err
	}

	// Get a buffer from the pool
	buf := bufferPool.Get().([]byte)
	defer bufferPool.Put(buf) // Put it back when we are done

	// We'll need a way to wrap this for the decoder.
	// A bytes.Buffer might be easier, or we can write a custom limited reader.
	// For now, let's use a simple approach to keep the logic clear.
	decoder := decoding.NewYencDecoder(rawReader)
	if err := decoder.DiscardHeader(); err != nil {
		return err
	}

	// Decode directly into the pooled slice
	n, err := io.ReadFull(decoder, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return err
	}

	if err := decoder.Verify(); err != nil {
		return err
	}

	// Write only the number of bytes actually read (n)
	return writer.Write(job.FilePath, job.Offset, buf[:n])
}
