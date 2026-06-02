package pgindex

import (
	"context"
	"fmt"
)

type ReleaseNZBGenerateCandidate struct {
	ReleaseID  string
	ProviderID int64
	Title      string
}

func (s *Store) ListReleaseNZBGenerateCandidates(ctx context.Context, limit int, policy ReleaseReadyPolicy) ([]ReleaseNZBGenerateCandidate, error) {
	if limit <= 0 {
		limit = 100
	}
	policy = NormalizeReleaseReadyPolicy(policy)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin generate claim tx: %w", err)
	}
	defer rollbackTx(tx)

	rows, err := tx.QueryContext(ctx, `
		WITH eligible AS (
			SELECT
				r.release_id,
				r.provider_id,
				r.title
			FROM releases r
			LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
			LEFT JOIN release_archive_state ras ON ras.release_id = r.release_id
			WHERE r.source_kind = 'usenet_index'
			  AND EXISTS (SELECT 1 FROM release_files rf WHERE rf.release_id = r.release_id)
			  AND EXISTS (SELECT 1 FROM release_newsgroups rng WHERE rng.release_id = r.release_id)
			  AND COALESCE(ras.archive_status, 'active') IN ('active', 'archive_failed')
			  AND (`+releaseReadyVisibilityClause("r", policy)+`)
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
		return nil, fmt.Errorf("list nzb generate candidates: %w", err)
	}
	defer rows.Close()

	out := make([]ReleaseNZBGenerateCandidate, 0, limit)
	for rows.Next() {
		var item ReleaseNZBGenerateCandidate
		if err := rows.Scan(&item.ReleaseID, &item.ProviderID, &item.Title); err != nil {
			return nil, fmt.Errorf("scan nzb generate candidate: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nzb generate candidates: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit generate claim tx: %w", err)
	}
	return out, nil
}
