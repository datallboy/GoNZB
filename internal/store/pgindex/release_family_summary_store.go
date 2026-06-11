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
	releaseFamilySummaryRefreshBatch         = 5000
	releaseFamilySummaryRefreshCap           = 1000
	releaseFamilySummaryRefreshHotCap        = 200
	releaseFamilySummaryRefreshColdCap       = 100
	releaseFamilySummaryRefreshQueryBatchCap = 25
	releaseFamilySummaryMergeRowsMax         = 2500
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
	}

	return releaseFamilySummaryKey{
		ProviderID:  providerID,
		NewsgroupID: newsgroupID,
		KeyKind:     keyKind,
		FamilyKey:   familyKey,
	}, true
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
				recover_pending,
				expected_file_coverage_pct,
				archive_file_coverage_pct,
				processed_at,
				updated_at
			)
			VALUES ($1,$2,$3,$4,'','','',0,0,0,0,0,0,FALSE,FALSE,0,NULL,'','',0,$5,FALSE,0,0,TIMESTAMPTZ 'epoch',NOW())
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
			recover_pending,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			processed_at,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,FALSE,$22,$23,TIMESTAMPTZ 'epoch',NOW())
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
				COALESCE(MAX(b.source_release_key), '') AS source_release_key,
				COALESCE(MAX(b.release_key), '') AS release_key,
				COALESCE(MAX(b.release_name), '') AS release_name,
				COUNT(b.id)::INTEGER AS binary_count,
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
			FROM requested r
			LEFT JOIN binaries b
			  ON b.provider_id = r.provider_id
			 AND b.newsgroup_id = r.newsgroup_id
			 AND b.release_family_key = r.family_key
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
					b.family_kind,
					b.file_name,
					b.binary_name,
					b.match_confidence,
					ROW_NUMBER() OVER (
						PARTITION BY r.provider_id, r.newsgroup_id, r.key_kind, r.family_key
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
					) AS row_num
				FROM requested r
				LEFT JOIN binaries b
				  ON b.provider_id = r.provider_id
				 AND b.newsgroup_id = r.newsgroup_id
				 AND b.release_family_key = r.family_key
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
		WITH requested(provider_id, newsgroup_id, key_kind, family_key) AS (
			VALUES %s
		)
		SELECT
			r.provider_id,
			r.newsgroup_id,
			r.key_kind,
			r.family_key,
			COALESCE(MAX(b.source_release_key), '') AS source_release_key,
			COALESCE(MAX(b.release_key), '') AS release_key,
			COALESCE(MAX(b.release_name), '') AS release_name,
			COUNT(b.id)::INTEGER AS binary_count,
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
		FROM requested r
		LEFT JOIN binaries b
		  ON b.provider_id = r.provider_id
		 AND b.newsgroup_id = r.newsgroup_id
		 AND b.release_family_key = r.family_key
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
		WITH requested(provider_id, newsgroup_id, key_kind, family_key) AS (
			VALUES %s
		)
		SELECT DISTINCT ON (r.provider_id, r.newsgroup_id, r.key_kind, r.family_key)
			r.provider_id,
			r.newsgroup_id,
			r.key_kind,
			r.family_key,
			COALESCE(b.family_kind, '') AS dominant_family_kind,
			COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '') AS dominant_file_name,
			COALESCE(b.match_confidence, 0)::DOUBLE PRECISION AS dominant_match_confidence
		FROM requested r
		LEFT JOIN binaries b
		  ON b.provider_id = r.provider_id
		 AND b.newsgroup_id = r.newsgroup_id
		 AND b.release_family_key = r.family_key
		ORDER BY
			r.provider_id,
			r.newsgroup_id,
			r.key_kind,
			r.family_key,
			CASE WHEN (COALESCE(b.is_main_payload, FALSE) OR NOT COALESCE(b.is_auxiliary, FALSE)) THEN 0 ELSE 1 END ASC,
			CASE WHEN COALESCE(b.total_parts, 0) > 0 AND COALESCE(b.observed_parts, 0) = COALESCE(b.total_parts, 0) THEN 0 ELSE 1 END ASC,
			COALESCE(b.observed_parts, 0) DESC,
			COALESCE(b.total_bytes, 0) DESC,
			COALESCE(b.match_confidence, 0) DESC,
			COALESCE(b.id, 0) ASC`,
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

func refreshReleaseFamilySummaryConn(ctx context.Context, conn *sql.Conn, key releaseFamilySummaryKey) error {
	if conn == nil {
		return fmt.Errorf("release family summary conn is required")
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

	var row releaseFamilySummaryRow
	query := `
		SELECT
			COALESCE(MAX(b.source_release_key), '') AS source_release_key,
			COALESCE(MAX(b.release_key), '') AS release_key,
			COALESCE(MAX(b.release_name), '') AS release_name,
			COUNT(*)::INTEGER AS binary_count,
			COALESCE(SUM(
				CASE WHEN b.observed_parts = b.total_parts AND b.total_parts > 0 THEN 1 ELSE 0 END
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
			COALESCE(b.family_kind, ''),
			COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), ''),
			COALESCE(b.match_confidence, 0)
		FROM binaries b
		WHERE ` + whereClause + `
		ORDER BY
			CASE WHEN (b.is_main_payload OR NOT b.is_auxiliary) THEN 0 ELSE 1 END ASC,
			CASE WHEN b.total_parts > 0 AND b.observed_parts = b.total_parts THEN 0 ELSE 1 END ASC,
			b.observed_parts DESC,
			b.total_bytes DESC,
			b.match_confidence DESC,
			b.id ASC
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
		args := make([]any, 0, len(batch)*24)
		for i, row := range batch {
			record := buildReleaseFamilySummaryRefreshRecord(row)
			base := i*24 + 1
			placeholders := make([]string, 0, 24)
			for offset := range 24 {
				placeholders = append(placeholders, fmt.Sprintf("$%d", base+offset))
			}
			values = append(values, "("+strings.Join(placeholders, ",")+",TIMESTAMPTZ 'epoch',NOW())")
			args = append(args, record...)
		}

		if _, err := runner.ExecContext(ctx, fmt.Sprintf(`
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
			recover_pending,
			expected_file_coverage_pct,
			archive_file_coverage_pct,
			processed_at,
			updated_at
		)
		VALUES %s
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
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
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
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
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
			COALESCE(BOOL_AND(LOWER(COALESCE(b.family_kind, '')) = 'contextual_obfuscated'), FALSE) AS all_contextual,
			COALESCE(MAX(GREATEST(b.expected_file_count, b.expected_archive_file_count)), 0)::INTEGER AS max_expected_any_file_count,
			COUNT(*) FILTER (WHERE b.file_index > 0)::INTEGER AS indexed_file_count,
			COUNT(*) FILTER (WHERE BTRIM(COALESCE(b.base_stem, '')) <> '')::INTEGER AS base_stem_file_count,
			COUNT(DISTINCT LOWER(BTRIM(COALESCE(b.base_stem, '')))) FILTER (
				WHERE BTRIM(COALESCE(b.base_stem, '')) <> ''
			)::INTEGER AS distinct_base_stem_count,
			COALESCE(BOOL_OR(
				LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '')) ~
				'\.(rar|zip|7z|7z\.[0-9]{3}|zip\.[0-9]{3}|r[0-9]{2,3}|part[0-9]+\.rar|mkv|mp4|avi|ts|mp3|flac|m4a|par2)$'
			), FALSE) AS has_usable_file_identity
		FROM binaries b
		WHERE b.provider_id = $1
		  AND b.newsgroup_id = $2
		  AND b.release_family_key = $3
		  AND (b.is_main_payload = TRUE OR b.is_auxiliary = FALSE)`,
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
			b.provider_id = $1
			AND b.newsgroup_id = $2
			AND b.release_family_key = $3`
	if key.KeyKind == ReleaseCandidateKeyKindBaseStem {
		matchClause = `
			b.provider_id = $1
			AND b.newsgroup_id = $2
			AND GREATEST(b.expected_file_count, b.expected_archive_file_count) > 1
			AND BTRIM(COALESCE(b.base_stem, '')) <> ''
			AND LOWER(BTRIM(b.base_stem)) = $3`
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM binaries b
			JOIN LATERAL (
				SELECT bp.article_header_id
				FROM binary_parts bp
				WHERE bp.binary_id = b.id
				ORDER BY bp.part_number, bp.id
				LIMIT 1
			) bp ON TRUE
			JOIN article_header_ingest_payloads p
			  ON p.article_header_id = bp.article_header_id
			WHERE ` + matchClause + `
			  AND b.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND b.is_main_payload = TRUE
			  AND COALESCE(b.recovered_source, '') <> 'yenc_header'
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
	matchClause := `
			b.provider_id = $1
			AND b.newsgroup_id = $2
			AND b.release_family_key = $3`
	_ = matchClause

	rows, err := runner.QueryContext(ctx, fmt.Sprintf(`
		WITH requested(provider_id, newsgroup_id, key_kind, family_key) AS (
			VALUES %s
		)
		SELECT DISTINCT b.provider_id, b.file_set_key
		FROM requested r
		JOIN binaries b
		  ON b.provider_id = r.provider_id
		 AND b.newsgroup_id = r.newsgroup_id
		 AND (
			(r.key_kind = 'release_family' AND b.release_family_key = r.family_key)
			OR
			(r.key_kind = 'base_stem'
			 AND GREATEST(b.expected_file_count, b.expected_archive_file_count) > 1
			 AND BTRIM(COALESCE(b.base_stem, '')) <> ''
			 AND LOWER(BTRIM(b.base_stem)) = r.family_key)
		 )
		WHERE COALESCE(b.recovered_source, '') = 'yenc_header'
		  AND BTRIM(b.file_set_key) <> ''
		  AND b.posted_at IS NOT NULL`,
		values), args...)
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
	byProvider := make(map[int64][]string, 1)
	for _, item := range fileSetKeys {
		byProvider[item.ProviderID] = append(byProvider[item.ProviderID], item.FileSetKey)
	}
	for providerID, providerFileSetKeys := range byProvider {
		if err := refreshRecoveredFileSetCandidatesBatch(ctx, runner, providerID, providerFileSetKeys); err != nil {
			return err
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

	rows, err := runner.QueryContext(ctx, fmt.Sprintf(`
		WITH requested(file_set_key) AS (
			VALUES %s
		)
		SELECT
			$1::BIGINT AS provider_id,
			r.file_set_key,
			COALESCE(MIN(b.newsgroup_id), 0)::BIGINT AS representative_newsgroup_id,
			COALESCE(MAX(NULLIF(BTRIM(b.source_release_key), '')), r.file_set_key) AS source_release_key,
			r.file_set_key AS release_key,
			COALESCE(MAX(NULLIF(BTRIM(b.release_name), '')), r.file_set_key) AS release_name,
			COUNT(b.id)::INTEGER AS binary_count,
			COUNT(*) FILTER (
				WHERE b.total_parts > 0 AND b.observed_parts = b.total_parts
			)::INTEGER AS complete_binary_count,
			COUNT(*) FILTER (
				WHERE (b.is_main_payload = TRUE OR b.is_auxiliary = FALSE)
				  AND b.total_parts > 0
				  AND b.observed_parts = b.total_parts
			)::INTEGER AS complete_main_payload_binary_count,
			COALESCE(MAX(b.expected_file_count), 0)::INTEGER AS expected_file_count,
			COALESCE(MAX(b.expected_archive_file_count), 0)::INTEGER AS expected_archive_file_count,
			COALESCE(BOOL_OR(b.expected_file_count > 0), FALSE) AS has_expected_file_count,
			COALESCE(BOOL_OR(b.expected_archive_file_count > 0), FALSE) AS has_expected_archive_file_count,
			COALESCE(SUM(b.total_bytes), 0)::BIGINT AS total_bytes,
			MIN(b.posted_at) AS earliest_posted_at,
			MAX(b.posted_at) AS latest_posted_at,
			MAX(b.updated_at) AS max_updated_at,
			COUNT(DISTINCT b.newsgroup_id)::INTEGER AS distinct_newsgroup_count,
			COUNT(*) FILTER (
				WHERE b.is_main_payload = TRUE OR b.is_auxiliary = FALSE
			)::INTEGER AS main_payload_binary_count
		FROM requested r
		LEFT JOIN binaries b
		  ON b.provider_id = $1
		 AND b.file_set_key = r.file_set_key
		 AND COALESCE(b.recovered_source, '') = 'yenc_header'
		 AND BTRIM(b.file_set_key) <> ''
		 AND b.posted_at IS NOT NULL
		GROUP BY r.file_set_key
		ORDER BY r.file_set_key`,
		strings.Join(values, ",")), args...)
	if err != nil {
		return fmt.Errorf("query recovered file-set candidate batch provider=%d count=%d: %w", providerID, len(values), err)
	}
	defer rows.Close()

	aggregates := make([]recoveredFileSetCandidateAggregate, 0, len(values))
	for rows.Next() {
		var row recoveredFileSetCandidateAggregate
		if err := rows.Scan(
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
			return fmt.Errorf("scan recovered file-set candidate batch row: %w", err)
		}
		aggregates = append(aggregates, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate recovered file-set candidate batch provider=%d: %w", providerID, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close recovered file-set candidate batch provider=%d rows: %w", providerID, err)
	}
	for _, row := range aggregates {
		if err := upsertRecoveredFileSetCandidateAggregate(ctx, runner, row); err != nil {
			return err
		}
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
			COALESCE(MIN(b.newsgroup_id), 0)::BIGINT AS representative_newsgroup_id,
			COALESCE(MAX(NULLIF(BTRIM(b.source_release_key), '')), $2) AS source_release_key,
			$2 AS release_key,
			COALESCE(MAX(NULLIF(BTRIM(b.release_name), '')), $2) AS release_name,
			COUNT(*)::INTEGER AS binary_count,
			COUNT(*) FILTER (
				WHERE b.total_parts > 0 AND b.observed_parts = b.total_parts
			)::INTEGER AS complete_binary_count,
			COUNT(*) FILTER (
				WHERE (b.is_main_payload = TRUE OR b.is_auxiliary = FALSE)
				  AND b.total_parts > 0
				  AND b.observed_parts = b.total_parts
			)::INTEGER AS complete_main_payload_binary_count,
			COALESCE(MAX(b.expected_file_count), 0)::INTEGER AS expected_file_count,
			COALESCE(MAX(b.expected_archive_file_count), 0)::INTEGER AS expected_archive_file_count,
			COALESCE(BOOL_OR(b.expected_file_count > 0), FALSE) AS has_expected_file_count,
			COALESCE(BOOL_OR(b.expected_archive_file_count > 0), FALSE) AS has_expected_archive_file_count,
			COALESCE(SUM(b.total_bytes), 0)::BIGINT AS total_bytes,
			MIN(b.posted_at) AS earliest_posted_at,
			MAX(b.posted_at) AS latest_posted_at,
			MAX(b.updated_at) AS max_updated_at,
			COUNT(DISTINCT b.newsgroup_id)::INTEGER AS distinct_newsgroup_count,
			COUNT(*) FILTER (
				WHERE b.is_main_payload = TRUE OR b.is_auxiliary = FALSE
			)::INTEGER AS main_payload_binary_count
		FROM binaries b
		WHERE b.provider_id = $1
		  AND b.file_set_key = $2
		  AND COALESCE(b.recovered_source, '') = 'yenc_header'
		  AND BTRIM(b.file_set_key) <> ''
		  AND b.posted_at IS NOT NULL`,
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
	maxExpected := maxInt(row.ExpectedFileCount, row.ExpectedArchiveFileCount)
	if row.BinaryCount == 0 ||
		row.DistinctNewsgroupCount <= 1 ||
		row.MainPayloadBinaryCount < 2 ||
		maxExpected <= 1 ||
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
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT (provider_id, file_set_key) DO UPDATE
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
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
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
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO NOTHING`,
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

	coldLimit := limit
	if coldLimit > releaseFamilySummaryRefreshColdCap {
		coldLimit = releaseFamilySummaryRefreshColdCap
	}
	metrics, err = s.refreshQueuedReleaseFamilySummariesChunk(ctx, coldLimit, releaseSummaryRefreshModeCold)
	if err != nil {
		return ReleaseSummaryRefreshMetrics{}, err
	}
	if err := s.backfillReadyReleaseCandidatesFromActionableSummaries(ctx, coldLimit); err != nil {
		return ReleaseSummaryRefreshMetrics{}, err
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
				s.provider_id,
				s.newsgroup_id,
				s.key_kind,
				s.family_key
			FROM release_family_readiness_summaries s
			LEFT JOIN release_ready_candidates c
			  ON c.provider_id = s.provider_id
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
		  ON s.provider_id = r.provider_id
		 AND s.newsgroup_id = r.newsgroup_id
		 AND s.key_kind = r.key_kind
		 AND s.family_key = r.family_key
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
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
			metrics.DequeueDuration += time.Since(dequeueStart)
			metrics.Dequeued = len(keys)
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
		var rows *sql.Rows
		dequeueStart := time.Now()
		rows, err = conn.QueryContext(ctx, `
				WITH scored_queue AS (
					SELECT
						q.ctid,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key,
						q.queued_at,
						CASE
							WHEN COALESCE(s.readiness_bucket, '') = $3 THEN 0
							WHEN COALESCE(s.readiness_bucket, '') = $4 THEN 1
							WHEN q.key_kind = 'base_stem' THEN 2
							WHEN s.family_key IS NULL THEN 4
							WHEN COALESCE(s.readiness_bucket, '') = $5 THEN 5
							WHEN COALESCE(s.readiness_bucket, '') = $6 THEN 6
							WHEN COALESCE(s.readiness_bucket, '') = $7 THEN 7
							ELSE 8
						END AS priority_rank
					FROM release_family_summary_refresh_queue q
					LEFT JOIN release_family_readiness_summaries s
					  ON s.provider_id = q.provider_id
					 AND s.newsgroup_id = q.newsgroup_id
					 AND s.key_kind = q.key_kind
					 AND s.family_key = q.family_key
					ORDER BY
						priority_rank,
						q.queued_at,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key
					LIMIT $2
				),
				locked_queue AS (
					SELECT
						q.ctid,
						q.provider_id,
						q.newsgroup_id,
						q.key_kind,
						q.family_key,
						sq.queued_at,
						sq.priority_rank
					FROM release_family_summary_refresh_queue q
					JOIN scored_queue sq ON sq.ctid = q.ctid
					FOR UPDATE OF q SKIP LOCKED
				),
				next_queue AS (
					SELECT
						lq.ctid,
						lq.provider_id,
						lq.newsgroup_id,
						lq.key_kind,
						lq.family_key
					FROM locked_queue lq
					WHERE lq.priority_rank >= 4
					ORDER BY
						lq.priority_rank,
						lq.queued_at,
						lq.provider_id,
						lq.newsgroup_id,
						lq.key_kind,
						lq.family_key
					LIMIT $1
				),
				dequeued AS (
					DELETE FROM release_family_summary_refresh_queue q
					USING next_queue n
					WHERE q.ctid = n.ctid
					RETURNING n.provider_id, n.newsgroup_id, n.key_kind, n.family_key
				)
				SELECT provider_id, newsgroup_id, key_kind, family_key
				FROM dequeued`,
			limit,
			candidateWindowLimit,
			releaseReadinessActionable,
			releaseReadinessFragmentOnly,
			releaseReadinessWeakObfuscated,
			releaseReadinessStaleCleanupOnly,
			releaseReadinessWeakSingle,
		)
		if err != nil {
			return fmt.Errorf("dequeue release family summary refresh batch: %w", err)
		}
		metrics.Mode = "cold"
		metrics.DequeueDuration += time.Since(dequeueStart)

		keys := make([]releaseFamilySummaryKey, 0, limit)
		for rows.Next() {
			var key releaseFamilySummaryKey
			if err := rows.Scan(&key.ProviderID, &key.NewsgroupID, &key.KeyKind, &key.FamilyKey); err != nil {
				rows.Close()
				return fmt.Errorf("scan release family summary refresh queue key: %w", err)
			}
			keys = append(keys, key)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate release family summary refresh queue: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close release family summary refresh queue rows: %w", err)
		}
		if len(keys) == 0 {
			if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
				return fmt.Errorf("commit empty release family summary refresh conn tx: %w", err)
			}
			committed = true
			metrics.PhaseADuration += time.Since(phaseAStart)
			return nil
		}
		metrics.Dequeued = len(keys)

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

func refreshDequeuedReleaseFamilySummaryKeysPhaseA(ctx context.Context, conn *sql.Conn, keys []releaseFamilySummaryKey) (releaseSummaryRefreshMetrics, error) {
	metrics := releaseSummaryRefreshMetrics{}
	releaseFamilyKeys := make([]releaseFamilySummaryKey, 0, len(keys))
	otherKeys := make([]releaseFamilySummaryKey, 0, len(keys))
	for _, key := range keys {
		if key.KeyKind == "release_family" {
			releaseFamilyKeys = append(releaseFamilyKeys, key)
			continue
		}
		otherKeys = append(otherKeys, key)
	}

	phaseMetrics, err := refreshReleaseFamilySummariesBatchCopyWithMetrics(ctx, conn, releaseFamilyKeys)
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
					LEFT JOIN release_family_readiness_summaries s
					  ON s.provider_id = q.provider_id
					 AND s.newsgroup_id = q.newsgroup_id
					 AND s.key_kind = q.key_kind
					 AND s.family_key = q.family_key
					WHERE s.family_key IS NULL
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
			args: []any{limit},
		},
	}

	for _, branch := range branches {
		rows, err := conn.QueryContext(ctx, branch.query, branch.args...)
		if err != nil {
			return nil, fmt.Errorf("dequeue hot release family summary refresh branch: %w", err)
		}
		keys := make([]releaseFamilySummaryKey, 0, limit)
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
		if len(keys) > 0 {
			return keys, nil
		}
	}

	return nil, nil
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
