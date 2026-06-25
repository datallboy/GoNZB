package scrape

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
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
	CheckCriticalIndexerIntegrity(ctx context.Context, ensureExtension bool) (*pgindex.IndexerIntegrityReport, error)

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
	RefreshYEncRecoveryAdmissionSnapshot(ctx context.Context) (*pgindex.YEncRecoveryAdmissionSnapshot, error)
	UpsertIndexerGroupProfile(ctx context.Context, providerID, newsgroupID int64, tier, reason string) error
	UpsertDeferredArticleRange(ctx context.Context, in pgindex.DeferredArticleRangeRecord) error
}

type provider interface {
	ID() string
	GroupStats(ctx context.Context, group string) (GroupStats, error)
	XOver(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error)
}

type providerAwareXOver interface {
	XOverWithProvider(ctx context.Context, group string, from, to int64) ([]OverviewHeader, string, error)
}

type GroupStats struct {
	Low        int64
	High       int64
	ProviderID string
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
	Concurrency              int
	MaxBatches               int
	BackfillUntilDateByGroup map[string]time.Time
}

type Service struct {
	repo           repository
	provider       provider
	log            logger
	opts           Options
	mu             sync.Mutex
	nextGroupIndex int

	integrityMu           sync.Mutex
	integrityCheckedAt    time.Time
	integrityCachedValid  bool
	integrityCached       *pgindex.IndexerIntegrityReport
	integrityPreflightTTL time.Duration
	amcheckWarned         bool
}

type runMetrics struct {
	Mode               string
	BatchSize          int64
	GroupsTotal        int
	GroupsScheduled    int
	GroupsProcessed    int
	GroupsWithWork     int
	WorkersUsed        int
	RangesFetched      int
	ArticleHeadersSeen int64
	ArticlesInserted   int64
	CutoffFiltered     int64
	CheckpointUpdates  int
	DeferredRanges     int
	RotationStartIndex int
	RotationNextIndex  int
}

type groupRunResult struct {
	HadWork            bool
	RangesFetched      int
	ArticleHeadersSeen int64
	ArticlesInserted   int64
	CutoffFiltered     int64
	CheckpointUpdates  int
	DeferredRanges     int
}

type groupRunOutcome struct {
	result groupRunResult
	err    error
}

func NewService(repo repository, p provider, log logger, opts Options) *Service {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 5000
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	if opts.MaxBatches <= 0 {
		opts.MaxBatches = opts.Concurrency
	}

	return &Service{
		repo:                  repo,
		provider:              p,
		log:                   log,
		opts:                  opts,
		integrityPreflightTTL: 5 * time.Minute,
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
	report, err := s.checkCriticalIndexerIntegrity(ctx)
	if err != nil {
		return nil, fmt.Errorf("check critical ingest index integrity: %w", err)
	}
	if report != nil {
		if report.HasFailures() {
			return nil, fmt.Errorf("critical ingest index integrity failed: %s", report.FailureSummary())
		}
		if !report.AmcheckAvailable {
			s.warnAmcheckUnavailableOnce()
		}
	}

	groups := s.effectiveGroups()
	if len(groups) == 0 {
		return (&runMetrics{Mode: mode, BatchSize: s.opts.BatchSize}).toMap(), nil
	}
	if s.provider == nil {
		return nil, fmt.Errorf("scrape provider is required")
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
	runErr := s.runGroups(ctx, providerID, mode, groups, metrics)

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

func (s *Service) checkCriticalIndexerIntegrity(ctx context.Context) (*pgindex.IndexerIntegrityReport, error) {
	s.integrityMu.Lock()
	defer s.integrityMu.Unlock()

	if s.integrityCachedValid && s.integrityPreflightTTL > 0 && !s.integrityCheckedAt.IsZero() && time.Since(s.integrityCheckedAt) < s.integrityPreflightTTL {
		return s.integrityCached, nil
	}

	report, err := s.repo.CheckCriticalIndexerIntegrity(ctx, false)
	if err != nil {
		return nil, err
	}
	s.integrityCached = report
	s.integrityCachedValid = true
	s.integrityCheckedAt = time.Now()
	return report, nil
}

func (s *Service) warnAmcheckUnavailableOnce() {
	if s.log == nil {
		return
	}

	s.integrityMu.Lock()
	defer s.integrityMu.Unlock()
	if s.amcheckWarned {
		return
	}
	s.amcheckWarned = true
	s.log.Warn("scrape integrity preflight: amcheck verification is unavailable to the current database role; proceeding with metadata-only checks. Run maintenance integrity checks with a privileged database role for full amcheck coverage")
}

func (s *Service) runGroups(ctx context.Context, providerID int64, mode string, groups []string, metrics *runMetrics) error {
	metrics.GroupsTotal = len(groups)
	scheduled, startIndex, nextIndex := s.reserveRunGroups(groups)
	metrics.GroupsScheduled = len(scheduled)
	metrics.RotationStartIndex = startIndex
	metrics.RotationNextIndex = nextIndex
	if len(scheduled) == 0 {
		return nil
	}

	workerCount := s.opts.Concurrency
	if workerCount > len(scheduled) {
		workerCount = len(scheduled)
	}
	if workerCount <= 0 {
		workerCount = 1
	}
	metrics.WorkersUsed = workerCount

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan string, len(scheduled))
	outcomes := make(chan groupRunOutcome, len(scheduled))

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for group := range jobs {
				result, err := s.runGroup(runCtx, providerID, mode, group)
				if err != nil {
					cancel()
				}
				outcomes <- groupRunOutcome{result: result, err: err}
			}
		}()
	}

	for _, group := range scheduled {
		jobs <- group
	}
	close(jobs)

	var firstErr error
	for range scheduled {
		outcome := <-outcomes
		metrics.GroupsProcessed++
		if outcome.result.HadWork {
			metrics.GroupsWithWork++
		}
		metrics.RangesFetched += outcome.result.RangesFetched
		metrics.ArticleHeadersSeen += outcome.result.ArticleHeadersSeen
		metrics.ArticlesInserted += outcome.result.ArticlesInserted
		metrics.CutoffFiltered += outcome.result.CutoffFiltered
		metrics.CheckpointUpdates += outcome.result.CheckpointUpdates
		metrics.DeferredRanges += outcome.result.DeferredRanges

		if outcome.err != nil && !isContextCancellation(outcome.err) && firstErr == nil {
			firstErr = outcome.err
		}
	}

	wg.Wait()
	return firstErr
}

func (s *Service) runGroup(ctx context.Context, providerID int64, mode, group string) (groupRunResult, error) {
	switch mode {
	case "latest":
		return s.runLatestGroup(ctx, providerID, group)
	case "backfill":
		return s.runBackfillGroup(ctx, providerID, group)
	default:
		return groupRunResult{}, fmt.Errorf("unsupported scrape mode %q", mode)
	}
}

func (s *Service) runLatestGroup(ctx context.Context, providerID int64, group string) (groupRunResult, error) {
	newsgroupID, err := s.repo.EnsureNewsgroup(ctx, group)
	if err != nil {
		return groupRunResult{}, fmt.Errorf("ensure newsgroup %s: %w", group, err)
	}

	stats, err := s.provider.GroupStats(ctx, group)
	if err != nil {
		return groupRunResult{}, fmt.Errorf("group stats %s: %w", group, err)
	}
	providerID, err = s.effectiveProviderID(ctx, providerID, stats.ProviderID)
	if err != nil {
		return groupRunResult{}, err
	}
	if err := s.repo.UpsertIndexerGroupProfile(ctx, providerID, newsgroupID, "warm", "configured_latest_scrape"); err != nil {
		return groupRunResult{}, err
	}
	if stats.High <= 0 {
		s.log.Warn("scrape latest: group %s has no articles yet", group)
		return groupRunResult{}, nil
	}

	last, err := s.repo.GetLatestCheckpoint(ctx, providerID, newsgroupID)
	if err != nil {
		return groupRunResult{}, fmt.Errorf("get latest checkpoint %s: %w", group, err)
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
		return groupRunResult{}, nil
	}

	s.log.Info("scrape latest: group=%s start=%d high=%d batch=%d", group, start, stats.High, s.opts.BatchSize)

	to := start + s.opts.BatchSize - 1
	if to > stats.High {
		to = stats.High
	}
	if deferred, err := s.deferRangeWhenRecoveryPressured(ctx, "latest", providerID, newsgroupID, group, start, to); err != nil {
		return groupRunResult{}, err
	} else if deferred {
		return groupRunResult{HadWork: true, DeferredRanges: 1}, nil
	}

	headers, inserted, _, _, actualProviderID, err := s.fetchInsertRange(ctx, providerID, newsgroupID, group, start, to, nil, stats.ProviderID)
	if err != nil {
		return groupRunResult{}, err
	}
	providerID = actualProviderID

	if err := s.repo.UpsertLatestCheckpoint(ctx, providerID, newsgroupID, to); err != nil {
		return groupRunResult{}, fmt.Errorf("upsert latest checkpoint %s=%d: %w", group, to, err)
	}

	s.log.Info("scrape latest: group=%s range=%d-%d inserted=%d", group, start, to, inserted)

	return groupRunResult{
		HadWork:            true,
		RangesFetched:      1,
		ArticleHeadersSeen: int64(len(headers)),
		ArticlesInserted:   inserted,
		CheckpointUpdates:  1,
	}, nil
}

func (s *Service) runBackfillGroup(ctx context.Context, providerID int64, group string) (groupRunResult, error) {
	newsgroupID, err := s.repo.EnsureNewsgroup(ctx, group)
	if err != nil {
		return groupRunResult{}, fmt.Errorf("ensure newsgroup %s: %w", group, err)
	}

	stats, err := s.provider.GroupStats(ctx, group)
	if err != nil {
		return groupRunResult{}, fmt.Errorf("group stats %s: %w", group, err)
	}
	providerID, err = s.effectiveProviderID(ctx, providerID, stats.ProviderID)
	if err != nil {
		return groupRunResult{}, err
	}
	if err := s.repo.UpsertIndexerGroupProfile(ctx, providerID, newsgroupID, "warm", "configured_backfill_scrape"); err != nil {
		return groupRunResult{}, err
	}
	if stats.High <= 0 {
		s.log.Warn("scrape backfill: group %s has no articles yet", group)
		return groupRunResult{}, nil
	}

	latestCursor, err := s.repo.GetLatestCheckpoint(ctx, providerID, newsgroupID)
	if err != nil {
		return groupRunResult{}, fmt.Errorf("get latest checkpoint %s: %w", group, err)
	}

	backfillCursor, err := s.repo.GetBackfillCheckpoint(ctx, providerID, newsgroupID)
	if err != nil {
		return groupRunResult{}, fmt.Errorf("get backfill checkpoint %s: %w", group, err)
	}

	cutoffDate, hasCutoff := s.opts.BackfillUntilDateByGroup[group]
	state, err := s.repo.GetBackfillCheckpointState(ctx, providerID, newsgroupID)
	if err != nil {
		return groupRunResult{}, fmt.Errorf("get backfill checkpoint state %s: %w", group, err)
	}
	if hasCutoff {
		cutoff := cutoffDate.UTC()
		reachedForGroup, err := s.repo.HasBackfillCutoffReachedForGroup(ctx, newsgroupID, cutoff)
		if err != nil {
			return groupRunResult{}, fmt.Errorf("check backfill cutoff state %s: %w", group, err)
		}
		if reachedForGroup {
			s.log.Debug("scrape backfill: group %s already reached cutoff %s for another provider", group, cutoff.Format("2006-01-02"))
			return groupRunResult{}, nil
		}
		if state == nil || state.UntilDate == nil || !state.UntilDate.Equal(cutoff) {
			if err := s.repo.SetBackfillCheckpointState(ctx, providerID, newsgroupID, &cutoff, false, ""); err != nil {
				return groupRunResult{}, fmt.Errorf("set backfill cutoff state %s: %w", group, err)
			}
			state = &pgindex.BackfillCheckpointState{
				ArticleNumber: backfillCursor,
				UntilDate:     &cutoff,
			}
		} else if state.CutoffReached {
			s.log.Debug("scrape backfill: group %s already reached cutoff %s", group, cutoff.Format("2006-01-02"))
			return groupRunResult{}, nil
		}
	} else if state != nil && (state.UntilDate != nil || state.CutoffReached || strings.TrimSpace(state.StoppedReason) != "") {
		if err := s.repo.SetBackfillCheckpointState(ctx, providerID, newsgroupID, nil, false, ""); err != nil {
			return groupRunResult{}, fmt.Errorf("clear backfill cutoff state %s: %w", group, err)
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
		return groupRunResult{}, nil
	}

	start := end - s.opts.BatchSize + 1
	if start < stats.Low {
		start = stats.Low
	}

	s.log.Info("scrape backfill: group=%s start=%d end=%d low=%d batch=%d", group, start, end, stats.Low, s.opts.BatchSize)
	if deferred, err := s.deferRangeWhenRecoveryPressured(ctx, "backfill", providerID, newsgroupID, group, start, end); err != nil {
		return groupRunResult{}, err
	} else if deferred {
		nextCursor := start - 1
		if nextCursor < 0 {
			nextCursor = 0
		}
		if err := s.repo.UpsertBackfillCheckpoint(ctx, providerID, newsgroupID, nextCursor); err != nil {
			return groupRunResult{}, fmt.Errorf("upsert deferred backfill checkpoint %s=%d: %w", group, nextCursor, err)
		}
		return groupRunResult{HadWork: true, DeferredRanges: 1, CheckpointUpdates: 1}, nil
	}

	var cutoff *time.Time
	if hasCutoff {
		c := cutoffDate.UTC()
		cutoff = &c
	}
	headers, inserted, oldestSeen, cutoffFiltered, actualProviderID, err := s.fetchInsertRange(ctx, providerID, newsgroupID, group, start, end, cutoff, stats.ProviderID)
	if err != nil {
		return groupRunResult{}, err
	}
	providerID = actualProviderID

	if hasCutoff {
		if oldestSeen != nil && !oldestSeen.After(cutoffDate.UTC()) {
			if err := s.repo.SetBackfillCheckpointState(ctx, providerID, newsgroupID, ptrTime(cutoffDate.UTC()), true, "until_date_reached"); err != nil {
				return groupRunResult{}, fmt.Errorf("mark backfill cutoff reached %s: %w", group, err)
			}
			s.log.Info("scrape backfill: group=%s reached cutoff=%s oldest=%s inserted=%d", group, cutoffDate.UTC().Format(time.RFC3339), oldestSeen.Format(time.RFC3339), inserted)
			return groupRunResult{
				HadWork:            true,
				RangesFetched:      1,
				ArticleHeadersSeen: int64(len(headers)),
				ArticlesInserted:   inserted,
				CutoffFiltered:     int64(cutoffFiltered),
			}, nil
		}
	}

	nextCursor := start - 1
	if nextCursor < 0 {
		nextCursor = 0
	}

	if err := s.repo.UpsertBackfillCheckpoint(ctx, providerID, newsgroupID, nextCursor); err != nil {
		return groupRunResult{}, fmt.Errorf("upsert backfill checkpoint %s=%d: %w", group, nextCursor, err)
	}

	s.log.Info("scrape backfill: group=%s range=%d-%d inserted=%d next=%d", group, start, end, inserted, nextCursor)
	return groupRunResult{
		HadWork:            true,
		RangesFetched:      1,
		ArticleHeadersSeen: int64(len(headers)),
		ArticlesInserted:   inserted,
		CutoffFiltered:     int64(cutoffFiltered),
		CheckpointUpdates:  1,
	}, nil
}

func (s *Service) fetchInsertRange(ctx context.Context, providerID, newsgroupID int64, group string, from, to int64, cutoff *time.Time, expectedProviderKey string) ([]pgindex.ArticleHeader, int64, *time.Time, int, int64, error) {
	var (
		rows      []OverviewHeader
		actualKey string
		err       error
	)
	if aware, ok := s.provider.(providerAwareXOver); ok {
		rows, actualKey, err = aware.XOverWithProvider(ctx, group, from, to)
	} else {
		rows, err = s.provider.XOver(ctx, group, from, to)
		actualKey = s.provider.ID()
	}
	if err != nil {
		return nil, 0, nil, 0, 0, fmt.Errorf("xover %s %d-%d: %w", group, from, to, err)
	}
	if strings.TrimSpace(actualKey) != "" && strings.TrimSpace(expectedProviderKey) != "" && !strings.EqualFold(actualKey, expectedProviderKey) {
		providerID, err = s.effectiveProviderID(ctx, providerID, actualKey)
		if err != nil {
			return nil, 0, nil, 0, 0, err
		}
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
		return nil, 0, oldestSeen, cutoffFiltered, 0, fmt.Errorf("insert headers %s %d-%d: %w", group, from, to, err)
	}

	return headers, inserted, oldestSeen, cutoffFiltered, providerID, nil
}

func (s *Service) deferRangeWhenRecoveryPressured(ctx context.Context, mode string, providerID, newsgroupID int64, group string, from, to int64) (bool, error) {
	if from <= 0 || to < from {
		return false, nil
	}
	snapshot, err := s.repo.RefreshYEncRecoveryAdmissionSnapshot(ctx)
	if err != nil {
		return false, err
	}
	if snapshot == nil {
		return false, nil
	}
	hardBlocked := snapshot.HardCap > 0 && snapshot.OpenTotal >= snapshot.HardCap
	softBlocked := snapshot.SoftCap > 0 && snapshot.OpenTotal >= snapshot.SoftCap
	if !hardBlocked && !(mode == "backfill" && softBlocked) {
		return false, nil
	}
	reason := "recovery_soft_cap"
	if hardBlocked {
		reason = "recovery_hard_cap"
	}
	if err := s.repo.UpsertDeferredArticleRange(ctx, pgindex.DeferredArticleRangeRecord{
		ProviderID:               providerID,
		NewsgroupID:              newsgroupID,
		ArticleLow:               from,
		ArticleHigh:              to,
		EstimatedArticleCount:    to - from + 1,
		EstimatedObfuscatedCount: to - from + 1,
		Reason:                   reason,
		PriorityScore:            deferredRangePriority(mode, snapshot),
	}); err != nil {
		return false, err
	}
	s.log.Info("scrape %s deferred: group=%s range=%d-%d reason=%s open_yenc=%d soft_cap=%d hard_cap=%d", mode, group, from, to, reason, snapshot.OpenTotal, snapshot.SoftCap, snapshot.HardCap)
	return true, nil
}

func deferredRangePriority(mode string, snapshot *pgindex.YEncRecoveryAdmissionSnapshot) float64 {
	score := 10.0
	if mode == "latest" {
		score = 100
	}
	if snapshot != nil && snapshot.HardCap > 0 {
		score -= (float64(snapshot.OpenTotal) / float64(snapshot.HardCap)) * 10
	}
	if score < 0 {
		return 0
	}
	return score
}

func (s *Service) effectiveProviderID(ctx context.Context, fallbackID int64, providerKey string) (int64, error) {
	providerKey = strings.TrimSpace(providerKey)
	if providerKey == "" || strings.EqualFold(providerKey, strings.TrimSpace(s.provider.ID())) {
		return fallbackID, nil
	}
	providerID, err := s.repo.EnsureProvider(ctx, providerKey, providerKey)
	if err != nil {
		return 0, fmt.Errorf("ensure provider %s: %w", providerKey, err)
	}
	return providerID, nil
}

func (s *Service) effectiveGroups() []string {
	if len(s.opts.Newsgroups) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.opts.Newsgroups))
	for _, raw := range s.opts.Newsgroups {
		group := strings.TrimSpace(raw)
		if group == "" || slices.Contains(out, group) {
			continue
		}
		out = append(out, group)
	}
	return out
}

func (s *Service) reserveRunGroups(groups []string) ([]string, int, int) {
	if len(groups) == 0 {
		return nil, 0, 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	start := s.nextGroupIndex % len(groups)
	if start < 0 {
		start = 0
	}
	budget := s.opts.MaxBatches
	if budget <= 0 {
		budget = s.opts.Concurrency
	}
	if budget <= 0 {
		budget = 1
	}
	if budget > len(groups) {
		budget = len(groups)
	}

	scheduled := make([]string, 0, budget)
	for i := 0; i < budget; i++ {
		scheduled = append(scheduled, groups[(start+i)%len(groups)])
	}
	s.nextGroupIndex = (start + budget) % len(groups)
	return scheduled, start, s.nextGroupIndex
}

func (m *runMetrics) toMap() map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return map[string]any{
		"mode":                 m.Mode,
		"groups_total":         m.GroupsTotal,
		"groups_scheduled":     m.GroupsScheduled,
		"groups_processed":     m.GroupsProcessed,
		"groups_with_work":     m.GroupsWithWork,
		"workers_used":         m.WorkersUsed,
		"ranges_fetched":       m.RangesFetched,
		"article_headers_seen": m.ArticleHeadersSeen,
		"articles_inserted":    m.ArticlesInserted,
		"cutoff_filtered":      m.CutoffFiltered,
		"checkpoint_updates":   m.CheckpointUpdates,
		"deferred_ranges":      m.DeferredRanges,
		"batch_size":           m.BatchSize,
		"rotation_start_index": m.RotationStartIndex,
		"rotation_next_index":  m.RotationNextIndex,
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
	s = strings.ReplaceAll(s, "\x00", "")
	if s == "" {
		return ""
	}
	if utf8.ValidString(s) {
		return s
	}
	return strings.ReplaceAll(string(bytes.ToValidUTF8([]byte(s), []byte{})), "\x00", "")
}

func isContextCancellation(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}
