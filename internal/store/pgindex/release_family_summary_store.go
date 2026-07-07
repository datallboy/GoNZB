package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	releaseReadinessActionable               = "actionable"
	releaseReadinessFragmentOnly             = "fragment_only"
	releaseReadinessStaleCleanupOnly         = "stale_cleanup_only"
	releaseReadinessWeakSingle               = "weak_single_binary"
	releaseReadinessWeakObfuscated           = "weak_obfuscated_set"
	releaseReadinessPreferBaseStem           = "prefer_base_stem"
	releaseReadinessOvergrouped              = "overgrouped_contextual"
	releaseFamilyDirtyBatchSize              = 10000
	releaseFamilySummaryRefreshBatch         = 10000
	releaseFamilySummaryRefreshCap           = 10000
	releaseFamilySummaryRefreshHotCap        = 10000
	releaseFamilySummaryRefreshColdCap       = 10000
	releaseFamilySummaryRefreshQueryBatchCap = 5000
	releaseFamilySummaryMergeRowsMax         = 2000
	releaseRecoveredFileSetSyncCap           = 128
	releaseRecoveredFileSetSyncChunkSize     = 32
)

type releaseSummaryRefreshMode int

const (
	releaseSummaryRefreshModeHot releaseSummaryRefreshMode = iota
	releaseSummaryRefreshModeCold
)

var summaryOpaqueTokenRE = regexp.MustCompile(`(?i)^[a-z0-9]{12,}$`)
var summaryNumericOpaqueReleaseRE = regexp.MustCompile(`^[0-9]{5,}\s+[a-z](\s+[a-z]{2,4})?$`)

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
		if summaryIsOpaqueBaseStemKey(familyKey) {
			return releaseFamilySummaryKey{}, false
		}
	}

	return releaseFamilySummaryKey{
		ProviderID:  providerID,
		NewsgroupID: newsgroupID,
		KeyKind:     keyKind,
		FamilyKey:   familyKey,
	}, true
}

func summaryIsOpaqueBaseStemKey(familyKey string) bool {
	familyKey = strings.TrimSpace(strings.ToLower(familyKey))
	if familyKey == "" {
		return false
	}
	return summaryOpaqueTokenRE.MatchString(strings.ReplaceAll(familyKey, " ", ""))
}

type releaseFamilySummaryRow struct {
	ProviderID                     int64
	NewsgroupID                    int64
	KeyKind                        string
	FamilyKey                      string
	SourceReleaseKey               string
	ReleaseKey                     string
	ReleaseName                    string
	BinaryCount                    int
	CompleteBinaryCount            int
	CompleteMainPayloadBinaryCount int
	ExpectedFileCount              int
	ExpectedArchiveFileCount       int
	HasExpectedFileCount           bool
	HasExpectedArchiveFileCount    bool
	TotalBytes                     int64
	EarliestPostedAt               sql.NullTime
	DominantFamilyKind             string
	DominantFileName               string
	DominantMatchConfidence        float64
	RecoverPending                 bool
}

type releaseSummaryRefreshMetrics struct {
	Refreshed                    int
	Dequeued                     int
	Mode                         string
	HotAttempts                  int
	ColdAttempts                 int
	HotDequeued                  int
	ColdDequeued                 int
	DequeueDuration              time.Duration
	SummaryRefreshDuration       time.Duration
	SummaryAggregateDuration     time.Duration
	SummaryDominantDuration      time.Duration
	ReadyCandidateSyncDuration   time.Duration
	RecoveredFileSetSyncDuration time.Duration
	PhaseADuration               time.Duration
	PhaseBDuration               time.Duration
}

type ReleaseSummaryRefreshMetrics struct {
	Refreshed                    int
	Dequeued                     int
	Mode                         string
	HotAttempts                  int
	ColdAttempts                 int
	HotDequeued                  int
	ColdDequeued                 int
	DequeueDuration              time.Duration
	SummaryRefreshDuration       time.Duration
	SummaryAggregateDuration     time.Duration
	SummaryDominantDuration      time.Duration
	ReadyCandidateSyncDuration   time.Duration
	RecoveredFileSetSyncDuration time.Duration
	PhaseADuration               time.Duration
	PhaseBDuration               time.Duration
}

type releaseCandidateSummaryState struct {
	Key                            releaseFamilySummaryKey
	SourceReleaseKey               string
	ReleaseKey                     string
	ReleaseName                    string
	BinaryCount                    int
	CompleteBinaryCount            int
	CompleteMainPayloadBinaryCount int
	ExpectedFileCount              int
	ExpectedArchiveFileCount       int
	HasExpectedFileCount           bool
	HasExpectedArchiveFileCount    bool
	ExpectedFileCoveragePct        float64
	ArchiveFileCoveragePct         float64
	TotalBytes                     int64
	EarliestPostedAt               sql.NullTime
	UpdatedAt                      sql.NullTime
	DominantFamilyKind             string
	DominantFileName               string
	DominantMatchConfidence        float64
	ReadinessBucket                string
	RecoverPending                 bool
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
			bc.provider_id = $1
			AND bc.newsgroup_id = $2
			AND bic.release_family_key = $3
			AND NOT EXISTS (
				SELECT 1
				FROM binary_lifecycle bl
				WHERE bl.source_posted_at = bc.source_posted_at
				  AND bl.binary_id = bc.binary_id
				  AND bl.lifecycle_status = 'superseded'
			)`
	if key.KeyKind == "base_stem" {
		whereClause = `
			bc.provider_id = $1
			AND bc.newsgroup_id = $2
			AND GREATEST(bic.expected_file_count, bic.expected_archive_file_count) > 1
			AND BTRIM(bic.base_stem) <> ''
			AND LOWER(BTRIM(bic.base_stem)) = $3
			AND NOT EXISTS (
				SELECT 1
				FROM binary_lifecycle bl
				WHERE bl.source_posted_at = bc.source_posted_at
				  AND bl.binary_id = bc.binary_id
				  AND bl.lifecycle_status = 'superseded'
			)`
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
			COALESCE(MAX(bic.source_release_key), '') AS source_release_key,
			COALESCE(MAX(bic.release_key), '') AS release_key,
			COALESCE(MAX(bic.release_name), '') AS release_name,
			COUNT(*)::INTEGER AS binary_count,
			COALESCE(SUM(
				CASE
					WHEN bos.observed_parts = bos.total_parts AND bos.total_parts > 0 THEN 1
					ELSE 0
				END
			), 0)::INTEGER AS complete_binary_count,
			COALESCE(SUM(
				CASE
					WHEN (bic.is_main_payload OR NOT bic.is_auxiliary)
					 AND bos.observed_parts = bos.total_parts
					 AND bos.total_parts > 0 THEN 1
					ELSE 0
				END
			), 0)::INTEGER AS complete_main_payload_binary_count,
			COALESCE(MAX(bic.expected_file_count), 0)::INTEGER AS expected_file_count,
			COALESCE(MAX(bic.expected_archive_file_count), 0)::INTEGER AS expected_archive_file_count,
			COALESCE(BOOL_OR(bic.expected_file_count > 0), FALSE) AS has_expected_file_count,
			COALESCE(BOOL_OR(bic.expected_archive_file_count > 0), FALSE) AS has_expected_archive_file_count,
			COALESCE(SUM(bos.total_bytes), 0)::BIGINT AS total_bytes,
			MIN(bos.posted_at) AS earliest_posted_at
		FROM binary_core bc
		JOIN binary_identity_current bic
		  ON bic.source_posted_at = bc.source_posted_at
		 AND bic.binary_id = bc.binary_id
		JOIN binary_observation_stats bos
		  ON bos.source_posted_at = bc.source_posted_at
		 AND bos.binary_id = bc.binary_id
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
				source_posted_at,
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
				recover_pending,
				expected_file_coverage_pct,
				archive_file_coverage_pct,
				processed_at,
				updated_at
			)
			VALUES (NOW(),$1,$2,$3,$4,'','','',0,0,0,0,0,0,FALSE,FALSE,0,NULL,'','',0,$5,FALSE,0,0,TIMESTAMPTZ 'epoch',NOW())
			ON CONFLICT (source_posted_at, provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
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
			    recover_pending = EXCLUDED.recover_pending,
			    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
			    archive_file_coverage_pct = EXCLUDED.archive_file_coverage_pct,
			    processed_at = CASE
			    	WHEN release_family_readiness_summaries.source_release_key IS DISTINCT FROM EXCLUDED.source_release_key
			    	 OR release_family_readiness_summaries.release_key IS DISTINCT FROM EXCLUDED.release_key
			    	 OR release_family_readiness_summaries.release_name IS DISTINCT FROM EXCLUDED.release_name
			    	 OR release_family_readiness_summaries.binary_count IS DISTINCT FROM EXCLUDED.binary_count
			    	 OR release_family_readiness_summaries.complete_binary_count IS DISTINCT FROM EXCLUDED.complete_binary_count
			    	 OR release_family_readiness_summaries.complete_main_payload_binary_count IS DISTINCT FROM EXCLUDED.complete_main_payload_binary_count
			    	 OR release_family_readiness_summaries.incomplete_binary_count IS DISTINCT FROM EXCLUDED.incomplete_binary_count
			    	 OR release_family_readiness_summaries.expected_file_count IS DISTINCT FROM EXCLUDED.expected_file_count
			    	 OR release_family_readiness_summaries.expected_archive_file_count IS DISTINCT FROM EXCLUDED.expected_archive_file_count
			    	 OR release_family_readiness_summaries.has_expected_file_count IS DISTINCT FROM EXCLUDED.has_expected_file_count
			    	 OR release_family_readiness_summaries.has_expected_archive_file_count IS DISTINCT FROM EXCLUDED.has_expected_archive_file_count
			    	 OR release_family_readiness_summaries.total_bytes IS DISTINCT FROM EXCLUDED.total_bytes
			    	 OR release_family_readiness_summaries.earliest_posted_at IS DISTINCT FROM EXCLUDED.earliest_posted_at
			    	 OR release_family_readiness_summaries.dominant_family_kind IS DISTINCT FROM EXCLUDED.dominant_family_kind
			    	 OR release_family_readiness_summaries.dominant_file_name IS DISTINCT FROM EXCLUDED.dominant_file_name
			    	 OR release_family_readiness_summaries.dominant_match_confidence IS DISTINCT FROM EXCLUDED.dominant_match_confidence
			    	 OR release_family_readiness_summaries.readiness_bucket IS DISTINCT FROM EXCLUDED.readiness_bucket
			    	 OR release_family_readiness_summaries.recover_pending IS DISTINCT FROM EXCLUDED.recover_pending
			    	 OR release_family_readiness_summaries.expected_file_coverage_pct IS DISTINCT FROM EXCLUDED.expected_file_coverage_pct
			    	 OR release_family_readiness_summaries.archive_file_coverage_pct IS DISTINCT FROM EXCLUDED.archive_file_coverage_pct
			    	THEN COALESCE(release_family_readiness_summaries.processed_at, release_family_readiness_summaries.updated_at)
			    	ELSE release_family_readiness_summaries.processed_at
			    END,
			    updated_at = CASE
			    	WHEN release_family_readiness_summaries.source_release_key IS DISTINCT FROM EXCLUDED.source_release_key
			    	 OR release_family_readiness_summaries.release_key IS DISTINCT FROM EXCLUDED.release_key
			    	 OR release_family_readiness_summaries.release_name IS DISTINCT FROM EXCLUDED.release_name
			    	 OR release_family_readiness_summaries.binary_count IS DISTINCT FROM EXCLUDED.binary_count
			    	 OR release_family_readiness_summaries.complete_binary_count IS DISTINCT FROM EXCLUDED.complete_binary_count
			    	 OR release_family_readiness_summaries.complete_main_payload_binary_count IS DISTINCT FROM EXCLUDED.complete_main_payload_binary_count
			    	 OR release_family_readiness_summaries.incomplete_binary_count IS DISTINCT FROM EXCLUDED.incomplete_binary_count
			    	 OR release_family_readiness_summaries.expected_file_count IS DISTINCT FROM EXCLUDED.expected_file_count
			    	 OR release_family_readiness_summaries.expected_archive_file_count IS DISTINCT FROM EXCLUDED.expected_archive_file_count
			    	 OR release_family_readiness_summaries.has_expected_file_count IS DISTINCT FROM EXCLUDED.has_expected_file_count
			    	 OR release_family_readiness_summaries.has_expected_archive_file_count IS DISTINCT FROM EXCLUDED.has_expected_archive_file_count
			    	 OR release_family_readiness_summaries.total_bytes IS DISTINCT FROM EXCLUDED.total_bytes
			    	 OR release_family_readiness_summaries.earliest_posted_at IS DISTINCT FROM EXCLUDED.earliest_posted_at
			    	 OR release_family_readiness_summaries.dominant_family_kind IS DISTINCT FROM EXCLUDED.dominant_family_kind
			    	 OR release_family_readiness_summaries.dominant_file_name IS DISTINCT FROM EXCLUDED.dominant_file_name
			    	 OR release_family_readiness_summaries.dominant_match_confidence IS DISTINCT FROM EXCLUDED.dominant_match_confidence
			    	 OR release_family_readiness_summaries.readiness_bucket IS DISTINCT FROM EXCLUDED.readiness_bucket
			    	 OR release_family_readiness_summaries.recover_pending IS DISTINCT FROM EXCLUDED.recover_pending
			    	 OR release_family_readiness_summaries.expected_file_coverage_pct IS DISTINCT FROM EXCLUDED.expected_file_coverage_pct
			    	 OR release_family_readiness_summaries.archive_file_coverage_pct IS DISTINCT FROM EXCLUDED.archive_file_coverage_pct
			    	THEN NOW()
			    	ELSE release_family_readiness_summaries.updated_at
			    END`,
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
			COALESCE(bic.family_kind, ''),
			COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), ''),
			COALESCE(bic.match_confidence, 0)
		FROM binary_core bc
		JOIN binary_identity_current bic
		  ON bic.source_posted_at = bc.source_posted_at
		 AND bic.binary_id = bc.binary_id
		JOIN binary_observation_stats bos
		  ON bos.source_posted_at = bc.source_posted_at
		 AND bos.binary_id = bc.binary_id
		WHERE ` + whereClause + `
		ORDER BY
			CASE WHEN (bic.is_main_payload OR NOT bic.is_auxiliary) THEN 0 ELSE 1 END ASC,
			CASE WHEN bos.total_parts > 0 AND bos.observed_parts = bos.total_parts THEN 0 ELSE 1 END ASC,
			bos.observed_parts DESC,
			bos.total_bytes DESC,
			bic.match_confidence DESC,
			bc.binary_id ASC
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
			source_posted_at,
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
			recover_pending,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			processed_at,
			updated_at
		)
		VALUES (COALESCE($17::timestamptz, NOW()),$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,FALSE,$22,$23,TIMESTAMPTZ 'epoch',NOW())
		ON CONFLICT (source_posted_at, provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
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
		    recover_pending = EXCLUDED.recover_pending,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    archive_file_coverage_pct = EXCLUDED.archive_file_coverage_pct,
		    processed_at = CASE
		    	WHEN release_family_readiness_summaries.source_release_key IS DISTINCT FROM EXCLUDED.source_release_key
		    	 OR release_family_readiness_summaries.release_key IS DISTINCT FROM EXCLUDED.release_key
		    	 OR release_family_readiness_summaries.release_name IS DISTINCT FROM EXCLUDED.release_name
		    	 OR release_family_readiness_summaries.binary_count IS DISTINCT FROM EXCLUDED.binary_count
		    	 OR release_family_readiness_summaries.complete_binary_count IS DISTINCT FROM EXCLUDED.complete_binary_count
		    	 OR release_family_readiness_summaries.complete_main_payload_binary_count IS DISTINCT FROM EXCLUDED.complete_main_payload_binary_count
		    	 OR release_family_readiness_summaries.incomplete_binary_count IS DISTINCT FROM EXCLUDED.incomplete_binary_count
		    	 OR release_family_readiness_summaries.expected_file_count IS DISTINCT FROM EXCLUDED.expected_file_count
		    	 OR release_family_readiness_summaries.expected_archive_file_count IS DISTINCT FROM EXCLUDED.expected_archive_file_count
		    	 OR release_family_readiness_summaries.has_expected_file_count IS DISTINCT FROM EXCLUDED.has_expected_file_count
		    	 OR release_family_readiness_summaries.has_expected_archive_file_count IS DISTINCT FROM EXCLUDED.has_expected_archive_file_count
		    	 OR release_family_readiness_summaries.total_bytes IS DISTINCT FROM EXCLUDED.total_bytes
		    	 OR release_family_readiness_summaries.earliest_posted_at IS DISTINCT FROM EXCLUDED.earliest_posted_at
		    	 OR release_family_readiness_summaries.dominant_family_kind IS DISTINCT FROM EXCLUDED.dominant_family_kind
		    	 OR release_family_readiness_summaries.dominant_file_name IS DISTINCT FROM EXCLUDED.dominant_file_name
		    	 OR release_family_readiness_summaries.dominant_match_confidence IS DISTINCT FROM EXCLUDED.dominant_match_confidence
		    	 OR release_family_readiness_summaries.readiness_bucket IS DISTINCT FROM EXCLUDED.readiness_bucket
		    	 OR release_family_readiness_summaries.recover_pending IS DISTINCT FROM EXCLUDED.recover_pending
		    	 OR release_family_readiness_summaries.expected_file_coverage_pct IS DISTINCT FROM EXCLUDED.expected_file_coverage_pct
		    	 OR release_family_readiness_summaries.archive_file_coverage_pct IS DISTINCT FROM EXCLUDED.archive_file_coverage_pct
		    	THEN COALESCE(release_family_readiness_summaries.processed_at, release_family_readiness_summaries.updated_at)
		    	ELSE release_family_readiness_summaries.processed_at
		    END,
		    updated_at = CASE
		    	WHEN release_family_readiness_summaries.source_release_key IS DISTINCT FROM EXCLUDED.source_release_key
		    	 OR release_family_readiness_summaries.release_key IS DISTINCT FROM EXCLUDED.release_key
		    	 OR release_family_readiness_summaries.release_name IS DISTINCT FROM EXCLUDED.release_name
		    	 OR release_family_readiness_summaries.binary_count IS DISTINCT FROM EXCLUDED.binary_count
		    	 OR release_family_readiness_summaries.complete_binary_count IS DISTINCT FROM EXCLUDED.complete_binary_count
		    	 OR release_family_readiness_summaries.complete_main_payload_binary_count IS DISTINCT FROM EXCLUDED.complete_main_payload_binary_count
		    	 OR release_family_readiness_summaries.incomplete_binary_count IS DISTINCT FROM EXCLUDED.incomplete_binary_count
		    	 OR release_family_readiness_summaries.expected_file_count IS DISTINCT FROM EXCLUDED.expected_file_count
		    	 OR release_family_readiness_summaries.expected_archive_file_count IS DISTINCT FROM EXCLUDED.expected_archive_file_count
		    	 OR release_family_readiness_summaries.has_expected_file_count IS DISTINCT FROM EXCLUDED.has_expected_file_count
		    	 OR release_family_readiness_summaries.has_expected_archive_file_count IS DISTINCT FROM EXCLUDED.has_expected_archive_file_count
		    	 OR release_family_readiness_summaries.total_bytes IS DISTINCT FROM EXCLUDED.total_bytes
		    	 OR release_family_readiness_summaries.earliest_posted_at IS DISTINCT FROM EXCLUDED.earliest_posted_at
		    	 OR release_family_readiness_summaries.dominant_family_kind IS DISTINCT FROM EXCLUDED.dominant_family_kind
		    	 OR release_family_readiness_summaries.dominant_file_name IS DISTINCT FROM EXCLUDED.dominant_file_name
		    	 OR release_family_readiness_summaries.dominant_match_confidence IS DISTINCT FROM EXCLUDED.dominant_match_confidence
		    	 OR release_family_readiness_summaries.readiness_bucket IS DISTINCT FROM EXCLUDED.readiness_bucket
		    	 OR release_family_readiness_summaries.recover_pending IS DISTINCT FROM EXCLUDED.recover_pending
		    	 OR release_family_readiness_summaries.expected_file_coverage_pct IS DISTINCT FROM EXCLUDED.expected_file_coverage_pct
		    	 OR release_family_readiness_summaries.archive_file_coverage_pct IS DISTINCT FROM EXCLUDED.archive_file_coverage_pct
		    	THEN NOW()
		    	ELSE release_family_readiness_summaries.updated_at
		    END`,
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

func refreshReleaseFamilySummariesBatch(ctx context.Context, tx *sql.Tx, keys []releaseFamilySummaryKey) error {
	if tx == nil {
		return fmt.Errorf("release family summary tx is required")
	}
	if len(keys) == 0 {
		return nil
	}

	args := make([]any, 0, len(keys)*4)
	values := make([]string, 0, len(keys))
	for i, key := range keys {
		base := (i * 4) + 1
		values = append(values, fmt.Sprintf("($%d::bigint,$%d::bigint,$%d::text,$%d::text)", base, base+1, base+2, base+3))
		args = append(args, key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey)
	}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		WITH requested(provider_id, newsgroup_id, key_kind, family_key) AS (
			VALUES %s
		),
		aggregates AS (
			SELECT
				r.provider_id,
				r.newsgroup_id,
				r.key_kind,
				r.family_key,
				COALESCE(MAX(bic.source_release_key), '') AS source_release_key,
				COALESCE(MAX(bic.release_key), '') AS release_key,
				COALESCE(MAX(bic.release_name), '') AS release_name,
				COUNT(bic.binary_id)::INTEGER AS binary_count,
				COALESCE(SUM(
					CASE
						WHEN bos.observed_parts = bos.total_parts AND bos.total_parts > 0 THEN 1
						ELSE 0
					END
				), 0)::INTEGER AS complete_binary_count,
				COALESCE(SUM(
					CASE
						WHEN (bic.is_main_payload OR NOT bic.is_auxiliary)
						 AND bos.observed_parts = bos.total_parts
						 AND bos.total_parts > 0 THEN 1
						ELSE 0
					END
				), 0)::INTEGER AS complete_main_payload_binary_count,
				COALESCE(MAX(bic.expected_file_count), 0)::INTEGER AS expected_file_count,
				COALESCE(MAX(bic.expected_archive_file_count), 0)::INTEGER AS expected_archive_file_count,
				COALESCE(BOOL_OR(bic.expected_file_count > 0), FALSE) AS has_expected_file_count,
				COALESCE(BOOL_OR(bic.expected_archive_file_count > 0), FALSE) AS has_expected_archive_file_count,
				COALESCE(SUM(bos.total_bytes), 0)::BIGINT AS total_bytes,
				MIN(bos.posted_at) AS earliest_posted_at
			FROM requested r
			LEFT JOIN binary_identity_current bic
			  ON bic.provider_id = r.provider_id
			 AND bic.newsgroup_id = r.newsgroup_id
			 AND (
				(r.key_kind = 'release_family' AND bic.release_family_key = r.family_key)
				OR
				(r.key_kind = 'base_stem'
				 AND GREATEST(bic.expected_file_count, bic.expected_archive_file_count) > 1
				 AND BTRIM(bic.base_stem) <> ''
				 AND LOWER(BTRIM(bic.base_stem)) = r.family_key)
				 )
				 AND NOT EXISTS (
					SELECT 1
					FROM binary_lifecycle bl
					WHERE bl.source_posted_at = bic.source_posted_at
			  AND bl.binary_id = bic.binary_id
			  AND bl.lifecycle_status = 'superseded'
				 )
			LEFT JOIN binary_core bc ON bc.binary_id = bic.binary_id
			LEFT JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bic.source_posted_at
			 AND bos.binary_id = bic.binary_id
			GROUP BY r.provider_id, r.newsgroup_id, r.key_kind, r.family_key
		),
		dominant AS (
			SELECT
				provider_id,
				newsgroup_id,
				key_kind,
				family_key,
				COALESCE(family_kind, '') AS dominant_family_kind,
				COALESCE(NULLIF(file_name, ''), NULLIF(binary_name, ''), '') AS dominant_file_name,
				COALESCE(match_confidence, 0)::DOUBLE PRECISION AS dominant_match_confidence
			FROM (
				SELECT
					r.provider_id,
					r.newsgroup_id,
					r.key_kind,
					r.family_key,
					bic.family_kind,
					bic.file_name,
					bic.binary_name,
					bic.match_confidence,
					ROW_NUMBER() OVER (
						PARTITION BY r.provider_id, r.newsgroup_id, r.key_kind, r.family_key
						ORDER BY
							CASE WHEN (COALESCE(bic.is_main_payload, FALSE) OR NOT COALESCE(bic.is_auxiliary, FALSE)) THEN 0 ELSE 1 END ASC,
							CASE WHEN COALESCE(bos.total_parts, 0) > 0 AND COALESCE(bos.observed_parts, 0) = COALESCE(bos.total_parts, 0) THEN 0 ELSE 1 END ASC,
							COALESCE(bos.observed_parts, 0) DESC,
							COALESCE(bos.total_bytes, 0) DESC,
							COALESCE(bic.match_confidence, 0) DESC,
							COALESCE(bc.binary_id, 0) ASC
					) AS row_num
				FROM requested r
				LEFT JOIN binary_identity_current bic
				  ON bic.provider_id = r.provider_id
				 AND bic.newsgroup_id = r.newsgroup_id
				 AND (
					(r.key_kind = 'release_family' AND bic.release_family_key = r.family_key)
					OR
					(r.key_kind = 'base_stem'
					 AND GREATEST(bic.expected_file_count, bic.expected_archive_file_count) > 1
					 AND BTRIM(bic.base_stem) <> ''
					 AND LOWER(BTRIM(bic.base_stem)) = r.family_key)
				 )
				 AND NOT EXISTS (
					SELECT 1
					FROM binary_lifecycle bl
					WHERE bl.source_posted_at = bic.source_posted_at
			  AND bl.binary_id = bic.binary_id
			  AND bl.lifecycle_status = 'superseded'
				 )
				LEFT JOIN binary_core bc ON bc.binary_id = bic.binary_id
				LEFT JOIN binary_observation_stats bos
				  ON bos.source_posted_at = bic.source_posted_at
				 AND bos.binary_id = bic.binary_id
			) ranked
			WHERE row_num = 1
		)
		SELECT
			a.provider_id,
			a.newsgroup_id,
			a.key_kind,
			a.family_key,
			a.source_release_key,
			a.release_key,
			a.release_name,
			a.binary_count,
			a.complete_binary_count,
			a.complete_main_payload_binary_count,
			a.expected_file_count,
			a.expected_archive_file_count,
			a.has_expected_file_count,
			a.has_expected_archive_file_count,
			a.total_bytes,
			a.earliest_posted_at,
			COALESCE(d.dominant_family_kind, ''),
			COALESCE(d.dominant_file_name, ''),
			COALESCE(d.dominant_match_confidence, 0)
		FROM aggregates a
		LEFT JOIN dominant d
		  ON d.provider_id = a.provider_id
		 AND d.newsgroup_id = a.newsgroup_id
		 AND d.key_kind = a.key_kind
		 AND d.family_key = a.family_key
		ORDER BY a.provider_id, a.newsgroup_id, a.key_kind, a.family_key`,
		strings.Join(values, ",")), args...)
	if err != nil {
		return fmt.Errorf("query release family summary batch count=%d: %w", len(keys), err)
	}
	defer rows.Close()

	summaries := make([]releaseFamilySummaryRow, 0, len(keys))
	for rows.Next() {
		var row releaseFamilySummaryRow
		if err := rows.Scan(
			&row.ProviderID,
			&row.NewsgroupID,
			&row.KeyKind,
			&row.FamilyKey,
			&row.SourceReleaseKey,
			&row.ReleaseKey,
			&row.ReleaseName,
			&row.BinaryCount,
			&row.CompleteBinaryCount,
			&row.CompleteMainPayloadBinaryCount,
			&row.ExpectedFileCount,
			&row.ExpectedArchiveFileCount,
			&row.HasExpectedFileCount,
			&row.HasExpectedArchiveFileCount,
			&row.TotalBytes,
			&row.EarliestPostedAt,
			&row.DominantFamilyKind,
			&row.DominantFileName,
			&row.DominantMatchConfidence,
		); err != nil {
			return fmt.Errorf("scan release family summary batch row: %w", err)
		}
		summaries = append(summaries, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate release family summary batch rows: %w", err)
	}
	if len(summaries) == 0 {
		return nil
	}

	return mergeReleaseFamilySummaryRows(ctx, tx, summaries)
}

func releaseFamilySummaryBatchValues(keys []releaseFamilySummaryKey) (string, []any) {
	args := make([]any, 0, len(keys)*4)
	values := make([]string, 0, len(keys))
	for i, key := range keys {
		base := (i * 4) + 1
		values = append(values, fmt.Sprintf("($%d::bigint,$%d::bigint,$%d::text,$%d::text)", base, base+1, base+2, base+3))
		args = append(args, key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey)
	}
	return strings.Join(values, ","), args
}

func releaseSummarySourcePostedAt(t sql.NullTime) time.Time {
	if t.Valid {
		return t.Time.UTC()
	}
	return time.Now().UTC()
}

func buildReleaseFamilySummaryRefreshRecord(row releaseFamilySummaryRow) []any {
	readinessBucket := releaseReadinessFragmentOnly
	if row.BinaryCount == 0 {
		readinessBucket = releaseReadinessStaleCleanupOnly
	} else if row.CompleteMainPayloadBinaryCount > 0 {
		readinessBucket = releaseReadinessActionable
	}
	if row.BinaryCount == 1 &&
		row.CompleteMainPayloadBinaryCount == 1 &&
		row.ExpectedFileCount <= 0 &&
		row.ExpectedArchiveFileCount <= 0 &&
		!summaryAllowsStandaloneBinaryRelease(row.DominantFamilyKind, row.DominantFileName, row.DominantMatchConfidence) {
		readinessBucket = releaseReadinessWeakSingle
	}
	if readinessBucket == releaseReadinessActionable && summaryIsWeakObfuscatedFamily(row.DominantFamilyKind) {
		readinessBucket = releaseReadinessWeakObfuscated
	}
	expectedFileCoveragePct := 0.0
	if row.ExpectedFileCount > 0 {
		expectedFileCoveragePct = (float64(row.CompleteBinaryCount) / float64(row.ExpectedFileCount)) * 100
		if expectedFileCoveragePct > 100 {
			expectedFileCoveragePct = 100
		}
	}
	archiveFileCoveragePct := 0.0
	if row.ExpectedArchiveFileCount > 0 {
		archiveFileCoveragePct = (float64(row.CompleteMainPayloadBinaryCount) / float64(row.ExpectedArchiveFileCount)) * 100
		if archiveFileCoveragePct > 100 {
			archiveFileCoveragePct = 100
		}
	}

	var earliestPostedAtValue any
	if row.EarliestPostedAt.Valid {
		t := row.EarliestPostedAt.Time.UTC()
		earliestPostedAtValue = t
	}

	return []any{
		releaseSummarySourcePostedAt(row.EarliestPostedAt),
		row.ProviderID,
		row.NewsgroupID,
		row.KeyKind,
		row.FamilyKey,
		row.SourceReleaseKey,
		row.ReleaseKey,
		row.ReleaseName,
		row.BinaryCount,
		row.CompleteBinaryCount,
		row.CompleteMainPayloadBinaryCount,
		row.BinaryCount - row.CompleteBinaryCount,
		row.ExpectedFileCount,
		row.ExpectedArchiveFileCount,
		row.HasExpectedFileCount,
		row.HasExpectedArchiveFileCount,
		row.TotalBytes,
		earliestPostedAtValue,
		row.DominantFamilyKind,
		row.DominantFileName,
		row.DominantMatchConfidence,
		readinessBucket,
		row.RecoverPending,
		expectedFileCoveragePct,
		archiveFileCoveragePct,
	}
}

func refreshReleaseFamilySummariesBatchCopy(ctx context.Context, conn *sql.Conn, keys []releaseFamilySummaryKey) error {
	_, err := refreshReleaseFamilySummariesBatchCopyWithMetrics(ctx, conn, keys)
	return err
}

func refreshReleaseFamilySummariesBatchCopyWithMetrics(ctx context.Context, conn *sql.Conn, keys []releaseFamilySummaryKey) (releaseSummaryRefreshMetrics, error) {
	metrics := releaseSummaryRefreshMetrics{}
	if conn == nil {
		return metrics, fmt.Errorf("release family summary conn is required")
	}
	if len(keys) == 0 {
		return metrics, nil
	}

	for start := 0; start < len(keys); start += releaseFamilySummaryRefreshQueryBatchCap {
		end := start + releaseFamilySummaryRefreshQueryBatchCap
		if end > len(keys) {
			end = len(keys)
		}
		chunkMetrics, err := refreshReleaseFamilySummariesBatchCopyChunkWithMetrics(ctx, conn, keys[start:end])
		if err != nil {
			return metrics, err
		}
		metrics.SummaryAggregateDuration += chunkMetrics.SummaryAggregateDuration
		metrics.SummaryDominantDuration += chunkMetrics.SummaryDominantDuration
		metrics.SummaryRefreshDuration += chunkMetrics.SummaryAggregateDuration + chunkMetrics.SummaryDominantDuration
	}
	return metrics, nil
}

func refreshReleaseFamilySummariesBatchCopyChunk(ctx context.Context, conn *sql.Conn, keys []releaseFamilySummaryKey) error {
	_, err := refreshReleaseFamilySummariesBatchCopyChunkWithMetrics(ctx, conn, keys)
	return err
}

func refreshReleaseFamilySummariesBatchCopyChunkWithMetrics(ctx context.Context, conn *sql.Conn, keys []releaseFamilySummaryKey) (releaseSummaryRefreshMetrics, error) {
	metrics := releaseSummaryRefreshMetrics{}
	if conn == nil {
		return metrics, fmt.Errorf("release family summary conn is required")
	}
	if len(keys) == 0 {
		return metrics, nil
	}

	values, args := releaseFamilySummaryBatchValues(keys)
	aggregateStart := time.Now()
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		WITH requested(provider_id, newsgroup_id, key_kind, family_key) AS MATERIALIZED (
			VALUES %s
		),
		matched AS MATERIALIZED (
			SELECT
				r.provider_id,
				r.newsgroup_id,
				r.key_kind,
				r.family_key,
				bic.binary_id,
				bic.source_posted_at,
				bic.source_release_key,
				bic.release_key,
				bic.release_name,
				bic.expected_file_count,
				bic.expected_archive_file_count,
				bic.is_main_payload,
				bic.is_auxiliary,
				bic.family_kind,
				bic.file_name,
				bic.binary_name,
				bic.match_confidence,
				bc.binary_id AS core_binary_id,
				bos.observed_parts,
				bos.total_parts,
				bos.total_bytes,
				bos.posted_at
			FROM requested r
			LEFT JOIN LATERAL (
				SELECT bic.*
				FROM binary_identity_current bic
				WHERE bic.provider_id = r.provider_id
				  AND bic.newsgroup_id = r.newsgroup_id
				  AND bic.release_family_key = r.family_key
				  AND BTRIM(bic.release_family_key) <> ''
			) bic ON TRUE
			LEFT JOIN binary_lifecycle bl
			  ON bl.source_posted_at = bic.source_posted_at
			 AND bl.binary_id = bic.binary_id
			 AND bl.lifecycle_status = 'superseded'
			LEFT JOIN binary_core bc
			  ON bc.source_posted_at = bic.source_posted_at
			 AND bc.binary_id = bic.binary_id
			LEFT JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bic.source_posted_at
			 AND bos.binary_id = bic.binary_id
			WHERE bic.binary_id IS NULL
			   OR bl.binary_id IS NULL
		)
		SELECT
			r.provider_id,
			r.newsgroup_id,
			r.key_kind,
			r.family_key,
			COALESCE(MAX(m.source_release_key), '') AS source_release_key,
			COALESCE(MAX(m.release_key), '') AS release_key,
			COALESCE(MAX(m.release_name), '') AS release_name,
			COUNT(m.core_binary_id)::INTEGER AS binary_count,
			COALESCE(SUM(
				CASE
					WHEN m.observed_parts = m.total_parts AND m.total_parts > 0 THEN 1
					ELSE 0
				END
			), 0)::INTEGER AS complete_binary_count,
			COALESCE(SUM(
				CASE
					WHEN (m.is_main_payload OR NOT m.is_auxiliary)
					 AND m.observed_parts = m.total_parts
					 AND m.total_parts > 0 THEN 1
					ELSE 0
				END
			), 0)::INTEGER AS complete_main_payload_binary_count,
			COALESCE(MAX(m.expected_file_count), 0)::INTEGER AS expected_file_count,
			COALESCE(MAX(m.expected_archive_file_count), 0)::INTEGER AS expected_archive_file_count,
			COALESCE(BOOL_OR(m.expected_file_count > 0), FALSE) AS has_expected_file_count,
			COALESCE(BOOL_OR(m.expected_archive_file_count > 0), FALSE) AS has_expected_archive_file_count,
			COALESCE(SUM(m.total_bytes), 0)::BIGINT AS total_bytes,
			MIN(m.posted_at) AS earliest_posted_at
		FROM requested r
		LEFT JOIN matched m
		  ON m.provider_id = r.provider_id
		 AND m.newsgroup_id = r.newsgroup_id
		 AND m.key_kind = r.key_kind
		 AND m.family_key = r.family_key
		GROUP BY r.provider_id, r.newsgroup_id, r.key_kind, r.family_key
		ORDER BY r.provider_id, r.newsgroup_id, r.key_kind, r.family_key`,
		values), args...)
	if err != nil {
		return metrics, fmt.Errorf("query release family aggregate batch count=%d: %w", len(keys), err)
	}
	defer rows.Close()

	summaryByKey := make(map[releaseFamilySummaryKey]releaseFamilySummaryRow, len(keys))
	for rows.Next() {
		var row releaseFamilySummaryRow
		if err := rows.Scan(
			&row.ProviderID,
			&row.NewsgroupID,
			&row.KeyKind,
			&row.FamilyKey,
			&row.SourceReleaseKey,
			&row.ReleaseKey,
			&row.ReleaseName,
			&row.BinaryCount,
			&row.CompleteBinaryCount,
			&row.CompleteMainPayloadBinaryCount,
			&row.ExpectedFileCount,
			&row.ExpectedArchiveFileCount,
			&row.HasExpectedFileCount,
			&row.HasExpectedArchiveFileCount,
			&row.TotalBytes,
			&row.EarliestPostedAt,
		); err != nil {
			return metrics, fmt.Errorf("scan release family aggregate batch row: %w", err)
		}
		key := releaseFamilySummaryKey{
			ProviderID:  row.ProviderID,
			NewsgroupID: row.NewsgroupID,
			KeyKind:     row.KeyKind,
			FamilyKey:   row.FamilyKey,
		}
		summaryByKey[key] = row
	}
	if err := rows.Err(); err != nil {
		return metrics, fmt.Errorf("iterate release family aggregate batch rows: %w", err)
	}
	metrics.SummaryAggregateDuration += time.Since(aggregateStart)
	if len(summaryByKey) == 0 {
		return metrics, nil
	}

	dominantStart := time.Now()
	rows, err = conn.QueryContext(ctx, fmt.Sprintf(`
		WITH requested(provider_id, newsgroup_id, key_kind, family_key) AS MATERIALIZED (
			VALUES %s
		),
		matched AS MATERIALIZED (
			SELECT
				r.provider_id,
				r.newsgroup_id,
				r.key_kind,
				r.family_key,
				bic.binary_id,
				bic.source_posted_at,
				bic.family_kind,
				bic.file_name,
				bic.binary_name,
				bic.match_confidence,
				bic.is_main_payload,
				bic.is_auxiliary,
				bos.observed_parts,
				bos.total_parts,
				bos.total_bytes
			FROM requested r
			LEFT JOIN LATERAL (
				SELECT bic.*
				FROM binary_identity_current bic
				WHERE bic.provider_id = r.provider_id
				  AND bic.newsgroup_id = r.newsgroup_id
				  AND bic.release_family_key = r.family_key
				  AND BTRIM(bic.release_family_key) <> ''
			) bic ON TRUE
			LEFT JOIN binary_lifecycle bl
			  ON bl.source_posted_at = bic.source_posted_at
			 AND bl.binary_id = bic.binary_id
			 AND bl.lifecycle_status = 'superseded'
			LEFT JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bic.source_posted_at
			 AND bos.binary_id = bic.binary_id
			WHERE bic.binary_id IS NULL
			   OR bl.binary_id IS NULL
		)
		SELECT DISTINCT ON (r.provider_id, r.newsgroup_id, r.key_kind, r.family_key)
			r.provider_id,
			r.newsgroup_id,
			r.key_kind,
			r.family_key,
			COALESCE(m.family_kind, '') AS dominant_family_kind,
			COALESCE(NULLIF(m.file_name, ''), NULLIF(m.binary_name, ''), '') AS dominant_file_name,
			COALESCE(m.match_confidence, 0)::DOUBLE PRECISION AS dominant_match_confidence
		FROM requested r
		LEFT JOIN matched m
		  ON m.provider_id = r.provider_id
		 AND m.newsgroup_id = r.newsgroup_id
		 AND m.key_kind = r.key_kind
		 AND m.family_key = r.family_key
		ORDER BY
			r.provider_id,
			r.newsgroup_id,
			r.key_kind,
			r.family_key,
			CASE WHEN (COALESCE(m.is_main_payload, FALSE) OR NOT COALESCE(m.is_auxiliary, FALSE)) THEN 0 ELSE 1 END ASC,
			CASE WHEN COALESCE(m.total_parts, 0) > 0 AND COALESCE(m.observed_parts, 0) = COALESCE(m.total_parts, 0) THEN 0 ELSE 1 END ASC,
			COALESCE(m.observed_parts, 0) DESC,
			COALESCE(m.total_bytes, 0) DESC,
			COALESCE(m.match_confidence, 0) DESC,
			COALESCE(m.binary_id, 0) ASC`,
		values), args...)
	if err != nil {
		return metrics, fmt.Errorf("query release family dominant batch count=%d: %w", len(keys), err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			key                     releaseFamilySummaryKey
			dominantFamilyKind      string
			dominantFileName        string
			dominantMatchConfidence float64
		)
		if err := rows.Scan(
			&key.ProviderID,
			&key.NewsgroupID,
			&key.KeyKind,
			&key.FamilyKey,
			&dominantFamilyKind,
			&dominantFileName,
			&dominantMatchConfidence,
		); err != nil {
			return metrics, fmt.Errorf("scan release family dominant batch row: %w", err)
		}
		row, ok := summaryByKey[key]
		if !ok {
			continue
		}
		row.DominantFamilyKind = dominantFamilyKind
		row.DominantFileName = dominantFileName
		row.DominantMatchConfidence = dominantMatchConfidence
		summaryByKey[key] = row
	}
	if err := rows.Err(); err != nil {
		return metrics, fmt.Errorf("iterate release family dominant batch rows: %w", err)
	}
	metrics.SummaryDominantDuration += time.Since(dominantStart)

	summaries := make([]releaseFamilySummaryRow, 0, len(summaryByKey))
	for _, key := range keys {
		if row, ok := summaryByKey[key]; ok {
			summaries = append(summaries, row)
		}
	}
	if len(summaries) == 0 {
		return metrics, nil
	}

	if err := mergeReleaseFamilySummaryRows(ctx, conn, summaries); err != nil {
		return metrics, err
	}
	return metrics, nil
}

func refreshBaseStemSummariesBatchCopyWithMetrics(ctx context.Context, conn *sql.Conn, keys []releaseFamilySummaryKey) (releaseSummaryRefreshMetrics, error) {
	metrics := releaseSummaryRefreshMetrics{}
	if conn == nil {
		return metrics, fmt.Errorf("release family summary conn is required")
	}
	if len(keys) == 0 {
		return metrics, nil
	}

	for start := 0; start < len(keys); start += releaseFamilySummaryRefreshQueryBatchCap {
		end := start + releaseFamilySummaryRefreshQueryBatchCap
		if end > len(keys) {
			end = len(keys)
		}
		chunkMetrics, err := refreshBaseStemSummariesBatchCopyChunkWithMetrics(ctx, conn, keys[start:end])
		if err != nil {
			return metrics, err
		}
		metrics.SummaryAggregateDuration += chunkMetrics.SummaryAggregateDuration
		metrics.SummaryDominantDuration += chunkMetrics.SummaryDominantDuration
		metrics.SummaryRefreshDuration += chunkMetrics.SummaryAggregateDuration + chunkMetrics.SummaryDominantDuration
	}
	return metrics, nil
}

func refreshBaseStemSummariesBatchCopyChunkWithMetrics(ctx context.Context, conn *sql.Conn, keys []releaseFamilySummaryKey) (releaseSummaryRefreshMetrics, error) {
	metrics := releaseSummaryRefreshMetrics{}
	if conn == nil {
		return metrics, fmt.Errorf("release family summary conn is required")
	}
	if len(keys) == 0 {
		return metrics, nil
	}

	values, args := releaseFamilySummaryBatchValues(keys)
	aggregateStart := time.Now()
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		WITH requested(provider_id, newsgroup_id, key_kind, family_key) AS MATERIALIZED (
			VALUES %s
		),
		matched AS MATERIALIZED (
			SELECT
				r.provider_id,
				r.newsgroup_id,
				r.key_kind,
				r.family_key,
				bic.binary_id,
				bic.source_posted_at,
				bic.source_release_key,
				bic.release_key,
				bic.release_name,
				bic.expected_file_count,
				bic.expected_archive_file_count,
				bic.is_main_payload,
				bic.is_auxiliary,
				bic.family_kind,
				bic.file_name,
				bic.binary_name,
				bic.match_confidence,
				bc.binary_id AS core_binary_id,
				bos.observed_parts,
				bos.total_parts,
				bos.total_bytes,
				bos.posted_at
			FROM requested r
			LEFT JOIN LATERAL (
				SELECT bic.*
				FROM binary_identity_current bic
				WHERE bic.provider_id = r.provider_id
				  AND bic.newsgroup_id = r.newsgroup_id
				  AND GREATEST(bic.expected_file_count, bic.expected_archive_file_count) > 1
				  AND BTRIM(bic.base_stem) <> ''
				  AND LOWER(BTRIM(bic.base_stem)) = r.family_key
			) bic ON TRUE
			LEFT JOIN binary_lifecycle bl
			  ON bl.source_posted_at = bic.source_posted_at
			 AND bl.binary_id = bic.binary_id
			 AND bl.lifecycle_status = 'superseded'
			LEFT JOIN binary_core bc
			  ON bc.source_posted_at = bic.source_posted_at
			 AND bc.binary_id = bic.binary_id
			LEFT JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bic.source_posted_at
			 AND bos.binary_id = bic.binary_id
			WHERE bic.binary_id IS NULL
			   OR bl.binary_id IS NULL
		)
		SELECT
			r.provider_id,
			r.newsgroup_id,
			r.key_kind,
			r.family_key,
			COALESCE(MAX(m.source_release_key), '') AS source_release_key,
			COALESCE(MAX(m.release_key), '') AS release_key,
			COALESCE(MAX(m.release_name), '') AS release_name,
			COUNT(m.core_binary_id)::INTEGER AS binary_count,
			COALESCE(SUM(
				CASE
					WHEN m.observed_parts = m.total_parts AND m.total_parts > 0 THEN 1
					ELSE 0
				END
			), 0)::INTEGER AS complete_binary_count,
			COALESCE(SUM(
				CASE
					WHEN (m.is_main_payload OR NOT m.is_auxiliary)
					 AND m.observed_parts = m.total_parts
					 AND m.total_parts > 0 THEN 1
					ELSE 0
				END
			), 0)::INTEGER AS complete_main_payload_binary_count,
			COALESCE(MAX(m.expected_file_count), 0)::INTEGER AS expected_file_count,
			COALESCE(MAX(m.expected_archive_file_count), 0)::INTEGER AS expected_archive_file_count,
			COALESCE(BOOL_OR(m.expected_file_count > 0), FALSE) AS has_expected_file_count,
			COALESCE(BOOL_OR(m.expected_archive_file_count > 0), FALSE) AS has_expected_archive_file_count,
			COALESCE(SUM(m.total_bytes), 0)::BIGINT AS total_bytes,
			MIN(m.posted_at) AS earliest_posted_at
		FROM requested r
		LEFT JOIN matched m
		  ON m.provider_id = r.provider_id
		 AND m.newsgroup_id = r.newsgroup_id
		 AND m.key_kind = r.key_kind
		 AND m.family_key = r.family_key
		GROUP BY r.provider_id, r.newsgroup_id, r.key_kind, r.family_key
		ORDER BY r.provider_id, r.newsgroup_id, r.key_kind, r.family_key`,
		values), args...)
	if err != nil {
		return metrics, fmt.Errorf("query base stem aggregate batch count=%d: %w", len(keys), err)
	}
	defer rows.Close()

	summaryByKey := make(map[releaseFamilySummaryKey]releaseFamilySummaryRow, len(keys))
	for rows.Next() {
		var row releaseFamilySummaryRow
		if err := rows.Scan(
			&row.ProviderID,
			&row.NewsgroupID,
			&row.KeyKind,
			&row.FamilyKey,
			&row.SourceReleaseKey,
			&row.ReleaseKey,
			&row.ReleaseName,
			&row.BinaryCount,
			&row.CompleteBinaryCount,
			&row.CompleteMainPayloadBinaryCount,
			&row.ExpectedFileCount,
			&row.ExpectedArchiveFileCount,
			&row.HasExpectedFileCount,
			&row.HasExpectedArchiveFileCount,
			&row.TotalBytes,
			&row.EarliestPostedAt,
		); err != nil {
			return metrics, fmt.Errorf("scan base stem aggregate batch row: %w", err)
		}
		key := releaseFamilySummaryKey{
			ProviderID:  row.ProviderID,
			NewsgroupID: row.NewsgroupID,
			KeyKind:     row.KeyKind,
			FamilyKey:   row.FamilyKey,
		}
		summaryByKey[key] = row
	}
	if err := rows.Err(); err != nil {
		return metrics, fmt.Errorf("iterate base stem aggregate batch rows: %w", err)
	}
	metrics.SummaryAggregateDuration += time.Since(aggregateStart)
	if len(summaryByKey) == 0 {
		return metrics, nil
	}

	dominantStart := time.Now()
	rows, err = conn.QueryContext(ctx, fmt.Sprintf(`
		WITH requested(provider_id, newsgroup_id, key_kind, family_key) AS MATERIALIZED (
			VALUES %s
		),
		matched AS MATERIALIZED (
			SELECT
				r.provider_id,
				r.newsgroup_id,
				r.key_kind,
				r.family_key,
				bic.binary_id,
				bic.source_posted_at,
				bic.family_kind,
				bic.file_name,
				bic.binary_name,
				bic.match_confidence,
				bic.is_main_payload,
				bic.is_auxiliary,
				bos.observed_parts,
				bos.total_parts,
				bos.total_bytes
			FROM requested r
			LEFT JOIN LATERAL (
				SELECT bic.*
				FROM binary_identity_current bic
				WHERE bic.provider_id = r.provider_id
				  AND bic.newsgroup_id = r.newsgroup_id
				  AND GREATEST(bic.expected_file_count, bic.expected_archive_file_count) > 1
				  AND BTRIM(bic.base_stem) <> ''
				  AND LOWER(BTRIM(bic.base_stem)) = r.family_key
			) bic ON TRUE
			LEFT JOIN binary_lifecycle bl
			  ON bl.source_posted_at = bic.source_posted_at
			 AND bl.binary_id = bic.binary_id
			 AND bl.lifecycle_status = 'superseded'
			LEFT JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bic.source_posted_at
			 AND bos.binary_id = bic.binary_id
			WHERE bic.binary_id IS NULL
			   OR bl.binary_id IS NULL
		)
		SELECT DISTINCT ON (r.provider_id, r.newsgroup_id, r.key_kind, r.family_key)
			r.provider_id,
			r.newsgroup_id,
			r.key_kind,
			r.family_key,
			COALESCE(m.family_kind, '') AS dominant_family_kind,
			COALESCE(NULLIF(m.file_name, ''), NULLIF(m.binary_name, ''), '') AS dominant_file_name,
			COALESCE(m.match_confidence, 0)::DOUBLE PRECISION AS dominant_match_confidence
		FROM requested r
		LEFT JOIN matched m
		  ON m.provider_id = r.provider_id
		 AND m.newsgroup_id = r.newsgroup_id
		 AND m.key_kind = r.key_kind
		 AND m.family_key = r.family_key
		ORDER BY
			r.provider_id,
			r.newsgroup_id,
			r.key_kind,
			r.family_key,
			CASE WHEN (COALESCE(m.is_main_payload, FALSE) OR NOT COALESCE(m.is_auxiliary, FALSE)) THEN 0 ELSE 1 END ASC,
			CASE WHEN COALESCE(m.total_parts, 0) > 0 AND COALESCE(m.observed_parts, 0) = COALESCE(m.total_parts, 0) THEN 0 ELSE 1 END ASC,
			COALESCE(m.observed_parts, 0) DESC,
			COALESCE(m.total_bytes, 0) DESC,
			COALESCE(m.match_confidence, 0) DESC,
			COALESCE(m.binary_id, 0) ASC`,
		values), args...)
	if err != nil {
		return metrics, fmt.Errorf("query base stem dominant batch count=%d: %w", len(keys), err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			key                     releaseFamilySummaryKey
			dominantFamilyKind      string
			dominantFileName        string
			dominantMatchConfidence float64
		)
		if err := rows.Scan(
			&key.ProviderID,
			&key.NewsgroupID,
			&key.KeyKind,
			&key.FamilyKey,
			&dominantFamilyKind,
			&dominantFileName,
			&dominantMatchConfidence,
		); err != nil {
			return metrics, fmt.Errorf("scan base stem dominant batch row: %w", err)
		}
		row, ok := summaryByKey[key]
		if !ok {
			continue
		}
		row.DominantFamilyKind = dominantFamilyKind
		row.DominantFileName = dominantFileName
		row.DominantMatchConfidence = dominantMatchConfidence
		summaryByKey[key] = row
	}
	if err := rows.Err(); err != nil {
		return metrics, fmt.Errorf("iterate base stem dominant batch rows: %w", err)
	}
	metrics.SummaryDominantDuration += time.Since(dominantStart)

	summaries := make([]releaseFamilySummaryRow, 0, len(summaryByKey))
	for _, key := range keys {
		if row, ok := summaryByKey[key]; ok {
			summaries = append(summaries, row)
		}
	}
	if len(summaries) == 0 {
		return metrics, nil
	}
	if err := mergeReleaseFamilySummaryRows(ctx, conn, summaries); err != nil {
		return metrics, err
	}
	return metrics, nil
}

func refreshReleaseFamilySummaryConn(ctx context.Context, conn *sql.Conn, key releaseFamilySummaryKey) error {
	if conn == nil {
		return fmt.Errorf("release family summary conn is required")
	}

	whereClause := `
			bc.provider_id = $1
			AND bc.newsgroup_id = $2
			AND bic.release_family_key = $3
			AND NOT EXISTS (
				SELECT 1
				FROM binary_lifecycle bl
				WHERE bl.source_posted_at = bc.source_posted_at
				  AND bl.binary_id = bc.binary_id
				  AND bl.lifecycle_status = 'superseded'
			)`
	if key.KeyKind == "base_stem" {
		whereClause = `
			bc.provider_id = $1
			AND bc.newsgroup_id = $2
			AND GREATEST(bic.expected_file_count, bic.expected_archive_file_count) > 1
			AND BTRIM(bic.base_stem) <> ''
			AND LOWER(BTRIM(bic.base_stem)) = $3
			AND NOT EXISTS (
				SELECT 1
				FROM binary_lifecycle bl
				WHERE bl.source_posted_at = bc.source_posted_at
				  AND bl.binary_id = bc.binary_id
				  AND bl.lifecycle_status = 'superseded'
			)`
	}

	var row releaseFamilySummaryRow
	query := `
		SELECT
			COALESCE(MAX(bic.source_release_key), '') AS source_release_key,
			COALESCE(MAX(bic.release_key), '') AS release_key,
			COALESCE(MAX(bic.release_name), '') AS release_name,
			COUNT(*)::INTEGER AS binary_count,
			COALESCE(SUM(
				CASE WHEN bos.observed_parts = bos.total_parts AND bos.total_parts > 0 THEN 1 ELSE 0 END
			), 0)::INTEGER AS complete_binary_count,
			COALESCE(SUM(
				CASE
					WHEN (bic.is_main_payload OR NOT bic.is_auxiliary)
					 AND bos.observed_parts = bos.total_parts
					 AND bos.total_parts > 0 THEN 1
					ELSE 0
				END
			), 0)::INTEGER AS complete_main_payload_binary_count,
			COALESCE(MAX(bic.expected_file_count), 0)::INTEGER AS expected_file_count,
			COALESCE(MAX(bic.expected_archive_file_count), 0)::INTEGER AS expected_archive_file_count,
			COALESCE(BOOL_OR(bic.expected_file_count > 0), FALSE) AS has_expected_file_count,
			COALESCE(BOOL_OR(bic.expected_archive_file_count > 0), FALSE) AS has_expected_archive_file_count,
			COALESCE(SUM(bos.total_bytes), 0)::BIGINT AS total_bytes,
			MIN(bos.posted_at) AS earliest_posted_at
		FROM binary_core bc
		JOIN binary_identity_current bic
		  ON bic.source_posted_at = bc.source_posted_at
		 AND bic.binary_id = bc.binary_id
		JOIN binary_observation_stats bos
		  ON bos.source_posted_at = bc.source_posted_at
		 AND bos.binary_id = bc.binary_id
		WHERE ` + whereClause
	if err := conn.QueryRowContext(ctx, query, key.ProviderID, key.NewsgroupID, key.FamilyKey).Scan(
		&row.SourceReleaseKey,
		&row.ReleaseKey,
		&row.ReleaseName,
		&row.BinaryCount,
		&row.CompleteBinaryCount,
		&row.CompleteMainPayloadBinaryCount,
		&row.ExpectedFileCount,
		&row.ExpectedArchiveFileCount,
		&row.HasExpectedFileCount,
		&row.HasExpectedArchiveFileCount,
		&row.TotalBytes,
		&row.EarliestPostedAt,
	); err != nil {
		return fmt.Errorf("query release family summary provider=%d group=%d kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
	}
	row.ProviderID = key.ProviderID
	row.NewsgroupID = key.NewsgroupID
	row.KeyKind = key.KeyKind
	row.FamilyKey = key.FamilyKey

	dominantQuery := `
		SELECT
			COALESCE(bic.family_kind, ''),
			COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), ''),
			COALESCE(bic.match_confidence, 0)
		FROM binary_core bc
		JOIN binary_identity_current bic
		  ON bic.source_posted_at = bc.source_posted_at
		 AND bic.binary_id = bc.binary_id
		JOIN binary_observation_stats bos
		  ON bos.source_posted_at = bc.source_posted_at
		 AND bos.binary_id = bc.binary_id
		WHERE ` + whereClause + `
		ORDER BY
			CASE WHEN (bic.is_main_payload OR NOT bic.is_auxiliary) THEN 0 ELSE 1 END ASC,
			CASE WHEN bos.total_parts > 0 AND bos.observed_parts = bos.total_parts THEN 0 ELSE 1 END ASC,
			bos.observed_parts DESC,
			bos.total_bytes DESC,
			bic.match_confidence DESC,
			bc.binary_id ASC
		LIMIT 1`
	if row.BinaryCount > 0 {
		if err := conn.QueryRowContext(ctx, dominantQuery, key.ProviderID, key.NewsgroupID, key.FamilyKey).Scan(
			&row.DominantFamilyKind,
			&row.DominantFileName,
			&row.DominantMatchConfidence,
		); err != nil {
			return fmt.Errorf("query dominant release family binary provider=%d group=%d kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
		}
	}

	return mergeReleaseFamilySummaryRows(ctx, conn, []releaseFamilySummaryRow{row})
}

func mergeReleaseFamilySummaryRows(ctx context.Context, runner sqlExecQueryer, summaries []releaseFamilySummaryRow) error {
	if runner == nil {
		return fmt.Errorf("release family summary runner is required")
	}
	if len(summaries) == 0 {
		return nil
	}

	for start := 0; start < len(summaries); start += releaseFamilySummaryMergeRowsMax {
		end := start + releaseFamilySummaryMergeRowsMax
		if end > len(summaries) {
			end = len(summaries)
		}
		batch := summaries[start:end]
		values := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*25)
		for i, row := range batch {
			record := buildReleaseFamilySummaryRefreshRecord(row)
			base := i*25 + 1
			placeholders := make([]string, 0, 25)
			for offset := range 25 {
				placeholders = append(placeholders, fmt.Sprintf("$%d", base+offset))
			}
			values = append(values, "("+strings.Join(placeholders, ",")+",TIMESTAMPTZ 'epoch',NOW())")
			args = append(args, record...)
		}

		if _, err := runner.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO release_family_readiness_summaries (
			source_posted_at,
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
			recover_pending,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			processed_at,
			updated_at
		)
		VALUES %s
		ON CONFLICT (source_posted_at, provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
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
		    recover_pending = EXCLUDED.recover_pending,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    archive_file_coverage_pct = EXCLUDED.archive_file_coverage_pct,
		    processed_at = CASE
		    	WHEN release_family_readiness_summaries.source_release_key IS DISTINCT FROM EXCLUDED.source_release_key
		    	 OR release_family_readiness_summaries.release_key IS DISTINCT FROM EXCLUDED.release_key
		    	 OR release_family_readiness_summaries.release_name IS DISTINCT FROM EXCLUDED.release_name
		    	 OR release_family_readiness_summaries.binary_count IS DISTINCT FROM EXCLUDED.binary_count
		    	 OR release_family_readiness_summaries.complete_binary_count IS DISTINCT FROM EXCLUDED.complete_binary_count
		    	 OR release_family_readiness_summaries.complete_main_payload_binary_count IS DISTINCT FROM EXCLUDED.complete_main_payload_binary_count
		    	 OR release_family_readiness_summaries.incomplete_binary_count IS DISTINCT FROM EXCLUDED.incomplete_binary_count
		    	 OR release_family_readiness_summaries.expected_file_count IS DISTINCT FROM EXCLUDED.expected_file_count
		    	 OR release_family_readiness_summaries.expected_archive_file_count IS DISTINCT FROM EXCLUDED.expected_archive_file_count
		    	 OR release_family_readiness_summaries.has_expected_file_count IS DISTINCT FROM EXCLUDED.has_expected_file_count
		    	 OR release_family_readiness_summaries.has_expected_archive_file_count IS DISTINCT FROM EXCLUDED.has_expected_archive_file_count
		    	 OR release_family_readiness_summaries.total_bytes IS DISTINCT FROM EXCLUDED.total_bytes
		    	 OR release_family_readiness_summaries.earliest_posted_at IS DISTINCT FROM EXCLUDED.earliest_posted_at
		    	 OR release_family_readiness_summaries.dominant_family_kind IS DISTINCT FROM EXCLUDED.dominant_family_kind
		    	 OR release_family_readiness_summaries.dominant_file_name IS DISTINCT FROM EXCLUDED.dominant_file_name
		    	 OR release_family_readiness_summaries.dominant_match_confidence IS DISTINCT FROM EXCLUDED.dominant_match_confidence
		    	 OR release_family_readiness_summaries.readiness_bucket IS DISTINCT FROM EXCLUDED.readiness_bucket
		    	 OR release_family_readiness_summaries.recover_pending IS DISTINCT FROM EXCLUDED.recover_pending
		    	 OR release_family_readiness_summaries.expected_file_coverage_pct IS DISTINCT FROM EXCLUDED.expected_file_coverage_pct
		    	 OR release_family_readiness_summaries.archive_file_coverage_pct IS DISTINCT FROM EXCLUDED.archive_file_coverage_pct
		    	THEN COALESCE(release_family_readiness_summaries.processed_at, release_family_readiness_summaries.updated_at)
		    	ELSE release_family_readiness_summaries.processed_at
		    END,
		    updated_at = CASE
		    	WHEN release_family_readiness_summaries.source_release_key IS DISTINCT FROM EXCLUDED.source_release_key
		    	 OR release_family_readiness_summaries.release_key IS DISTINCT FROM EXCLUDED.release_key
		    	 OR release_family_readiness_summaries.release_name IS DISTINCT FROM EXCLUDED.release_name
		    	 OR release_family_readiness_summaries.binary_count IS DISTINCT FROM EXCLUDED.binary_count
		    	 OR release_family_readiness_summaries.complete_binary_count IS DISTINCT FROM EXCLUDED.complete_binary_count
		    	 OR release_family_readiness_summaries.complete_main_payload_binary_count IS DISTINCT FROM EXCLUDED.complete_main_payload_binary_count
		    	 OR release_family_readiness_summaries.incomplete_binary_count IS DISTINCT FROM EXCLUDED.incomplete_binary_count
		    	 OR release_family_readiness_summaries.expected_file_count IS DISTINCT FROM EXCLUDED.expected_file_count
		    	 OR release_family_readiness_summaries.expected_archive_file_count IS DISTINCT FROM EXCLUDED.expected_archive_file_count
		    	 OR release_family_readiness_summaries.has_expected_file_count IS DISTINCT FROM EXCLUDED.has_expected_file_count
		    	 OR release_family_readiness_summaries.has_expected_archive_file_count IS DISTINCT FROM EXCLUDED.has_expected_archive_file_count
		    	 OR release_family_readiness_summaries.total_bytes IS DISTINCT FROM EXCLUDED.total_bytes
		    	 OR release_family_readiness_summaries.earliest_posted_at IS DISTINCT FROM EXCLUDED.earliest_posted_at
		    	 OR release_family_readiness_summaries.dominant_family_kind IS DISTINCT FROM EXCLUDED.dominant_family_kind
		    	 OR release_family_readiness_summaries.dominant_file_name IS DISTINCT FROM EXCLUDED.dominant_file_name
		    	 OR release_family_readiness_summaries.dominant_match_confidence IS DISTINCT FROM EXCLUDED.dominant_match_confidence
		    	 OR release_family_readiness_summaries.readiness_bucket IS DISTINCT FROM EXCLUDED.readiness_bucket
		    	 OR release_family_readiness_summaries.recover_pending IS DISTINCT FROM EXCLUDED.recover_pending
		    	 OR release_family_readiness_summaries.expected_file_coverage_pct IS DISTINCT FROM EXCLUDED.expected_file_coverage_pct
		    	 OR release_family_readiness_summaries.archive_file_coverage_pct IS DISTINCT FROM EXCLUDED.archive_file_coverage_pct
		    	THEN NOW()
		    	ELSE release_family_readiness_summaries.updated_at
		    END`,
			strings.Join(values, ",")), args...); err != nil {
			return fmt.Errorf("merge release family summary rows count=%d batch=%d: %w", len(batch), len(summaries), err)
		}
	}
	return nil
}

type releaseFamilyShape struct {
	AllContextual           bool
	MaxExpectedAnyFileCount int
	IndexedFileCount        int
	BaseStemFileCount       int
	DistinctBaseStemCount   int
	HasUsableFileIdentity   bool
}

func finalizeReleaseCandidateMaterialization(ctx context.Context, runner sqlExecQueryRower, keys []releaseFamilySummaryKey) error {
	if err := finalizeReleaseCandidateMaterializationWithoutRecoveredFileSets(ctx, runner, keys); err != nil {
		return err
	}
	return refreshRecoveredFileSetCandidatesForSummaryKeys(ctx, runner, keys)
}

func finalizeReleaseCandidateMaterializationWithoutRecoveredFileSets(ctx context.Context, runner sqlExecQueryRower, keys []releaseFamilySummaryKey) error {
	if runner == nil {
		return fmt.Errorf("release family summary runner is required")
	}
	if len(keys) == 0 {
		return nil
	}

	normalized := make([]releaseFamilySummaryKey, 0, len(keys))
	seen := make(map[releaseFamilySummaryKey]struct{}, len(keys))
	for _, candidate := range keys {
		key, ok := normalizeReleaseFamilySummaryKey(candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, candidate.FamilyKey)
		if !ok {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	sortReleaseFamilySummaryKeys(normalized)
	values, args := releaseFamilySummaryBatchValues(normalized)

	if _, err := runner.ExecContext(ctx, fmt.Sprintf(`
		WITH requested(provider_id, newsgroup_id, key_kind, family_key) AS (
			VALUES %s
		)
		DELETE FROM release_ready_candidates c
		USING requested r
		LEFT JOIN release_family_readiness_summaries s
		  ON s.provider_id = r.provider_id
		 AND s.newsgroup_id = r.newsgroup_id
		 AND s.key_kind = r.key_kind
		 AND s.family_key = r.family_key
		WHERE c.provider_id = r.provider_id
		  AND c.newsgroup_id = r.newsgroup_id
		  AND c.key_kind = r.key_kind
		  AND c.family_key = r.family_key
		  AND (
		  	s.family_key IS NULL OR
		  	COALESCE(s.readiness_bucket, '') <> %s
		  )`,
		values,
		pgLiteral(releaseReadinessActionable)), args...); err != nil {
		return fmt.Errorf("delete parked ready release candidates batch count=%d: %w", len(normalized), err)
	}

	if _, err := runner.ExecContext(ctx, fmt.Sprintf(`
		WITH requested(provider_id, newsgroup_id, key_kind, family_key) AS (
			VALUES %s
		)
		INSERT INTO release_ready_candidates (
			source_posted_at,
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
			expected_file_count,
			expected_archive_file_count,
			has_expected_file_count,
			has_expected_archive_file_count,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			total_bytes,
			earliest_posted_at,
			ready_reason,
			updated_at
		)
		SELECT
			s.source_posted_at,
			s.provider_id,
			s.newsgroup_id,
			s.key_kind,
			s.family_key,
			COALESCE(s.source_release_key, ''),
			COALESCE(s.release_key, ''),
			COALESCE(s.release_name, ''),
			COALESCE(s.binary_count, 0),
			COALESCE(s.complete_binary_count, 0),
			COALESCE(s.complete_main_payload_binary_count, 0),
			COALESCE(s.expected_file_count, 0),
			COALESCE(s.expected_archive_file_count, 0),
			COALESCE(s.has_expected_file_count, FALSE),
			COALESCE(s.has_expected_archive_file_count, FALSE),
			COALESCE(s.expected_file_coverage_pct, 0)::DOUBLE PRECISION,
			COALESCE(s.archive_file_coverage_pct, 0)::DOUBLE PRECISION,
			COALESCE(s.total_bytes, 0)::BIGINT,
			s.earliest_posted_at,
			%s,
			s.updated_at
		FROM requested r
		JOIN release_family_readiness_summaries s
		  ON s.provider_id = r.provider_id
		 AND s.newsgroup_id = r.newsgroup_id
		 AND s.key_kind = r.key_kind
		 AND s.family_key = r.family_key
		WHERE COALESCE(s.readiness_bucket, '') = %s
		ON CONFLICT (source_posted_at, provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET source_release_key = EXCLUDED.source_release_key,
		    release_key = EXCLUDED.release_key,
		    release_name = EXCLUDED.release_name,
		    binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    expected_archive_file_count = EXCLUDED.expected_archive_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    has_expected_archive_file_count = EXCLUDED.has_expected_archive_file_count,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    archive_file_coverage_pct = EXCLUDED.archive_file_coverage_pct,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    ready_reason = EXCLUDED.ready_reason,
		    updated_at = EXCLUDED.updated_at`,
		values,
		pgLiteral(releaseReadinessActionable),
		pgLiteral(releaseReadinessActionable)), args...); err != nil {
		return fmt.Errorf("upsert ready release candidates batch count=%d: %w", len(normalized), err)
	}
	return nil
}

func syncReadyReleaseCandidateForSummaryState(ctx context.Context, runner sqlExecQueryRower, state releaseCandidateSummaryState) error {
	if strings.TrimSpace(state.ReadinessBucket) != releaseReadinessActionable {
		if _, err := runner.ExecContext(ctx, `
			DELETE FROM release_ready_candidates
			WHERE provider_id = $1
			  AND newsgroup_id = $2
			  AND key_kind = $3
			  AND family_key = $4`,
			state.Key.ProviderID,
			state.Key.NewsgroupID,
			state.Key.KeyKind,
			state.Key.FamilyKey,
		); err != nil {
			return fmt.Errorf("delete parked ready release candidate provider=%d group=%d kind=%s family=%q: %w", state.Key.ProviderID, state.Key.NewsgroupID, state.Key.KeyKind, state.Key.FamilyKey, err)
		}
		return nil
	}

	var earliestPostedAtValue any
	if state.EarliestPostedAt.Valid {
		earliestPostedAtValue = state.EarliestPostedAt.Time.UTC()
	}
	var updatedAtValue any = sql.NullTime{}
	if state.UpdatedAt.Valid {
		updatedAtValue = state.UpdatedAt.Time.UTC()
	}

	if _, err := runner.ExecContext(ctx, `
		INSERT INTO release_ready_candidates (
			source_posted_at,
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
			expected_file_count,
			expected_archive_file_count,
			has_expected_file_count,
			has_expected_archive_file_count,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			total_bytes,
			earliest_posted_at,
			ready_reason,
			updated_at
		)
		VALUES (COALESCE($18::timestamptz, NOW()),$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		ON CONFLICT (source_posted_at, provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET source_release_key = EXCLUDED.source_release_key,
		    release_key = EXCLUDED.release_key,
		    release_name = EXCLUDED.release_name,
		    binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    expected_archive_file_count = EXCLUDED.expected_archive_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    has_expected_archive_file_count = EXCLUDED.has_expected_archive_file_count,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    archive_file_coverage_pct = EXCLUDED.archive_file_coverage_pct,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    ready_reason = EXCLUDED.ready_reason,
		    updated_at = EXCLUDED.updated_at`,
		state.Key.ProviderID,
		state.Key.NewsgroupID,
		state.Key.KeyKind,
		state.Key.FamilyKey,
		state.SourceReleaseKey,
		state.ReleaseKey,
		state.ReleaseName,
		state.BinaryCount,
		state.CompleteBinaryCount,
		state.CompleteMainPayloadBinaryCount,
		state.ExpectedFileCount,
		state.ExpectedArchiveFileCount,
		state.HasExpectedFileCount,
		state.HasExpectedArchiveFileCount,
		state.ExpectedFileCoveragePct,
		state.ArchiveFileCoveragePct,
		state.TotalBytes,
		earliestPostedAtValue,
		releaseReadinessActionable,
		updatedAtValue,
	); err != nil {
		return fmt.Errorf("upsert ready release candidate provider=%d group=%d kind=%s family=%q: %w", state.Key.ProviderID, state.Key.NewsgroupID, state.Key.KeyKind, state.Key.FamilyKey, err)
	}
	return nil
}

func hydrateReleaseCandidateState(ctx context.Context, runner sqlExecQueryRower, key releaseFamilySummaryKey) (releaseCandidateSummaryState, error) {
	if runner == nil {
		return releaseCandidateSummaryState{}, fmt.Errorf("release family summary runner is required")
	}

	state := releaseCandidateSummaryState{Key: key}
	if err := runner.QueryRowContext(ctx, `
		SELECT
			COALESCE(source_release_key, ''),
			COALESCE(release_key, ''),
			COALESCE(release_name, ''),
			COALESCE(binary_count, 0),
			COALESCE(complete_binary_count, 0),
			COALESCE(complete_main_payload_binary_count, 0),
			COALESCE(expected_file_count, 0),
			COALESCE(expected_archive_file_count, 0),
			COALESCE(has_expected_file_count, FALSE),
			COALESCE(has_expected_archive_file_count, FALSE),
			COALESCE(expected_file_coverage_pct, 0)::DOUBLE PRECISION,
			COALESCE(archive_file_coverage_pct, 0)::DOUBLE PRECISION,
			COALESCE(total_bytes, 0)::BIGINT,
			earliest_posted_at,
			updated_at,
			COALESCE(dominant_family_kind, ''),
			COALESCE(dominant_file_name, ''),
			COALESCE(dominant_match_confidence, 0)::DOUBLE PRECISION,
			COALESCE(readiness_bucket, '')
		FROM release_family_readiness_summaries
		WHERE provider_id = $1
		  AND newsgroup_id = $2
		  AND key_kind = $3
		  AND family_key = $4`,
		key.ProviderID,
		key.NewsgroupID,
		key.KeyKind,
		key.FamilyKey,
	).Scan(
		&state.SourceReleaseKey,
		&state.ReleaseKey,
		&state.ReleaseName,
		&state.BinaryCount,
		&state.CompleteBinaryCount,
		&state.CompleteMainPayloadBinaryCount,
		&state.ExpectedFileCount,
		&state.ExpectedArchiveFileCount,
		&state.HasExpectedFileCount,
		&state.HasExpectedArchiveFileCount,
		&state.ExpectedFileCoveragePct,
		&state.ArchiveFileCoveragePct,
		&state.TotalBytes,
		&state.EarliestPostedAt,
		&state.UpdatedAt,
		&state.DominantFamilyKind,
		&state.DominantFileName,
		&state.DominantMatchConfidence,
		&state.ReadinessBucket,
	); err != nil {
		return releaseCandidateSummaryState{}, fmt.Errorf("load release candidate state provider=%d group=%d kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
	}

	finalBucket := strings.TrimSpace(state.ReadinessBucket)
	if finalBucket == "" {
		finalBucket = releaseReadinessFragmentOnly
	}

	if key.KeyKind == ReleaseCandidateKeyKindReleaseFamily {
		if finalBucket == releaseReadinessFragmentOnly {
			normalizedName := normalizeSummaryReleaseCandidateName(firstNonBlank(state.ReleaseName, key.FamilyKey))
			shape, err := loadReleaseFamilyShape(ctx, runner, key)
			if err != nil {
				return releaseCandidateSummaryState{}, err
			}
			if summaryNumericOpaqueReleaseRE.MatchString(normalizedName) && !shape.HasUsableFileIdentity {
				finalBucket = releaseReadinessWeakObfuscated
			}
		}
		if finalBucket == releaseReadinessActionable {
			shape, err := loadReleaseFamilyShape(ctx, runner, key)
			if err != nil {
				return releaseCandidateSummaryState{}, err
			}
			if shape.AllContextual &&
				shape.MaxExpectedAnyFileCount > 1 &&
				shape.IndexedFileCount >= 2 &&
				shape.BaseStemFileCount == shape.DistinctBaseStemCount &&
				shape.DistinctBaseStemCount >= maxInt(shape.MaxExpectedAnyFileCount, 8) &&
				state.BinaryCount >= maxInt(shape.MaxExpectedAnyFileCount*3, 24) {
				finalBucket = releaseReadinessOvergrouped
			} else if shape.AllContextual &&
				shape.MaxExpectedAnyFileCount > 1 &&
				shape.IndexedFileCount >= 2 &&
				shape.BaseStemFileCount >= 2 &&
				shape.DistinctBaseStemCount < shape.BaseStemFileCount {
				finalBucket = releaseReadinessPreferBaseStem
			}
		}
	}

	recoverPending := false
	switch {
	case key.KeyKind == ReleaseCandidateKeyKindBaseStem:
		pending, err := isReleaseCandidateRecoverPending(ctx, runner, key)
		if err != nil {
			return releaseCandidateSummaryState{}, err
		}
		recoverPending = pending
	case finalBucket == releaseReadinessFragmentOnly:
		pending, err := isReleaseCandidateRecoverPending(ctx, runner, key)
		if err != nil {
			return releaseCandidateSummaryState{}, err
		}
		recoverPending = pending
	case finalBucket == releaseReadinessWeakSingle,
		finalBucket == releaseReadinessWeakObfuscated,
		finalBucket == releaseReadinessOvergrouped:
		pending, err := isReleaseCandidateRecoverPending(ctx, runner, key)
		if err != nil {
			return releaseCandidateSummaryState{}, err
		}
		recoverPending = pending
	}

	if _, err := runner.ExecContext(ctx, `
		UPDATE release_family_readiness_summaries
		SET readiness_bucket = $5,
		    recover_pending = $6
		WHERE provider_id = $1
		  AND newsgroup_id = $2
		  AND key_kind = $3
		  AND family_key = $4`,
		key.ProviderID,
		key.NewsgroupID,
		key.KeyKind,
		key.FamilyKey,
		finalBucket,
		recoverPending,
	); err != nil {
		return releaseCandidateSummaryState{}, fmt.Errorf("update release candidate state provider=%d group=%d kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
	}

	state.ReadinessBucket = finalBucket
	state.RecoverPending = recoverPending
	return state, nil
}

func loadReleaseFamilyShape(ctx context.Context, runner sqlExecQueryRower, key releaseFamilySummaryKey) (releaseFamilyShape, error) {
	var shape releaseFamilyShape
	if err := runner.QueryRowContext(ctx, `
		SELECT
			COALESCE(BOOL_AND(LOWER(COALESCE(bic.family_kind, '')) = 'contextual_obfuscated'), FALSE) AS all_contextual,
			COALESCE(MAX(GREATEST(bic.expected_file_count, bic.expected_archive_file_count)), 0)::INTEGER AS max_expected_any_file_count,
			COUNT(*) FILTER (WHERE bic.file_index > 0)::INTEGER AS indexed_file_count,
			COUNT(*) FILTER (WHERE BTRIM(COALESCE(bic.base_stem, '')) <> '')::INTEGER AS base_stem_file_count,
			COUNT(DISTINCT LOWER(BTRIM(COALESCE(bic.base_stem, '')))) FILTER (
				WHERE BTRIM(COALESCE(bic.base_stem, '')) <> ''
			)::INTEGER AS distinct_base_stem_count,
			COALESCE(BOOL_OR(
				LOWER(COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) ~
				'\.(rar|zip|7z|7z\.[0-9]{3}|zip\.[0-9]{3}|r[0-9]{2,3}|part[0-9]+\.rar|mkv|mp4|avi|ts|mp3|flac|m4a|par2)$'
			), FALSE) AS has_usable_file_identity
		FROM binary_core bc
		JOIN binary_identity_current bic
		  ON bic.source_posted_at = bc.source_posted_at
		 AND bic.binary_id = bc.binary_id
		WHERE bc.provider_id = $1
		  AND bc.newsgroup_id = $2
		  AND bic.release_family_key = $3
		  AND (bic.is_main_payload = TRUE OR bic.is_auxiliary = FALSE)
		  AND NOT EXISTS (
			SELECT 1
			FROM binary_lifecycle bl
			WHERE bl.source_posted_at = bic.source_posted_at
			  AND bl.binary_id = bic.binary_id
			  AND bl.lifecycle_status = 'superseded'
		  )`,
		key.ProviderID,
		key.NewsgroupID,
		key.FamilyKey,
	).Scan(
		&shape.AllContextual,
		&shape.MaxExpectedAnyFileCount,
		&shape.IndexedFileCount,
		&shape.BaseStemFileCount,
		&shape.DistinctBaseStemCount,
		&shape.HasUsableFileIdentity,
	); err != nil {
		return shape, fmt.Errorf("load release family shape provider=%d group=%d family=%q: %w", key.ProviderID, key.NewsgroupID, key.FamilyKey, err)
	}
	return shape, nil
}

func isReleaseCandidateRecoverPending(ctx context.Context, runner sqlExecQueryRower, key releaseFamilySummaryKey) (bool, error) {
	matchClause := `
			bc.provider_id = $1
			AND bc.newsgroup_id = $2
			AND bic.release_family_key = $3`
	if key.KeyKind == ReleaseCandidateKeyKindBaseStem {
		matchClause = `
			bc.provider_id = $1
			AND bc.newsgroup_id = $2
			AND GREATEST(bic.expected_file_count, bic.expected_archive_file_count) > 1
			AND BTRIM(COALESCE(bic.base_stem, '')) <> ''
			AND LOWER(BTRIM(bic.base_stem)) = $3`
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM binary_core bc
			JOIN binary_identity_current bic
			  ON bic.source_posted_at = bc.source_posted_at
			 AND bic.binary_id = bc.binary_id
			JOIN binary_recovery_current brc
			  ON brc.source_posted_at = bc.source_posted_at
			 AND brc.binary_id = bc.binary_id
			JOIN LATERAL (
				SELECT bp.article_header_id
				FROM binary_parts bp
				WHERE bp.binary_id = bc.binary_id
				ORDER BY bp.part_number, bp.id
				LIMIT 1
			) bp ON TRUE
			JOIN article_header_ingest_payloads p
			  ON p.article_header_id = bp.article_header_id
			WHERE ` + matchClause + `
			  AND bic.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND bic.is_main_payload = TRUE
			  AND COALESCE(brc.recovered_source, '') <> 'yenc_header'
` + yencRecoverySubjectFileNamePredicate + `
		)`

	var pending bool
	if err := runner.QueryRowContext(ctx, query, key.ProviderID, key.NewsgroupID, key.FamilyKey).Scan(&pending); err != nil {
		return false, fmt.Errorf("query recover-pending release candidate provider=%d group=%d kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
	}
	return pending, nil
}

func refreshRecoveredFileSetCandidatesForSummaryKey(ctx context.Context, runner sqlExecQueryRower, key releaseFamilySummaryKey) error {
	return refreshRecoveredFileSetCandidatesForSummaryKeys(ctx, runner, []releaseFamilySummaryKey{key})
}

func refreshRecoveredFileSetCandidatesForSummaryKeys(ctx context.Context, runner sqlExecQueryRower, keys []releaseFamilySummaryKey) error {
	if len(keys) == 0 {
		return nil
	}

	values, args := releaseFamilySummaryBatchValues(keys)

	rows, err := runner.QueryContext(ctx, fmt.Sprintf(`
		WITH requested_input(provider_id, newsgroup_id, key_kind, family_key) AS MATERIALIZED (
			VALUES %s
		),
		requested AS MATERIALIZED (
			SELECT
				r.provider_id,
				r.newsgroup_id,
				r.key_kind,
				r.family_key,
				s.source_posted_at AS source_hint
			FROM requested_input r
			JOIN LATERAL (
				SELECT
					COALESCE(summary.earliest_posted_at, summary.source_posted_at) AS source_posted_at,
					summary.binary_count,
					summary.complete_main_payload_binary_count,
					summary.recover_pending
				FROM release_family_readiness_summaries summary
				WHERE summary.provider_id = r.provider_id
				  AND summary.newsgroup_id = r.newsgroup_id
				  AND summary.key_kind = r.key_kind
				  AND summary.family_key = r.family_key
				ORDER BY summary.updated_at DESC, summary.source_posted_at DESC
				LIMIT 1
			) s ON true
			WHERE s.binary_count > 1
			   OR s.complete_main_payload_binary_count > 0
			   OR s.recover_pending = TRUE
		),
		release_family_matches AS MATERIALIZED (
			SELECT DISTINCT bic.provider_id, bic.file_set_key
			FROM requested r
			JOIN LATERAL (
				SELECT bic.provider_id, bic.file_set_key, bic.source_posted_at, bic.binary_id
				FROM binary_identity_current bic
				WHERE bic.provider_id = r.provider_id
				  AND bic.newsgroup_id = r.newsgroup_id
				  AND bic.release_family_key = r.family_key
				  AND BTRIM(bic.release_family_key) <> ''
				  AND BTRIM(bic.file_set_key) <> ''
				  AND bic.source_posted_at >= r.source_hint - INTERVAL '1 day'
				  AND bic.source_posted_at < r.source_hint + INTERVAL '1 day'
			) bic ON r.key_kind = 'release_family'
			WHERE EXISTS (
				SELECT 1
				FROM binary_observation_stats bos
				WHERE bos.source_posted_at = bic.source_posted_at
				  AND bos.binary_id = bic.binary_id
				  AND bos.posted_at IS NOT NULL
			)
			AND EXISTS (
				SELECT 1
				FROM binary_recovery_current brc
				WHERE brc.source_posted_at = bic.source_posted_at
				  AND brc.binary_id = bic.binary_id
				  AND brc.recovered_source = 'yenc_header'
			)
			ORDER BY bic.provider_id, bic.file_set_key
			LIMIT %d
		),
		base_stem_matches AS MATERIALIZED (
			SELECT DISTINCT bic.provider_id, bic.file_set_key
			FROM requested r
			JOIN LATERAL (
				SELECT bic.provider_id, bic.file_set_key, bic.source_posted_at, bic.binary_id
				FROM binary_identity_current bic
				WHERE bic.provider_id = r.provider_id
				  AND bic.newsgroup_id = r.newsgroup_id
				  AND GREATEST(bic.expected_file_count, bic.expected_archive_file_count) > 1
				  AND BTRIM(bic.base_stem) <> ''
				  AND LOWER(BTRIM(bic.base_stem)) = r.family_key
				  AND BTRIM(bic.file_set_key) <> ''
				  AND bic.source_posted_at >= r.source_hint - INTERVAL '1 day'
				  AND bic.source_posted_at < r.source_hint + INTERVAL '1 day'
			) bic ON r.key_kind = 'base_stem'
			WHERE EXISTS (
				SELECT 1
				FROM binary_observation_stats bos
				WHERE bos.source_posted_at = bic.source_posted_at
				  AND bos.binary_id = bic.binary_id
				  AND bos.posted_at IS NOT NULL
			)
			AND EXISTS (
				SELECT 1
				FROM binary_recovery_current brc
				WHERE brc.source_posted_at = bic.source_posted_at
				  AND brc.binary_id = bic.binary_id
				  AND brc.recovered_source = 'yenc_header'
			)
			ORDER BY bic.provider_id, bic.file_set_key
			LIMIT %d
		)
		SELECT provider_id, file_set_key FROM release_family_matches
		UNION
		SELECT provider_id, file_set_key FROM base_stem_matches`,
		values, releaseRecoveredFileSetSyncCap, releaseRecoveredFileSetSyncCap), args...)
	if err != nil {
		return fmt.Errorf("list impacted recovered file sets for summary key batch count=%d: %w", len(keys), err)
	}
	defer rows.Close()

	type recoveredFileSetKey struct {
		ProviderID int64
		FileSetKey string
	}
	fileSetKeys := make([]recoveredFileSetKey, 0, 8)
	for rows.Next() {
		var item recoveredFileSetKey
		if err := rows.Scan(&item.ProviderID, &item.FileSetKey); err != nil {
			return fmt.Errorf("scan impacted recovered file set key: %w", err)
		}
		fileSetKeys = append(fileSetKeys, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate impacted recovered file sets for summary key batch count=%d: %w", len(keys), err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close impacted recovered file set rows for summary key batch count=%d: %w", len(keys), err)
	}
	sort.Slice(fileSetKeys, func(i, j int) bool {
		if fileSetKeys[i].ProviderID != fileSetKeys[j].ProviderID {
			return fileSetKeys[i].ProviderID < fileSetKeys[j].ProviderID
		}
		return fileSetKeys[i].FileSetKey < fileSetKeys[j].FileSetKey
	})
	if len(fileSetKeys) > releaseRecoveredFileSetSyncCap {
		fileSetKeys = fileSetKeys[:releaseRecoveredFileSetSyncCap]
	}
	byProvider := make(map[int64][]string, 1)
	for _, item := range fileSetKeys {
		byProvider[item.ProviderID] = append(byProvider[item.ProviderID], item.FileSetKey)
	}
	for providerID, providerFileSetKeys := range byProvider {
		for start := 0; start < len(providerFileSetKeys); start += releaseRecoveredFileSetSyncChunkSize {
			end := start + releaseRecoveredFileSetSyncChunkSize
			if end > len(providerFileSetKeys) {
				end = len(providerFileSetKeys)
			}
			if err := refreshRecoveredFileSetCandidatesBatch(ctx, runner, providerID, providerFileSetKeys[start:end]); err != nil {
				return err
			}
		}
	}
	return nil
}

type recoveredFileSetCandidateAggregate struct {
	ProviderID                     int64
	FileSetKey                     string
	RepresentativeNewsgroupID      int64
	SourceReleaseKey               string
	ReleaseKey                     string
	ReleaseName                    string
	BinaryCount                    int
	CompleteBinaryCount            int
	CompleteMainPayloadBinaryCount int
	ExpectedFileCount              int
	ExpectedArchiveFileCount       int
	HasExpectedFileCount           bool
	HasExpectedArchiveFileCount    bool
	TotalBytes                     int64
	EarliestPostedAt               sql.NullTime
	LatestPostedAt                 sql.NullTime
	MaxUpdatedAt                   sql.NullTime
	DistinctNewsgroupCount         int
	MainPayloadBinaryCount         int
}

func refreshRecoveredFileSetCandidatesBatch(ctx context.Context, runner sqlExecQueryRower, providerID int64, fileSetKeys []string) error {
	if providerID <= 0 || len(fileSetKeys) == 0 {
		return nil
	}

	args := make([]any, 0, len(fileSetKeys)+1)
	args = append(args, providerID)
	values := make([]string, 0, len(fileSetKeys))
	seen := make(map[string]struct{}, len(fileSetKeys))
	for _, fileSetKey := range fileSetKeys {
		fileSetKey = strings.TrimSpace(fileSetKey)
		if fileSetKey == "" {
			continue
		}
		if _, exists := seen[fileSetKey]; exists {
			continue
		}
		seen[fileSetKey] = struct{}{}
		args = append(args, fileSetKey)
		values = append(values, fmt.Sprintf("($%d::text)", len(args)))
	}
	if len(values) == 0 {
		return nil
	}

	actionableParam := len(args) + 1
	fragmentParam := len(args) + 2
	keyKindParam := len(args) + 3
	aggregateSQL := fmt.Sprintf(`
		WITH requested(file_set_key) AS (
			VALUES %s
		),
		requested_hints AS MATERIALIZED (
			SELECT
				r.file_set_key,
				MIN(c.source_posted_at) AS min_source_posted_at,
				MAX(c.source_posted_at) AS max_source_posted_at
			FROM requested r
			JOIN release_recovered_file_set_candidates c
			  ON c.provider_id = $1
			 AND c.file_set_key = r.file_set_key
			GROUP BY r.file_set_key
		),
		aggregates AS MATERIALIZED (
			SELECT
				$1::BIGINT AS provider_id,
				r.file_set_key,
				COALESCE(MIN(bc.newsgroup_id), 0)::BIGINT AS representative_newsgroup_id,
				COALESCE(MAX(NULLIF(BTRIM(bic.source_release_key), '')), r.file_set_key) AS source_release_key,
				r.file_set_key AS release_key,
				COALESCE(MAX(NULLIF(BTRIM(bic.release_name), '')), r.file_set_key) AS release_name,
				COUNT(bic.binary_id)::INTEGER AS binary_count,
				COUNT(*) FILTER (
					WHERE bos.total_parts > 0 AND bos.observed_parts = bos.total_parts
				)::INTEGER AS complete_binary_count,
				COUNT(*) FILTER (
					WHERE (bic.is_main_payload = TRUE OR bic.is_auxiliary = FALSE)
					  AND bos.total_parts > 0
					  AND bos.observed_parts = bos.total_parts
				)::INTEGER AS complete_main_payload_binary_count,
				COALESCE(MAX(bic.expected_file_count), 0)::INTEGER AS expected_file_count,
				COALESCE(MAX(bic.expected_archive_file_count), 0)::INTEGER AS expected_archive_file_count,
				COALESCE(BOOL_OR(bic.expected_file_count > 0), FALSE) AS has_expected_file_count,
				COALESCE(BOOL_OR(bic.expected_archive_file_count > 0), FALSE) AS has_expected_archive_file_count,
				COALESCE(SUM(bos.total_bytes), 0)::BIGINT AS total_bytes,
				MIN(bos.posted_at) AS earliest_posted_at,
				MAX(bos.posted_at) AS latest_posted_at,
				MAX(GREATEST(bic.updated_at, bos.updated_at, brc.updated_at)) AS max_updated_at,
				COUNT(DISTINCT bc.newsgroup_id)::INTEGER AS distinct_newsgroup_count,
				COUNT(*) FILTER (
					WHERE bic.is_main_payload = TRUE OR bic.is_auxiliary = FALSE
				)::INTEGER AS main_payload_binary_count
			FROM requested r
			LEFT JOIN requested_hints h
			  ON h.file_set_key = r.file_set_key
			JOIN binary_identity_current bic
			  ON bic.provider_id = $1
			 AND bic.file_set_key = r.file_set_key
			 AND BTRIM(bic.file_set_key) <> ''
			 AND (
				h.file_set_key IS NULL OR
				(
					bic.source_posted_at >= h.min_source_posted_at - INTERVAL '1 day' AND
					bic.source_posted_at < h.max_source_posted_at + INTERVAL '1 day'
				)
			 )
			 AND NOT EXISTS (
				SELECT 1
				FROM binary_lifecycle bl
				WHERE bl.source_posted_at = bic.source_posted_at
			  AND bl.binary_id = bic.binary_id
			  AND bl.lifecycle_status = 'superseded'
			 )
			JOIN binary_core bc ON bc.binary_id = bic.binary_id
			JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bic.source_posted_at
			 AND bos.binary_id = bic.binary_id
			JOIN binary_recovery_current brc
			  ON brc.source_posted_at = bic.source_posted_at
			 AND brc.binary_id = bic.binary_id
			 AND brc.recovered_source = 'yenc_header'
			WHERE bos.posted_at IS NOT NULL
			GROUP BY r.file_set_key
		),
		scored AS MATERIALIZED (
			SELECT
				a.*,
				GREATEST(a.expected_file_count, a.expected_archive_file_count) AS max_expected,
				CASE
					WHEN a.expected_file_count > 0 THEN LEAST(100::DOUBLE PRECISION, (a.complete_main_payload_binary_count::DOUBLE PRECISION / a.expected_file_count::DOUBLE PRECISION) * 100)
					ELSE 0::DOUBLE PRECISION
				END AS expected_file_coverage_pct,
				CASE
					WHEN a.expected_archive_file_count > 0 THEN LEAST(100::DOUBLE PRECISION, (a.complete_main_payload_binary_count::DOUBLE PRECISION / a.expected_archive_file_count::DOUBLE PRECISION) * 100)
					ELSE 0::DOUBLE PRECISION
				END AS archive_file_coverage_pct,
				CASE
					WHEN a.complete_main_payload_binary_count > 0 THEN $%d
					ELSE $%d
				END AS readiness_bucket
			FROM aggregates a
		),
		valid AS MATERIALIZED (
			SELECT *
			FROM scored
			WHERE binary_count > 0
			  AND distinct_newsgroup_count > 1
			  AND main_payload_binary_count >= 2
			  AND earliest_posted_at IS NOT NULL
			  AND latest_posted_at IS NOT NULL
			AND latest_posted_at - earliest_posted_at <= INTERVAL '24 hours'
		)
	`, strings.Join(values, ","), actionableParam, fragmentParam)

	statementArgs := append([]any(nil), args...)
	statementArgs = append(statementArgs, releaseReadinessActionable, releaseReadinessFragmentOnly)
	statementArgsWithKind := append(append([]any(nil), statementArgs...), ReleaseCandidateKeyKindRecoveredFileSet)

	var deletedRecovered, deletedReady, upsertedRecovered, upsertedReady int
	if err := runner.QueryRowContext(ctx, aggregateSQL+`,
		deleted_recovered AS (
			DELETE FROM release_recovered_file_set_candidates c
			USING requested r
			LEFT JOIN valid v ON v.file_set_key = r.file_set_key
			WHERE c.provider_id = $1
			  AND c.file_set_key = r.file_set_key
			  AND v.file_set_key IS NULL
			RETURNING 1
		),
		deleted_ready AS (
			DELETE FROM release_ready_candidates c
			USING requested r
			LEFT JOIN valid v ON v.file_set_key = r.file_set_key
			WHERE c.provider_id = $1
			  AND c.key_kind = $`+fmt.Sprint(keyKindParam)+`
			  AND c.family_key = r.file_set_key
			  AND (
			  	v.file_set_key IS NULL OR
			  	v.readiness_bucket <> $`+fmt.Sprint(actionableParam)+`
			  )
			RETURNING 1
		),
		upserted_recovered AS (
			INSERT INTO release_recovered_file_set_candidates (
			source_posted_at,
			provider_id,
			file_set_key,
			representative_newsgroup_id,
			source_release_key,
			release_key,
			release_name,
			binary_count,
			complete_binary_count,
			complete_main_payload_binary_count,
			expected_file_count,
			expected_archive_file_count,
			has_expected_file_count,
			has_expected_archive_file_count,
			total_bytes,
			earliest_posted_at,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			readiness_bucket,
			updated_at
			)
			SELECT
				COALESCE(earliest_posted_at, NOW()),
				provider_id,
				file_set_key,
				representative_newsgroup_id,
				source_release_key,
				release_key,
				release_name,
				binary_count,
				complete_binary_count,
				complete_main_payload_binary_count,
				expected_file_count,
				expected_archive_file_count,
				has_expected_file_count,
				has_expected_archive_file_count,
				total_bytes,
				earliest_posted_at,
				expected_file_coverage_pct,
				archive_file_coverage_pct,
				readiness_bucket,
				COALESCE(max_updated_at, NOW())
			FROM valid
			ON CONFLICT (source_posted_at, provider_id, file_set_key) DO UPDATE
			SET representative_newsgroup_id = EXCLUDED.representative_newsgroup_id,
			    source_release_key = EXCLUDED.source_release_key,
			    release_key = EXCLUDED.release_key,
			    release_name = EXCLUDED.release_name,
			    binary_count = EXCLUDED.binary_count,
			    complete_binary_count = EXCLUDED.complete_binary_count,
			    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
			    expected_file_count = EXCLUDED.expected_file_count,
			    expected_archive_file_count = EXCLUDED.expected_archive_file_count,
			    has_expected_file_count = EXCLUDED.has_expected_file_count,
			    has_expected_archive_file_count = EXCLUDED.has_expected_archive_file_count,
			    total_bytes = EXCLUDED.total_bytes,
			    earliest_posted_at = EXCLUDED.earliest_posted_at,
			    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
			    archive_file_coverage_pct = EXCLUDED.archive_file_coverage_pct,
			    readiness_bucket = EXCLUDED.readiness_bucket,
			    updated_at = EXCLUDED.updated_at
			RETURNING 1
		),
		upserted_ready AS (
			INSERT INTO release_ready_candidates (
			source_posted_at,
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
			expected_file_count,
			expected_archive_file_count,
			has_expected_file_count,
			has_expected_archive_file_count,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			total_bytes,
			earliest_posted_at,
			ready_reason,
			updated_at
			)
			SELECT
				COALESCE(earliest_posted_at, NOW()),
				provider_id,
				representative_newsgroup_id,
				$`+fmt.Sprint(keyKindParam)+`,
				file_set_key,
				source_release_key,
				release_key,
				release_name,
				binary_count,
				complete_binary_count,
				complete_main_payload_binary_count,
				expected_file_count,
				expected_archive_file_count,
				has_expected_file_count,
				has_expected_archive_file_count,
				expected_file_coverage_pct,
				archive_file_coverage_pct,
				total_bytes,
				earliest_posted_at,
				$`+fmt.Sprint(actionableParam)+`,
				COALESCE(max_updated_at, NOW())
			FROM valid
			WHERE readiness_bucket = $`+fmt.Sprint(actionableParam)+`
			ON CONFLICT (source_posted_at, provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
			SET source_release_key = EXCLUDED.source_release_key,
			    release_key = EXCLUDED.release_key,
			    release_name = EXCLUDED.release_name,
			    binary_count = EXCLUDED.binary_count,
			    complete_binary_count = EXCLUDED.complete_binary_count,
			    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
			    expected_file_count = EXCLUDED.expected_file_count,
			    expected_archive_file_count = EXCLUDED.expected_archive_file_count,
			    has_expected_file_count = EXCLUDED.has_expected_file_count,
			    has_expected_archive_file_count = EXCLUDED.has_expected_archive_file_count,
			    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
			    archive_file_coverage_pct = EXCLUDED.archive_file_coverage_pct,
			    total_bytes = EXCLUDED.total_bytes,
			    earliest_posted_at = EXCLUDED.earliest_posted_at,
			    ready_reason = EXCLUDED.ready_reason,
			    updated_at = EXCLUDED.updated_at
			RETURNING 1
		)
		SELECT
			(SELECT COUNT(*) FROM deleted_recovered),
			(SELECT COUNT(*) FROM deleted_ready),
			(SELECT COUNT(*) FROM upserted_recovered),
			(SELECT COUNT(*) FROM upserted_ready)`, statementArgsWithKind...).Scan(&deletedRecovered, &deletedReady, &upsertedRecovered, &upsertedReady); err != nil {
		return fmt.Errorf("refresh recovered file-set candidates provider=%d count=%d: %w", providerID, len(values), err)
	}
	return nil
}

func refreshRecoveredFileSetCandidate(ctx context.Context, runner sqlExecQueryRower, providerID int64, fileSetKey string) error {
	fileSetKey = strings.TrimSpace(fileSetKey)
	if providerID <= 0 || fileSetKey == "" {
		return nil
	}

	var row recoveredFileSetCandidateAggregate
	if err := runner.QueryRowContext(ctx, `
		SELECT
			$1::BIGINT AS provider_id,
			$2::TEXT AS file_set_key,
			COALESCE(MIN(bc.newsgroup_id), 0)::BIGINT AS representative_newsgroup_id,
			COALESCE(MAX(NULLIF(BTRIM(bic.source_release_key), '')), $2) AS source_release_key,
			$2 AS release_key,
			COALESCE(MAX(NULLIF(BTRIM(bic.release_name), '')), $2) AS release_name,
			COUNT(bic.binary_id)::INTEGER AS binary_count,
			COUNT(*) FILTER (
				WHERE bos.total_parts > 0 AND bos.observed_parts = bos.total_parts
			)::INTEGER AS complete_binary_count,
			COUNT(*) FILTER (
				WHERE (bic.is_main_payload = TRUE OR bic.is_auxiliary = FALSE)
				  AND bos.total_parts > 0
				  AND bos.observed_parts = bos.total_parts
			)::INTEGER AS complete_main_payload_binary_count,
			COALESCE(MAX(bic.expected_file_count), 0)::INTEGER AS expected_file_count,
			COALESCE(MAX(bic.expected_archive_file_count), 0)::INTEGER AS expected_archive_file_count,
			COALESCE(BOOL_OR(bic.expected_file_count > 0), FALSE) AS has_expected_file_count,
			COALESCE(BOOL_OR(bic.expected_archive_file_count > 0), FALSE) AS has_expected_archive_file_count,
			COALESCE(SUM(bos.total_bytes), 0)::BIGINT AS total_bytes,
			MIN(bos.posted_at) AS earliest_posted_at,
			MAX(bos.posted_at) AS latest_posted_at,
			MAX(GREATEST(bic.updated_at, bos.updated_at, brc.updated_at)) AS max_updated_at,
			COUNT(DISTINCT bc.newsgroup_id)::INTEGER AS distinct_newsgroup_count,
			COUNT(*) FILTER (
				WHERE bic.is_main_payload = TRUE OR bic.is_auxiliary = FALSE
			)::INTEGER AS main_payload_binary_count
		FROM binary_identity_current bic
		JOIN binary_core bc ON bc.binary_id = bic.binary_id
		JOIN binary_observation_stats bos
		  ON bos.source_posted_at = bic.source_posted_at
		 AND bos.binary_id = bic.binary_id
		JOIN binary_recovery_current brc
		  ON brc.source_posted_at = bic.source_posted_at
		 AND brc.binary_id = bic.binary_id
		WHERE bic.provider_id = $1
		  AND bic.file_set_key = $2
		  AND COALESCE(brc.recovered_source, '') = 'yenc_header'
		  AND BTRIM(bic.file_set_key) <> ''
		  AND bos.posted_at IS NOT NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM binary_lifecycle bl
			WHERE bl.source_posted_at = bic.source_posted_at
			  AND bl.binary_id = bic.binary_id
			  AND bl.lifecycle_status = 'superseded'
		  )`,
		providerID,
		fileSetKey,
	).Scan(
		&row.ProviderID,
		&row.FileSetKey,
		&row.RepresentativeNewsgroupID,
		&row.SourceReleaseKey,
		&row.ReleaseKey,
		&row.ReleaseName,
		&row.BinaryCount,
		&row.CompleteBinaryCount,
		&row.CompleteMainPayloadBinaryCount,
		&row.ExpectedFileCount,
		&row.ExpectedArchiveFileCount,
		&row.HasExpectedFileCount,
		&row.HasExpectedArchiveFileCount,
		&row.TotalBytes,
		&row.EarliestPostedAt,
		&row.LatestPostedAt,
		&row.MaxUpdatedAt,
		&row.DistinctNewsgroupCount,
		&row.MainPayloadBinaryCount,
	); err != nil {
		return fmt.Errorf("query recovered file-set candidate provider=%d file_set=%q: %w", providerID, fileSetKey, err)
	}
	return upsertRecoveredFileSetCandidateAggregate(ctx, runner, row)
}

func upsertRecoveredFileSetCandidateAggregate(ctx context.Context, runner sqlExecQueryRower, row recoveredFileSetCandidateAggregate) error {
	if row.BinaryCount == 0 ||
		row.DistinctNewsgroupCount <= 1 ||
		row.MainPayloadBinaryCount < 2 ||
		!row.EarliestPostedAt.Valid ||
		!row.LatestPostedAt.Valid ||
		row.LatestPostedAt.Time.Sub(row.EarliestPostedAt.Time) > 24*time.Hour {
		if _, err := runner.ExecContext(ctx, `
			DELETE FROM release_recovered_file_set_candidates
			WHERE provider_id = $1
			  AND file_set_key = $2`,
			row.ProviderID,
			row.FileSetKey,
		); err != nil {
			return fmt.Errorf("delete stale recovered file-set candidate provider=%d file_set=%q: %w", row.ProviderID, row.FileSetKey, err)
		}
		if _, err := runner.ExecContext(ctx, `
			DELETE FROM release_ready_candidates
			WHERE provider_id = $1
			  AND key_kind = $2
			  AND family_key = $3`,
			row.ProviderID,
			ReleaseCandidateKeyKindRecoveredFileSet,
			row.FileSetKey,
		); err != nil {
			return fmt.Errorf("delete stale ready recovered file-set candidate provider=%d file_set=%q: %w", row.ProviderID, row.FileSetKey, err)
		}
		return nil
	}

	expectedFileCoveragePct := 0.0
	if row.ExpectedFileCount > 0 {
		expectedFileCoveragePct = minFloat(100, (float64(row.CompleteMainPayloadBinaryCount)/float64(row.ExpectedFileCount))*100)
	}
	archiveFileCoveragePct := 0.0
	if row.ExpectedArchiveFileCount > 0 {
		archiveFileCoveragePct = minFloat(100, (float64(row.CompleteMainPayloadBinaryCount)/float64(row.ExpectedArchiveFileCount))*100)
	}
	readinessBucket := releaseReadinessFragmentOnly
	if row.CompleteMainPayloadBinaryCount > 0 {
		readinessBucket = releaseReadinessActionable
	}

	var earliestPostedAtValue any
	if row.EarliestPostedAt.Valid {
		earliestPostedAtValue = row.EarliestPostedAt.Time.UTC()
	}
	var updatedAtValue any = sql.NullTime{}
	if row.MaxUpdatedAt.Valid {
		updatedAtValue = row.MaxUpdatedAt.Time.UTC()
	}

	if _, err := runner.ExecContext(ctx, `
		INSERT INTO release_recovered_file_set_candidates (
			source_posted_at,
			provider_id,
			file_set_key,
			representative_newsgroup_id,
			source_release_key,
			release_key,
			release_name,
			binary_count,
			complete_binary_count,
			complete_main_payload_binary_count,
			expected_file_count,
			expected_archive_file_count,
			has_expected_file_count,
			has_expected_archive_file_count,
			total_bytes,
			earliest_posted_at,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			readiness_bucket,
			updated_at
		)
		VALUES (COALESCE($15::timestamptz, NOW()),$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT (source_posted_at, provider_id, file_set_key) DO UPDATE
		SET representative_newsgroup_id = EXCLUDED.representative_newsgroup_id,
		    source_release_key = EXCLUDED.source_release_key,
		    release_key = EXCLUDED.release_key,
		    release_name = EXCLUDED.release_name,
		    binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    expected_archive_file_count = EXCLUDED.expected_archive_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    has_expected_archive_file_count = EXCLUDED.has_expected_archive_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    archive_file_coverage_pct = EXCLUDED.archive_file_coverage_pct,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    updated_at = EXCLUDED.updated_at`,
		row.ProviderID,
		row.FileSetKey,
		row.RepresentativeNewsgroupID,
		row.SourceReleaseKey,
		row.ReleaseKey,
		row.ReleaseName,
		row.BinaryCount,
		row.CompleteBinaryCount,
		row.CompleteMainPayloadBinaryCount,
		row.ExpectedFileCount,
		row.ExpectedArchiveFileCount,
		row.HasExpectedFileCount,
		row.HasExpectedArchiveFileCount,
		row.TotalBytes,
		earliestPostedAtValue,
		expectedFileCoveragePct,
		archiveFileCoveragePct,
		readinessBucket,
		updatedAtValue,
	); err != nil {
		return fmt.Errorf("upsert recovered file-set candidate provider=%d file_set=%q: %w", row.ProviderID, row.FileSetKey, err)
	}

	if readinessBucket != releaseReadinessActionable {
		if _, err := runner.ExecContext(ctx, `
			DELETE FROM release_ready_candidates
			WHERE provider_id = $1
			  AND key_kind = $2
			  AND family_key = $3`,
			row.ProviderID,
			ReleaseCandidateKeyKindRecoveredFileSet,
			row.FileSetKey,
		); err != nil {
			return fmt.Errorf("delete parked ready recovered file-set candidate provider=%d file_set=%q: %w", row.ProviderID, row.FileSetKey, err)
		}
		return nil
	}

	if _, err := runner.ExecContext(ctx, `
		INSERT INTO release_ready_candidates (
			source_posted_at,
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
			expected_file_count,
			expected_archive_file_count,
			has_expected_file_count,
			has_expected_archive_file_count,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			total_bytes,
			earliest_posted_at,
			ready_reason,
			updated_at
		)
		VALUES (COALESCE($18::timestamptz, NOW()),$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		ON CONFLICT (source_posted_at, provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET source_release_key = EXCLUDED.source_release_key,
		    release_key = EXCLUDED.release_key,
		    release_name = EXCLUDED.release_name,
		    binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    expected_archive_file_count = EXCLUDED.expected_archive_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    has_expected_archive_file_count = EXCLUDED.has_expected_archive_file_count,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    archive_file_coverage_pct = EXCLUDED.archive_file_coverage_pct,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    ready_reason = EXCLUDED.ready_reason,
		    updated_at = EXCLUDED.updated_at`,
		row.ProviderID,
		row.RepresentativeNewsgroupID,
		ReleaseCandidateKeyKindRecoveredFileSet,
		row.FileSetKey,
		row.SourceReleaseKey,
		row.ReleaseKey,
		row.ReleaseName,
		row.BinaryCount,
		row.CompleteBinaryCount,
		row.CompleteMainPayloadBinaryCount,
		row.ExpectedFileCount,
		row.ExpectedArchiveFileCount,
		row.HasExpectedFileCount,
		row.HasExpectedArchiveFileCount,
		expectedFileCoveragePct,
		archiveFileCoveragePct,
		row.TotalBytes,
		earliestPostedAtValue,
		releaseReadinessActionable,
		updatedAtValue,
	); err != nil {
		return fmt.Errorf("upsert ready recovered file-set candidate provider=%d file_set=%q: %w", row.ProviderID, row.FileSetKey, err)
	}

	return nil
}

func normalizeSummaryReleaseCandidateName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("_", " ", ".", " ", "-", " ")
	value = replacer.Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func pgLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func markReleaseFamilyDirty(ctx context.Context, tx *sql.Tx, providerID, newsgroupID int64, keyKind, familyKey string) error {
	if tx == nil {
		return fmt.Errorf("release summary queue tx is required")
	}

	key, ok := normalizeReleaseFamilySummaryKey(providerID, newsgroupID, keyKind, familyKey)
	if !ok {
		return nil
	}

	return markReleaseFamiliesDirtyBatch(ctx, tx, []releaseFamilySummaryKey{key})
}

func markReleaseFamiliesDirtyBatch(ctx context.Context, runner sqlExecQueryer, keys []releaseFamilySummaryKey) error {
	if runner == nil {
		return fmt.Errorf("release summary queue tx is required")
	}
	if len(keys) == 0 {
		return nil
	}

	normalized := make([]releaseFamilySummaryKey, 0, len(keys))
	seen := make(map[releaseFamilySummaryKey]struct{}, len(keys))
	for _, candidate := range keys {
		key, ok := normalizeReleaseFamilySummaryKey(candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, candidate.FamilyKey)
		if !ok {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	if len(normalized) == 0 {
		return nil
	}
	sortReleaseFamilySummaryKeys(normalized)

	for start := 0; start < len(normalized); start += releaseFamilyDirtyBatchSize {
		end := start + releaseFamilyDirtyBatchSize
		if end > len(normalized) {
			end = len(normalized)
		}
		batch := normalized[start:end]
		queueValues := make([]string, 0, len(batch))
		queueArgs := make([]any, 0, len(batch)*4)
		for i, key := range batch {
			base := (i * 4) + 1
			queueValues = append(queueValues, fmt.Sprintf("($%d,$%d,$%d,$%d,NOW())", base, base+1, base+2, base+3))
			queueArgs = append(queueArgs, key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey)
		}
		if _, err := runner.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO release_family_summary_refresh_queue (
			provider_id,
			newsgroup_id,
			key_kind,
			family_key,
			queued_at
		)
		VALUES %s
		ON CONFLICT DO NOTHING`,
			strings.Join(queueValues, ",")), queueArgs...); err != nil {
			return fmt.Errorf("enqueue release family summary refresh batch count=%d: %w", len(batch), err)
		}
	}
	return nil
}

func (s *Store) RefreshQueuedReleaseFamilySummaries(ctx context.Context, limit int) (int, error) {
	metrics, err := s.RefreshQueuedReleaseFamilySummariesWithMetrics(ctx, limit)
	if err != nil {
		return 0, err
	}
	return metrics.Refreshed, nil
}

func (s *Store) RefreshQueuedReleaseFamilySummariesWithMetrics(ctx context.Context, limit int) (ReleaseSummaryRefreshMetrics, error) {
	if s == nil || s.db == nil {
		return ReleaseSummaryRefreshMetrics{}, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 {
		limit = releaseFamilySummaryRefreshBatch
	}
	if limit > releaseFamilySummaryRefreshCap {
		limit = releaseFamilySummaryRefreshCap
	}
	hotLimit := limit
	if hotLimit > releaseFamilySummaryRefreshHotCap {
		hotLimit = releaseFamilySummaryRefreshHotCap
	}

	metrics, err := s.refreshQueuedReleaseFamilySummariesChunk(ctx, hotLimit, releaseSummaryRefreshModeHot)
	if err != nil {
		return ReleaseSummaryRefreshMetrics{}, err
	}
	if err := s.backfillReadyReleaseCandidatesFromActionableSummaries(ctx, hotLimit); err != nil {
		return ReleaseSummaryRefreshMetrics{}, err
	}
	if metrics.Refreshed > 0 {
		return metrics, nil
	}

	coldLimit := limit - metrics.Refreshed
	if coldLimit > releaseFamilySummaryRefreshColdCap {
		coldLimit = releaseFamilySummaryRefreshColdCap
	}
	coldMetrics, err := s.refreshQueuedReleaseFamilySummariesChunk(ctx, coldLimit, releaseSummaryRefreshModeCold)
	if err != nil {
		return ReleaseSummaryRefreshMetrics{}, err
	}
	if err := s.backfillReadyReleaseCandidatesFromActionableSummaries(ctx, coldLimit); err != nil {
		return ReleaseSummaryRefreshMetrics{}, err
	}
	metrics.Refreshed += coldMetrics.Refreshed
	metrics.Dequeued += coldMetrics.Dequeued
	metrics.HotAttempts += coldMetrics.HotAttempts
	metrics.ColdAttempts += coldMetrics.ColdAttempts
	metrics.HotDequeued += coldMetrics.HotDequeued
	metrics.ColdDequeued += coldMetrics.ColdDequeued
	metrics.DequeueDuration += coldMetrics.DequeueDuration
	metrics.SummaryRefreshDuration += coldMetrics.SummaryRefreshDuration
	metrics.SummaryAggregateDuration += coldMetrics.SummaryAggregateDuration
	metrics.SummaryDominantDuration += coldMetrics.SummaryDominantDuration
	metrics.ReadyCandidateSyncDuration += coldMetrics.ReadyCandidateSyncDuration
	metrics.RecoveredFileSetSyncDuration += coldMetrics.RecoveredFileSetSyncDuration
	metrics.PhaseADuration += coldMetrics.PhaseADuration
	metrics.PhaseBDuration += coldMetrics.PhaseBDuration
	if metrics.Mode != "" && coldMetrics.Mode != "" && metrics.Mode != coldMetrics.Mode {
		metrics.Mode = "mixed"
	} else if metrics.Mode == "" {
		metrics.Mode = coldMetrics.Mode
	}
	return metrics, nil
}

func (s *Store) backfillReadyReleaseCandidatesFromActionableSummaries(ctx context.Context, limit int) error {
	if s == nil || s.db == nil || limit <= 0 {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		WITH requested AS (
			SELECT
				s.source_posted_at,
				s.provider_id,
				s.newsgroup_id,
				s.key_kind,
				s.family_key
			FROM release_family_readiness_summaries s
			LEFT JOIN release_ready_candidates c
			  ON c.source_posted_at = s.source_posted_at
			 AND c.provider_id = s.provider_id
			 AND c.newsgroup_id = s.newsgroup_id
			 AND c.key_kind = s.key_kind
			 AND c.family_key = s.family_key
			WHERE s.readiness_bucket = $1
			  AND (
			  	c.family_key IS NULL OR
			  	c.updated_at < s.updated_at
			  )
			ORDER BY
				CASE
					WHEN s.expected_archive_file_count > 0 THEN s.archive_file_coverage_pct
					ELSE s.expected_file_coverage_pct
				END DESC,
				s.complete_main_payload_binary_count DESC,
				s.complete_binary_count DESC,
				s.updated_at DESC
			LIMIT $2
		)
		INSERT INTO release_ready_candidates (
			source_posted_at,
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
			expected_file_count,
			expected_archive_file_count,
			has_expected_file_count,
			has_expected_archive_file_count,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			total_bytes,
			earliest_posted_at,
			ready_reason,
			updated_at
		)
		SELECT
			s.source_posted_at,
			s.provider_id,
			s.newsgroup_id,
			s.key_kind,
			s.family_key,
			COALESCE(s.source_release_key, ''),
			COALESCE(s.release_key, ''),
			COALESCE(s.release_name, ''),
			COALESCE(s.binary_count, 0),
			COALESCE(s.complete_binary_count, 0),
			COALESCE(s.complete_main_payload_binary_count, 0),
			COALESCE(s.expected_file_count, 0),
			COALESCE(s.expected_archive_file_count, 0),
			COALESCE(s.has_expected_file_count, FALSE),
			COALESCE(s.has_expected_archive_file_count, FALSE),
			COALESCE(s.expected_file_coverage_pct, 0)::DOUBLE PRECISION,
			COALESCE(s.archive_file_coverage_pct, 0)::DOUBLE PRECISION,
			COALESCE(s.total_bytes, 0)::BIGINT,
			s.earliest_posted_at,
			$1,
			s.updated_at
		FROM requested r
		JOIN release_family_readiness_summaries s
		  ON s.source_posted_at = r.source_posted_at
		 AND s.provider_id = r.provider_id
		 AND s.newsgroup_id = r.newsgroup_id
		 AND s.key_kind = r.key_kind
		 AND s.family_key = r.family_key
		ON CONFLICT (source_posted_at, provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET source_release_key = EXCLUDED.source_release_key,
		    release_key = EXCLUDED.release_key,
		    release_name = EXCLUDED.release_name,
		    binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    expected_archive_file_count = EXCLUDED.expected_archive_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    has_expected_archive_file_count = EXCLUDED.has_expected_archive_file_count,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    archive_file_coverage_pct = EXCLUDED.archive_file_coverage_pct,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    ready_reason = EXCLUDED.ready_reason,
		    updated_at = EXCLUDED.updated_at`,
		releaseReadinessActionable,
		limit,
	)
	if err != nil {
		return fmt.Errorf("backfill ready release candidates from actionable summaries limit=%d: %w", limit, err)
	}
	return nil
}

func (s *Store) refreshQueuedReleaseFamilySummariesChunk(ctx context.Context, limit int, mode releaseSummaryRefreshMode) (ReleaseSummaryRefreshMetrics, error) {
	if limit <= 0 {
		return ReleaseSummaryRefreshMetrics{}, nil
	}

	candidateWindowLimit := limit * 20
	if candidateWindowLimit < limit {
		candidateWindowLimit = limit
	}
	if candidateWindowLimit < 500 {
		candidateWindowLimit = 500
	}
	if candidateWindowLimit > releaseFamilySummaryRefreshBatch {
		candidateWindowLimit = releaseFamilySummaryRefreshBatch
	}

	metrics := releaseSummaryRefreshMetrics{}
	if err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			return fmt.Errorf("acquire release family summary refresh conn: %w", err)
		}
		defer conn.Close()

		if _, err := conn.ExecContext(ctx, `BEGIN`); err != nil {
			return fmt.Errorf("begin release family summary refresh conn tx: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
			}
		}()

		if mode == releaseSummaryRefreshModeHot {
			phaseAStart := time.Now()
			dequeueStart := time.Now()
			keys, err := dequeueHotReleaseFamilySummaryRefreshKeys(ctx, conn, limit)
			if err != nil {
				return err
			}
			metrics.Mode = "hot"
			metrics.HotAttempts = 1
			metrics.DequeueDuration += time.Since(dequeueStart)
			dequeued := len(keys)
			keys = dedupeReleaseFamilySummaryKeys(keys)
			metrics.Dequeued = dequeued
			metrics.HotDequeued = dequeued
			if len(keys) == 0 {
				if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
					return fmt.Errorf("commit empty release family summary refresh conn tx: %w", err)
				}
				committed = true
				metrics.PhaseADuration += time.Since(phaseAStart)
				return nil
			}
			summaryStart := time.Now()
			phaseAMetrics, err := refreshDequeuedReleaseFamilySummaryKeysPhaseA(ctx, conn, keys)
			if err != nil {
				return err
			}
			_ = summaryStart
			metrics.SummaryRefreshDuration += phaseAMetrics.SummaryRefreshDuration
			metrics.SummaryAggregateDuration += phaseAMetrics.SummaryAggregateDuration
			metrics.SummaryDominantDuration += phaseAMetrics.SummaryDominantDuration
			if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
				return fmt.Errorf("commit release family summary refresh conn tx: %w", err)
			}
			committed = true
			metrics.Refreshed = len(keys)
			metrics.PhaseADuration += time.Since(phaseAStart)
			phaseBMetrics, err := s.finalizeReleaseFamilySummaryMaterialization(ctx, keys)
			if err != nil {
				return err
			}
			metrics.ReadyCandidateSyncDuration += phaseBMetrics.ReadyCandidateSyncDuration
			metrics.RecoveredFileSetSyncDuration += phaseBMetrics.RecoveredFileSetSyncDuration
			metrics.PhaseBDuration += phaseBMetrics.PhaseBDuration
			return nil
		}

		phaseAStart := time.Now()
		dequeueStart := time.Now()
		keys, err := dequeueColdReleaseFamilySummaryRefreshKeys(ctx, conn, limit, candidateWindowLimit)
		if err != nil {
			return err
		}
		metrics.Mode = "cold"
		metrics.ColdAttempts = 1
		metrics.DequeueDuration += time.Since(dequeueStart)
		if len(keys) == 0 {
			if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
				return fmt.Errorf("commit empty release family summary refresh conn tx: %w", err)
			}
			committed = true
			metrics.PhaseADuration += time.Since(phaseAStart)
			return nil
		}
		dequeued := len(keys)
		keys = dedupeReleaseFamilySummaryKeys(keys)
		metrics.Dequeued = dequeued
		metrics.ColdDequeued = dequeued

		summaryStart := time.Now()
		phaseAMetrics, err := refreshDequeuedReleaseFamilySummaryKeysPhaseA(ctx, conn, keys)
		if err != nil {
			return err
		}
		_ = summaryStart
		metrics.SummaryRefreshDuration += phaseAMetrics.SummaryRefreshDuration
		metrics.SummaryAggregateDuration += phaseAMetrics.SummaryAggregateDuration
		metrics.SummaryDominantDuration += phaseAMetrics.SummaryDominantDuration

		if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
			return fmt.Errorf("commit release family summary refresh conn tx: %w", err)
		}
		committed = true
		metrics.Refreshed = len(keys)
		metrics.PhaseADuration += time.Since(phaseAStart)
		phaseBMetrics, err := s.finalizeReleaseFamilySummaryMaterialization(ctx, keys)
		if err != nil {
			return err
		}
		metrics.ReadyCandidateSyncDuration += phaseBMetrics.ReadyCandidateSyncDuration
		metrics.RecoveredFileSetSyncDuration += phaseBMetrics.RecoveredFileSetSyncDuration
		metrics.PhaseBDuration += phaseBMetrics.PhaseBDuration
		return nil
	}); err != nil {
		return ReleaseSummaryRefreshMetrics{}, err
	}
	return ReleaseSummaryRefreshMetrics{
		Refreshed:                    metrics.Refreshed,
		Dequeued:                     metrics.Dequeued,
		Mode:                         metrics.Mode,
		HotAttempts:                  metrics.HotAttempts,
		ColdAttempts:                 metrics.ColdAttempts,
		HotDequeued:                  metrics.HotDequeued,
		ColdDequeued:                 metrics.ColdDequeued,
		DequeueDuration:              metrics.DequeueDuration,
		SummaryRefreshDuration:       metrics.SummaryRefreshDuration,
		SummaryAggregateDuration:     metrics.SummaryAggregateDuration,
		SummaryDominantDuration:      metrics.SummaryDominantDuration,
		ReadyCandidateSyncDuration:   metrics.ReadyCandidateSyncDuration,
		RecoveredFileSetSyncDuration: metrics.RecoveredFileSetSyncDuration,
		PhaseADuration:               metrics.PhaseADuration,
		PhaseBDuration:               metrics.PhaseBDuration,
	}, nil
}

func dedupeReleaseFamilySummaryKeys(keys []releaseFamilySummaryKey) []releaseFamilySummaryKey {
	if len(keys) == 0 {
		return nil
	}
	out := make([]releaseFamilySummaryKey, 0, len(keys))
	seen := make(map[releaseFamilySummaryKey]struct{}, len(keys))
	for _, candidate := range keys {
		key, ok := normalizeReleaseFamilySummaryKey(candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, candidate.FamilyKey)
		if !ok {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	sortReleaseFamilySummaryKeys(out)
	return out
}

func refreshDequeuedReleaseFamilySummaryKeysPhaseA(ctx context.Context, conn *sql.Conn, keys []releaseFamilySummaryKey) (releaseSummaryRefreshMetrics, error) {
	metrics := releaseSummaryRefreshMetrics{}
	releaseFamilyKeys := make([]releaseFamilySummaryKey, 0, len(keys))
	baseStemKeys := make([]releaseFamilySummaryKey, 0, len(keys))
	otherKeys := make([]releaseFamilySummaryKey, 0, len(keys))
	for _, key := range keys {
		switch key.KeyKind {
		case "release_family":
			releaseFamilyKeys = append(releaseFamilyKeys, key)
		case "base_stem":
			baseStemKeys = append(baseStemKeys, key)
		default:
			otherKeys = append(otherKeys, key)
		}
	}

	phaseMetrics, err := refreshReleaseFamilySummariesBatchCopyWithMetrics(ctx, conn, releaseFamilyKeys)
	if err != nil {
		return metrics, err
	}
	metrics.SummaryRefreshDuration += phaseMetrics.SummaryRefreshDuration
	metrics.SummaryAggregateDuration += phaseMetrics.SummaryAggregateDuration
	metrics.SummaryDominantDuration += phaseMetrics.SummaryDominantDuration
	phaseMetrics, err = refreshBaseStemSummariesBatchCopyWithMetrics(ctx, conn, baseStemKeys)
	if err != nil {
		return metrics, err
	}
	metrics.SummaryRefreshDuration += phaseMetrics.SummaryRefreshDuration
	metrics.SummaryAggregateDuration += phaseMetrics.SummaryAggregateDuration
	metrics.SummaryDominantDuration += phaseMetrics.SummaryDominantDuration
	for _, key := range otherKeys {
		if err := refreshReleaseFamilySummaryConn(ctx, conn, key); err != nil {
			return metrics, err
		}
	}
	return metrics, nil
}

func (s *Store) finalizeReleaseFamilySummaryMaterialization(ctx context.Context, keys []releaseFamilySummaryKey) (releaseSummaryRefreshMetrics, error) {
	metrics := releaseSummaryRefreshMetrics{}
	if len(keys) == 0 {
		return metrics, nil
	}

	if err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			return fmt.Errorf("acquire release family summary materialization conn: %w", err)
		}
		defer conn.Close()

		if _, err := conn.ExecContext(ctx, `BEGIN`); err != nil {
			return fmt.Errorf("begin release family summary materialization conn tx: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
			}
		}()

		phaseBStart := time.Now()
		readySyncStart := time.Now()
		if err := finalizeReleaseCandidateMaterializationWithoutRecoveredFileSets(ctx, conn, keys); err != nil {
			return err
		}
		metrics.ReadyCandidateSyncDuration += time.Since(readySyncStart)

		recoveredStart := time.Now()
		if err := refreshRecoveredFileSetCandidatesForSummaryKeys(ctx, conn, keys); err != nil {
			return err
		}
		metrics.RecoveredFileSetSyncDuration += time.Since(recoveredStart)

		if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
			return fmt.Errorf("commit release family summary materialization conn tx: %w", err)
		}
		committed = true
		metrics.PhaseBDuration += time.Since(phaseBStart)
		return nil
	}); err != nil {
		return releaseSummaryRefreshMetrics{}, err
	}

	return metrics, nil
}

func dequeueHotReleaseFamilySummaryRefreshKeys(ctx context.Context, conn *sql.Conn, limit int) ([]releaseFamilySummaryKey, error) {
	branches := []struct {
		query string
		args  []any
	}{
		{
			query: `
				WITH candidate_rows AS (
					SELECT
						q.ctid,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key
					FROM release_family_readiness_summaries s
					JOIN release_family_summary_refresh_queue q
					  ON q.provider_id = s.provider_id
					 AND q.newsgroup_id = s.newsgroup_id
					 AND q.key_kind = s.key_kind
					 AND q.family_key = s.family_key
					WHERE s.readiness_bucket = $2
					ORDER BY q.queued_at, q.provider_id, q.newsgroup_id, q.key_kind, q.family_key
					LIMIT $1
					FOR UPDATE OF q SKIP LOCKED
				),
				dequeued AS (
					DELETE FROM release_family_summary_refresh_queue q
					USING candidate_rows c
					WHERE q.ctid = c.ctid
					RETURNING c.provider_id, c.newsgroup_id, c.key_kind, c.family_key
				)
				SELECT provider_id, newsgroup_id, key_kind, family_key
				FROM dequeued`,
			args: []any{limit, releaseReadinessActionable},
		},
		{
			query: `
				WITH ordered_queue AS MATERIALIZED (
					SELECT
						q.ctid,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key,
						q.queued_at
					FROM release_family_summary_refresh_queue q
					ORDER BY
						q.queued_at,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key
					LIMIT $2
				),
				candidate_rows AS (
					SELECT
						q.ctid,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key
					FROM release_family_summary_refresh_queue q
					JOIN ordered_queue oq ON oq.ctid = q.ctid
					WHERE NOT EXISTS (
						SELECT 1
						FROM release_family_readiness_summaries s
						WHERE s.provider_id = oq.provider_id
						  AND s.newsgroup_id = oq.newsgroup_id
						  AND s.key_kind = oq.key_kind
						  AND s.family_key = oq.family_key
					)
					ORDER BY oq.queued_at, oq.provider_id, oq.newsgroup_id, oq.key_kind, oq.family_key
					LIMIT $1
					FOR UPDATE OF q SKIP LOCKED
				),
				dequeued AS (
					DELETE FROM release_family_summary_refresh_queue q
					USING candidate_rows c
					WHERE q.ctid = c.ctid
					RETURNING c.provider_id, c.newsgroup_id, c.key_kind, c.family_key
				)
				SELECT provider_id, newsgroup_id, key_kind, family_key
				FROM dequeued`,
			args: []any{limit, releaseFamilySummaryRefreshBatch},
		},
		{
			query: `
				WITH candidate_rows AS (
					SELECT
						q.ctid,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key
					FROM release_family_summary_refresh_queue q
					WHERE q.key_kind = 'base_stem'
					ORDER BY q.queued_at, q.provider_id, q.newsgroup_id, q.family_key
					LIMIT $1
					FOR UPDATE OF q SKIP LOCKED
				),
				dequeued AS (
					DELETE FROM release_family_summary_refresh_queue q
					USING candidate_rows c
					WHERE q.ctid = c.ctid
					RETURNING c.provider_id, c.newsgroup_id, c.key_kind, c.family_key
				)
				SELECT provider_id, newsgroup_id, key_kind, family_key
				FROM dequeued`,
			args: []any{limit},
		},
	}

	keys := make([]releaseFamilySummaryKey, 0, limit)
	for _, branch := range branches {
		remaining := limit - len(keys)
		if remaining <= 0 {
			break
		}
		args := append([]any(nil), branch.args...)
		if len(args) > 0 {
			args[0] = remaining
		}

		rows, err := conn.QueryContext(ctx, branch.query, args...)
		if err != nil {
			return nil, fmt.Errorf("dequeue hot release family summary refresh branch: %w", err)
		}
		for rows.Next() {
			var key releaseFamilySummaryKey
			if err := rows.Scan(&key.ProviderID, &key.NewsgroupID, &key.KeyKind, &key.FamilyKey); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan hot release family summary refresh queue key: %w", err)
			}
			keys = append(keys, key)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate hot release family summary refresh queue: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close hot release family summary refresh queue rows: %w", err)
		}
	}

	return keys, nil
}

func dequeueColdReleaseFamilySummaryRefreshKeys(ctx context.Context, conn *sql.Conn, limit, candidateWindowLimit int) ([]releaseFamilySummaryKey, error) {
	branches := []struct {
		query string
		args  []any
	}{
		{
			query: `
				WITH candidate_rows AS (
					SELECT
						q.ctid,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key
					FROM release_family_readiness_summaries s
					JOIN release_family_summary_refresh_queue q
					  ON q.provider_id = s.provider_id
					 AND q.newsgroup_id = s.newsgroup_id
					 AND q.key_kind = s.key_kind
					 AND q.family_key = s.family_key
					WHERE s.readiness_bucket = $2
					ORDER BY q.queued_at, q.provider_id, q.newsgroup_id, q.key_kind, q.family_key
					LIMIT $1
					FOR UPDATE OF q SKIP LOCKED
				),
				dequeued AS (
					DELETE FROM release_family_summary_refresh_queue q
					USING candidate_rows c
					WHERE q.ctid = c.ctid
					RETURNING c.provider_id, c.newsgroup_id, c.key_kind, c.family_key
				)
				SELECT provider_id, newsgroup_id, key_kind, family_key
				FROM dequeued`,
			args: []any{limit, releaseReadinessFragmentOnly},
		},
		{
			query: `
				WITH candidate_rows AS (
					SELECT
						q.ctid,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key
					FROM release_family_readiness_summaries s
					JOIN release_family_summary_refresh_queue q
					  ON q.provider_id = s.provider_id
					 AND q.newsgroup_id = s.newsgroup_id
					 AND q.key_kind = s.key_kind
					 AND q.family_key = s.family_key
					WHERE s.readiness_bucket IN ($2, $3, $4)
					ORDER BY q.queued_at, q.provider_id, q.newsgroup_id, q.key_kind, q.family_key
					LIMIT $1
					FOR UPDATE OF q SKIP LOCKED
				),
				dequeued AS (
					DELETE FROM release_family_summary_refresh_queue q
					USING candidate_rows c
					WHERE q.ctid = c.ctid
					RETURNING c.provider_id, c.newsgroup_id, c.key_kind, c.family_key
				)
				SELECT provider_id, newsgroup_id, key_kind, family_key
				FROM dequeued`,
			args: []any{limit, releaseReadinessWeakObfuscated, releaseReadinessStaleCleanupOnly, releaseReadinessWeakSingle},
		},
		{
			query: `
				WITH ordered_queue AS MATERIALIZED (
					SELECT
						q.ctid,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key,
						q.queued_at
					FROM release_family_summary_refresh_queue q
					ORDER BY
						q.queued_at,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key
					LIMIT $2
				),
				candidate_rows AS (
					SELECT
						q.ctid,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key
					FROM release_family_summary_refresh_queue q
					JOIN ordered_queue oq ON oq.ctid = q.ctid
					WHERE NOT EXISTS (
						SELECT 1
						FROM release_family_readiness_summaries s
						WHERE s.provider_id = oq.provider_id
						  AND s.newsgroup_id = oq.newsgroup_id
						  AND s.key_kind = oq.key_kind
						  AND s.family_key = oq.family_key
					)
					ORDER BY oq.queued_at, oq.provider_id, oq.newsgroup_id, oq.key_kind, oq.family_key
					LIMIT $1
					FOR UPDATE OF q SKIP LOCKED
				),
				dequeued AS (
					DELETE FROM release_family_summary_refresh_queue q
					USING candidate_rows c
					WHERE q.ctid = c.ctid
					RETURNING c.provider_id, c.newsgroup_id, c.key_kind, c.family_key
				)
				SELECT provider_id, newsgroup_id, key_kind, family_key
				FROM dequeued`,
			args: []any{limit, candidateWindowLimit},
		},
	}

	keys := make([]releaseFamilySummaryKey, 0, limit)
	for _, branch := range branches {
		remaining := limit - len(keys)
		if remaining <= 0 {
			break
		}
		args := append([]any(nil), branch.args...)
		if len(args) > 0 {
			args[0] = remaining
		}

		rows, err := conn.QueryContext(ctx, branch.query, args...)
		if err != nil {
			return nil, fmt.Errorf("dequeue cold release family summary refresh branch: %w", err)
		}
		for rows.Next() {
			var key releaseFamilySummaryKey
			if err := rows.Scan(&key.ProviderID, &key.NewsgroupID, &key.KeyKind, &key.FamilyKey); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan cold release family summary refresh queue key: %w", err)
			}
			keys = append(keys, key)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate cold release family summary refresh queue: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close cold release family summary refresh queue rows: %w", err)
		}
	}

	return keys, nil
}

func (s *Store) CountQueuedReleaseFamilySummaries(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("pgindex store is not initialized")
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM release_family_summary_refresh_queue`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count queued release family summaries: %w", err)
	}
	return count, nil
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
