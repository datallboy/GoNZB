package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/releasepolicy"
	"github.com/datallboy/gonzb/internal/indexing/releasetitle"
)

const inspectionReplaceInsertBatchSize = 200
const par2CoverageUpdateChunkSize = 15000
const releaseTitleCandidateLookupChunk = 10000

var (
	par2CoverageSplitArchiveRE = regexp.MustCompile(`(?i)\.(?:7z|zip)\.0*(\d+)$`)
	par2CoverageRARPartRE      = regexp.MustCompile(`(?i)\.part0*(\d+)\.rar$`)
	par2CoverageRARRIndexRE    = regexp.MustCompile(`(?i)\.r(\d{2,3})$`)
	par2CoverageRARFamilyRE    = regexp.MustCompile(`(?i)\.part\d+\.rar$|\.r\d{2,3}$`)
	par2CoverageSeparatorRE    = regexp.MustCompile(`[\[\]\(\)\{\}\-_=+,;:]+`)
	par2CoverageMultiSpaceRE   = regexp.MustCompile(`\s+`)
	par2CoverageNonKeyCharsRE  = regexp.MustCompile(`[^\pL\pN]+`)
)

func execInspectionReplaceBatch(ctx context.Context, tx *sql.Tx, insertPrefix string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}

	for start := 0; start < len(rows); start += inspectionReplaceInsertBatchSize {
		end := start + inspectionReplaceInsertBatchSize
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

		if _, err := tx.ExecContext(ctx, query.String(), args...); err != nil {
			return err
		}
	}

	return nil
}

type inspectionReleaseIDQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (s *Store) existingReleaseIDsForInspectionRows(ctx context.Context, q inspectionReleaseIDQueryer, releaseIDs []string) (map[string]bool, error) {
	uniqueIDs := make([]string, 0, len(releaseIDs))
	seen := make(map[string]bool, len(releaseIDs))
	for _, releaseID := range releaseIDs {
		releaseID = strings.TrimSpace(releaseID)
		if releaseID == "" || seen[releaseID] {
			continue
		}
		seen[releaseID] = true
		uniqueIDs = append(uniqueIDs, releaseID)
	}
	if len(uniqueIDs) == 0 {
		return map[string]bool{}, nil
	}

	placeholders := make([]string, 0, len(uniqueIDs))
	args := make([]any, 0, len(uniqueIDs))
	for i, releaseID := range uniqueIDs {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, releaseID)
	}

	rows, err := q.QueryContext(ctx, `
		SELECT release_id
		FROM releases
		WHERE release_id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := make(map[string]bool, len(uniqueIDs))
	for rows.Next() {
		var releaseID string
		if err := rows.Scan(&releaseID); err != nil {
			return nil, err
		}
		existing[strings.TrimSpace(releaseID)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return existing, nil
}

func binaryStillExistsInTx(ctx context.Context, tx *sql.Tx, binaryID int64) (bool, error) {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM binary_core WHERE binary_id = $1)`, binaryID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check binary existence %d: %w", binaryID, err)
	}
	return exists, nil
}

func artifactReleaseIDs(rows []BinaryInspectionArtifactRecord) []string {
	releaseIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		releaseIDs = append(releaseIDs, row.ReleaseID)
	}
	return releaseIDs
}

func par2SetReleaseIDs(rows []BinaryPAR2SetRecord) []string {
	releaseIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		releaseIDs = append(releaseIDs, row.ReleaseID)
	}
	return releaseIDs
}

func par2TargetReleaseIDs(rows []BinaryPAR2TargetRecord) []string {
	releaseIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		releaseIDs = append(releaseIDs, row.ReleaseID)
	}
	return releaseIDs
}

func archiveEntryReleaseIDs(rows []BinaryArchiveEntryRecord) []string {
	releaseIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		releaseIDs = append(releaseIDs, row.ReleaseID)
	}
	return releaseIDs
}

func mediaStreamReleaseIDs(rows []BinaryMediaStreamRecord) []string {
	releaseIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		releaseIDs = append(releaseIDs, row.ReleaseID)
	}
	return releaseIDs
}

func (s *Store) ListReleaseTitleCandidates(ctx context.Context, binaryIDs []int64) ([]ReleaseTitleCandidate, error) {
	if len(binaryIDs) == 0 {
		return nil, nil
	}

	out := make([]ReleaseTitleCandidate, 0, len(binaryIDs)*3)
	for start := 0; start < len(binaryIDs); start += releaseTitleCandidateLookupChunk {
		end := start + releaseTitleCandidateLookupChunk
		if end > len(binaryIDs) {
			end = len(binaryIDs)
		}
		placeholders := make([]string, 0, end-start)
		args := make([]any, 0, end-start)
		for _, binaryID := range binaryIDs[start:end] {
			if binaryID <= 0 {
				continue
			}
			args = append(args, binaryID)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		if len(args) == 0 {
			continue
		}
		if len(args) > postgresBindParameterSoftLimit {
			return nil, fmt.Errorf("release title candidate lookup chunk has %d bind parameters", len(args))
		}
		filter := strings.Join(placeholders, ",")

		appendRows := func(query string, source string, confidence float64) error {
			rows, err := s.db.QueryContext(ctx, query, args...)
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var item ReleaseTitleCandidate
				item.Source = source
				item.Confidence = confidence
				if err := rows.Scan(&item.BinaryID, &item.Value); err != nil {
					return err
				}
				item.Value = strings.TrimSpace(item.Value)
				if item.Value == "" {
					continue
				}
				out = append(out, item)
			}
			return rows.Err()
		}

		if err := appendRows(`
			SELECT binary_id, summary_json->>'archive_entry'
			FROM binary_inspections
			WHERE stage_name = 'inspect_media'
			  AND binary_id IN (`+filter+`)
			  AND COALESCE(summary_json->>'archive_entry', '') <> ''`,
			"archive_entry", 0.98); err != nil {
			return nil, fmt.Errorf("list inspect_media title candidates: %w", err)
		}

		if err := appendRows(`
			SELECT binary_id, entry_name
			FROM binary_archive_entries
			WHERE binary_id IN (`+filter+`)
			  AND is_dir = FALSE
			  AND (
				media_type IN ('video', 'audio')
				OR lower(entry_name) ~ '\.(mkv|mp4|avi|ts|flac|mp3|m4a)$'
			  )`,
			"archive_entry", 0.92); err != nil {
			return nil, fmt.Errorf("list archive entry title candidates: %w", err)
		}

		if err := appendRows(`
			SELECT binary_id, text_value
			FROM binary_text_evidence
			WHERE stage_name = 'inspect_nfo'
			  AND evidence_kind = 'nfo_text'
			  AND binary_id IN (`+filter+`)
			  AND text_value <> ''`,
			"nfo", 0.84); err != nil {
			return nil, fmt.Errorf("list nfo title candidates: %w", err)
		}
	}

	return out, nil
}

func (s *Store) listIndexerPasswordCandidates(ctx context.Context, releaseID string) ([]IndexerPasswordCandidateSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			COALESCE(binary_id, 0),
			COALESCE(artifact_id, 0),
			source_kind,
			source_ref,
			confidence,
			verification_status,
			last_verified_at,
			last_error
		FROM release_password_candidates
		WHERE release_id = $1
		ORDER BY verification_status DESC, confidence DESC, updated_at DESC, id DESC`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list password candidates for %s: %w", releaseID, err)
	}
	defer rows.Close()

	out := make([]IndexerPasswordCandidateSummary, 0, 8)
	for rows.Next() {
		var item IndexerPasswordCandidateSummary
		var lastVerifiedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.BinaryID,
			&item.ArtifactID,
			&item.SourceKind,
			&item.SourceRef,
			&item.Confidence,
			&item.VerificationStatus,
			&lastVerifiedAt,
			&item.LastError,
		); err != nil {
			return nil, fmt.Errorf("scan password candidate summary: %w", err)
		}
		if lastVerifiedAt.Valid {
			t := lastVerifiedAt.Time.UTC()
			item.LastVerifiedAt = &t
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate password candidates for %s: %w", releaseID, err)
	}

	return out, nil
}

func (s *Store) listIndexerExternalMatches(ctx context.Context, source, releaseID string) ([]IndexerExternalMatchSummary, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	var query string
	switch strings.TrimSpace(source) {
	case "tmdb":
		query = `
			SELECT
				tmdb_id,
				media_type,
				title,
				original_title,
				year,
				confidence,
				chosen,
				payload_json
			FROM release_tmdb_matches
			WHERE release_id = $1
			ORDER BY chosen DESC, confidence DESC, title`
	case "tvdb":
		query = `
			SELECT
				tvdb_id,
				media_type,
				title,
				original_title,
				year,
				confidence,
				chosen,
				payload_json
			FROM release_tvdb_matches
			WHERE release_id = $1
			ORDER BY chosen DESC, confidence DESC, title`
	default:
		return nil, fmt.Errorf("unsupported external match source %q", source)
	}

	rows, err := s.db.QueryContext(ctx, query, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list %s matches for %s: %w", source, releaseID, err)
	}
	defer rows.Close()

	out := []IndexerExternalMatchSummary{}
	for rows.Next() {
		var item IndexerExternalMatchSummary
		item.Source = source
		if err := rows.Scan(
			&item.ExternalID,
			&item.MediaType,
			&item.Title,
			&item.OriginalTitle,
			&item.Year,
			&item.Confidence,
			&item.Chosen,
			&item.Payload,
		); err != nil {
			return nil, fmt.Errorf("scan %s match summary: %w", source, err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s matches for %s: %w", source, releaseID, err)
	}

	return out, nil
}

func (s *Store) listIndexerPredbMatches(ctx context.Context, releaseID string) ([]IndexerPredbMatchSummary, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			p.id,
			p.title,
			p.category,
			p.source,
			COALESCE(p.team, ''),
			COALESCE(p.genre, ''),
			COALESCE(p.url, ''),
			COALESCE(p.size_kb, 0),
			COALESCE(p.file_count, 0),
			p.posted_at,
			rpm.confidence,
			rpm.chosen,
			COALESCE(p.payload_json, '{}'::jsonb)
		FROM release_predb_matches rpm
		JOIN predb_entries p ON p.id = rpm.predb_entry_id
		WHERE rpm.release_id = $1
		ORDER BY rpm.chosen DESC, rpm.confidence DESC, p.title`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list predb matches for %s: %w", releaseID, err)
	}
	defer rows.Close()

	out := []IndexerPredbMatchSummary{}
	for rows.Next() {
		var item IndexerPredbMatchSummary
		var postedAt sql.NullTime
		if err := rows.Scan(
			&item.EntryID,
			&item.Title,
			&item.Category,
			&item.Source,
			&item.Team,
			&item.Genre,
			&item.URL,
			&item.SizeKB,
			&item.FileCount,
			&postedAt,
			&item.Confidence,
			&item.Chosen,
			&item.Payload,
		); err != nil {
			return nil, fmt.Errorf("scan predb match summary: %w", err)
		}
		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate predb matches for %s: %w", releaseID, err)
	}
	return out, nil
}

func (s *Store) listIndexerInspectionSummaries(ctx context.Context, whereClause string, args ...any) ([]IndexerInspectionSummary, error) {
	query := `
		SELECT
			stage_name,
			binary_id,
			COALESCE(release_id, ''),
			status,
			error_text,
			materialized_bytes,
			tool_provenance_json,
			summary_json,
			started_at,
			finished_at,
			updated_at
		FROM binary_inspections`
	if strings.TrimSpace(whereClause) != "" {
		query += ` WHERE ` + whereClause
	}
	query += ` ORDER BY updated_at DESC, stage_name, binary_id`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list inspection summaries: %w", err)
	}
	defer rows.Close()

	out := make([]IndexerInspectionSummary, 0, 16)
	for rows.Next() {
		var (
			item        IndexerInspectionSummary
			startedAt   sql.NullTime
			finishedAt  sql.NullTime
			toolJSON    []byte
			summaryJSON []byte
		)
		if err := rows.Scan(
			&item.StageName,
			&item.BinaryID,
			&item.ReleaseID,
			&item.Status,
			&item.ErrorText,
			&item.MaterializedBytes,
			&toolJSON,
			&summaryJSON,
			&startedAt,
			&finishedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan inspection summary: %w", err)
		}
		item.ToolProvenance = cloneRawJSON(toolJSON)
		item.Summary = cloneRawJSON(summaryJSON)
		if startedAt.Valid {
			t := startedAt.Time.UTC()
			item.StartedAt = &t
		}
		if finishedAt.Valid {
			t := finishedAt.Time.UTC()
			item.FinishedAt = &t
		}
		item.UpdatedAt = item.UpdatedAt.UTC()
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inspection summaries: %w", err)
	}

	return out, nil
}

func (s *Store) listIndexerInspectionArtifacts(ctx context.Context, binaryID int64) ([]IndexerBinaryInspectionArtifactSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			stage_name,
			artifact_role,
			artifact_name,
			artifact_path,
			bytes_total,
			mime_type,
			signature,
			source_kind,
			metadata_json
		FROM binary_inspection_artifacts
		WHERE binary_id = $1
		ORDER BY updated_at DESC, stage_name, artifact_role, artifact_name`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list inspection artifacts for binary %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]IndexerBinaryInspectionArtifactSummary, 0, 8)
	for rows.Next() {
		var item IndexerBinaryInspectionArtifactSummary
		var metadataJSON []byte
		if err := rows.Scan(
			&item.StageName,
			&item.ArtifactRole,
			&item.ArtifactName,
			&item.ArtifactPath,
			&item.BytesTotal,
			&item.MIMEType,
			&item.Signature,
			&item.SourceKind,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan inspection artifact summary: %w", err)
		}
		item.Metadata = cloneRawJSON(metadataJSON)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inspection artifacts for binary %d: %w", binaryID, err)
	}

	return out, nil
}

func (s *Store) listIndexerArchiveEntries(ctx context.Context, binaryID int64) ([]IndexerArchiveEntrySummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			entry_name,
			is_dir,
			uncompressed_bytes,
			compressed_bytes,
			encrypted,
			comment_text,
			media_type,
			signature,
			metadata_json
		FROM binary_archive_entries
		WHERE binary_id = $1
		ORDER BY entry_name`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list archive entries for binary %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]IndexerArchiveEntrySummary, 0, 16)
	for rows.Next() {
		var item IndexerArchiveEntrySummary
		var metadataJSON []byte
		if err := rows.Scan(
			&item.EntryName,
			&item.IsDir,
			&item.UncompressedBytes,
			&item.CompressedBytes,
			&item.Encrypted,
			&item.Comment,
			&item.MediaType,
			&item.Signature,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan archive entry summary: %w", err)
		}
		item.Metadata = cloneRawJSON(metadataJSON)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate archive entries for binary %d: %w", binaryID, err)
	}

	return out, nil
}

func (s *Store) listIndexerMediaStreams(ctx context.Context, binaryID int64) ([]IndexerMediaStreamSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			stream_index,
			stream_type,
			codec_name,
			codec_long_name,
			profile,
			width,
			height,
			channels,
			language,
			duration_seconds,
			bit_rate,
			default_disposition,
			forced_disposition,
			metadata_json
		FROM binary_media_streams
		WHERE binary_id = $1
		ORDER BY stream_index, stream_type`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list media streams for binary %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]IndexerMediaStreamSummary, 0, 8)
	for rows.Next() {
		var item IndexerMediaStreamSummary
		var metadataJSON []byte
		if err := rows.Scan(
			&item.StreamIndex,
			&item.StreamType,
			&item.CodecName,
			&item.CodecLongName,
			&item.Profile,
			&item.Width,
			&item.Height,
			&item.Channels,
			&item.Language,
			&item.DurationSeconds,
			&item.BitRate,
			&item.DefaultDisposition,
			&item.ForcedDisposition,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan media stream summary: %w", err)
		}
		item.Metadata = cloneRawJSON(metadataJSON)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media streams for binary %d: %w", binaryID, err)
	}

	return out, nil
}

func (s *Store) listIndexerTextEvidence(ctx context.Context, binaryID int64) ([]IndexerTextEvidenceSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			stage_name,
			evidence_kind,
			text_value,
			tokens_json,
			metadata_json
		FROM binary_text_evidence
		WHERE binary_id = $1
		ORDER BY updated_at DESC, stage_name, evidence_kind`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list text evidence for binary %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]IndexerTextEvidenceSummary, 0, 8)
	for rows.Next() {
		var item IndexerTextEvidenceSummary
		var tokensJSON []byte
		var metadataJSON []byte
		if err := rows.Scan(
			&item.StageName,
			&item.EvidenceKind,
			&item.TextValue,
			&tokensJSON,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan text evidence summary: %w", err)
		}
		item.Tokens = decodeJSONStringSlice(tokensJSON)
		item.Metadata = cloneRawJSON(metadataJSON)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate text evidence for binary %d: %w", binaryID, err)
	}

	return out, nil
}

func (s *Store) listIndexerPAR2Sets(ctx context.Context, binaryID int64) ([]IndexerPAR2SetSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			set_name,
			base_name,
			is_volume,
			volume_number,
			recovery_blocks,
			signature_ok,
			metadata_json
		FROM binary_par2_sets
		WHERE binary_id = $1
		ORDER BY set_name`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list par2 sets for binary %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]IndexerPAR2SetSummary, 0, 4)
	for rows.Next() {
		var item IndexerPAR2SetSummary
		var metadataJSON []byte
		if err := rows.Scan(
			&item.SetName,
			&item.BaseName,
			&item.IsVolume,
			&item.VolumeNumber,
			&item.RecoveryBlocks,
			&item.SignatureOK,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan par2 set summary: %w", err)
		}
		item.Metadata = cloneRawJSON(metadataJSON)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate par2 sets for binary %d: %w", binaryID, err)
	}

	return out, nil
}

func (s *Store) listIndexerBinaryParts(ctx context.Context, binaryID int64) ([]IndexerBinaryPartSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			bp.article_header_id,
			COALESCE(ah.provider_id, 0),
			COALESCE(ah.newsgroup_id, 0),
			COALESCE(ng.group_name, ''),
			COALESCE(ah.article_number, 0),
			bp.message_id,
			COALESCE(p.subject, ''),
			COALESCE(p.poster, ''),
			ah.date_utc,
			bp.part_number,
			bp.total_parts,
			bp.segment_bytes,
			bp.file_name,
			COALESCE(ah.bytes, 0),
			COALESCE(ah.lines, 0),
			COALESCE(p.yenc_part_number, 0),
			COALESCE(p.yenc_total_parts, 0),
			COALESCE(p.yenc_file_size, 0),
			COALESCE(wi.status, ''),
			wi.ready_at,
			COALESCE(wi.admission_reason, ''),
			COALESCE(brc.recovered_kind, ''),
			COALESCE(brc.recovered_source, ''),
			COALESCE(brc.recovered_file_name, '')
		FROM binary_parts bp
		LEFT JOIN article_headers ah
		  ON ah.source_posted_at = bp.source_posted_at
		 AND ah.id = bp.article_header_id
		LEFT JOIN newsgroups ng ON ng.id = ah.newsgroup_id
		LEFT JOIN article_header_ingest_payloads p
		  ON p.source_posted_at = bp.source_posted_at
		 AND p.article_header_id = bp.article_header_id
		LEFT JOIN yenc_recovery_work_items wi
		  ON wi.source_posted_at = bp.source_posted_at
		 AND wi.article_header_id = bp.article_header_id
		LEFT JOIN binary_recovery_current brc ON brc.binary_id = bp.binary_id
		WHERE bp.binary_id = $1
		ORDER BY bp.part_number, bp.id`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list binary parts for binary %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]IndexerBinaryPartSummary, 0, 128)
	for rows.Next() {
		var item IndexerBinaryPartSummary
		var dateUTC, readyAt sql.NullTime
		if err := rows.Scan(
			&item.ArticleHeaderID,
			&item.ProviderID,
			&item.NewsgroupID,
			&item.GroupName,
			&item.ArticleNumber,
			&item.MessageID,
			&item.Subject,
			&item.Poster,
			&dateUTC,
			&item.PartNumber,
			&item.TotalParts,
			&item.SegmentBytes,
			&item.FileName,
			&item.ArticleBytes,
			&item.ArticleLines,
			&item.YEncPartNumber,
			&item.YEncTotalParts,
			&item.YEncFileSize,
			&item.YEncRecoveryStatus,
			&readyAt,
			&item.YEncRecoveryError,
			&item.RecoveredKind,
			&item.RecoveredSource,
			&item.RecoveredFileName,
		); err != nil {
			return nil, fmt.Errorf("scan binary part summary: %w", err)
		}
		if dateUTC.Valid {
			t := dateUTC.Time.UTC()
			item.DateUTC = &t
		}
		if readyAt.Valid {
			t := readyAt.Time.UTC()
			item.YEncRecoveryReadyAt = &t
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate binary parts for binary %d: %w", binaryID, err)
	}

	return out, nil
}

func (s *Store) ListBinaryInspectionCandidates(ctx context.Context, stageName string, limit int) ([]BinaryInspectionCandidate, error) {
	return s.listBinaryInspectionCandidates(ctx, s.db, stageName, limit, BinaryInspectionCandidateOptions{})
}

func (s *Store) ListBinaryInspectionCandidatesWithOptions(ctx context.Context, stageName string, limit int, opts BinaryInspectionCandidateOptions) ([]BinaryInspectionCandidate, error) {
	return s.listBinaryInspectionCandidates(ctx, s.db, stageName, limit, opts)
}

type binaryInspectionQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

const binaryInspectionCandidateStateCTE = `
binary_state AS (
	SELECT
		bc.binary_id AS id,
		bc.provider_id,
		bc.newsgroup_id,
		bc.poster_id,
		bc.binary_key,
		bic.release_family_key,
		bic.base_stem,
		bic.release_key,
		bic.release_name,
		bic.binary_name,
		bic.file_name,
		bic.file_index,
		bic.expected_file_count,
		bic.expected_archive_file_count,
		bic.is_auxiliary,
		bic.is_main_payload,
		bic.match_confidence,
		bic.match_status,
		bos.posted_at,
		bos.total_bytes,
		bos.total_parts,
		bos.observed_parts,
		bos.first_article_number,
		bos.last_article_number,
		COALESCE(brc.recovered_kind, '') AS recovered_kind,
		COALESCE(brc.recovered_extension, '') AS recovered_extension,
		COALESCE(brc.recovered_source, '') AS recovered_source,
		COALESCE(brc.recovered_confidence, 0) AS recovered_confidence,
		COALESCE(brc.recovered_file_name, '') AS recovered_file_name,
		GREATEST(
			bc.updated_at,
			bic.updated_at,
			bos.updated_at,
			COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')
		) AS updated_at
	FROM binary_core bc
	JOIN binary_identity_current bic ON bic.binary_id = bc.binary_id
	JOIN binary_observation_stats bos ON bos.binary_id = bc.binary_id
	LEFT JOIN binary_recovery_current brc ON brc.binary_id = bc.binary_id
)`

const binaryInspectionPAR2CandidateStateCTE = `
candidate_source AS (
	SELECT DISTINCT ON (rf.binary_id)
		rf.binary_id,
		rf.release_id
	FROM release_files rf
	JOIN binary_identity_current bic ON bic.binary_id = rf.binary_id
	LEFT JOIN binary_recovery_current brc ON brc.binary_id = rf.binary_id
	WHERE rf.binary_id > 0
	  AND (
		rf.is_pars = TRUE OR
		LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.par2' OR
		COALESCE(brc.recovered_kind, '') = 'par2' OR
		COALESCE(brc.recovered_extension, '') = '.par2'
	  )
	ORDER BY rf.binary_id, rf.updated_at DESC, rf.release_id
),
binary_state AS (
	SELECT
		bc.binary_id AS id,
		cs.release_id,
		bc.provider_id,
		bc.newsgroup_id,
		bc.poster_id,
		bc.binary_key,
		bic.release_family_key,
		bic.base_stem,
		bic.release_key,
		bic.release_name,
		bic.binary_name,
		bic.file_name,
		bic.file_index,
		bic.expected_file_count,
		bic.expected_archive_file_count,
		bic.is_auxiliary,
		bic.is_main_payload,
		bic.match_confidence,
		bic.match_status,
		bos.posted_at,
		bos.total_bytes,
		bos.total_parts,
		bos.observed_parts,
		bos.first_article_number,
		bos.last_article_number,
		COALESCE(brc.recovered_kind, '') AS recovered_kind,
		COALESCE(brc.recovered_extension, '') AS recovered_extension,
		COALESCE(brc.recovered_source, '') AS recovered_source,
		COALESCE(brc.recovered_confidence, 0) AS recovered_confidence,
		COALESCE(brc.recovered_file_name, '') AS recovered_file_name,
		GREATEST(
			bc.updated_at,
			bic.updated_at,
			bos.updated_at,
			COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')
		) AS updated_at
	FROM candidate_source cs
	JOIN binary_core bc ON bc.binary_id = cs.binary_id
	JOIN binary_identity_current bic ON bic.binary_id = cs.binary_id
	JOIN binary_observation_stats bos ON bos.binary_id = cs.binary_id
	LEFT JOIN binary_recovery_current brc ON brc.binary_id = cs.binary_id
)`

func (s *Store) listBinaryInspectionCandidates(ctx context.Context, q binaryInspectionQueryer, stageName string, limit int, opts BinaryInspectionCandidateOptions) ([]BinaryInspectionCandidate, error) {
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return nil, fmt.Errorf("stage name is required")
	}
	if limit <= 0 {
		limit = 100
	}
	if isQueuedInspectionStage(stageName) {
		candidates, err := s.listInspectionReadyQueueCandidates(ctx, q, stageName, limit)
		if err != nil {
			return nil, err
		}
		if len(candidates) > 0 {
			return candidates, nil
		}
		if db, ok := q.(*sql.DB); ok && db == s.db {
			refreshLimit := limit * 10
			if refreshLimit < 1000 {
				refreshLimit = 1000
			}
			if _, err := s.RefreshInspectionReadyQueue(ctx, stageName, refreshLimit); err != nil {
				return nil, err
			}
			return s.listInspectionReadyQueueCandidates(ctx, q, stageName, limit)
		}
		return candidates, nil
	}
	return s.listBinaryInspectionCandidatesRaw(ctx, q, stageName, limit, opts)
}

func (s *Store) listBinaryInspectionCandidatesRaw(ctx context.Context, q binaryInspectionQueryer, stageName string, limit int, opts BinaryInspectionCandidateOptions) ([]BinaryInspectionCandidate, error) {
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return nil, fmt.Errorf("stage name is required")
	}
	if limit <= 0 {
		limit = 100
	}

	filter, err := inspectCandidateFilter(stageName, opts.RequireExpectedFileCount)
	if err != nil {
		return nil, err
	}

	errorRerunPredicate := `
			COALESCE(bi.summary_json->>'probe_error', '') <> '' OR
			COALESCE(bi.summary_json->>'ffprobe_error', '') <> '' OR
			COALESCE(bi.summary_json->>'extract_error', '') <> '' OR
			COALESCE(bi.summary_json->>'archive_extract_error', '') <> ''`
	if stageName == "inspect_archive" {
		errorRerunPredicate += `
			OR COALESCE(bi.summary_json->>'probe_error_detail', '') ILIKE '%has no articles%'
			OR (
				COALESCE(bi.summary_json->>'probe_strategy', '') = 'metadata_only' AND
				CASE
					WHEN jsonb_typeof(bi.summary_json->'archive_entries') = 'array' THEN jsonb_array_length(bi.summary_json->'archive_entries')
					ELSE 0
				END = 0 AND (
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.7z' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.001$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.zip' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.001$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.part0*1\.rar$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.r00$' OR
					(
						LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.rar' AND
						LOWER(COALESCE(rf.file_name, b.file_name, '')) !~ '\.part\d+\.rar$' AND
						LOWER(COALESCE(rf.file_name, b.file_name, '')) !~ '\.r\d{2,3}$'
					)
				)
			)`
	}
	filteredPredicate := `
		  AND NOT EXISTS (
			SELECT 1
			FROM binary_inspections cfi
			WHERE cfi.stage_name = 'inspect_discovery'
			  AND cfi.binary_id = b.id
			  AND cfi.status = 'completed'
			  AND COALESCE(cfi.summary_json->>'content_filtered', '') = 'true'
		  )`
	rerunPredicate := `
			bi.id IS NULL OR
			bi.status = 'failed' OR
			(
				bi.status = 'running' AND
				(
					bi.inspection_claimed_until IS NULL OR
					bi.inspection_claimed_until < NOW()
				)
			) OR
			b.updated_at > bi.updated_at OR
			` + errorRerunPredicate
	if stageName == "inspect_media" {
		rerunPredicate += `
			OR (
				abi.updated_at IS NOT NULL AND (
					bi.id IS NULL OR
					abi.updated_at > bi.updated_at
				)
			)`
	}

	if stageName == "inspect_par2" {
		query := `
			WITH ` + binaryInspectionPAR2CandidateStateCTE + `,
			candidate_rows AS (
				SELECT
					b.id,
					b.release_id,
					b.provider_id,
					b.release_family_key,
					COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), b.release_name) AS display_file_name,
					COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '') AS raw_file_name,
					b.binary_name,
					b.release_name,
					COALESCE(p.poster_name, '') AS poster,
					b.posted_at,
					b.total_bytes,
					b.total_parts,
					b.observed_parts,
					b.match_confidence,
					b.updated_at AS source_updated_at,
					COALESCE(bi.status, '') AS current_status,
					bi.updated_at AS current_updated_at,
					COALESCE(bi.summary_json, '{}'::jsonb) AS current_summary_json,
					EXISTS (
						SELECT 1
						FROM release_files rf
						WHERE rf.binary_id = b.id
					) AS release_linked,
					CASE
						WHEN LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '')) ~ '\.vol[0-9]+(?:\+| )[0-9]+\.par2$'
						THEN regexp_replace(
							LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '')),
							'\.vol[0-9]+(?:\+| )[0-9]+\.par2$',
							'.par2'
						)
						ELSE LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), ''))
					END AS par2_set_name,
					CASE
						WHEN LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '')) ~ '\.vol[0-9]+(?:\+| )[0-9]+\.par2$'
						THEN 1
						ELSE 0
					END AS volume_rank,
					CASE
						WHEN LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '')) ~ '\.vol([0-9]+)(?:\+| )[0-9]+\.par2$'
						THEN COALESCE(NULLIF(substring(
							LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), ''))
							FROM '\.vol([0-9]+)(?:\+| )[0-9]+\.par2$'
						), ''), '0')::integer
						ELSE 0
					END AS volume_number,
					EXISTS (
						SELECT 1
						FROM binary_par2_targets bpt
						WHERE bpt.binary_id = b.id
					) AS has_targets,
					(
						COALESCE(bi.status, '') = 'completed' AND
						CASE
							WHEN COALESCE(bi.summary_json->>'target_count', '') ~ '^[0-9]+$'
							THEN (bi.summary_json->>'target_count')::integer = 0
							ELSE FALSE
						END
					) AS completed_zero_targets,
					(
						` + rerunPredicate + `
					) AS needs_rerun
				FROM binary_state b
				LEFT JOIN posters p ON p.id = b.poster_id
				LEFT JOIN binary_inspections bi
					ON bi.stage_name = $1
					AND bi.binary_id = b.id
				WHERE (
					LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '')) LIKE '%.par2' OR
					COALESCE(b.recovered_kind, '') = 'par2' OR
					COALESCE(b.recovered_extension, '') = '.par2'
				)
				  AND b.observed_parts > 0
				  AND (
					bi.inspection_claimed_until IS NULL OR
					bi.inspection_claimed_until < NOW()
				  )
			),
			set_state AS (
				SELECT
					par2_set_name,
					BOOL_OR(volume_rank = 0) AS has_manifest,
					BOOL_OR(has_targets) AS has_any_targets,
					BOOL_OR(CASE WHEN volume_rank = 0 THEN needs_rerun ELSE FALSE END) AS manifest_needs_rerun,
					BOOL_OR(
						current_status = 'completed' AND
						(
							COALESCE(current_summary_json->>'probe_skip_reason', '') = 'article_not_found' OR
							(
								COALESCE(current_summary_json->>'probe_skip_reason', '') = 'prefix_sample_failed' AND
								COALESCE(current_summary_json->>'probe_error_detail', '') ILIKE '%article not found (430)%'
							)
						)
					) AS has_completed_missing_article_probe,
					BOOL_OR(
						current_status = 'completed' AND
						CASE
							WHEN COALESCE(current_summary_json->>'target_count', '') ~ '^[0-9]+$'
							THEN (current_summary_json->>'target_count')::integer = 0
							ELSE FALSE
						END
					) AS has_completed_zero_targets
				FROM candidate_rows
				GROUP BY par2_set_name
			),
			eligible_rows AS (
				SELECT
					cr.*
				FROM candidate_rows cr
				JOIN set_state ss ON ss.par2_set_name = cr.par2_set_name
				WHERE cr.release_linked
				  AND NOT (cr.volume_rank = 1 AND ss.has_completed_zero_targets)
				  AND (
					cr.needs_rerun OR
					(
						NOT ss.has_any_targets AND
						NOT ss.has_completed_missing_article_probe AND
						NOT ss.has_completed_zero_targets
					)
				)
				  AND (
					NOT ss.has_manifest OR
					cr.volume_rank = 0 OR
					(
						NOT ss.manifest_needs_rerun AND
						NOT ss.has_any_targets
					)
				  )
			)
			SELECT
				$1,
				id,
				release_id,
				provider_id,
				'' AS title,
				'' AS source_title,
				'' AS deobfuscated_title,
				release_family_key AS group_name,
				display_file_name AS file_name,
				binary_name,
				release_name,
				poster,
				posted_at,
				total_bytes,
				total_parts,
				match_confidence,
				source_updated_at,
				current_status,
				current_updated_at,
				current_summary_json,
				'{}'::jsonb AS archive_summary_json
			FROM (
				SELECT DISTINCT ON (par2_set_name)
					*
				FROM eligible_rows
				ORDER BY par2_set_name, volume_rank, volume_number, source_updated_at DESC, id DESC
			) chosen
			ORDER BY release_linked DESC, (observed_parts >= total_parts) DESC, source_updated_at DESC, id DESC
			LIMIT $2`
		return scanBinaryInspectionCandidates(ctx, q, query, stageName, limit)
	}

	if stageName == "inspect_discovery" {
		query := `
			SELECT
				$1 AS stage_name,
				bic.binary_id AS binary_id,
				'' AS release_id,
				bc.provider_id,
				'' AS title,
				'' AS source_title,
				'' AS deobfuscated_title,
				COALESCE(NULLIF(bic.release_family_key, ''), NULLIF(bic.base_stem, ''), '') AS group_name,
				COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '') AS file_name,
				bic.binary_name,
				bic.release_name,
				COALESCE(p.poster_name, '') AS poster,
				bos.posted_at,
				bos.total_bytes,
				bos.total_parts,
				bic.match_confidence,
				GREATEST(
					bc.updated_at,
					bic.updated_at,
					bos.updated_at,
					COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')
				) AS source_updated_at,
				COALESCE(bi.status, '') AS current_status,
				bi.updated_at AS current_updated_at,
				COALESCE(bi.summary_json, '{}'::jsonb) AS current_summary_json,
				'{}'::jsonb AS archive_summary_json
			FROM binary_identity_current bic
			JOIN binary_core bc ON bc.binary_id = bic.binary_id
			JOIN binary_observation_stats bos ON bos.binary_id = bic.binary_id
			LEFT JOIN binary_recovery_current brc ON brc.binary_id = bic.binary_id
			LEFT JOIN posters p ON p.id = bc.poster_id
			LEFT JOIN binary_inspections bi
				ON bi.stage_name = $1
				AND bi.binary_id = bic.binary_id
			WHERE COALESCE(brc.recovered_extension, '') = ''
			  AND (bic.is_main_payload = TRUE OR bic.is_auxiliary = FALSE)
			  AND (
				LOWER(COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.bin' OR
				COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '') !~ '\.[A-Za-z0-9]{1,8}$'
			  )
			  AND (
				bi.id IS NULL OR
				bi.status = 'failed' OR
				(
					bi.status = 'running' AND
					(
						bi.inspection_claimed_until IS NULL OR
						bi.inspection_claimed_until < NOW()
					)
				) OR
				GREATEST(
					bc.updated_at,
					bic.updated_at,
					bos.updated_at,
					COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')
				) > bi.updated_at OR
				` + errorRerunPredicate + `
			  )
			  AND (
				bi.inspection_claimed_until IS NULL OR
				bi.inspection_claimed_until < NOW()
			  )
			ORDER BY bic.updated_at DESC, bic.binary_id DESC
			LIMIT $2`
		return scanBinaryInspectionCandidates(ctx, q, query, stageName, limit)
	}

	query := `
		WITH ` + binaryInspectionCandidateStateCTE + `
		SELECT
			$1,
			b.id,
			r.release_id,
			r.provider_id,
			r.title,
			r.source_title,
			r.deobfuscated_title,
			r.group_name,
			COALESCE(rf.file_name, b.file_name, ''),
			b.binary_name,
			b.release_name,
			COALESCE(r.poster, ''),
			b.posted_at,
			b.total_bytes,
			b.total_parts,
			b.match_confidence,
			b.updated_at,
			COALESCE(bi.status, ''),
			bi.updated_at,
			COALESCE(bi.summary_json, '{}'::jsonb),
			COALESCE(abi.summary_json, '{}'::jsonb)
		FROM binary_state b
		JOIN release_files rf ON rf.binary_id = b.id
		JOIN releases r ON r.release_id = rf.release_id
		LEFT JOIN binary_inspections bi
			ON bi.stage_name = $1
			AND bi.binary_id = b.id
		LEFT JOIN binary_inspections abi
			ON abi.stage_name = 'inspect_archive'
			AND abi.binary_id = b.id
		WHERE ` + filter + `
		  AND (
			` + rerunPredicate + `
		  )
		  AND (
			bi.inspection_claimed_until IS NULL OR
			bi.inspection_claimed_until < NOW()
		  )`
	query += filteredPredicate
	if stageName == "inspect_archive" {
		representativePredicate := `
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.001$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.001$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.part0*1\.rar$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.r00$' OR
					(
						LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.rar' AND
						LOWER(COALESCE(rf.file_name, b.file_name, '')) !~ '\.part\d+\.rar$'
					) OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.7z' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.zip'`
		query = `
			WITH ` + binaryInspectionCandidateStateCTE + `
			SELECT *
			FROM (
				SELECT DISTINCT ON (
					r.release_id,
					CASE
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.\d{3}$'
							THEN REGEXP_REPLACE(LOWER(COALESCE(rf.file_name, b.file_name, '')), '\.7z\.\d{3}$', '.7z')
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.\d{3}$'
							THEN REGEXP_REPLACE(LOWER(COALESCE(rf.file_name, b.file_name, '')), '\.zip\.\d{3}$', '.zip')
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.part\d+\.rar$'
							THEN REGEXP_REPLACE(LOWER(COALESCE(rf.file_name, b.file_name, '')), '\.part\d+\.rar$', '.rar')
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.r\d{2,3}$'
							THEN REGEXP_REPLACE(LOWER(COALESCE(rf.file_name, b.file_name, '')), '\.r\d{2,3}$', '.rar')
						ELSE LOWER(COALESCE(rf.file_name, b.file_name, ''))
					END
				)
					$1 AS stage_name,
					b.id AS binary_id,
					r.release_id,
					r.provider_id,
					r.title,
					r.source_title,
					r.deobfuscated_title,
					r.group_name,
					COALESCE(rf.file_name, b.file_name, '') AS file_name,
					b.binary_name,
					b.release_name,
					COALESCE(r.poster, '') AS poster,
					b.posted_at,
					b.total_bytes,
					b.total_parts,
					b.match_confidence,
					b.updated_at AS source_updated_at,
					COALESCE(bi.status, '') AS current_status,
					bi.updated_at AS current_updated_at,
					COALESCE(bi.summary_json, '{}'::jsonb) AS current_summary_json,
					COALESCE(abi.summary_json, '{}'::jsonb) AS archive_summary_json
				FROM binary_state b
				JOIN release_files rf ON rf.binary_id = b.id
				JOIN releases r ON r.release_id = rf.release_id
				LEFT JOIN binary_inspections bi
					ON bi.stage_name = $1
					AND bi.binary_id = b.id
				LEFT JOIN binary_inspections abi
					ON abi.stage_name = 'inspect_archive'
					AND abi.binary_id = b.id
				WHERE ` + filter + `
				  AND (` + representativePredicate + `)
				  AND (
					bi.id IS NULL OR
					bi.status = 'failed' OR
					(
						bi.status = 'running' AND
						(
							bi.inspection_claimed_until IS NULL OR
							bi.inspection_claimed_until < NOW()
						)
					) OR
					b.updated_at > bi.updated_at OR
					` + errorRerunPredicate + `
				  )
				  AND (
					bi.inspection_claimed_until IS NULL OR
					bi.inspection_claimed_until < NOW()
				  )
				  ` + filteredPredicate + `
				ORDER BY
					r.release_id,
					CASE
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.\d{3}$'
							THEN REGEXP_REPLACE(LOWER(COALESCE(rf.file_name, b.file_name, '')), '\.7z\.\d{3}$', '.7z')
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.\d{3}$'
							THEN REGEXP_REPLACE(LOWER(COALESCE(rf.file_name, b.file_name, '')), '\.zip\.\d{3}$', '.zip')
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.part\d+\.rar$'
							THEN REGEXP_REPLACE(LOWER(COALESCE(rf.file_name, b.file_name, '')), '\.part\d+\.rar$', '.rar')
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.r\d{2,3}$'
							THEN REGEXP_REPLACE(LOWER(COALESCE(rf.file_name, b.file_name, '')), '\.r\d{2,3}$', '.rar')
						ELSE LOWER(COALESCE(rf.file_name, b.file_name, ''))
					END,
					CASE
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.001$' THEN 0
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.001$' THEN 0
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.part0*1\.rar$' THEN 0
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.r00$' THEN 0
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.7z' THEN 1
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.zip' THEN 1
						WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.rar' THEN 1
						ELSE 2
					END,
					COALESCE(NULLIF(rf.file_index, 0), NULLIF(b.file_index, 0), 2147483647),
					b.updated_at DESC,
					b.id
			) candidates
			ORDER BY candidates.source_updated_at DESC, candidates.binary_id DESC
			LIMIT $2`
	} else {
		query += `
		ORDER BY r.updated_at DESC, b.updated_at DESC, b.id
		LIMIT $2`
	}

	rows, err := q.QueryContext(ctx, query, stageName, limit)
	if err != nil {
		return nil, fmt.Errorf("list binary inspection candidates %s: %w", stageName, err)
	}
	defer rows.Close()

	out := make([]BinaryInspectionCandidate, 0, limit)
	for rows.Next() {
		var item BinaryInspectionCandidate
		var postedAt sql.NullTime
		var sourceUpdatedAt time.Time
		var currentUpdatedAt sql.NullTime

		if err := rows.Scan(
			&item.StageName,
			&item.BinaryID,
			&item.ReleaseID,
			&item.ProviderID,
			&item.ReleaseTitle,
			&item.SourceTitle,
			&item.DeobfuscatedTitle,
			&item.GroupName,
			&item.FileName,
			&item.BinaryName,
			&item.ReleaseName,
			&item.Poster,
			&postedAt,
			&item.TotalBytes,
			&item.TotalParts,
			&item.MatchConfidence,
			&sourceUpdatedAt,
			&item.CurrentStatus,
			&currentUpdatedAt,
			&item.CurrentSummaryJSON,
			&item.ArchiveSummaryJSON,
		); err != nil {
			return nil, fmt.Errorf("scan binary inspection candidate: %w", err)
		}

		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}
		t := sourceUpdatedAt.UTC()
		item.SourceUpdatedAt = &t
		if currentUpdatedAt.Valid {
			ct := currentUpdatedAt.Time.UTC()
			item.CurrentUpdatedAt = &ct
		}

		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate binary inspection candidates %s: %w", stageName, err)
	}

	return out, nil
}

func scanBinaryInspectionCandidates(ctx context.Context, q binaryInspectionQueryer, query, stageName string, limit int) ([]BinaryInspectionCandidate, error) {
	rows, err := q.QueryContext(ctx, query, stageName, limit)
	if err != nil {
		return nil, fmt.Errorf("list binary inspection candidates %s: %w", stageName, err)
	}
	defer rows.Close()

	out := make([]BinaryInspectionCandidate, 0, limit)
	for rows.Next() {
		var item BinaryInspectionCandidate
		var postedAt sql.NullTime
		var sourceUpdatedAt time.Time
		var currentUpdatedAt sql.NullTime

		if err := rows.Scan(
			&item.StageName,
			&item.BinaryID,
			&item.ReleaseID,
			&item.ProviderID,
			&item.ReleaseTitle,
			&item.SourceTitle,
			&item.DeobfuscatedTitle,
			&item.GroupName,
			&item.FileName,
			&item.BinaryName,
			&item.ReleaseName,
			&item.Poster,
			&postedAt,
			&item.TotalBytes,
			&item.TotalParts,
			&item.MatchConfidence,
			&sourceUpdatedAt,
			&item.CurrentStatus,
			&currentUpdatedAt,
			&item.CurrentSummaryJSON,
			&item.ArchiveSummaryJSON,
		); err != nil {
			return nil, fmt.Errorf("scan binary inspection candidate: %w", err)
		}

		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}
		t := sourceUpdatedAt.UTC()
		item.SourceUpdatedAt = &t
		if currentUpdatedAt.Valid {
			ct := currentUpdatedAt.Time.UTC()
			item.CurrentUpdatedAt = &ct
		}

		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate binary inspection candidates %s: %w", stageName, err)
	}

	return out, nil
}

func (s *Store) ClaimBinaryInspectionCandidates(ctx context.Context, req BinaryInspectionClaimRequest) ([]BinaryInspectionCandidate, error) {
	req.StageName = strings.TrimSpace(req.StageName)
	if req.StageName == "" {
		return nil, fmt.Errorf("stage name is required")
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}
	req.Owner = strings.TrimSpace(req.Owner)
	if req.Owner == "" {
		req.Owner = "inspect"
	}
	if req.LeaseDuration <= 0 {
		req.LeaseDuration = 15 * time.Minute
	}
	if _, err := inspectCandidateFilter(req.StageName, req.Options.RequireExpectedFileCount); err != nil {
		return nil, err
	}
	if isQueuedInspectionStage(req.StageName) {
		refreshLimit := req.Limit * 10
		if refreshLimit < 1000 {
			refreshLimit = 1000
		}
		if _, err := s.RefreshInspectionReadyQueue(ctx, req.StageName, refreshLimit); err != nil {
			return nil, err
		}
	}

	var candidates []BinaryInspectionCandidate
	if err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin binary inspection claim tx: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "gonzb-inspect-claim-"+req.StageName); err != nil {
			return fmt.Errorf("lock binary inspection claim %s: %w", req.StageName, err)
		}

		attemptCandidates, err := s.listBinaryInspectionCandidates(ctx, tx, req.StageName, req.Limit, req.Options)
		if err != nil {
			return err
		}
		if len(attemptCandidates) == 0 {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit empty binary inspection claim tx: %w", err)
			}
			candidates = attemptCandidates
			return nil
		}
		sort.SliceStable(attemptCandidates, func(i, j int) bool {
			return attemptCandidates[i].BinaryID < attemptCandidates[j].BinaryID
		})

		args := make([]any, 0, len(attemptCandidates)*4+3)
		args = append(args, req.StageName, req.Owner, req.LeaseDuration.Seconds())
		values := make([]string, 0, len(attemptCandidates))
		for _, candidate := range attemptCandidates {
			base := len(args)
			var sourceUpdated any
			if candidate.SourceUpdatedAt != nil {
				sourceUpdated = candidate.SourceUpdatedAt.UTC()
			}
			args = append(args, candidate.BinaryID, strings.TrimSpace(candidate.ReleaseID), sourceUpdated, candidate.BinaryID)
			values = append(values, fmt.Sprintf("($%d::bigint,$%d::text,$%d::timestamptz,$%d::bigint)", base+1, base+2, base+3, base+4))
		}

		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		WITH requested(binary_id, requested_release_id, source_updated_at, lookup_binary_id) AS (
			VALUES %s
		),
		locked_binaries AS (
			SELECT
				req.binary_id,
				req.requested_release_id,
					req.source_updated_at,
					req.lookup_binary_id,
					bc.binary_id AS locked_binary_id,
					COALESCE(bc.source_posted_at, NOW()) AS source_posted_at
			FROM requested req
			JOIN binary_core bc
			  ON bc.binary_id = req.binary_id
			FOR KEY SHARE OF bc
		)
		INSERT INTO binary_inspections (
			stage_name,
			binary_id,
				release_id,
				source_posted_at,
				status,
			started_at,
			finished_at,
			error_text,
			materialized_bytes,
			tool_provenance_json,
			summary_json,
			source_updated_at,
			inspection_claimed_by,
			inspection_claimed_until,
			updated_at
		)
		SELECT
			$1,
				req.locked_binary_id,
			COALESCE(
				CASE
					WHEN req.requested_release_id <> '' AND EXISTS (
						SELECT 1
						FROM releases r
						WHERE r.release_id = req.requested_release_id
					) THEN req.requested_release_id
					ELSE NULL
				END,
				(
					SELECT rf.release_id
					FROM release_files rf
					WHERE rf.binary_id = req.lookup_binary_id
					ORDER BY rf.release_id
					LIMIT 1
				)
				),
				req.source_posted_at,
				'running',
			NOW(),
			NULL,
			'',
			0,
			'{}'::jsonb,
			'{}'::jsonb,
			req.source_updated_at,
			$2,
			NOW() + ($3::DOUBLE PRECISION * INTERVAL '1 second'),
			NOW()
			FROM locked_binaries req
			ON CONFLICT (source_posted_at, stage_name, binary_id) DO UPDATE
			SET release_id = COALESCE(EXCLUDED.release_id, binary_inspections.release_id),
		    status = 'running',
		    started_at = NOW(),
		    finished_at = NULL,
		    error_text = '',
		    materialized_bytes = 0,
		    tool_provenance_json = '{}'::jsonb,
		    summary_json = '{}'::jsonb,
		    source_updated_at = EXCLUDED.source_updated_at,
		    inspection_claimed_by = EXCLUDED.inspection_claimed_by,
		    inspection_claimed_until = EXCLUDED.inspection_claimed_until,
		    updated_at = NOW()`, strings.Join(values, ",")), args...); err != nil {
			return fmt.Errorf("claim %d binary inspections for %s: %w", len(attemptCandidates), req.StageName, err)
		}
		if isQueuedInspectionStage(req.StageName) {
			if err := markInspectReadyQueueRowsRunning(ctx, tx, req.StageName, attemptCandidates, req.Owner, req.LeaseDuration); err != nil {
				return err
			}
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit binary inspection claim tx: %w", err)
		}
		candidates = attemptCandidates
		return nil
	}); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (s *Store) StartBinaryInspection(ctx context.Context, stageName string, binaryID int64, releaseID string, sourceUpdatedAt *time.Time) error {
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return fmt.Errorf("stage name is required")
	}
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	var sourceUpdated any
	if sourceUpdatedAt != nil {
		sourceUpdated = sourceUpdatedAt.UTC()
	}
	releaseID = strings.TrimSpace(releaseID)
	const directInspectionClaimOwner = "inspection.start"
	const directInspectionClaimLeaseSeconds = 900

	_, err := s.db.ExecContext(ctx, `
			WITH locked_binary AS (
				SELECT bc.binary_id, COALESCE(bc.source_posted_at, NOW()) AS source_posted_at
				FROM binary_core bc
				WHERE bc.binary_id = $2
			FOR KEY SHARE
		)
		INSERT INTO binary_inspections (
			stage_name,
				binary_id,
				release_id,
				source_posted_at,
				status,
			started_at,
			finished_at,
			error_text,
			materialized_bytes,
			tool_provenance_json,
			summary_json,
			source_updated_at,
			inspection_claimed_by,
			inspection_claimed_until,
			updated_at
		)
			SELECT
				$1,
				lb.binary_id,
			COALESCE(
				CASE
					WHEN $3::TEXT <> '' AND EXISTS (SELECT 1 FROM releases r WHERE r.release_id = $3)
					THEN $3
					ELSE NULL
				END,
				(
					SELECT rf.release_id
					FROM release_files rf
					WHERE rf.binary_id = $2
					ORDER BY rf.release_id
					LIMIT 1
				)
				),
				lb.source_posted_at,
				'running',
			NOW(),
			NULL,
			'',
			0,
			'{}'::jsonb,
			'{}'::jsonb,
			$4,
			$5,
			NOW() + ($6::DOUBLE PRECISION * INTERVAL '1 second'),
			NOW()
			FROM locked_binary lb
			ON CONFLICT (source_posted_at, stage_name, binary_id) DO UPDATE
			SET release_id = COALESCE(EXCLUDED.release_id, binary_inspections.release_id),
		    status = 'running',
		    started_at = NOW(),
		    finished_at = NULL,
		    error_text = '',
		    materialized_bytes = 0,
		    tool_provenance_json = '{}'::jsonb,
		    summary_json = '{}'::jsonb,
		    source_updated_at = EXCLUDED.source_updated_at,
		    inspection_claimed_by = EXCLUDED.inspection_claimed_by,
		    inspection_claimed_until = EXCLUDED.inspection_claimed_until,
		    updated_at = NOW()`,
		stageName,
		binaryID,
		releaseID,
		sourceUpdated,
		directInspectionClaimOwner,
		directInspectionClaimLeaseSeconds,
	)
	if err != nil {
		return fmt.Errorf("start binary inspection %s/%d: %w", stageName, binaryID, err)
	}
	if err := s.markInspectReadyQueueRunning(ctx, stageName, binaryID, directInspectionClaimOwner, directInspectionClaimLeaseSeconds*time.Second, sourceUpdatedAt); err != nil {
		return err
	}

	return nil
}

func (s *Store) CompleteBinaryInspection(ctx context.Context, in BinaryInspectionRecord) error {
	return s.finishBinaryInspection(ctx, in, "completed")
}

func (s *Store) FailBinaryInspection(ctx context.Context, in BinaryInspectionRecord) error {
	return s.finishBinaryInspection(ctx, in, "failed")
}

func (s *Store) UpsertReleasePasswordCandidate(ctx context.Context, in ReleasePasswordCandidateRecord) (int64, error) {
	in.ReleaseID = strings.TrimSpace(in.ReleaseID)
	if in.ReleaseID == "" {
		return 0, fmt.Errorf("release id is required")
	}
	in.PasswordValue = strings.TrimSpace(in.PasswordValue)
	if in.PasswordValue == "" {
		return 0, fmt.Errorf("password value is required")
	}
	in.SourceKind = strings.TrimSpace(in.SourceKind)
	if in.SourceKind == "" {
		in.SourceKind = "inspect_hint"
	}
	in.SourceRef = strings.TrimSpace(in.SourceRef)
	in.VerificationStatus = strings.TrimSpace(in.VerificationStatus)
	if in.VerificationStatus == "" {
		in.VerificationStatus = "pending"
	}

	var binaryID any
	if in.BinaryID > 0 {
		binaryID = in.BinaryID
	}
	var artifactID any
	if in.ArtifactID > 0 {
		artifactID = in.ArtifactID
	}
	var verifiedAt any
	if in.LastVerifiedAt != nil {
		verifiedAt = in.LastVerifiedAt.UTC()
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO release_password_candidates (
			release_id,
			binary_id,
			artifact_id,
			password_value,
			source_kind,
			source_ref,
			confidence,
			verification_status,
			last_verified_at,
			last_error,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW())
		ON CONFLICT (release_id, password_value, source_kind, source_ref) DO UPDATE
		SET binary_id = COALESCE(EXCLUDED.binary_id, release_password_candidates.binary_id),
		    artifact_id = COALESCE(EXCLUDED.artifact_id, release_password_candidates.artifact_id),
		    confidence = GREATEST(release_password_candidates.confidence, EXCLUDED.confidence),
		    verification_status = CASE
		    	WHEN release_password_candidates.verification_status = 'verified' THEN release_password_candidates.verification_status
		    	ELSE EXCLUDED.verification_status
		    END,
		    last_verified_at = COALESCE(EXCLUDED.last_verified_at, release_password_candidates.last_verified_at),
		    last_error = EXCLUDED.last_error,
		    updated_at = NOW()
		RETURNING id`,
		in.ReleaseID,
		binaryID,
		artifactID,
		in.PasswordValue,
		in.SourceKind,
		in.SourceRef,
		in.Confidence,
		in.VerificationStatus,
		verifiedAt,
		strings.TrimSpace(in.LastError),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert release password candidate %s/%s: %w", in.ReleaseID, in.PasswordValue, err)
	}

	return id, nil
}

func (s *Store) ListPasswordVerificationCandidates(ctx context.Context, limit int) ([]PasswordVerificationCandidate, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			rpc.id,
			rpc.release_id,
			COALESCE(rpc.binary_id, 0),
			COALESCE(rpc.artifact_id, 0),
			r.title,
			r.source_title,
			r.deobfuscated_title,
			rpc.password_value,
			rpc.source_kind,
			rpc.source_ref,
			rpc.confidence,
			rpc.verification_status,
			rpc.last_error
		FROM release_password_candidates rpc
		JOIN releases r ON r.release_id = rpc.release_id
		WHERE r.encrypted = TRUE
		  AND rpc.verification_status <> 'verified'
		ORDER BY rpc.confidence DESC, rpc.updated_at DESC, rpc.id
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list password verification candidates: %w", err)
	}
	defer rows.Close()

	out := make([]PasswordVerificationCandidate, 0, limit)
	for rows.Next() {
		var item PasswordVerificationCandidate
		if err := rows.Scan(
			&item.ID,
			&item.ReleaseID,
			&item.BinaryID,
			&item.ArtifactID,
			&item.Title,
			&item.SourceTitle,
			&item.DeobfuscatedTitle,
			&item.PasswordValue,
			&item.SourceKind,
			&item.SourceRef,
			&item.Confidence,
			&item.VerificationStatus,
			&item.LastError,
		); err != nil {
			return nil, fmt.Errorf("scan password verification candidate: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate password verification candidates: %w", err)
	}

	return out, nil
}

func (s *Store) UpdateReleasePasswordCandidateStatus(ctx context.Context, candidateID int64, status string, verifiedAt *time.Time, lastError string) error {
	if candidateID <= 0 {
		return fmt.Errorf("candidate id is required")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return fmt.Errorf("status is required")
	}

	var verified any
	if verifiedAt != nil {
		verified = verifiedAt.UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE release_password_candidates
		SET verification_status = $2,
		    last_verified_at = COALESCE($3, last_verified_at),
		    last_error = $4,
		    updated_at = NOW()
		WHERE id = $1`,
		candidateID,
		status,
		verified,
		strings.TrimSpace(lastError),
	)
	if err != nil {
		return fmt.Errorf("update password candidate status %d: %w", candidateID, err)
	}

	return nil
}

func (s *Store) ApplyReleaseInspectionUpdate(ctx context.Context, in ReleaseInspectionUpdate) error {
	in.ReleaseID = strings.TrimSpace(in.ReleaseID)
	if in.ReleaseID == "" {
		return fmt.Errorf("release id is required")
	}

	adjustedAvailability, adjustedTier, err := s.deriveAdjustedAvailability(ctx, in)
	if err != nil {
		return err
	}

	subtitlesJSON, err := json.Marshal(sanitizeStringSlice(in.SubtitleLanguages))
	if err != nil {
		return fmt.Errorf("marshal subtitle languages for %s: %w", in.ReleaseID, err)
	}
	mediaTagsJSON, err := json.Marshal(sanitizeStringSlice(in.MediaTags))
	if err != nil {
		return fmt.Errorf("marshal media tags for %s: %w", in.ReleaseID, err)
	}

	var metadataUpdated any
	if in.MetadataUpdatedAt != nil {
		metadataUpdated = in.MetadataUpdatedAt.UTC()
	}
	var preferredPasswordID any
	if in.PreferredPasswordID != nil && *in.PreferredPasswordID > 0 {
		preferredPasswordID = *in.PreferredPasswordID
	}
	var adjustedAvailabilityValue any
	adjustedTierValue := ""
	if adjustedAvailability != nil {
		adjustedAvailabilityValue = *adjustedAvailability
		adjustedTierValue = adjustedTier
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE releases
		SET encrypted = COALESCE($2, encrypted),
		    has_par2 = COALESCE($3, has_par2),
		    has_nfo = COALESCE($4, has_nfo),
		    passworded = COALESCE($5, passworded),
		    passworded_known = COALESCE($6, passworded_known),
		    passworded_unknown = COALESCE($7, passworded_unknown),
		    password_state = CASE WHEN $8 <> '' THEN $8 ELSE password_state END,
		    preferred_password_id = COALESCE($9, preferred_password_id),
		    archive_count = GREATEST(archive_count, COALESCE($10, archive_count)),
		    video_count = GREATEST(video_count, COALESCE($11, video_count)),
		    audio_count = GREATEST(audio_count, COALESCE($12, audio_count)),
		    expected_file_count = GREATEST(expected_file_count, COALESCE($13, expected_file_count)),
		    expected_archive_file_count = GREATEST(expected_archive_file_count, COALESCE($14, expected_archive_file_count)),
		    runtime_seconds = COALESCE($15, runtime_seconds),
		    sample_present = COALESCE($16, sample_present),
		    primary_resolution = CASE WHEN $17 <> '' THEN $17 ELSE primary_resolution END,
		    primary_video_codec = CASE WHEN $18 <> '' THEN $18 ELSE primary_video_codec END,
		    primary_audio_codec = CASE WHEN $19 <> '' THEN $19 ELSE primary_audio_codec END,
		    subtitle_languages_json = CASE
		    	WHEN jsonb_array_length($20::jsonb) > 0 THEN $20::jsonb
		    	ELSE subtitle_languages_json
		    END,
		    media_tags_json = CASE
		    	WHEN jsonb_array_length($21::jsonb) > 0 THEN $21::jsonb
		    	ELSE media_tags_json
		    END,
		    availability_score = COALESCE($22, availability_score),
		    availability_tier = CASE WHEN $23 <> '' THEN $23 ELSE availability_tier END,
		    media_quality_score = GREATEST(media_quality_score, COALESCE($24, media_quality_score)),
		    media_quality_tier = CASE WHEN $25 <> '' THEN $25 ELSE media_quality_tier END,
		    metadata_updated_at = COALESCE($26, metadata_updated_at),
		    updated_at = NOW()
		WHERE release_id = $1`,
		in.ReleaseID,
		nullableBool(in.Encrypted),
		nullableBool(in.HasPAR2),
		nullableBool(in.HasNFO),
		nullableBool(in.Passworded),
		nullableBool(in.PasswordedKnown),
		nullableBool(in.PasswordedUnknown),
		strings.TrimSpace(in.PasswordState),
		preferredPasswordID,
		nullableInt(in.ArchiveCount),
		nullableInt(in.VideoCount),
		nullableInt(in.AudioCount),
		nullableInt(in.ExpectedFileCount),
		nullableInt(in.ExpectedArchiveFileCount),
		nullableInt(in.RuntimeSeconds),
		nullableBool(in.SamplePresent),
		strings.TrimSpace(in.PrimaryResolution),
		strings.TrimSpace(in.PrimaryVideoCodec),
		strings.TrimSpace(in.PrimaryAudioCodec),
		string(subtitlesJSON),
		string(mediaTagsJSON),
		adjustedAvailabilityValue,
		strings.TrimSpace(adjustedTierValue),
		nullableFloat64(in.MediaQualityScore),
		strings.TrimSpace(in.MediaQualityTier),
		metadataUpdated,
	)
	if err != nil {
		return fmt.Errorf("apply release inspection update %s: %w", in.ReleaseID, err)
	}
	if rows, rowsErr := res.RowsAffected(); rowsErr == nil && rows == 0 {
		return fmt.Errorf("%w: %s", ErrReleaseNotFound, in.ReleaseID)
	}

	if err := s.applyDerivedInspectionTitleUpdate(ctx, in.ReleaseID, in.MetadataUpdatedAt); err != nil {
		return err
	}
	if err := s.refreshReleaseCategory(ctx, in.ReleaseID); err != nil {
		return err
	}

	return nil
}

func (s *Store) applyDerivedInspectionTitleUpdate(ctx context.Context, releaseID string, metadataUpdatedAt *time.Time) error {
	sourceTitle, currentTitleSource, currentTitleConfidence, binaryIDs, err := s.loadReleaseTitleInputs(ctx, releaseID)
	if err != nil {
		return err
	}
	if len(binaryIDs) == 0 {
		return nil
	}

	candidates, err := s.ListReleaseTitleCandidates(ctx, binaryIDs)
	if err != nil {
		return err
	}
	inspectionInputs := make([]releasetitle.InspectionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		inspectionInputs = append(inspectionInputs, releasetitle.InspectionCandidate{
			Source:     candidate.Source,
			Value:      candidate.Value,
			Confidence: candidate.Confidence,
		})
	}
	best, ok := releasetitle.ChooseBestInspectionTitle(sourceTitle, inspectionInputs)
	if !ok {
		return nil
	}
	if !releasetitle.ShouldAdoptInspectionTitle(sourceTitle, best) {
		return nil
	}

	displaySource := releasetitle.DisplayTitleStyle(sourceTitle)
	if releasetitle.NormalizeSearchTitle(best.DisplayTitle) == releasetitle.NormalizeSearchTitle(displaySource) {
		return nil
	}
	if currentTitleSource != "" && currentTitleSource != "source" && currentTitleConfidence >= best.Confidence {
		return nil
	}

	var metadataUpdated any
	if metadataUpdatedAt != nil {
		metadataUpdated = metadataUpdatedAt.UTC()
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE releases
		SET title = $2,
		    deobfuscated_title = $3,
		    title_source = $4,
		    title_confidence = $5,
		    search_title = $6,
		    metadata_updated_at = COALESCE($7, metadata_updated_at),
		    updated_at = NOW()
		WHERE release_id = $1`,
		releaseID,
		strings.TrimSpace(best.DisplayTitle),
		strings.TrimSpace(best.ReleaseTitle),
		strings.TrimSpace(best.Source),
		best.Confidence,
		releasetitle.NormalizeSearchTitle(best.DisplayTitle),
		metadataUpdated,
	)
	if err != nil {
		return fmt.Errorf("apply derived inspection title update %s: %w", releaseID, err)
	}
	if err := s.refreshReleaseCategory(ctx, releaseID); err != nil {
		return err
	}
	return nil
}

func (s *Store) loadReleaseTitleInputs(ctx context.Context, releaseID string) (string, string, float64, []int64, error) {
	var (
		sourceTitle       string
		currentTitleFrom  string
		currentConfidence float64
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(source_title, ''), COALESCE(title_source, ''), COALESCE(title_confidence, 0)
		FROM releases
		WHERE release_id = $1`,
		releaseID,
	).Scan(&sourceTitle, &currentTitleFrom, &currentConfidence); err != nil {
		if err == sql.ErrNoRows {
			return "", "", 0, nil, fmt.Errorf("%w: %s", ErrReleaseNotFound, releaseID)
		}
		return "", "", 0, nil, fmt.Errorf("load release title inputs %s: %w", releaseID, err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT binary_id
		FROM release_files
		WHERE release_id = $1
		  AND binary_id IS NOT NULL
		ORDER BY binary_id`, releaseID)
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("load release file binary ids %s: %w", releaseID, err)
	}
	defer rows.Close()

	binaryIDs := make([]int64, 0, 16)
	for rows.Next() {
		var binaryID int64
		if err := rows.Scan(&binaryID); err != nil {
			return "", "", 0, nil, fmt.Errorf("scan release file binary id %s: %w", releaseID, err)
		}
		if binaryID > 0 {
			binaryIDs = append(binaryIDs, binaryID)
		}
	}
	if err := rows.Err(); err != nil {
		return "", "", 0, nil, fmt.Errorf("iterate release file binary ids %s: %w", releaseID, err)
	}

	return sourceTitle, currentTitleFrom, currentConfidence, binaryIDs, nil
}

func (s *Store) deriveAdjustedAvailability(ctx context.Context, in ReleaseInspectionUpdate) (*float64, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("pgindex store is not initialized")
	}

	var (
		completionPct     float64
		availabilityScore float64
		passwordedKnown   bool
		passwordedUnknown bool
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT completion_pct, availability_score, passworded_known, passworded_unknown
		FROM releases
		WHERE release_id = $1`,
		in.ReleaseID,
	).Scan(&completionPct, &availabilityScore, &passwordedKnown, &passwordedUnknown); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", fmt.Errorf("%w: %s", ErrReleaseNotFound, in.ReleaseID)
		}
		return nil, "", fmt.Errorf("load current availability for %s: %w", in.ReleaseID, err)
	}

	finalKnown := passwordedKnown
	if in.PasswordedKnown != nil {
		finalKnown = *in.PasswordedKnown
	}
	finalUnknown := passwordedUnknown
	if in.PasswordedUnknown != nil {
		finalUnknown = *in.PasswordedUnknown
	}
	if finalKnown {
		finalUnknown = false
	}

	adjusted, tier := releasepolicy.AdjustAvailabilityForInspection(releasepolicy.AvailabilityAdjustmentInput{
		CompletionPct:     completionPct,
		AvailabilityScore: availabilityScore,
		PasswordedKnown:   finalKnown,
		PasswordedUnknown: finalUnknown,
	})
	return adjusted, tier, nil
}

func (s *Store) ReplaceBinaryInspectionArtifacts(ctx context.Context, stageName string, binaryID int64, rows []BinaryInspectionArtifactRecord) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return fmt.Errorf("stage name is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin artifact replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if err := s.replaceBinaryInspectionArtifactsInTx(ctx, tx, stageName, binaryID, rows); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit artifact replace tx: %w", err)
	}
	return nil
}

func (s *Store) ReplaceBinaryArchiveEntries(ctx context.Context, binaryID int64, rows []BinaryArchiveEntryRecord) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin archive entry replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM binary_archive_entries WHERE binary_id = $1`, binaryID); err != nil {
		return fmt.Errorf("delete archive entries %d: %w", binaryID, err)
	}
	exists, err := binaryStillExistsInTx(ctx, tx, binaryID)
	if err != nil {
		return err
	}
	if !exists {
		return tx.Commit()
	}

	existingReleaseIDs, err := s.existingReleaseIDsForInspectionRows(ctx, tx, archiveEntryReleaseIDs(rows))
	if err != nil {
		return fmt.Errorf("load archive entry release ids %d: %w", binaryID, err)
	}

	insertRows := make([][]any, 0, len(rows))
	for _, row := range rows {
		metadataJSON, err := json.Marshal(sanitizeJSONMap(row.Metadata))
		if err != nil {
			return fmt.Errorf("marshal archive entry metadata %d: %w", binaryID, err)
		}
		var releaseID any
		if trimmedReleaseID := strings.TrimSpace(row.ReleaseID); existingReleaseIDs[trimmedReleaseID] {
			releaseID = trimmedReleaseID
		}
		insertRows = append(insertRows, []any{
			binaryID,
			releaseID,
			strings.TrimSpace(row.EntryName),
			row.IsDir,
			row.UncompressedBytes,
			row.CompressedBytes,
			row.Encrypted,
			strings.TrimSpace(row.Comment),
			strings.TrimSpace(row.MediaType),
			strings.TrimSpace(row.Signature),
			string(metadataJSON),
			time.Now().UTC(),
		})
	}

	if err := execInspectionReplaceBatch(ctx, tx, `
			INSERT INTO binary_archive_entries (
				binary_id,
				release_id,
				entry_name,
				is_dir,
				uncompressed_bytes,
				compressed_bytes,
				encrypted,
				comment_text,
				media_type,
				signature,
				metadata_json,
				updated_at
			)
			VALUES `, insertRows); err != nil {
		return fmt.Errorf("insert archive entries %d: %w", binaryID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit archive entry replace tx: %w", err)
	}
	return nil
}

func (s *Store) ReplaceBinaryMediaStreams(ctx context.Context, binaryID int64, rows []BinaryMediaStreamRecord) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin media stream replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM binary_media_streams WHERE binary_id = $1`, binaryID); err != nil {
		return fmt.Errorf("delete media streams %d: %w", binaryID, err)
	}
	exists, err := binaryStillExistsInTx(ctx, tx, binaryID)
	if err != nil {
		return err
	}
	if !exists {
		return tx.Commit()
	}

	existingReleaseIDs, err := s.existingReleaseIDsForInspectionRows(ctx, tx, mediaStreamReleaseIDs(rows))
	if err != nil {
		return fmt.Errorf("load media stream release ids %d: %w", binaryID, err)
	}

	insertRows := make([][]any, 0, len(rows))
	for _, row := range rows {
		metadataJSON, err := json.Marshal(sanitizeJSONMap(row.Metadata))
		if err != nil {
			return fmt.Errorf("marshal media stream metadata %d: %w", binaryID, err)
		}
		var releaseID any
		if trimmedReleaseID := strings.TrimSpace(row.ReleaseID); existingReleaseIDs[trimmedReleaseID] {
			releaseID = trimmedReleaseID
		}
		insertRows = append(insertRows, []any{
			binaryID,
			releaseID,
			row.StreamIndex,
			strings.TrimSpace(row.StreamType),
			strings.TrimSpace(row.CodecName),
			strings.TrimSpace(row.CodecLongName),
			strings.TrimSpace(row.Profile),
			row.Width,
			row.Height,
			row.Channels,
			strings.TrimSpace(row.Language),
			row.DurationSeconds,
			row.BitRate,
			row.DefaultDisposition,
			row.ForcedDisposition,
			string(metadataJSON),
			time.Now().UTC(),
		})
	}

	if err := execInspectionReplaceBatch(ctx, tx, `
			INSERT INTO binary_media_streams (
				binary_id,
				release_id,
				stream_index,
				stream_type,
				codec_name,
				codec_long_name,
				profile,
				width,
				height,
				channels,
				language,
				duration_seconds,
				bit_rate,
				default_disposition,
				forced_disposition,
				metadata_json,
				updated_at
			)
			VALUES `, insertRows); err != nil {
		return fmt.Errorf("insert media streams %d: %w", binaryID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit media stream replace tx: %w", err)
	}
	return nil
}

func (s *Store) ReplaceBinaryTextEvidence(ctx context.Context, stageName string, binaryID int64, rows []BinaryTextEvidenceRecord) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return fmt.Errorf("stage name is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin text evidence replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM binary_text_evidence
		WHERE binary_id = $1 AND stage_name = $2`,
		binaryID,
		stageName,
	); err != nil {
		return fmt.Errorf("delete text evidence %s/%d: %w", stageName, binaryID, err)
	}

	insertRows := make([][]any, 0, len(rows))
	for _, row := range rows {
		tokensJSON, err := json.Marshal(sanitizeStringSlice(row.Tokens))
		if err != nil {
			return fmt.Errorf("marshal text evidence tokens %s/%d: %w", stageName, binaryID, err)
		}
		metadataJSON, err := json.Marshal(sanitizeJSONMap(row.Metadata))
		if err != nil {
			return fmt.Errorf("marshal text evidence metadata %s/%d: %w", stageName, binaryID, err)
		}
		var releaseID any
		if strings.TrimSpace(row.ReleaseID) != "" {
			releaseID = strings.TrimSpace(sanitizeUTF8(row.ReleaseID))
		}
		insertRows = append(insertRows, []any{
			binaryID,
			releaseID,
			stageName,
			strings.TrimSpace(sanitizeUTF8(row.EvidenceKind)),
			sanitizeUTF8(row.TextValue),
			string(tokensJSON),
			string(metadataJSON),
			time.Now().UTC(),
		})
	}

	if err := execInspectionReplaceBatch(ctx, tx, `
			INSERT INTO binary_text_evidence (
				binary_id,
				release_id,
				stage_name,
				evidence_kind,
				text_value,
				tokens_json,
				metadata_json,
				updated_at
			)
			VALUES `, insertRows); err != nil {
		return fmt.Errorf("insert text evidence %s/%d: %w", stageName, binaryID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit text evidence replace tx: %w", err)
	}
	return nil
}

func (s *Store) ReplaceBinaryPAR2Sets(ctx context.Context, binaryID int64, rows []BinaryPAR2SetRecord) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin par2 set replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if err := s.replaceBinaryPAR2SetsInTx(ctx, tx, binaryID, rows); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit par2 set replace tx: %w", err)
	}
	return nil
}

func (s *Store) ReplaceBinaryPAR2Targets(ctx context.Context, binaryID int64, rows []BinaryPAR2TargetRecord) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin par2 target replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if err := s.replaceBinaryPAR2TargetsInTx(ctx, tx, binaryID, rows); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit par2 target replace tx: %w", err)
	}
	return nil
}

func (s *Store) ApplyBinaryPAR2TargetCoverage(ctx context.Context, binaryID int64, rows []BinaryPAR2TargetRecord) (*BinaryPAR2TargetCoverageResult, error) {
	result := &BinaryPAR2TargetCoverageResult{TargetCount: len(rows)}
	if binaryID <= 0 {
		return result, fmt.Errorf("binary id is required")
	}

	targets := normalizePAR2CoverageTargets(rows)
	result.MainTargetCount = len(targets)

	// PAR2 target rows are already persisted in binary_par2_targets. Do not fan
	// out inspection writes into binary identity/stat projections; assemble owns them.
	return result, nil
}

func (s *Store) ApplyPAR2InspectionBatch(ctx context.Context, rows []PAR2InspectionBatchRecord) (*PAR2InspectionBatchResult, error) {
	if len(rows) == 0 {
		return &PAR2InspectionBatchResult{}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin par2 inspection batch tx: %w", err)
	}
	defer rollbackTx(tx)

	out := &PAR2InspectionBatchResult{}
	batchSummaryKeys := make([]releaseFamilySummaryKey, 0, len(rows)*2)
	seenSummaryKeys := make(map[releaseFamilySummaryKey]struct{}, len(rows)*2)
	for _, row := range rows {
		if row.BinaryID <= 0 {
			return out, fmt.Errorf("par2 inspection batch binary id is required")
		}
		stageName := strings.TrimSpace(row.StageName)
		if stageName == "" {
			stageName = "inspect_par2"
		}
		if err := s.replaceBinaryInspectionArtifactsInTx(ctx, tx, stageName, row.BinaryID, row.ArtifactRows); err != nil {
			return out, err
		}
		out.RowsWritten += int64(len(row.ArtifactRows))
		if err := s.replaceBinaryPAR2SetsInTx(ctx, tx, row.BinaryID, row.PAR2SetRows); err != nil {
			return out, err
		}
		out.RowsWritten += int64(len(row.PAR2SetRows))
		if err := s.replaceBinaryPAR2TargetsInTx(ctx, tx, row.BinaryID, row.PAR2TargetRows); err != nil {
			return out, err
		}
		out.RowsWritten += int64(len(row.PAR2TargetRows))
		summary := sanitizeStringMap(row.Summary)
		if len(row.PAR2TargetRows) > 0 {
			coverage, err := s.applyBinaryPAR2TargetCoverageInTx(ctx, tx, row.BinaryID, row.PAR2TargetRows)
			if err != nil {
				return out, err
			}
			if coverage != nil {
				summary["main_target_count"] = coverage.MainTargetCount
				summary["target_coverage_updates"] = coverage.UpdatedBinaryCount
				out.RowsWritten += int64(coverage.UpdatedBinaryCount)
				for _, key := range coverage.SummaryKeys {
					batchSummaryKeys = appendReleaseFamilySummaryKey(batchSummaryKeys, seenSummaryKeys, key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey)
				}
			}
		}
		if err := s.finishBinaryInspectionWithDB(ctx, tx, BinaryInspectionRecord{
			StageName:         stageName,
			BinaryID:          row.BinaryID,
			ReleaseID:         row.ReleaseID,
			Status:            "completed",
			MaterializedBytes: row.MaterializedBytes,
			ToolProvenance:    row.ToolProvenance,
			Summary:           summary,
			SourceUpdatedAt:   row.SourceUpdatedAt,
		}, "completed"); err != nil {
			return out, err
		}
		out.RowsWritten++
		out.FlushedCandidates++
	}
	sortReleaseFamilySummaryKeys(batchSummaryKeys)
	if err := markReleaseFamiliesDirtyBatch(ctx, tx, batchSummaryKeys); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("commit par2 inspection batch tx: %w", err)
	}
	return out, nil
}

func (s *Store) replaceBinaryInspectionArtifactsInTx(ctx context.Context, tx *sql.Tx, stageName string, binaryID int64, rows []BinaryInspectionArtifactRecord) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM binary_inspection_artifacts
		WHERE binary_id = $1 AND stage_name = $2`,
		binaryID,
		stageName,
	); err != nil {
		return fmt.Errorf("delete inspection artifacts %s/%d: %w", stageName, binaryID, err)
	}
	exists, err := binaryStillExistsInTx(ctx, tx, binaryID)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	existingReleaseIDs, err := s.existingReleaseIDsForInspectionRows(ctx, tx, artifactReleaseIDs(rows))
	if err != nil {
		return fmt.Errorf("load artifact release ids %s/%d: %w", stageName, binaryID, err)
	}

	insertRows := make([][]any, 0, len(rows))
	for _, row := range rows {
		metadataJSON, err := json.Marshal(sanitizeJSONMap(row.Metadata))
		if err != nil {
			return fmt.Errorf("marshal inspection artifact metadata %s/%d: %w", stageName, binaryID, err)
		}
		var releaseID any
		if trimmedReleaseID := strings.TrimSpace(row.ReleaseID); existingReleaseIDs[trimmedReleaseID] {
			releaseID = trimmedReleaseID
		}
		insertRows = append(insertRows, []any{
			binaryID,
			releaseID,
			stageName,
			strings.TrimSpace(row.ArtifactRole),
			strings.TrimSpace(row.ArtifactName),
			strings.TrimSpace(row.ArtifactPath),
			row.BytesTotal,
			strings.TrimSpace(row.MIMEType),
			strings.TrimSpace(row.Signature),
			strings.TrimSpace(row.SourceKind),
			string(metadataJSON),
			time.Now().UTC(),
		})
	}

	if err := execInspectionReplaceBatch(ctx, tx, `
			INSERT INTO binary_inspection_artifacts (
				binary_id,
				release_id,
				stage_name,
				artifact_role,
				artifact_name,
				artifact_path,
				bytes_total,
				mime_type,
				signature,
				source_kind,
				metadata_json,
				updated_at
			)
			VALUES `, insertRows); err != nil {
		return fmt.Errorf("insert inspection artifacts %s/%d: %w", stageName, binaryID, err)
	}
	return nil
}

func (s *Store) replaceBinaryPAR2SetsInTx(ctx context.Context, tx *sql.Tx, binaryID int64, rows []BinaryPAR2SetRecord) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM binary_par2_sets WHERE binary_id = $1`, binaryID); err != nil {
		return fmt.Errorf("delete par2 sets %d: %w", binaryID, err)
	}

	existingReleaseIDs, err := s.existingReleaseIDsForInspectionRows(ctx, tx, par2SetReleaseIDs(rows))
	if err != nil {
		return fmt.Errorf("load par2 set release ids %d: %w", binaryID, err)
	}

	insertRows := make([][]any, 0, len(rows))
	for _, row := range rows {
		metadataJSON, err := json.Marshal(sanitizeJSONMap(row.Metadata))
		if err != nil {
			return fmt.Errorf("marshal par2 set metadata %d: %w", binaryID, err)
		}
		var releaseID any
		if trimmedReleaseID := strings.TrimSpace(row.ReleaseID); existingReleaseIDs[trimmedReleaseID] {
			releaseID = trimmedReleaseID
		}
		insertRows = append(insertRows, []any{
			binaryID,
			releaseID,
			strings.TrimSpace(row.SetName),
			strings.TrimSpace(row.BaseName),
			row.IsVolume,
			row.VolumeNumber,
			row.RecoveryBlocks,
			row.SignatureOK,
			string(metadataJSON),
			time.Now().UTC(),
		})
	}
	if err := execInspectionReplaceBatch(ctx, tx, `
			INSERT INTO binary_par2_sets (
				binary_id,
				release_id,
				set_name,
				base_name,
				is_volume,
				volume_number,
				recovery_blocks,
				signature_ok,
				metadata_json,
				updated_at
			)
			VALUES `, insertRows); err != nil {
		return fmt.Errorf("insert par2 sets %d: %w", binaryID, err)
	}
	return nil
}

func (s *Store) replaceBinaryPAR2TargetsInTx(ctx context.Context, tx *sql.Tx, binaryID int64, rows []BinaryPAR2TargetRecord) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM binary_par2_targets WHERE binary_id = $1`, binaryID); err != nil {
		return fmt.Errorf("delete par2 targets %d: %w", binaryID, err)
	}

	existingReleaseIDs, err := s.existingReleaseIDsForInspectionRows(ctx, tx, par2TargetReleaseIDs(rows))
	if err != nil {
		return fmt.Errorf("load par2 target release ids %d: %w", binaryID, err)
	}

	insertRows := make([][]any, 0, len(rows))
	for _, row := range rows {
		fileName := strings.TrimSpace(row.FileName)
		if fileName == "" {
			continue
		}
		metadataJSON, err := json.Marshal(sanitizeJSONMap(row.Metadata))
		if err != nil {
			return fmt.Errorf("marshal par2 target metadata %d: %w", binaryID, err)
		}
		releaseID := ""
		if trimmedReleaseID := strings.TrimSpace(row.ReleaseID); existingReleaseIDs[trimmedReleaseID] {
			releaseID = trimmedReleaseID
		}
		insertRows = append(insertRows, []any{
			binaryID,
			releaseID,
			fileName,
			row.FileSize,
			string(metadataJSON),
			time.Now().UTC(),
		})
	}
	if err := execInspectionReplaceBatch(ctx, tx, `
			INSERT INTO binary_par2_targets (
				binary_id,
				release_id,
				file_name,
				file_size,
				metadata_json,
				updated_at
			)
			VALUES `, insertRows); err != nil {
		return fmt.Errorf("insert par2 targets %d: %w", binaryID, err)
	}
	return nil
}

func (s *Store) applyBinaryPAR2TargetCoverageInTx(ctx context.Context, tx *sql.Tx, binaryID int64, rows []BinaryPAR2TargetRecord) (*BinaryPAR2TargetCoverageResult, error) {
	result := &BinaryPAR2TargetCoverageResult{TargetCount: len(rows)}
	if binaryID <= 0 {
		return result, fmt.Errorf("binary id is required")
	}

	targets := normalizePAR2CoverageTargets(rows)
	result.MainTargetCount = len(targets)

	// PAR2 target rows are already persisted in binary_par2_targets. Do not fan
	// out inspection writes into binary identity/stat projections; assemble owns them.
	return result, nil
}

type par2CoverageTarget struct {
	fileName       string
	normalizedName string
	baseStem       string
	fileIndex      int
}

func normalizePAR2CoverageTargets(rows []BinaryPAR2TargetRecord) []par2CoverageTarget {
	seen := make(map[string]struct{}, len(rows))
	out := make([]par2CoverageTarget, 0, len(rows))
	for _, row := range rows {
		fileName := strings.TrimSpace(row.FileName)
		normalizedName := strings.ToLower(fileName)
		if normalizedName == "" {
			continue
		}
		if _, ok := seen[normalizedName]; ok {
			continue
		}
		if par2CoverageIsAuxiliary(fileName) {
			continue
		}
		baseStem := par2CoverageBaseStem(fileName)
		if baseStem == "" {
			continue
		}
		seen[normalizedName] = struct{}{}
		out = append(out, par2CoverageTarget{
			fileName:       fileName,
			normalizedName: normalizedName,
			baseStem:       baseStem,
			fileIndex:      par2CoverageFileIndex(fileName),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].normalizedName < out[j].normalizedName
	})
	return out
}

func singlePAR2CoverageBaseStem(targets []par2CoverageTarget) (string, bool) {
	stem := ""
	for _, target := range targets {
		if target.baseStem == "" {
			return "", false
		}
		if stem == "" {
			stem = target.baseStem
			continue
		}
		if stem != target.baseStem {
			return "", false
		}
	}
	return stem, stem != ""
}

func par2CoverageIsAuxiliary(fileName string) bool {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(lower, ".par2") ||
		strings.Contains(lower, ".vol") && strings.HasSuffix(lower, ".par2") ||
		strings.HasSuffix(lower, ".sfv") ||
		strings.HasSuffix(lower, ".nfo") ||
		strings.HasSuffix(lower, ".srr")
}

func par2CoverageFileIndex(fileName string) int {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	if match := par2CoverageRARPartRE.FindStringSubmatch(lower); len(match) == 2 {
		return int(parseInt64PGIndex(match[1]))
	}
	if match := par2CoverageRARRIndexRE.FindStringSubmatch(lower); len(match) == 2 {
		return int(parseInt64PGIndex(match[1])) + 2
	}
	if match := par2CoverageSplitArchiveRE.FindStringSubmatch(lower); len(match) == 2 {
		return int(parseInt64PGIndex(match[1]))
	}
	if strings.HasSuffix(lower, ".rar") || strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".7z") {
		return 1
	}
	return 0
}

func par2CoverageBaseStem(fileName string) string {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	switch {
	case par2CoverageSplitArchiveRE.MatchString(lower):
		lower = par2CoverageSplitArchiveRE.ReplaceAllString(lower, "")
	case par2CoverageRARFamilyRE.MatchString(lower):
		lower = par2CoverageRARFamilyRE.ReplaceAllString(lower, "")
	case strings.HasSuffix(lower, ".rar"):
		lower = strings.TrimSuffix(lower, ".rar")
	case strings.Contains(lower, "."):
		lower = lower[:strings.LastIndex(lower, ".")]
	}
	lower = par2CoverageSeparatorRE.ReplaceAllString(lower, " ")
	lower = par2CoverageMultiSpaceRE.ReplaceAllString(lower, " ")
	return strings.TrimSpace(lower)
}

func par2CoverageReleaseKey(baseStem string) string {
	key := par2CoverageNonKeyCharsRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(baseStem)), "")
	return strings.TrimSpace(key)
}

func parseInt64PGIndex(value string) int64 {
	var out int64
	for _, r := range strings.TrimSpace(value) {
		if r < '0' || r > '9' {
			break
		}
		out = out*10 + int64(r-'0')
	}
	return out
}

func (s *Store) finishBinaryInspection(ctx context.Context, in BinaryInspectionRecord, fallbackStatus string) error {
	return s.finishBinaryInspectionWithDB(ctx, s.db, in, fallbackStatus)
}

type inspectionExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *Store) finishBinaryInspectionWithDB(ctx context.Context, execer inspectionExecer, in BinaryInspectionRecord, fallbackStatus string) error {
	in.StageName = strings.TrimSpace(in.StageName)
	if in.StageName == "" {
		return fmt.Errorf("stage name is required")
	}
	if in.BinaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = fallbackStatus
	}
	if status == "" {
		status = "completed"
	}

	var releaseID any
	if strings.TrimSpace(in.ReleaseID) != "" {
		releaseID = strings.TrimSpace(in.ReleaseID)
	}
	var sourceUpdated any
	if in.SourceUpdatedAt != nil {
		sourceUpdated = in.SourceUpdatedAt.UTC()
	}

	toolJSON, err := json.Marshal(sanitizeStringMap(in.ToolProvenance))
	if err != nil {
		return fmt.Errorf("marshal tool provenance for %s/%d: %w", in.StageName, in.BinaryID, err)
	}
	status, in.ErrorText = normalizeBinaryInspectionTerminalState(in.StageName, status, in.ErrorText, in.Summary)
	summaryJSON, err := json.Marshal(sanitizeStringMap(in.Summary))
	if err != nil {
		return fmt.Errorf("marshal inspection summary for %s/%d: %w", in.StageName, in.BinaryID, err)
	}

	res, err := execer.ExecContext(ctx, `
		UPDATE binary_inspections
		SET release_id = COALESCE(
				CASE
					WHEN $3::TEXT <> '' AND EXISTS (SELECT 1 FROM releases r WHERE r.release_id = $3)
					THEN $3
					ELSE NULL
				END,
				release_id
			),
		    status = $4,
		    finished_at = NOW(),
		    error_text = $5,
		    materialized_bytes = $6,
		    tool_provenance_json = $7,
		    summary_json = $8,
		    source_updated_at = COALESCE($9, source_updated_at),
		    inspection_claimed_by = '',
		    inspection_claimed_until = NULL,
		    updated_at = NOW()
		WHERE stage_name = $1
		  AND binary_id = $2`,
		in.StageName,
		in.BinaryID,
		releaseID,
		status,
		strings.TrimSpace(in.ErrorText),
		in.MaterializedBytes,
		toolJSON,
		summaryJSON,
		sourceUpdated,
	)
	if err != nil {
		return fmt.Errorf("finish binary inspection %s/%d: %w", in.StageName, in.BinaryID, err)
	}
	if rows, rowsErr := res.RowsAffected(); rowsErr == nil && rows > 0 {
		if err := finishInspectReadyQueueRow(ctx, execer, in.StageName, in.BinaryID, status, in.ErrorText); err != nil {
			return err
		}
		return nil
	}

	_, err = execer.ExecContext(ctx, `
		INSERT INTO binary_inspections (
			stage_name,
				binary_id,
				release_id,
				source_posted_at,
				status,
			started_at,
			finished_at,
			error_text,
			materialized_bytes,
			tool_provenance_json,
			summary_json,
			source_updated_at,
			updated_at
		)
		SELECT
				$1,
				bc.binary_id,
			COALESCE(
				CASE
					WHEN $3::TEXT <> '' AND EXISTS (SELECT 1 FROM releases r WHERE r.release_id = $3)
					THEN $3
					ELSE NULL
				END,
				(
					SELECT rf.release_id
					FROM release_files rf
					WHERE rf.binary_id = $2
					ORDER BY rf.release_id
					LIMIT 1
				)
				),
				COALESCE(bc.source_posted_at, NOW()),
				$4,
			NOW(),
			NOW(),
			$5,
			$6,
			$7,
			$8,
			$9,
			NOW()
			FROM binary_core bc
			WHERE bc.binary_id = $2
			ON CONFLICT (source_posted_at, stage_name, binary_id) DO UPDATE
			SET release_id = COALESCE(EXCLUDED.release_id, binary_inspections.release_id),
		    status = EXCLUDED.status,
		    finished_at = NOW(),
		    error_text = EXCLUDED.error_text,
		    materialized_bytes = EXCLUDED.materialized_bytes,
		    tool_provenance_json = EXCLUDED.tool_provenance_json,
		    summary_json = EXCLUDED.summary_json,
		    source_updated_at = COALESCE(EXCLUDED.source_updated_at, binary_inspections.source_updated_at),
		    inspection_claimed_by = '',
		    inspection_claimed_until = NULL,
		    updated_at = NOW()`,
		in.StageName,
		in.BinaryID,
		releaseID,
		status,
		strings.TrimSpace(in.ErrorText),
		in.MaterializedBytes,
		toolJSON,
		summaryJSON,
		sourceUpdated,
	)
	if err != nil {
		return fmt.Errorf("finish binary inspection %s/%d: %w", in.StageName, in.BinaryID, err)
	}
	if err := finishInspectReadyQueueRow(ctx, execer, in.StageName, in.BinaryID, status, in.ErrorText); err != nil {
		return err
	}

	return nil
}

func normalizeBinaryInspectionTerminalState(stageName, status, errorText string, summary map[string]any) (string, string) {
	status = strings.TrimSpace(status)
	errorText = strings.TrimSpace(errorText)
	if status != "completed" {
		return status, errorText
	}
	if !inspectionStageSupportsErrorFailure(stageName) {
		return status, errorText
	}
	for _, key := range []string{"probe_error", "ffprobe_error", "extract_error", "archive_extract_error"} {
		if msg := inspectionSummaryMessage(summary, key); msg != "" {
			if errorText == "" {
				errorText = msg
			}
			return "failed", errorText
		}
	}
	if errorText != "" {
		return "failed", errorText
	}
	return status, errorText
}

func inspectionStageSupportsErrorFailure(stageName string) bool {
	switch strings.TrimSpace(stageName) {
	case "inspect_archive", "inspect_media", "inspect_discovery", "inspect_par2", "inspect_nfo", "inspect_password":
		return true
	default:
		return false
	}
}

func inspectionSummaryMessage(summary map[string]any, key string) string {
	if len(summary) == 0 {
		return ""
	}
	raw, ok := summary[strings.TrimSpace(key)]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func isRecoverableInspectionError(msg string) bool {
	msg = strings.ToLower(strings.TrimSpace(msg))
	if msg == "" {
		return false
	}
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable")
}

func inspectCandidateFilter(stageName string, requireExpectedFileCount bool) (string, error) {
	expectedFileCountGate := "TRUE"
	if requireExpectedFileCount {
		expectedFileCountGate = "(r.expected_file_count <= 0 OR r.file_count >= r.expected_file_count)"
	}
	payloadCompleteGate := `b.total_parts > 0 AND
		b.observed_parts >= b.total_parts AND
		(b.is_main_payload = TRUE OR b.is_auxiliary = FALSE)`
	switch stageName {
	case "inspect_discovery":
		return `r.completion_pct >= 100 AND
		(r.expected_file_count <= 0 OR r.file_count >= r.expected_file_count) AND
		COALESCE(b.recovered_extension, '') = '' AND
		(b.is_main_payload = TRUE OR b.is_auxiliary = FALSE) AND
		(
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.bin' OR
			COALESCE(rf.file_name, b.file_name, '') !~ '\.[A-Za-z0-9]{1,8}$'
		)`, nil
	case "inspect_par2":
		return `rf.is_pars = TRUE OR
		LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.par2' OR
		COALESCE(b.recovered_kind, '') = 'par2' OR
		COALESCE(b.recovered_extension, '') = '.par2'`, nil
	case "inspect_nfo":
		return "LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.nfo'", nil
	case "inspect_archive":
		return payloadCompleteGate + ` AND
		(
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.7z' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.001$' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.zip' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.001$' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.part0*1\.rar$' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.r00$' OR
			(
				LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.rar' AND
				LOWER(COALESCE(rf.file_name, b.file_name, '')) !~ '\.part\d+\.rar$' AND
				LOWER(COALESCE(rf.file_name, b.file_name, '')) !~ '\.r\d{2,3}$'
			)
		)`, nil
	case "inspect_password":
		return `r.encrypted = TRUE AND
		r.completion_pct >= 100 AND
		` + expectedFileCountGate + ` AND
		(
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.7z' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.001$' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.zip' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.001$' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.rar' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.part0*1\.rar$'
		)`, nil
	case "inspect_media":
		return payloadCompleteGate + ` AND
		(
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.mkv' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.mp4' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.avi' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.ts' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.flac' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.mp3' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.m4a' OR
			(
				CASE
					WHEN jsonb_typeof(abi.summary_json->'archive_entries') = 'array' THEN jsonb_array_length(abi.summary_json->'archive_entries')
					ELSE 0
				END > 0 AND (
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.7z' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.001$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.zip' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.001$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.part0*1\.rar$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.r00$' OR
					(
						LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.rar' AND
						LOWER(COALESCE(rf.file_name, b.file_name, '')) !~ '\.part\d+\.rar$' AND
						LOWER(COALESCE(rf.file_name, b.file_name, '')) !~ '\.r\d{2,3}$'
					)
				)
			)
		)`, nil
	default:
		return "", fmt.Errorf("unsupported inspection stage %q", stageName)
	}
}

func nullableBool(v *bool) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableFloat64(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
