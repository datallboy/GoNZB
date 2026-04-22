package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const (
	releaseReadinessActionable       = "actionable"
	releaseReadinessFragmentOnly     = "fragment_only"
	releaseReadinessStaleCleanupOnly = "stale_cleanup_only"
)

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
			AND b.expected_file_count > 1
			AND BTRIM(b.base_stem) <> ''
			AND LOWER(BTRIM(b.base_stem)) = $3`
	}

	var (
		sourceReleaseKey     string
		releaseKey           string
		releaseName          string
		binaryCount          int
		completeBinaryCount  int
		hasExpectedFileCount bool
		totalBytes           int64
		earliestPostedAt     sql.NullTime
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
			COALESCE(BOOL_OR(b.expected_file_count > 0), FALSE) AS has_expected_file_count,
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
		&hasExpectedFileCount,
		&totalBytes,
		&earliestPostedAt,
	); err != nil {
		return fmt.Errorf("query release family summary provider=%d group=%d kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
	}

	if binaryCount == 0 {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM release_family_readiness_summaries
			WHERE provider_id = $1
			  AND newsgroup_id = $2
			  AND key_kind = $3
			  AND family_key = $4`,
			key.ProviderID,
			key.NewsgroupID,
			key.KeyKind,
			key.FamilyKey,
		); err != nil {
			return fmt.Errorf("delete empty release family summary provider=%d group=%d kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
		}
		return nil
	}

	readinessBucket := releaseReadinessFragmentOnly
	if completeBinaryCount > 0 {
		readinessBucket = releaseReadinessActionable
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
			incomplete_binary_count,
			has_expected_file_count,
			total_bytes,
			earliest_posted_at,
			readiness_bucket,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NOW())
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET source_release_key = EXCLUDED.source_release_key,
		    release_key = EXCLUDED.release_key,
		    release_name = EXCLUDED.release_name,
		    binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    readiness_bucket = EXCLUDED.readiness_bucket,
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
		binaryCount-completeBinaryCount,
		hasExpectedFileCount,
		totalBytes,
		earliestPostedAtValue,
		readinessBucket,
	); err != nil {
		return fmt.Errorf("upsert release family summary provider=%d group=%d kind=%s family=%q: %w", key.ProviderID, key.NewsgroupID, key.KeyKind, key.FamilyKey, err)
	}

	return nil
}
