package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	releaseReadinessActionable       = "actionable"
	releaseReadinessFragmentOnly     = "fragment_only"
	releaseReadinessStaleCleanupOnly = "stale_cleanup_only"
	releaseReadinessWeakSingle       = "weak_single_binary"
	releaseReadinessWeakObfuscated   = "weak_obfuscated_set"
	releaseReadinessPreferBaseStem   = "prefer_base_stem"
	releaseReadinessOvergrouped      = "overgrouped_contextual"
)

var summaryOpaqueTokenRE = regexp.MustCompile(`(?i)^[a-z0-9]{12,}$`)

type releaseFamilySummaryKey struct {
	ProviderID  int64
	NewsgroupID int64
	KeyKind     string
	FamilyKey   string
}

func normalizeReleaseFamilySummaryKey(providerID, newsgroupID int64, keyKind, familyKey string) (releaseFamilySummaryKey, bool) {
	if providerID <= 0 || newsgroupID <= 0 {
		return releaseFamilySummaryKey{}, false
	}

	keyKind = strings.TrimSpace(keyKind)
	familyKey = strings.TrimSpace(familyKey)
	if keyKind == "" || familyKey == "" {
		return releaseFamilySummaryKey{}, false
	}
	if keyKind == "base_stem" {
		familyKey = strings.ToLower(familyKey)
	}

	return releaseFamilySummaryKey{
		ProviderID:  providerID,
		NewsgroupID: newsgroupID,
		KeyKind:     keyKind,
		FamilyKey:   familyKey,
	}, true
}

func appendReleaseFamilySummaryKey(keys []releaseFamilySummaryKey, seen map[releaseFamilySummaryKey]struct{}, providerID, newsgroupID int64, keyKind, familyKey string) []releaseFamilySummaryKey {
	key, ok := normalizeReleaseFamilySummaryKey(providerID, newsgroupID, keyKind, familyKey)
	if !ok {
		return keys
	}
	if _, exists := seen[key]; exists {
		return keys
	}
	seen[key] = struct{}{}
	return append(keys, key)
}

func sortReleaseFamilySummaryKeys(keys []releaseFamilySummaryKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].ProviderID != keys[j].ProviderID {
			return keys[i].ProviderID < keys[j].ProviderID
		}
		if keys[i].NewsgroupID != keys[j].NewsgroupID {
			return keys[i].NewsgroupID < keys[j].NewsgroupID
		}
		if keys[i].KeyKind != keys[j].KeyKind {
			return keys[i].KeyKind < keys[j].KeyKind
		}
		return keys[i].FamilyKey < keys[j].FamilyKey
	})
}

func refreshReleaseFamilySummary(ctx context.Context, tx *sql.Tx, key releaseFamilySummaryKey) error {
	if tx == nil {
		return fmt.Errorf("release family summary tx is required")
	}

	whereClause := `
			b.provider_id = $1
			AND b.newsgroup_id = $2
			AND b.release_family_key = $3`
	if key.KeyKind == "base_stem" {
		whereClause = `
			b.provider_id = $1
			AND b.newsgroup_id = $2
			AND GREATEST(b.expected_file_count, b.expected_archive_file_count) > 1
			AND BTRIM(b.base_stem) <> ''
			AND LOWER(BTRIM(b.base_stem)) = $3`
	}

	var (
		sourceReleaseKey               string
		releaseKey                     string
		releaseName                    string
		binaryCount                    int
		completeBinaryCount            int
		completeMainPayloadBinaryCount int
		expectedFileCount              int
		expectedArchiveFileCount       int
		hasExpectedFileCount           bool
		hasExpectedArchiveFileCount    bool
		totalBytes                     int64
		earliestPostedAt               sql.NullTime
		dominantFamilyKind             string
		dominantFileName               string
		dominantMatchConfidence        float64
	)
	query := `
		SELECT
			COALESCE(MAX(b.source_release_key), '') AS source_release_key,
			COALESCE(MAX(b.release_key), '') AS release_key,
			COALESCE(MAX(b.release_name), '') AS release_name,
			COUNT(*)::INTEGER AS binary_count,
			COALESCE(SUM(
				CASE
					WHEN b.observed_parts = b.total_parts AND b.total_parts > 0 THEN 1
					ELSE 0
				END
			), 0)::INTEGER AS complete_binary_count,
			COALESCE(SUM(
				CASE
					WHEN (b.is_main_payload OR NOT b.is_auxiliary)
					 AND b.observed_parts = b.total_parts
					 AND b.total_parts > 0 THEN 1
					ELSE 0
				END
			), 0)::INTEGER AS complete_main_payload_binary_count,
			COALESCE(MAX(b.expected_file_count), 0)::INTEGER AS expected_file_count,
			COALESCE(MAX(b.expected_archive_file_count), 0)::INTEGER AS expected_archive_file_count,
			COALESCE(BOOL_OR(b.expected_file_count > 0), FALSE) AS has_expected_file_count,
			COALESCE(BOOL_OR(b.expected_archive_file_count > 0), FALSE) AS has_expected_archive_file_count,
			COALESCE(SUM(b.total_bytes), 0)::BIGINT AS total_bytes,
			MIN(b.posted_at) AS earliest_posted_at
		FROM binaries b
		WHERE ` + whereClause
	if err := tx.QueryRowContext(ctx, query, key.ProviderID, key.NewsgroupID, key.FamilyKey).Scan(
		&sourceReleaseKey,
		&releaseKey,
		&releaseName,
		&binaryCount,
		&completeBinaryCount,
		&completeMainPayloadBinaryCount,
		&expectedFileCount,
		&expectedArchiveFileCount,
		&hasExpectedFileCount,
		&hasExpectedArchiveFileCount,
		&totalBytes,
		&earliestPostedAt,
	); err != nil {
		return fmt.Errorf("query release family summary provider=%d group=%d kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
	}

	if binaryCount == 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO release_family_readiness_summaries (
				provider_id,
				newsgroup_id,
				key_kind,
				family_key,
				source_release_key,
				release_key,
				release_name,
				binary_count,
				complete_binary_count,
				complete_main_payload_binary_count,
				incomplete_binary_count,
				expected_file_count,
				expected_archive_file_count,
				has_expected_file_count,
				has_expected_archive_file_count,
				total_bytes,
				earliest_posted_at,
				dominant_family_kind,
				dominant_file_name,
				dominant_match_confidence,
				readiness_bucket,
				expected_file_coverage_pct,
				archive_file_coverage_pct,
				processed_at,
				updated_at
			)
			VALUES ($1,$2,$3,$4,'','','',0,0,0,0,0,0,FALSE,FALSE,0,NULL,'','',0,$5,0,0,TIMESTAMPTZ 'epoch',NOW())
			ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
			SET source_release_key = EXCLUDED.source_release_key,
			    release_key = EXCLUDED.release_key,
			    release_name = EXCLUDED.release_name,
			    binary_count = EXCLUDED.binary_count,
			    complete_binary_count = EXCLUDED.complete_binary_count,
			    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
			    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
			    expected_file_count = EXCLUDED.expected_file_count,
			    expected_archive_file_count = EXCLUDED.expected_archive_file_count,
			    has_expected_file_count = EXCLUDED.has_expected_file_count,
			    has_expected_archive_file_count = EXCLUDED.has_expected_archive_file_count,
			    total_bytes = EXCLUDED.total_bytes,
			    earliest_posted_at = EXCLUDED.earliest_posted_at,
			    dominant_family_kind = EXCLUDED.dominant_family_kind,
			    dominant_file_name = EXCLUDED.dominant_file_name,
			    dominant_match_confidence = EXCLUDED.dominant_match_confidence,
			    readiness_bucket = EXCLUDED.readiness_bucket,
			    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
			    archive_file_coverage_pct = EXCLUDED.archive_file_coverage_pct,
			    processed_at = COALESCE(release_family_readiness_summaries.processed_at, release_family_readiness_summaries.updated_at),
			    updated_at = NOW()`,
			key.ProviderID,
			key.NewsgroupID,
			key.KeyKind,
			key.FamilyKey,
			releaseReadinessStaleCleanupOnly,
		); err != nil {
			return fmt.Errorf("upsert stale cleanup release family summary provider=%d group=%d kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
		}
		return nil
	}

	dominantQuery := `
		SELECT
			COALESCE(b.family_kind, ''),
			COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), ''),
			COALESCE(b.match_confidence, 0)
		FROM binaries b
		WHERE ` + whereClause + `
		ORDER BY
			CASE
				WHEN (b.is_main_payload OR NOT b.is_auxiliary) THEN 0
				ELSE 1
			END ASC,
			CASE
				WHEN b.total_parts > 0 AND b.observed_parts = b.total_parts THEN 0
				ELSE 1
			END ASC,
			b.observed_parts DESC,
			b.total_bytes DESC,
			b.match_confidence DESC,
			b.id ASC
		LIMIT 1`
	if err := tx.QueryRowContext(ctx, dominantQuery, key.ProviderID, key.NewsgroupID, key.FamilyKey).Scan(
		&dominantFamilyKind,
		&dominantFileName,
		&dominantMatchConfidence,
	); err != nil {
		return fmt.Errorf("query dominant release family binary provider=%d group=%d kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
	}

	readinessBucket := releaseReadinessFragmentOnly
	if completeMainPayloadBinaryCount > 0 {
		readinessBucket = releaseReadinessActionable
	}
	if binaryCount == 1 &&
		completeMainPayloadBinaryCount == 1 &&
		expectedFileCount <= 0 &&
		expectedArchiveFileCount <= 0 &&
		!summaryAllowsStandaloneBinaryRelease(dominantFamilyKind, dominantFileName, dominantMatchConfidence) {
		readinessBucket = releaseReadinessWeakSingle
	}
	if readinessBucket == releaseReadinessActionable && summaryIsWeakObfuscatedFamily(dominantFamilyKind) {
		readinessBucket = releaseReadinessWeakObfuscated
	}
	expectedFileCoveragePct := 0.0
	if expectedFileCount > 0 {
		expectedFileCoveragePct = (float64(completeBinaryCount) / float64(expectedFileCount)) * 100
		if expectedFileCoveragePct > 100 {
			expectedFileCoveragePct = 100
		}
	}
	archiveFileCoveragePct := 0.0
	if expectedArchiveFileCount > 0 {
		archiveFileCoveragePct = (float64(completeMainPayloadBinaryCount) / float64(expectedArchiveFileCount)) * 100
		if archiveFileCoveragePct > 100 {
			archiveFileCoveragePct = 100
		}
	}

	var earliestPostedAtValue any
	if earliestPostedAt.Valid {
		t := earliestPostedAt.Time.UTC()
		earliestPostedAtValue = t
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id,
			newsgroup_id,
			key_kind,
			family_key,
			source_release_key,
			release_key,
			release_name,
			binary_count,
			complete_binary_count,
			complete_main_payload_binary_count,
			incomplete_binary_count,
			expected_file_count,
			expected_archive_file_count,
			has_expected_file_count,
			has_expected_archive_file_count,
			total_bytes,
			earliest_posted_at,
			dominant_family_kind,
			dominant_file_name,
			dominant_match_confidence,
			readiness_bucket,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			processed_at,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,TIMESTAMPTZ 'epoch',NOW())
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET source_release_key = EXCLUDED.source_release_key,
		    release_key = EXCLUDED.release_key,
		    release_name = EXCLUDED.release_name,
		    binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    expected_archive_file_count = EXCLUDED.expected_archive_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    has_expected_archive_file_count = EXCLUDED.has_expected_archive_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    dominant_family_kind = EXCLUDED.dominant_family_kind,
		    dominant_file_name = EXCLUDED.dominant_file_name,
		    dominant_match_confidence = EXCLUDED.dominant_match_confidence,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    archive_file_coverage_pct = EXCLUDED.archive_file_coverage_pct,
		    processed_at = COALESCE(release_family_readiness_summaries.processed_at, release_family_readiness_summaries.updated_at),
		    updated_at = NOW()`,
		key.ProviderID,
		key.NewsgroupID,
		key.KeyKind,
		key.FamilyKey,
		sourceReleaseKey,
		releaseKey,
		releaseName,
		binaryCount,
		completeBinaryCount,
		completeMainPayloadBinaryCount,
		binaryCount-completeBinaryCount,
		expectedFileCount,
		expectedArchiveFileCount,
		hasExpectedFileCount,
		hasExpectedArchiveFileCount,
		totalBytes,
		earliestPostedAtValue,
		dominantFamilyKind,
		dominantFileName,
		dominantMatchConfidence,
		readinessBucket,
		expectedFileCoveragePct,
		archiveFileCoveragePct,
	); err != nil {
		return fmt.Errorf("upsert release family summary provider=%d group=%d kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
	}

	return nil
}

func markReleaseFamilyDirty(ctx context.Context, tx *sql.Tx, providerID, newsgroupID int64, keyKind, familyKey string) error {
	if tx == nil {
		return fmt.Errorf("release summary queue tx is required")
	}

	key, ok := normalizeReleaseFamilySummaryKey(providerID, newsgroupID, keyKind, familyKey)
	if !ok {
		return nil
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id,
			newsgroup_id,
			key_kind,
			family_key,
			source_release_key,
			release_key,
			release_name,
			binary_count,
			complete_binary_count,
			complete_main_payload_binary_count,
			incomplete_binary_count,
			expected_file_count,
			expected_archive_file_count,
			has_expected_file_count,
			has_expected_archive_file_count,
			total_bytes,
			earliest_posted_at,
			dominant_family_kind,
			dominant_file_name,
			dominant_match_confidence,
			readiness_bucket,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			processed_at,
			updated_at
		)
		VALUES ($1,$2,$3,$4,'','','',0,0,0,0,0,0,FALSE,FALSE,0,NULL,'','',0,$5,0,0,TIMESTAMPTZ 'epoch',NOW())
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET processed_at = COALESCE(release_family_readiness_summaries.processed_at, release_family_readiness_summaries.updated_at),
		    updated_at = NOW()`,
		key.ProviderID,
		key.NewsgroupID,
		key.KeyKind,
		key.FamilyKey,
		releaseReadinessStaleCleanupOnly,
	)
	if err != nil {
		return fmt.Errorf("mark release family dirty provider=%d group=%d key_kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
	}
	return nil
}

func summaryAllowsStandaloneBinaryRelease(familyKind, fileName string, matchConfidence float64) bool {
	familyKind = strings.TrimSpace(strings.ToLower(familyKind))
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return false
	}
	if familyKind == "contextual_obfuscated" {
		return false
	}
	if matchConfidence < 0.85 {
		return false
	}

	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".mkv", ".mp4", ".avi", ".mov", ".mp3", ".flac", ".m4a", ".wav":
	default:
		return false
	}

	base := strings.TrimSpace(strings.TrimSuffix(fileName, filepath.Ext(fileName)))
	base = strings.ReplaceAll(base, ".", " ")
	base = strings.ReplaceAll(base, "_", " ")
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.Join(strings.Fields(strings.ToLower(base)), " ")
	if base == "" {
		return false
	}
	if summaryOpaqueTokenRE.MatchString(strings.ReplaceAll(base, " ", "")) {
		return false
	}
	return true
}

func summaryIsWeakObfuscatedFamily(familyKind string) bool {
	switch strings.TrimSpace(strings.ToLower(familyKind)) {
	case "numeric_obfuscated_set", "opaque_set":
		return true
	default:
		return false
	}
}
