package indexing

import (
	"context"
	"fmt"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/assemble"
	"github.com/datallboy/gonzb/internal/indexing/release"
	"github.com/datallboy/gonzb/internal/indexing/scheduler"
	"github.com/datallboy/gonzb/internal/indexing/scrape"
)

type Service struct {
	scrape    *scrape.Service
	assemble  *assemble.Service
	release   *release.Service
	scheduler *scheduler.Service
}

func NewService(
	scrapeSvc *scrape.Service,
	assembleSvc *assemble.Service,
	releaseSvc *release.Service,
	schedulerSvc *scheduler.Service,
) *Service {
	return &Service{
		scrape:    scrapeSvc,
		assemble:  assembleSvc,
		release:   releaseSvc,
		scheduler: schedulerSvc,
	}
}

func (s *Service) ScrapeOnce(ctx context.Context) error {
	if s.scrape == nil {
		return fmt.Errorf("scrape service is not configured")
	}
	return s.scrape.RunOnce(ctx)
}

func (s *Service) Start(ctx context.Context, interval time.Duration) error {
	_ = interval
	if s.scheduler == nil {
		return fmt.Errorf("scheduler service is not configured")
	}
	return s.scheduler.Run(ctx)
}

func (s *Service) RunPipelineOnce(ctx context.Context) error {
	if s.scrape != nil {
		if err := s.scrape.RunOnce(ctx); err != nil {
			return fmt.Errorf("scrape pass failed: %w", err)
		}
	}
	if s.assemble == nil {
		return fmt.Errorf("assemble service is not configured")
	}
	if err := s.assemble.RunOnce(ctx); err != nil {
		return fmt.Errorf("assemble pass failed: %w", err)
	}
	if s.release == nil {
		return fmt.Errorf("release service is not configured")
	}
	if err := s.release.RunOnce(ctx); err != nil {
		return fmt.Errorf("release pass failed: %w", err)
	}
	return nil
}

func (s *Service) ReleaseOnce(ctx context.Context) error {
	if s.release == nil {
		return fmt.Errorf("release service is not configured")
	}
	return s.release.RunOnce(ctx)
}

func (s *Service) AssembleOnce(ctx context.Context) error {
	if s.assemble == nil {
		return fmt.Errorf("assemble service is not configured")
	}
	return s.assemble.RunOnce(ctx)
}
