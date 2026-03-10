package scrape

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type repository interface {
	EnsureProvider(ctx context.Context, providerKey, displayName string) (int64, error)
	EnsureNewsgroup(ctx context.Context, groupName string) (int64, error)

	StartScrapeRun(ctx context.Context, providerID int64) (int64, error)
	FinishScrapeRun(ctx context.Context, runID int64, status, errorText string) error

	GetCheckpoint(ctx context.Context, providerID, newsgroupID int64) (int64, error)
	UpsertCheckpoint(ctx context.Context, providerID, newsgroupID, lastArticleNumber int64) error

	InsertArticleHeaders(ctx context.Context, providerID, newsgroupID int64, headers []pgindex.ArticleHeader) (int64, error)
}

type provider interface {
	ID() string
	GroupStats(ctx context.Context, group string) (GroupStats, error)
	XOver(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error)
}

type GroupStats struct {
	Low  int64
	High int64
}

type OverviewHeader struct {
	ArticleNumber int64
	MessageID     string
	Subject       string
	Poster        string
	DateUTC       *time.Time
	Bytes         int64
	Lines         int
	Xref          string
	RawOverview   map[string]any
}

type Options struct {
	Newsgroups []string
	BatchSize  int64
}

type Service struct {
	repo     repository
	provider provider
	log      logger
	opts     Options
}

func NewService(repo repository, p provider, log logger, opts Options) *Service {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 5000
	}

	return &Service{
		repo:     repo,
		provider: p,
		log:      log,
		opts:     opts,
	}
}

// RunOnce executes one scrape pass across configured groups and advances checkpoints.
func (s *Service) RunOnce(ctx context.Context) error {
	if s.repo == nil {
		return fmt.Errorf("scrape repo is required")
	}
	if s.provider == nil {
		return fmt.Errorf("scrape provider is required")
	}
	if len(s.opts.Newsgroups) == 0 {
		return fmt.Errorf("at least one newsgroup is required")
	}

	providerKey := strings.TrimSpace(s.provider.ID())
	if providerKey == "" {
		return fmt.Errorf("provider id is required")
	}

	providerID, err := s.repo.EnsureProvider(ctx, providerKey, providerKey)
	if err != nil {
		return fmt.Errorf("ensure provider: %w", err)
	}

	runID, err := s.repo.StartScrapeRun(ctx, providerID)
	if err != nil {
		return fmt.Errorf("start scrape run: %w", err)
	}

	runErr := s.runGroups(ctx, providerID)

	status := "completed"
	errText := ""
	if runErr != nil {
		status = "failed"
		errText = runErr.Error()
	}

	if finishErr := s.repo.FinishScrapeRun(ctx, runID, status, errText); finishErr != nil {
		if runErr != nil {
			return fmt.Errorf("%v (also failed to finish scrape run: %w)", runErr, finishErr)
		}
		return fmt.Errorf("finish scrape run: %w", finishErr)
	}

	return runErr
}

func (s *Service) runGroups(ctx context.Context, providerID int64) error {
	for _, g := range s.opts.Newsgroups {
		group := strings.TrimSpace(g)
		if group == "" {
			continue
		}

		if err := s.runGroup(ctx, providerID, group); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) runGroup(ctx context.Context, providerID int64, group string) error {
	newsgroupID, err := s.repo.EnsureNewsgroup(ctx, group)
	if err != nil {
		return fmt.Errorf("ensure newsgroup %s: %w", group, err)
	}

	stats, err := s.provider.GroupStats(ctx, group)
	if err != nil {
		return fmt.Errorf("group stats %s: %w", group, err)
	}
	if stats.High <= 0 {
		s.log.Warn("scrape: group %s has no articles yet", group)
		return nil
	}

	last, err := s.repo.GetCheckpoint(ctx, providerID, newsgroupID)
	if err != nil {
		return fmt.Errorf("get checkpoint %s: %w", group, err)
	}

	start := last + 1
	if start < stats.Low {
		start = stats.Low
	}
	if start > stats.High {
		s.log.Debug("scrape: group %s up-to-date at %d", group, last)
		return nil
	}

	s.log.Info("scrape: group=%s start=%d high=%d batch=%d", group, start, stats.High, s.opts.BatchSize)

	for from := start; from <= stats.High; from += s.opts.BatchSize {
		to := from + s.opts.BatchSize - 1
		if to > stats.High {
			to = stats.High
		}

		rows, err := s.provider.XOver(ctx, group, from, to)
		if err != nil {
			return fmt.Errorf("xover %s %d-%d: %w", group, from, to, err)
		}

		headers := make([]pgindex.ArticleHeader, 0, len(rows))
		for _, r := range rows {
			if r.ArticleNumber <= 0 || strings.TrimSpace(r.MessageID) == "" {
				continue
			}
			headers = append(headers, pgindex.ArticleHeader{
				ArticleNumber: r.ArticleNumber,
				MessageID:     r.MessageID,
				Subject:       r.Subject,
				Poster:        r.Poster,
				DateUTC:       r.DateUTC,
				Bytes:         r.Bytes,
				Lines:         r.Lines,
				Xref:          r.Xref,
				RawOverview:   r.RawOverview,
			})
		}

		inserted, err := s.repo.InsertArticleHeaders(ctx, providerID, newsgroupID, headers)
		if err != nil {
			return fmt.Errorf("insert headers %s %d-%d: %w", group, from, to, err)
		}

		// Advance checkpoint to requested range end so scraper continues forward
		// even when server omits some article numbers.
		if err := s.repo.UpsertCheckpoint(ctx, providerID, newsgroupID, to); err != nil {
			return fmt.Errorf("upsert checkpoint %s=%d: %w", group, to, err)
		}

		s.log.Info("scrape: group=%s range=%d-%d fetched=%d inserted=%d", group, from, to, len(rows), inserted)
	}

	return nil
}
