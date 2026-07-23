package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/categories/newsnab"
)

type PublicIndexerReleaseListParams struct {
	Query             string
	Limit             int
	Offset            int
	Sort              string
	Classification    string
	BrowseCategory    string
	BrowseSubcategory string
	CategoryIDs       []int
	HasNFO            *bool
	HasPAR2           *bool
	PasswordState     string
	AvailabilityTier  string
	MediaQualityTier  string
	CompletionMin     float64
	PostedAfter       *time.Time
	PostedBefore      *time.Time
	SizeMin           int64
	SizeMax           int64
	MetadataStatus    string
	IMDBID            string
	TVDBID            int64
	Season            int
	Episode           int
	ReadyPolicy       ReleaseReadyPolicy
}

type PublicIndexerReleaseSummary struct {
	ReleaseID         string     `json:"release_id"`
	GUID              string     `json:"guid"`
	Title             string     `json:"title"`
	PostedAt          *time.Time `json:"posted_at,omitempty"`
	AddedAt           *time.Time `json:"added_at,omitempty"`
	SizeBytes         int64      `json:"size_bytes"`
	FileCount         int        `json:"file_count"`
	CompletionPct     float64    `json:"completion_pct"`
	CategoryID        int        `json:"category_id"`
	Category          string     `json:"category"`
	Classification    string     `json:"classification"`
	HasPAR2           bool       `json:"has_par2"`
	HasNFO            bool       `json:"has_nfo"`
	PasswordState     string     `json:"password_state,omitempty"`
	AvailabilityScore float64    `json:"availability_score"`
	AvailabilityTier  string     `json:"availability_tier"`
	MediaQualityScore float64    `json:"media_quality_score"`
	MediaQualityTier  string     `json:"media_quality_tier"`
	TMDBID            int64      `json:"tmdb_id,omitempty"`
	TVDBID            int64      `json:"tvdb_id,omitempty"`
	IMDBID            string     `json:"imdb_id,omitempty"`
	ExternalMediaType string     `json:"external_media_type,omitempty"`
	ExternalTitle     string     `json:"external_title,omitempty"`
	ExternalYear      int        `json:"external_year,omitempty"`
	MetadataUpdatedAt *time.Time `json:"metadata_updated_at,omitempty"`
}

type PublicIndexerReleaseFileSummary struct {
	FileName      string     `json:"file_name"`
	SizeBytes     int64      `json:"size_bytes"`
	FileIndex     int        `json:"file_index"`
	IsPars        bool       `json:"is_pars,omitempty"`
	PostedAt      *time.Time `json:"posted_at,omitempty"`
	ArticleCount  int        `json:"article_count"`
	TotalParts    int        `json:"total_parts"`
	ObservedParts int        `json:"observed_parts"`
}

type PublicIndexerReleaseDetail struct {
	Release      PublicIndexerReleaseSummary       `json:"release"`
	Files        []PublicIndexerReleaseFileSummary `json:"files"`
	Media        PublicIndexerReleaseMediaSummary  `json:"media"`
	External     PublicIndexerReleaseExternal      `json:"external"`
	Capabilities PublicIndexerReleaseCapabilities  `json:"capabilities"`
}

type PublicIndexerReleaseMediaSummary struct {
	RuntimeSeconds    int      `json:"runtime_seconds"`
	PrimaryResolution string   `json:"primary_resolution,omitempty"`
	PrimaryVideoCodec string   `json:"primary_video_codec,omitempty"`
	PrimaryAudioCodec string   `json:"primary_audio_codec,omitempty"`
	SubtitleLanguages []string `json:"subtitle_languages,omitempty"`
	SamplePresent     bool     `json:"sample_present"`
	ArchiveCount      int      `json:"archive_count"`
	VideoCount        int      `json:"video_count"`
	AudioCount        int      `json:"audio_count"`
}

type PublicIndexerReleaseExternal struct {
	TMDBID            int64      `json:"tmdb_id,omitempty"`
	TVDBID            int64      `json:"tvdb_id,omitempty"`
	IMDBID            string     `json:"imdb_id,omitempty"`
	ExternalMediaType string     `json:"external_media_type,omitempty"`
	ExternalTitle     string     `json:"external_title,omitempty"`
	ExternalYear      int        `json:"external_year,omitempty"`
	MetadataUpdatedAt *time.Time `json:"metadata_updated_at,omitempty"`
}

type PublicIndexerReleaseCapabilities struct {
	CanSendToDownloader bool `json:"can_send_to_downloader"`
}

func publicIndexerReleaseVisibilityClause(alias string, policy ReleaseReadyPolicy) string {
	if policy == (ReleaseReadyPolicy{}) {
		policy = DefaultReleaseReadyPolicy()
	}
	return releaseReadyVisibilityClause(alias, policy)
}

func sanitizePublicPasswordState(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	normalized := normalizeReleasePasswordState(raw, false, false, false)
	switch normalized {
	case "not_passworded", "password_known", "password_unknown":
		return normalized
	default:
		return ""
	}
}

func scanPublicIndexerReleaseSummary(scanner interface {
	Scan(dest ...any) error
}) (PublicIndexerReleaseSummary, error) {
	var item PublicIndexerReleaseSummary
	var (
		postedAt          sql.NullTime
		addedAt           sql.NullTime
		metadataUpdatedAt sql.NullTime
	)
	if err := scanner.Scan(
		&item.ReleaseID,
		&item.GUID,
		&item.Title,
		&postedAt,
		&addedAt,
		&item.SizeBytes,
		&item.FileCount,
		&item.CompletionPct,
		&item.CategoryID,
		&item.Category,
		&item.Classification,
		&item.HasPAR2,
		&item.HasNFO,
		&item.PasswordState,
		&item.AvailabilityScore,
		&item.AvailabilityTier,
		&item.MediaQualityScore,
		&item.MediaQualityTier,
		&item.TMDBID,
		&item.TVDBID,
		&item.ExternalMediaType,
		&item.ExternalTitle,
		&item.ExternalYear,
		&item.IMDBID,
		&metadataUpdatedAt,
	); err != nil {
		return PublicIndexerReleaseSummary{}, err
	}

	if postedAt.Valid {
		t := postedAt.Time.UTC()
		item.PostedAt = &t
	}
	if addedAt.Valid {
		t := addedAt.Time.UTC()
		item.AddedAt = &t
	}
	if metadataUpdatedAt.Valid {
		t := metadataUpdatedAt.Time.UTC()
		item.MetadataUpdatedAt = &t
	}
	item.PasswordState = sanitizePublicPasswordState(item.PasswordState)

	return item, nil
}

func normalizePublicSort(sort string) string {
	switch strings.TrimSpace(sort) {
	case "", "posted_at_desc":
		return "posted_at_desc"
	case "posted_at_asc", "size_desc", "size_asc", "title_asc", "availability_desc", "quality_desc":
		return sort
	default:
		return "posted_at_desc"
	}
}

func publicSortClause(sort string) string {
	switch normalizePublicSort(sort) {
	case "posted_at_asc":
		return "r.posted_at ASC NULLS LAST, r.updated_at DESC, COALESCE(NULLIF(ro.display_title, ''), r.title)"
	case "size_desc":
		return "r.size_bytes DESC, r.posted_at DESC NULLS LAST, COALESCE(NULLIF(ro.display_title, ''), r.title)"
	case "size_asc":
		return "r.size_bytes ASC, r.posted_at DESC NULLS LAST, COALESCE(NULLIF(ro.display_title, ''), r.title)"
	case "title_asc":
		return "COALESCE(NULLIF(ro.display_title, ''), r.title) ASC, r.posted_at DESC NULLS LAST"
	case "availability_desc":
		return "r.availability_score DESC, r.posted_at DESC NULLS LAST, COALESCE(NULLIF(ro.display_title, ''), r.title)"
	case "quality_desc":
		return "r.media_quality_score DESC, r.posted_at DESC NULLS LAST, COALESCE(NULLIF(ro.display_title, ''), r.title)"
	default:
		return "r.posted_at DESC NULLS LAST, r.updated_at DESC, COALESCE(NULLIF(ro.display_title, ''), r.title)"
	}
}

func buildPublicIndexerFilterSQL(params PublicIndexerReleaseListParams) (string, []any) {
	clauses := []string{publicIndexerReleaseVisibilityClause("r", params.ReadyPolicy)}
	args := make([]any, 0, 16)
	arg := 1

	add := func(clause string, values ...any) {
		clauses = append(clauses, clause)
		args = append(args, values...)
		arg += len(values)
	}

	if query := strings.TrimSpace(params.Query); query != "" {
		add(fmt.Sprintf("r.search_title ILIKE '%%' || $%d || '%%'", arg), query)
	}
	if v := strings.TrimSpace(params.Classification); v != "" {
		add(fmt.Sprintf("r.classification = $%d", arg), v)
	}
	if clause, values := publicBrowseClause(params.BrowseCategory, params.BrowseSubcategory, arg); clause != "" {
		add(clause, values...)
	}
	if clause, values := publicNumericCategoryClause(params.CategoryIDs, arg); clause != "" {
		add(clause, values...)
	}
	if params.HasNFO != nil {
		add(fmt.Sprintf("r.has_nfo = $%d", arg), *params.HasNFO)
	}
	if params.HasPAR2 != nil {
		add(fmt.Sprintf("r.has_par2 = $%d", arg), *params.HasPAR2)
	}
	if v := sanitizePublicPasswordState(strings.TrimSpace(params.PasswordState)); v != "" {
		add(fmt.Sprintf("%s = $%d", releasePasswordStateSQL("r"), arg), v)
	}
	if v := strings.TrimSpace(params.AvailabilityTier); v != "" {
		add(fmt.Sprintf("r.availability_tier = $%d", arg), v)
	}
	if v := strings.TrimSpace(params.MediaQualityTier); v != "" {
		add(fmt.Sprintf("r.media_quality_tier = $%d", arg), v)
	}
	if params.CompletionMin > 0 {
		add(fmt.Sprintf("r.completion_pct >= $%d", arg), params.CompletionMin)
	}
	if params.PostedAfter != nil {
		add(fmt.Sprintf("r.posted_at >= $%d", arg), params.PostedAfter.UTC())
	}
	if params.PostedBefore != nil {
		add(fmt.Sprintf("r.posted_at <= $%d", arg), params.PostedBefore.UTC())
	}
	if params.SizeMin > 0 {
		add(fmt.Sprintf("r.size_bytes >= $%d", arg), params.SizeMin)
	}
	if params.SizeMax > 0 {
		add(fmt.Sprintf("r.size_bytes <= $%d", arg), params.SizeMax)
	}
	if imdbID := normalizePublicIMDBID(params.IMDBID); imdbID != "" {
		add(fmt.Sprintf("LOWER(COALESCE(ro.imdb_id_override, '')) IN ($%d, $%d)", arg, arg+1), imdbID, "tt"+imdbID)
	}
	if params.TVDBID > 0 {
		add(fmt.Sprintf("(CASE WHEN COALESCE(ro.tvdb_id_override, 0) > 0 THEN ro.tvdb_id_override ELSE r.tvdb_id END) = $%d", arg), params.TVDBID)
	}
	if params.Season > 0 {
		add(fmt.Sprintf("r.season_number = $%d", arg), params.Season)
	}
	if params.Episode > 0 {
		add(fmt.Sprintf("r.episode_number = $%d", arg), params.Episode)
	}
	switch strings.TrimSpace(params.MetadataStatus) {
	case "updated":
		add("r.metadata_updated_at IS NOT NULL")
	case "missing":
		add("r.metadata_updated_at IS NULL")
	}

	return strings.Join(clauses, "\n  AND "), args
}

func publicNumericCategoryClause(categoryIDs []int, argStart int) (string, []any) {
	clauses := make([]string, 0, len(categoryIDs))
	args := make([]any, 0, len(categoryIDs)*2)
	seen := make(map[int]struct{}, len(categoryIDs))
	arg := argStart
	for _, id := range categoryIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if id%1000 == 0 {
			clauses = append(clauses, fmt.Sprintf("COALESCE(r.category_id, %d) BETWEEN $%d AND $%d", newsnab.OtherMisc, arg, arg+1))
			args = append(args, id, id+999)
			arg += 2
			continue
		}
		clauses = append(clauses, fmt.Sprintf("COALESCE(r.category_id, %d) = $%d", newsnab.OtherMisc, arg))
		args = append(args, id)
		arg++
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args
}

func normalizePublicIMDBID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "tt")
	return value
}

func publicBrowseClause(category, subcategory string, argStart int) (string, []any) {
	ids := newsnab.IDsForBrowse(category, subcategory)
	if len(ids) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		parts = append(parts, fmt.Sprintf("$%d", argStart+i))
		args = append(args, id)
	}
	return fmt.Sprintf("COALESCE(r.category_id, %d) IN (%s)", newsnab.OtherMisc, strings.Join(parts, ", ")), args
}

func (s *Store) ListPublicIndexerReleases(ctx context.Context, params PublicIndexerReleaseListParams) ([]PublicIndexerReleaseSummary, int, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Offset < 0 {
		params.Offset = 0
	}

	filterSQL, args := buildPublicIndexerFilterSQL(params)

	var total int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM releases r
		LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
		WHERE %s`, filterSQL),
		args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count public indexer releases: %w", err)
	}

	args = append(args, params.Limit, params.Offset)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			r.release_id,
			r.guid,
			COALESCE(NULLIF(ro.display_title, ''), r.title) AS title,
			r.posted_at,
			r.created_at,
			r.size_bytes,
			r.file_count,
			r.completion_pct,
			r.category_id,
			r.category,
			COALESCE(NULLIF(ro.classification_override, ''), r.classification) AS classification,
			r.has_par2,
			r.has_nfo,
			r.password_state,
			r.availability_score,
			r.availability_tier,
			r.media_quality_score,
			r.media_quality_tier,
			CASE WHEN COALESCE(ro.tmdb_id_override, 0) > 0 THEN ro.tmdb_id_override ELSE r.tmdb_id END,
			CASE WHEN COALESCE(ro.tvdb_id_override, 0) > 0 THEN ro.tvdb_id_override ELSE r.tvdb_id END,
			r.external_media_type,
			COALESCE(NULLIF(ro.display_title, ''), NULLIF(r.original_media_title, ''), r.title),
			r.external_year,
			COALESCE(ro.imdb_id_override, '') AS imdb_id,
			r.metadata_updated_at
		FROM releases r
		LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, filterSQL, publicSortClause(params.Sort), len(args)-1, len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list public indexer releases: %w", err)
	}
	defer rows.Close()

	out := make([]PublicIndexerReleaseSummary, 0, params.Limit)
	for rows.Next() {
		item, err := scanPublicIndexerReleaseSummary(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan public indexer release summary: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate public indexer releases: %w", err)
	}

	return out, total, nil
}

func (s *Store) GetPublicIndexerReleaseDetail(ctx context.Context, releaseID string) (*PublicIndexerReleaseDetail, error) {
	return s.GetPublicIndexerReleaseDetailWithPolicy(ctx, releaseID, DefaultReleaseReadyPolicy())
}

func (s *Store) GetPublicIndexerReleaseDetailWithPolicy(ctx context.Context, releaseID string, policy ReleaseReadyPolicy) (*PublicIndexerReleaseDetail, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT
			r.release_id,
			r.guid,
			COALESCE(NULLIF(ro.display_title, ''), r.title) AS title,
			r.posted_at,
			r.created_at,
			r.size_bytes,
			r.file_count,
			r.completion_pct,
			r.category_id,
			r.category,
			COALESCE(NULLIF(ro.classification_override, ''), r.classification) AS classification,
			r.has_par2,
			r.has_nfo,
			r.password_state,
			r.availability_score,
			r.availability_tier,
			r.media_quality_score,
			r.media_quality_tier,
			CASE WHEN COALESCE(ro.tmdb_id_override, 0) > 0 THEN ro.tmdb_id_override ELSE r.tmdb_id END,
			CASE WHEN COALESCE(ro.tvdb_id_override, 0) > 0 THEN ro.tvdb_id_override ELSE r.tvdb_id END,
			r.external_media_type,
			COALESCE(NULLIF(ro.display_title, ''), NULLIF(r.original_media_title, ''), r.title),
			r.external_year,
			COALESCE(ro.imdb_id_override, '') AS imdb_id,
			r.metadata_updated_at
		FROM releases r
		LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
		WHERE r.release_id = $1
		  AND (%s)`, publicIndexerReleaseVisibilityClause("r", policy)),
		releaseID,
	)

	release, err := scanPublicIndexerReleaseSummary(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get public indexer release detail %s: %w", releaseID, err)
	}

	var (
		runtimeSeconds    int
		primaryResolution string
		primaryVideoCodec string
		primaryAudioCodec string
		subtitleJSON      []byte
		samplePresent     bool
		archiveCount      int
		videoCount        int
		audioCount        int
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(runtime_seconds, 0),
			COALESCE(primary_resolution, ''),
			COALESCE(primary_video_codec, ''),
			COALESCE(primary_audio_codec, ''),
			COALESCE(subtitle_languages_json, '[]'::jsonb),
			COALESCE(sample_present, FALSE),
			COALESCE(archive_count, 0),
			COALESCE(video_count, 0),
			COALESCE(audio_count, 0)
		FROM releases r
		WHERE r.release_id = $1`, releaseID,
	).Scan(
		&runtimeSeconds,
		&primaryResolution,
		&primaryVideoCodec,
		&primaryAudioCodec,
		&subtitleJSON,
		&samplePresent,
		&archiveCount,
		&videoCount,
		&audioCount,
	); err != nil {
		return nil, fmt.Errorf("get public release media summary %s: %w", releaseID, err)
	}
	var subtitleLanguages []string
	if len(subtitleJSON) > 0 {
		if err := json.Unmarshal(subtitleJSON, &subtitleLanguages); err != nil {
			return nil, fmt.Errorf("decode subtitle languages for %s: %w", releaseID, err)
		}
	}

	filesRows, err := s.db.QueryContext(ctx, `
		SELECT
			cf.file_name,
			cf.size_bytes,
			cf.file_index,
			cf.is_pars,
			cf.posted_at,
			cf.article_count,
			cf.total_parts,
			cf.observed_parts
		FROM release_catalog_files cf
		WHERE cf.release_id = $1
		ORDER BY cf.file_index, cf.id`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list public release files for %s: %w", releaseID, err)
	}
	defer filesRows.Close()

	files := make([]PublicIndexerReleaseFileSummary, 0, 32)
	for filesRows.Next() {
		var item PublicIndexerReleaseFileSummary
		var postedAt sql.NullTime
		if err := filesRows.Scan(
			&item.FileName,
			&item.SizeBytes,
			&item.FileIndex,
			&item.IsPars,
			&postedAt,
			&item.ArticleCount,
			&item.TotalParts,
			&item.ObservedParts,
		); err != nil {
			return nil, fmt.Errorf("scan public release file summary: %w", err)
		}
		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}
		files = append(files, item)
	}
	if err := filesRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public release files for %s: %w", releaseID, err)
	}

	return &PublicIndexerReleaseDetail{
		Release: release,
		Files:   files,
		Media: PublicIndexerReleaseMediaSummary{
			RuntimeSeconds:    runtimeSeconds,
			PrimaryResolution: primaryResolution,
			PrimaryVideoCodec: primaryVideoCodec,
			PrimaryAudioCodec: primaryAudioCodec,
			SubtitleLanguages: subtitleLanguages,
			SamplePresent:     samplePresent,
			ArchiveCount:      archiveCount,
			VideoCount:        videoCount,
			AudioCount:        audioCount,
		},
		External: PublicIndexerReleaseExternal{
			TMDBID:            release.TMDBID,
			TVDBID:            release.TVDBID,
			IMDBID:            release.IMDBID,
			ExternalMediaType: release.ExternalMediaType,
			ExternalTitle:     release.ExternalTitle,
			ExternalYear:      release.ExternalYear,
			MetadataUpdatedAt: release.MetadataUpdatedAt,
		},
	}, nil
}
