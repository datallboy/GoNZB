package pgindex

import (
	"context"
	"fmt"
)

type ReleaseNZBGenerateCandidate struct {
	ReleaseID string
	Title     string
}

func (s *Store) ListReleaseNZBGenerateCandidates(ctx context.Context, limit int, policy ReleaseReadyPolicy) ([]ReleaseNZBGenerateCandidate, error) {
	if limit <= 0 {
		limit = 100
	}
	policy = NormalizeReleaseReadyPolicy(policy)

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.release_id,
			r.title
		FROM releases r
		LEFT JOIN nzb_cache n ON n.release_id = r.release_id
		LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
		WHERE r.source_kind = 'usenet_index'
		  AND COALESCE(n.generation_status, '') <> 'ready'
		  AND EXISTS (SELECT 1 FROM release_files rf WHERE rf.release_id = r.release_id)
		  AND EXISTS (SELECT 1 FROM release_newsgroups rng WHERE rng.release_id = r.release_id)
		  AND COALESCE((
			SELECT ras.archive_status
			FROM release_archive_state ras
			WHERE ras.release_id = r.release_id
		  ), 'active') IN ('active', 'archive_failed')
		  AND (`+releaseReadyVisibilityClause("r", policy)+`)
		ORDER BY r.posted_at DESC NULLS LAST, r.release_id
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list nzb generate candidates: %w", err)
	}
	defer rows.Close()

	out := make([]ReleaseNZBGenerateCandidate, 0, limit)
	for rows.Next() {
		var item ReleaseNZBGenerateCandidate
		if err := rows.Scan(&item.ReleaseID, &item.Title); err != nil {
			return nil, fmt.Errorf("scan nzb generate candidate: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nzb generate candidates: %w", err)
	}
	return out, nil
}
