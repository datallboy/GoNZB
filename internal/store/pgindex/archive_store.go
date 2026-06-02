package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type ReleaseArchiveState struct {
	ReleaseID         string     `json:"release_id"`
	ArchiveStatus     string     `json:"archive_status"`
	ArchiveStore      string     `json:"archive_store"`
	ObjectStoreKind   string     `json:"object_store_kind"`
	ObjectKey         string     `json:"object_key"`
	ContentHashSHA256 string     `json:"content_hash_sha256"`
	ObjectSizeBytes   int64      `json:"object_size_bytes"`
	ContentEncoding   string     `json:"content_encoding"`
	SourceModule      string     `json:"source_module"`
	ArchivedAt        *time.Time `json:"archived_at,omitempty"`
	PurgeEligibleAt   *time.Time `json:"purge_eligible_at,omitempty"`
	PurgeCompletedAt  *time.Time `json:"purge_completed_at,omitempty"`
	LastArchiveError  string     `json:"last_archive_error"`
}

type ReleaseArchiveCandidate struct {
	ReleaseID  string
	ProviderID int64
	Title      string
}

type ReleaseArchiveStoredRecord struct {
	ReleaseID         string
	ArchiveStore      string
	ObjectStoreKind   string
	ObjectKey         string
	ContentHashSHA256 string
	ObjectSizeBytes   int64
	ContentEncoding   string
	SourceModule      string
}

type ReleasePurgeCandidate struct {
	ReleaseID string
	ObjectKey string
}

type ReleasePurgeResult struct {
	ReleaseID                 string           `json:"release_id"`
	DeletedRowsByTable        map[string]int64 `json:"deleted_rows_by_table"`
	SkippedSharedBinaryRows   int64            `json:"skipped_shared_binary_rows"`
	DeletedBinaryRows         int64            `json:"deleted_binary_rows"`
	DeletedArticleHeaderRows  int64            `json:"deleted_article_header_rows"`
	DeletedArticlePayloadRows int64            `json:"deleted_article_payload_rows"`
}

type releaseArchiveDetailSnapshot struct {
	Release      PublicIndexerReleaseSummary
	Files        []PublicIndexerReleaseFileSummary
	Media        PublicIndexerReleaseMediaSummary
	External     PublicIndexerReleaseExternal
	Capabilities PublicIndexerReleaseCapabilities
}

func (s *Store) GetReleaseArchiveState(ctx context.Context, releaseID string) (*ReleaseArchiveState, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT
			release_id,
			archive_status,
			archive_store,
			object_store_kind,
			object_key,
			content_hash_sha256,
			object_size_bytes,
			content_encoding,
			source_module,
			archived_at,
			purge_eligible_at,
			purge_completed_at,
			last_archive_error
		FROM release_archive_state
		WHERE release_id = $1`, releaseID)

	item, err := scanReleaseArchiveState(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get release archive state %s: %w", releaseID, err)
	}
	return item, nil
}

func scanReleaseArchiveState(scanner interface{ Scan(dest ...any) error }) (*ReleaseArchiveState, error) {
	item := &ReleaseArchiveState{}
	var archivedAt, purgeEligibleAt, purgeCompletedAt sql.NullTime
	if err := scanner.Scan(
		&item.ReleaseID,
		&item.ArchiveStatus,
		&item.ArchiveStore,
		&item.ObjectStoreKind,
		&item.ObjectKey,
		&item.ContentHashSHA256,
		&item.ObjectSizeBytes,
		&item.ContentEncoding,
		&item.SourceModule,
		&archivedAt,
		&purgeEligibleAt,
		&purgeCompletedAt,
		&item.LastArchiveError,
	); err != nil {
		return nil, err
	}
	if archivedAt.Valid {
		t := archivedAt.Time.UTC()
		item.ArchivedAt = &t
	}
	if purgeEligibleAt.Valid {
		t := purgeEligibleAt.Time.UTC()
		item.PurgeEligibleAt = &t
	}
	if purgeCompletedAt.Valid {
		t := purgeCompletedAt.Time.UTC()
		item.PurgeCompletedAt = &t
	}
	return item, nil
}

func (s *Store) ClaimReleaseArchiveCandidates(ctx context.Context, limit int) ([]ReleaseArchiveCandidate, error) {
	if limit <= 0 {
		limit = 100
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin archive claim tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		WITH eligible AS (
			SELECT
				r.release_id,
				r.provider_id,
				r.title
			FROM releases r
			JOIN nzb_cache n ON n.release_id = r.release_id
			WHERE r.source_kind = 'usenet_index'
			  AND n.generation_status = 'ready'
			  AND EXISTS (SELECT 1 FROM release_files rf WHERE rf.release_id = r.release_id)
			  AND EXISTS (SELECT 1 FROM release_newsgroups rng WHERE rng.release_id = r.release_id)
			  AND COALESCE((
				SELECT ras.archive_status
				FROM release_archive_state ras
				WHERE ras.release_id = r.release_id
			  ), 'active') IN ('active', 'archive_failed')
			ORDER BY r.posted_at DESC NULLS LAST, r.release_id
			LIMIT $1
			FOR UPDATE OF r SKIP LOCKED
		),
		upserted AS (
			INSERT INTO release_archive_state (
				release_id,
				archive_status,
				last_archive_error,
				updated_at
			)
			SELECT
				e.release_id,
				'archive_pending',
				'',
				NOW()
			FROM eligible e
			ON CONFLICT (release_id) DO UPDATE
			SET archive_status = 'archive_pending',
			    last_archive_error = '',
			    updated_at = NOW()
			WHERE release_archive_state.archive_status IN ('active', 'archive_failed')
			RETURNING release_id
		)
		SELECT e.release_id, e.provider_id, e.title
		FROM eligible e
		JOIN upserted u ON u.release_id = e.release_id
		ORDER BY e.release_id`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim archive candidates: %w", err)
	}
	defer rows.Close()

	out := make([]ReleaseArchiveCandidate, 0, limit)
	for rows.Next() {
		var item ReleaseArchiveCandidate
		if err := rows.Scan(&item.ReleaseID, &item.ProviderID, &item.Title); err != nil {
			return nil, fmt.Errorf("scan archive candidate: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate archive candidates: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit archive claim tx: %w", err)
	}
	return out, nil
}

func (s *Store) MarkReleaseArchiveStored(ctx context.Context, in ReleaseArchiveStoredRecord) error {
	in.ReleaseID = strings.TrimSpace(in.ReleaseID)
	if in.ReleaseID == "" {
		return fmt.Errorf("release id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin archive store tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO release_archive_state (
			release_id,
			archive_status,
			archive_store,
			object_store_kind,
			object_key,
			content_hash_sha256,
			object_size_bytes,
			content_encoding,
			source_module,
			archived_at,
			purge_eligible_at,
			purge_completed_at,
			last_archive_error,
			updated_at
		)
		VALUES ($1, 'purge_pending', $2, $3, $4, $5, $6, $7, $8, NOW(), NOW(), NULL, '', NOW())
		ON CONFLICT (release_id) DO UPDATE
		SET archive_status = 'purge_pending',
		    archive_store = EXCLUDED.archive_store,
		    object_store_kind = EXCLUDED.object_store_kind,
		    object_key = EXCLUDED.object_key,
		    content_hash_sha256 = EXCLUDED.content_hash_sha256,
		    object_size_bytes = EXCLUDED.object_size_bytes,
		    content_encoding = EXCLUDED.content_encoding,
		    source_module = EXCLUDED.source_module,
		    archived_at = NOW(),
		    purge_eligible_at = NOW(),
		    purge_completed_at = NULL,
		    last_archive_error = '',
		    updated_at = NOW()`,
		in.ReleaseID,
		archiveFirstNonBlank(in.ArchiveStore, "indexer_archive"),
		archiveFirstNonBlank(in.ObjectStoreKind, "fs"),
		strings.TrimSpace(in.ObjectKey),
		strings.TrimSpace(in.ContentHashSHA256),
		in.ObjectSizeBytes,
		archiveFirstNonBlank(in.ContentEncoding, "identity"),
		archiveFirstNonBlank(in.SourceModule, "usenet_index"),
	); err != nil {
		return fmt.Errorf("upsert release archive state %s: %w", in.ReleaseID, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM release_archive_lineage_binaries WHERE release_id = $1`, in.ReleaseID); err != nil {
		return fmt.Errorf("clear archive lineage binaries %s: %w", in.ReleaseID, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM release_archive_lineage_article_headers WHERE release_id = $1`, in.ReleaseID); err != nil {
		return fmt.Errorf("clear archive lineage article headers %s: %w", in.ReleaseID, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO release_archive_lineage_binaries (release_id, binary_id)
		SELECT DISTINCT $1, rf.binary_id
		FROM release_files rf
		WHERE rf.release_id = $1
		  AND rf.binary_id IS NOT NULL`, in.ReleaseID); err != nil {
		return fmt.Errorf("seed archive lineage binaries %s: %w", in.ReleaseID, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO release_archive_lineage_article_headers (release_id, article_header_id)
		SELECT DISTINCT $1, bp.article_header_id
		FROM release_files rf
		JOIN binary_parts bp ON bp.binary_id = rf.binary_id
		WHERE rf.release_id = $1`, in.ReleaseID); err != nil {
		return fmt.Errorf("seed archive lineage article headers %s: %w", in.ReleaseID, err)
	}

	if err := upsertReleaseArchiveDetailSnapshot(ctx, tx, in.ReleaseID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit archive store tx %s: %w", in.ReleaseID, err)
	}
	return nil
}

func (s *Store) MarkReleaseArchiveFailed(ctx context.Context, releaseID, errText string) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO release_archive_state (
			release_id,
			archive_status,
			last_archive_error,
			updated_at
		)
		VALUES ($1, 'archive_failed', $2, NOW())
		ON CONFLICT (release_id) DO UPDATE
		SET archive_status = 'archive_failed',
		    last_archive_error = $2,
		    updated_at = NOW()`,
		releaseID,
		strings.TrimSpace(errText),
	)
	if err != nil {
		return fmt.Errorf("mark release archive failed %s: %w", releaseID, err)
	}
	return nil
}

func (s *Store) ClaimReleasePurgeCandidates(ctx context.Context, limit int) ([]ReleasePurgeCandidate, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT release_id, object_key
		FROM release_archive_state
		WHERE archive_status = 'purge_pending'
		ORDER BY purge_eligible_at ASC NULLS FIRST, archived_at ASC NULLS FIRST, release_id
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim purge candidates: %w", err)
	}
	defer rows.Close()

	out := make([]ReleasePurgeCandidate, 0, limit)
	for rows.Next() {
		var item ReleasePurgeCandidate
		if err := rows.Scan(&item.ReleaseID, &item.ObjectKey); err != nil {
			return nil, fmt.Errorf("scan purge candidate: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate purge candidates: %w", err)
	}
	return out, nil
}

func (s *Store) PurgeArchivedReleaseSources(ctx context.Context, releaseID string) (*ReleasePurgeResult, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin purge tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result := &ReleasePurgeResult{
		ReleaseID:          releaseID,
		DeletedRowsByTable: map[string]int64{},
	}

	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT archive_status
		FROM release_archive_state
		WHERE release_id = $1
		FOR UPDATE`, releaseID).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("release archive state not found for %s", releaseID)
		}
		return nil, fmt.Errorf("lock release archive state %s: %w", releaseID, err)
	}
	if status != "purge_pending" {
		return nil, fmt.Errorf("release %s is not purge_pending", releaseID)
	}

	var totalLineageBinaries int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM release_archive_lineage_binaries WHERE release_id = $1`, releaseID).Scan(&totalLineageBinaries); err != nil {
		return nil, fmt.Errorf("count release lineage binaries %s: %w", releaseID, err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT lb.binary_id
		FROM release_archive_lineage_binaries lb
		WHERE lb.release_id = $1
		  AND NOT EXISTS (
			SELECT 1
			FROM release_files other_rf
			LEFT JOIN release_archive_state other_ras ON other_ras.release_id = other_rf.release_id
			WHERE other_rf.binary_id = lb.binary_id
			  AND other_rf.release_id <> $1
			  AND COALESCE(other_ras.archive_status, 'active') NOT IN ('archived', 'purge_pending', 'purged')
		  )`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list purgeable binaries %s: %w", releaseID, err)
	}
	defer rows.Close()

	binaryIDs := make([]int64, 0, 64)
	for rows.Next() {
		var binaryID int64
		if err := rows.Scan(&binaryID); err != nil {
			return nil, fmt.Errorf("scan purgeable binary id: %w", err)
		}
		binaryIDs = append(binaryIDs, binaryID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate purgeable binary ids: %w", err)
	}
	result.SkippedSharedBinaryRows = totalLineageBinaries - int64(len(binaryIDs))

	if len(binaryIDs) > 0 {
		filter, args := bigintFilter(binaryIDs, 1)
		for _, table := range []string{
			"binary_parts",
			"binary_grouping_evidence",
			"binary_inspections",
			"binary_inspection_artifacts",
			"binary_archive_entries",
			"binary_text_evidence",
			"binary_media_streams",
			"binary_par2_sets",
			"binary_par2_targets",
			"yenc_recovery_work_items",
		} {
			count, err := countRowsByBinaryIDs(ctx, tx, table, filter, args...)
			if err != nil {
				return nil, fmt.Errorf("count %s rows for %s: %w", table, releaseID, err)
			}
			result.DeletedRowsByTable[table] = count
		}
		deleted, err := execDeleteCount(ctx, tx, `DELETE FROM binaries WHERE id IN (`+filter+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("delete binaries for %s: %w", releaseID, err)
		}
		result.DeletedBinaryRows = deleted
		result.DeletedRowsByTable["binaries"] = deleted
	}

	deletedReleaseFiles, err := execDeleteCount(ctx, tx, `
		DELETE FROM release_files
		WHERE release_id = $1`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("delete release files for %s: %w", releaseID, err)
	}
	result.DeletedRowsByTable["release_files"] = deletedReleaseFiles

	deletedReleaseNewsgroups, err := execDeleteCount(ctx, tx, `
		DELETE FROM release_newsgroups
		WHERE release_id = $1`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("delete release newsgroups for %s: %w", releaseID, err)
	}
	result.DeletedRowsByTable["release_newsgroups"] = deletedReleaseNewsgroups

	deletedNZBCache, err := execDeleteCount(ctx, tx, `
		DELETE FROM nzb_cache
		WHERE release_id = $1`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("delete nzb cache for %s: %w", releaseID, err)
	}
	result.DeletedRowsByTable["nzb_cache"] = deletedNZBCache

	deletedPayloads, err := execDeleteCount(ctx, tx, `
		DELETE FROM article_header_ingest_payloads p
		WHERE p.article_header_id IN (
			SELECT lah.article_header_id
			FROM release_archive_lineage_article_headers lah
			WHERE lah.release_id = $1
		)
		  AND NOT EXISTS (
			SELECT 1 FROM binary_parts bp WHERE bp.article_header_id = p.article_header_id
		  )`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("delete article payloads for %s: %w", releaseID, err)
	}
	result.DeletedArticlePayloadRows = deletedPayloads
	result.DeletedRowsByTable["article_header_ingest_payloads"] = deletedPayloads

	deletedHeaders, err := execDeleteCount(ctx, tx, `
		DELETE FROM article_headers ah
		WHERE ah.id IN (
			SELECT lah.article_header_id
			FROM release_archive_lineage_article_headers lah
			WHERE lah.release_id = $1
		)
		  AND NOT EXISTS (
			SELECT 1 FROM binary_parts bp WHERE bp.article_header_id = ah.id
		  )`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("delete article headers for %s: %w", releaseID, err)
	}
	result.DeletedArticleHeaderRows = deletedHeaders
	result.DeletedRowsByTable["article_headers"] = deletedHeaders

	deletedLineageBinaries, err := execDeleteCount(ctx, tx, `
		DELETE FROM release_archive_lineage_binaries
		WHERE release_id = $1`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("delete release archive lineage binaries for %s: %w", releaseID, err)
	}
	result.DeletedRowsByTable["release_archive_lineage_binaries"] = deletedLineageBinaries

	deletedLineageHeaders, err := execDeleteCount(ctx, tx, `
		DELETE FROM release_archive_lineage_article_headers
		WHERE release_id = $1`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("delete release archive lineage article headers for %s: %w", releaseID, err)
	}
	result.DeletedRowsByTable["release_archive_lineage_article_headers"] = deletedLineageHeaders

	if _, err := tx.ExecContext(ctx, `
		UPDATE release_archive_state
		SET archive_status = 'purged',
		    purge_completed_at = NOW(),
		    updated_at = NOW()
		WHERE release_id = $1`, releaseID); err != nil {
		return nil, fmt.Errorf("mark release purged %s: %w", releaseID, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit purge tx %s: %w", releaseID, err)
	}
	return result, nil
}

func execDeleteCount(ctx context.Context, runner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, query string, args ...any) (int64, error) {
	res, err := runner.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}

func bigintFilter(ids []int64, start int) (string, []any) {
	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		placeholders = append(placeholders, fmt.Sprintf("$%d", start+i))
		args = append(args, id)
	}
	return strings.Join(placeholders, ","), args
}

func countRowsByBinaryIDs(ctx context.Context, runner interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table, filter string, args ...any) (int64, error) {
	var count int64
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE binary_id IN (%s)`, table, filter)
	if err := runner.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func archiveFirstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func upsertReleaseArchiveDetailSnapshot(ctx context.Context, tx *sql.Tx, releaseID string) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO release_archive_detail_snapshots (
			release_id,
			guid,
			title,
			posted_at,
			added_at,
			size_bytes,
			file_count,
			completion_pct,
			category_id,
			category,
			classification,
			has_par2,
			has_nfo,
			password_state,
			availability_score,
			availability_tier,
			media_quality_score,
			media_quality_tier,
			tmdb_id,
			tvdb_id,
			imdb_id,
			external_media_type,
			external_title,
			external_year,
			metadata_updated_at,
			runtime_seconds,
			primary_resolution,
			primary_video_codec,
			primary_audio_codec,
			sample_present,
			archive_count,
			video_count,
			audio_count,
			updated_at
		)
		SELECT
			r.release_id,
			COALESCE(r.guid, ''),
			COALESCE(NULLIF(ro.display_title, ''), r.title),
			r.posted_at,
			r.created_at,
			r.size_bytes,
			r.file_count,
			r.completion_pct,
			r.category_id,
			COALESCE(r.category, ''),
			COALESCE(NULLIF(ro.classification_override, ''), r.classification),
			r.has_par2,
			r.has_nfo,
			r.password_state,
			r.availability_score,
			COALESCE(r.availability_tier, ''),
			r.media_quality_score,
			COALESCE(r.media_quality_tier, ''),
			CASE WHEN COALESCE(ro.tmdb_id_override, 0) > 0 THEN ro.tmdb_id_override ELSE r.tmdb_id END,
			CASE WHEN COALESCE(ro.tvdb_id_override, 0) > 0 THEN ro.tvdb_id_override ELSE r.tvdb_id END,
			COALESCE(ro.imdb_id_override, ''),
			COALESCE(r.external_media_type, ''),
			COALESCE(NULLIF(ro.display_title, ''), NULLIF(r.original_media_title, ''), r.title),
			r.external_year,
			r.metadata_updated_at,
			r.runtime_seconds,
			COALESCE(r.primary_resolution, ''),
			COALESCE(r.primary_video_codec, ''),
			COALESCE(r.primary_audio_codec, ''),
			r.sample_present,
			r.archive_count,
			r.video_count,
			r.audio_count,
			NOW()
		FROM releases r
		LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
		WHERE r.release_id = $1
		ON CONFLICT (release_id) DO UPDATE
		SET guid = EXCLUDED.guid,
		    title = EXCLUDED.title,
		    posted_at = EXCLUDED.posted_at,
		    added_at = EXCLUDED.added_at,
		    size_bytes = EXCLUDED.size_bytes,
		    file_count = EXCLUDED.file_count,
		    completion_pct = EXCLUDED.completion_pct,
		    category_id = EXCLUDED.category_id,
		    category = EXCLUDED.category,
		    classification = EXCLUDED.classification,
		    has_par2 = EXCLUDED.has_par2,
		    has_nfo = EXCLUDED.has_nfo,
		    password_state = EXCLUDED.password_state,
		    availability_score = EXCLUDED.availability_score,
		    availability_tier = EXCLUDED.availability_tier,
		    media_quality_score = EXCLUDED.media_quality_score,
		    media_quality_tier = EXCLUDED.media_quality_tier,
		    tmdb_id = EXCLUDED.tmdb_id,
		    tvdb_id = EXCLUDED.tvdb_id,
		    imdb_id = EXCLUDED.imdb_id,
		    external_media_type = EXCLUDED.external_media_type,
		    external_title = EXCLUDED.external_title,
		    external_year = EXCLUDED.external_year,
		    metadata_updated_at = EXCLUDED.metadata_updated_at,
		    runtime_seconds = EXCLUDED.runtime_seconds,
		    primary_resolution = EXCLUDED.primary_resolution,
		    primary_video_codec = EXCLUDED.primary_video_codec,
		    primary_audio_codec = EXCLUDED.primary_audio_codec,
		    sample_present = EXCLUDED.sample_present,
		    archive_count = EXCLUDED.archive_count,
		    video_count = EXCLUDED.video_count,
		    audio_count = EXCLUDED.audio_count,
		    updated_at = NOW()`,
		releaseID,
	); err != nil {
		return fmt.Errorf("upsert release archive detail snapshot %s: %w", releaseID, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM release_archive_detail_files WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("clear release archive detail files %s: %w", releaseID, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM release_archive_detail_subtitle_languages WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("clear release archive detail subtitles %s: %w", releaseID, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO release_archive_detail_files (
			release_id,
			file_name,
			size_bytes,
			file_index,
			is_pars,
			posted_at,
			article_count,
			total_parts,
			observed_parts
		)
		SELECT
			rf.release_id,
			rf.file_name,
			rf.size_bytes,
			rf.file_index,
			rf.is_pars,
			rf.posted_at,
			COUNT(bp.article_header_id)::integer AS article_count,
			COALESCE(MAX(b.total_parts), 0)::integer,
			COALESCE(MAX(b.observed_parts), 0)::integer
		FROM release_files rf
		LEFT JOIN binaries b ON b.id = rf.binary_id
		LEFT JOIN binary_parts bp ON bp.binary_id = rf.binary_id
		WHERE rf.release_id = $1
		GROUP BY rf.release_id, rf.file_name, rf.size_bytes, rf.file_index, rf.is_pars, rf.posted_at`,
		releaseID,
	); err != nil {
		return fmt.Errorf("seed release archive detail files %s: %w", releaseID, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO release_archive_detail_subtitle_languages (
			release_id,
			ordinal,
			language
		)
		SELECT
			r.release_id,
			l.ordinal::integer,
			l.language
		FROM releases r
		CROSS JOIN LATERAL jsonb_array_elements_text(COALESCE(r.subtitle_languages_json, '[]'::jsonb)) WITH ORDINALITY AS l(language, ordinal)
		WHERE r.release_id = $1`,
		releaseID,
	); err != nil {
		return fmt.Errorf("seed release archive detail subtitle languages %s: %w", releaseID, err)
	}

	return nil
}

func (s *Store) getReleaseArchiveDetailSnapshot(ctx context.Context, releaseID string) (*releaseArchiveDetailSnapshot, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			release_id,
			guid,
			title,
			posted_at,
			added_at,
			size_bytes,
			file_count,
			completion_pct,
			category_id,
			category,
			classification,
			has_par2,
			has_nfo,
			password_state,
			availability_score,
			availability_tier,
			media_quality_score,
			media_quality_tier,
			tmdb_id,
			tvdb_id,
			imdb_id,
			external_media_type,
			external_title,
			external_year,
			metadata_updated_at,
			runtime_seconds,
			primary_resolution,
			primary_video_codec,
			primary_audio_codec,
			sample_present,
			archive_count,
			video_count,
			audio_count
		FROM release_archive_detail_snapshots
		WHERE release_id = $1`, releaseID)

	var snapshot releaseArchiveDetailSnapshot
	var (
		postedAt          sql.NullTime
		addedAt           sql.NullTime
		metadataUpdatedAt sql.NullTime
	)
	if err := row.Scan(
		&snapshot.Release.ReleaseID,
		&snapshot.Release.GUID,
		&snapshot.Release.Title,
		&postedAt,
		&addedAt,
		&snapshot.Release.SizeBytes,
		&snapshot.Release.FileCount,
		&snapshot.Release.CompletionPct,
		&snapshot.Release.CategoryID,
		&snapshot.Release.Category,
		&snapshot.Release.Classification,
		&snapshot.Release.HasPAR2,
		&snapshot.Release.HasNFO,
		&snapshot.Release.PasswordState,
		&snapshot.Release.AvailabilityScore,
		&snapshot.Release.AvailabilityTier,
		&snapshot.Release.MediaQualityScore,
		&snapshot.Release.MediaQualityTier,
		&snapshot.Release.TMDBID,
		&snapshot.Release.TVDBID,
		&snapshot.Release.IMDBID,
		&snapshot.Release.ExternalMediaType,
		&snapshot.Release.ExternalTitle,
		&snapshot.Release.ExternalYear,
		&metadataUpdatedAt,
		&snapshot.Media.RuntimeSeconds,
		&snapshot.Media.PrimaryResolution,
		&snapshot.Media.PrimaryVideoCodec,
		&snapshot.Media.PrimaryAudioCodec,
		&snapshot.Media.SamplePresent,
		&snapshot.Media.ArchiveCount,
		&snapshot.Media.VideoCount,
		&snapshot.Media.AudioCount,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get release archive detail snapshot %s: %w", releaseID, err)
	}
	if postedAt.Valid {
		t := postedAt.Time.UTC()
		snapshot.Release.PostedAt = &t
	}
	if addedAt.Valid {
		t := addedAt.Time.UTC()
		snapshot.Release.AddedAt = &t
	}
	if metadataUpdatedAt.Valid {
		t := metadataUpdatedAt.Time.UTC()
		snapshot.Release.MetadataUpdatedAt = &t
	}
	snapshot.Release.PasswordState = sanitizePublicPasswordState(snapshot.Release.PasswordState)
	snapshot.External = PublicIndexerReleaseExternal{
		TMDBID:            snapshot.Release.TMDBID,
		TVDBID:            snapshot.Release.TVDBID,
		IMDBID:            snapshot.Release.IMDBID,
		ExternalMediaType: snapshot.Release.ExternalMediaType,
		ExternalTitle:     snapshot.Release.ExternalTitle,
		ExternalYear:      snapshot.Release.ExternalYear,
		MetadataUpdatedAt: snapshot.Release.MetadataUpdatedAt,
	}

	fileRows, err := s.db.QueryContext(ctx, `
		SELECT
			file_name,
			size_bytes,
			file_index,
			is_pars,
			posted_at,
			article_count,
			total_parts,
			observed_parts
		FROM release_archive_detail_files
		WHERE release_id = $1
		ORDER BY file_index, file_name`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list release archive detail files %s: %w", releaseID, err)
	}
	defer fileRows.Close()
	snapshot.Files = make([]PublicIndexerReleaseFileSummary, 0, 16)
	for fileRows.Next() {
		var item PublicIndexerReleaseFileSummary
		var filePostedAt sql.NullTime
		if err := fileRows.Scan(
			&item.FileName,
			&item.SizeBytes,
			&item.FileIndex,
			&item.IsPars,
			&filePostedAt,
			&item.ArticleCount,
			&item.TotalParts,
			&item.ObservedParts,
		); err != nil {
			return nil, fmt.Errorf("scan release archive detail file %s: %w", releaseID, err)
		}
		if filePostedAt.Valid {
			t := filePostedAt.Time.UTC()
			item.PostedAt = &t
		}
		snapshot.Files = append(snapshot.Files, item)
	}
	if err := fileRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release archive detail files %s: %w", releaseID, err)
	}

	subtitleRows, err := s.db.QueryContext(ctx, `
		SELECT language
		FROM release_archive_detail_subtitle_languages
		WHERE release_id = $1
		ORDER BY ordinal`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list release archive detail subtitles %s: %w", releaseID, err)
	}
	defer subtitleRows.Close()
	for subtitleRows.Next() {
		var language string
		if err := subtitleRows.Scan(&language); err != nil {
			return nil, fmt.Errorf("scan release archive detail subtitle %s: %w", releaseID, err)
		}
		snapshot.Media.SubtitleLanguages = append(snapshot.Media.SubtitleLanguages, language)
	}
	if err := subtitleRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release archive detail subtitles %s: %w", releaseID, err)
	}

	return &snapshot, nil
}

func (s *Store) BackfillMissingReleaseArchiveDetailSnapshots(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		limit = 500
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin release archive detail snapshot backfill tx: %w", err)
	}
	defer rollbackTx(tx)

	rows, err := tx.QueryContext(ctx, `
		SELECT ras.release_id
		FROM release_archive_state ras
		LEFT JOIN release_archive_detail_snapshots snap ON snap.release_id = ras.release_id
		WHERE ras.archive_status IN ('purge_pending', 'purged')
		  AND snap.release_id IS NULL
		ORDER BY ras.archived_at ASC NULLS FIRST, ras.release_id
		LIMIT $1
		FOR UPDATE OF ras`, limit)
	if err != nil {
		return 0, fmt.Errorf("list missing release archive detail snapshots: %w", err)
	}
	defer rows.Close()

	releaseIDs := make([]string, 0, limit)
	for rows.Next() {
		var releaseID string
		if err := rows.Scan(&releaseID); err != nil {
			return 0, fmt.Errorf("scan missing release archive detail snapshot release id: %w", err)
		}
		releaseIDs = append(releaseIDs, releaseID)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate missing release archive detail snapshots: %w", err)
	}

	for _, releaseID := range releaseIDs {
		if err := upsertReleaseArchiveDetailSnapshot(ctx, tx, releaseID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit release archive detail snapshot backfill tx: %w", err)
	}

	return int64(len(releaseIDs)), nil
}
