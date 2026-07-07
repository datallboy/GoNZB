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
	visibilityPolicy := policy
	if visibilityPolicy.RequirePayloadComplete {
		visibilityPolicy.MinCompletionPct = 0
	}
	inspectionClause := "TRUE"
	if policy.RequireInspection {
		inspectionClause = `
			  (
				(
				  EXISTS (
					SELECT 1
					FROM release_files rf
					LEFT JOIN binary_core bc ON bc.binary_id = rf.binary_id
					LEFT JOIN binary_identity_current bic
					  ON bic.source_posted_at = bc.source_posted_at
					 AND bic.binary_id = bc.binary_id
					WHERE rf.release_id = r.release_id
					  AND COALESCE(bic.is_main_payload, TRUE) = TRUE
					  AND (
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.7z' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) ~ '\.7z\.001$' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.zip' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) ~ '\.zip\.001$' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) ~ '\.part0*1\.rar$' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) ~ '\.r00$' OR
						(
							LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.rar' AND
							LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) !~ '\.part\d+\.rar$' AND
							LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) !~ '\.r\d{2,3}$'
						)
					  )
				  )
				  AND EXISTS (
					SELECT 1
					FROM release_files rf
					JOIN binary_inspections bai
					  ON bai.binary_id = rf.binary_id
					 AND bai.stage_name = 'inspect_archive'
					 AND bai.status = 'completed'
					WHERE rf.release_id = r.release_id
				  )
				  AND EXISTS (
					SELECT 1
					FROM release_files rf
					JOIN binary_inspections bmi
					  ON bmi.binary_id = rf.binary_id
					 AND bmi.stage_name = 'inspect_media'
					 AND bmi.status = 'completed'
					WHERE rf.release_id = r.release_id
				  )
				)
				OR (
				  NOT EXISTS (
					SELECT 1
					FROM release_files rf
					LEFT JOIN binary_core bc ON bc.binary_id = rf.binary_id
					LEFT JOIN binary_identity_current bic
					  ON bic.source_posted_at = bc.source_posted_at
					 AND bic.binary_id = bc.binary_id
					WHERE rf.release_id = r.release_id
					  AND COALESCE(bic.is_main_payload, TRUE) = TRUE
					  AND (
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.7z' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) ~ '\.7z\.001$' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.zip' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) ~ '\.zip\.001$' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) ~ '\.part0*1\.rar$' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) ~ '\.r00$' OR
						(
							LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.rar' AND
							LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) !~ '\.part\d+\.rar$' AND
							LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) !~ '\.r\d{2,3}$'
						)
					  )
				  )
				  AND EXISTS (
					SELECT 1
					FROM release_files rf
					LEFT JOIN binary_core bc ON bc.binary_id = rf.binary_id
					LEFT JOIN binary_identity_current bic
					  ON bic.source_posted_at = bc.source_posted_at
					 AND bic.binary_id = bc.binary_id
					JOIN binary_inspections bmi
					  ON bmi.binary_id = rf.binary_id
					 AND bmi.stage_name = 'inspect_media'
					 AND bmi.status = 'completed'
					WHERE rf.release_id = r.release_id
					  AND COALESCE(bic.is_main_payload, TRUE) = TRUE
					  AND (
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.mkv' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.mp4' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.avi' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.ts' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.flac' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.mp3' OR
						LOWER(COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.m4a'
					  )
				  )
				)
			  )`
	}

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
			  AND `+inspectionClause+`
			  AND COALESCE(ras.archive_status, 'active') IN ('active', 'archive_failed')
			  AND (`+releaseReadyVisibilityClause("r", visibilityPolicy)+`)
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
