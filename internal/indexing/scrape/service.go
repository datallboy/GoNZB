package scrape

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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

	GetLatestCheckpoint(ctx context.Context, providerID, newsgroupID int64) (int64, error)
	UpsertLatestCheckpoint(ctx context.Context, providerID, newsgroupID, lastArticleNumber int64) error
	GetBackfillCheckpoint(ctx context.Context, providerID, newsgroupID int64) (int64, error)
	UpsertBackfillCheckpoint(ctx context.Context, providerID, newsgroupID, backfillArticleNumber int64) error

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

// backward-compatible alias. Default scrape mode is latest.
func (s *Service) RunOnce(ctx context.Context) error {
	return s.RunLatestOnce(ctx)
}

// latest mode prioritizes the head of the group and continues forward.
func (s *Service) RunLatestOnce(ctx context.Context) error {
	return s.runMode(ctx, "latest")
}

// backfill mode walks backward from the most recent known boundary.
func (s *Service) RunBackfillOnce(ctx context.Context) error {
	return s.runMode(ctx, "backfill")
}

func (s *Service) runMode(ctx context.Context, mode string) error {
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

	runErr := s.runGroups(ctx, providerID, mode)

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

func (s *Service) runGroups(ctx context.Context, providerID int64, mode string) error {
	for _, g := range s.opts.Newsgroups {
		group := strings.TrimSpace(g)
		if group == "" {
			continue
		}

		var err error
		switch mode {
		case "latest":
			err = s.runLatestGroup(ctx, providerID, group)
		case "backfill":
			err = s.runBackfillGroup(ctx, providerID, group)
		default:
			err = fmt.Errorf("unsupported scrape mode %q", mode)
		}

		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) runLatestGroup(ctx context.Context, providerID int64, group string) error {
	newsgroupID, err := s.repo.EnsureNewsgroup(ctx, group)
	if err != nil {
		return fmt.Errorf("ensure newsgroup %s: %w", group, err)
	}

	stats, err := s.provider.GroupStats(ctx, group)
	if err != nil {
		return fmt.Errorf("group stats %s: %w", group, err)
	}
	if stats.High <= 0 {
		s.log.Warn("scrape latest: group %s has no articles yet", group)
		return nil
	}

	last, err := s.repo.GetLatestCheckpoint(ctx, providerID, newsgroupID)
	if err != nil {
		return fmt.Errorf("get latest checkpoint %s: %w", group, err)
	}

	// cold start begins near the head, not the oldest retained article.
	start := last + 1
	if last == 0 {
		start = stats.High - s.opts.BatchSize + 1
	}
	if start < stats.Low {
		start = stats.Low
	}
	if start > stats.High {
		s.log.Debug("scrape latest: group %s up-to-date at %d", group, last)
		return nil
	}

	s.log.Info("scrape latest: group=%s start=%d high=%d batch=%d", group, start, stats.High, s.opts.BatchSize)

	for from := start; from <= stats.High; from += s.opts.BatchSize {
		to := from + s.opts.BatchSize - 1
		if to > stats.High {
			to = stats.High
		}

		inserted, err := s.fetchInsertRange(ctx, providerID, newsgroupID, group, from, to)
		if err != nil {
			return err
		}

		if err := s.repo.UpsertLatestCheckpoint(ctx, providerID, newsgroupID, to); err != nil {
			return fmt.Errorf("upsert latest checkpoint %s=%d: %w", group, to, err)
		}

		s.log.Info("scrape latest: group=%s range=%d-%d inserted=%d", group, from, to, inserted)
	}

	return nil
}

func (s *Service) runBackfillGroup(ctx context.Context, providerID int64, group string) error {
	newsgroupID, err := s.repo.EnsureNewsgroup(ctx, group)
	if err != nil {
		return fmt.Errorf("ensure newsgroup %s: %w", group, err)
	}

	stats, err := s.provider.GroupStats(ctx, group)
	if err != nil {
		return fmt.Errorf("group stats %s: %w", group, err)
	}
	if stats.High <= 0 {
		s.log.Warn("scrape backfill: group %s has no articles yet", group)
		return nil
	}

	latestCursor, err := s.repo.GetLatestCheckpoint(ctx, providerID, newsgroupID)
	if err != nil {
		return fmt.Errorf("get latest checkpoint %s: %w", group, err)
	}

	backfillCursor, err := s.repo.GetBackfillCheckpoint(ctx, providerID, newsgroupID)
	if err != nil {
		return fmt.Errorf("get backfill checkpoint %s: %w", group, err)
	}

	// first backfill starts just behind the latest frontier if present,
	// otherwise it starts from the current group head.
	end := backfillCursor
	if end == 0 {
		if latestCursor > 0 {
			end = latestCursor - 1
		} else {
			end = stats.High
		}
	}

	if end > stats.High {
		end = stats.High
	}
	if end < stats.Low {
		s.log.Debug("scrape backfill: group %s completed at %d", group, end)
		return nil
	}

	start := end - s.opts.BatchSize + 1
	if start < stats.Low {
		start = stats.Low
	}

	s.log.Info("scrape backfill: group=%s start=%d end=%d low=%d batch=%d", group, start, end, stats.Low, s.opts.BatchSize)

	inserted, err := s.fetchInsertRange(ctx, providerID, newsgroupID, group, start, end)
	if err != nil {
		return err
	}

	nextCursor := start - 1
	if nextCursor < 0 {
		nextCursor = 0
	}

	if err := s.repo.UpsertBackfillCheckpoint(ctx, providerID, newsgroupID, nextCursor); err != nil {
		return fmt.Errorf("upsert backfill checkpoint %s=%d: %w", group, nextCursor, err)
	}

	s.log.Info("scrape backfill: group=%s range=%d-%d inserted=%d next=%d", group, start, end, inserted, nextCursor)
	return nil
}

func (s *Service) fetchInsertRange(ctx context.Context, providerID, newsgroupID int64, group string, from, to int64) (int64, error) {
	rows, err := s.provider.XOver(ctx, group, from, to)
	if err != nil {
		return 0, fmt.Errorf("xover %s %d-%d: %w", group, from, to, err)
	}

	headers := make([]pgindex.ArticleHeader, 0, len(rows))
	for _, r := range rows {
		if r.ArticleNumber <= 0 || strings.TrimSpace(r.MessageID) == "" {
			continue
		}

		cleanRaw := make(map[string]any, len(r.RawOverview))
		for k, v := range r.RawOverview {
			cleanKey := sanitizeScrapeUTF8(k)
			switch tv := v.(type) {
			case string:
				cleanRaw[cleanKey] = sanitizeScrapeUTF8(tv)
			default:
				cleanRaw[cleanKey] = v
			}
		}

		headers = append(headers, pgindex.ArticleHeader{
			ArticleNumber: r.ArticleNumber,
			MessageID:     sanitizeScrapeUTF8(r.MessageID),
			Subject:       sanitizeScrapeUTF8(r.Subject),
			Poster:        sanitizeScrapeUTF8(r.Poster),
			DateUTC:       r.DateUTC,
			Bytes:         r.Bytes,
			Lines:         r.Lines,
			Xref:          sanitizeScrapeUTF8(r.Xref),
			RawOverview:   cleanRaw,
		})
	}

	inserted, err := s.repo.InsertArticleHeaders(ctx, providerID, newsgroupID, headers)
	if err != nil {
		return 0, fmt.Errorf("insert headers %s %d-%d: %w", group, from, to, err)
	}

	return inserted, nil
}

func sanitizeScrapeUTF8(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if utf8.ValidString(s) {
		return s
	}
	return string(bytes.ToValidUTF8([]byte(s), []byte{}))
}
