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
			CASE
				WHEN COALESCE(ras.object_key, '') <> ''
				  AND ras.archive_status IN ('archived', 'purge_pending', 'purged')
				THEN TRUE
				ELSE FALSE
			END
		FROM releases r
		LEFT JOIN release_archive_state ras ON ras.release_id = r.release_id
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
			CASE
				WHEN COALESCE(ras.object_key, '') <> ''
				  AND ras.archive_status IN ('archived', 'purge_pending', 'purged')
				THEN TRUE
				ELSE FALSE
			END
		FROM releases r
		LEFT JOIN release_archive_state ras ON ras.release_id = r.release_id
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
		var binaryID sql.NullInt64

		if err := rows.Scan(
			&item.ID,
			&binaryID,
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
		if binaryID.Valid {
			item.BinaryID = binaryID.Int64
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog release files: %w", err)
	}

	return out, nil
}

func (s *Store) GetCatalogBinaryFile(ctx context.Context, binaryID int64) (*CatalogReleaseFile, error) {
	if binaryID <= 0 {
		return nil, fmt.Errorf("binary id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT
			0::BIGINT AS id,
			b.id AS binary_id,
			COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), b.release_name) AS file_name,
			COALESCE(NULLIF(b.binary_name, ''), NULLIF(b.file_name, ''), b.release_name) AS subject,
			COALESCE(p.poster_name, '') AS poster,
			b.posted_at,
			b.total_bytes AS size_bytes,
			LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '')) LIKE '%.par2' AS is_pars,
			b.file_index
		FROM binaries b
		LEFT JOIN posters p ON p.id = b.poster_id
		WHERE b.id = $1`, binaryID)

	var item CatalogReleaseFile
	var postedAt sql.NullTime
	if err := row.Scan(
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
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get catalog binary file %d: %w", binaryID, err)
	}
	if postedAt.Valid {
		t := postedAt.Time.UTC()
		item.PostedAt = &t
	}
	return &item, nil
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
	if len(out) > 0 {
		return out, nil
	}

	var binaryID sql.NullInt64
	err = s.db.QueryRowContext(ctx, `
		SELECT binary_id
		FROM release_files
		WHERE id = $1`, releaseFileID,
	).Scan(&binaryID)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load release file binary %d: %w", releaseFileID, err)
	}
	if !binaryID.Valid || binaryID.Int64 <= 0 {
		return out, nil
	}

	fallback, err := s.ListCatalogBinaryArticles(ctx, binaryID.Int64)
	if err != nil {
		return nil, fmt.Errorf("fallback binary articles for release file %d binary %d: %w", releaseFileID, binaryID.Int64, err)
	}
	return fallback, nil
}

func (s *Store) ListCatalogBinaryArticles(ctx context.Context, binaryID int64) ([]CatalogArticleRef, error) {
	if binaryID <= 0 {
		return nil, fmt.Errorf("binary id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			ah.message_id,
			ah.bytes,
			bp.part_number
		FROM binary_parts bp
		JOIN article_headers ah ON ah.id = bp.article_header_id
		WHERE bp.binary_id = $1
		ORDER BY bp.part_number`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list catalog binary articles %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]CatalogArticleRef, 0, 128)
	for rows.Next() {
		var item CatalogArticleRef
		if err := rows.Scan(&item.MessageID, &item.Bytes, &item.PartNumber); err != nil {
			return nil, fmt.Errorf("scan catalog binary article ref: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog binary article refs: %w", err)
	}
	return out, nil
}

func (s *Store) ListCatalogBinaryNewsgroups(ctx context.Context, binaryID int64) ([]string, error) {
	if binaryID <= 0 {
		return nil, fmt.Errorf("binary id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ng.group_name
		FROM binaries b
		JOIN newsgroups ng ON ng.id = b.newsgroup_id
		WHERE b.id = $1
		ORDER BY ng.group_name`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list binary newsgroups %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]string, 0, 1)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan binary newsgroup: %w", err)
		}
		if strings.TrimSpace(name) != "" {
			out = append(out, strings.TrimSpace(name))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate binary newsgroups %d: %w", binaryID, err)
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
	var archived bool

	if err := scanner.Scan(
		&rel.ID,
		&rel.Title,
		&rel.GUID,
		&source,
		&rel.Size,
		&postedAt,
		&rel.Category,
		&rel.Poster,
		&archived,
	); err != nil {
		return nil, err
	}

	if postedAt.Valid {
		rel.PublishDate = postedAt.Time.UTC()
	}
	rel.Source = source
	rel.CachePresent = archived

	return &rel, nil
}
