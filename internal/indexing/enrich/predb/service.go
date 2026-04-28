package predb

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
	ListReleaseEnrichmentCandidates(ctx context.Context, stageName string, limit int) ([]pgindex.ReleaseEnrichmentCandidate, error)
	UpsertPredbEntries(ctx context.Context, rows []pgindex.PredbEntryRecord) error
	GetPredbBackfillWindow(ctx context.Context) (*pgindex.PredbBackfillWindow, error)
	GetPredbEntryWindow(ctx context.Context) (*pgindex.PredbBackfillWindow, error)
	GetPredbBackfillCheckpoint(ctx context.Context, provider string) (*pgindex.PredbBackfillCheckpoint, error)
	UpsertPredbBackfillCheckpoint(ctx context.Context, in pgindex.PredbBackfillCheckpoint) error
	ListPredbEntriesForWindow(ctx context.Context, from, to *time.Time, categoryHint string, limit int) ([]pgindex.PredbEntrySummary, error)
	ReplaceReleasePredbMatches(ctx context.Context, releaseID string, rows []pgindex.ReleasePredbMatchRecord) error
	ApplyReleasePredbUpdate(ctx context.Context, in pgindex.ReleasePredbUpdate) error
}

type SearchProvider interface {
	ProviderName() string
	Search(ctx context.Context, query Query) ([]Match, error)
}

type detailedSearchProvider interface {
	SearchProvider
	SearchDetailed(ctx context.Context, query Query) ([]Match, []providerResult, error)
}

type FeedProvider interface {
	ProviderName() string
	FetchRecent(ctx context.Context, limit int) ([]pgindex.PredbEntryRecord, error)
}

type BackfillProvider interface {
	ProviderName() string
	FetchPage(ctx context.Context, offset, limit int) ([]pgindex.PredbEntryRecord, bool, error)
}

type Options struct {
	Limit            int
	HTTPTimeout      time.Duration
	Provider         string
	BaseURL          string
	FeedURL          string
	DumpURL          string
	BackfillPageSize int
	MaxBackfillPages int
}

type Query struct {
	Text              string
	Title             string
	CanonicalTitle    string
	Year              int
	IsTV              bool
	Season            int
	Episode           int
	PostedAt          *time.Time
	RuntimeSeconds    int
	Resolution        string
	VideoCodec        string
	AudioCodec        string
	CurrentTitle      string
	CurrentTitleSrc   string
	CurrentConfidence float64
}

type Match struct {
	ExternalID int64
	Title      string
	Category   string
	Source     string
	Team       string
	Genre      string
	URL        string
	SizeKB     float64
	FileCount  int
	PostedAt   *time.Time
	Confidence float64
	Payload    map[string]any
}

type Service struct {
	repo           repository
	log            logger
	opts           Options
	searchProvider SearchProvider
	feedProvider   FeedProvider
	backfill       BackfillProvider
}

type providerResult struct {
	Name      string
	Count     int
	ErrorText string
}

func DefaultOptions(opts Options) Options {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.HTTPTimeout <= 0 {
		opts.HTTPTimeout = 10 * time.Second
	}
	if strings.TrimSpace(opts.Provider) == "" {
		opts.Provider = "club,me"
	}
	if strings.TrimSpace(opts.BaseURL) == "" {
		opts.BaseURL = "https://predb.club/api/v1"
	}
	if strings.TrimSpace(opts.FeedURL) == "" {
		opts.FeedURL = "https://predb.me/?rss=1"
	}
	if strings.TrimSpace(opts.DumpURL) == "" {
		opts.DumpURL = ""
	}
	if opts.BackfillPageSize <= 0 {
		opts.BackfillPageSize = 1000
	}
	if opts.MaxBackfillPages <= 0 {
		opts.MaxBackfillPages = 250
	}
	return opts
}

func NewService(repo repository, log logger, opts Options) *Service {
	opts = DefaultOptions(opts)
	svc := &Service{
		repo: repo,
		log:  log,
		opts: opts,
	}
	searchProviders := []SearchProvider{}
	feedProviders := []FeedProvider{}
	backfillProviders := []BackfillProvider{}
	for _, rawProvider := range strings.Split(opts.Provider, ",") {
		switch strings.ToLower(strings.TrimSpace(rawProvider)) {
		case "", "club":
			club := newClubClient(opts)
			searchProviders = append(searchProviders, club)
			backfillProviders = append(backfillProviders, club)
			feedProviders = append(feedProviders, newClubRSSClient(opts))
		case "me":
			feedProviders = append(feedProviders, newMeRSSClient(opts))
		default:
			if log != nil {
				log.Warn("enrich_predb: unsupported provider=%q; skipping", rawProvider)
			}
		}
	}
	svc.searchProvider = chainSearchProviders(searchProviders...)
	svc.feedProvider = chainFeedProviders(feedProviders...)
	svc.backfill = firstBackfillProvider(backfillProviders...)
	return svc
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	sceneMetrics, err := s.RunSceneNameRecoveryOnceWithMetrics(ctx)
	if err != nil {
		return sceneMetrics, err
	}
	fallbackMetrics, err := s.RunMetadataFallbackOnceWithMetrics(ctx)
	metrics := map[string]any{
		"scene_name_recovery": sceneMetrics,
		"metadata_fallback":   fallbackMetrics,
	}
	return metrics, err
}

func (s *Service) RunSceneNameRecoveryOnce(ctx context.Context) error {
	_, err := s.RunSceneNameRecoveryOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunSceneNameRecoveryOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("predb repo is required")
	}
	if s.searchProvider == nil {
		if s.log != nil {
			s.log.Debug("enrich_predb scene-name-recovery: no search provider configured; skipping")
		}
		return map[string]any{"candidates": 0, "updated": 0, "provider_configured": false}, nil
	}

	candidates, err := s.repo.ListReleaseEnrichmentCandidates(ctx, "enrich_predb_scene_name_recovery", s.opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("list enrich_predb scene-name-recovery candidates: %w", err)
	}
	metrics := map[string]any{"candidates": len(candidates), "updated": 0, "provider_configured": true, "limit": s.opts.Limit}
	if len(candidates) == 0 {
		if s.log != nil {
			s.log.Debug("enrich_predb scene-name-recovery: no candidates available")
		}
		return metrics, nil
	}

	updated := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			metrics["updated"] = updated
			return metrics, err
		}
		applied, err := s.sceneNameRecoveryCandidate(ctx, candidate)
		if err != nil {
			if isRecoverablePredbSearchError(err) {
				if s.log != nil {
					s.log.Warn("enrich_predb scene-name-recovery: release_id=%s recoverable_error=%v", candidate.ReleaseID, err)
				}
				break
			}
			metrics["updated"] = updated
			return metrics, fmt.Errorf("enrich predb scene-name-recovery release %s: %w", candidate.ReleaseID, err)
		}
		if applied {
			updated++
		}
	}
	if s.log != nil {
		s.log.Info("enrich_predb scene-name-recovery: candidates=%d updated=%d", len(candidates), updated)
	}
	metrics["updated"] = updated
	return metrics, nil
}

func (s *Service) RunMetadataFallbackOnce(ctx context.Context) error {
	_, err := s.RunMetadataFallbackOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunMetadataFallbackOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("predb repo is required")
	}
	candidates, err := s.repo.ListReleaseEnrichmentCandidates(ctx, "enrich_predb_metadata_only_fallback", s.opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("list enrich_predb metadata-only-fallback candidates: %w", err)
	}
	metrics := map[string]any{"candidates": len(candidates), "updated": 0, "limit": s.opts.Limit}
	if len(candidates) == 0 {
		if s.log != nil {
			s.log.Debug("enrich_predb metadata-only-fallback: no candidates available")
		}
		return metrics, nil
	}

	updated := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			metrics["updated"] = updated
			return metrics, err
		}
		applied, err := s.metadataFallbackCandidate(ctx, candidate)
		if err != nil {
			metrics["updated"] = updated
			return metrics, fmt.Errorf("enrich predb metadata-only-fallback release %s: %w", candidate.ReleaseID, err)
		}
		if applied {
			updated++
		}
	}
	if s.log != nil {
		s.log.Info("enrich_predb metadata-only-fallback: candidates=%d updated=%d", len(candidates), updated)
	}
	metrics["updated"] = updated
	return metrics, nil
}

func (s *Service) RunSyncFeedOnce(ctx context.Context) error {
	if s.repo == nil {
		return fmt.Errorf("predb repo is required")
	}
	if s.feedProvider == nil {
		if s.log != nil {
			s.log.Debug("enrich_predb sync-feed: no feed provider configured; skipping")
		}
		return nil
	}
	rows, err := s.feedProvider.FetchRecent(ctx, s.opts.Limit)
	if err != nil {
		return fmt.Errorf("sync predb feed: %w", err)
	}
	if len(rows) == 0 {
		if s.log != nil {
			s.log.Debug("enrich_predb sync-feed: no feed rows returned")
		}
		return nil
	}
	if err := s.repo.UpsertPredbEntries(ctx, rows); err != nil {
		return fmt.Errorf("upsert predb feed rows: %w", err)
	}
	if s.log != nil {
		s.log.Info("enrich_predb sync-feed: rows=%d", len(rows))
	}
	return nil
}

func (s *Service) RunSyncBackfillOnce(ctx context.Context) error {
	if s.repo == nil {
		return fmt.Errorf("predb repo is required")
	}
	if s.backfill == nil {
		if s.log != nil {
			s.log.Debug("enrich_predb sync-backfill: no backfill provider configured; skipping")
		}
		return nil
	}
	window, err := s.repo.GetPredbBackfillWindow(ctx)
	if err != nil {
		return fmt.Errorf("get predb backfill window: %w", err)
	}
	if window == nil || window.From == nil {
		if s.log != nil {
			s.log.Debug("enrich_predb sync-backfill: no indexed date window available")
		}
		return nil
	}
	targetFrom := window.From.UTC().Add(-72 * time.Hour)
	var targetTo *time.Time
	if window.To != nil {
		t := window.To.UTC().Add(72 * time.Hour)
		targetTo = &t
	}
	entryWindow, err := s.repo.GetPredbEntryWindow(ctx)
	if err != nil {
		return fmt.Errorf("get local predb entry window: %w", err)
	}
	checkpoint, err := s.repo.GetPredbBackfillCheckpoint(ctx, s.backfill.ProviderName())
	if err != nil {
		return fmt.Errorf("get predb backfill checkpoint: %w", err)
	}
	var localOldest *time.Time
	if entryWindow != nil && entryWindow.From != nil {
		t := entryWindow.From.UTC()
		localOldest = &t
	}
	if localOldest != nil && !localOldest.After(targetFrom) {
		if s.log != nil {
			s.log.Info("enrich_predb sync-backfill: local coverage already reaches target_from=%s oldest_local=%s",
				targetFrom.Format(time.RFC3339),
				localOldest.Format(time.RFC3339),
			)
		}
		return nil
	}
	if s.log != nil {
		s.log.Info("enrich_predb sync-backfill: target_from=%s target_to=%s local_oldest=%s checkpoint_oldest=%s checkpoint_offset=%d page_size=%d max_pages=%d provider=%s",
			targetFrom.Format(time.RFC3339),
			formatOptTime(targetTo),
			formatOptTime(localOldest),
			formatCheckpointTime(checkpoint),
			checkpointOffset(checkpoint),
			s.opts.BackfillPageSize,
			s.opts.MaxBackfillPages,
			s.backfill.ProviderName(),
		)
	}

	totalRows := 0
	offset := backfillStartOffset(checkpoint, s.opts.BackfillPageSize)
	reachedTarget := false
	seekingLocalBoundary := localOldest != nil
	if checkpoint != nil && checkpoint.OldestPostedAt != nil {
		seekingLocalBoundary = true
	}
	seekStepPages := 1
	lastAfterAnchorOffset := -1
	for page := 0; page < s.opts.MaxBackfillPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, more, err := s.backfill.FetchPage(ctx, offset, s.opts.BackfillPageSize)
		if err != nil {
			return fmt.Errorf("fetch predb backfill page offset=%d: %w", offset, err)
		}
		if len(rows) == 0 {
			if s.log != nil {
				s.log.Debug("enrich_predb sync-backfill: no rows returned offset=%d", offset)
			}
			break
		}
		oldest := oldestPredbTimestamp(rows)
		newest := newestPredbTimestamp(rows)
		anchorTime, anchorTitle := backfillAnchor(checkpoint, localOldest)
		if seekingLocalBoundary {
			if pageReachesAnchor(rows, anchorTime, anchorTitle) {
				if seekStepPages > 1 && lastAfterAnchorOffset >= 0 && offset > lastAfterAnchorOffset+len(rows) {
					offset = lastAfterAnchorOffset + len(rows)
					seekStepPages = 1
					if s.log != nil {
						s.log.Debug("enrich_predb sync-backfill: bracketed anchor; switching to linear scan offset=%d anchor=%s",
							offset,
							formatOptTime(anchorTime),
						)
					}
					continue
				}
				seekingLocalBoundary = false
				if s.log != nil {
					s.log.Info("enrich_predb sync-backfill: reached resume boundary at offset=%d oldest=%s anchor=%s",
						offset,
						formatOptTime(oldest),
						formatOptTime(anchorTime),
					)
				}
			} else {
				lastAfterAnchorOffset = offset
				nextOffset := offset + len(rows)*seekStepPages
				if seekStepPages < 64 {
					seekStepPages *= 2
				}
				if s.log != nil {
					s.log.Debug("enrich_predb sync-backfill: seeking resume boundary offset=%d next_offset=%d oldest=%s newest=%s anchor=%s step_pages=%d",
						offset,
						nextOffset,
						formatOptTime(oldest),
						formatOptTime(newest),
						formatOptTime(anchorTime),
						seekStepPages,
					)
				}
				if !more {
					break
				}
				offset = nextOffset
				continue
			}
		}
		if err := s.repo.UpsertPredbEntries(ctx, rows); err != nil {
			return fmt.Errorf("upsert predb backfill rows offset=%d: %w", offset, err)
		}
		totalRows += len(rows)
		if cp := checkpointForPage(s.backfill.ProviderName(), offset, rows); cp != nil {
			if err := s.repo.UpsertPredbBackfillCheckpoint(ctx, *cp); err != nil {
				return fmt.Errorf("upsert predb backfill checkpoint offset=%d: %w", offset, err)
			}
			checkpoint = cp
		}
		if s.log != nil {
			if page == 0 || (page+1)%10 == 0 {
				s.log.Info("enrich_predb sync-backfill: page=%d offset=%d rows=%d oldest=%s newest=%s total_rows=%d",
					page+1,
					offset,
					len(rows),
					formatOptTime(oldest),
					formatOptTime(newest),
					totalRows,
				)
			} else {
				s.log.Debug("enrich_predb sync-backfill: page=%d offset=%d rows=%d oldest=%s newest=%s",
					page+1,
					offset,
					len(rows),
					formatOptTime(oldest),
					formatOptTime(newest),
				)
			}
		}
		if oldest != nil && !oldest.After(targetFrom) {
			reachedTarget = true
			break
		}
		if !more {
			break
		}
		offset += len(rows)
	}
	if s.log != nil {
		s.log.Info("enrich_predb sync-backfill: rows=%d reached_target=%t", totalRows, reachedTarget)
	}
	return nil
}

func (s *Service) sceneNameRecoveryCandidate(ctx context.Context, candidate pgindex.ReleaseEnrichmentCandidate) (bool, error) {
	query, ok := deriveQuery(candidate)
	if !ok {
		if s.log != nil {
			s.log.Debug("enrich_predb scene-name-recovery: skip release_id=%s reason=no_query title=%q matched=%q", candidate.ReleaseID, candidate.Title, candidate.MatchedMediaTitle)
		}
		return false, nil
	}
	if s.log != nil {
		s.log.Debug(
			"enrich_predb: query release_id=%s text=%q title=%q canonical=%q year=%d is_tv=%t season=%d episode=%d title_source=%q",
			candidate.ReleaseID,
			query.Text,
			query.Title,
			query.CanonicalTitle,
			query.Year,
			query.IsTV,
			query.Season,
			query.Episode,
			candidate.TitleSource,
		)
	}

	var (
		matches []Match
		details []providerResult
		err     error
	)
	if provider, ok := s.searchProvider.(detailedSearchProvider); ok {
		matches, details, err = provider.SearchDetailed(ctx, query)
	} else {
		matches, err = s.searchProvider.Search(ctx, query)
	}
	if err != nil {
		return false, err
	}
	if s.log != nil {
		for _, detail := range details {
			if detail.ErrorText != "" {
				s.log.Debug("enrich_predb scene-name-recovery: provider release_id=%s provider=%s error=%s", candidate.ReleaseID, detail.Name, detail.ErrorText)
				continue
			}
			s.log.Debug("enrich_predb scene-name-recovery: provider release_id=%s provider=%s matches=%d", candidate.ReleaseID, detail.Name, detail.Count)
		}
	}
	if len(matches) == 0 {
		if s.log != nil {
			s.log.Debug("enrich_predb scene-name-recovery: no_matches release_id=%s query=%q", candidate.ReleaseID, query.Text)
		}
		if err := s.repo.ReplaceReleasePredbMatches(ctx, candidate.ReleaseID, nil); err != nil {
			return false, fmt.Errorf("replace predb matches: %w", err)
		}
		return false, nil
	}

	ranked := rankMatches(query, matches)
	best, haveBest := bestMatch(ranked)
	rows := toPredbRecords(candidate.ReleaseID, ranked, haveBest)
	if err := s.repo.ReplaceReleasePredbMatches(ctx, candidate.ReleaseID, rows); err != nil {
		return false, fmt.Errorf("replace predb matches: %w", err)
	}

	if !haveBest || best.Confidence < 0.82 {
		if s.log != nil {
			s.log.Debug("enrich_predb scene-name-recovery: skip release_id=%s reason=low_confidence best=%q confidence=%.3f", candidate.ReleaseID, best.Title, best.Confidence)
		}
		return false, nil
	}
	if !shouldApplyPredbTitle(candidate, best) {
		if s.log != nil {
			s.log.Debug("enrich_predb scene-name-recovery: skip release_id=%s reason=title_guard best=%q title_source=%q", candidate.ReleaseID, best.Title, candidate.TitleSource)
		}
		return false, nil
	}

	now := time.Now().UTC()
	update := pgindex.ReleasePredbUpdate{
		ReleaseID:               candidate.ReleaseID,
		Title:                   displayTitle(best.Title),
		DeobfuscatedTitle:       releaseTitle(best.Title),
		TitleSource:             "predb",
		TitleConfidence:         best.Confidence,
		IdentityStatus:          "probable",
		IdentityConfidenceScore: best.Confidence,
		MetadataUpdatedAt:       &now,
	}
	if err := s.repo.ApplyReleasePredbUpdate(ctx, update); err != nil {
		return false, fmt.Errorf("apply release predb update: %w", err)
	}
	if s.log != nil {
		s.log.Debug("enrich_predb scene-name-recovery: applied release_id=%s title=%q confidence=%.3f", candidate.ReleaseID, update.Title, update.TitleConfidence)
	}
	return true, nil
}

func (s *Service) metadataFallbackCandidate(ctx context.Context, candidate pgindex.ReleaseEnrichmentCandidate) (bool, error) {
	query, ok := deriveMetadataFallbackQuery(candidate)
	if !ok {
		if s.log != nil {
			s.log.Debug("enrich_predb metadata-only-fallback: skip release_id=%s reason=no_metadata", candidate.ReleaseID)
		}
		return false, nil
	}
	categoryHint := metadataCategoryHint(query)
	from, to := metadataWindow(query)
	entries, err := s.repo.ListPredbEntriesForWindow(ctx, from, to, categoryHint, 300)
	if err != nil {
		return false, fmt.Errorf("list local predb entries: %w", err)
	}
	if s.log != nil {
		s.log.Debug("enrich_predb metadata-only-fallback: release_id=%s candidates=%d category=%q", candidate.ReleaseID, len(entries), categoryHint)
	}
	if len(entries) == 0 {
		return false, nil
	}
	match, ok := bestMetadataFallbackMatch(query, entries)
	if !ok || match.Confidence < 0.90 {
		if s.log != nil {
			s.log.Debug("enrich_predb metadata-only-fallback: skip release_id=%s reason=low_confidence confidence=%.3f", candidate.ReleaseID, match.Confidence)
		}
		return false, nil
	}
	rows := []pgindex.ReleasePredbMatchRecord{{
		ReleaseID:       candidate.ReleaseID,
		ExternalID:      match.Entry.ExternalID,
		NormalizedTitle: match.Entry.NormalizedTitle,
		Title:           match.Entry.Title,
		Category:        match.Entry.Category,
		Source:          match.Entry.Source,
		Team:            match.Entry.Team,
		Genre:           match.Entry.Genre,
		URL:             match.Entry.URL,
		SizeKB:          match.Entry.SizeKB,
		FileCount:       match.Entry.FileCount,
		PostedAt:        match.Entry.PostedAt,
		Confidence:      match.Confidence,
		Chosen:          true,
		Payload:         match.Entry.Payload,
	}}
	if err := s.repo.ReplaceReleasePredbMatches(ctx, candidate.ReleaseID, rows); err != nil {
		return false, fmt.Errorf("replace predb metadata fallback matches: %w", err)
	}
	update := pgindex.ReleasePredbUpdate{
		ReleaseID:               candidate.ReleaseID,
		Title:                   displayTitle(match.Entry.Title),
		DeobfuscatedTitle:       releaseTitle(match.Entry.Title),
		TitleSource:             "predb",
		TitleConfidence:         match.Confidence,
		IdentityStatus:          "probable",
		IdentityConfidenceScore: match.Confidence,
	}
	now := time.Now().UTC()
	update.MetadataUpdatedAt = &now
	if err := s.repo.ApplyReleasePredbUpdate(ctx, update); err != nil {
		return false, fmt.Errorf("apply metadata fallback predb update: %w", err)
	}
	if s.log != nil {
		s.log.Debug("enrich_predb metadata-only-fallback: applied release_id=%s title=%q confidence=%.3f", candidate.ReleaseID, update.Title, update.TitleConfidence)
	}
	return true, nil
}

func toPredbRecords(releaseID string, matches []Match, haveBest bool) []pgindex.ReleasePredbMatchRecord {
	out := make([]pgindex.ReleasePredbMatchRecord, 0, len(matches))
	bestTitle := ""
	bestConfidence := -1.0
	if haveBest && len(matches) > 0 {
		bestTitle = strings.TrimSpace(matches[0].Title)
		bestConfidence = matches[0].Confidence
	}
	for _, match := range matches {
		out = append(out, pgindex.ReleasePredbMatchRecord{
			ReleaseID:       releaseID,
			ExternalID:      match.ExternalID,
			NormalizedTitle: normalizedTitle(match.Title),
			Title:           strings.TrimSpace(match.Title),
			Category:        strings.TrimSpace(match.Category),
			Source:          strings.TrimSpace(match.Source),
			Team:            strings.TrimSpace(match.Team),
			Genre:           strings.TrimSpace(match.Genre),
			URL:             strings.TrimSpace(match.URL),
			SizeKB:          match.SizeKB,
			FileCount:       match.FileCount,
			PostedAt:        match.PostedAt,
			Confidence:      match.Confidence,
			Chosen:          strings.TrimSpace(match.Title) == bestTitle && match.Confidence == bestConfidence,
			Payload:         match.Payload,
		})
	}
	return out
}

func bestMatch(matches []Match) (Match, bool) {
	for _, match := range matches {
		if strings.TrimSpace(match.Title) != "" {
			return match, true
		}
	}
	return Match{}, false
}

func shouldApplyPredbTitle(candidate pgindex.ReleaseEnrichmentCandidate, best Match) bool {
	if strings.TrimSpace(best.Title) == "" {
		return false
	}
	if candidate.TitleSource != "" && candidate.TitleSource != "source" {
		return false
	}
	return true
}

func isRecoverablePredbSearchError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "status 429"):
		return true
	case strings.Contains(text, "too many requests"):
		return true
	case strings.Contains(text, "timeout"):
		return true
	case strings.Contains(text, "i/o timeout"):
		return true
	case strings.Contains(text, "connection reset by peer"):
		return true
	case strings.Contains(text, "broken pipe"):
		return true
	case strings.Contains(text, "unexpected eof"):
		return true
	default:
		return false
	}
}

type searchProviderChain struct {
	providers []SearchProvider
}

func chainSearchProviders(providers ...SearchProvider) SearchProvider {
	filtered := make([]SearchProvider, 0, len(providers))
	for _, provider := range providers {
		if provider != nil {
			filtered = append(filtered, provider)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return searchProviderChain{providers: filtered}
}

func (p searchProviderChain) ProviderName() string {
	names := make([]string, 0, len(p.providers))
	for _, provider := range p.providers {
		names = append(names, provider.ProviderName())
	}
	return strings.Join(names, ",")
}

func (p searchProviderChain) Search(ctx context.Context, query Query) ([]Match, error) {
	matches, _, err := p.SearchDetailed(ctx, query)
	return matches, err
}

func (p searchProviderChain) SearchDetailed(ctx context.Context, query Query) ([]Match, []providerResult, error) {
	merged := []Match{}
	seen := map[string]struct{}{}
	results := make([]providerResult, 0, len(p.providers))
	var lastErr error
	for _, provider := range p.providers {
		matches, err := provider.Search(ctx, query)
		if err != nil {
			lastErr = err
			results = append(results, providerResult{
				Name:      provider.ProviderName(),
				ErrorText: err.Error(),
			})
			continue
		}
		results = append(results, providerResult{
			Name:  provider.ProviderName(),
			Count: len(matches),
		})
		for _, match := range matches {
			key := normalizedTitle(match.Title) + "|" + strings.TrimSpace(match.Source)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, match)
		}
	}
	if len(merged) == 0 && lastErr != nil {
		return nil, results, lastErr
	}
	return merged, results, nil
}

type feedProviderChain struct {
	providers []FeedProvider
}

func chainFeedProviders(providers ...FeedProvider) FeedProvider {
	filtered := make([]FeedProvider, 0, len(providers))
	for _, provider := range providers {
		if provider != nil {
			filtered = append(filtered, provider)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return feedProviderChain{providers: filtered}
}

func (p feedProviderChain) ProviderName() string {
	names := make([]string, 0, len(p.providers))
	for _, provider := range p.providers {
		names = append(names, provider.ProviderName())
	}
	return strings.Join(names, ",")
}

func (p feedProviderChain) FetchRecent(ctx context.Context, limit int) ([]pgindex.PredbEntryRecord, error) {
	merged := []pgindex.PredbEntryRecord{}
	seen := map[string]struct{}{}
	var lastErr error
	for _, provider := range p.providers {
		rows, err := provider.FetchRecent(ctx, limit)
		if err != nil {
			lastErr = err
			continue
		}
		for _, row := range rows {
			key := normalizeFeedEntryTitle(row.Title) + "|" + strings.TrimSpace(row.Source)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, row)
		}
	}
	if len(merged) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return merged, nil
}

func firstBackfillProvider(providers ...BackfillProvider) BackfillProvider {
	for _, provider := range providers {
		if provider != nil {
			return provider
		}
	}
	return nil
}

func oldestPredbTimestamp(rows []pgindex.PredbEntryRecord) *time.Time {
	var oldest *time.Time
	for _, row := range rows {
		if row.PostedAt == nil {
			continue
		}
		t := row.PostedAt.UTC()
		if oldest == nil || t.Before(*oldest) {
			oldest = &t
		}
	}
	return oldest
}

func oldestPredbEntry(rows []pgindex.PredbEntryRecord) (*pgindex.PredbEntryRecord, bool) {
	var best *pgindex.PredbEntryRecord
	for i := range rows {
		row := rows[i]
		if row.PostedAt == nil {
			continue
		}
		if best == nil {
			copy := row
			best = &copy
			continue
		}
		bestTime := best.PostedAt.UTC()
		rowTime := row.PostedAt.UTC()
		if rowTime.Before(bestTime) || (rowTime.Equal(bestTime) && normalizeFeedEntryTitle(row.Title) < normalizeFeedEntryTitle(best.Title)) {
			copy := row
			best = &copy
		}
	}
	if best == nil {
		return nil, false
	}
	return best, true
}

func newestPredbTimestamp(rows []pgindex.PredbEntryRecord) *time.Time {
	var newest *time.Time
	for _, row := range rows {
		if row.PostedAt == nil {
			continue
		}
		t := row.PostedAt.UTC()
		if newest == nil || t.After(*newest) {
			newest = &t
		}
	}
	return newest
}

func checkpointOffset(checkpoint *pgindex.PredbBackfillCheckpoint) int {
	if checkpoint == nil {
		return 0
	}
	if checkpoint.OffsetHint < 0 {
		return 0
	}
	return checkpoint.OffsetHint
}

func formatCheckpointTime(checkpoint *pgindex.PredbBackfillCheckpoint) string {
	if checkpoint == nil {
		return ""
	}
	return formatOptTime(checkpoint.OldestPostedAt)
}

func backfillStartOffset(checkpoint *pgindex.PredbBackfillCheckpoint, pageSize int) int {
	if checkpoint == nil || checkpoint.OffsetHint <= 0 || pageSize <= 0 {
		return 0
	}
	safetyWindow := pageSize * 3
	offset := checkpoint.OffsetHint - safetyWindow
	if offset < 0 {
		return 0
	}
	return offset
}

func backfillAnchor(checkpoint *pgindex.PredbBackfillCheckpoint, localOldest *time.Time) (*time.Time, string) {
	if checkpoint != nil && checkpoint.OldestPostedAt != nil {
		return checkpoint.OldestPostedAt, strings.TrimSpace(checkpoint.OldestNormalizedTitle)
	}
	return localOldest, ""
}

func pageReachesAnchor(rows []pgindex.PredbEntryRecord, anchorTime *time.Time, anchorTitle string) bool {
	if anchorTime == nil {
		return false
	}
	oldest := oldestPredbTimestamp(rows)
	if oldest != nil && oldest.Before(*anchorTime) {
		return true
	}
	for _, row := range rows {
		if row.PostedAt == nil {
			continue
		}
		t := row.PostedAt.UTC()
		if !t.Equal(anchorTime.UTC()) {
			continue
		}
		if strings.TrimSpace(anchorTitle) == "" || normalizeFeedEntryTitle(row.Title) == strings.TrimSpace(anchorTitle) {
			return true
		}
	}
	if oldest != nil && oldest.Equal(anchorTime.UTC()) {
		return true
	}
	return false
}

func checkpointForPage(provider string, offset int, rows []pgindex.PredbEntryRecord) *pgindex.PredbBackfillCheckpoint {
	oldest, ok := oldestPredbEntry(rows)
	if !ok || oldest.PostedAt == nil {
		return nil
	}
	t := oldest.PostedAt.UTC()
	return &pgindex.PredbBackfillCheckpoint{
		Provider:              strings.TrimSpace(provider),
		OffsetHint:            offset + len(rows),
		OldestPostedAt:        &t,
		OldestNormalizedTitle: normalizeFeedEntryTitle(oldest.Title),
	}
}

func formatOptTime(v *time.Time) string {
	if v == nil {
		return ""
	}
	return v.UTC().Format(time.RFC3339)
}

func normalizeFeedEntryTitle(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "_", ".")
	v = strings.Join(strings.Fields(v), ".")
	return strings.Trim(v, ".")
}
