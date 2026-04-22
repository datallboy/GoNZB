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
	ID                              int64
	ProviderID                      int64
	NewsgroupID                     int64
	NewsgroupName                   string
	ArticleNumber                   int64
	MessageID                       string
	Subject                         string
	Poster                          string
	DateUTC                         *time.Time
	Bytes                           int64
	Lines                           int
	Xref                            string
	PosterID                        int64
	FileName                        string
	FileIndex                       int
	FileTotal                       int
	YEncPart                        int
	YEncTotal                       int
	YEncFileSize                    int64
	StructuredIdentityBinaryMatched bool
	RawOverview                     map[string]any
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

	laneALimit := 1
	if limit > 1 {
		laneALimit = (limit * 7) / 10
		if laneALimit <= 0 {
			laneALimit = 1
		}
	}

	priorityBinaryWindow := laneALimit
	if priorityBinaryWindow < 1000 {
		priorityBinaryWindow = 1000
	}
	if priorityBinaryWindow > 2000 {
		priorityBinaryWindow = 2000
	}

	priorityBinaries, err := s.listPriorityAssemblyBinaries(ctx, priorityBinaryWindow)
	if err != nil {
		return nil, err
	}

	type laneABucket struct {
		headers []AssemblyCandidate
	}

	buckets := make([]laneABucket, 0, len(priorityBinaries))
	bucketHeaderCount := 0
	for start := 0; start < len(priorityBinaries) && bucketHeaderCount < laneALimit; start += 20 {
		end := start + 20
		if end > len(priorityBinaries) {
			end = len(priorityBinaries)
		}

		batchMatches, err := s.listPendingHeadersForProgressBinaries(ctx, priorityBinaries[start:end])
		if err != nil {
			return nil, err
		}

		for _, matches := range batchMatches {
			if len(matches) == 0 {
				continue
			}
			buckets = append(buckets, laneABucket{headers: matches})
			bucketHeaderCount += len(matches)
			if bucketHeaderCount >= laneALimit {
				break
			}
		}
	}

	out := make([]AssemblyCandidate, 0, limit)
	selectedIDs := make(map[int64]struct{}, limit)
	for offset := 0; len(out) < laneALimit; offset++ {
		progressed := false
		for _, bucket := range buckets {
			if offset >= len(bucket.headers) {
				continue
			}
			candidate := bucket.headers[offset]
			if _, exists := selectedIDs[candidate.ID]; exists {
				continue
			}
			selectedIDs[candidate.ID] = struct{}{}
			out = append(out, candidate)
			progressed = true
			if len(out) >= laneALimit {
				break
			}
		}
		if !progressed {
			break
		}
	}

	remaining := limit - len(out)
	if remaining <= 0 {
		return out, nil
	}

	recentWindow := remaining * 20
	if recentWindow < remaining {
		recentWindow = remaining
	}

	recentHeaders, err := s.listRecentUnassembledHeaders(ctx, recentWindow)
	if err != nil {
		return nil, err
	}
	for _, candidate := range recentHeaders {
		if len(out) >= limit {
			break
		}
		if _, exists := selectedIDs[candidate.ID]; exists {
			continue
		}
		selectedIDs[candidate.ID] = struct{}{}
		out = append(out, candidate)
	}

	return out, nil
}

type assemblyProgressBinary struct {
	BinaryID           int64
	ProviderID         int64
	NewsgroupID        int64
	NormalizedFileName string
	MissingParts       int
}

func (s *Store) listPriorityAssemblyBinaries(ctx context.Context, limit int) ([]assemblyProgressBinary, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH ranked AS (
			SELECT
				b.id AS binary_id,
				b.provider_id,
				b.newsgroup_id,
				LOWER(BTRIM(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, '')))) AS normalized_file_name,
				b.is_main_payload,
				b.observed_parts,
				CASE
					WHEN b.total_parts > 0 THEN b.observed_parts::DOUBLE PRECISION / b.total_parts::DOUBLE PRECISION
					ELSE 0
				END AS completion_ratio,
				GREATEST(b.total_parts - b.observed_parts, 0) AS missing_parts,
				ROW_NUMBER() OVER (
					PARTITION BY
						b.provider_id,
						b.newsgroup_id,
						LOWER(BTRIM(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''))))
					ORDER BY
						CASE
							WHEN b.is_main_payload THEN 0
							ELSE 1
						END ASC,
						CASE
							WHEN b.total_parts > 0 THEN b.observed_parts::DOUBLE PRECISION / b.total_parts::DOUBLE PRECISION
							ELSE 0
						END DESC,
						b.observed_parts DESC,
						b.id DESC
				) AS file_rank
			FROM binaries b
			WHERE b.total_parts > 0
			  AND b.observed_parts < b.total_parts
			  AND BTRIM(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''))) <> ''
		)
		SELECT
			binary_id,
			provider_id,
			newsgroup_id,
			normalized_file_name,
			missing_parts
		FROM ranked
		WHERE file_rank = 1
		ORDER BY
			CASE
				WHEN is_main_payload THEN 0
				ELSE 1
			END ASC,
			completion_ratio DESC,
			observed_parts DESC,
			binary_id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list priority assembly binaries: %w", err)
	}
	defer rows.Close()

	out := make([]assemblyProgressBinary, 0, limit)
	for rows.Next() {
		var item assemblyProgressBinary
		if err := rows.Scan(
			&item.BinaryID,
			&item.ProviderID,
			&item.NewsgroupID,
			&item.NormalizedFileName,
			&item.MissingParts,
		); err != nil {
			return nil, fmt.Errorf("scan priority assembly binary: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate priority assembly binaries: %w", err)
	}

	return out, nil
}

func (s *Store) listPendingHeadersForProgressBinaries(ctx context.Context, binaries []assemblyProgressBinary) ([][]AssemblyCandidate, error) {
	if len(binaries) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(binaries)*6)
	values := make([]string, 0, len(binaries))
	for i, binary := range binaries {
		perBinaryLimit := binary.MissingParts
		switch {
		case perBinaryLimit <= 0:
			perBinaryLimit = 1
		case perBinaryLimit > 1:
			perBinaryLimit = 1
		}

		base := (i * 6) + 1
		values = append(values, fmt.Sprintf("($%d::bigint,$%d::bigint,$%d::bigint,$%d::text,$%d::integer,$%d::integer)", base, base+1, base+2, base+3, base+4, base+5))
		args = append(
			args,
			binary.BinaryID,
			binary.ProviderID,
			binary.NewsgroupID,
			binary.NormalizedFileName,
			perBinaryLimit,
			i,
		)
	}

	query := fmt.Sprintf(`
		WITH requested_binaries (
			binary_id,
			provider_id,
			newsgroup_id,
			normalized_file_name,
			per_binary_limit,
			binary_rank
		) AS (
			VALUES %s
		)
		SELECT
			rb.binary_id,
			matches.id,
			matches.provider_id,
			matches.newsgroup_id,
			matches.group_name,
			matches.article_number,
			matches.message_id,
			matches.subject,
			matches.poster,
			matches.date_utc,
			matches.bytes,
			matches.lines,
			matches.xref,
			matches.poster_id,
			matches.subject_file_name,
			matches.subject_file_index,
			matches.subject_file_total,
			matches.yenc_part_number,
			matches.yenc_total_parts,
			matches.yenc_file_size,
			matches.structured_identity_binary_matched,
			matches.raw_overview
		FROM requested_binaries rb
		JOIN LATERAL (
			SELECT
				ah.id,
				ah.provider_id,
				ah.newsgroup_id,
				ng.group_name,
				ah.article_number,
				ah.message_id,
				p.subject,
				COALESCE(po.poster_name, p.poster, '') AS poster,
				ah.date_utc,
				ah.bytes,
				ah.lines,
				p.xref,
				COALESCE(p.poster_id, 0) AS poster_id,
				COALESCE(p.subject_file_name, '') AS subject_file_name,
				COALESCE(p.subject_file_index, 0) AS subject_file_index,
				COALESCE(p.subject_file_total, 0) AS subject_file_total,
				COALESCE(p.yenc_part_number, 0) AS yenc_part_number,
				COALESCE(p.yenc_total_parts, 0) AS yenc_total_parts,
				COALESCE(p.yenc_file_size, 0) AS yenc_file_size,
				TRUE AS structured_identity_binary_matched,
				COALESCE(p.raw_overview_json::text, '') AS raw_overview
			FROM article_header_ingest_payloads p
			JOIN article_headers ah
			  ON ah.id = p.article_header_id
			 AND ah.provider_id = rb.provider_id
			 AND ah.newsgroup_id = rb.newsgroup_id
			 AND ah.assembled_at IS NULL
			JOIN newsgroups ng ON ng.id = ah.newsgroup_id
			LEFT JOIN posters po ON po.id = p.poster_id
			WHERE BTRIM(p.subject_file_name) <> ''
			  AND LOWER(BTRIM(p.subject_file_name)) = rb.normalized_file_name
			ORDER BY p.article_header_id DESC
			LIMIT rb.per_binary_limit
		) matches ON TRUE
		ORDER BY rb.binary_rank ASC, matches.id DESC`, strings.Join(values, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list pending headers for progress binaries: %w", err)
	}
	defer rows.Close()

	grouped := make(map[int64][]AssemblyCandidate, len(binaries))
	for rows.Next() {
		var binaryID int64
		item, err := scanAssemblyCandidateWithBinaryID(rows, &binaryID)
		if err != nil {
			return nil, err
		}
		grouped[binaryID] = append(grouped[binaryID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending headers for progress binaries: %w", err)
	}

	out := make([][]AssemblyCandidate, 0, len(binaries))
	for _, binary := range binaries {
		out = append(out, grouped[binary.BinaryID])
	}
	return out, nil
}

func (s *Store) listRecentUnassembledHeaders(ctx context.Context, limit int) ([]AssemblyCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			ah.id,
			ah.provider_id,
			ah.newsgroup_id,
			ng.group_name,
			ah.article_number,
			ah.message_id,
			p.subject,
			COALESCE(po.poster_name, p.poster, '') AS poster,
			ah.date_utc,
			ah.bytes,
			ah.lines,
			p.xref,
			COALESCE(p.poster_id, 0) AS poster_id,
			COALESCE(p.subject_file_name, '') AS subject_file_name,
			COALESCE(p.subject_file_index, 0) AS subject_file_index,
			COALESCE(p.subject_file_total, 0) AS subject_file_total,
			COALESCE(p.yenc_part_number, 0) AS yenc_part_number,
			COALESCE(p.yenc_total_parts, 0) AS yenc_total_parts,
			COALESCE(p.yenc_file_size, 0) AS yenc_file_size,
			FALSE AS structured_identity_binary_matched,
			COALESCE(p.raw_overview_json::text, '') AS raw_overview
		FROM article_headers ah
		JOIN article_header_ingest_payloads p ON p.article_header_id = ah.id
		JOIN newsgroups ng ON ng.id = ah.newsgroup_id
		LEFT JOIN posters po ON po.id = p.poster_id
		WHERE ah.assembled_at IS NULL
		ORDER BY ah.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent unassembled headers: %w", err)
	}
	defer rows.Close()

	out := make([]AssemblyCandidate, 0, limit)
	for rows.Next() {
		item, err := scanAssemblyCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent unassembled headers: %w", err)
	}

	return out, nil
}

func scanAssemblyCandidate(scanner interface {
	Scan(dest ...any) error
}) (AssemblyCandidate, error) {
	return scanAssemblyCandidateWithBinaryID(scanner, nil)
}

func scanAssemblyCandidateWithBinaryID(scanner interface {
	Scan(dest ...any) error
}, binaryID *int64) (AssemblyCandidate, error) {
	var (
		item AssemblyCandidate
		date sql.NullTime
		raw  string
	)

	dest := make([]any, 0, 22)
	if binaryID != nil {
		dest = append(dest, binaryID)
	}
	dest = append(dest,
		&item.ID,
		&item.ProviderID,
		&item.NewsgroupID,
		&item.NewsgroupName,
		&item.ArticleNumber,
		&item.MessageID,
		&item.Subject,
		&item.Poster,
		&date,
		&item.Bytes,
		&item.Lines,
		&item.Xref,
		&item.PosterID,
		&item.FileName,
		&item.FileIndex,
		&item.FileTotal,
		&item.YEncPart,
		&item.YEncTotal,
		&item.YEncFileSize,
		&item.StructuredIdentityBinaryMatched,
		&raw,
	)

	if err := scanner.Scan(dest...); err != nil {
		return AssemblyCandidate{}, fmt.Errorf("scan unassembled article header: %w", err)
	}

	if date.Valid {
		t := date.Time.UTC()
		item.DateUTC = &t
	}

	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &item.RawOverview)
	}
	if item.RawOverview == nil {
		item.RawOverview = make(map[string]any, 7)
	}
	if strings.TrimSpace(item.FileName) != "" {
		item.RawOverview["name"] = item.FileName
	}
	if item.FileIndex > 0 {
		item.RawOverview["file_index"] = item.FileIndex
	}
	if item.FileTotal > 0 {
		item.RawOverview["file_total"] = item.FileTotal
	}
	if item.YEncPart > 0 {
		item.RawOverview["part"] = item.YEncPart
	}
	if item.YEncTotal > 0 {
		item.RawOverview["total"] = item.YEncTotal
	}
	if item.YEncFileSize > 0 {
		item.RawOverview["size"] = item.YEncFileSize
	}

	return item, nil
}

func (s *Store) CountUnassembledArticleHeaders(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM article_headers
		WHERE assembled_at IS NULL`,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unassembled article headers: %w", err)
	}
	return count, nil
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
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,NOW())
		ON CONFLICT (provider_id, newsgroup_id, binary_key) DO UPDATE
		SET poster_id = COALESCE(EXCLUDED.poster_id, binaries.poster_id),
		    source_release_key = EXCLUDED.source_release_key,
		    release_family_key = EXCLUDED.release_family_key,
		    file_family_key = EXCLUDED.file_family_key,
		    family_kind = EXCLUDED.family_kind,
		    base_stem = EXCLUDED.base_stem,
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

	if err := markReleaseFamilyDirty(ctx, tx, in.ProviderID, in.NewsgroupID, "release_family", in.ReleaseFamilyKey); err != nil {
		return 0, err
	}
	if in.ExpectedFileCount > 1 {
		if err := markReleaseFamilyDirty(ctx, tx, in.ProviderID, in.NewsgroupID, "base_stem", strings.ToLower(strings.TrimSpace(in.BaseStem))); err != nil {
			return 0, err
		}
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

func markReleaseFamilyDirty(ctx context.Context, tx *sql.Tx, providerID, newsgroupID int64, keyKind, familyKey string) error {
	if tx == nil {
		return fmt.Errorf("release dirty queue tx is required")
	}
	familyKey = strings.TrimSpace(familyKey)
	if providerID <= 0 || newsgroupID <= 0 || familyKey == "" {
		return nil
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO release_stage_dirty_families (
			provider_id,
			newsgroup_id,
			key_kind,
			family_key,
			updated_at
		)
		VALUES ($1,$2,$3,$4,NOW())
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET updated_at = NOW()`,
		providerID,
		newsgroupID,
		strings.TrimSpace(keyKind),
		familyKey,
	)
	if err != nil {
		return fmt.Errorf("mark release family dirty provider=%d group=%d key_kind=%s family=%q: %w", providerID, newsgroupID, keyKind, familyKey, err)
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

	if _, err := s.db.ExecContext(ctx, `
		UPDATE article_headers
		SET assembled_at = COALESCE(assembled_at, NOW())
		WHERE id = $1`, in.ArticleHeaderID); err != nil {
		return fmt.Errorf("mark article header %d assembled: %w", in.ArticleHeaderID, err)
	}

	return nil
}

// CHANGED: recompute binary aggregate stats after parts were inserted.
func (s *Store) RefreshBinaryStats(ctx context.Context, binaryID int64) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin refresh binary stats tx %d: %w", binaryID, err)
	}
	defer rollbackTx(tx)

	var (
		providerID        int64
		newsgroupID       int64
		releaseFamilyKey  string
		baseStem          string
		expectedFileCount int
	)
	err = tx.QueryRowContext(ctx, `
		WITH agg AS (
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
		)
		UPDATE binaries b
		SET observed_parts = agg.observed_parts,
		    total_bytes = agg.total_bytes,
		    first_article_number = agg.first_article_number,
		    last_article_number = agg.last_article_number,
		    posted_at = COALESCE(agg.posted_at, b.posted_at),
		    updated_at = NOW()
		FROM agg
		WHERE b.id = agg.binary_id
		RETURNING
			b.provider_id,
			b.newsgroup_id,
			b.release_family_key,
			b.base_stem,
			b.expected_file_count`,
		binaryID,
	).Scan(
		&providerID,
		&newsgroupID,
		&releaseFamilyKey,
		&baseStem,
		&expectedFileCount,
	)
	if err != nil {
		return fmt.Errorf("refresh binary stats %d: %w", binaryID, err)
	}

	if err := markReleaseFamilyDirty(ctx, tx, providerID, newsgroupID, "release_family", releaseFamilyKey); err != nil {
		return err
	}
	if expectedFileCount > 1 {
		if err := markReleaseFamilyDirty(ctx, tx, providerID, newsgroupID, "base_stem", strings.ToLower(strings.TrimSpace(baseStem))); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit refresh binary stats tx %d: %w", binaryID, err)
	}

	return nil
}
