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
	GetBackfillCheckpointState(ctx context.Context, providerID, newsgroupID int64) (*pgindex.BackfillCheckpointState, error)
	HasBackfillCutoffReachedForGroup(ctx context.Context, newsgroupID int64, untilDate time.Time) (bool, error)
	SetBackfillCheckpointState(ctx context.Context, providerID, newsgroupID int64, untilDate *time.Time, cutoffReached bool, stoppedReason string) error

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
	Newsgroups               []string
	BatchSize                int64
	BackfillUntilDateByGroup map[string]time.Time
}

type Service struct {
	repo     repository
	provider provider
	log      logger
	opts     Options
}

type runMetrics struct {
	Mode               string
	BatchSize          int64
	GroupsTotal        int
	GroupsProcessed    int
	GroupsWithWork     int
	RangesFetched      int
	ArticleHeadersSeen int64
	ArticlesInserted   int64
	CutoffFiltered     int64
	CheckpointUpdates  int
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
	_, err := s.RunLatestOnceWithMetrics(ctx)
	return err
}

// latest mode prioritizes the head of the group and continues forward.
func (s *Service) RunLatestOnce(ctx context.Context) error {
	_, err := s.RunLatestOnceWithMetrics(ctx)
	return err
}

// backfill mode walks backward from the most recent known boundary.
func (s *Service) RunBackfillOnce(ctx context.Context) error {
	_, err := s.RunBackfillOnceWithMetrics(ctx)
	return err
}

func (s *Service) runMode(ctx context.Context, mode string) error {
	_, err := s.runModeWithMetrics(ctx, mode)
	return err
}

func (s *Service) RunLatestOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	return s.runModeWithMetrics(ctx, "latest")
}

func (s *Service) RunBackfillOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	return s.runModeWithMetrics(ctx, "backfill")
}

func (s *Service) runModeWithMetrics(ctx context.Context, mode string) (map[string]any, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("scrape repo is required")
	}
	if s.provider == nil {
		return nil, fmt.Errorf("scrape provider is required")
	}
	if len(s.opts.Newsgroups) == 0 {
		return nil, fmt.Errorf("at least one newsgroup is required")
	}

	providerKey := strings.TrimSpace(s.provider.ID())
	if providerKey == "" {
		return nil, fmt.Errorf("provider id is required")
	}

	providerID, err := s.repo.EnsureProvider(ctx, providerKey, providerKey)
	if err != nil {
		return nil, fmt.Errorf("ensure provider: %w", err)
	}

	runID, err := s.repo.StartScrapeRun(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("start scrape run: %w", err)
	}

	metrics := &runMetrics{Mode: mode, BatchSize: s.opts.BatchSize}
	runErr := s.runGroups(ctx, providerID, mode, metrics)

	status := "completed"
	errText := ""
	if runErr != nil {
		status = "failed"
		errText = runErr.Error()
	}

	if finishErr := s.repo.FinishScrapeRun(ctx, runID, status, errText); finishErr != nil {
		if runErr != nil {
			return metrics.toMap(), fmt.Errorf("%v (also failed to finish scrape run: %w)", runErr, finishErr)
		}
		return metrics.toMap(), fmt.Errorf("finish scrape run: %w", finishErr)
	}

	return metrics.toMap(), runErr
}

func (s *Service) runGroups(ctx context.Context, providerID int64, mode string, metrics *runMetrics) error {
	metrics.GroupsTotal = len(s.opts.Newsgroups)
	for _, g := range s.opts.Newsgroups {
		group := strings.TrimSpace(g)
		if group == "" {
			continue
		}
		metrics.GroupsProcessed++

		var err error
		switch mode {
		case "latest":
			err = s.runLatestGroup(ctx, providerID, group, metrics)
		case "backfill":
			err = s.runBackfillGroup(ctx, providerID, group, metrics)
		default:
			err = fmt.Errorf("unsupported scrape mode %q", mode)
		}

		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) runLatestGroup(ctx context.Context, providerID int64, group string, metrics *runMetrics) error {
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
	metrics.GroupsWithWork++

	s.log.Info("scrape latest: group=%s start=%d high=%d batch=%d", group, start, stats.High, s.opts.BatchSize)

	for from := start; from <= stats.High; from += s.opts.BatchSize {
		to := from + s.opts.BatchSize - 1
		if to > stats.High {
			to = stats.High
		}

		headers, inserted, _, _, err := s.fetchInsertRange(ctx, providerID, newsgroupID, group, from, to, nil)
		if err != nil {
			return err
		}
		metrics.RangesFetched++
		metrics.ArticleHeadersSeen += int64(len(headers))
		metrics.ArticlesInserted += inserted
		metrics.CheckpointUpdates++

		if err := s.repo.UpsertLatestCheckpoint(ctx, providerID, newsgroupID, to); err != nil {
			return fmt.Errorf("upsert latest checkpoint %s=%d: %w", group, to, err)
		}

		s.log.Info("scrape latest: group=%s range=%d-%d inserted=%d", group, from, to, inserted)
	}

	return nil
}

func (s *Service) runBackfillGroup(ctx context.Context, providerID int64, group string, metrics *runMetrics) error {
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

	cutoffDate, hasCutoff := s.opts.BackfillUntilDateByGroup[group]
	state, err := s.repo.GetBackfillCheckpointState(ctx, providerID, newsgroupID)
	if err != nil {
		return fmt.Errorf("get backfill checkpoint state %s: %w", group, err)
	}
	if hasCutoff {
		cutoff := cutoffDate.UTC()
		reachedForGroup, err := s.repo.HasBackfillCutoffReachedForGroup(ctx, newsgroupID, cutoff)
		if err != nil {
			return fmt.Errorf("check backfill cutoff state %s: %w", group, err)
		}
		if reachedForGroup {
			s.log.Debug("scrape backfill: group %s already reached cutoff %s for another provider", group, cutoff.Format("2006-01-02"))
			return nil
		}
		if state == nil || state.UntilDate == nil || !state.UntilDate.Equal(cutoff) {
			if err := s.repo.SetBackfillCheckpointState(ctx, providerID, newsgroupID, &cutoff, false, ""); err != nil {
				return fmt.Errorf("set backfill cutoff state %s: %w", group, err)
			}
			state = &pgindex.BackfillCheckpointState{
				ArticleNumber: backfillCursor,
				UntilDate:     &cutoff,
			}
		} else if state.CutoffReached {
			s.log.Debug("scrape backfill: group %s already reached cutoff %s", group, cutoff.Format("2006-01-02"))
			return nil
		}
	} else if state != nil && (state.UntilDate != nil || state.CutoffReached || strings.TrimSpace(state.StoppedReason) != "") {
		if err := s.repo.SetBackfillCheckpointState(ctx, providerID, newsgroupID, nil, false, ""); err != nil {
			return fmt.Errorf("clear backfill cutoff state %s: %w", group, err)
		}
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
	metrics.GroupsWithWork++

	start := end - s.opts.BatchSize + 1
	if start < stats.Low {
		start = stats.Low
	}

	s.log.Info("scrape backfill: group=%s start=%d end=%d low=%d batch=%d", group, start, end, stats.Low, s.opts.BatchSize)

	var cutoff *time.Time
	if hasCutoff {
		c := cutoffDate.UTC()
		cutoff = &c
	}
	headers, inserted, oldestSeen, cutoffFiltered, err := s.fetchInsertRange(ctx, providerID, newsgroupID, group, start, end, cutoff)
	if err != nil {
		return err
	}
	metrics.RangesFetched++
	metrics.ArticleHeadersSeen += int64(len(headers))
	metrics.ArticlesInserted += inserted
	metrics.CutoffFiltered += int64(cutoffFiltered)

	if hasCutoff {
		if oldestSeen != nil && !oldestSeen.After(cutoffDate.UTC()) {
			if err := s.repo.SetBackfillCheckpointState(ctx, providerID, newsgroupID, ptrTime(cutoffDate.UTC()), true, "until_date_reached"); err != nil {
				return fmt.Errorf("mark backfill cutoff reached %s: %w", group, err)
			}
			s.log.Info("scrape backfill: group=%s reached cutoff=%s oldest=%s inserted=%d", group, cutoffDate.UTC().Format(time.RFC3339), oldestSeen.Format(time.RFC3339), inserted)
			return nil
		}
	}

	nextCursor := start - 1
	if nextCursor < 0 {
		nextCursor = 0
	}

	if err := s.repo.UpsertBackfillCheckpoint(ctx, providerID, newsgroupID, nextCursor); err != nil {
		return fmt.Errorf("upsert backfill checkpoint %s=%d: %w", group, nextCursor, err)
	}
	metrics.CheckpointUpdates++

	s.log.Info("scrape backfill: group=%s range=%d-%d inserted=%d next=%d", group, start, end, inserted, nextCursor)
	return nil
}

func (s *Service) fetchInsertRange(ctx context.Context, providerID, newsgroupID int64, group string, from, to int64, cutoff *time.Time) ([]pgindex.ArticleHeader, int64, *time.Time, int, error) {
	rows, err := s.provider.XOver(ctx, group, from, to)
	if err != nil {
		return nil, 0, nil, 0, fmt.Errorf("xover %s %d-%d: %w", group, from, to, err)
	}

	headers := make([]pgindex.ArticleHeader, 0, len(rows))
	var oldestSeen *time.Time
	cutoffFiltered := 0
	for _, r := range rows {
		if r.ArticleNumber <= 0 || strings.TrimSpace(r.MessageID) == "" {
			continue
		}
		if r.DateUTC != nil {
			t := r.DateUTC.UTC()
			if oldestSeen == nil || t.Before(*oldestSeen) {
				oldestSeen = &t
			}
			if cutoff != nil && t.Before(cutoff.UTC()) {
				cutoffFiltered++
				continue
			}
		} else if cutoff != nil {
			cutoffFiltered++
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
		return nil, 0, oldestSeen, cutoffFiltered, fmt.Errorf("insert headers %s %d-%d: %w", group, from, to, err)
	}

	return headers, inserted, oldestSeen, cutoffFiltered, nil
}

func (m *runMetrics) toMap() map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return map[string]any{
		"mode":                 m.Mode,
		"groups_total":         m.GroupsTotal,
		"groups_processed":     m.GroupsProcessed,
		"groups_with_work":     m.GroupsWithWork,
		"ranges_fetched":       m.RangesFetched,
		"article_headers_seen": m.ArticleHeadersSeen,
		"articles_inserted":    m.ArticlesInserted,
		"cutoff_filtered":      m.CutoffFiltered,
		"checkpoint_updates":   m.CheckpointUpdates,
		"batch_size":           m.BatchSize,
	}
}

func ptrTime(v time.Time) *time.Time {
	t := v.UTC()
	return &t
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
