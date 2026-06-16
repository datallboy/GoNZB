package pgindex

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const (
	assembleLaneARatioNumerator            = 7
	assembleLaneARatioDenominator          = 10
	assemblePriorityBinaryMinScan          = 1000
	assemblePriorityBinaryMaxScan          = 2000
	assemblePriorityBinaryBatch            = 20
	assemblePriorityHeaderWindowMultiplier = 2
	assemblePriorityHeaderMinScan          = 500
	assemblePriorityHeaderMaxScan          = 2000
	assembleClaimStatementTimeout          = 15 * time.Second
	refreshBinaryStatsBatchSize            = 8000
	binaryCompletionKeySyncChunkSize       = 8000
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
	record                      BinaryRecord
	postedAt                    any
	posterID                    any
	evidenceJSON                []byte
	groupingSummaryKind         string
	groupingSummaryStatus       string
	groupingSummaryFallbackUsed bool
	keepDetailed                bool
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
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func normalizeBinaryIdentity(in *BinaryRecord) {
	if in == nil {
		return
	}
	in.SourceReleaseKey = firstNonBlank(in.SourceReleaseKey, in.ReleaseFamilyKey, in.ReleaseKey)
	if shouldDeferPromotableBinaryIdentity(in) {
		in.ReleaseFamilyKey = ""
		in.FileSetKey = ""
		in.BaseStem = ""
	} else {
		in.ReleaseFamilyKey = firstNonBlank(in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey)
	}
	// Keep legacy release_key as a compatibility mirror of release_family_key during cutover.
	in.ReleaseKey = firstNonBlank(in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey)
}

func shouldDeferPromotableBinaryIdentity(in *BinaryRecord) bool {
	if in == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(in.FamilyKind)) {
	case "contextual_obfuscated", "numeric_obfuscated_set", "opaque_set":
	default:
		return false
	}
	switch strings.ToLower(strings.TrimSpace(in.IdentityStrength)) {
	case "weak", "provisional":
	default:
		return false
	}
	if strings.EqualFold(strings.TrimSpace(in.SubjectSetKind), "readable_title") {
		return false
	}
	return !hasPromotableBinaryFileIdentity(in.FileName)
}

func hasPromotableBinaryFileIdentity(fileName string) bool {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	if lower == "" {
		return false
	}
	if strings.HasSuffix(lower, ".rar") || strings.HasSuffix(lower, ".par2") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(lower))
	if len(ext) == 4 && strings.HasPrefix(ext, ".r") && isNumericASCII(ext[2:]) {
		return true
	}
	if isSplitArchivePart(lower) {
		return true
	}
	switch ext {
	case ".mkv", ".mp4", ".avi", ".ts", ".mp3", ".flac", ".m4a", ".zip", ".7z":
		return true
	default:
		return false
	}
}

func isSplitArchivePart(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	if len(ext) != 4 || ext[0] != '.' || !isNumericASCII(ext[1:]) {
		return false
	}
	baseExt := strings.ToLower(filepath.Ext(strings.TrimSuffix(fileName, ext)))
	return baseExt == ".7z" || baseExt == ".zip"
}

func isNumericASCII(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
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

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`SET LOCAL statement_timeout = %d`, assembleClaimStatementTimeout.Milliseconds())); err != nil {
		return nil, fmt.Errorf("set assembly claim statement timeout: %w", err)
	}

	var lockAcquired bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('gonzb-assemble-claim'))`).Scan(&lockAcquired); err != nil {
		return nil, fmt.Errorf("lock assembly claim selector: %w", err)
	}
	if !lockAcquired {
		return nil, nil
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
		priorityIDs, err := s.listPriorityAssemblyHeaderIDsWithFallback(ctx, q, laneALimit, priorityHeaderWindow)
		if err != nil {
			priorityIDs = nil
		} else {
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
	}

	remaining := limit - len(selected)
	if remaining <= 0 {
		return s.hydrateAssemblyCandidates(ctx, q, selected)
	}

	recentIDs, err := s.listRecentUnassembledHeaderIDs(ctx, q, remaining, recentHeaderWindow, selectedIDs, false)
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

func (s *Store) listPriorityAssemblyHeaderIDsWithFallback(ctx context.Context, q assemblyQueryer, limit, pendingWindow int) ([]int64, error) {
	tx, inTx := q.(*sql.Tx)
	if !inTx {
		return s.listPriorityAssemblyHeaderIDs(ctx, q, limit, pendingWindow)
	}

	if _, err := tx.ExecContext(ctx, `SAVEPOINT assembly_priority_selector`); err != nil {
		return nil, err
	}
	out, err := s.listPriorityAssemblyHeaderIDs(ctx, q, limit, pendingWindow)
	if err != nil {
		_, _ = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT assembly_priority_selector`)
		_, _ = tx.ExecContext(ctx, `RELEASE SAVEPOINT assembly_priority_selector`)
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `RELEASE SAVEPOINT assembly_priority_selector`); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) listPriorityAssemblyHeaderIDs(ctx context.Context, q assemblyQueryer, limit, pendingWindow int) ([]int64, error) {
	if limit <= 0 {
		return nil, nil
	}
	if pendingWindow < limit {
		pendingWindow = limit
	}

	rows, err := q.QueryContext(ctx, `
		WITH candidate_binaries AS (
			SELECT
				binary_id,
				provider_id,
				newsgroup_id,
				normalized_file_name,
				is_main_payload,
				observed_parts,
				completion_ratio,
				ROW_NUMBER() OVER () AS binary_rank
			FROM binary_completion_keys
			ORDER BY
				is_main_payload DESC,
				completion_ratio DESC,
				observed_parts DESC,
				binary_id DESC
			LIMIT $2
		),
		selected AS (
			SELECT
				matches.id,
				cb.binary_id,
				cb.is_main_payload,
				cb.observed_parts,
				cb.completion_ratio,
				cb.binary_rank
			FROM candidate_binaries cb
			JOIN LATERAL (
				SELECT ah.id
				FROM article_header_assembly_keys hk
				JOIN article_headers ah
				  ON ah.id = hk.article_header_id
				WHERE hk.provider_id = cb.provider_id
				  AND hk.newsgroup_id = cb.newsgroup_id
				  AND hk.normalized_file_name = cb.normalized_file_name
				  AND ah.assembled_at IS NULL
				  AND (
					ah.assembly_claimed_until IS NULL
					OR ah.assembly_claimed_until < NOW()
				  )
				ORDER BY hk.article_header_id DESC
				LIMIT 1
			) matches ON true
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
				binary_id,
				provider_id,
				newsgroup_id,
				normalized_file_name,
				is_main_payload,
				observed_parts,
				completion_ratio,
				GREATEST(total_parts - observed_parts, 0) AS missing_parts,
				ROW_NUMBER() OVER (
					PARTITION BY provider_id, newsgroup_id, normalized_file_name
					ORDER BY
						is_main_payload DESC,
						completion_ratio DESC,
						observed_parts DESC,
						binary_id DESC
				) AS file_rank
			FROM binary_completion_keys
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
			is_main_payload DESC,
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
				COALESCE(po.poster_name, apr.poster_name, p.poster, '') AS poster,
				ah.date_utc,
				ah.bytes,
				ah.lines,
				p.xref,
				COALESCE(apr.poster_id, p.poster_id, 0) AS poster_id,
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
			LEFT JOIN article_header_poster_refs apr ON apr.article_header_id = p.article_header_id
			LEFT JOIN posters po ON po.id = apr.poster_id
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

	if len(excludeIDs) == 0 && !excludeStructuredMatches {
		rows, err := q.QueryContext(ctx, `
			SELECT ah.id
			FROM article_headers ah
			WHERE ah.assembled_at IS NULL
			  AND (
			  	ah.assembly_claimed_until IS NULL
			  	OR ah.assembly_claimed_until < NOW()
			  )
			ORDER BY ah.id DESC
			LIMIT $1`, limit)
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
		  	JOIN binary_core bc
		  	  ON bc.provider_id = rp.provider_id
		  	 AND bc.newsgroup_id = rp.newsgroup_id
		  	JOIN binary_identity_current bic
		  	  ON bic.binary_id = bc.binary_id
		  	 AND LOWER(BTRIM(COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, '')))) = LOWER(BTRIM(p.subject_file_name))
		  	JOIN binary_observation_stats bos
		  	  ON bos.binary_id = bc.binary_id
		  	 AND bos.total_parts > 0
		  	 AND bos.observed_parts < bos.total_parts
		  	 AND BTRIM(COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''))) <> ''
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
			COALESCE(po.poster_name, apr.poster_name, p.poster, '') AS poster,
			ah.date_utc,
			ah.bytes,
			ah.lines,
			p.xref,
			COALESCE(apr.poster_id, p.poster_id, 0) AS poster_id,
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
		LEFT JOIN article_header_poster_refs apr ON apr.article_header_id = p.article_header_id
		LEFT JOIN posters po ON po.id = apr.poster_id
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
	return s.syncYEncRecoveryWorkItemRetryState(ctx, articleHeaderID)
}

func (s *Store) RecordYEncRecoveryNoop(ctx context.Context, articleHeaderID int64) error {
	if articleHeaderID <= 0 {
		return fmt.Errorf("article header id is required")
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE article_header_ingest_payloads
		SET yenc_recovery_missing_count = article_header_ingest_payloads.yenc_recovery_missing_count + 1,
		    yenc_recovery_last_missing_at = NOW(),
		    yenc_recovery_retry_after = NOW() + CASE
		    	WHEN article_header_ingest_payloads.yenc_recovery_missing_count + 1 = 1 THEN INTERVAL '15 minutes'
		    	WHEN article_header_ingest_payloads.yenc_recovery_missing_count + 1 = 2 THEN INTERVAL '1 hour'
		    	WHEN article_header_ingest_payloads.yenc_recovery_missing_count + 1 = 3 THEN INTERVAL '6 hours'
		    	ELSE INTERVAL '24 hours'
		    END
		WHERE article_header_id = $1`, articleHeaderID,
	)
	if err != nil {
		return fmt.Errorf("record yenc recovery noop for article header %d: %w", articleHeaderID, err)
	}
	return s.syncYEncRecoveryWorkItemRetryState(ctx, articleHeaderID)
}

func (s *Store) syncYEncRecoveryWorkItemRetryState(ctx context.Context, articleHeaderID int64) error {
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
		    lease_owner = '',
		    lease_expires_at = NULL,
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

	prepared := preparedBinaryRecord{record: in, evidenceJSON: []byte(`{}`)}
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
		prepared.groupingSummaryKind, prepared.groupingSummaryStatus, prepared.groupingSummaryFallbackUsed = groupingSummaryScalars(cleanEvidence)
		prepared.keepDetailed = shouldPersistDetailedGroupingEvidence(in, cleanEvidence)
	}

	return prepared, nil
}

func (s *Store) upsertBinaryChunkWithRetries(ctx context.Context, records []preparedBinaryRecord) ([]int64, error) {
	started := time.Now()
	telemetry := binaryUpsertTelemetryFromContext(ctx)
	var lastErr error
	for attempt := 1; attempt <= defaultRetryableTxAttempts; attempt++ {
		ids, err := s.upsertBinaryChunkOnce(ctx, records)
		if err == nil {
			if telemetry != nil {
				telemetry.recordChunk(len(records), attempt-1, time.Since(started))
			}
			return ids, nil
		}
		lastErr = err
		if telemetry != nil && isRetryablePostgresTxError(err) {
			telemetry.recordRetry(err)
		}
		if !isRetryablePostgresTxError(err) || attempt == defaultRetryableTxAttempts {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * defaultRetryableTxDelay):
		}
	}
	return nil, lastErr
}

func (s *Store) upsertBinaryChunkOnce(ctx context.Context, records []preparedBinaryRecord) ([]int64, error) {
	if deferReleaseFamilySummaryRefreshFromContext(ctx) {
		return s.upsertBinaryChunkOnceDeferredCopy(ctx, records)
	}

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

func (s *Store) upsertBinaryChunkOnceDeferredCopy(ctx context.Context, records []preparedBinaryRecord) ([]int64, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire binary upsert conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN`); err != nil {
		return nil, fmt.Errorf("begin binary upsert conn tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	ids, chunkSummaryKeys, err := upsertBinaryChunkWithStage(ctx, conn, records, func() error {
		return stageUpsertBinaryChunkCopy(ctx, conn, records)
	})
	if err != nil {
		return nil, err
	}
	sortReleaseFamilySummaryKeys(chunkSummaryKeys)
	if err := markReleaseFamiliesDirtyBatch(ctx, conn, chunkSummaryKeys); err != nil {
		return nil, err
	}
	if telemetry := binaryUpsertTelemetryFromContext(ctx); telemetry != nil {
		telemetry.recordDeferredSummaryRefresh(len(chunkSummaryKeys))
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, fmt.Errorf("commit binary upsert conn tx: %w", err)
	}
	committed = true
	return ids, nil
}

func upsertBinaryChunk(ctx context.Context, tx *sql.Tx, records []preparedBinaryRecord) ([]int64, []releaseFamilySummaryKey, error) {
	return upsertBinaryChunkWithStage(ctx, tx, records, func() error {
		return stageUpsertBinaryChunk(ctx, tx, records)
	})
}

func upsertBinaryChunkWithStage(ctx context.Context, runner sqlExecQueryer, records []preparedBinaryRecord, stageFn func() error) ([]int64, []releaseFamilySummaryKey, error) {
	if len(records) == 0 {
		return nil, nil, nil
	}

	locks := make([]binaryIdentityLock, 0, len(records))
	for _, record := range records {
		locks = append(locks, binaryIdentityLock{
			ProviderID:  record.record.ProviderID,
			NewsgroupID: record.record.NewsgroupID,
			BinaryKey:   record.record.BinaryKey,
		})
	}
	lockStarted := time.Now()
	if err := lockBinaryIdentityKeys(ctx, runner, locks); err != nil {
		return nil, nil, err
	}
	if telemetry := binaryUpsertTelemetryFromContext(ctx); telemetry != nil {
		telemetry.recordLockDuration(time.Since(lockStarted))
	}

	stageStarted := time.Now()
	if err := stageFn(); err != nil {
		return nil, nil, err
	}
	if telemetry := binaryUpsertTelemetryFromContext(ctx); telemetry != nil {
		telemetry.recordStageDuration(time.Since(stageStarted))
	}
	existingSnapshotStarted := time.Now()
	if err := stageExistingBinaryChunk(ctx, runner); err != nil {
		return nil, nil, err
	}
	if telemetry := binaryUpsertTelemetryFromContext(ctx); telemetry != nil {
		telemetry.recordExistingSnapshotDuration(time.Since(existingSnapshotStarted))
	}

	upsertQueryStarted := time.Now()
	updateStarted := time.Now()
	if _, err := runner.ExecContext(ctx, `
		UPDATE binary_core bc
		SET poster_id = COALESCE(r.poster_id, bc.poster_id),
		    original_binary_name = CASE
		    	WHEN bc.original_binary_name = '' THEN r.binary_name
		    	ELSE bc.original_binary_name
		    END,
		    updated_at = NOW()
		FROM tmp_upsert_binaries r
		JOIN tmp_existing_binaries e ON e.ordinal = r.ordinal
		WHERE bc.binary_id = e.binary_id
		  AND (
		  	bc.poster_id IS DISTINCT FROM COALESCE(r.poster_id, bc.poster_id)
		  	OR (bc.original_binary_name = '' AND r.binary_name <> '')
		  )`); err != nil {
		return nil, nil, fmt.Errorf("update binary_core batch: %w", err)
	}
	if telemetry := binaryUpsertTelemetryFromContext(ctx); telemetry != nil {
		telemetry.recordUpdateDuration(time.Since(updateStarted))
	}
	insertStarted := time.Now()
	if _, err := runner.ExecContext(ctx, `
		INSERT INTO binary_core (
			provider_id,
			newsgroup_id,
			poster_id,
			binary_key,
			original_binary_name,
			created_at,
			updated_at
		)
		SELECT
			r.provider_id,
			r.newsgroup_id,
			r.poster_id,
			r.binary_key,
			r.binary_name,
			NOW(),
			NOW()
		FROM tmp_upsert_binaries r
		LEFT JOIN tmp_existing_binaries e ON e.ordinal = r.ordinal
		WHERE e.binary_id IS NULL
		ORDER BY
			r.provider_id,
			r.newsgroup_id,
			r.binary_key
		ON CONFLICT (provider_id, newsgroup_id, binary_key) DO NOTHING`); err != nil {
		return nil, nil, fmt.Errorf("insert binary_core batch: %w", err)
	}
	if telemetry := binaryUpsertTelemetryFromContext(ctx); telemetry != nil {
		telemetry.recordInsertDuration(time.Since(insertStarted))
	}
	if err := stageExistingBinaryChunk(ctx, runner); err != nil {
		return nil, nil, err
	}
	if _, err := runner.ExecContext(ctx, `
		INSERT INTO binary_observation_stats (
			binary_id,
			provider_id,
			newsgroup_id,
			total_parts,
			observed_parts,
			total_bytes,
			first_article_number,
			last_article_number,
			posted_at,
			refreshed_at,
			updated_at
		)
		SELECT
			e.binary_id,
			r.provider_id,
			r.newsgroup_id,
			r.total_parts,
			0,
			0,
			0,
			0,
			r.posted_at,
			NOW(),
			NOW()
		FROM tmp_upsert_binaries r
		JOIN tmp_existing_binaries e ON e.ordinal = r.ordinal
		ON CONFLICT (binary_id) DO UPDATE
		SET total_parts = GREATEST(binary_observation_stats.total_parts, EXCLUDED.total_parts),
		    posted_at = COALESCE(binary_observation_stats.posted_at, EXCLUDED.posted_at),
		    updated_at = NOW()`); err != nil {
		return nil, nil, fmt.Errorf("upsert binary_observation_stats batch: %w", err)
	}
	if _, err := runner.ExecContext(ctx, `
		INSERT INTO binary_identity_current (
			binary_id,
			provider_id,
			newsgroup_id,
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
			release_key,
			release_name,
			binary_name,
			file_name,
			file_index,
			expected_file_count,
			expected_archive_file_count,
			is_auxiliary,
			is_main_payload,
			match_confidence,
			match_status,
			grouping_summary_kind,
			grouping_summary_status,
			grouping_summary_fallback_used,
			updated_at
		)
		SELECT
			e.binary_id,
			r.provider_id,
			r.newsgroup_id,
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
			r.release_key,
			r.release_name,
			r.binary_name,
			r.file_name,
			r.file_index,
			r.expected_file_count,
			0,
			r.is_auxiliary,
			r.is_main_payload,
			r.match_confidence,
			r.match_status,
			r.grouping_summary_kind,
			r.grouping_summary_status,
			r.grouping_summary_fallback_used,
			NOW()
		FROM tmp_upsert_binaries r
		JOIN tmp_existing_binaries e ON e.ordinal = r.ordinal
		ON CONFLICT (binary_id) DO UPDATE
		SET source_release_key = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.source_release_key ELSE binary_identity_current.source_release_key END,
		    release_family_key = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.release_family_key ELSE binary_identity_current.release_family_key END,
		    file_set_key = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.file_set_key ELSE binary_identity_current.file_set_key END,
		    file_family_key = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.file_family_key ELSE binary_identity_current.file_family_key END,
		    identity_strength = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.identity_strength ELSE binary_identity_current.identity_strength END,
		    identity_reason = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.identity_reason ELSE binary_identity_current.identity_reason END,
		    subject_set_token = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.subject_set_token ELSE binary_identity_current.subject_set_token END,
		    subject_set_kind = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.subject_set_kind ELSE binary_identity_current.subject_set_kind END,
		    family_kind = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.family_kind ELSE binary_identity_current.family_kind END,
		    base_stem = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.base_stem ELSE binary_identity_current.base_stem END,
		    release_key = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.release_key ELSE binary_identity_current.release_key END,
		    release_name = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.release_name ELSE binary_identity_current.release_name END,
		    binary_name = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.binary_name ELSE binary_identity_current.binary_name END,
		    file_name = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.file_name ELSE binary_identity_current.file_name END,
		    file_index = CASE
		    	WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence AND EXCLUDED.file_index > 0 THEN EXCLUDED.file_index
		    	ELSE binary_identity_current.file_index
		    END,
		    expected_file_count = GREATEST(binary_identity_current.expected_file_count, EXCLUDED.expected_file_count),
		    is_auxiliary = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.is_auxiliary ELSE binary_identity_current.is_auxiliary END,
		    is_main_payload = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.is_main_payload ELSE binary_identity_current.is_main_payload END,
		    match_confidence = GREATEST(binary_identity_current.match_confidence, EXCLUDED.match_confidence),
		    match_status = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.match_status ELSE binary_identity_current.match_status END,
		    grouping_summary_kind = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.grouping_summary_kind ELSE binary_identity_current.grouping_summary_kind END,
		    grouping_summary_status = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.grouping_summary_status ELSE binary_identity_current.grouping_summary_status END,
		    grouping_summary_fallback_used = CASE WHEN EXCLUDED.match_confidence >= binary_identity_current.match_confidence THEN EXCLUDED.grouping_summary_fallback_used ELSE binary_identity_current.grouping_summary_fallback_used END,
		    updated_at = NOW()`); err != nil {
		return nil, nil, fmt.Errorf("upsert binary_identity_current batch: %w", err)
	}
	if _, err := runner.ExecContext(ctx, `
		INSERT INTO binary_recovery_current (
			binary_id,
			provider_id,
			newsgroup_id,
			updated_at
		)
		SELECT
			e.binary_id,
			r.provider_id,
			r.newsgroup_id,
			NOW()
		FROM tmp_upsert_binaries r
		JOIN tmp_existing_binaries e ON e.ordinal = r.ordinal
		ON CONFLICT (binary_id) DO NOTHING`); err != nil {
		return nil, nil, fmt.Errorf("upsert binary_recovery_current seed batch: %w", err)
	}
	if _, err := runner.ExecContext(ctx, `
		INSERT INTO binary_lifecycle (
			binary_id,
			provider_id,
			newsgroup_id,
			lifecycle_status,
			updated_at
		)
		SELECT
			e.binary_id,
			r.provider_id,
			r.newsgroup_id,
			'active',
			NOW()
	FROM tmp_upsert_binaries r
		JOIN tmp_existing_binaries e ON e.ordinal = r.ordinal
		ON CONFLICT (binary_id) DO NOTHING`); err != nil {
		return nil, nil, fmt.Errorf("upsert binary_lifecycle seed batch: %w", err)
	}
	if err := syncBinaryCompletionKeysForStagedBinaries(ctx, runner); err != nil {
		return nil, nil, err
	}
	readbackStarted := time.Now()
	rows, err := runner.QueryContext(ctx, `
		SELECT
			e.ordinal,
			e.binary_id,
			e.existing_release_family_key,
			e.existing_base_stem,
			e.existing_expected_file_count,
			bic.release_family_key,
			bic.base_stem,
			bic.expected_file_count,
			r.provider_id,
			r.newsgroup_id
		FROM tmp_existing_binaries e
		JOIN tmp_upsert_binaries r ON r.ordinal = e.ordinal
		JOIN binary_identity_current bic ON bic.binary_id = e.binary_id
		ORDER BY e.ordinal`)
	if err != nil {
		return nil, nil, fmt.Errorf("query persisted binaries batch: %w", err)
	}
	if telemetry := binaryUpsertTelemetryFromContext(ctx); telemetry != nil {
		telemetry.recordReadbackDuration(time.Since(readbackStarted))
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
	if err := applyBinaryEvidenceBatch(ctx, runner, evidenceRecords); err != nil {
		return nil, nil, err
	}
	if telemetry := binaryUpsertTelemetryFromContext(ctx); telemetry != nil {
		telemetry.recordEvidenceDuration(time.Since(evidenceStarted))
	}

	return ids, summaryKeys, nil
}

func stageUpsertBinaryChunk(ctx context.Context, tx *sql.Tx, records []preparedBinaryRecord) error {
	if tx == nil {
		return fmt.Errorf("binary upsert tx is required")
	}
	if len(records) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_upsert_binaries (
			ordinal INTEGER NOT NULL,
			provider_id BIGINT NOT NULL,
			newsgroup_id BIGINT NOT NULL,
			poster_id BIGINT NULL,
			source_release_key TEXT NOT NULL,
			release_family_key TEXT NOT NULL,
			file_set_key TEXT NOT NULL,
			file_family_key TEXT NOT NULL,
			identity_strength TEXT NOT NULL,
			identity_reason TEXT NOT NULL,
			subject_set_token TEXT NOT NULL,
			subject_set_kind TEXT NOT NULL,
			family_kind TEXT NOT NULL,
			base_stem TEXT NOT NULL,
			is_auxiliary BOOLEAN NOT NULL,
			is_main_payload BOOLEAN NOT NULL,
			release_key TEXT NOT NULL,
			release_name TEXT NOT NULL,
			binary_key TEXT NOT NULL,
			binary_name TEXT NOT NULL,
			file_name TEXT NOT NULL,
			file_index INTEGER NOT NULL,
			expected_file_count INTEGER NOT NULL,
			total_parts INTEGER NOT NULL,
			posted_at TIMESTAMPTZ NULL,
			match_confidence DOUBLE PRECISION NOT NULL,
			match_status TEXT NOT NULL,
			grouping_summary_kind TEXT NOT NULL,
			grouping_summary_status TEXT NOT NULL,
			grouping_summary_fallback_used BOOLEAN NOT NULL,
			grouping_evidence_payload JSONB NOT NULL
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create binary upsert temp table: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO tmp_upsert_binaries (
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
			grouping_summary_kind,
			grouping_summary_status,
			grouping_summary_fallback_used,
			grouping_evidence_payload
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,
			$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
			$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31
		)`)
	if err != nil {
		return fmt.Errorf("prepare binary upsert temp insert: %w", err)
	}
	defer stmt.Close()

	for i, record := range records {
		if _, err := stmt.ExecContext(ctx,
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
			record.groupingSummaryKind,
			record.groupingSummaryStatus,
			record.groupingSummaryFallbackUsed,
			record.evidenceJSON,
		); err != nil {
			return fmt.Errorf("insert binary upsert temp row: %w", err)
		}
	}
	return nil
}

func stageUpsertBinaryChunkCopy(ctx context.Context, conn *sql.Conn, records []preparedBinaryRecord) error {
	if conn == nil {
		return fmt.Errorf("binary upsert conn is required")
	}
	if len(records) == 0 {
		return nil
	}
	if _, err := conn.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_upsert_binaries (
			ordinal INTEGER NOT NULL,
			provider_id BIGINT NOT NULL,
			newsgroup_id BIGINT NOT NULL,
			poster_id BIGINT NULL,
			source_release_key TEXT NOT NULL,
			release_family_key TEXT NOT NULL,
			file_set_key TEXT NOT NULL,
			file_family_key TEXT NOT NULL,
			identity_strength TEXT NOT NULL,
			identity_reason TEXT NOT NULL,
			subject_set_token TEXT NOT NULL,
			subject_set_kind TEXT NOT NULL,
			family_kind TEXT NOT NULL,
			base_stem TEXT NOT NULL,
			is_auxiliary BOOLEAN NOT NULL,
			is_main_payload BOOLEAN NOT NULL,
			release_key TEXT NOT NULL,
			release_name TEXT NOT NULL,
			binary_key TEXT NOT NULL,
			binary_name TEXT NOT NULL,
			file_name TEXT NOT NULL,
			file_index INTEGER NOT NULL,
			expected_file_count INTEGER NOT NULL,
			total_parts INTEGER NOT NULL,
			posted_at TIMESTAMPTZ NULL,
			match_confidence DOUBLE PRECISION NOT NULL,
			match_status TEXT NOT NULL,
			grouping_summary_kind TEXT NOT NULL,
			grouping_summary_status TEXT NOT NULL,
			grouping_summary_fallback_used BOOLEAN NOT NULL,
			grouping_evidence_payload JSONB NOT NULL
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create binary upsert temp table: %w", err)
	}

	err := conn.Raw(func(driverConn any) error {
		pgxConn := driverConn.(*stdlib.Conn).Conn()
		rows := pgx.CopyFromSlice(len(records), func(i int) ([]any, error) {
			record := records[i]
			return []any{
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
				record.groupingSummaryKind,
				record.groupingSummaryStatus,
				record.groupingSummaryFallbackUsed,
				record.evidenceJSON,
			}, nil
		})
		_, err := pgxConn.CopyFrom(ctx,
			pgx.Identifier{"tmp_upsert_binaries"},
			[]string{
				"ordinal",
				"provider_id",
				"newsgroup_id",
				"poster_id",
				"source_release_key",
				"release_family_key",
				"file_set_key",
				"file_family_key",
				"identity_strength",
				"identity_reason",
				"subject_set_token",
				"subject_set_kind",
				"family_kind",
				"base_stem",
				"is_auxiliary",
				"is_main_payload",
				"release_key",
				"release_name",
				"binary_key",
				"binary_name",
				"file_name",
				"file_index",
				"expected_file_count",
				"total_parts",
				"posted_at",
				"match_confidence",
				"match_status",
				"grouping_summary_kind",
				"grouping_summary_status",
				"grouping_summary_fallback_used",
				"grouping_evidence_payload",
			},
			rows,
		)
		if err != nil {
			return fmt.Errorf("copy binary upsert temp rows: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func stageExistingBinaryChunk(ctx context.Context, runner sqlExecQueryer) error {
	if runner == nil {
		return fmt.Errorf("binary upsert tx is required")
	}
	_, _ = runner.ExecContext(ctx, `DROP TABLE IF EXISTS tmp_existing_binaries`)
	if _, err := runner.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_existing_binaries ON COMMIT DROP AS
		SELECT
			r.ordinal,
			bc.binary_id,
			COALESCE(bic.release_family_key, '') AS existing_release_family_key,
			COALESCE(bic.base_stem, '') AS existing_base_stem,
			COALESCE(bic.expected_file_count, 0) AS existing_expected_file_count
		FROM tmp_upsert_binaries r
		JOIN binary_core bc
		  ON bc.provider_id = r.provider_id
		 AND bc.newsgroup_id = r.newsgroup_id
		 AND bc.binary_key = r.binary_key
		LEFT JOIN binary_identity_current bic ON bic.binary_id = bc.binary_id`); err != nil {
		return fmt.Errorf("stage existing binary upsert rows: %w", err)
	}
	if _, err := runner.ExecContext(ctx, `
		CREATE UNIQUE INDEX tmp_existing_binaries_ordinal_idx
		ON tmp_existing_binaries (ordinal)`); err != nil {
		return fmt.Errorf("index existing binary upsert rows: %w", err)
	}
	return nil
}

func syncBinaryCompletionKeysForStagedBinaries(ctx context.Context, runner sqlExecQueryer) error {
	if runner == nil {
		return fmt.Errorf("binary completion key runner is required")
	}
	if _, err := runner.ExecContext(ctx, `
		DELETE FROM binary_completion_keys bck
		USING tmp_existing_binaries e
		WHERE bck.binary_id = e.binary_id`); err != nil {
		return fmt.Errorf("delete staged binary completion keys: %w", err)
	}
	if _, err := runner.ExecContext(ctx, `
		INSERT INTO binary_completion_keys (
			binary_id,
			provider_id,
			newsgroup_id,
			normalized_file_name,
			is_main_payload,
			observed_parts,
			total_parts,
			completion_ratio,
			updated_at
		)
		SELECT
			bic.binary_id,
			bic.provider_id,
			bic.newsgroup_id,
			lower(btrim(coalesce(nullif(bic.file_name, ''), nullif(bic.binary_name, '')))),
			bic.is_main_payload,
			bos.observed_parts,
			bos.total_parts,
			CASE
				WHEN bos.total_parts > 0 THEN bos.observed_parts::double precision / bos.total_parts::double precision
				ELSE 0
			END,
			NOW()
		FROM tmp_existing_binaries e
		JOIN binary_identity_current bic ON bic.binary_id = e.binary_id
		JOIN binary_observation_stats bos ON bos.binary_id = e.binary_id
		WHERE bos.total_parts > 0
		  AND bos.observed_parts < bos.total_parts
		  AND btrim(coalesce(nullif(bic.file_name, ''), nullif(bic.binary_name, ''))) <> ''
		ON CONFLICT (binary_id) DO UPDATE
		SET provider_id = EXCLUDED.provider_id,
		    newsgroup_id = EXCLUDED.newsgroup_id,
		    normalized_file_name = EXCLUDED.normalized_file_name,
		    is_main_payload = EXCLUDED.is_main_payload,
		    observed_parts = EXCLUDED.observed_parts,
		    total_parts = EXCLUDED.total_parts,
		    completion_ratio = EXCLUDED.completion_ratio,
		    updated_at = NOW()`); err != nil {
		return fmt.Errorf("upsert staged binary completion keys: %w", err)
	}
	return nil
}

func syncBinaryCompletionKeysForBinaryIDsInTx(ctx context.Context, tx *sql.Tx, binaryIDs []int64) error {
	if tx == nil {
		return fmt.Errorf("binary completion key tx is required")
	}
	if len(binaryIDs) == 0 {
		return nil
	}

	unique := make([]int64, 0, len(binaryIDs))
	seen := make(map[int64]struct{}, len(binaryIDs))
	for _, binaryID := range binaryIDs {
		if binaryID <= 0 {
			continue
		}
		if _, ok := seen[binaryID]; ok {
			continue
		}
		seen[binaryID] = struct{}{}
		unique = append(unique, binaryID)
	}
	if len(unique) == 0 {
		return nil
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })

	for start := 0; start < len(unique); start += binaryCompletionKeySyncChunkSize {
		end := start + binaryCompletionKeySyncChunkSize
		if end > len(unique) {
			end = len(unique)
		}
		if err := syncBinaryCompletionKeyChunkInTx(ctx, tx, unique[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func syncBinaryCompletionKeyChunkInTx(ctx context.Context, tx *sql.Tx, binaryIDs []int64) error {
	if len(binaryIDs) == 0 {
		return nil
	}

	var values strings.Builder
	args := make([]any, 0, len(binaryIDs))
	for _, binaryID := range binaryIDs {
		if values.Len() > 0 {
			values.WriteByte(',')
		}
		fmt.Fprintf(&values, "($%d::bigint)", len(args)+1)
		args = append(args, binaryID)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		WITH requested(binary_id) AS (
			VALUES %s
		)
		DELETE FROM binary_completion_keys bck
		USING requested r
		WHERE bck.binary_id = r.binary_id`, values.String()), args...); err != nil {
		return fmt.Errorf("delete binary completion keys: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		WITH requested(binary_id) AS (
			VALUES %s
		)
		INSERT INTO binary_completion_keys (
			binary_id,
			provider_id,
			newsgroup_id,
			normalized_file_name,
			is_main_payload,
			observed_parts,
			total_parts,
			completion_ratio,
			updated_at
		)
		SELECT
			bic.binary_id,
			bic.provider_id,
			bic.newsgroup_id,
			lower(btrim(coalesce(nullif(bic.file_name, ''), nullif(bic.binary_name, '')))),
			bic.is_main_payload,
			bos.observed_parts,
			bos.total_parts,
			CASE
				WHEN bos.total_parts > 0 THEN bos.observed_parts::double precision / bos.total_parts::double precision
				ELSE 0
			END,
			NOW()
		FROM requested r
		JOIN binary_identity_current bic ON bic.binary_id = r.binary_id
		JOIN binary_observation_stats bos ON bos.binary_id = r.binary_id
		WHERE bos.total_parts > 0
		  AND bos.observed_parts < bos.total_parts
		  AND btrim(coalesce(nullif(bic.file_name, ''), nullif(bic.binary_name, ''))) <> ''
		ON CONFLICT (binary_id) DO UPDATE
		SET provider_id = EXCLUDED.provider_id,
		    newsgroup_id = EXCLUDED.newsgroup_id,
		    normalized_file_name = EXCLUDED.normalized_file_name,
		    is_main_payload = EXCLUDED.is_main_payload,
		    observed_parts = EXCLUDED.observed_parts,
		    total_parts = EXCLUDED.total_parts,
		    completion_ratio = EXCLUDED.completion_ratio,
		    updated_at = NOW()`, values.String()), args...); err != nil {
		return fmt.Errorf("upsert binary completion keys: %w", err)
	}
	return nil
}

func groupingSummaryScalars(evidence map[string]any) (kind, status string, fallbackUsed bool) {
	if len(evidence) == 0 {
		return "", "", false
	}
	summary, ok := evidence["summary"].(map[string]any)
	if !ok {
		return "", "", false
	}
	kind, _ = summary["kind"].(string)
	status, _ = summary["status"].(string)
	fallbackUsed, _ = summary["fallback_used"].(bool)
	return strings.TrimSpace(kind), strings.TrimSpace(status), fallbackUsed
}

func shouldPersistDetailedGroupingEvidence(_ BinaryRecord, _ map[string]any) bool {
	// Detailed matcher traces are intentionally not retained in PostgreSQL by
	// default. The compact inline summary is enough for release formation and
	// admin review, while the full per-binary JSONB payload created excessive
	// write amplification on long indexer runs.
	return false
}

func applyBinaryEvidenceBatch(ctx context.Context, runner sqlExecQueryer, records []binaryEvidenceRecord) error {
	if runner == nil {
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
		if err := deleteBinaryGroupingEvidenceBatch(ctx, runner, deleteIDs); err != nil {
			return err
		}
	}
	if len(upsertRecords) > 0 {
		if err := upsertBinaryGroupingEvidenceBatch(ctx, runner, upsertRecords); err != nil {
			return err
		}
	}
	return nil
}

func deleteBinaryGroupingEvidenceBatch(ctx context.Context, runner sqlExecQueryer, binaryIDs []int64) error {
	if runner == nil {
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
	if _, err := runner.ExecContext(ctx, fmt.Sprintf(`
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

func upsertBinaryGroupingEvidenceBatch(ctx context.Context, runner sqlExecQueryer, records []binaryEvidenceRecord) error {
	if runner == nil {
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
	if _, err := runner.ExecContext(ctx, fmt.Sprintf(`
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

	return retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
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
	})
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
	query = fmt.Sprintf(`
		WITH claimed_headers(id) AS (
			VALUES %s
		)
		DELETE FROM article_header_assembly_keys hk
		USING claimed_headers
		WHERE hk.article_header_id = claimed_headers.id`,
		strings.Join(headerValues, ","))
	if _, err := tx.ExecContext(ctx, query, headerArgs...); err != nil {
		return fmt.Errorf("complete %d article header assembly keys: %w", len(headerIDs), err)
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
		SELECT bc.binary_id
		FROM binary_core bc
		JOIN requested r ON r.id = bc.binary_id`, strings.Join(values, ",")), args...)
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

	if err := syncBinaryCompletionKeysForBinaryIDsInTx(ctx, tx, binaryIDs); err != nil {
		return err
	}

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
			SELECT bos.binary_id
			FROM binary_observation_stats bos
			JOIN requested r ON r.binary_id = bos.binary_id
			ORDER BY bos.binary_id
			FOR UPDATE OF bos
		),
		part_rows AS MATERIALIZED (
			SELECT
				bp.binary_id,
				bp.segment_bytes,
				bp.article_header_id
			FROM locked_binaries lb
			JOIN binary_parts bp ON bp.binary_id = lb.binary_id
		),
		part_rows_with_headers AS MATERIALIZED (
			SELECT
				p.binary_id,
				p.segment_bytes,
				ah.article_number,
				ah.date_utc
			FROM part_rows p
			JOIN article_headers ah ON ah.id = p.article_header_id
		),
		agg AS (
			SELECT
				p.binary_id,
				COUNT(*)::INTEGER AS observed_parts,
				COALESCE(SUM(p.segment_bytes), 0)::BIGINT AS total_bytes,
				COALESCE(MIN(p.article_number), 0)::BIGINT AS first_article_number,
				COALESCE(MAX(p.article_number), 0)::BIGINT AS last_article_number,
				MIN(p.date_utc) AS posted_at
			FROM part_rows_with_headers p
			GROUP BY p.binary_id
		),
		updated AS (
			UPDATE binary_observation_stats bos
			SET observed_parts = agg.observed_parts,
			    total_bytes = agg.total_bytes,
			    first_article_number = agg.first_article_number,
			    last_article_number = agg.last_article_number,
			    posted_at = COALESCE(agg.posted_at, bos.posted_at),
			    refreshed_at = NOW(),
			    updated_at = NOW()
			FROM agg
			WHERE bos.binary_id = agg.binary_id
			RETURNING
				bos.binary_id,
				bos.provider_id,
				bos.newsgroup_id
		)
		SELECT
			u.provider_id,
			u.newsgroup_id,
			COALESCE(bic.release_family_key, ''),
			COALESCE(bic.base_stem, ''),
			COALESCE(bic.expected_file_count, 0),
			COALESCE(bic.expected_archive_file_count, 0)
		FROM updated u
		JOIN binary_identity_current bic ON bic.binary_id = u.binary_id`, values.String()), args...)
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
		// Some lane-B binaries can legitimately exist before they have enough
		// identity to materialize release-family/base-stem summary keys. The
		// aggregate stats update is still valid in that case, so do not fail the
		// whole batch just because no summary keys were derivable yet.
		return nil, nil
	}

	return summaryKeys, nil
}
