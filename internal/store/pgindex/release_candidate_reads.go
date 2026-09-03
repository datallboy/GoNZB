package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type IndexerReleaseCandidateListParams struct {
	Query           string
	Newsgroup       string
	EvaluationState string
	ReadyReason     string
	KeyKind         string
	Sort            string
	Limit           int
	Offset          int
}

type IndexerReleaseCandidateSummary struct {
	SourcePostedAt              time.Time  `json:"source_posted_at"`
	ProviderID                  int64      `json:"provider_id"`
	NewsgroupID                 int64      `json:"newsgroup_id"`
	NewsgroupName               string     `json:"newsgroup_name"`
	KeyKind                     string     `json:"key_kind"`
	FamilyKey                   string     `json:"family_key"`
	SourceReleaseKey            string     `json:"source_release_key"`
	ReleaseKey                  string     `json:"release_key"`
	ReleaseName                 string     `json:"release_name"`
	BinaryCount                 int        `json:"binary_count"`
	CompleteBinaryCount         int        `json:"complete_binary_count"`
	CompleteMainPayloadCount    int        `json:"complete_main_payload_binary_count"`
	ExpectedFileCount           int        `json:"expected_file_count"`
	ExpectedArchiveFileCount    int        `json:"expected_archive_file_count"`
	HasExpectedFileCount        bool       `json:"has_expected_file_count"`
	HasExpectedArchiveFileCount bool       `json:"has_expected_archive_file_count"`
	ExpectedFileCoveragePct     float64    `json:"expected_file_coverage_pct"`
	ArchiveFileCoveragePct      float64    `json:"archive_file_coverage_pct"`
	TotalBytes                  int64      `json:"total_bytes"`
	PostedAt                    *time.Time `json:"posted_at,omitempty"`
	ReadyReason                 string     `json:"ready_reason"`
	RecoverPending              bool       `json:"recover_pending"`
	EvaluationState             string     `json:"evaluation_state"`
	EvaluationNote              string     `json:"evaluation_note"`
	EvaluatedAt                 *time.Time `json:"evaluated_at,omitempty"`
	UpdatedAt                   time.Time  `json:"updated_at"`
	FormedReleaseID             string     `json:"formed_release_id"`
	FormedReleaseTitle          string     `json:"formed_release_title"`
}

func (s *Store) ListIndexerReleaseCandidates(ctx context.Context, params IndexerReleaseCandidateListParams) ([]IndexerReleaseCandidateSummary, int, error) {
	params.Query = strings.TrimSpace(params.Query)
	params.Newsgroup = strings.TrimSpace(params.Newsgroup)
	params.EvaluationState = strings.TrimSpace(strings.ToLower(params.EvaluationState))
	params.ReadyReason = strings.TrimSpace(strings.ToLower(params.ReadyReason))
	params.KeyKind = strings.TrimSpace(strings.ToLower(params.KeyKind))
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Offset < 0 {
		params.Offset = 0
	}

	args := make([]any, 0, 8)
	where := strings.Builder{}
	where.WriteString("WHERE 1=1")
	if params.Query != "" {
		args = append(args, "%"+params.Query+"%")
		fmt.Fprintf(&where, ` AND (
			c.release_name ILIKE $%d OR
			c.release_key ILIKE $%d OR
			c.source_release_key ILIKE $%d OR
			c.family_key ILIKE $%d
		)`, len(args), len(args), len(args), len(args))
	}
	if params.Newsgroup != "" {
		args = append(args, "%"+params.Newsgroup+"%")
		fmt.Fprintf(&where, " AND COALESCE(ng.group_name, '') ILIKE $%d", len(args))
	}
	if params.ReadyReason != "" {
		args = append(args, params.ReadyReason)
		fmt.Fprintf(&where, " AND LOWER(c.ready_reason) = $%d", len(args))
	}
	if params.KeyKind != "" {
		args = append(args, params.KeyKind)
		fmt.Fprintf(&where, " AND LOWER(c.key_kind) = $%d", len(args))
	}
	if params.EvaluationState != "" {
		args = append(args, params.EvaluationState)
		fmt.Fprintf(&where, " AND "+releaseCandidateEvaluationStateSQL()+" = $%d", len(args))
	}

	fromSQL := `
		FROM release_ready_candidates c
		LEFT JOIN newsgroups ng ON ng.id = c.newsgroup_id
		LEFT JOIN release_ready_candidate_acks a
		  ON a.provider_id = c.provider_id
		 AND a.newsgroup_id = c.newsgroup_id
		 AND a.key_kind = c.key_kind
		 AND a.family_key = c.family_key
		LEFT JOIN release_family_readiness_summaries rs
		  ON rs.source_posted_at = c.source_posted_at
		 AND rs.provider_id = c.provider_id
		 AND rs.newsgroup_id = c.newsgroup_id
		 AND rs.key_kind = c.key_kind
		 AND rs.family_key = c.family_key
		LEFT JOIN LATERAL (
			SELECT r.release_id, r.title
			FROM releases r
			WHERE r.source_kind = 'usenet_index'
			  AND r.provider_id = c.provider_id
			  AND (
				r.release_key = c.release_key OR
				r.source_release_key = c.source_release_key OR
				r.release_family_key = c.family_key
			  )
			ORDER BY r.updated_at DESC, r.release_id
			LIMIT 1
		) formed ON TRUE`

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) `+fromSQL+` `+where.String(), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count indexer release candidates: %w", err)
	}

	args = append(args, params.Limit, params.Offset)
	limitArg := len(args) - 1
	offsetArg := len(args)
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			c.source_posted_at,
			c.provider_id,
			c.newsgroup_id,
			COALESCE(ng.group_name, ''),
			c.key_kind,
			c.family_key,
			c.source_release_key,
			c.release_key,
			c.release_name,
			c.binary_count,
			c.complete_binary_count,
			c.complete_main_payload_binary_count,
			c.expected_file_count,
			c.expected_archive_file_count,
			c.has_expected_file_count,
			c.has_expected_archive_file_count,
			c.expected_file_coverage_pct,
			c.archive_file_coverage_pct,
			c.total_bytes,
			c.earliest_posted_at,
			c.ready_reason,
			COALESCE(rs.recover_pending, FALSE),
			`+releaseCandidateEvaluationStateSQL()+`,
			a.processed_at,
			c.updated_at,
			COALESCE(formed.release_id, ''),
			COALESCE(formed.title, '')
		`+fromSQL+` `+where.String()+`
		ORDER BY `+releaseCandidateSortSQL(params.Sort)+`
		LIMIT $`+fmt.Sprint(limitArg)+` OFFSET $`+fmt.Sprint(offsetArg),
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list indexer release candidates: %w", err)
	}
	defer rows.Close()

	items := make([]IndexerReleaseCandidateSummary, 0, params.Limit)
	for rows.Next() {
		var item IndexerReleaseCandidateSummary
		var postedAt sql.NullTime
		var evaluatedAt sql.NullTime
		if err := rows.Scan(
			&item.SourcePostedAt,
			&item.ProviderID,
			&item.NewsgroupID,
			&item.NewsgroupName,
			&item.KeyKind,
			&item.FamilyKey,
			&item.SourceReleaseKey,
			&item.ReleaseKey,
			&item.ReleaseName,
			&item.BinaryCount,
			&item.CompleteBinaryCount,
			&item.CompleteMainPayloadCount,
			&item.ExpectedFileCount,
			&item.ExpectedArchiveFileCount,
			&item.HasExpectedFileCount,
			&item.HasExpectedArchiveFileCount,
			&item.ExpectedFileCoveragePct,
			&item.ArchiveFileCoveragePct,
			&item.TotalBytes,
			&postedAt,
			&item.ReadyReason,
			&item.RecoverPending,
			&item.EvaluationState,
			&evaluatedAt,
			&item.UpdatedAt,
			&item.FormedReleaseID,
			&item.FormedReleaseTitle,
		); err != nil {
			return nil, 0, fmt.Errorf("scan indexer release candidate: %w", err)
		}
		if postedAt.Valid {
			value := postedAt.Time.UTC()
			item.PostedAt = &value
		}
		if evaluatedAt.Valid {
			value := evaluatedAt.Time.UTC()
			item.EvaluatedAt = &value
		}
		item.EvaluationNote = releaseCandidateEvaluationNote(item)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate indexer release candidates: %w", err)
	}
	return items, total, nil
}

func releaseCandidateEvaluationStateSQL() string {
	return `CASE
		WHEN formed.release_id IS NOT NULL THEN 'formed'
		WHEN c.updated_at > COALESCE(a.processed_at, TIMESTAMPTZ 'epoch') THEN 'pending'
		ELSE 'evaluated'
	END`
}

func releaseCandidateSortSQL(sortValue string) string {
	switch strings.TrimSpace(strings.ToLower(sortValue)) {
	case "posted_asc":
		return "c.earliest_posted_at ASC NULLS LAST, c.updated_at DESC, c.family_key"
	case "posted_desc":
		return "c.earliest_posted_at DESC NULLS LAST, c.updated_at DESC, c.family_key"
	case "coverage_desc":
		return "GREATEST(c.expected_file_coverage_pct, c.archive_file_coverage_pct) DESC, c.complete_main_payload_binary_count DESC, c.updated_at DESC"
	case "completion_desc":
		return "c.complete_main_payload_binary_count DESC, c.complete_binary_count DESC, c.binary_count DESC, c.updated_at DESC"
	case "name_asc":
		return "LOWER(c.release_name) ASC, c.updated_at DESC, c.family_key"
	case "updated_asc":
		return "c.updated_at ASC, c.family_key"
	default:
		return "c.updated_at DESC, c.source_posted_at DESC, c.family_key, c.key_kind"
	}
}

func releaseCandidateEvaluationNote(item IndexerReleaseCandidateSummary) string {
	switch item.EvaluationState {
	case "formed":
		return "formed_release"
	case "pending":
		return "awaiting_release_stage"
	}
	if item.RecoverPending {
		return "recovery_pending"
	}
	if item.CompleteMainPayloadCount == 0 {
		return "no_complete_main_payload"
	}
	if item.HasExpectedArchiveFileCount && item.ArchiveFileCoveragePct < 100 {
		return "archive_file_set_incomplete"
	}
	if item.HasExpectedFileCount && item.ExpectedFileCoveragePct < 100 {
		return "expected_file_set_incomplete"
	}
	return "evaluated_not_formed"
}
