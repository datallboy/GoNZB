package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	yencRecoverySeedScanLowYieldThreshold = 25
	yencRecoveryReadyWindowMultiplier     = 8
	yencRecoveryReadyWindowMax            = 8000
)

func yencRecoveryReadyWindowLimit(limit int) int {
	if limit <= 0 {
		return yencRecoverySeedScanLowYieldThreshold
	}
	window := limit * yencRecoveryReadyWindowMultiplier
	if window < limit {
		window = limit
	}
	if window > yencRecoveryReadyWindowMax {
		window = yencRecoveryReadyWindowMax
	}
	return window
}

func (s *Store) ListYEncRecoveryCandidates(ctx context.Context, limit int) ([]YEncRecoveryCandidate, error) {
	if limit <= 0 {
		limit = 100
	}

	if _, err := s.retireStaleReadyYEncRecoveryWorkItems(ctx); err != nil {
		return nil, err
	}

	readyCount, err := s.countReadyYEncRecoveryCandidates(ctx, limit)
	if err != nil {
		return nil, err
	}
	if readyCount > yencRecoverySeedScanLowYieldThreshold {
		s.clearYEncRecoverySeedScanBackoff()
	}
	if readyCount == 0 {
		if _, _, seedErr := s.maybeBackfillYEncRecoveryWorkItems(ctx, limit); seedErr != nil {
			return nil, seedErr
		}
	}

	return s.listReadyYEncRecoveryCandidates(ctx, limit)
}

func (s *Store) maybeBackfillYEncRecoveryWorkItems(ctx context.Context, limit int) (int64, int64, error) {
	if limit <= 0 {
		limit = yencRecoveryWorkItemSeedLimit
	}
	if s.shouldBackoffYEncRecoverySeedScan(time.Now()) {
		return 0, 0, nil
	}
	readyCount, err := s.countReadyYEncRecoveryCandidates(ctx, limit)
	if err != nil {
		return 0, 0, err
	}
	if readyCount > 0 {
		if readyCount > yencRecoverySeedScanLowYieldThreshold {
			s.clearYEncRecoverySeedScanBackoff()
		}
		return 0, 0, nil
	}
	seedLimit := limit
	if seedLimit > yencRecoveryWorkItemSeedLimit {
		seedLimit = yencRecoveryWorkItemSeedLimit
	}
	upserted, retired, err := s.BackfillYEncRecoveryWorkItems(ctx, seedLimit)
	if err != nil {
		return 0, 0, err
	}
	s.recordYEncRecoverySeedScanResult(time.Now(), readyCount, upserted)
	return upserted, retired, nil
}

func (s *Store) shouldBackoffYEncRecoverySeedScan(now time.Time) bool {
	s.yencSeedScanMu.Lock()
	defer s.yencSeedScanMu.Unlock()
	return !s.yencSeedScanBackoffUntil.IsZero() && now.Before(s.yencSeedScanBackoffUntil)
}

func (s *Store) clearYEncRecoverySeedScanBackoff() {
	s.yencSeedScanMu.Lock()
	defer s.yencSeedScanMu.Unlock()
	s.yencSeedScanConsecutiveEmpty = 0
	s.yencSeedScanBackoffUntil = time.Time{}
}

func (s *Store) recordYEncRecoverySeedScanResult(now time.Time, priorReadyCount int, upserted int64) {
	s.yencSeedScanMu.Lock()
	defer s.yencSeedScanMu.Unlock()

	if priorReadyCount > yencRecoverySeedScanLowYieldThreshold || upserted > yencRecoverySeedScanLowYieldThreshold {
		s.yencSeedScanConsecutiveEmpty = 0
		s.yencSeedScanBackoffUntil = time.Time{}
		return
	}

	s.yencSeedScanConsecutiveEmpty++
	var backoff time.Duration
	switch s.yencSeedScanConsecutiveEmpty {
	case 1:
		backoff = 1 * time.Minute
	case 2:
		backoff = 5 * time.Minute
	default:
		backoff = 15 * time.Minute
	}
	s.yencSeedScanBackoffUntil = now.Add(backoff)
}

func (s *Store) retireStaleReadyYEncRecoveryWorkItems(ctx context.Context) (int64, error) {
	var retired int64
	if err := s.db.QueryRowContext(ctx, `
		WITH stale AS (
			SELECT wi.binary_id
			FROM yenc_recovery_work_items wi
			WHERE wi.status IN ('ready', 'running')
			  AND BTRIM(COALESCE(wi.message_id, '')) = ''
			ORDER BY wi.updated_at
			LIMIT 5000
		),
		retired AS (
			UPDATE yenc_recovery_work_items wi
			SET status = 'stale',
			    updated_at = NOW()
			FROM stale s
			WHERE wi.binary_id = s.binary_id
			RETURNING 1
		)
		SELECT COUNT(*) FROM retired`,
	).Scan(&retired); err != nil {
		return 0, fmt.Errorf("retire stale ready yenc recovery work items: %w", err)
	}
	return retired, nil
}

func (s *Store) countReadyYEncRecoveryCandidates(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}

	var count int
	windowLimit := yencRecoveryReadyWindowLimit(limit)
	if err := s.withParallelGatherDisabledTx(ctx, true, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `
			WITH ready_window AS (
				SELECT wi.binary_id
				FROM yenc_recovery_work_items wi
				WHERE wi.status = 'ready'
				  AND wi.ready_at <= NOW()
				  AND BTRIM(COALESCE(wi.message_id, '')) <> ''
				ORDER BY wi.priority_rank, wi.updated_at DESC, wi.binary_id
				LIMIT $1
			)
			SELECT COUNT(*) FROM (SELECT 1 FROM ready_window LIMIT $2) ready`,
			windowLimit,
			limit,
		).Scan(&count)
	}); err != nil {
		return 0, fmt.Errorf("count ready yenc recovery candidates: %w", err)
	}
	return count, nil
}

func (s *Store) listReadyYEncRecoveryCandidates(ctx context.Context, limit int) ([]YEncRecoveryCandidate, error) {
	windowLimit := yencRecoveryReadyWindowLimit(limit)
	var out []YEncRecoveryCandidate
	err := s.withParallelGatherDisabledTx(ctx, false, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			WITH expired AS (
				UPDATE yenc_recovery_work_items wi
				SET status = 'ready',
				    lease_owner = '',
				    lease_expires_at = NULL,
				    updated_at = NOW()
				WHERE wi.status = 'running'
				  AND wi.lease_expires_at <= NOW()
				RETURNING wi.binary_id
			),
			ready_window AS (
				SELECT
					wi.binary_id,
					wi.article_header_id,
					wi.provider_id,
					wi.newsgroup_id,
					wi.newsgroup_name,
					wi.article_number,
					wi.message_id,
					wi.subject,
					wi.poster,
					wi.date_utc,
					wi.article_bytes,
					wi.article_lines,
					wi.xref,
					wi.subject_file_name,
					wi.subject_file_index,
					wi.subject_file_total,
					wi.yenc_part_number,
					wi.yenc_total_parts,
					wi.yenc_file_size,
					wi.missing_count,
					wi.ready_at,
					wi.current_binary_key,
					wi.current_release_family_key,
					wi.current_base_stem,
					wi.current_readiness_bucket,
					wi.structured_identity_binary_matched,
					wi.priority_rank,
					wi.updated_at
				FROM yenc_recovery_work_items wi
				WHERE wi.status = 'ready'
				  AND wi.ready_at <= NOW()
				  AND BTRIM(COALESCE(wi.message_id, '')) <> ''
				ORDER BY wi.priority_rank, wi.updated_at DESC, wi.binary_id
				LIMIT $2
				FOR UPDATE SKIP LOCKED
			),
			ranked AS (
				SELECT
					rw.binary_id,
					ROW_NUMBER() OVER (
						PARTITION BY rw.provider_id, rw.newsgroup_id, rw.priority_rank
						ORDER BY rw.updated_at DESC, rw.binary_id
					) AS group_rank
				FROM ready_window rw
			),
			selected AS (
				SELECT rw.*, r.group_rank
				FROM ready_window rw
				JOIN ranked r ON r.binary_id = rw.binary_id
				ORDER BY rw.priority_rank, r.group_rank, rw.updated_at DESC, rw.binary_id
				LIMIT $1
			),
			claimed AS (
				UPDATE yenc_recovery_work_items wi
				SET status = 'running',
				    lease_owner = 'recover_yenc',
				    lease_expires_at = NOW() + INTERVAL '30 minutes',
				    updated_at = NOW()
				FROM selected s
				WHERE wi.binary_id = s.binary_id
				RETURNING
					s.binary_id,
					s.article_header_id,
					s.provider_id,
					s.newsgroup_id,
					s.newsgroup_name,
					s.article_number,
					s.message_id,
					s.subject,
					s.poster,
					s.date_utc,
					s.article_bytes,
					s.article_lines,
					s.xref,
					s.subject_file_name,
					s.subject_file_index,
					s.subject_file_total,
					s.yenc_part_number,
					s.yenc_total_parts,
					s.yenc_file_size,
					s.missing_count,
					s.ready_at,
					s.current_binary_key,
					s.current_release_family_key,
					s.current_base_stem,
					s.current_readiness_bucket,
					s.structured_identity_binary_matched,
					s.group_rank,
					s.priority_rank,
					s.updated_at
			)
			SELECT
				binary_id,
				article_header_id,
				provider_id,
				newsgroup_id,
				newsgroup_name,
				article_number,
				message_id,
				subject,
				poster,
				date_utc,
				article_bytes,
				article_lines,
				xref,
				subject_file_name,
				subject_file_index,
				subject_file_total,
				yenc_part_number,
				yenc_total_parts,
				yenc_file_size,
				missing_count,
				ready_at,
				current_binary_key,
				current_release_family_key,
				current_base_stem,
				current_readiness_bucket,
				structured_identity_binary_matched,
				group_rank
			FROM claimed
			ORDER BY
				priority_rank,
				group_rank,
				updated_at DESC,
				binary_id
			LIMIT $1`,
			limit,
			windowLimit,
		)
		if err != nil {
			return err
		}
		defer rows.Close()

		out = make([]YEncRecoveryCandidate, 0, limit)
		for rows.Next() {
			item, err := scanYEncRecoveryCandidateWithRank(rows)
			if err != nil {
				return err
			}
			out = append(out, item)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate yenc recovery candidates: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list yenc recovery candidates: %w", err)
	}
	return out, nil
}

func (s *Store) withParallelGatherDisabledTx(ctx context.Context, readOnly bool, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SET LOCAL max_parallel_workers_per_gather = 0`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_parallel_hash = off`); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecordYEncRecoveryTransientFailure(ctx context.Context, articleHeaderID int64) error {
	if articleHeaderID <= 0 {
		return fmt.Errorf("article header id is required")
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE yenc_recovery_work_items
		SET status = 'ready',
		    ready_at = NOW() + INTERVAL '15 minutes',
		    lease_owner = '',
		    lease_expires_at = NULL,
		    updated_at = NOW()
		WHERE article_header_id = $1`, articleHeaderID); err != nil {
		return fmt.Errorf("record yenc recovery transient failure for article header %d: %w", articleHeaderID, err)
	}
	return nil
}

func scanYEncRecoveryCandidate(scanner interface{ Scan(dest ...any) error }) (YEncRecoveryCandidate, error) {
	return scanYEncRecoveryCandidateDest(scanner, nil)
}

func scanYEncRecoveryCandidateWithRank(scanner interface{ Scan(dest ...any) error }) (YEncRecoveryCandidate, error) {
	var groupRank int
	return scanYEncRecoveryCandidateDest(scanner, &groupRank)
}

func scanYEncRecoveryCandidateDest(scanner interface{ Scan(dest ...any) error }, groupRank *int) (YEncRecoveryCandidate, error) {
	var (
		item       YEncRecoveryCandidate
		date       sql.NullTime
		retryAfter sql.NullTime
	)
	dest := []any{
		&item.BinaryID,
		&item.ArticleHeaderID,
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
		&item.FileName,
		&item.FileIndex,
		&item.FileTotal,
		&item.YEncPart,
		&item.YEncTotal,
		&item.YEncFileSize,
		&item.YEncRecoveryMissingCount,
		&retryAfter,
		&item.CurrentBinaryKey,
		&item.CurrentReleaseFamilyKey,
		&item.CurrentBaseStem,
		&item.CurrentReadinessBucket,
		&item.StructuredIdentityBinaryMatched,
	}
	if groupRank != nil {
		dest = append(dest, groupRank)
	}
	if err := scanner.Scan(dest...); err != nil {
		return YEncRecoveryCandidate{}, fmt.Errorf("scan yenc recovery candidate: %w", err)
	}
	if date.Valid {
		t := date.Time.UTC()
		item.DateUTC = &t
	}
	if retryAfter.Valid {
		t := retryAfter.Time.UTC()
		item.YEncRecoveryRetryAfter = &t
	}
	item.RawOverview = map[string]any{}
	return item, nil
}

func (s *Store) ApplyYEncHeaderRecovery(ctx context.Context, in YEncHeaderRecoveryRecord) (*YEncHeaderRecoveryResult, error) {
	if in.BinaryID <= 0 {
		return nil, fmt.Errorf("binary id is required")
	}
	if strings.TrimSpace(in.BinaryKey) == "" || strings.TrimSpace(in.FileName) == "" {
		return nil, fmt.Errorf("recovered binary key and file name are required")
	}
	normalizeYEncHeaderRecoveryRecord(&in)

	var result *YEncHeaderRecoveryResult
	if err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin yenc recovery tx: %w", err)
		}
		defer rollbackTx(tx)

		seed, err := loadYEncRecoveryBinarySeed(ctx, tx, in.BinaryID)
		if err != nil {
			return err
		}
		if err := lockBinaryIdentityKey(ctx, tx, seed.ProviderID, seed.NewsgroupID, in.BinaryKey); err != nil {
			return err
		}

		targetID, err := findYEncRecoveryTargetBinary(ctx, tx, seed.ProviderID, seed.NewsgroupID, in.BinaryKey)
		if err != nil {
			return err
		}
		if targetID == 0 || targetID == in.BinaryID {
			if err := updateBinaryFromYEncRecovery(ctx, tx, in.BinaryID, in); err != nil {
				return err
			}
			targetID = in.BinaryID
		} else {
			if err := updateBinaryFromYEncRecovery(ctx, tx, targetID, in); err != nil {
				return err
			}
			if err := mergeRecoveredBinaryParts(ctx, tx, in.BinaryID, targetID, in.FileName); err != nil {
				return err
			}
			if err := mergeRecoveredReleaseFiles(ctx, tx, in.BinaryID, targetID, in.FileName); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM binary_core WHERE binary_id = $1`, in.BinaryID); err != nil {
				return fmt.Errorf("delete merged yenc source binary core %d: %w", in.BinaryID, err)
			}
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE article_header_ingest_payloads
			SET yenc_recovery_missing_count = 0,
			    yenc_recovery_last_missing_at = NULL,
			    yenc_recovery_retry_after = NULL
			WHERE article_header_id = $1`,
			in.ArticleHeaderID,
		); err != nil {
			return fmt.Errorf("clear yenc recovery backoff article %d: %w", in.ArticleHeaderID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE yenc_recovery_work_items
			SET status = 'done',
			    ready_at = NOW(),
			    lease_owner = '',
			    lease_expires_at = NULL,
			    updated_at = NOW()
			WHERE binary_id IN ($1, $2)`,
			in.BinaryID,
			targetID,
		); err != nil {
			return fmt.Errorf("mark yenc recovery work items done binary=%d target=%d: %w", in.BinaryID, targetID, err)
		}

		keys, err := refreshBinaryStatsInTx(ctx, tx, targetID)
		if err != nil {
			return err
		}
		keys = append(keys,
			releaseFamilySummaryKey{ProviderID: seed.ProviderID, NewsgroupID: seed.NewsgroupID, KeyKind: "release_family", FamilyKey: seed.ReleaseFamilyKey},
			releaseFamilySummaryKey{ProviderID: seed.ProviderID, NewsgroupID: seed.NewsgroupID, KeyKind: "base_stem", FamilyKey: seed.BaseStem},
			releaseFamilySummaryKey{ProviderID: seed.ProviderID, NewsgroupID: seed.NewsgroupID, KeyKind: "release_family", FamilyKey: in.ReleaseFamilyKey},
			releaseFamilySummaryKey{ProviderID: seed.ProviderID, NewsgroupID: seed.NewsgroupID, KeyKind: "base_stem", FamilyKey: in.BaseStem},
		)
		if err := markReleaseFamiliesDirtyBatch(ctx, tx, dedupeYEncRecoverySummaryKeys(keys)); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit yenc recovery tx: %w", err)
		}
		result = &YEncHeaderRecoveryResult{BinaryID: in.BinaryID, TargetBinaryID: targetID, Merged: targetID != in.BinaryID}
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

type yencRecoveryBinarySeed struct {
	ID               int64
	ProviderID       int64
	NewsgroupID      int64
	ReleaseFamilyKey string
	BaseStem         string
}

func loadYEncRecoveryBinarySeed(ctx context.Context, tx *sql.Tx, binaryID int64) (yencRecoveryBinarySeed, error) {
	var seed yencRecoveryBinarySeed
	err := tx.QueryRowContext(ctx, `
		SELECT
			bc.binary_id,
			bc.provider_id,
			bc.newsgroup_id,
			bic.release_family_key,
			bic.base_stem
		FROM binary_core bc
		JOIN binary_identity_current bic ON bic.binary_id = bc.binary_id
		WHERE bc.binary_id = $1
		FOR UPDATE OF bc, bic`,
		binaryID,
	).Scan(&seed.ID, &seed.ProviderID, &seed.NewsgroupID, &seed.ReleaseFamilyKey, &seed.BaseStem)
	if err == sql.ErrNoRows {
		return seed, fmt.Errorf("%w: %d for yenc recovery", ErrBinaryNotFound, binaryID)
	}
	if err != nil {
		return seed, fmt.Errorf("load yenc recovery binary %d: %w", binaryID, err)
	}
	return seed, nil
}

func findYEncRecoveryTargetBinary(ctx context.Context, tx *sql.Tx, providerID, newsgroupID int64, binaryKey string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		SELECT binary_id
		FROM binary_core
		WHERE provider_id = $1
		  AND newsgroup_id = $2
		  AND binary_key = $3
		FOR UPDATE`,
		providerID,
		newsgroupID,
		strings.TrimSpace(binaryKey),
	).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("find yenc recovery target binary: %w", err)
	}
	return id, nil
}

func updateBinaryFromYEncRecovery(ctx context.Context, tx *sql.Tx, binaryID int64, in YEncHeaderRecoveryRecord) error {
	groupingSummaryKind, groupingSummaryStatus, groupingSummaryFallbackUsed := groupingSummaryScalars(sanitizeStringMap(in.GroupingEvidence))
	if _, err := tx.ExecContext(ctx, `
		UPDATE binary_core
		SET binary_key = $2,
		    updated_at = NOW()
		WHERE binary_id = $1`,
		binaryID,
		in.BinaryKey,
	); err != nil {
		return fmt.Errorf("update yenc recovered binary core %d: %w", binaryID, err)
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE binary_identity_current
		SET source_release_key = $2,
		    release_family_key = $3,
		    file_set_key = $4,
		    file_family_key = $5,
		    identity_strength = $6,
		    identity_reason = $7,
		    subject_set_token = $8,
		    subject_set_kind = $9,
		    family_kind = $10,
		    base_stem = $11,
		    is_auxiliary = $12,
		    is_main_payload = $13,
		    release_key = $14,
		    release_name = $15,
		    binary_name = $16,
		    file_name = $17,
		    file_index = CASE WHEN $18 > 0 THEN $18 ELSE file_index END,
		    expected_file_count = GREATEST(expected_file_count, $19),
		    match_confidence = GREATEST(match_confidence, $20),
		    match_status = $21,
		    grouping_summary_kind = $22,
		    grouping_summary_status = $23,
		    grouping_summary_fallback_used = $24,
		    updated_at = NOW()
		WHERE binary_id = $1`,
		binaryID,
		in.SourceReleaseKey,
		in.ReleaseFamilyKey,
		in.FileSetKey,
		in.FileFamilyKey,
		in.IdentityStrength,
		in.IdentityReason,
		in.SubjectSetToken,
		in.SubjectSetKind,
		in.FamilyKind,
		in.BaseStem,
		in.IsAuxiliary,
		in.IsMainPayload,
		in.ReleaseKey,
		in.ReleaseName,
		in.BinaryName,
		in.FileName,
		in.FileIndex,
		in.ExpectedFileCount,
		in.MatchConfidence,
		in.MatchStatus,
		groupingSummaryKind,
		groupingSummaryStatus,
		groupingSummaryFallbackUsed,
	)
	if err != nil {
		return fmt.Errorf("update yenc recovered binary identity %d: %w", binaryID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE binary_observation_stats
		SET total_parts = GREATEST(total_parts, $2),
		    updated_at = NOW()
		WHERE binary_id = $1`,
		binaryID,
		in.TotalParts,
	); err != nil {
		return fmt.Errorf("update yenc recovered binary stats %d: %w", binaryID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE binary_recovery_current
		SET recovered_source = 'yenc_header',
		    recovered_confidence = GREATEST(recovered_confidence, $2),
		    recovered_file_name = $3,
		    recovered_at = NOW(),
		    updated_at = NOW()
		WHERE binary_id = $1`,
		binaryID,
		in.MatchConfidence,
		in.FileName,
	); err != nil {
		return fmt.Errorf("update yenc recovered binary recovery %d: %w", binaryID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE binary_parts
		SET file_name = $2,
		    total_parts = GREATEST(total_parts, $3),
		    updated_at = NOW()
		WHERE binary_id = $1`,
		binaryID,
		in.FileName,
		in.TotalParts,
	); err != nil {
		return fmt.Errorf("update yenc recovered binary parts %d: %w", binaryID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE release_files
		SET file_name = $2
		WHERE binary_id = $1`,
		binaryID,
		in.FileName,
	); err != nil {
		return fmt.Errorf("update yenc recovered release files %d: %w", binaryID, err)
	}
	if err := syncBinaryCompletionKeysForBinaryIDsInTx(ctx, tx, []int64{binaryID}); err != nil {
		return err
	}
	return nil
}

func mergeRecoveredBinaryParts(ctx context.Context, tx *sql.Tx, sourceID, targetID int64, fileName string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, part_number, segment_bytes
		FROM binary_parts
		WHERE binary_id = $1
		ORDER BY part_number, id`,
		sourceID,
	)
	if err != nil {
		return fmt.Errorf("list yenc source binary parts %d: %w", sourceID, err)
	}
	defer rows.Close()

	type part struct {
		ID           int64
		PartNumber   int
		SegmentBytes int64
	}
	parts := []part{}
	for rows.Next() {
		var p part
		if err := rows.Scan(&p.ID, &p.PartNumber, &p.SegmentBytes); err != nil {
			return fmt.Errorf("scan yenc source part: %w", err)
		}
		parts = append(parts, p)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate yenc source parts: %w", err)
	}

	for _, p := range parts {
		var existingID int64
		var existingBytes int64
		err := tx.QueryRowContext(ctx, `
			SELECT id, segment_bytes
			FROM binary_parts
			WHERE binary_id = $1
			  AND part_number = $2
			FOR UPDATE`,
			targetID,
			p.PartNumber,
		).Scan(&existingID, &existingBytes)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("lock yenc target part binary=%d part=%d: %w", targetID, p.PartNumber, err)
		}
		if err == nil && existingBytes >= p.SegmentBytes {
			if _, err := tx.ExecContext(ctx, `DELETE FROM binary_parts WHERE id = $1`, p.ID); err != nil {
				return fmt.Errorf("delete duplicate yenc source part %d: %w", p.ID, err)
			}
			continue
		}
		if err == nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM binary_parts WHERE id = $1`, existingID); err != nil {
				return fmt.Errorf("delete weaker yenc target part %d: %w", existingID, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE binary_parts
			SET binary_id = $2,
			    file_name = $3,
			    updated_at = NOW()
			WHERE id = $1`,
			p.ID,
			targetID,
			fileName,
		); err != nil {
			return fmt.Errorf("move yenc source part %d to binary %d: %w", p.ID, targetID, err)
		}
	}
	return nil
}

func mergeRecoveredReleaseFiles(ctx context.Context, tx *sql.Tx, sourceID, targetID int64, fileName string) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE release_files
		SET binary_id = $2,
		    file_name = $3
		WHERE binary_id = $1`,
		sourceID,
		targetID,
		fileName,
	); err != nil {
		return fmt.Errorf("move yenc release files from binary %d to %d: %w", sourceID, targetID, err)
	}
	return nil
}

func normalizeYEncHeaderRecoveryRecord(in *YEncHeaderRecoveryRecord) {
	if in == nil {
		return
	}
	in.SourceReleaseKey = strings.TrimSpace(in.SourceReleaseKey)
	in.ReleaseFamilyKey = firstNonBlank(in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey)
	in.FileFamilyKey = strings.TrimSpace(in.FileFamilyKey)
	in.FamilyKind = strings.TrimSpace(in.FamilyKind)
	in.BaseStem = strings.TrimSpace(in.BaseStem)
	in.ReleaseKey = firstNonBlank(in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey)
	in.ReleaseName = strings.TrimSpace(in.ReleaseName)
	in.BinaryKey = strings.TrimSpace(in.BinaryKey)
	in.BinaryName = strings.TrimSpace(in.BinaryName)
	in.FileName = strings.TrimSpace(in.FileName)
	in.MatchStatus = firstNonBlank(in.MatchStatus, "probable")
	if in.GroupingEvidence == nil {
		in.GroupingEvidence = map[string]any{}
	}
}

func dedupeYEncRecoverySummaryKeys(in []releaseFamilySummaryKey) []releaseFamilySummaryKey {
	seen := make(map[releaseFamilySummaryKey]struct{}, len(in))
	out := make([]releaseFamilySummaryKey, 0, len(in))
	for _, key := range in {
		if key.ProviderID <= 0 || key.NewsgroupID <= 0 || strings.TrimSpace(key.KeyKind) == "" || strings.TrimSpace(key.FamilyKey) == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProviderID != out[j].ProviderID {
			return out[i].ProviderID < out[j].ProviderID
		}
		if out[i].NewsgroupID != out[j].NewsgroupID {
			return out[i].NewsgroupID < out[j].NewsgroupID
		}
		if out[i].KeyKind != out[j].KeyKind {
			return out[i].KeyKind < out[j].KeyKind
		}
		return out[i].FamilyKey < out[j].FamilyKey
	})
	return out
}
