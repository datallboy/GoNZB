package scheduler

import (
	"context"
	"fmt"
	"time"
)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type scrapeRunner interface {
	RunOnce(ctx context.Context) error
}

type Service struct {
	runner   scrapeRunner
	log      logger
	interval time.Duration
}

func NewService(runner scrapeRunner, log logger, interval time.Duration) *Service {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	return &Service{
		runner:   runner,
		log:      log,
		interval: interval,
	}
}

// Run starts periodic scrape passes until context cancellation.
func (s *Service) Run(ctx context.Context) error {
	if s.runner == nil {
		return fmt.Errorf("scheduler runner is required")
	}

	s.log.Info("index scheduler started, interval=%s", s.interval.String())

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Run one immediately on startup.
	if err := s.runner.RunOnce(ctx); err != nil {
		s.log.Error("index scheduler initial run failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			s.log.Info("index scheduler stopped")
			return nil
		case <-ticker.C:
			if err := s.runner.RunOnce(ctx); err != nil {
				s.log.Error("index scheduler run failed: %v", err)
			}
		}
	}
}
