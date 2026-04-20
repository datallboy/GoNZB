package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type PublicIndexerReleaseSummary struct {
	ReleaseID         string     `json:"release_id"`
	GUID              string     `json:"guid"`
	Title             string     `json:"title"`
	PostedAt          *time.Time `json:"posted_at,omitempty"`
	SizeBytes         int64      `json:"size_bytes"`
	FileCount         int        `json:"file_count"`
	CompletionPct     float64    `json:"completion_pct"`
	HasPAR2           bool       `json:"has_par2"`
	HasNFO            bool       `json:"has_nfo"`
	PasswordState     string     `json:"password_state,omitempty"`
	AvailabilityScore float64    `json:"availability_score"`
	AvailabilityTier  string     `json:"availability_tier"`
	MediaQualityScore float64    `json:"media_quality_score"`
	MediaQualityTier  string     `json:"media_quality_tier"`
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
	Release PublicIndexerReleaseSummary       `json:"release"`
	Files   []PublicIndexerReleaseFileSummary `json:"files"`
}

func publicIndexerReleaseVisibilityClause(alias string) string {
	return fmt.Sprintf(`
		COALESCE(%[1]s.search_title, '') <> ''
		AND LOWER(BTRIM(COALESCE(%[1]s.title, ''))) <> 'unknown-release'
		AND COALESCE(%[1]s.match_confidence, 0) >= 0.55
		AND COALESCE(%[1]s.completion_pct, 0) >= 50
		AND COALESCE(%[1]s.identity_status, '') IN ('identified', 'probable')
		AND (
			COALESCE(%[1]s.expected_file_count, 0) <= 1
			OR COALESCE(%[1]s.file_count, 0) >= 2
		)
		AND NOT (
			COALESCE(%[1]s.search_title, '') ~* '(^|[^a-z0-9])(seed|test)([^a-z0-9]|$)'
			OR COALESCE(%[1]s.group_name, '') ~* '(^|[._-])(seed|test)([._-]|$)'
		)`, alias)
}

func sanitizePublicPasswordState(raw string) string {
	switch strings.TrimSpace(raw) {
	case "not_passworded", "passworded_known", "passworded_unknown":
		return raw
	default:
		return ""
	}
}

func scanPublicIndexerReleaseSummary(scanner interface {
	Scan(dest ...any) error
}) (PublicIndexerReleaseSummary, error) {
	var item PublicIndexerReleaseSummary
	var postedAt sql.NullTime
	if err := scanner.Scan(
		&item.ReleaseID,
		&item.GUID,
		&item.Title,
		&postedAt,
		&item.SizeBytes,
		&item.FileCount,
		&item.CompletionPct,
		&item.HasPAR2,
		&item.HasNFO,
		&item.PasswordState,
		&item.AvailabilityScore,
		&item.AvailabilityTier,
		&item.MediaQualityScore,
		&item.MediaQualityTier,
	); err != nil {
		return PublicIndexerReleaseSummary{}, err
	}

	if postedAt.Valid {
		t := postedAt.Time.UTC()
		item.PostedAt = &t
	}
	item.PasswordState = sanitizePublicPasswordState(item.PasswordState)

	return item, nil
}

func (s *Store) ListPublicIndexerReleases(ctx context.Context, query string, limit, offset int) ([]PublicIndexerReleaseSummary, int, error) {
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	filter := publicIndexerReleaseVisibilityClause("r")

	var total int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM releases r
		WHERE (%s)
		  AND ($1 = '' OR r.search_title ILIKE '%%' || $1 || '%%')`, filter),
		query,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count public indexer releases: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			r.release_id,
			r.guid,
			r.title,
			r.posted_at,
			r.size_bytes,
			r.file_count,
			r.completion_pct,
			r.has_par2,
			r.has_nfo,
			r.password_state,
			r.availability_score,
			r.availability_tier,
			r.media_quality_score,
			r.media_quality_tier
		FROM releases r
		WHERE (%s)
		  AND ($1 = '' OR r.search_title ILIKE '%%' || $1 || '%%')
		ORDER BY r.posted_at DESC NULLS LAST, r.updated_at DESC, r.title
		LIMIT $2 OFFSET $3`, filter),
		query,
		limit,
		offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list public indexer releases: %w", err)
	}
	defer rows.Close()

	out := make([]PublicIndexerReleaseSummary, 0, limit)
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
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT
			r.release_id,
			r.guid,
			r.title,
			r.posted_at,
			r.size_bytes,
			r.file_count,
			r.completion_pct,
			r.has_par2,
			r.has_nfo,
			r.password_state,
			r.availability_score,
			r.availability_tier,
			r.media_quality_score,
			r.media_quality_tier
		FROM releases r
		WHERE r.release_id = $1
		  AND (%s)`, publicIndexerReleaseVisibilityClause("r")),
		releaseID,
	)

	release, err := scanPublicIndexerReleaseSummary(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get public indexer release detail %s: %w", releaseID, err)
	}

	filesRows, err := s.db.QueryContext(ctx, `
		SELECT
			rf.file_name,
			rf.size_bytes,
			rf.file_index,
			rf.is_pars,
			rf.posted_at,
			COUNT(rfa.id) AS article_count,
			COALESCE(b.total_parts, 0),
			COALESCE(b.observed_parts, 0)
		FROM release_files rf
		LEFT JOIN release_file_articles rfa ON rfa.release_file_id = rf.id
		LEFT JOIN binaries b ON b.id = rf.binary_id
		WHERE rf.release_id = $1
		GROUP BY rf.id, rf.file_name, rf.size_bytes, rf.file_index, rf.is_pars, rf.posted_at, b.total_parts, b.observed_parts
		ORDER BY rf.file_index, rf.id`, releaseID)
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
	}, nil
}
