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
	GroupName string
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

type binaryPartSourceSpan struct {
	BinaryID int64
	Min      time.Time
	Max      time.Time
}

type releaseScanner interface {
	Scan(dest ...any) error
}

func normalizeBinaryPartSourceSpan(root time.Time, min, max sql.NullTime) binaryPartSourceSpan {
	out := binaryPartSourceSpan{
		Min: root.Add(-24 * time.Hour),
		Max: root.Add(24 * time.Hour),
	}
	if min.Valid {
		out.Min = min.Time.UTC()
	}
	if max.Valid {
		out.Max = max.Time.UTC()
	}
	return out
}

func (s *Store) loadBinaryPartSourceSpan(ctx context.Context, binaryID int64) (*binaryPartSourceSpan, error) {
	var root time.Time
	var min, max sql.NullTime
	row := s.db.QueryRowContext(ctx, `
		SELECT
			bc.source_posted_at,
			bos.part_source_posted_at_min,
			bos.part_source_posted_at_max
		FROM binary_core bc
		JOIN binary_observation_stats bos
		  ON bos.source_posted_at = bc.source_posted_at
		 AND bos.binary_id = bc.binary_id
		WHERE bc.binary_id = $1
		ORDER BY bc.source_posted_at
		LIMIT 1`, binaryID)
	if err := row.Scan(&root, &min, &max); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load binary part source span %d: %w", binaryID, err)
	}
	span := normalizeBinaryPartSourceSpan(root.UTC(), min, max)
	span.BinaryID = binaryID
	return &span, nil
}

func (s *Store) loadReleaseFileBinaryPartSourceSpan(ctx context.Context, releaseFileID int64) (*binaryPartSourceSpan, error) {
	var binaryID int64
	var root time.Time
	var min, max sql.NullTime
	row := s.db.QueryRowContext(ctx, `
		SELECT
			rf.binary_id,
			bc.source_posted_at,
			bos.part_source_posted_at_min,
			bos.part_source_posted_at_max
		FROM release_catalog_files cf
		JOIN release_files rf
		  ON rf.release_id = cf.release_id
		 AND rf.file_index = cf.file_index
		 AND rf.file_name = cf.file_name
		JOIN binary_core bc ON bc.binary_id = rf.binary_id
		JOIN binary_observation_stats bos
		  ON bos.source_posted_at = bc.source_posted_at
		 AND bos.binary_id = rf.binary_id
		WHERE cf.id = $1
		ORDER BY rf.id
		LIMIT 1`, releaseFileID)
	if err := row.Scan(&binaryID, &root, &min, &max); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load release file part source span %d: %w", releaseFileID, err)
	}
	span := normalizeBinaryPartSourceSpan(root.UTC(), min, max)
	span.BinaryID = binaryID
	return &span, nil
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
			cf.id,
			COALESCE(rf.binary_id, 0),
			COALESCE(ng.group_name, ''),
			cf.file_name,
			cf.subject,
			COALESCE(cf.poster, ''),
			cf.posted_at,
			cf.size_bytes,
			cf.is_pars,
			cf.file_index
		FROM release_catalog_files cf
		LEFT JOIN release_files rf
		  ON rf.release_id = cf.release_id
		 AND rf.file_index = cf.file_index
		 AND rf.file_name = cf.file_name
		LEFT JOIN binary_core bc ON bc.binary_id = rf.binary_id
		LEFT JOIN newsgroups ng ON ng.id = bc.newsgroup_id
		WHERE cf.release_id = $1
		ORDER BY cf.file_index, cf.id`, releaseID)
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
			&item.GroupName,
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
			bc.binary_id AS binary_id,
			COALESCE(ng.group_name, '') AS group_name,
			COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), bic.release_name) AS file_name,
			COALESCE(NULLIF(bic.binary_name, ''), NULLIF(bic.file_name, ''), bic.release_name) AS subject,
			COALESCE(p.poster_name, '') AS poster,
			bos.posted_at,
			bos.total_bytes AS size_bytes,
			LOWER(COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.par2' AS is_pars,
			bic.file_index
		FROM binary_core bc
		JOIN binary_identity_current bic
		  ON bic.source_posted_at = bc.source_posted_at
		 AND bic.binary_id = bc.binary_id
		JOIN binary_observation_stats bos
		  ON bos.source_posted_at = bc.source_posted_at
		 AND bos.binary_id = bc.binary_id
		LEFT JOIN newsgroups ng ON ng.id = bc.newsgroup_id
		LEFT JOIN posters p ON p.id = bc.poster_id
		WHERE bc.binary_id = $1`, binaryID)

	var item CatalogReleaseFile
	var postedAt sql.NullTime
	if err := row.Scan(
		&item.ID,
		&item.BinaryID,
		&item.GroupName,
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

	span, err := s.loadReleaseFileBinaryPartSourceSpan(ctx, releaseFileID)
	if err != nil {
		return nil, err
	}
	if span == nil || span.BinaryID <= 0 {
		return []CatalogArticleRef{}, nil
	}
	out, err := s.listCatalogArticlesForBinarySpan(ctx, *span)
	if err != nil {
		return nil, fmt.Errorf("list catalog release file articles %d: %w", releaseFileID, err)
	}
	if len(out) > 0 {
		return out, nil
	}

	fallback, err := s.ListCatalogBinaryArticles(ctx, span.BinaryID)
	if err != nil {
		return nil, fmt.Errorf("fallback binary articles for release file %d binary %d: %w", releaseFileID, span.BinaryID, err)
	}
	return fallback, nil
}

func (s *Store) ListCatalogBinaryArticles(ctx context.Context, binaryID int64) ([]CatalogArticleRef, error) {
	if binaryID <= 0 {
		return nil, fmt.Errorf("binary id is required")
	}

	span, err := s.loadBinaryPartSourceSpan(ctx, binaryID)
	if err != nil {
		return nil, err
	}
	if span == nil {
		return []CatalogArticleRef{}, nil
	}
	return s.listCatalogArticlesForBinarySpan(ctx, *span)
}

func (s *Store) listCatalogArticlesForBinarySpan(ctx context.Context, span binaryPartSourceSpan) ([]CatalogArticleRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH ranked_parts AS (
			SELECT
				ah.message_id,
				ah.bytes,
				bp.part_number,
				ROW_NUMBER() OVER (
					PARTITION BY bp.part_number
					ORDER BY bp.source_posted_at, ah.article_number, bp.id
				) AS keep_rank
			FROM binary_parts bp
			JOIN article_headers ah
			  ON ah.source_posted_at = bp.source_posted_at
			 AND ah.id = bp.article_header_id
			 AND ah.source_posted_at >= $1
			 AND ah.source_posted_at <= $2
			WHERE bp.source_posted_at >= $1
			  AND bp.source_posted_at <= $2
			  AND bp.binary_id = $3
		)
		SELECT message_id, bytes, part_number
		FROM ranked_parts
		WHERE keep_rank = 1
		ORDER BY part_number`, span.Min, span.Max, span.BinaryID)
	if err != nil {
		return nil, fmt.Errorf("list catalog articles for binary %d span %s..%s: %w", span.BinaryID, span.Min, span.Max, err)
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

func (s *Store) ListCatalogBinaryNewsgroups(ctx context.Context, binaryID int64) ([]string, error) {
	if binaryID <= 0 {
		return nil, fmt.Errorf("binary id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ng.group_name
		FROM binary_core bc
		JOIN newsgroups ng ON ng.id = bc.newsgroup_id
		WHERE bc.binary_id = $1
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
		WITH release_groups AS (
			SELECT ng.group_name
			FROM release_newsgroups rng
			JOIN newsgroups ng ON ng.id = rng.newsgroup_id
			WHERE rng.release_id = $1
		),
		crosspost_groups AS (
			SELECT DISTINCT ahcg.observed_group_name AS group_name
			FROM release_files rf
			JOIN binary_core bc ON bc.binary_id = rf.binary_id
			JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bc.source_posted_at
			 AND bos.binary_id = rf.binary_id
			JOIN binary_parts bp
			  ON bp.binary_id = rf.binary_id
			 AND bp.source_posted_at >= COALESCE(bos.part_source_posted_at_min, bc.source_posted_at - INTERVAL '1 day')
			 AND bp.source_posted_at <= COALESCE(bos.part_source_posted_at_max, bc.source_posted_at + INTERVAL '1 day')
			JOIN article_header_crosspost_groups ahcg
			  ON ahcg.source_posted_at = bp.source_posted_at
			 AND ahcg.article_header_id = bp.article_header_id
			WHERE rf.release_id = $1
			  AND BTRIM(COALESCE(ahcg.observed_group_name, '')) <> ''
		)
		SELECT DISTINCT group_name
		FROM (
			SELECT group_name FROM release_groups
			UNION ALL
			SELECT group_name FROM crosspost_groups
		) groups
		WHERE BTRIM(COALESCE(group_name, '')) <> ''
		ORDER BY group_name`, releaseID)
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
