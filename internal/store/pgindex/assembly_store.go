package pgindex

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	assembleLaneARatioNumerator            = 7
	assembleLaneARatioDenominator          = 10
	assemblePriorityBinaryMinScan          = 1000
	assemblePriorityBinaryMaxScan          = 2000
	assemblePriorityBinaryBatch            = 20
	assemblePriorityHeaderWindowMultiplier = 40
	assemblePriorityHeaderMinScan          = 5000
	assemblePriorityHeaderMaxScan          = 100000
	maxBinaryUpsertBatchRetries            = 3
	refreshBinaryStatsBatchSize            = 8000
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
	YEncRecoveryMissingCount        int
	YEncRecoveryRetryAfter          *time.Time
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
	FileSetKey        string
	FileFamilyKey     string
	IdentityStrength  string
	IdentityReason    string
	SubjectSetToken   string
	SubjectSetKind    string
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

type preparedBinaryRecord struct {
	record             BinaryRecord
	postedAt           any
	posterID           any
	evidenceJSON       []byte
	inlineEvidenceJSON []byte
	keepDetailed       bool
}

type binaryEvidenceRecord struct {
	BinaryID     int64
	Payload      []byte
	KeepDetailed bool
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

type AssemblyClaimRequest struct {
	Limit         int
	Owner         string
	LeaseDuration time.Duration
	Lane          string
}

const (
	AssemblyClaimLaneCombined = ""
	AssemblyClaimLaneA        = "lane_a"
	AssemblyClaimLaneB        = "lane_b"
)

type assemblyQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
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
	return s.listUnassembledArticleHeaders(ctx, s.db, limit)
}

func (s *Store) ClaimUnassembledArticleHeaders(ctx context.Context, req AssemblyClaimRequest) ([]AssemblyCandidate, error) {
	if req.Limit <= 0 {
		req.Limit = 1000
	}
	req.Owner = strings.TrimSpace(req.Owner)
	if req.Owner == "" {
		return nil, fmt.Errorf("assembly claim owner is required")
	}
	if req.LeaseDuration <= 0 {
		req.LeaseDuration = 5 * time.Minute
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin assembly claim tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('gonzb-assemble-claim'))`); err != nil {
		return nil, fmt.Errorf("lock assembly claim selector: %w", err)
	}

	candidates, err := s.listUnassembledArticleHeadersForLane(ctx, tx, req.Limit, req.Lane)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return candidates, nil
	}

	args := make([]any, 0, len(candidates)+2)
	values := make([]string, 0, len(candidates))
	args = append(args, req.Owner, int64(req.LeaseDuration/time.Second))
	for _, candidate := range candidates {
		args = append(args, candidate.ID)
		values = append(values, fmt.Sprintf("($%d::bigint)", len(args)))
	}

	query := fmt.Sprintf(`
		WITH requested(id) AS (
			VALUES %s
		),
		claimed AS (
			UPDATE article_headers ah
			SET assembly_claimed_by = $1,
			    assembly_claimed_until = NOW() + ($2::bigint * INTERVAL '1 second')
			FROM requested
			WHERE ah.id = requested.id
			  AND ah.assembled_at IS NULL
			  AND (
			  	ah.assembly_claimed_until IS NULL
			  	OR ah.assembly_claimed_until < NOW()
			  )
			RETURNING ah.id
		)
		SELECT id FROM claimed`,
		strings.Join(values, ","))

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("claim unassembled article headers: %w", err)
	}
	defer rows.Close()

	claimedIDs := make(map[int64]struct{}, len(candidates))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan claimed article header: %w", err)
		}
		claimedIDs[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed article headers: %w", err)
	}

	out := candidates[:0]
	for _, candidate := range candidates {
		if _, ok := claimedIDs[candidate.ID]; ok {
			out = append(out, candidate)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit assembly claim tx: %w", err)
	}
	return out, nil
}

func (s *Store) listUnassembledArticleHeaders(ctx context.Context, q assemblyQueryer, limit int) ([]AssemblyCandidate, error) {
	return s.listUnassembledArticleHeadersForLane(ctx, q, limit, AssemblyClaimLaneCombined)
}

func (s *Store) listUnassembledArticleHeadersForLane(ctx context.Context, q assemblyQueryer, limit int, lane string) ([]AssemblyCandidate, error) {
	if limit <= 0 {
		limit = 1000
	}

	lane = strings.TrimSpace(strings.ToLower(lane))
	laneALimit := limit
	switch lane {
	case AssemblyClaimLaneCombined:
		laneALimit = 1
		if limit > 1 {
			laneALimit = (limit * assembleLaneARatioNumerator) / assembleLaneARatioDenominator
			if laneALimit <= 0 {
				laneALimit = 1
			}
		}
	case AssemblyClaimLaneA:
		laneALimit = limit
	case AssemblyClaimLaneB:
		laneALimit = 0
	default:
		return nil, fmt.Errorf("unknown assembly claim lane %q", lane)
	}

	priorityHeaderWindow := assemblePriorityHeaderMinScan
	if laneALimit > 0 {
		priorityHeaderWindow = laneALimit * assemblePriorityHeaderWindowMultiplier
		if priorityHeaderWindow < assemblePriorityHeaderMinScan {
			priorityHeaderWindow = assemblePriorityHeaderMinScan
		}
		if priorityHeaderWindow > assemblePriorityHeaderMaxScan {
			priorityHeaderWindow = assemblePriorityHeaderMaxScan
		}
	}
	recentHeaderWindow := limit * assemblePriorityHeaderWindowMultiplier
	if recentHeaderWindow < assemblePriorityHeaderMinScan {
		recentHeaderWindow = assemblePriorityHeaderMinScan
	}
	if recentHeaderWindow > assemblePriorityHeaderMaxScan {
		recentHeaderWindow = assemblePriorityHeaderMaxScan
	}

	selected := make([]assemblyCandidateSelection, 0, limit)
	selectedIDs := make(map[int64]struct{}, limit)

	if laneALimit > 0 {
		priorityIDs, err := s.listPriorityAssemblyHeaderIDs(ctx, q, laneALimit, priorityHeaderWindow)
		if err != nil {
			return nil, err
		}
		for _, id := range priorityIDs {
			if len(selected) >= laneALimit {
				break
			}
			if _, exists := selectedIDs[id]; exists {
				continue
			}
			selectedIDs[id] = struct{}{}
			selected = append(selected, assemblyCandidateSelection{
				ID:                              id,
				StructuredIdentityBinaryMatched: true,
			})
		}
	}

	remaining := limit - len(selected)
	if lane == AssemblyClaimLaneA {
		return s.hydrateAssemblyCandidates(ctx, q, selected)
	}
	if remaining <= 0 {
		return s.hydrateAssemblyCandidates(ctx, q, selected)
	}

	recentIDs, err := s.listRecentUnassembledHeaderIDs(ctx, q, remaining, recentHeaderWindow, selectedIDs, lane == AssemblyClaimLaneB)
	if err != nil {
		return nil, err
	}
	for _, id := range recentIDs {
		if len(selected) >= limit {
			break
		}
		if _, exists := selectedIDs[id]; exists {
			continue
		}
		selectedIDs[id] = struct{}{}
		selected = append(selected, assemblyCandidateSelection{ID: id})
	}

	return s.hydrateAssemblyCandidates(ctx, q, selected)
}

type assemblyCandidateSelection struct {
	ID                              int64
	StructuredIdentityBinaryMatched bool
}

func (s *Store) listPriorityAssemblyHeaderIDs(ctx context.Context, q assemblyQueryer, limit, pendingWindow int) ([]int64, error) {
	if limit <= 0 {
		return nil, nil
	}
	if pendingWindow < limit {
		pendingWindow = limit
	}

	rows, err := q.QueryContext(ctx, `
		WITH recent_pending AS (
			SELECT
				ah.id,
				ah.provider_id,
				ah.newsgroup_id
			FROM article_headers ah
			WHERE ah.assembled_at IS NULL
			  AND (
			  	ah.assembly_claimed_until IS NULL
			  	OR ah.assembly_claimed_until < NOW()
			  )
			ORDER BY ah.id DESC
			LIMIT $2
		),
		pending_structured AS (
			SELECT
				rp.id,
				rp.provider_id,
				rp.newsgroup_id,
				LOWER(BTRIM(p.subject_file_name)) AS normalized_file_name
			FROM recent_pending rp
			JOIN article_header_ingest_payloads p ON p.article_header_id = rp.id
			WHERE BTRIM(p.subject_file_name) <> ''
		),
		matched AS (
			SELECT
				ps.id,
				b.binary_id,
				b.is_main_payload,
				b.observed_parts,
				b.completion_ratio
			FROM pending_structured ps
			JOIN LATERAL (
				SELECT
					b.id AS binary_id,
					b.is_main_payload,
					b.observed_parts,
					CASE
						WHEN b.total_parts > 0 THEN b.observed_parts::DOUBLE PRECISION / b.total_parts::DOUBLE PRECISION
						ELSE 0
					END AS completion_ratio
				FROM binaries b
				WHERE b.provider_id = ps.provider_id
				  AND b.newsgroup_id = ps.newsgroup_id
				  AND LOWER(BTRIM(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, '')))) = ps.normalized_file_name
				  AND b.total_parts > 0
				  AND b.observed_parts < b.total_parts
				  AND BTRIM(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''))) <> ''
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
				LIMIT 1
			) b ON true
		),
		selected AS (
			SELECT
				id,
				binary_id,
				is_main_payload,
				observed_parts,
				completion_ratio
			FROM matched
			ORDER BY
				CASE
					WHEN is_main_payload THEN 0
					ELSE 1
				END ASC,
				completion_ratio DESC,
				observed_parts DESC,
				binary_id DESC,
				id DESC
			LIMIT $1
		)
		SELECT
			s.id
		FROM selected s
		ORDER BY
			CASE
				WHEN s.is_main_payload THEN 0
				ELSE 1
			END ASC,
			s.completion_ratio DESC,
			s.observed_parts DESC,
			s.binary_id DESC,
			s.id DESC`,
		limit,
		pendingWindow,
	)
	if err != nil {
		return nil, fmt.Errorf("list priority assembly header ids: %w", err)
	}
	defer rows.Close()

	out := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan priority assembly header id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate priority assembly header ids: %w", err)
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
				'' AS raw_overview
			FROM article_header_ingest_payloads p
			JOIN article_headers ah
			  ON ah.id = p.article_header_id
			 AND ah.provider_id = rb.provider_id
			 AND ah.newsgroup_id = rb.newsgroup_id
			 AND ah.assembled_at IS NULL
			 AND (
			 	ah.assembly_claimed_until IS NULL
			 	OR ah.assembly_claimed_until < NOW()
			 )
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

func (s *Store) listRecentUnassembledHeaderIDs(ctx context.Context, q assemblyQueryer, limit, pendingWindow int, excludeIDs map[int64]struct{}, excludeStructuredMatches bool) ([]int64, error) {
	if limit <= 0 {
		return nil, nil
	}
	if pendingWindow < limit {
		pendingWindow = limit
	}

	args := []any{limit, pendingWindow}
	query := `
		WITH recent_pending AS (
			SELECT
				ah.id,
				ah.provider_id,
				ah.newsgroup_id
			FROM article_headers ah
			WHERE ah.assembled_at IS NULL
			  AND (
			  	ah.assembly_claimed_until IS NULL
			  	OR ah.assembly_claimed_until < NOW()
			  )
			ORDER BY ah.id DESC
			LIMIT $2
		)
		SELECT rp.id
		FROM recent_pending rp
		WHERE TRUE`
	if len(excludeIDs) > 0 {
		ids := make([]int64, 0, len(excludeIDs))
		for id := range excludeIDs {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		args = append(args, ids)
		query += fmt.Sprintf(`
		  AND NOT (rp.id = ANY($%d::bigint[]))`, len(args))
	}
	if excludeStructuredMatches {
		query += `
		  AND NOT EXISTS (
		  	SELECT 1
		  	FROM article_header_ingest_payloads p
		  	JOIN binaries b
		  	  ON b.provider_id = rp.provider_id
		  	 AND b.newsgroup_id = rp.newsgroup_id
		  	 AND LOWER(BTRIM(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, '')))) = LOWER(BTRIM(p.subject_file_name))
		  	 AND b.total_parts > 0
		  	 AND b.observed_parts < b.total_parts
		  	 AND BTRIM(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''))) <> ''
		  	WHERE p.article_header_id = rp.id
		  	  AND BTRIM(p.subject_file_name) <> ''
		  )`
	}
	query += `
		ORDER BY rp.id DESC
		LIMIT $1`

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list recent unassembled header ids: %w", err)
	}
	defer rows.Close()

	out := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan recent unassembled header id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent unassembled header ids: %w", err)
	}

	return out, nil
}

func (s *Store) hydrateAssemblyCandidates(ctx context.Context, q assemblyQueryer, selected []assemblyCandidateSelection) ([]AssemblyCandidate, error) {
	if len(selected) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(selected))
	ords := make([]int32, 0, len(selected))
	structuredMatches := make([]bool, 0, len(selected))
	for idx, item := range selected {
		ids = append(ids, item.ID)
		ords = append(ords, int32(idx))
		structuredMatches = append(structuredMatches, item.StructuredIdentityBinaryMatched)
	}

	rows, err := q.QueryContext(ctx, `
		WITH requested(id, ord, structured_identity_binary_matched) AS (
			SELECT *
			FROM UNNEST($1::bigint[], $2::integer[], $3::boolean[])
		)
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
			COALESCE(p.yenc_recovery_missing_count, 0) AS yenc_recovery_missing_count,
			p.yenc_recovery_retry_after,
			requested.structured_identity_binary_matched,
			'' AS raw_overview
		FROM requested
		JOIN article_headers ah ON ah.id = requested.id
		JOIN article_header_ingest_payloads p ON p.article_header_id = ah.id
		JOIN newsgroups ng ON ng.id = ah.newsgroup_id
		LEFT JOIN posters po ON po.id = p.poster_id
		ORDER BY requested.ord ASC`, ids, ords, structuredMatches)
	if err != nil {
		return nil, fmt.Errorf("hydrate assembly candidates: %w", err)
	}
	defer rows.Close()

	out := make([]AssemblyCandidate, 0, len(selected))
	for rows.Next() {
		item, err := scanAssemblyCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hydrated assembly candidates: %w", err)
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
		item       AssemblyCandidate
		date       sql.NullTime
		retryAfter sql.NullTime
		raw        string
	)

	dest := make([]any, 0, 24)
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
		&item.YEncRecoveryMissingCount,
		&retryAfter,
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
	if retryAfter.Valid {
		t := retryAfter.Time.UTC()
		item.YEncRecoveryRetryAfter = &t
	}

	if item.RawOverview == nil {
		item.RawOverview = make(map[string]any, 8)
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
	if item.Bytes > 0 {
		item.RawOverview["bytes"] = item.Bytes
	}

	return item, nil
}

func (s *Store) RecordYEncRecoveryNotFound(ctx context.Context, articleHeaderID int64) error {
	if articleHeaderID <= 0 {
		return fmt.Errorf("article header id is required")
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE article_header_ingest_payloads
		SET yenc_recovery_missing_count = article_header_ingest_payloads.yenc_recovery_missing_count + 1,
		    yenc_recovery_last_missing_at = NOW(),
		    yenc_recovery_retry_after = NOW() + CASE
		    	WHEN article_header_ingest_payloads.yenc_recovery_missing_count + 1 = 1 THEN INTERVAL '1 hour'
		    	WHEN article_header_ingest_payloads.yenc_recovery_missing_count + 1 = 2 THEN INTERVAL '6 hours'
		    	WHEN article_header_ingest_payloads.yenc_recovery_missing_count + 1 = 3 THEN INTERVAL '24 hours'
		    	ELSE INTERVAL '72 hours'
		    END
		WHERE article_header_id = $1`, articleHeaderID,
	)
	if err != nil {
		return fmt.Errorf("record yenc recovery not found for article header %d: %w", articleHeaderID, err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE yenc_recovery_work_items
		SET status = 'ready',
		    ready_at = COALESCE((
		    	SELECT p.yenc_recovery_retry_after
		    	FROM article_header_ingest_payloads p
		    	WHERE p.article_header_id = $1
		    ), NOW()),
		    missing_count = COALESCE((
		    	SELECT p.yenc_recovery_missing_count
		    	FROM article_header_ingest_payloads p
		    	WHERE p.article_header_id = $1
		    ), missing_count),
		    updated_at = NOW()
		WHERE article_header_id = $1`, articleHeaderID); err != nil {
		return fmt.Errorf("update yenc recovery work item retry state for article header %d: %w", articleHeaderID, err)
	}
	return nil
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

func (s *Store) EstimateUnassembledArticleHeaders(ctx context.Context) (int64, error) {
	var estimated float64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(reltuples, 0)
		FROM pg_class
		WHERE oid = 'idx_article_headers_pending_assembly'::regclass`,
	).Scan(&estimated); err != nil {
		return 0, fmt.Errorf("estimate unassembled article headers: %w", err)
	}
	if estimated <= 0 {
		return 0, nil
	}
	return int64(estimated + 0.5), nil
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
	ids, err := s.UpsertBinaries(ctx, []BinaryRecord{in})
	if err != nil {
		return 0, err
	}
	if len(ids) != 1 {
		return 0, fmt.Errorf("upsert binary returned %d ids", len(ids))
	}
	return ids[0], nil
}

func (s *Store) UpsertBinaries(ctx context.Context, records []BinaryRecord) ([]int64, error) {
	if len(records) == 0 {
		return nil, nil
	}

	prepared := make([]preparedBinaryRecord, 0, len(records))
	for _, record := range records {
		preparedRecord, err := prepareBinaryRecord(record)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, preparedRecord)
	}

	ids := make([]int64, len(prepared))
	chunkSize := binaryUpsertChunkSizeFromContext(ctx)

	for start := 0; start < len(prepared); start += chunkSize {
		end := start + chunkSize
		if end > len(prepared) {
			end = len(prepared)
		}
		chunkIDs, err := s.upsertBinaryChunkWithRetries(ctx, prepared[start:end])
		if err != nil {
			return nil, err
		}
		copy(ids[start:end], chunkIDs)
	}

	return ids, nil
}

func prepareBinaryRecord(in BinaryRecord) (preparedBinaryRecord, error) {
	if in.ProviderID <= 0 || in.NewsgroupID <= 0 {
		return preparedBinaryRecord{}, fmt.Errorf("provider id and newsgroup id are required")
	}

	normalizeBinaryIdentity(&in)
	in.BinaryKey = strings.TrimSpace(in.BinaryKey)
	if in.ReleaseKey == "" || in.BinaryKey == "" {
		return preparedBinaryRecord{}, fmt.Errorf("release key and binary key are required")
	}
	in.MatchStatus = strings.TrimSpace(in.MatchStatus)
	if in.MatchStatus == "" {
		in.MatchStatus = "low_confidence"
	}

	prepared := preparedBinaryRecord{record: in, evidenceJSON: []byte(`{}`), inlineEvidenceJSON: []byte(`{}`)}
	if in.PostedAt != nil {
		prepared.postedAt = in.PostedAt.UTC()
	}
	if in.PosterID > 0 {
		prepared.posterID = in.PosterID
	}

	cleanEvidence := sanitizeStringMap(in.GroupingEvidence)
	if len(cleanEvidence) > 0 {
		b, err := json.Marshal(cleanEvidence)
		if err != nil {
			return preparedBinaryRecord{}, fmt.Errorf("marshal binary grouping evidence %q: %w", in.BinaryKey, err)
		}
		prepared.evidenceJSON = b
		prepared.inlineEvidenceJSON = marshalInlineGroupingEvidence(cleanEvidence)
		prepared.keepDetailed = shouldPersistDetailedGroupingEvidence(in, cleanEvidence)
	}

	return prepared, nil
}

func (s *Store) upsertBinaryChunkWithRetries(ctx context.Context, records []preparedBinaryRecord) ([]int64, error) {
	started := time.Now()
	telemetry := binaryUpsertTelemetryFromContext(ctx)
	var lastErr error
	for attempt := 1; attempt <= maxBinaryUpsertBatchRetries; attempt++ {
		ids, err := s.upsertBinaryChunkOnce(ctx, records)
		if err == nil {
			if telemetry != nil {
				telemetry.recordChunk(len(records), attempt-1, time.Since(started))
			}
			return ids, nil
		}
		lastErr = err
		if telemetry != nil && isRetryableBinaryUpsertError(err) {
			telemetry.recordRetry(err)
		}
		if !isRetryableBinaryUpsertError(err) || attempt == maxBinaryUpsertBatchRetries {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
		}
	}
	return nil, lastErr
}

func (s *Store) upsertBinaryChunkOnce(ctx context.Context, records []preparedBinaryRecord) ([]int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin binary upsert chunk tx: %w", err)
	}
	defer rollbackTx(tx)

	ids, chunkSummaryKeys, err := upsertBinaryChunk(ctx, tx, records)
	if err != nil {
		return nil, err
	}
	sortReleaseFamilySummaryKeys(chunkSummaryKeys)
	if deferReleaseFamilySummaryRefreshFromContext(ctx) {
		if err := markReleaseFamiliesDirtyBatch(ctx, tx, chunkSummaryKeys); err != nil {
			return nil, err
		}
		if telemetry := binaryUpsertTelemetryFromContext(ctx); telemetry != nil {
			telemetry.recordDeferredSummaryRefresh(len(chunkSummaryKeys))
		}
	} else {
		for _, key := range chunkSummaryKeys {
			if err := refreshReleaseFamilySummary(ctx, tx, key); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit binary upsert chunk tx: %w", err)
	}
	return ids, nil
}

func isRetryableBinaryUpsertError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "SQLSTATE 40P01") || strings.Contains(text, "SQLSTATE 40001")
}

func upsertBinaryChunk(ctx context.Context, tx *sql.Tx, records []preparedBinaryRecord) ([]int64, []releaseFamilySummaryKey, error) {
	if len(records) == 0 {
		return nil, nil, nil
	}

	locks := make([]binaryIdentityLock, 0, len(records))
	values := make([]string, 0, len(records))
	args := make([]any, 0, len(records)*29)
	for i, record := range records {
		locks = append(locks, binaryIdentityLock{
			ProviderID:  record.record.ProviderID,
			NewsgroupID: record.record.NewsgroupID,
			BinaryKey:   record.record.BinaryKey,
		})
		base := (i * 29) + 1
		values = append(values, fmt.Sprintf(
			"($%d::integer,$%d::bigint,$%d::bigint,$%d::bigint,$%d::text,$%d::text,$%d::text,$%d::text,$%d::text,$%d::text,$%d::text,$%d::text,$%d::text,$%d::text,$%d::boolean,$%d::boolean,$%d::text,$%d::text,$%d::text,$%d::text,$%d::text,$%d::integer,$%d::integer,$%d::integer,$%d::timestamptz,$%d::double precision,$%d::text,$%d::jsonb,$%d::jsonb)",
			base,
			base+1,
			base+2,
			base+3,
			base+4,
			base+5,
			base+6,
			base+7,
			base+8,
			base+9,
			base+10,
			base+11,
			base+12,
			base+13,
			base+14,
			base+15,
			base+16,
			base+17,
			base+18,
			base+19,
			base+20,
			base+21,
			base+22,
			base+23,
			base+24,
			base+25,
			base+26,
			base+27,
			base+28,
		))
		args = append(args,
			i,
			record.record.ProviderID,
			record.record.NewsgroupID,
			record.posterID,
			strings.TrimSpace(record.record.SourceReleaseKey),
			strings.TrimSpace(record.record.ReleaseFamilyKey),
			strings.TrimSpace(record.record.FileSetKey),
			strings.TrimSpace(record.record.FileFamilyKey),
			strings.TrimSpace(record.record.IdentityStrength),
			strings.TrimSpace(record.record.IdentityReason),
			strings.TrimSpace(record.record.SubjectSetToken),
			strings.TrimSpace(record.record.SubjectSetKind),
			strings.TrimSpace(record.record.FamilyKind),
			strings.TrimSpace(record.record.BaseStem),
			record.record.IsAuxiliary,
			record.record.IsMainPayload,
			record.record.ReleaseKey,
			strings.TrimSpace(record.record.ReleaseName),
			record.record.BinaryKey,
			strings.TrimSpace(record.record.BinaryName),
			strings.TrimSpace(record.record.FileName),
			record.record.FileIndex,
			record.record.ExpectedFileCount,
			record.record.TotalParts,
			record.postedAt,
			record.record.MatchConfidence,
			record.record.MatchStatus,
			record.inlineEvidenceJSON,
			record.evidenceJSON,
		)
	}
	lockStarted := time.Now()
	if err := lockBinaryIdentityKeys(ctx, tx, locks); err != nil {
		return nil, nil, err
	}
	if telemetry := binaryUpsertTelemetryFromContext(ctx); telemetry != nil {
		telemetry.recordLockDuration(time.Since(lockStarted))
	}

	persistedValues := buildUpsertBinaryPersistedValues(records)
	persistedArgs := buildUpsertBinaryPersistedArgs(records)
	upsertQueryStarted := time.Now()
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		WITH requested (
			ordinal,
			provider_id,
			newsgroup_id,
			poster_id,
			source_release_key,
			release_family_key,
			file_set_key,
			file_family_key,
			identity_strength,
			identity_reason,
			subject_set_token,
			subject_set_kind,
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
			grouping_evidence_payload
		) AS (
			VALUES %s
		),
		ordered_requested AS (
			SELECT *
			FROM requested
			ORDER BY provider_id, newsgroup_id, binary_key
		),
		upserted AS (
			INSERT INTO binaries (
				provider_id,
				newsgroup_id,
				poster_id,
				source_release_key,
				release_family_key,
				file_set_key,
				file_family_key,
				identity_strength,
				identity_reason,
				subject_set_token,
				subject_set_kind,
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
			SELECT
				r.provider_id,
				r.newsgroup_id,
				r.poster_id,
				r.source_release_key,
				r.release_family_key,
				r.file_set_key,
				r.file_family_key,
				r.identity_strength,
				r.identity_reason,
				r.subject_set_token,
				r.subject_set_kind,
				r.family_kind,
				r.base_stem,
				r.is_auxiliary,
				r.is_main_payload,
				r.release_key,
				r.release_name,
				r.binary_key,
				r.binary_name,
				r.file_name,
				r.file_index,
				r.expected_file_count,
				r.total_parts,
				r.posted_at,
				r.match_confidence,
				r.match_status,
				r.grouping_evidence_json,
				NOW()
			FROM ordered_requested r
			ON CONFLICT (provider_id, newsgroup_id, binary_key) DO UPDATE
			SET poster_id = COALESCE(EXCLUDED.poster_id, binaries.poster_id),
			    source_release_key = EXCLUDED.source_release_key,
			    release_family_key = EXCLUDED.release_family_key,
			    file_set_key = EXCLUDED.file_set_key,
			    file_family_key = EXCLUDED.file_family_key,
			    identity_strength = EXCLUDED.identity_strength,
			    identity_reason = EXCLUDED.identity_reason,
			    subject_set_token = EXCLUDED.subject_set_token,
			    subject_set_kind = EXCLUDED.subject_set_kind,
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
			WHERE binaries.poster_id IS DISTINCT FROM COALESCE(EXCLUDED.poster_id, binaries.poster_id)
			   OR binaries.source_release_key IS DISTINCT FROM EXCLUDED.source_release_key
			   OR binaries.release_family_key IS DISTINCT FROM EXCLUDED.release_family_key
			   OR binaries.file_set_key IS DISTINCT FROM EXCLUDED.file_set_key
			   OR binaries.file_family_key IS DISTINCT FROM EXCLUDED.file_family_key
			   OR binaries.identity_strength IS DISTINCT FROM EXCLUDED.identity_strength
			   OR binaries.identity_reason IS DISTINCT FROM EXCLUDED.identity_reason
			   OR binaries.subject_set_token IS DISTINCT FROM EXCLUDED.subject_set_token
			   OR binaries.subject_set_kind IS DISTINCT FROM EXCLUDED.subject_set_kind
			   OR binaries.family_kind IS DISTINCT FROM EXCLUDED.family_kind
			   OR binaries.base_stem IS DISTINCT FROM EXCLUDED.base_stem
			   OR binaries.is_auxiliary IS DISTINCT FROM EXCLUDED.is_auxiliary
			   OR binaries.is_main_payload IS DISTINCT FROM EXCLUDED.is_main_payload
			   OR binaries.release_key IS DISTINCT FROM EXCLUDED.release_key
			   OR binaries.release_name IS DISTINCT FROM EXCLUDED.release_name
			   OR binaries.binary_name IS DISTINCT FROM EXCLUDED.binary_name
			   OR binaries.file_name IS DISTINCT FROM EXCLUDED.file_name
			   OR (EXCLUDED.file_index > 0 AND binaries.file_index IS DISTINCT FROM EXCLUDED.file_index)
			   OR binaries.expected_file_count < EXCLUDED.expected_file_count
			   OR binaries.total_parts < EXCLUDED.total_parts
			   OR (binaries.posted_at IS NULL AND EXCLUDED.posted_at IS NOT NULL)
			   OR binaries.match_confidence < EXCLUDED.match_confidence
			   OR (
			   	EXCLUDED.match_confidence >= binaries.match_confidence
			   	AND (
			   		binaries.match_status IS DISTINCT FROM EXCLUDED.match_status
			   		OR binaries.grouping_evidence_json IS DISTINCT FROM EXCLUDED.grouping_evidence_json
			   	)
			   )
			RETURNING
				id,
				provider_id,
				newsgroup_id,
				binary_key,
				release_family_key,
				base_stem,
				expected_file_count
		) SELECT 1 FROM upserted LIMIT 1`, strings.Join(values, ",")), args...); err != nil {
		return nil, nil, fmt.Errorf("upsert binaries batch: %w", err)
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		WITH requested (
			ordinal,
			provider_id,
			newsgroup_id,
			binary_key
		) AS (
			VALUES %s
		),
		existing AS (
			SELECT
				r.ordinal,
				b.release_family_key AS existing_release_family_key,
				b.base_stem AS existing_base_stem,
				b.expected_file_count AS existing_expected_file_count
			FROM requested r
			JOIN binaries b
			  ON b.provider_id = r.provider_id
			 AND b.newsgroup_id = r.newsgroup_id
			 AND b.binary_key = r.binary_key
		)
		SELECT
			r.ordinal,
			b.id,
			COALESCE(e.existing_release_family_key, ''),
			COALESCE(e.existing_base_stem, ''),
			COALESCE(e.existing_expected_file_count, 0),
			b.release_family_key,
			b.base_stem,
			b.expected_file_count,
			r.provider_id,
			r.newsgroup_id
		FROM requested r
		JOIN binaries b
		  ON b.provider_id = r.provider_id
		 AND b.newsgroup_id = r.newsgroup_id
		 AND b.binary_key = r.binary_key
		LEFT JOIN existing e ON e.ordinal = r.ordinal
		ORDER BY r.ordinal`, persistedValues), persistedArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("query persisted binaries batch: %w", err)
	}
	if telemetry := binaryUpsertTelemetryFromContext(ctx); telemetry != nil {
		telemetry.recordUpsertQueryDuration(time.Since(upsertQueryStarted))
	}
	defer rows.Close()

	ids := make([]int64, len(records))
	summaryKeys := make([]releaseFamilySummaryKey, 0, len(records)*2)
	seenSummaryKeys := make(map[releaseFamilySummaryKey]struct{}, len(records)*2)
	evidenceRecords := make([]binaryEvidenceRecord, 0, len(records))
	for rows.Next() {
		var (
			ordinal                   int
			id                        int64
			existingReleaseFamilyKey  string
			existingBaseStem          string
			existingExpectedFileCount int
			releaseFamilyKey          string
			baseStem                  string
			expectedFileCount         int
			providerID                int64
			newsgroupID               int64
		)
		if err := rows.Scan(
			&ordinal,
			&id,
			&existingReleaseFamilyKey,
			&existingBaseStem,
			&existingExpectedFileCount,
			&releaseFamilyKey,
			&baseStem,
			&expectedFileCount,
			&providerID,
			&newsgroupID,
		); err != nil {
			return nil, nil, fmt.Errorf("scan upserted binary batch row: %w", err)
		}
		if ordinal < 0 || ordinal >= len(records) {
			return nil, nil, fmt.Errorf("upsert binaries batch returned invalid ordinal %d", ordinal)
		}
		ids[ordinal] = id
		evidenceRecords = append(evidenceRecords, binaryEvidenceRecord{
			BinaryID:     id,
			Payload:      records[ordinal].evidenceJSON,
			KeepDetailed: records[ordinal].keepDetailed,
		})

		if existingExpectedFileCount > 0 || existingReleaseFamilyKey != "" || existingBaseStem != "" {
			identityChanged := strings.TrimSpace(existingReleaseFamilyKey) != strings.TrimSpace(releaseFamilyKey)
			existingBaseStemKey := ""
			if existingExpectedFileCount > 1 {
				existingBaseStemKey = strings.ToLower(strings.TrimSpace(existingBaseStem))
			}
			newBaseStemKey := ""
			if expectedFileCount > 1 {
				newBaseStemKey = strings.ToLower(strings.TrimSpace(baseStem))
			}
			identityChanged = identityChanged || existingBaseStemKey != newBaseStemKey
			if identityChanged {
				summaryKeys = appendReleaseFamilySummaryKey(summaryKeys, seenSummaryKeys, providerID, newsgroupID, "release_family", existingReleaseFamilyKey)
				if existingExpectedFileCount > 1 {
					summaryKeys = appendReleaseFamilySummaryKey(summaryKeys, seenSummaryKeys, providerID, newsgroupID, "base_stem", existingBaseStem)
				}
				summaryKeys = appendReleaseFamilySummaryKey(summaryKeys, seenSummaryKeys, providerID, newsgroupID, "release_family", releaseFamilyKey)
				if expectedFileCount > 1 {
					summaryKeys = appendReleaseFamilySummaryKey(summaryKeys, seenSummaryKeys, providerID, newsgroupID, "base_stem", baseStem)
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate upserted binary batch rows: %w", err)
	}
	for i, id := range ids {
		if id <= 0 {
			return nil, nil, fmt.Errorf("upsert binaries batch missing id for ordinal %d", i)
		}
	}

	evidenceStarted := time.Now()
	if err := applyBinaryEvidenceBatch(ctx, tx, evidenceRecords); err != nil {
		return nil, nil, err
	}
	if telemetry := binaryUpsertTelemetryFromContext(ctx); telemetry != nil {
		telemetry.recordEvidenceDuration(time.Since(evidenceStarted))
	}

	return ids, summaryKeys, nil
}

func marshalInlineGroupingEvidence(evidence map[string]any) []byte {
	if len(evidence) == 0 {
		return []byte(`{}`)
	}
	summary, ok := evidence["summary"]
	if !ok {
		return []byte(`{}`)
	}
	raw, err := json.Marshal(map[string]any{"summary": summary})
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}

func shouldPersistDetailedGroupingEvidence(in BinaryRecord, evidence map[string]any) bool {
	if len(evidence) == 0 {
		return false
	}
	if in.MatchConfidence < 0.85 {
		return true
	}

	switch strings.ToLower(strings.TrimSpace(in.IdentityStrength)) {
	case "weak", "provisional":
		return true
	}

	switch strings.ToLower(strings.TrimSpace(in.FamilyKind)) {
	case "contextual_obfuscated", "numeric_obfuscated_set", "opaque_set":
		return true
	}

	summary, _ := evidence["summary"].(map[string]any)
	if status, _ := summary["status"].(string); strings.TrimSpace(strings.ToLower(status)) != "" && strings.TrimSpace(strings.ToLower(status)) != "matched" {
		return true
	}
	if fallbackUsed, _ := summary["fallback_used"].(bool); fallbackUsed {
		return true
	}

	return false
}

func buildUpsertBinaryPersistedValues(records []preparedBinaryRecord) string {
	values := make([]string, 0, len(records))
	for i := range records {
		base := (i * 4) + 1
		values = append(values, fmt.Sprintf("($%d::integer,$%d::bigint,$%d::bigint,$%d::text)", base, base+1, base+2, base+3))
	}
	return strings.Join(values, ",")
}

func buildUpsertBinaryPersistedArgs(records []preparedBinaryRecord) []any {
	args := make([]any, 0, len(records)*4)
	for i, record := range records {
		args = append(args, i, record.record.ProviderID, record.record.NewsgroupID, record.record.BinaryKey)
	}
	return args
}

func applyBinaryEvidenceBatch(ctx context.Context, tx *sql.Tx, records []binaryEvidenceRecord) error {
	if tx == nil {
		return fmt.Errorf("binary grouping evidence tx is required")
	}
	if len(records) == 0 {
		return nil
	}

	deleteIDs := make([]int64, 0, len(records))
	upsertRecords := make([]binaryEvidenceRecord, 0, len(records))
	seenDelete := make(map[int64]struct{}, len(records))
	seenUpsert := make(map[int64]struct{}, len(records))
	for _, record := range records {
		if record.BinaryID <= 0 {
			return fmt.Errorf("binary id is required")
		}
		trimmed := bytes.TrimSpace(record.Payload)
		if !record.KeepDetailed || len(trimmed) == 0 || bytes.Equal(trimmed, []byte(`{}`)) {
			if _, ok := seenDelete[record.BinaryID]; ok {
				continue
			}
			seenDelete[record.BinaryID] = struct{}{}
			deleteIDs = append(deleteIDs, record.BinaryID)
			continue
		}
		if _, ok := seenUpsert[record.BinaryID]; ok {
			continue
		}
		seenUpsert[record.BinaryID] = struct{}{}
		upsertRecords = append(upsertRecords, binaryEvidenceRecord{
			BinaryID:     record.BinaryID,
			Payload:      trimmed,
			KeepDetailed: true,
		})
	}

	if len(deleteIDs) > 0 {
		if err := deleteBinaryGroupingEvidenceBatch(ctx, tx, deleteIDs); err != nil {
			return err
		}
	}
	if len(upsertRecords) > 0 {
		if err := upsertBinaryGroupingEvidenceBatch(ctx, tx, upsertRecords); err != nil {
			return err
		}
	}
	return nil
}

func deleteBinaryGroupingEvidenceBatch(ctx context.Context, tx *sql.Tx, binaryIDs []int64) error {
	if tx == nil {
		return fmt.Errorf("binary grouping evidence tx is required")
	}
	if len(binaryIDs) == 0 {
		return nil
	}
	values := make([]string, 0, len(binaryIDs))
	args := make([]any, 0, len(binaryIDs))
	for i, binaryID := range binaryIDs {
		values = append(values, fmt.Sprintf("($%d::bigint)", i+1))
		args = append(args, binaryID)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		WITH requested(binary_id) AS (
			VALUES %s
		)
		DELETE FROM binary_grouping_evidence bge
		USING requested
		WHERE bge.binary_id = requested.binary_id`, strings.Join(values, ",")), args...); err != nil {
		return fmt.Errorf("delete binary grouping evidence batch size=%d: %w", len(binaryIDs), err)
	}
	return nil
}

func upsertBinaryGroupingEvidenceBatch(ctx context.Context, tx *sql.Tx, records []binaryEvidenceRecord) error {
	if tx == nil {
		return fmt.Errorf("binary grouping evidence tx is required")
	}
	if len(records) == 0 {
		return nil
	}
	values := make([]string, 0, len(records))
	args := make([]any, 0, len(records)*2)
	for i, record := range records {
		base := (i * 2) + 1
		values = append(values, fmt.Sprintf("($%d::bigint,'matcher','v1',$%d::jsonb,NOW())", base, base+1))
		args = append(args, record.BinaryID, record.Payload)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO binary_grouping_evidence (
			binary_id,
			evidence_source,
			evidence_version,
			payload_json,
			updated_at
		)
		VALUES %s
		ON CONFLICT (binary_id) DO UPDATE
		SET payload_json = EXCLUDED.payload_json,
		    updated_at = NOW()
		WHERE binary_grouping_evidence.payload_json IS DISTINCT FROM EXCLUDED.payload_json`, strings.Join(values, ",")), args...); err != nil {
		return fmt.Errorf("upsert binary grouping evidence batch size=%d: %w", len(records), err)
	}
	return nil
}

func upsertBinaryGroupingEvidence(ctx context.Context, tx *sql.Tx, binaryID int64, payload []byte, keepDetailed bool) error {
	if tx == nil {
		return fmt.Errorf("binary grouping evidence tx is required")
	}
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}
	if !keepDetailed || len(bytes.TrimSpace(payload)) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte(`{}`)) {
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
		    updated_at = NOW()
		WHERE binary_grouping_evidence.payload_json IS DISTINCT FROM EXCLUDED.payload_json`,
		binaryID,
		payload,
	); err != nil {
		return fmt.Errorf("upsert binary grouping evidence %d: %w", binaryID, err)
	}
	return nil
}

// CHANGED: add/update one binary part row.
func (s *Store) UpsertBinaryPart(ctx context.Context, in BinaryPartRecord) error {
	return s.UpsertBinaryParts(ctx, []BinaryPartRecord{in})
}

// UpsertBinaryParts adds or updates binary parts in one transaction and marks
// the source article headers assembled with one set-based update.
func (s *Store) UpsertBinaryParts(ctx context.Context, records []BinaryPartRecord) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin binary parts upsert tx: %w", err)
	}
	defer rollbackTx(tx)

	const maxBinaryPartBatchRecords = 8000
	for start := 0; start < len(records); start += maxBinaryPartBatchRecords {
		end := start + maxBinaryPartBatchRecords
		if end > len(records) {
			end = len(records)
		}
		if err := upsertBinaryPartsChunk(ctx, tx, records[start:end]); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit binary parts upsert tx: %w", err)
	}

	return nil
}

func upsertBinaryPartsChunk(ctx context.Context, tx *sql.Tx, records []BinaryPartRecord) error {
	dedupedRecords := dedupeBinaryPartRecords(records)
	sort.Slice(dedupedRecords, func(i, j int) bool {
		if dedupedRecords[i].BinaryID != dedupedRecords[j].BinaryID {
			return dedupedRecords[i].BinaryID < dedupedRecords[j].BinaryID
		}
		if dedupedRecords[i].PartNumber != dedupedRecords[j].PartNumber {
			return dedupedRecords[i].PartNumber < dedupedRecords[j].PartNumber
		}
		return dedupedRecords[i].ArticleHeaderID < dedupedRecords[j].ArticleHeaderID
	})
	headerIDs := uniqueSortedArticleHeaderIDs(records)
	existingBinaryIDs, err := existingBinaryIDsForPartRecords(ctx, tx, dedupedRecords)
	if err != nil {
		return err
	}
	validRecords := make([]BinaryPartRecord, 0, len(dedupedRecords))
	validHeaderIDs := make([]int64, 0, len(headerIDs))
	retryHeaderIDs := make([]int64, 0, 8)
	retrySeen := make(map[int64]struct{}, 8)
	for _, record := range dedupedRecords {
		if _, ok := existingBinaryIDs[record.BinaryID]; ok {
			validRecords = append(validRecords, record)
			continue
		}
		if record.ArticleHeaderID > 0 {
			if _, seen := retrySeen[record.ArticleHeaderID]; !seen {
				retrySeen[record.ArticleHeaderID] = struct{}{}
				retryHeaderIDs = append(retryHeaderIDs, record.ArticleHeaderID)
			}
		}
	}
	for _, articleHeaderID := range headerIDs {
		if _, retry := retrySeen[articleHeaderID]; !retry {
			validHeaderIDs = append(validHeaderIDs, articleHeaderID)
		}
	}
	dedupedRecords = validRecords
	headerIDs = validHeaderIDs
	if len(retryHeaderIDs) > 0 {
		if err := releaseAssemblyClaims(ctx, tx, retryHeaderIDs); err != nil {
			return err
		}
	}
	if len(dedupedRecords) == 0 {
		return nil
	}

	partArgs := make([]any, 0, len(dedupedRecords)*7)
	headerArgs := make([]any, 0, len(headerIDs))
	partValues := make([]string, 0, len(dedupedRecords))
	headerValues := make([]string, 0, len(headerIDs))
	for i, record := range dedupedRecords {
		if record.BinaryID <= 0 || record.ArticleHeaderID <= 0 {
			return fmt.Errorf("binary id and article header id are required")
		}
		if record.PartNumber <= 0 {
			return fmt.Errorf("part number is required")
		}

		partBase := (i * 7) + 1
		partValues = append(partValues, fmt.Sprintf(
			"($%d,$%d,$%d,$%d,$%d,$%d,$%d,NOW())",
			partBase,
			partBase+1,
			partBase+2,
			partBase+3,
			partBase+4,
			partBase+5,
			partBase+6,
		))
		partArgs = append(
			partArgs,
			record.BinaryID,
			record.ArticleHeaderID,
			strings.TrimSpace(record.MessageID),
			record.PartNumber,
			record.TotalParts,
			record.SegmentBytes,
			strings.TrimSpace(record.FileName),
		)
	}
	for i, articleHeaderID := range headerIDs {
		headerValues = append(headerValues, fmt.Sprintf("($%d::bigint)", i+1))
		headerArgs = append(headerArgs, articleHeaderID)
	}

	query := fmt.Sprintf(`
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
		VALUES %s
		ON CONFLICT (binary_id, part_number) DO UPDATE
		SET article_header_id = EXCLUDED.article_header_id,
		    message_id = EXCLUDED.message_id,
		    total_parts = GREATEST(binary_parts.total_parts, EXCLUDED.total_parts),
		    segment_bytes = EXCLUDED.segment_bytes,
		    file_name = EXCLUDED.file_name,
		    updated_at = NOW()`,
		strings.Join(partValues, ","))
	if _, err := tx.ExecContext(ctx, query, partArgs...); err != nil {
		return fmt.Errorf("upsert %d binary parts: %w", len(dedupedRecords), err)
	}

	query = fmt.Sprintf(`
		WITH claimed_headers(id) AS (
			VALUES %s
		)
		UPDATE article_headers
		SET assembled_at = COALESCE(assembled_at, NOW()),
		    assembly_claimed_by = '',
		    assembly_claimed_until = NULL
		FROM claimed_headers
		WHERE article_headers.id = claimed_headers.id`,
		strings.Join(headerValues, ","))
	if _, err := tx.ExecContext(ctx, query, headerArgs...); err != nil {
		return fmt.Errorf("mark %d article headers assembled: %w", len(headerIDs), err)
	}

	return nil
}

func existingBinaryIDsForPartRecords(ctx context.Context, tx *sql.Tx, records []BinaryPartRecord) (map[int64]struct{}, error) {
	if len(records) == 0 {
		return map[int64]struct{}{}, nil
	}
	ids := make([]int64, 0, len(records))
	seen := make(map[int64]struct{}, len(records))
	for _, record := range records {
		if record.BinaryID <= 0 {
			continue
		}
		if _, ok := seen[record.BinaryID]; ok {
			continue
		}
		seen[record.BinaryID] = struct{}{}
		ids = append(ids, record.BinaryID)
	}
	if len(ids) == 0 {
		return map[int64]struct{}{}, nil
	}

	args := make([]any, 0, len(ids))
	values := make([]string, 0, len(ids))
	for i, id := range ids {
		args = append(args, id)
		values = append(values, fmt.Sprintf("($%d::bigint)", i+1))
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		WITH requested(id) AS (
			VALUES %s
		)
		SELECT b.id
		FROM binaries b
		JOIN requested r ON r.id = b.id`, strings.Join(values, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("query existing binary ids for part upsert: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]struct{}, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan existing binary id for part upsert: %w", err)
		}
		out[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing binary ids for part upsert: %w", err)
	}
	return out, nil
}

func releaseAssemblyClaims(ctx context.Context, tx *sql.Tx, articleHeaderIDs []int64) error {
	if len(articleHeaderIDs) == 0 {
		return nil
	}
	args := make([]any, 0, len(articleHeaderIDs))
	values := make([]string, 0, len(articleHeaderIDs))
	for i, id := range articleHeaderIDs {
		args = append(args, id)
		values = append(values, fmt.Sprintf("($%d::bigint)", i+1))
	}
	query := fmt.Sprintf(`
		WITH retry_headers(id) AS (
			VALUES %s
		)
		UPDATE article_headers
		SET assembly_claimed_by = '',
		    assembly_claimed_until = NULL
		FROM retry_headers
		WHERE article_headers.id = retry_headers.id`, strings.Join(values, ","))
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("release %d assembly header claims for retry: %w", len(articleHeaderIDs), err)
	}
	return nil
}

func uniqueSortedArticleHeaderIDs(records []BinaryPartRecord) []int64 {
	if len(records) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(records))
	seen := make(map[int64]struct{}, len(records))
	for _, record := range records {
		if record.ArticleHeaderID <= 0 {
			continue
		}
		if _, ok := seen[record.ArticleHeaderID]; ok {
			continue
		}
		seen[record.ArticleHeaderID] = struct{}{}
		ids = append(ids, record.ArticleHeaderID)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})
	return ids
}

func dedupeBinaryPartRecords(records []BinaryPartRecord) []BinaryPartRecord {
	if len(records) <= 1 {
		return records
	}

	type binaryPartKey struct {
		BinaryID   int64
		PartNumber int
	}

	bestByKey := make(map[binaryPartKey]BinaryPartRecord, len(records))
	order := make([]binaryPartKey, 0, len(records))
	for _, record := range records {
		key := binaryPartKey{BinaryID: record.BinaryID, PartNumber: record.PartNumber}
		existing, ok := bestByKey[key]
		if !ok {
			bestByKey[key] = record
			order = append(order, key)
			continue
		}
		bestByKey[key] = preferBinaryPartRecord(existing, record)
	}

	out := make([]BinaryPartRecord, 0, len(order))
	for _, key := range order {
		out = append(out, bestByKey[key])
	}
	return out
}

func preferBinaryPartRecord(current, candidate BinaryPartRecord) BinaryPartRecord {
	if candidate.SegmentBytes > current.SegmentBytes {
		return candidate
	}
	if candidate.SegmentBytes == current.SegmentBytes {
		if strings.TrimSpace(candidate.MessageID) != "" && strings.TrimSpace(current.MessageID) == "" {
			return candidate
		}
		if strings.TrimSpace(candidate.FileName) != "" && strings.TrimSpace(current.FileName) == "" {
			return candidate
		}
		if candidate.ArticleHeaderID > current.ArticleHeaderID {
			return candidate
		}
	}
	return current
}

// CHANGED: recompute binary aggregate stats after parts were inserted.
func (s *Store) RefreshBinaryStats(ctx context.Context, binaryID int64) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}
	return s.RefreshBinaryStatsBatch(ctx, []int64{binaryID})
}

func (s *Store) RefreshBinaryStatsBatch(ctx context.Context, binaryIDs []int64) error {
	if len(binaryIDs) == 0 {
		return nil
	}

	uniqueBinaryIDs := make([]int64, 0, len(binaryIDs))
	seenBinaryIDs := make(map[int64]struct{}, len(binaryIDs))
	for _, binaryID := range binaryIDs {
		if binaryID <= 0 {
			return fmt.Errorf("binary id is required")
		}
		if _, ok := seenBinaryIDs[binaryID]; ok {
			continue
		}
		seenBinaryIDs[binaryID] = struct{}{}
		uniqueBinaryIDs = append(uniqueBinaryIDs, binaryID)
	}
	sort.Slice(uniqueBinaryIDs, func(i, j int) bool {
		return uniqueBinaryIDs[i] < uniqueBinaryIDs[j]
	})

	for start := 0; start < len(uniqueBinaryIDs); start += refreshBinaryStatsBatchSize {
		end := start + refreshBinaryStatsBatchSize
		if end > len(uniqueBinaryIDs) {
			end = len(uniqueBinaryIDs)
		}
		if err := s.refreshBinaryStatsChunk(ctx, uniqueBinaryIDs[start:end]); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) refreshBinaryStatsChunk(ctx context.Context, binaryIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin refresh binary stats batch tx: %w", err)
	}
	defer rollbackTx(tx)

	statsUpdateStarted := time.Now()
	summaryKeys, err := refreshBinaryStatsIDsInTx(ctx, tx, binaryIDs)
	if err != nil {
		return err
	}
	statsUpdateDuration := time.Since(statsUpdateStarted)
	sortReleaseFamilySummaryKeys(summaryKeys)

	deferredSummaryRefresh := deferReleaseFamilySummaryRefreshFromContext(ctx)
	summaryMarkStarted := time.Now()
	if deferredSummaryRefresh {
		if err := markReleaseFamiliesDirtyBatch(ctx, tx, summaryKeys); err != nil {
			return err
		}
	} else {
		for _, key := range summaryKeys {
			if err := refreshReleaseFamilySummary(ctx, tx, key); err != nil {
				return err
			}
		}
	}
	summaryMarkDuration := time.Since(summaryMarkStarted)

	yencSyncStarted := time.Now()
	if _, _, err := s.syncYEncRecoveryWorkItemsForBinariesInTx(ctx, tx, binaryIDs); err != nil {
		return err
	}
	yencSyncDuration := time.Since(yencSyncStarted)

	if telemetry := binaryStatsRefreshTelemetryFromContext(ctx); telemetry != nil {
		telemetry.recordBatch(len(binaryIDs), len(summaryKeys), deferredSummaryRefresh, statsUpdateDuration, summaryMarkDuration, yencSyncDuration)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit refresh binary stats batch tx: %w", err)
	}
	return nil
}

func refreshBinaryStatsInTx(ctx context.Context, tx *sql.Tx, binaryID int64) ([]releaseFamilySummaryKey, error) {
	return refreshBinaryStatsIDsInTx(ctx, tx, []int64{binaryID})
}

func refreshBinaryStatsIDsInTx(ctx context.Context, tx *sql.Tx, binaryIDs []int64) ([]releaseFamilySummaryKey, error) {
	if len(binaryIDs) == 0 {
		return nil, nil
	}

	var values strings.Builder
	args := make([]any, 0, len(binaryIDs))
	for i, binaryID := range binaryIDs {
		if binaryID <= 0 {
			return nil, fmt.Errorf("binary id is required")
		}
		if i > 0 {
			values.WriteByte(',')
		}
		values.WriteString(fmt.Sprintf("($%d::bigint)", i+1))
		args = append(args, binaryID)
	}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		WITH requested(binary_id) AS (
			VALUES %s
		),
		locked_binaries AS MATERIALIZED (
			SELECT b.id
			FROM binaries b
			JOIN requested r ON r.binary_id = b.id
			ORDER BY b.id
			FOR UPDATE
		),
		part_rows AS MATERIALIZED (
			SELECT
				bp.binary_id,
				bp.segment_bytes,
				bp.article_header_id
			FROM locked_binaries lb
			JOIN binary_parts bp ON bp.binary_id = lb.id
		),
		agg AS (
			SELECT
				p.binary_id,
				COUNT(*)::INTEGER AS observed_parts,
				COALESCE(SUM(p.segment_bytes), 0)::BIGINT AS total_bytes,
				COALESCE(MIN(ah.article_number), 0)::BIGINT AS first_article_number,
				COALESCE(MAX(ah.article_number), 0)::BIGINT AS last_article_number,
				MIN(ah.date_utc) AS posted_at
			FROM part_rows p
			JOIN article_headers ah ON ah.id = p.article_header_id
			GROUP BY p.binary_id
		),
		updated AS (
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
				b.expected_file_count,
				b.expected_archive_file_count
		)
		SELECT
			provider_id,
			newsgroup_id,
			release_family_key,
			base_stem,
			expected_file_count,
			expected_archive_file_count
		FROM updated`, values.String()), args...)
	if err != nil {
		return nil, fmt.Errorf("refresh binary stats batch: %w", err)
	}
	defer rows.Close()

	summaryKeys := make([]releaseFamilySummaryKey, 0, len(binaryIDs)*2)
	seenSummaryKeys := make(map[releaseFamilySummaryKey]struct{}, len(binaryIDs)*2)
	for rows.Next() {
		var (
			providerID               int64
			newsgroupID              int64
			releaseFamilyKey         string
			baseStem                 string
			expectedFileCount        int
			expectedArchiveFileCount int
		)
		if err := rows.Scan(
			&providerID,
			&newsgroupID,
			&releaseFamilyKey,
			&baseStem,
			&expectedFileCount,
			&expectedArchiveFileCount,
		); err != nil {
			return nil, fmt.Errorf("scan refreshed binary stats: %w", err)
		}
		summaryKeys = appendReleaseFamilySummaryKey(summaryKeys, seenSummaryKeys, providerID, newsgroupID, "release_family", releaseFamilyKey)
		if expectedFileCount > 1 {
			summaryKeys = appendReleaseFamilySummaryKey(summaryKeys, seenSummaryKeys, providerID, newsgroupID, "base_stem", baseStem)
		}
		if expectedArchiveFileCount > 1 {
			summaryKeys = appendReleaseFamilySummaryKey(summaryKeys, seenSummaryKeys, providerID, newsgroupID, "base_stem", baseStem)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate refreshed binary stats: %w", err)
	}

	if len(summaryKeys) == 0 {
		return nil, fmt.Errorf("refresh binary stats batch: no binary stats were updated")
	}

	return summaryKeys, nil
}
