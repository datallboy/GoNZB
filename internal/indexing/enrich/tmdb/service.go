package tmdb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/supervisor"
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
	ReplaceReleaseTMDBMatches(ctx context.Context, releaseID string, rows []pgindex.ReleaseTMDBMatchRecord) error
	ReplaceReleaseTVDBMatches(ctx context.Context, releaseID string, rows []pgindex.ReleaseTVDBMatchRecord) error
	ApplyReleaseEnrichmentUpdate(ctx context.Context, in pgindex.ReleaseEnrichmentUpdate) error
}

type Options struct {
	Limit           int
	HTTPTimeout     time.Duration
	TMDBAPIKey      string
	TMDBAccessToken string
	TMDBBaseURL     string
	TVDBAPIKey      string
	TVDBPIN         string
	TVDBBaseURL     string
}

type tmdbSearcher interface {
	SearchMovie(ctx context.Context, query string, year int) ([]externalMatch, error)
	SearchTV(ctx context.Context, query string, year int) ([]externalMatch, error)
}

type tvdbSearcher interface {
	SearchSeries(ctx context.Context, query string, year int) ([]externalMatch, error)
}

type Service struct {
	repo repository
	log  logger
	opts Options
	tmdb tmdbSearcher
	tvdb tvdbSearcher
}

func DefaultOptions(opts Options) Options {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.HTTPTimeout <= 0 {
		opts.HTTPTimeout = 15 * time.Second
	}
	if strings.TrimSpace(opts.TMDBBaseURL) == "" {
		opts.TMDBBaseURL = "https://api.themoviedb.org/3"
	}
	if strings.TrimSpace(opts.TVDBBaseURL) == "" {
		opts.TVDBBaseURL = "https://api4.thetvdb.com/v4"
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
	if strings.TrimSpace(opts.TMDBAPIKey) != "" || strings.TrimSpace(opts.TMDBAccessToken) != "" {
		svc.tmdb = newTMDBClient(opts)
	}
	if strings.TrimSpace(opts.TVDBAPIKey) != "" {
		svc.tvdb = newTVDBClient(opts)
	}
	return svc
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("enrichment repo is required")
	}
	if s.tmdb == nil && s.tvdb == nil {
		if s.log != nil {
			s.log.Debug("enrich_tmdb: no TMDB/TVDB credentials configured; skipping")
		}
		return map[string]any{"candidates": 0, "updated": 0, "tmdb_enabled": false, "tvdb_enabled": false}, nil
	}

	candidates, err := s.repo.ListReleaseEnrichmentCandidates(ctx, string(supervisor.StageEnrichTMDB), s.opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("list enrich_tmdb candidates: %w", err)
	}
	metrics := map[string]any{
		"candidates":   len(candidates),
		"updated":      0,
		"tmdb_enabled": s.tmdb != nil,
		"tvdb_enabled": s.tvdb != nil,
		"limit":        s.opts.Limit,
	}
	if len(candidates) == 0 {
		if s.log != nil {
			s.log.Debug("enrich_tmdb: no enrichment candidates available")
		}
		return metrics, nil
	}

	updated := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			metrics["updated"] = updated
			return metrics, err
		}
		applied, err := s.enrichCandidate(ctx, candidate)
		if err != nil {
			metrics["updated"] = updated
			return metrics, fmt.Errorf("enrich release %s: %w", candidate.ReleaseID, err)
		}
		if applied {
			updated++
		}
	}
	if s.log != nil {
		s.log.Info("enrich_tmdb: candidates=%d updated=%d", len(candidates), updated)
	}
	metrics["updated"] = updated
	return metrics, nil
}

func (s *Service) enrichCandidate(ctx context.Context, candidate pgindex.ReleaseEnrichmentCandidate) (bool, error) {
	query, ok := deriveReleaseQuery(candidate)
	if !ok {
		return false, nil
	}

	var (
		tmdbMatches []externalMatch
		tvdbMatches []externalMatch
		best        externalMatch
		haveBest    bool
	)

	if query.IsTV {
		if s.tvdb != nil {
			tvMatches, err := s.tvdb.SearchSeries(ctx, query.BaseTitle, query.Year)
			if err != nil {
				if s.log != nil {
					s.log.Warn("enrich_tmdb: tvdb search failed release_id=%s query=%q err=%v", candidate.ReleaseID, query.BaseTitle, err)
				}
			} else {
				tvdbMatches = rankExternalMatches(query, tvMatches)
				if best, haveBest = bestExternalMatch(tvdbMatches); haveBest && best.Confidence >= 0.88 {
					// Strong TVDB series match; no need to query TMDB TV for the same release.
				} else {
					haveBest = false
				}
			}
		}
		if !haveBest && s.tmdb != nil {
			matches, err := s.tmdb.SearchTV(ctx, query.BaseTitle, query.Year)
			if err != nil {
				if s.log != nil {
					s.log.Warn("enrich_tmdb: tmdb tv search failed release_id=%s query=%q err=%v", candidate.ReleaseID, query.BaseTitle, err)
				}
			} else {
				tmdbMatches = rankExternalMatches(query, matches)
				best, haveBest = bestExternalMatch(append(tvdbMatches, tmdbMatches...))
			}
		}
	} else if s.tmdb != nil {
		matches, err := s.tmdb.SearchMovie(ctx, query.BaseTitle, query.Year)
		if err != nil {
			if s.log != nil {
				s.log.Warn("enrich_tmdb: tmdb movie search failed release_id=%s query=%q err=%v", candidate.ReleaseID, query.BaseTitle, err)
			}
		} else {
			tmdbMatches = rankExternalMatches(query, matches)
			best, haveBest = bestExternalMatch(tmdbMatches)
		}
	}

	if err := s.repo.ReplaceReleaseTMDBMatches(ctx, candidate.ReleaseID, toTMDBMatchRecords(candidate.ReleaseID, tmdbMatches)); err != nil {
		return false, fmt.Errorf("replace tmdb matches: %w", err)
	}
	if err := s.repo.ReplaceReleaseTVDBMatches(ctx, candidate.ReleaseID, toTVDBMatchRecords(candidate.ReleaseID, tvdbMatches)); err != nil {
		return false, fmt.Errorf("replace tvdb matches: %w", err)
	}

	if !haveBest || best.Confidence < 0.72 {
		return false, nil
	}

	now := time.Now().UTC()
	update := pgindex.ReleaseEnrichmentUpdate{
		ReleaseID:               candidate.ReleaseID,
		MatchedMediaTitle:       strings.TrimSpace(best.Title),
		OriginalMediaTitle:      strings.TrimSpace(best.OriginalTitle),
		ExternalMediaType:       strings.TrimSpace(best.MediaType),
		ExternalYear:            best.Year,
		IdentityConfidenceScore: best.Confidence,
		MetadataUpdatedAt:       &now,
	}
	switch best.Source {
	case "tmdb":
		update.TMDBID = best.ExternalID
	case "tvdb":
		update.TVDBID = best.ExternalID
	}
	if best.Confidence >= 0.88 {
		update.IdentityStatus = "identified"
	} else {
		update.IdentityStatus = "probable"
	}
	if query.IsTV && query.Season > 0 && query.Episode > 0 {
		update.SeasonNumber = query.Season
		update.EpisodeNumber = query.Episode
		if best.Source == "tvdb" || best.Source == "tmdb" {
			update.SeasonEpisodeSource = "composite"
			update.SeasonEpisodeConfidence = seasonEpisodeConfidence(query.ParsedFrom, best.Confidence)
		} else {
			update.SeasonEpisodeSource = query.ParsedFrom
			update.SeasonEpisodeConfidence = 0.84
		}
	}

	if err := s.repo.ApplyReleaseEnrichmentUpdate(ctx, update); err != nil {
		return false, fmt.Errorf("apply release enrichment update: %w", err)
	}
	return true, nil
}

func toTMDBMatchRecords(releaseID string, matches []externalMatch) []pgindex.ReleaseTMDBMatchRecord {
	out := make([]pgindex.ReleaseTMDBMatchRecord, 0, len(matches))
	for i, match := range matches {
		if match.Source != "tmdb" || match.ExternalID <= 0 {
			continue
		}
		out = append(out, pgindex.ReleaseTMDBMatchRecord{
			ReleaseID:     releaseID,
			TMDBID:        match.ExternalID,
			MediaType:     match.MediaType,
			Title:         match.Title,
			OriginalTitle: match.OriginalTitle,
			Year:          match.Year,
			Confidence:    match.Confidence,
			Chosen:        i == 0,
			Payload:       match.Payload,
		})
	}
	return out
}

func toTVDBMatchRecords(releaseID string, matches []externalMatch) []pgindex.ReleaseTVDBMatchRecord {
	out := make([]pgindex.ReleaseTVDBMatchRecord, 0, len(matches))
	for i, match := range matches {
		if match.Source != "tvdb" || match.ExternalID <= 0 {
			continue
		}
		out = append(out, pgindex.ReleaseTVDBMatchRecord{
			ReleaseID:     releaseID,
			TVDBID:        match.ExternalID,
			MediaType:     match.MediaType,
			Title:         match.Title,
			OriginalTitle: match.OriginalTitle,
			Year:          match.Year,
			Confidence:    match.Confidence,
			Chosen:        i == 0,
			Payload:       match.Payload,
		})
	}
	return out
}

func seasonEpisodeConfidence(parsedFrom string, externalConfidence float64) float64 {
	base := 0.82
	switch strings.TrimSpace(parsedFrom) {
	case "deobfuscated_title":
		base = 0.90
	case "title":
		base = 0.86
	case "source_title":
		base = 0.78
	}
	score := (base + externalConfidence) / 2
	if score > 1 {
		return 1
	}
	return score
}
