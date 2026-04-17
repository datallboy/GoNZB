package pgindex

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// unassembled header row used by Milestone 6 assembly service.
type AssemblyCandidate struct {
	ID            int64
	ProviderID    int64
	NewsgroupID   int64
	ArticleNumber int64
	MessageID     string
	Subject       string
	Poster        string
	DateUTC       *time.Time
	Bytes         int64
	Lines         int
	Xref          string
	RawOverview   map[string]any
}

// binary upsert input for assembly service.
type BinaryRecord struct {
	ProviderID        int64
	NewsgroupID       int64
	PosterID          int64
	SourceReleaseKey  string
	ReleaseFamilyKey  string
	FileFamilyKey     string
	FamilyKind        string
	BaseStem          string
	PostingBucket     string
	IsAuxiliary       bool
	IsMainPayload     bool
	ReleaseKey        string
	ReleaseName       string
	BinaryKey         string
	BinaryName        string
	FileName          string
	FileIndex         int
	ExpectedFileCount int
	TotalParts        int
	PostedAt          *time.Time
	MatchConfidence   float64
	MatchStatus       string
	GroupingEvidence  map[string]any
}

// binary part upsert input for assembly service.
type BinaryPartRecord struct {
	BinaryID        int64
	ArticleHeaderID int64
	MessageID       string
	PartNumber      int
	TotalParts      int
	SegmentBytes    int64
	FileName        string
}

func normalizeBinaryIdentity(in *BinaryRecord) {
	if in == nil {
		return
	}
	in.ReleaseFamilyKey = firstNonBlank(in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey)
	in.SourceReleaseKey = firstNonBlank(in.SourceReleaseKey, in.ReleaseFamilyKey, in.ReleaseKey)
	// Keep legacy release_key as a compatibility mirror of release_family_key during cutover.
	in.ReleaseKey = firstNonBlank(in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey)
}

func (s *Store) ListUnassembledArticleHeaders(ctx context.Context, limit int) ([]AssemblyCandidate, error) {
	if limit <= 0 {
		limit = 1000
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			ah.id,
			ah.provider_id,
			ah.newsgroup_id,
			ah.article_number,
			ah.message_id,
			ah.subject,
			COALESCE(p.poster_name, ah.poster),
			ah.date_utc,
			ah.bytes,
			ah.lines,
			ah.xref,
			COALESCE(ah.raw_overview_json::text, '')
		FROM article_headers ah
		LEFT JOIN article_poster_map apm ON apm.article_header_id = ah.id
		LEFT JOIN posters p ON p.id = apm.poster_id
		WHERE NOT EXISTS (
			SELECT 1
			FROM binary_parts bp
			WHERE bp.article_header_id = ah.id
		)
		ORDER BY ah.newsgroup_id, ah.article_number
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list unassembled article headers: %w", err)
	}
	defer rows.Close()

	out := make([]AssemblyCandidate, 0, limit)
	for rows.Next() {
		var (
			item AssemblyCandidate
			date sql.NullTime
			raw  string
		)

		if err := rows.Scan(
			&item.ID,
			&item.ProviderID,
			&item.NewsgroupID,
			&item.ArticleNumber,
			&item.MessageID,
			&item.Subject,
			&item.Poster,
			&date,
			&item.Bytes,
			&item.Lines,
			&item.Xref,
			&raw,
		); err != nil {
			return nil, fmt.Errorf("scan unassembled article header: %w", err)
		}

		if date.Valid {
			t := date.Time.UTC()
			item.DateUTC = &t
		}

		if raw != "" {
			_ = json.Unmarshal([]byte(raw), &item.RawOverview)
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unassembled article headers: %w", err)
	}

	return out, nil
}

// CHANGED: normalize posters into a dimension table.
func (s *Store) EnsurePoster(ctx context.Context, posterName string) (int64, error) {
	posterName = strings.TrimSpace(posterName)
	if posterName == "" {
		return 0, nil
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO posters (poster_name)
		VALUES ($1)
		ON CONFLICT (poster_name) DO UPDATE
		SET poster_name = EXCLUDED.poster_name
		RETURNING id`,
		posterName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure poster %q: %w", posterName, err)
	}

	return id, nil
}

// CHANGED: map article header -> poster id for later enrichment/debugging.
func (s *Store) LinkArticlePoster(ctx context.Context, articleHeaderID, posterID int64) error {
	if articleHeaderID <= 0 || posterID <= 0 {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO article_poster_map (article_header_id, poster_id)
		VALUES ($1, $2)
		ON CONFLICT (article_header_id) DO UPDATE
		SET poster_id = EXCLUDED.poster_id`,
		articleHeaderID, posterID,
	)
	if err != nil {
		return fmt.Errorf("link article %d to poster %d: %w", articleHeaderID, posterID, err)
	}

	return nil
}

// CHANGED: create/update a binary grouping row.
func (s *Store) UpsertBinary(ctx context.Context, in BinaryRecord) (int64, error) {
	if in.ProviderID <= 0 || in.NewsgroupID <= 0 {
		return 0, fmt.Errorf("provider id and newsgroup id are required")
	}

	normalizeBinaryIdentity(&in)
	in.BinaryKey = strings.TrimSpace(in.BinaryKey)
	if in.ReleaseKey == "" || in.BinaryKey == "" {
		return 0, fmt.Errorf("release key and binary key are required")
	}
	in.MatchStatus = strings.TrimSpace(in.MatchStatus)
	if in.MatchStatus == "" {
		in.MatchStatus = "low_confidence"
	}

	var postedAt any
	if in.PostedAt != nil {
		postedAt = in.PostedAt.UTC()
	}

	var posterID any
	if in.PosterID > 0 {
		posterID = in.PosterID
	}

	evidenceJSON := []byte(`{}`)
	if cleanEvidence := sanitizeStringMap(in.GroupingEvidence); len(cleanEvidence) > 0 {
		b, err := json.Marshal(cleanEvidence)
		if err != nil {
			return 0, fmt.Errorf("marshal binary grouping evidence %q: %w", in.BinaryKey, err)
		}
		evidenceJSON = b
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin binary upsert tx: %w", err)
	}
	defer rollbackTx(tx)

	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO binaries (
			provider_id,
			newsgroup_id,
			poster_id,
			source_release_key,
			release_family_key,
			file_family_key,
			family_kind,
			base_stem,
			posting_bucket,
			is_auxiliary,
			is_main_payload,
			release_key,
			release_name,
			binary_key,
			binary_name,
			file_name,
			file_index,
			expected_file_count,
			total_parts,
			posted_at,
			match_confidence,
			match_status,
			grouping_evidence_json,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,NOW())
		ON CONFLICT (provider_id, newsgroup_id, binary_key) DO UPDATE
		SET poster_id = COALESCE(EXCLUDED.poster_id, binaries.poster_id),
		    source_release_key = EXCLUDED.source_release_key,
		    release_family_key = EXCLUDED.release_family_key,
		    file_family_key = EXCLUDED.file_family_key,
		    family_kind = EXCLUDED.family_kind,
		    base_stem = EXCLUDED.base_stem,
		    posting_bucket = EXCLUDED.posting_bucket,
		    is_auxiliary = EXCLUDED.is_auxiliary,
		    is_main_payload = EXCLUDED.is_main_payload,
		    release_key = EXCLUDED.release_key,
		    release_name = EXCLUDED.release_name,
		    binary_name = EXCLUDED.binary_name,
		    file_name = EXCLUDED.file_name,
		    file_index = CASE
		    	WHEN EXCLUDED.file_index > 0 THEN EXCLUDED.file_index
		    	ELSE binaries.file_index
		    END,
		    expected_file_count = GREATEST(binaries.expected_file_count, EXCLUDED.expected_file_count),
		    total_parts = GREATEST(binaries.total_parts, EXCLUDED.total_parts),
		    posted_at = COALESCE(binaries.posted_at, EXCLUDED.posted_at),
		    match_confidence = GREATEST(binaries.match_confidence, EXCLUDED.match_confidence),
		    match_status = CASE
		    	WHEN EXCLUDED.match_confidence >= binaries.match_confidence THEN EXCLUDED.match_status
		    	ELSE binaries.match_status
		    END,
		    grouping_evidence_json = CASE
		    	WHEN EXCLUDED.match_confidence >= binaries.match_confidence THEN EXCLUDED.grouping_evidence_json
		    	ELSE binaries.grouping_evidence_json
		    END,
		    updated_at = NOW()
		RETURNING id`,
		in.ProviderID,
		in.NewsgroupID,
		posterID,
		strings.TrimSpace(in.SourceReleaseKey),
		strings.TrimSpace(in.ReleaseFamilyKey),
		strings.TrimSpace(in.FileFamilyKey),
		strings.TrimSpace(in.FamilyKind),
		strings.TrimSpace(in.BaseStem),
		strings.TrimSpace(in.PostingBucket),
		in.IsAuxiliary,
		in.IsMainPayload,
		in.ReleaseKey,
		strings.TrimSpace(in.ReleaseName),
		in.BinaryKey,
		strings.TrimSpace(in.BinaryName),
		strings.TrimSpace(in.FileName),
		in.FileIndex,
		in.ExpectedFileCount,
		in.TotalParts,
		postedAt,
		in.MatchConfidence,
		in.MatchStatus,
		[]byte(`{}`),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert binary %q: %w", in.BinaryKey, err)
	}

	if err := upsertBinaryGroupingEvidence(ctx, tx, id, evidenceJSON); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit binary upsert tx %q: %w", in.BinaryKey, err)
	}

	return id, nil
}

func upsertBinaryGroupingEvidence(ctx context.Context, tx *sql.Tx, binaryID int64, payload []byte) error {
	if tx == nil {
		return fmt.Errorf("binary grouping evidence tx is required")
	}
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}
	if len(bytes.TrimSpace(payload)) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte(`{}`)) {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM binary_grouping_evidence
			WHERE binary_id = $1`, binaryID); err != nil {
			return fmt.Errorf("delete binary grouping evidence %d: %w", binaryID, err)
		}
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO binary_grouping_evidence (
			binary_id,
			evidence_source,
			evidence_version,
			payload_json,
			updated_at
		)
		VALUES ($1, 'matcher', 'v1', $2, NOW())
		ON CONFLICT (binary_id) DO UPDATE
		SET payload_json = EXCLUDED.payload_json,
		    updated_at = NOW()`,
		binaryID,
		payload,
	); err != nil {
		return fmt.Errorf("upsert binary grouping evidence %d: %w", binaryID, err)
	}
	return nil
}

// CHANGED: add/update one binary part row.
func (s *Store) UpsertBinaryPart(ctx context.Context, in BinaryPartRecord) error {
	if in.BinaryID <= 0 || in.ArticleHeaderID <= 0 {
		return fmt.Errorf("binary id and article header id are required")
	}
	if in.PartNumber <= 0 {
		return fmt.Errorf("part number is required")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO binary_parts (
			binary_id,
			article_header_id,
			message_id,
			part_number,
			total_parts,
			segment_bytes,
			file_name,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
		ON CONFLICT (binary_id, part_number) DO UPDATE
		SET article_header_id = EXCLUDED.article_header_id,
		    message_id = EXCLUDED.message_id,
		    total_parts = GREATEST(binary_parts.total_parts, EXCLUDED.total_parts),
		    segment_bytes = EXCLUDED.segment_bytes,
		    file_name = EXCLUDED.file_name,
		    updated_at = NOW()`,
		in.BinaryID,
		in.ArticleHeaderID,
		strings.TrimSpace(in.MessageID),
		in.PartNumber,
		in.TotalParts,
		in.SegmentBytes,
		strings.TrimSpace(in.FileName),
	)
	if err != nil {
		return fmt.Errorf("upsert binary part binary=%d part=%d: %w", in.BinaryID, in.PartNumber, err)
	}

	return nil
}

// CHANGED: recompute binary aggregate stats after parts were inserted.
func (s *Store) RefreshBinaryStats(ctx context.Context, binaryID int64) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE binaries b
		SET observed_parts = agg.observed_parts,
		    total_bytes = agg.total_bytes,
		    first_article_number = agg.first_article_number,
		    last_article_number = agg.last_article_number,
		    posted_at = COALESCE(agg.posted_at, b.posted_at),
		    updated_at = NOW()
		FROM (
			SELECT
				bp.binary_id,
				COUNT(*)::INTEGER AS observed_parts,
				COALESCE(SUM(bp.segment_bytes), 0)::BIGINT AS total_bytes,
				COALESCE(MIN(ah.article_number), 0)::BIGINT AS first_article_number,
				COALESCE(MAX(ah.article_number), 0)::BIGINT AS last_article_number,
				MIN(ah.date_utc) AS posted_at
			FROM binary_parts bp
			JOIN article_headers ah ON ah.id = bp.article_header_id
			WHERE bp.binary_id = $1
			GROUP BY bp.binary_id
		) agg
		WHERE b.id = agg.binary_id`, binaryID)
	if err != nil {
		return fmt.Errorf("refresh binary stats %d: %w", binaryID, err)
	}

	return nil
}
