package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	enrichmentInsertBatchSize = 200
	predbEntryIDLookupChunk   = 10000
)

type preparedPredbEntry struct {
	NormalizedTitle string
	Title           string
	Category        string
	Source          string
	ExternalID      int64
	Team            string
	Genre           string
	URL             string
	SizeKB          float64
	FileCount       int
	PostedAt        *time.Time
	PayloadJSON     string
}

func dedupePreparedPredbEntries(rows []preparedPredbEntry) []preparedPredbEntry {
	if len(rows) <= 1 {
		return rows
	}

	indexByNormalized := make(map[string]int, len(rows))
	out := make([]preparedPredbEntry, 0, len(rows))
	for _, row := range rows {
		if idx, exists := indexByNormalized[row.NormalizedTitle]; exists {
			out[idx] = row
			continue
		}
		indexByNormalized[row.NormalizedTitle] = len(out)
		out = append(out, row)
	}
	return out
}

func execEnrichmentInsertBatch(ctx context.Context, tx *sql.Tx, insertPrefix, insertSuffix string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}

	for start := 0; start < len(rows); start += enrichmentInsertBatchSize {
		end := start + enrichmentInsertBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[start:end]

		var (
			query strings.Builder
			args  []any
		)
		query.WriteString(insertPrefix)
		args = make([]any, 0, len(batch)*len(batch[0]))

		for i, row := range batch {
			if i > 0 {
				query.WriteByte(',')
			}
			query.WriteByte('(')
			for j, value := range row {
				if j > 0 {
					query.WriteByte(',')
				}
				args = append(args, value)
				query.WriteString(fmt.Sprintf("$%d", len(args)))
			}
			query.WriteByte(')')
		}
		query.WriteString(insertSuffix)

		if _, err := tx.ExecContext(ctx, query.String(), args...); err != nil {
			return err
		}
	}

	return nil
}

func preparePredbEntryRecord(normalizedTitle, title, category, source string, externalID int64, team, genre, url string, sizeKB float64, fileCount int, postedAt *time.Time, payload map[string]any) (*preparedPredbEntry, error) {
	normalized := strings.TrimSpace(normalizedTitle)
	title = strings.TrimSpace(title)
	if normalized == "" {
		normalized = normalizePredbTitle(title)
	}
	if normalized == "" || title == "" {
		return nil, nil
	}

	payloadJSON, err := json.Marshal(jsonOrEmptyMap(payload))
	if err != nil {
		return nil, err
	}

	return &preparedPredbEntry{
		NormalizedTitle: normalized,
		Title:           title,
		Category:        strings.TrimSpace(category),
		Source:          strings.TrimSpace(source),
		ExternalID:      externalID,
		Team:            strings.TrimSpace(team),
		Genre:           strings.TrimSpace(genre),
		URL:             strings.TrimSpace(url),
		SizeKB:          sizeKB,
		FileCount:       fileCount,
		PostedAt:        postedAt,
		PayloadJSON:     string(payloadJSON),
	}, nil
}

func upsertPredbEntriesTx(ctx context.Context, tx *sql.Tx, rows []preparedPredbEntry) error {
	insertRows := make([][]any, 0, len(rows))
	for _, row := range rows {
		insertRows = append(insertRows, []any{
			row.NormalizedTitle,
			row.Title,
			row.Category,
			row.Source,
			row.ExternalID,
			row.Team,
			row.Genre,
			row.URL,
			row.SizeKB,
			row.FileCount,
			row.PostedAt,
			row.PayloadJSON,
			time.Now().UTC(),
		})
	}

	return execEnrichmentInsertBatch(ctx, tx, `
		INSERT INTO predb_entries (
			normalized_title,
			title,
			category,
			source,
			external_id,
			team,
			genre,
			url,
			size_kb,
			file_count,
			posted_at,
			payload_json,
			updated_at
		)
		VALUES `,
		`
		ON CONFLICT (normalized_title) DO UPDATE
		SET title = EXCLUDED.title,
		    category = CASE WHEN EXCLUDED.category <> '' THEN EXCLUDED.category ELSE predb_entries.category END,
		    source = CASE WHEN EXCLUDED.source <> '' THEN EXCLUDED.source ELSE predb_entries.source END,
		    external_id = CASE WHEN EXCLUDED.external_id > 0 THEN EXCLUDED.external_id ELSE predb_entries.external_id END,
		    team = CASE WHEN EXCLUDED.team <> '' THEN EXCLUDED.team ELSE predb_entries.team END,
		    genre = CASE WHEN EXCLUDED.genre <> '' THEN EXCLUDED.genre ELSE predb_entries.genre END,
		    url = CASE WHEN EXCLUDED.url <> '' THEN EXCLUDED.url ELSE predb_entries.url END,
		    size_kb = CASE WHEN EXCLUDED.size_kb > 0 THEN EXCLUDED.size_kb ELSE predb_entries.size_kb END,
		    file_count = CASE WHEN EXCLUDED.file_count > 0 THEN EXCLUDED.file_count ELSE predb_entries.file_count END,
		    posted_at = COALESCE(EXCLUDED.posted_at, predb_entries.posted_at),
		    payload_json = CASE
		    	WHEN EXCLUDED.payload_json <> '{}'::jsonb THEN EXCLUDED.payload_json
		    	ELSE predb_entries.payload_json
		    END,
		    updated_at = NOW()`, insertRows)
}

func loadPredbEntryIDsTx(ctx context.Context, tx *sql.Tx, normalizedTitles []string) (map[string]int64, error) {
	if len(normalizedTitles) == 0 {
		return map[string]int64{}, nil
	}

	out := make(map[string]int64, len(normalizedTitles))
	for start := 0; start < len(normalizedTitles); start += predbEntryIDLookupChunk {
		end := start + predbEntryIDLookupChunk
		if end > len(normalizedTitles) {
			end = len(normalizedTitles)
		}
		placeholders := make([]string, 0, end-start)
		args := make([]any, 0, end-start)
		for _, normalized := range normalizedTitles[start:end] {
			args = append(args, normalized)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		if len(args) > postgresBindParameterSoftLimit {
			return nil, fmt.Errorf("predb entry id lookup chunk has %d bind parameters", len(args))
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT normalized_title, id
			FROM predb_entries
			WHERE normalized_title IN (`+strings.Join(placeholders, ",")+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var normalized string
			var entryID int64
			if err := rows.Scan(&normalized, &entryID); err != nil {
				rows.Close()
				return nil, err
			}
			out[normalized] = entryID
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func (s *Store) ListReleaseEnrichmentCandidates(ctx context.Context, stageName string, limit int) ([]ReleaseEnrichmentCandidate, error) {
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return nil, fmt.Errorf("stage name is required")
	}
	if limit <= 0 {
		limit = 100
	}

	where := "TRUE"
	switch stageName {
	case "enrich_predb", "enrich_predb_scene_name_recovery":
		where = `(
			title_source = 'source'
			OR title_source = 'source_obfuscated'
			OR title_source = ''
			OR deobfuscated_title = ''
			OR LOWER(BTRIM(COALESCE(title, ''))) = 'unknown-release'
		) AND (
			matched_media_title <> ''
			OR BTRIM(COALESCE(deobfuscated_title, '')) <> ''
			OR BTRIM(COALESCE(title, '')) <> ''
			OR BTRIM(COALESCE(source_title, '')) <> ''
		)`
	case "enrich_predb_metadata_only_fallback":
		where = `(
			title_source = 'source'
			OR title_source = 'source_obfuscated'
			OR deobfuscated_title = ''
		) AND (
			external_media_type <> ''
			OR external_year > 0
			OR season_number > 0
			OR episode_number > 0
			OR runtime_seconds > 0
			OR primary_resolution <> ''
			OR primary_video_codec <> ''
			OR primary_audio_codec <> ''
		)`
	case "enrich_tmdb":
		where = `(
			classification IN ('video', 'video_archive')
			OR (
				classification = 'archive'
				AND (
					video_count > 0
					OR runtime_seconds > 0
					OR primary_resolution <> ''
					OR primary_video_codec <> ''
					OR matched_media_title <> ''
					OR title_source = 'archive_entry'
				)
			)
		) AND (
			(tmdb_id = 0 AND tvdb_id = 0)
			OR (
				(tvdb_id > 0 OR external_media_type = 'tv')
				AND season_number = 0
				AND episode_number = 0
			)
		)`
	default:
		return nil, fmt.Errorf("unsupported enrichment stage %q", stageName)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			release_id,
			title,
			source_title,
			deobfuscated_title,
			matched_media_title,
			title_source,
			classification,
			identity_status,
			match_confidence,
			identity_confidence_score,
			tmdb_id,
			tvdb_id,
			external_media_type,
			external_year,
			season_number,
			episode_number,
			posted_at,
			size_bytes,
			payload_size.size_bytes AS payload_size_bytes,
			payload_size.size_source AS payload_size_source,
			runtime_seconds,
			primary_resolution,
			primary_video_codec,
			primary_audio_codec
		FROM releases
		LEFT JOIN LATERAL (
			WITH payload_files AS (
				SELECT
					rf.binary_id,
					rf.file_name,
					rf.size_bytes,
					LOWER(COALESCE(rf.file_name, '')) AS lower_name
				FROM release_files rf
				WHERE rf.release_id = releases.release_id
				  AND COALESCE(rf.is_pars, FALSE) = FALSE
				  AND LOWER(COALESCE(rf.file_name, '')) !~ '\.(nfo|sfv|srr|srs|nzb|par2|vol[0-9]+\+[0-9]+\.par2)$'
			),
			file_sizes AS (
				SELECT
					pf.file_name,
					pf.lower_name ~ '(\.part0*[0-9]+\.rar|\.r[0-9]{2,3}|\.(7z|zip)\.0*[0-9]+)$' AS is_split_archive,
					pf.lower_name ~ '(\.part0*[0-9]+\.rar|\.r[0-9]{2,3}|\.rar|\.(7z|zip)(\.[0-9]+)?)$' AS is_archive_payload,
					COALESCE(par2_size.size_bytes, yenc_size.size_bytes, NULLIF(pf.size_bytes, 0), observed_size.size_bytes, 0) AS size_bytes,
					CASE
						WHEN COALESCE(par2_size.size_bytes, 0) > 0 THEN 'par2_target'
						WHEN COALESCE(yenc_size.size_bytes, 0) > 0 THEN 'yenc_or_subject_size'
						WHEN COALESCE(pf.size_bytes, 0) > 0 THEN 'release_file_observed'
						WHEN COALESCE(observed_size.size_bytes, 0) > 0 THEN 'observed_encoded_or_unknown'
						ELSE ''
					END AS size_source
				FROM payload_files pf
				LEFT JOIN LATERAL (
					SELECT MAX(bpt.file_size) AS size_bytes
					FROM binary_par2_targets bpt
					WHERE bpt.release_id = releases.release_id
					  AND LOWER(BTRIM(bpt.file_name)) = LOWER(BTRIM(pf.file_name))
					  AND COALESCE(bpt.file_size, 0) > 0
				) par2_size ON TRUE
				LEFT JOIN LATERAL (
					SELECT MAX(p.yenc_file_size) AS size_bytes
					FROM binary_parts bp
					JOIN article_header_ingest_payloads p
					  ON p.source_posted_at = bp.source_posted_at
					 AND p.article_header_id = bp.article_header_id
					WHERE bp.binary_id = pf.binary_id
					  AND COALESCE(p.yenc_file_size, 0) > 0
				) yenc_size ON TRUE
				LEFT JOIN LATERAL (
					SELECT MAX(cf.size_bytes) AS size_bytes
					FROM release_catalog_files cf
					WHERE cf.release_id = releases.release_id
					  AND LOWER(BTRIM(cf.file_name)) = LOWER(BTRIM(pf.file_name))
					  AND COALESCE(cf.is_pars, FALSE) = FALSE
					  AND COALESCE(cf.size_bytes, 0) > 0
				) observed_size ON TRUE
			),
			size_summary AS (
				SELECT
					COALESCE(SUM(size_bytes) FILTER (WHERE size_bytes > 0), 0) AS sum_size_bytes,
					COALESCE(MAX(size_bytes) FILTER (WHERE size_bytes > 0), 0) AS max_size_bytes,
					COUNT(*) FILTER (WHERE is_archive_payload) AS archive_file_count,
					COUNT(*) FILTER (WHERE is_split_archive) AS split_archive_file_count,
					COUNT(*) FILTER (WHERE size_bytes > 0) AS sized_file_count,
					COUNT(*) FILTER (WHERE size_source = 'par2_target') AS par2_file_count,
					COUNT(*) FILTER (WHERE size_source = 'yenc_or_subject_size') AS yenc_file_count,
					COUNT(*) FILTER (WHERE size_source IN ('release_file_observed', 'observed_encoded_or_unknown')) AS observed_file_count
				FROM file_sizes
			),
			top_file AS (
				SELECT size_source
				FROM file_sizes
				WHERE size_bytes > 0
				ORDER BY size_bytes DESC, file_name
				LIMIT 1
			)
			SELECT
				CASE
					WHEN size_summary.archive_file_count >= 2
					  AND size_summary.split_archive_file_count >= 1
					THEN size_summary.sum_size_bytes
					ELSE size_summary.max_size_bytes
				END AS size_bytes,
				CASE
					WHEN size_summary.archive_file_count >= 2
					  AND size_summary.split_archive_file_count >= 1
					  AND size_summary.sized_file_count > 0
					  AND size_summary.observed_file_count > 0 THEN 'decoded_payload_sum_with_observed_fallback'
					WHEN size_summary.archive_file_count >= 2
					  AND size_summary.split_archive_file_count >= 1
					  AND size_summary.sized_file_count > 0
					  AND size_summary.par2_file_count = size_summary.sized_file_count THEN 'par2_target_sum'
					WHEN size_summary.archive_file_count >= 2
					  AND size_summary.split_archive_file_count >= 1
					  AND size_summary.sized_file_count > 0
					  AND size_summary.yenc_file_count = size_summary.sized_file_count THEN 'yenc_or_subject_size_sum'
					WHEN size_summary.archive_file_count >= 2
					  AND size_summary.split_archive_file_count >= 1
					  AND size_summary.sized_file_count > 0 THEN 'decoded_payload_sum'
					ELSE COALESCE(top_file.size_source, '')
				END AS size_source
			FROM size_summary
			LEFT JOIN top_file ON TRUE
		) payload_size ON TRUE
		WHERE `+where+`
		ORDER BY updated_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list release enrichment candidates %s: %w", stageName, err)
	}
	defer rows.Close()

	out := make([]ReleaseEnrichmentCandidate, 0, limit)
	for rows.Next() {
		var item ReleaseEnrichmentCandidate
		var postedAt sql.NullTime
		if err := rows.Scan(
			&item.ReleaseID,
			&item.Title,
			&item.SourceTitle,
			&item.DeobfuscatedTitle,
			&item.MatchedMediaTitle,
			&item.TitleSource,
			&item.Classification,
			&item.IdentityStatus,
			&item.MatchConfidence,
			&item.IdentityConfidenceScore,
			&item.TMDBID,
			&item.TVDBID,
			&item.ExternalMediaType,
			&item.ExternalYear,
			&item.SeasonNumber,
			&item.EpisodeNumber,
			&postedAt,
			&item.SizeBytes,
			&item.PayloadSizeBytes,
			&item.PayloadSizeSource,
			&item.RuntimeSeconds,
			&item.PrimaryResolution,
			&item.PrimaryVideoCodec,
			&item.PrimaryAudioCodec,
		); err != nil {
			return nil, fmt.Errorf("scan release enrichment candidate: %w", err)
		}
		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release enrichment candidates %s: %w", stageName, err)
	}

	return out, nil
}

func (s *Store) ReplaceReleaseTMDBMatches(ctx context.Context, releaseID string, rows []ReleaseTMDBMatchRecord) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tmdb match replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM release_tmdb_matches WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("delete tmdb matches for %s: %w", releaseID, err)
	}

	insertRows := make([][]any, 0, len(rows))
	for _, row := range rows {
		payloadJSON, err := json.Marshal(jsonOrEmptyMap(row.Payload))
		if err != nil {
			return fmt.Errorf("marshal tmdb payload for %s/%d: %w", releaseID, row.TMDBID, err)
		}
		insertRows = append(insertRows, []any{
			releaseID,
			row.TMDBID,
			strings.TrimSpace(row.MediaType),
			strings.TrimSpace(row.Title),
			strings.TrimSpace(row.OriginalTitle),
			row.Year,
			row.Confidence,
			row.Chosen,
			string(payloadJSON),
			time.Now().UTC(),
		})
	}

	if err := execEnrichmentInsertBatch(ctx, tx, `
			INSERT INTO release_tmdb_matches (
				release_id,
				tmdb_id,
				media_type,
				title,
				original_title,
				year,
				confidence,
				chosen,
				payload_json,
				updated_at
			)
			VALUES `, ``, insertRows); err != nil {
		return fmt.Errorf("insert tmdb matches for %s: %w", releaseID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tmdb match replace tx: %w", err)
	}
	return nil
}

func (s *Store) ReplaceReleasePredbMatches(ctx context.Context, releaseID string, rows []ReleasePredbMatchRecord) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin predb match replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM release_predb_matches WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("delete predb matches for %s: %w", releaseID, err)
	}

	preparedEntries := make([]preparedPredbEntry, 0, len(rows))
	matchRows := make([]ReleasePredbMatchRecord, 0, len(rows))
	for _, row := range rows {
		prepared, err := preparePredbEntryRecord(
			row.NormalizedTitle,
			row.Title,
			row.Category,
			row.Source,
			row.ExternalID,
			row.Team,
			row.Genre,
			row.URL,
			row.SizeKB,
			row.FileCount,
			row.PostedAt,
			row.Payload,
		)
		if err != nil {
			return fmt.Errorf("marshal predb payload for %s/%s: %w", releaseID, strings.TrimSpace(row.Title), err)
		}
		if prepared == nil {
			continue
		}
		preparedEntries = append(preparedEntries, *prepared)
		row.NormalizedTitle = prepared.NormalizedTitle
		matchRows = append(matchRows, row)
	}

	preparedEntries = dedupePreparedPredbEntries(preparedEntries)
	if err := upsertPredbEntriesTx(ctx, tx, preparedEntries); err != nil {
		return fmt.Errorf("upsert predb entries for %s: %w", releaseID, err)
	}

	normalizedTitles := make([]string, 0, len(preparedEntries))
	for _, row := range preparedEntries {
		normalizedTitles = append(normalizedTitles, row.NormalizedTitle)
	}
	entryIDs, err := loadPredbEntryIDsTx(ctx, tx, normalizedTitles)
	if err != nil {
		return fmt.Errorf("load predb entry ids for %s: %w", releaseID, err)
	}

	insertRows := make([][]any, 0, len(matchRows))
	for _, row := range matchRows {
		entryID, ok := entryIDs[strings.TrimSpace(row.NormalizedTitle)]
		if !ok || entryID <= 0 {
			return fmt.Errorf("missing predb entry id for %s/%s", releaseID, row.NormalizedTitle)
		}
		insertRows = append(insertRows, []any{
			releaseID,
			entryID,
			row.Confidence,
			row.Chosen,
			time.Now().UTC(),
		})
	}

	if err := execEnrichmentInsertBatch(ctx, tx, `
			INSERT INTO release_predb_matches (
				release_id,
				predb_entry_id,
				confidence,
				chosen,
				updated_at
			)
			VALUES `, ``, insertRows); err != nil {
		return fmt.Errorf("insert predb matches for %s: %w", releaseID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit predb match replace tx: %w", err)
	}
	return nil
}

func (s *Store) UpsertPredbEntries(ctx context.Context, rows []PredbEntryRecord) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin predb entry upsert tx: %w", err)
	}
	defer rollbackTx(tx)

	preparedRows := make([]preparedPredbEntry, 0, len(rows))
	for _, row := range rows {
		prepared, err := preparePredbEntryRecord(
			row.NormalizedTitle,
			row.Title,
			row.Category,
			row.Source,
			row.ExternalID,
			row.Team,
			row.Genre,
			row.URL,
			row.SizeKB,
			row.FileCount,
			row.PostedAt,
			row.Payload,
		)
		if err != nil {
			return fmt.Errorf("marshal predb entry payload for %s: %w", strings.TrimSpace(row.Title), err)
		}
		if prepared == nil {
			continue
		}
		preparedRows = append(preparedRows, *prepared)
	}

	preparedRows = dedupePreparedPredbEntries(preparedRows)
	if err := upsertPredbEntriesTx(ctx, tx, preparedRows); err != nil {
		return fmt.Errorf("upsert predb entries: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit predb entry upsert tx: %w", err)
	}
	return nil
}

func (s *Store) GetPredbBackfillWindow(ctx context.Context) (*PredbBackfillWindow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT MIN(date_utc) FROM article_headers WHERE date_utc IS NOT NULL) AS article_min,
			(SELECT MAX(date_utc) FROM article_headers WHERE date_utc IS NOT NULL) AS article_max,
			(SELECT MIN(posted_at) FROM releases WHERE posted_at IS NOT NULL) AS release_min,
			(SELECT MAX(posted_at) FROM releases WHERE posted_at IS NOT NULL) AS release_max`)

	var articleMin sql.NullTime
	var articleMax sql.NullTime
	var releaseMin sql.NullTime
	var releaseMax sql.NullTime
	if err := row.Scan(&articleMin, &articleMax, &releaseMin, &releaseMax); err != nil {
		return nil, fmt.Errorf("get predb backfill window: %w", err)
	}

	var from *time.Time
	var to *time.Time
	applyMin := func(v sql.NullTime) {
		if !v.Valid {
			return
		}
		t := v.Time.UTC()
		if from == nil || t.Before(*from) {
			from = &t
		}
	}
	applyMax := func(v sql.NullTime) {
		if !v.Valid {
			return
		}
		t := v.Time.UTC()
		if to == nil || t.After(*to) {
			to = &t
		}
	}
	applyMin(articleMin)
	applyMin(releaseMin)
	applyMax(articleMax)
	applyMax(releaseMax)

	if from == nil && to == nil {
		return nil, nil
	}
	return &PredbBackfillWindow{
		From: from,
		To:   to,
	}, nil
}

func (s *Store) GetPredbEntryWindow(ctx context.Context) (*PredbBackfillWindow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			MIN(posted_at) AS oldest_posted_at,
			MAX(posted_at) AS newest_posted_at
		FROM predb_entries
		WHERE posted_at IS NOT NULL`)

	var oldest sql.NullTime
	var newest sql.NullTime
	if err := row.Scan(&oldest, &newest); err != nil {
		return nil, fmt.Errorf("get predb entry window: %w", err)
	}

	var from *time.Time
	var to *time.Time
	if oldest.Valid {
		t := oldest.Time.UTC()
		from = &t
	}
	if newest.Valid {
		t := newest.Time.UTC()
		to = &t
	}
	if from == nil && to == nil {
		return nil, nil
	}
	return &PredbBackfillWindow{
		From: from,
		To:   to,
	}, nil
}

func (s *Store) GetPredbBackfillCheckpoint(ctx context.Context, provider string) (*PredbBackfillCheckpoint, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil, fmt.Errorf("predb backfill provider is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT
			provider,
			offset_hint,
			oldest_posted_at,
			oldest_normalized_title
		FROM predb_backfill_checkpoints
		WHERE provider = $1`, provider)

	var item PredbBackfillCheckpoint
	var oldest sql.NullTime
	if err := row.Scan(&item.Provider, &item.OffsetHint, &oldest, &item.OldestNormalizedTitle); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get predb backfill checkpoint %s: %w", provider, err)
	}
	if oldest.Valid {
		t := oldest.Time.UTC()
		item.OldestPostedAt = &t
	}
	return &item, nil
}

func (s *Store) UpsertPredbBackfillCheckpoint(ctx context.Context, in PredbBackfillCheckpoint) error {
	in.Provider = strings.TrimSpace(in.Provider)
	if in.Provider == "" {
		return fmt.Errorf("predb backfill provider is required")
	}
	in.OldestNormalizedTitle = strings.TrimSpace(in.OldestNormalizedTitle)
	if in.OffsetHint < 0 {
		in.OffsetHint = 0
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO predb_backfill_checkpoints (
			provider,
			offset_hint,
			oldest_posted_at,
			oldest_normalized_title,
			updated_at
		)
		VALUES ($1,$2,$3,$4,NOW())
		ON CONFLICT (provider) DO UPDATE
		SET offset_hint = EXCLUDED.offset_hint,
		    oldest_posted_at = EXCLUDED.oldest_posted_at,
		    oldest_normalized_title = EXCLUDED.oldest_normalized_title,
		    updated_at = NOW()`,
		in.Provider,
		in.OffsetHint,
		in.OldestPostedAt,
		in.OldestNormalizedTitle,
	)
	if err != nil {
		return fmt.Errorf("upsert predb backfill checkpoint %s: %w", in.Provider, err)
	}
	return nil
}

func (s *Store) ListPredbEntriesForWindow(ctx context.Context, from, to *time.Time, categoryHint string, limit int) ([]PredbEntrySummary, error) {
	if limit <= 0 {
		limit = 200
	}
	categoryHint = strings.TrimSpace(categoryHint)
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			COALESCE(external_id, 0),
			normalized_title,
			title,
			category,
			source,
			team,
			genre,
			url,
			size_kb,
			file_count,
			posted_at,
			COALESCE(payload_json, '{}'::jsonb)
		FROM predb_entries
		WHERE ($1::timestamptz IS NULL OR posted_at >= $1)
		  AND ($2::timestamptz IS NULL OR posted_at <= $2)
		  AND ($3 = '' OR category ILIKE $3 || '%')
		ORDER BY
			CASE
				WHEN $1::timestamptz IS NOT NULL AND $2::timestamptz IS NOT NULL AND posted_at IS NOT NULL
					THEN ABS(EXTRACT(EPOCH FROM (posted_at - ($2::timestamptz - INTERVAL '3 days'))))
				ELSE NULL
			END ASC NULLS LAST,
			posted_at DESC NULLS LAST,
			updated_at DESC
		LIMIT $4`,
		from,
		to,
		categoryHint,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list predb entries for window: %w", err)
	}
	defer rows.Close()

	out := make([]PredbEntrySummary, 0, limit)
	for rows.Next() {
		var item PredbEntrySummary
		var postedAt sql.NullTime
		var payloadJSON []byte
		if err := rows.Scan(
			&item.EntryID,
			&item.ExternalID,
			&item.NormalizedTitle,
			&item.Title,
			&item.Category,
			&item.Source,
			&item.Team,
			&item.Genre,
			&item.URL,
			&item.SizeKB,
			&item.FileCount,
			&postedAt,
			&payloadJSON,
		); err != nil {
			return nil, fmt.Errorf("scan predb entry summary: %w", err)
		}
		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}
		if len(payloadJSON) > 0 {
			_ = json.Unmarshal(payloadJSON, &item.Payload)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate predb entries for window: %w", err)
	}
	return out, nil
}

func (s *Store) ReplaceReleaseTVDBMatches(ctx context.Context, releaseID string, rows []ReleaseTVDBMatchRecord) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tvdb match replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM release_tvdb_matches WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("delete tvdb matches for %s: %w", releaseID, err)
	}

	insertRows := make([][]any, 0, len(rows))
	for _, row := range rows {
		payloadJSON, err := json.Marshal(jsonOrEmptyMap(row.Payload))
		if err != nil {
			return fmt.Errorf("marshal tvdb payload for %s/%d: %w", releaseID, row.TVDBID, err)
		}
		insertRows = append(insertRows, []any{
			releaseID,
			row.TVDBID,
			strings.TrimSpace(row.MediaType),
			strings.TrimSpace(row.Title),
			strings.TrimSpace(row.OriginalTitle),
			row.Year,
			row.Confidence,
			row.Chosen,
			string(payloadJSON),
			time.Now().UTC(),
		})
	}

	if err := execEnrichmentInsertBatch(ctx, tx, `
			INSERT INTO release_tvdb_matches (
				release_id,
				tvdb_id,
				media_type,
				title,
				original_title,
				year,
				confidence,
				chosen,
				payload_json,
				updated_at
			)
			VALUES `, ``, insertRows); err != nil {
		return fmt.Errorf("insert tvdb matches for %s: %w", releaseID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tvdb match replace tx: %w", err)
	}
	return nil
}

func normalizePredbTitle(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "_", ".")
	v = strings.Join(strings.Fields(v), ".")
	return strings.Trim(v, ".")
}

func (s *Store) ApplyReleaseEnrichmentUpdate(ctx context.Context, in ReleaseEnrichmentUpdate) error {
	in.ReleaseID = strings.TrimSpace(in.ReleaseID)
	if in.ReleaseID == "" {
		return fmt.Errorf("release id is required")
	}

	return execReleaseMutationTx(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE releases
			SET matched_media_title = CASE
			    	WHEN $2 <> '' THEN $2
			    	ELSE matched_media_title
			    END,
			    original_media_title = CASE
			    	WHEN $3 <> '' THEN $3
			    	ELSE original_media_title
			    END,
			    tmdb_id = CASE
			    	WHEN $4 > 0 THEN $4
			    	ELSE tmdb_id
			    END,
			    tvdb_id = CASE
			    	WHEN $5 > 0 THEN $5
			    	ELSE tvdb_id
			    END,
			    external_media_type = CASE
			    	WHEN $6 <> '' THEN $6
			    	ELSE external_media_type
			    END,
			    external_year = CASE
			    	WHEN $7 > 0 THEN $7
			    	ELSE external_year
			    END,
			    season_number = CASE
			    	WHEN $8 > 0 THEN $8
			    	ELSE season_number
			    END,
			    episode_number = CASE
			    	WHEN $9 > 0 THEN $9
			    	ELSE episode_number
			    END,
			    season_episode_source = CASE
			    	WHEN $10 <> '' THEN $10
			    	ELSE season_episode_source
			    END,
			    season_episode_confidence = GREATEST(season_episode_confidence, $11),
			    identity_status = CASE
			    	WHEN $12 <> '' THEN $12
			    	ELSE identity_status
			    END,
			    identity_confidence_score = GREATEST(identity_confidence_score, $13),
			    metadata_updated_at = COALESCE($14, metadata_updated_at),
			    updated_at = NOW()
			WHERE release_id = $1`,
			in.ReleaseID,
			strings.TrimSpace(in.MatchedMediaTitle),
			strings.TrimSpace(in.OriginalMediaTitle),
			in.TMDBID,
			in.TVDBID,
			strings.TrimSpace(in.ExternalMediaType),
			in.ExternalYear,
			in.SeasonNumber,
			in.EpisodeNumber,
			strings.TrimSpace(in.SeasonEpisodeSource),
			in.SeasonEpisodeConfidence,
			strings.TrimSpace(in.IdentityStatus),
			in.IdentityConfidenceScore,
			nullableTimeValue(in.MetadataUpdatedAt),
		)
		if err != nil {
			return fmt.Errorf("apply release enrichment update %s: %w", in.ReleaseID, err)
		}
		if err := s.refreshReleaseCategoryTx(ctx, tx, in.ReleaseID); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) ApplyReleasePredbUpdate(ctx context.Context, in ReleasePredbUpdate) error {
	releaseID := strings.TrimSpace(in.ReleaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	return execReleaseMutationTx(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
				UPDATE releases
				SET title = CASE
				        WHEN $2 <> '' AND ($4 IN ('manual', 'manual_predb') OR title_source = '' OR title_source IN ('source', 'source_obfuscated')) THEN $2
				        ELSE title
				    END,
				    deobfuscated_title = CASE
				        WHEN $3 <> '' THEN $3
				        ELSE deobfuscated_title
				    END,
				    title_source = CASE
				        WHEN $4 <> '' AND ($4 IN ('manual', 'manual_predb') OR title_source = '' OR title_source IN ('source', 'source_obfuscated')) THEN $4
				        ELSE title_source
				    END,
				    title_confidence = CASE
				        WHEN $5 > title_confidence AND ($4 IN ('manual', 'manual_predb') OR title_source = '' OR title_source IN ('source', 'source_obfuscated')) THEN $5
				        ELSE title_confidence
				    END,
				    identity_status = CASE
				        WHEN $6 <> '' AND identity_status <> 'identified' THEN $6
				        ELSE identity_status
				    END,
				    identity_confidence_score = GREATEST(identity_confidence_score, $7),
				    search_title = CASE
				        WHEN $2 <> '' AND (title_source = '' OR title_source IN ('source', 'source_obfuscated')) THEN LOWER($2)
				        ELSE search_title
				    END,
			    metadata_updated_at = COALESCE($8, metadata_updated_at),
			    updated_at = NOW()
			WHERE release_id = $1`,
			releaseID,
			strings.TrimSpace(in.Title),
			strings.TrimSpace(in.DeobfuscatedTitle),
			strings.TrimSpace(in.TitleSource),
			in.TitleConfidence,
			strings.TrimSpace(in.IdentityStatus),
			in.IdentityConfidenceScore,
			nullableTimeValue(in.MetadataUpdatedAt),
		)
		if err != nil {
			return fmt.Errorf("apply release predb update %s: %w", releaseID, err)
		}
		if err := s.refreshReleaseCategoryTx(ctx, tx, releaseID); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) ApplyReleaseManualIdentity(ctx context.Context, in ReleaseManualIdentityUpdate) error {
	releaseID := strings.TrimSpace(in.ReleaseID)
	title := strings.TrimSpace(in.Title)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}
	if title == "" {
		return fmt.Errorf("title is required")
	}
	titleSource := strings.TrimSpace(in.TitleSource)
	if titleSource == "" {
		titleSource = "manual"
	}
	if titleSource != "manual" && titleSource != "manual_predb" {
		return fmt.Errorf("unsupported manual title source %q", titleSource)
	}
	identityStatus := strings.TrimSpace(in.IdentityStatus)
	if identityStatus == "" {
		identityStatus = "identified"
	}
	confidence := in.IdentityConfidenceScore
	if confidence <= 0 {
		confidence = 1
	}
	now := time.Now().UTC()
	if in.MetadataUpdatedAt != nil {
		now = in.MetadataUpdatedAt.UTC()
	}

	return execReleaseMutationTx(ctx, s.db, func(tx *sql.Tx) error {
		if in.PredbEntryID > 0 {
			var exists bool
			if err := tx.QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1
					FROM release_predb_matches
					WHERE release_id = $1
					  AND predb_entry_id = $2
				)`, releaseID, in.PredbEntryID,
			).Scan(&exists); err != nil {
				return fmt.Errorf("check release predb match %s/%d: %w", releaseID, in.PredbEntryID, err)
			}
			if !exists {
				return fmt.Errorf("predb candidate %d is not linked to release %s", in.PredbEntryID, releaseID)
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE release_predb_matches
				SET chosen = (predb_entry_id = $2),
				    updated_at = NOW()
				WHERE release_id = $1`, releaseID, in.PredbEntryID,
			); err != nil {
				return fmt.Errorf("choose release predb match %s/%d: %w", releaseID, in.PredbEntryID, err)
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE release_predb_matches
				SET chosen = FALSE,
				    updated_at = NOW()
				WHERE release_id = $1`, releaseID,
			); err != nil {
				return fmt.Errorf("clear release predb choices %s: %w", releaseID, err)
			}
		}

		deobfuscatedTitle := releaseIdentityDotTitle(title)
		_, err := tx.ExecContext(ctx, `
			UPDATE releases
			SET title = $2,
			    deobfuscated_title = $3,
			    search_title = LOWER($2),
			    title_source = $4,
			    title_confidence = GREATEST(title_confidence, $5),
			    identity_status = $6,
			    identity_confidence_score = GREATEST(identity_confidence_score, $7),
			    external_media_type = CASE WHEN $8 <> '' THEN $8 ELSE external_media_type END,
			    external_year = CASE WHEN $9 > 0 THEN $9 ELSE external_year END,
			    season_number = CASE WHEN $10 > 0 THEN $10 ELSE season_number END,
			    episode_number = CASE WHEN $11 > 0 THEN $11 ELSE episode_number END,
			    classification = CASE WHEN $12 <> '' THEN $12 ELSE classification END,
			    season_episode_source = CASE WHEN $10 > 0 OR $11 > 0 THEN $4 ELSE season_episode_source END,
			    season_episode_confidence = CASE WHEN $10 > 0 OR $11 > 0 THEN GREATEST(season_episode_confidence, $7) ELSE season_episode_confidence END,
			    metadata_updated_at = $13,
			    updated_at = NOW()
			WHERE release_id = $1`,
			releaseID,
			title,
			deobfuscatedTitle,
			titleSource,
			confidence,
			identityStatus,
			confidence,
			strings.TrimSpace(in.ExternalMediaType),
			in.ExternalYear,
			in.SeasonNumber,
			in.EpisodeNumber,
			strings.TrimSpace(in.Classification),
			now,
		)
		if err != nil {
			return fmt.Errorf("apply release manual identity %s: %w", releaseID, err)
		}
		if err := s.refreshReleaseCategoryTx(ctx, tx, releaseID); err != nil {
			return err
		}
		return nil
	})
}

func releaseIdentityDotTitle(v string) string {
	v = strings.ReplaceAll(strings.TrimSpace(v), "_", ".")
	v = strings.ReplaceAll(v, " ", ".")
	v = strings.Join(strings.FieldsFunc(v, func(r rune) bool { return r == '.' }), ".")
	return strings.Trim(v, ".")
}
