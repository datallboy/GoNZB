package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const yencRecoverySeedScanLowYieldThreshold = 25

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
	if readyCount < limit {
		if readyCount <= yencRecoverySeedScanLowYieldThreshold && s.shouldBackoffYEncRecoverySeedScan(time.Now()) {
			return s.listReadyYEncRecoveryCandidates(ctx, limit)
		}
		shortfall := limit - readyCount
		seedLimit := shortfall * 4
		if seedLimit < shortfall {
			seedLimit = shortfall
		}
		if seedLimit < limit {
			seedLimit = limit
		}
		if seedLimit > yencRecoveryWorkItemSeedLimit {
			seedLimit = yencRecoveryWorkItemSeedLimit
		}
		upserted, _, err := s.BackfillYEncRecoveryWorkItems(ctx, seedLimit)
		if err != nil {
			return nil, err
		}
		s.recordYEncRecoverySeedScanResult(time.Now(), readyCount, upserted)
	}

	return s.listReadyYEncRecoveryCandidates(ctx, limit)
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
			LEFT JOIN article_headers ah ON ah.id = wi.article_header_id
			LEFT JOIN newsgroups ng ON ng.id = wi.newsgroup_id
			WHERE wi.status = 'ready'
			  AND (
			  	ah.id IS NULL OR
			  	ng.id IS NULL OR
			  	BTRIM(COALESCE(wi.message_id, '')) = ''
			  )
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
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM (
			SELECT wi.binary_id
			FROM yenc_recovery_work_items wi
			JOIN article_headers ah ON ah.id = wi.article_header_id
			JOIN newsgroups ng ON ng.id = ah.newsgroup_id
			WHERE wi.status = 'ready'
			  AND wi.ready_at <= NOW()
			  AND BTRIM(COALESCE(ah.message_id, '')) <> ''
			LIMIT $1
		) ready`,
		limit,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count ready yenc recovery candidates: %w", err)
	}
	return count, nil
}

func (s *Store) listReadyYEncRecoveryCandidates(ctx context.Context, limit int) ([]YEncRecoveryCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH ready_items AS (
			SELECT
				wi.binary_id,
				ah.id,
				ah.provider_id,
				ah.newsgroup_id,
				ng.group_name,
				ah.article_number,
				ah.message_id,
				COALESCE(NULLIF(p.subject, ''), NULLIF(b.binary_name, ''), NULLIF(b.file_name, ''), NULLIF(b.release_name, ''), '') AS subject,
				COALESCE(p.poster, '') AS poster,
				ah.date_utc,
				ah.bytes,
				ah.lines,
				COALESCE(p.xref, '') AS xref,
				COALESCE(NULLIF(p.subject_file_name, ''), NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '') AS subject_file_name,
				CASE
					WHEN COALESCE(p.subject_file_index, 0) > 0 THEN p.subject_file_index
					WHEN COALESCE(b.file_index, 0) > 0 THEN b.file_index
					ELSE 0
				END AS subject_file_index,
				GREATEST(
					COALESCE(p.subject_file_total, 0),
					COALESCE(b.expected_file_count, 0),
					COALESCE(b.expected_archive_file_count, 0)
				) AS subject_file_total,
				COALESCE(p.yenc_part_number, 0) AS yenc_part_number,
				GREATEST(COALESCE(p.yenc_total_parts, 0), COALESCE(b.total_parts, 0)) AS yenc_total_parts,
				COALESCE(p.yenc_file_size, 0) AS yenc_file_size,
				COALESCE(p.yenc_recovery_missing_count, 0) AS yenc_recovery_missing_count,
				p.yenc_recovery_retry_after,
				wi.current_binary_key,
				wi.current_release_family_key,
				wi.current_base_stem,
				wi.current_readiness_bucket,
				wi.structured_identity_binary_matched,
				wi.priority_rank,
				wi.updated_at,
				ROW_NUMBER() OVER (
					PARTITION BY ah.provider_id, ah.newsgroup_id, wi.priority_rank
					ORDER BY wi.updated_at DESC, wi.binary_id
				) AS group_rank
			FROM yenc_recovery_work_items wi
			JOIN binaries b ON b.id = wi.binary_id
			JOIN article_headers ah ON ah.id = wi.article_header_id
			LEFT JOIN article_header_ingest_payloads p ON p.article_header_id = ah.id
			JOIN newsgroups ng ON ng.id = ah.newsgroup_id
			WHERE wi.status = 'ready'
			  AND wi.ready_at <= NOW()
			  AND BTRIM(COALESCE(ah.message_id, '')) <> ''
		)
		SELECT
			binary_id,
			id,
			provider_id,
			newsgroup_id,
			group_name,
			article_number,
			message_id,
			subject,
			poster,
			date_utc,
			bytes,
			lines,
			xref,
			subject_file_name,
			subject_file_index,
			subject_file_total,
			yenc_part_number,
			yenc_total_parts,
			yenc_file_size,
			yenc_recovery_missing_count,
			yenc_recovery_retry_after,
			current_binary_key,
			current_release_family_key,
			current_base_stem,
			current_readiness_bucket,
			structured_identity_binary_matched
		FROM ready_items
		ORDER BY
			priority_rank,
			group_rank,
			updated_at DESC,
			binary_id
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list yenc recovery candidates: %w", err)
	}
	defer rows.Close()

	out := make([]YEncRecoveryCandidate, 0, limit)
	for rows.Next() {
		item, err := scanYEncRecoveryCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate yenc recovery candidates: %w", err)
	}
	return out, nil
}

func scanYEncRecoveryCandidate(scanner interface{ Scan(dest ...any) error }) (YEncRecoveryCandidate, error) {
	var (
		item       YEncRecoveryCandidate
		date       sql.NullTime
		retryAfter sql.NullTime
	)
	if err := scanner.Scan(
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
	); err != nil {
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
			if _, err := tx.ExecContext(ctx, `DELETE FROM binaries WHERE id = $1`, in.BinaryID); err != nil {
				return fmt.Errorf("delete merged yenc source binary %d: %w", in.BinaryID, err)
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
		SELECT id, provider_id, newsgroup_id, release_family_key, base_stem
		FROM binaries
		WHERE id = $1
		FOR UPDATE`,
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
		SELECT id
		FROM binaries
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
	evidenceJSON, err := json.Marshal(in.GroupingEvidence)
	if err != nil {
		return fmt.Errorf("marshal yenc grouping evidence: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE binaries
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
		    binary_key = $16,
		    binary_name = $17,
		    file_name = $18,
		    file_index = CASE WHEN $19 > 0 THEN $19 ELSE file_index END,
		    expected_file_count = GREATEST(expected_file_count, $20),
		    total_parts = GREATEST(total_parts, $21),
		    match_confidence = GREATEST(match_confidence, $22),
		    match_status = $23,
		    grouping_evidence_json = $24::jsonb,
		    recovered_source = 'yenc_header',
		    recovered_confidence = GREATEST(recovered_confidence, $22),
		    recovered_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1`,
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
		in.BinaryKey,
		in.BinaryName,
		in.FileName,
		in.FileIndex,
		in.ExpectedFileCount,
		in.TotalParts,
		in.MatchConfidence,
		in.MatchStatus,
		string(evidenceJSON),
	)
	if err != nil {
		return fmt.Errorf("update yenc recovered binary %d: %w", binaryID, err)
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
