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
