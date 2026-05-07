package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

// read DTOs for PG-backed NZB materialization in Milestone 7.
type CatalogReleaseFile struct {
	ID        int64
	BinaryID  int64
	FileName  string
	Subject   string
	Poster    string
	PostedAt  *time.Time
	SizeBytes int64
	IsPars    bool
	FileIndex int
}

type CatalogArticleRef struct {
	MessageID  string
	Bytes      int64
	PartNumber int
}

type releaseScanner interface {
	Scan(dest ...any) error
}

// CHANGED: PG release catalog read by id for later resolver work.
func (s *Store) GetCatalogReleaseByID(ctx context.Context, releaseID string) (*domain.Release, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT
			r.release_id,
			r.title,
			r.guid,
			r.source_kind,
			r.size_bytes,
			r.posted_at,
			r.category,
			r.poster,
			COALESCE(n.generation_status, 'pending')
		FROM releases r
		LEFT JOIN nzb_cache n ON n.release_id = r.release_id
		WHERE r.release_id = $1`, releaseID)

	rel, err := scanCatalogRelease(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get catalog release %s: %w", releaseID, err)
	}

	return rel, nil
}

// CHANGED: PG release catalog search for later aggregator/resolver integration.
func (s *Store) SearchCatalogReleases(ctx context.Context, query string, limit int) ([]*domain.Release, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []*domain.Release{}, nil
	}
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.release_id,
			r.title,
			r.guid,
			r.source_kind,
			r.size_bytes,
			r.posted_at,
			r.category,
			r.poster,
			COALESCE(n.generation_status, 'pending')
		FROM releases r
		LEFT JOIN nzb_cache n ON n.release_id = r.release_id
		WHERE r.search_title ILIKE '%' || $1 || '%'
		ORDER BY r.posted_at DESC NULLS LAST, r.title
		LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search catalog releases %q: %w", query, err)
	}
	defer rows.Close()

	out := make([]*domain.Release, 0, limit)
	for rows.Next() {
		rel, err := scanCatalogRelease(rows)
		if err != nil {
			return nil, fmt.Errorf("scan catalog search result: %w", err)
		}
		out = append(out, rel)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog search results: %w", err)
	}

	return out, nil
}

// CHANGED: list release files for one formed PG release.
func (s *Store) ListCatalogReleaseFiles(ctx context.Context, releaseID string) ([]CatalogReleaseFile, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			binary_id,
			file_name,
			subject,
			poster,
			posted_at,
			size_bytes,
			is_pars,
			file_index
		FROM release_files
		WHERE release_id = $1
		ORDER BY file_index, id`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list catalog release files %s: %w", releaseID, err)
	}
	defer rows.Close()

	out := make([]CatalogReleaseFile, 0, 32)
	for rows.Next() {
		var item CatalogReleaseFile
		var postedAt sql.NullTime

		if err := rows.Scan(
			&item.ID,
			&item.BinaryID,
			&item.FileName,
			&item.Subject,
			&item.Poster,
			&postedAt,
			&item.SizeBytes,
			&item.IsPars,
			&item.FileIndex,
		); err != nil {
			return nil, fmt.Errorf("scan catalog release file: %w", err)
		}

		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog release files: %w", err)
	}

	return out, nil
}

// CHANGED: list article refs for one release_file row via release_files -> binary_parts.
func (s *Store) ListCatalogReleaseFileArticles(ctx context.Context, releaseFileID int64) ([]CatalogArticleRef, error) {
	if releaseFileID <= 0 {
		return nil, fmt.Errorf("release file id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			ah.message_id,
			ah.bytes,
			bp.part_number
		FROM release_files rf
		JOIN binary_parts bp ON bp.binary_id = rf.binary_id
		JOIN article_headers ah ON ah.id = bp.article_header_id
		WHERE rf.id = $1
		ORDER BY bp.part_number`, releaseFileID)
	if err != nil {
		return nil, fmt.Errorf("list catalog release file articles %d: %w", releaseFileID, err)
	}
	defer rows.Close()

	out := make([]CatalogArticleRef, 0, 128)
	for rows.Next() {
		var item CatalogArticleRef
		if err := rows.Scan(&item.MessageID, &item.Bytes, &item.PartNumber); err != nil {
			return nil, fmt.Errorf("scan catalog article ref: %w", err)
		}
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog article refs: %w", err)
	}

	return out, nil
}

// CHANGED: list newsgroups attached to a formed release.
func (s *Store) ListCatalogReleaseNewsgroups(ctx context.Context, releaseID string) ([]string, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ng.group_name
		FROM release_newsgroups rng
		JOIN newsgroups ng ON ng.id = rng.newsgroup_id
		WHERE rng.release_id = $1
		ORDER BY ng.group_name`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list catalog release newsgroups %s: %w", releaseID, err)
	}
	defer rows.Close()

	out := make([]string, 0, 8)
	for rows.Next() {
		var groupName string
		if err := rows.Scan(&groupName); err != nil {
			return nil, fmt.Errorf("scan catalog release newsgroup: %w", err)
		}
		out = append(out, strings.TrimSpace(groupName))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog release newsgroups: %w", err)
	}

	return out, nil
}

func scanCatalogRelease(scanner releaseScanner) (*domain.Release, error) {
	var rel domain.Release
	var source string
	var postedAt sql.NullTime
	var generationStatus string

	if err := scanner.Scan(
		&rel.ID,
		&rel.Title,
		&rel.GUID,
		&source,
		&rel.Size,
		&postedAt,
		&rel.Category,
		&rel.Poster,
		&generationStatus,
	); err != nil {
		return nil, err
	}

	if postedAt.Valid {
		rel.PublishDate = postedAt.Time.UTC()
	}
	rel.Source = source
	rel.CachePresent = strings.EqualFold(generationStatus, "ready")

	return &rel, nil
}
