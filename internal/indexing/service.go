package indexing

import (
	"context"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/scheduler"
	"github.com/datallboy/gonzb/internal/indexing/scrape"
)

type Service struct {
	scrape    *scrape.Service
	scheduler *scheduler.Service
}

func NewService(scrapeSvc *scrape.Service, schedulerSvc *scheduler.Service) *Service {
	return &Service{
		scrape:    scrapeSvc,
		scheduler: schedulerSvc,
	}
}

func (s *Service) ScrapeOnce(ctx context.Context) error {
	return s.scrape.RunOnce(ctx)
}

func (s *Service) Start(ctx context.Context, interval time.Duration) error {
	_ = interval
	return s.scheduler.Run(ctx)
}
