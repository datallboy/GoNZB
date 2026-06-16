package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/categories/newsnab"
	"github.com/segmentio/ksuid"
)

// binary summary returned to release formation.
type BinarySummary struct {
	BinaryID                 int64
	ProviderID               int64
	NewsgroupID              int64
	SourceReleaseKey         string
	ReleaseFamilyKey         string
	FileFamilyKey            string
	FamilyKind               string
	BaseStem                 string
	IsAuxiliary              bool
	IsMainPayload            bool
	ReleaseKey               string
	ReleaseName              string
	BinaryKey                string
	BinaryName               string
	FileName                 string
	FileIndex                int
	ExpectedFileCount        int
	ExpectedArchiveFileCount int
	Poster                   string
	PostedAt                 *time.Time
	TotalParts               int
	ObservedParts            int
	TotalBytes               int64
	FirstArticleNumber       int64
	LastArticleNumber        int64
	MatchConfidence          float64
	MatchStatus              string
}

// grouped release candidate used by release formation.
type ReleaseCandidate struct {
	ProviderID                     int64
	NewsgroupID                    int64
	KeyKind                        string
	SourceReleaseKey               string
	ReleaseFamilyKey               string
	ReleaseKey                     string
	ReleaseName                    string
	PostedAt                       *time.Time
	BinaryCount                    int
	CompleteBinaryCount            int
	CompleteMainPayloadBinaryCount int
	ExpectedFileCount              int
	ExpectedArchiveFileCount       int
	ExpectedFileCoveragePct        float64
	ArchiveFileCoveragePct         float64
	ReadinessBucket                string
	TotalBytes                     int64
}

const (
	ReleaseCandidateKeyKindReleaseFamily    = "release_family"
	ReleaseCandidateKeyKindBaseStem         = "base_stem"
	ReleaseCandidateKeyKindRecoveredFileSet = "recovered_file_set"
	binaryPartArticleLookupChunk            = 10000
	releaseFileBinaryIDDeleteChunk          = 10000
	releaseFileInsertBatchSize              = 4000
	releaseNewsgroupInsertBatchSize         = 10000
)

type ReleaseCandidateSelectionOptions struct {
	MinExpectedFileCoveragePct float64
}

type ReleaseCandidateAck struct {
	ProviderID  int64
	NewsgroupID int64
	KeyKind     string
	FamilyKey   string
}

func sortReleaseCandidateAcks(acks []ReleaseCandidateAck) {
	sort.Slice(acks, func(i, j int) bool {
		if acks[i].ProviderID != acks[j].ProviderID {
			return acks[i].ProviderID < acks[j].ProviderID
		}
		if acks[i].NewsgroupID != acks[j].NewsgroupID {
			return acks[i].NewsgroupID < acks[j].NewsgroupID
		}
		if acks[i].KeyKind != acks[j].KeyKind {
			return acks[i].KeyKind < acks[j].KeyKind
		}
		return acks[i].FamilyKey < acks[j].FamilyKey
	})
}

// release catalog upsert input.
type ReleaseRecord struct {
	ReleaseID                string
	GUID                     string
	ProviderID               int64
	SourceReleaseKey         string
	ReleaseFamilyKey         string
	ReleaseKey               string
	GroupName                string
	Title                    string
	SourceTitle              string
	DeobfuscatedTitle        string
	MatchedMediaTitle        string
	TitleSource              string
	TitleConfidence          float64
	SearchTitle              string
	CategoryID               int
	Category                 string
	Classification           string
	Poster                   string
	SizeBytes                int64
	PostedAt                 *time.Time
	FileCount                int
	ExpectedFileCount        int
	ExpectedArchiveFileCount int
	ParFileCount             int
	CompletionPct            float64
	MatchConfidence          float64
	IdentityStatus           string
	Passworded               bool
	PasswordedKnown          bool
	PasswordedUnknown        bool
	PasswordState            string
	PreferredPasswordID      int64
	Encrypted                bool
	HasPAR2                  bool
	HasNFO                   bool
	ArchiveCount             int
	VideoCount               int
	AudioCount               int
	SamplePresent            bool
	AvailabilityScore        float64
	AvailabilityTier         string
	MediaQualityScore        float64
	MediaQualityTier         string
	IdentityConfidenceScore  float64
	RuntimeSeconds           int
	PrimaryResolution        string
	PrimaryVideoCodec        string
	PrimaryAudioCodec        string
	SubtitleLanguages        []string
	MediaTags                []string
	MetadataUpdatedAt        *time.Time
}

// article mapping row per release file.
type ReleaseFileArticleRecord struct {
	ArticleHeaderID int64
	PartNumber      int
}

// release file upsert input.
type ReleaseFileRecord struct {
	BinaryID  int64
	FileName  string
	SizeBytes int64
	FileIndex int
	IsPars    bool
	Subject   string
	Poster    string
	PostedAt  *time.Time
	Articles  []ReleaseFileArticleRecord
}

func normalizeReleaseIdentity(in *ReleaseRecord) {
	if in == nil {
		return
	}
	in.GroupName = firstNonBlank(in.GroupName)
	in.ReleaseFamilyKey = firstNonBlank(in.ReleaseFamilyKey, in.SourceReleaseKey, in.ReleaseKey, in.GroupName)
	in.SourceReleaseKey = firstNonBlank(in.SourceReleaseKey, in.ReleaseFamilyKey, in.ReleaseKey, in.GroupName)
	// Keep legacy release_key as a compatibility mirror of release_family_key.
	in.ReleaseKey = firstNonBlank(in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey)
	if in.CategoryID <= 0 {
		in.CategoryID = newsnab.OtherMisc
	}
	if strings.TrimSpace(in.Category) == "" {
		in.Category = newsnab.DisplayName(in.CategoryID)
	}
}

// CHANGED: return release groups whose binaries are new or changed since last formation.
func (s *Store) ListReleaseCandidates(ctx context.Context, limit int, opts ReleaseCandidateSelectionOptions) ([]ReleaseCandidate, error) {
	if limit <= 0 {
		limit = 1000
	}
	_ = opts

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			c.provider_id,
			c.newsgroup_id,
			c.key_kind,
			c.source_release_key,
			c.family_key,
			c.release_key,
			c.release_name,
			c.earliest_posted_at AS posted_at,
			c.binary_count,
			c.complete_binary_count,
			c.complete_main_payload_binary_count,
			c.expected_file_count,
			c.expected_archive_file_count,
			c.expected_file_coverage_pct,
			c.archive_file_coverage_pct,
			c.ready_reason AS readiness_bucket,
			c.total_bytes
		FROM release_ready_candidates c
		LEFT JOIN release_ready_candidate_acks a
		  ON a.provider_id = c.provider_id
		 AND a.newsgroup_id = c.newsgroup_id
		 AND a.key_kind = c.key_kind
		 AND a.family_key = c.family_key
		WHERE c.updated_at > COALESCE(a.processed_at, TIMESTAMPTZ 'epoch')
		ORDER BY
			CASE
				WHEN c.expected_archive_file_count > 0 THEN c.archive_file_coverage_pct
				ELSE c.expected_file_coverage_pct
			END DESC,
			c.complete_main_payload_binary_count DESC,
			c.complete_binary_count DESC,
			CASE
				WHEN c.has_expected_file_count OR c.has_expected_archive_file_count THEN 1
				ELSE 0
			END DESC,
			CASE
				WHEN c.key_kind = 'recovered_file_set' THEN 0
				WHEN c.key_kind = 'base_stem' THEN 1
				ELSE 2
			END ASC,
			c.updated_at ASC,
			c.family_key ASC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list release candidates: %w", err)
	}
	defer rows.Close()

	out := make([]ReleaseCandidate, 0, limit)
	for rows.Next() {
		var item ReleaseCandidate
		var postedAt sql.NullTime

		if err := rows.Scan(
			&item.ProviderID,
			&item.NewsgroupID,
			&item.KeyKind,
			&item.SourceReleaseKey,
			&item.ReleaseFamilyKey,
			&item.ReleaseKey,
			&item.ReleaseName,
			&postedAt,
			&item.BinaryCount,
			&item.CompleteBinaryCount,
			&item.CompleteMainPayloadBinaryCount,
			&item.ExpectedFileCount,
			&item.ExpectedArchiveFileCount,
			&item.ExpectedFileCoveragePct,
			&item.ArchiveFileCoveragePct,
			&item.ReadinessBucket,
			&item.TotalBytes,
		); err != nil {
			return nil, fmt.Errorf("scan release candidate: %w", err)
		}

		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release candidates: %w", err)
	}

	return out, nil
}

func (s *Store) ListExistingReleaseCandidates(ctx context.Context, limit, offset int) ([]ReleaseCandidate, error) {
	if limit <= 0 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH candidate_binaries AS (
			SELECT
				bc.binary_id AS id,
				bc.provider_id,
				bc.newsgroup_id,
				bic.source_release_key,
				bic.release_key,
				bic.release_name,
				bic.release_family_key,
				bic.base_stem,
				bic.expected_file_count,
				bic.expected_archive_file_count,
				bic.is_main_payload,
				bic.is_auxiliary,
				bos.posted_at,
				bos.total_parts,
				bos.observed_parts,
				bos.total_bytes,
				CASE
					WHEN NULLIF(BTRIM(bic.base_stem), '') IS NOT NULL
					 AND GREATEST(bic.expected_file_count, bic.expected_archive_file_count) > 1
					 AND COUNT(*) OVER (
						PARTITION BY bic.provider_id, bic.newsgroup_id, LOWER(BTRIM(bic.base_stem)), GREATEST(bic.expected_file_count, bic.expected_archive_file_count)
					 ) > 1
					THEN LOWER(BTRIM(bic.base_stem))
					ELSE bic.release_family_key
				END AS effective_release_family_key
			FROM binary_core bc
			JOIN binary_identity_current bic ON bic.binary_id = bc.binary_id
			JOIN binary_observation_stats bos ON bos.binary_id = bc.binary_id
		)
		SELECT
			b.provider_id,
			MIN(b.newsgroup_id)::BIGINT AS newsgroup_id,
			'release_family' AS key_kind,
			MAX(b.source_release_key) AS source_release_key,
			b.effective_release_family_key,
			MAX(b.release_key) AS release_key,
			MAX(b.release_name) AS release_name,
			MIN(b.posted_at) AS posted_at,
			COUNT(*)::INTEGER AS binary_count,
			COUNT(*) FILTER (
				WHERE b.total_parts > 0 AND b.observed_parts = b.total_parts
			)::INTEGER AS complete_binary_count,
			CASE
				WHEN COUNT(*) FILTER (
					WHERE b.total_parts > 0 AND b.observed_parts = b.total_parts
				) > 0 THEN 'actionable'
				WHEN COUNT(*) > 0 THEN 'fragment_only'
				ELSE 'stale_cleanup_only'
			END AS readiness_bucket,
			COALESCE(SUM(b.total_bytes), 0)::BIGINT AS total_bytes
		FROM releases r
		JOIN candidate_binaries b
		  ON b.provider_id = r.provider_id
		 AND b.effective_release_family_key = r.release_family_key
		GROUP BY b.provider_id, b.effective_release_family_key
		HAVING COUNT(*) FILTER (WHERE b.is_main_payload OR NOT b.is_auxiliary) >= 2
		    OR COALESCE(MAX(GREATEST(b.expected_file_count, b.expected_archive_file_count)), 0) <= 1
		ORDER BY MIN(b.posted_at) NULLS LAST, b.effective_release_family_key
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list existing release candidates: %w", err)
	}
	defer rows.Close()

	out := make([]ReleaseCandidate, 0, limit)
	for rows.Next() {
		var item ReleaseCandidate
		var postedAt sql.NullTime

		if err := rows.Scan(
			&item.ProviderID,
			&item.NewsgroupID,
			&item.KeyKind,
			&item.SourceReleaseKey,
			&item.ReleaseFamilyKey,
			&item.ReleaseKey,
			&item.ReleaseName,
			&postedAt,
			&item.BinaryCount,
			&item.CompleteBinaryCount,
			&item.ReadinessBucket,
			&item.TotalBytes,
		); err != nil {
			return nil, fmt.Errorf("scan existing release candidate: %w", err)
		}

		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing release candidates: %w", err)
	}

	return out, nil
}

// CHANGED: fetch binaries that belong to one release candidate.
func (s *Store) ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, keyKind, releaseFamilyKey string) ([]BinarySummary, error) {
	if providerID <= 0 {
		return nil, fmt.Errorf("provider id is required")
	}
	keyKind = strings.TrimSpace(keyKind)
	releaseFamilyKey = strings.TrimSpace(releaseFamilyKey)
	if releaseFamilyKey == "" {
		return nil, fmt.Errorf("release family key is required")
	}

	candidateSelector := `
			SELECT bic.binary_id
			FROM binary_identity_current bic
			WHERE bic.provider_id = $1
			  AND bic.release_family_key = $2`
	if newsgroupID > 0 {
		candidateSelector += `
			  AND bic.newsgroup_id = $3`
	}
	switch keyKind {
	case ReleaseCandidateKeyKindBaseStem:
		candidateSelector = `
			SELECT bic.binary_id
			FROM binary_identity_current bic
			WHERE bic.provider_id = $1
			  AND GREATEST(bic.expected_file_count, bic.expected_archive_file_count) > 1
			  AND BTRIM(bic.base_stem) <> ''
			  AND LOWER(BTRIM(bic.base_stem)) = $2`
		if newsgroupID > 0 {
			candidateSelector += `
			  AND bic.newsgroup_id = $3`
		}
	case ReleaseCandidateKeyKindReleaseFamily:
	case ReleaseCandidateKeyKindRecoveredFileSet:
		candidateSelector = `
			SELECT bic.binary_id
			FROM binary_identity_current bic
			WHERE bic.provider_id = $1
			  AND BTRIM(bic.file_set_key) <> ''
			  AND bic.file_set_key = $2`
	default:
		candidateSelector += `
			UNION
			SELECT bic.binary_id
			FROM binary_identity_current bic
			WHERE bic.provider_id = $1
			  AND GREATEST(bic.expected_file_count, bic.expected_archive_file_count) > 1
			  AND BTRIM(bic.base_stem) <> ''
			  AND LOWER(BTRIM(bic.base_stem)) = $2`
		if newsgroupID > 0 {
			candidateSelector += `
			  AND bic.newsgroup_id = $3`
		}
	}

	query := `
		WITH candidate_binaries AS (
` + candidateSelector + `
		)
		SELECT
			bc.binary_id,
			bc.provider_id,
			bc.newsgroup_id,
			bic.source_release_key,
			CASE
				WHEN NULLIF(BTRIM(bic.base_stem), '') IS NOT NULL
				 AND GREATEST(bic.expected_file_count, bic.expected_archive_file_count) > 1
				 AND LOWER(BTRIM(bic.base_stem)) = $2
				THEN LOWER(BTRIM(bic.base_stem))
				ELSE bic.release_family_key
			END AS effective_release_family_key,
			bic.file_family_key,
			bic.family_kind,
			bic.base_stem,
			bic.is_auxiliary,
			bic.is_main_payload,
			bic.release_key,
			bic.release_name,
			bc.binary_key,
			bic.binary_name,
			bic.file_name,
			bic.file_index,
			bic.expected_file_count,
			bic.expected_archive_file_count,
			COALESCE(p.poster_name, ''),
			bos.posted_at,
			bos.total_parts,
			bos.observed_parts,
			bos.total_bytes,
			bos.first_article_number,
			bos.last_article_number,
			bic.match_confidence,
			bic.match_status
		FROM candidate_binaries cb
		JOIN binary_core bc ON bc.binary_id = cb.binary_id
		JOIN binary_identity_current bic ON bic.binary_id = cb.binary_id
		JOIN binary_observation_stats bos ON bos.binary_id = cb.binary_id
		LEFT JOIN posters p ON p.id = bc.poster_id`
	args := []any{providerID, releaseFamilyKey}
	if newsgroupID > 0 {
		args = append(args, newsgroupID)
	}
	query += `
		ORDER BY bic.file_index, bic.file_name, bos.first_article_number, bc.binary_id`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list binaries for release candidate: %w", err)
	}
	defer rows.Close()

	out := make([]BinarySummary, 0, 32)
	for rows.Next() {
		var item BinarySummary
		var postedAt sql.NullTime

		if err := rows.Scan(
			&item.BinaryID,
			&item.ProviderID,
			&item.NewsgroupID,
			&item.SourceReleaseKey,
			&item.ReleaseFamilyKey,
			&item.FileFamilyKey,
			&item.FamilyKind,
			&item.BaseStem,
			&item.IsAuxiliary,
			&item.IsMainPayload,
			&item.ReleaseKey,
			&item.ReleaseName,
			&item.BinaryKey,
			&item.BinaryName,
			&item.FileName,
			&item.FileIndex,
			&item.ExpectedFileCount,
			&item.ExpectedArchiveFileCount,
			&item.Poster,
			&postedAt,
			&item.TotalParts,
			&item.ObservedParts,
			&item.TotalBytes,
			&item.FirstArticleNumber,
			&item.LastArticleNumber,
			&item.MatchConfidence,
			&item.MatchStatus,
		); err != nil {
			return nil, fmt.Errorf("scan binary summary: %w", err)
		}

		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate binary summaries: %w", err)
	}

	return out, nil
}

func (s *Store) AckReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, keyKind, familyKey string) error {
	return s.AckReleaseCandidates(ctx, []ReleaseCandidateAck{{
		ProviderID:  providerID,
		NewsgroupID: newsgroupID,
		KeyKind:     keyKind,
		FamilyKey:   familyKey,
	}})
}

func (s *Store) AckReleaseCandidates(ctx context.Context, candidates []ReleaseCandidateAck) error {
	if len(candidates) == 0 {
		return nil
	}

	unique := make([]ReleaseCandidateAck, 0, len(candidates))
	seen := make(map[ReleaseCandidateAck]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.ProviderID <= 0 {
			return fmt.Errorf("provider id is required")
		}
		if candidate.NewsgroupID <= 0 && candidate.KeyKind != ReleaseCandidateKeyKindRecoveredFileSet {
			return fmt.Errorf("newsgroup id is required")
		}
		candidate.KeyKind = strings.TrimSpace(candidate.KeyKind)
		candidate.FamilyKey = strings.TrimSpace(candidate.FamilyKey)
		if candidate.KeyKind == "" || candidate.FamilyKey == "" {
			return fmt.Errorf("key kind and family key are required")
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	sortReleaseCandidateAcks(unique)

	const maxAckBatchRows = 1000
	for start := 0; start < len(unique); start += maxAckBatchRows {
		end := start + maxAckBatchRows
		if end > len(unique) {
			end = len(unique)
		}
		if err := ackReleaseCandidatesChunk(ctx, s.db, unique[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) PromoteBaseStemCandidatesForReleaseFamily(ctx context.Context, providerID, newsgroupID int64, releaseFamilyKey string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	if providerID <= 0 {
		return fmt.Errorf("provider id is required")
	}
	if newsgroupID <= 0 {
		return fmt.Errorf("newsgroup id is required")
	}
	releaseFamilyKey = strings.TrimSpace(releaseFamilyKey)
	if releaseFamilyKey == "" {
		return fmt.Errorf("release family key is required")
	}

	return retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin promote base-stem candidates tx: %w", err)
		}
		defer rollbackTx(tx)

		rows, err := tx.QueryContext(ctx, `
			SELECT DISTINCT LOWER(BTRIM(bic.base_stem)) AS family_key
			FROM binary_identity_current bic
			WHERE bic.provider_id = $1
			  AND bic.newsgroup_id = $2
			  AND bic.release_family_key = $3
			  AND bic.expected_file_count > 1
			  AND bic.file_index > 0
			  AND BTRIM(COALESCE(bic.base_stem, '')) <> ''
			  AND (bic.is_main_payload = TRUE OR bic.is_auxiliary = FALSE)`,
			providerID,
			newsgroupID,
			releaseFamilyKey,
		)
		if err != nil {
			return fmt.Errorf("list promote base-stem candidates for provider=%d group=%d family=%q: %w", providerID, newsgroupID, releaseFamilyKey, err)
		}
		keys := make([]releaseFamilySummaryKey, 0)
		for rows.Next() {
			var familyKey string
			if err := rows.Scan(&familyKey); err != nil {
				rows.Close()
				return fmt.Errorf("scan promote base-stem candidate family key: %w", err)
			}
			keys = append(keys, releaseFamilySummaryKey{
				ProviderID:  providerID,
				NewsgroupID: newsgroupID,
				KeyKind:     "base_stem",
				FamilyKey:   familyKey,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate promote base-stem candidate family keys: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close promote base-stem candidate family keys: %w", err)
		}
		if err := markReleaseFamiliesDirtyBatch(ctx, tx, keys); err != nil {
			return fmt.Errorf("enqueue promote base-stem candidates for provider=%d group=%d family=%q: %w", providerID, newsgroupID, releaseFamilyKey, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit promote base-stem candidates tx: %w", err)
		}
		return nil
	})
}

func ackReleaseCandidatesChunk(ctx context.Context, db *sql.DB, candidates []ReleaseCandidateAck) error {
	args := make([]any, 0, len(candidates)*4)
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		base := len(args)
		args = append(args, candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, candidate.FamilyKey)
		values = append(values, fmt.Sprintf("($%d::bigint,$%d::bigint,$%d::text,$%d::text)", base+1, base+2, base+3, base+4))
	}
	if err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO release_ready_candidate_acks (
				provider_id,
				newsgroup_id,
				key_kind,
				family_key,
				processed_at,
				updated_at
			)
			SELECT
				c.provider_id,
				c.newsgroup_id,
				c.key_kind,
				c.family_key,
				c.updated_at,
				NOW()
			FROM release_ready_candidates c
			JOIN (VALUES `+strings.Join(values, ",")+`) AS v(provider_id, newsgroup_id, key_kind, family_key)
			  ON c.provider_id = v.provider_id
			 AND c.newsgroup_id = v.newsgroup_id
			 AND c.key_kind = v.key_kind
			 AND c.family_key = v.family_key
			ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
			SET processed_at = GREATEST(release_ready_candidate_acks.processed_at, EXCLUDED.processed_at),
			    updated_at = NOW()`,
			args...,
		)
		return err
	}); err != nil {
		return fmt.Errorf("ack %d release ready candidates: %w", len(candidates), err)
	}
	return nil
}

// CHANGED: fetch article ids/part numbers for one binary so release file article refs can be derived on demand.
func (s *Store) ListBinaryPartArticles(ctx context.Context, binaryID int64) ([]ReleaseFileArticleRecord, error) {
	if binaryID <= 0 {
		return nil, fmt.Errorf("binary id is required")
	}

	articlesByBinaryID, err := s.ListBinaryPartArticlesBatch(ctx, []int64{binaryID})
	if err != nil {
		return nil, err
	}
	return articlesByBinaryID[binaryID], nil
}

// CHANGED: fetch article ids/part numbers for a set of binaries in one query.
func (s *Store) ListBinaryPartArticlesBatch(ctx context.Context, binaryIDs []int64) (map[int64][]ReleaseFileArticleRecord, error) {
	out := make(map[int64][]ReleaseFileArticleRecord, len(binaryIDs))
	if len(binaryIDs) == 0 {
		return out, nil
	}

	seen := make(map[int64]struct{}, len(binaryIDs))
	uniqueIDs := make([]int64, 0, len(binaryIDs))
	for _, binaryID := range binaryIDs {
		if binaryID <= 0 {
			continue
		}
		if _, ok := seen[binaryID]; ok {
			continue
		}
		seen[binaryID] = struct{}{}
		uniqueIDs = append(uniqueIDs, binaryID)
		out[binaryID] = nil
	}
	if len(uniqueIDs) == 0 {
		return out, nil
	}

	for start := 0; start < len(uniqueIDs); start += binaryPartArticleLookupChunk {
		end := start + binaryPartArticleLookupChunk
		if end > len(uniqueIDs) {
			end = len(uniqueIDs)
		}
		args := make([]any, 0, end-start)
		placeholders := make([]string, 0, end-start)
		for _, binaryID := range uniqueIDs[start:end] {
			args = append(args, binaryID)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		if len(args) > postgresBindParameterSoftLimit {
			return nil, fmt.Errorf("binary part article lookup chunk has %d bind parameters", len(args))
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT binary_id, article_header_id, part_number
			FROM binary_parts
			WHERE binary_id IN (`+strings.Join(placeholders, ",")+`)
			ORDER BY binary_id, part_number`, args...)
		if err != nil {
			return nil, fmt.Errorf("list binary part articles batch: %w", err)
		}

		for rows.Next() {
			var binaryID int64
			var item ReleaseFileArticleRecord
			if err := rows.Scan(&binaryID, &item.ArticleHeaderID, &item.PartNumber); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan binary part article: %w", err)
			}
			out[binaryID] = append(out[binaryID], item)
		}

		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate binary part articles: %w", err)
		}
		rows.Close()
	}

	return out, nil
}

// CHANGED: create/update a release row and keep its id stable.
func (s *Store) UpsertRelease(ctx context.Context, in ReleaseRecord) (string, error) {
	return upsertReleaseWithRunner(ctx, s.db, in)
}

func upsertReleaseWithRunner(ctx context.Context, runner sqlExecQueryRower, in ReleaseRecord) (string, error) {
	if in.ProviderID <= 0 {
		return "", fmt.Errorf("provider id is required")
	}
	normalizeReleaseIdentity(&in)
	if in.ReleaseFamilyKey == "" {
		return "", fmt.Errorf("release family key is required")
	}
	if in.GroupName == "" {
		return "", fmt.Errorf("group name is required")
	}
	if strings.TrimSpace(in.ReleaseID) == "" {
		in.ReleaseID = ksuid.New().String()
	}
	if strings.TrimSpace(in.GUID) == "" {
		in.GUID = StableReleaseGUID(in.ProviderID, in.GroupName)
	}

	var postedAt any
	if in.PostedAt != nil {
		postedAt = in.PostedAt.UTC()
	}
	var metadataUpdatedAt any
	if in.MetadataUpdatedAt != nil {
		metadataUpdatedAt = in.MetadataUpdatedAt.UTC()
	}
	var preferredPasswordID any
	if in.PreferredPasswordID > 0 {
		preferredPasswordID = in.PreferredPasswordID
	}
	subtitleJSON, err := json.Marshal(sanitizeStringSlice(in.SubtitleLanguages))
	if err != nil {
		return "", fmt.Errorf("marshal subtitle languages for %q: %w", in.GroupName, err)
	}
	mediaTagsJSON, err := json.Marshal(sanitizeStringSlice(in.MediaTags))
	if err != nil {
		return "", fmt.Errorf("marshal media tags for %q: %w", in.GroupName, err)
	}

	var releaseID string
	err = runner.QueryRowContext(ctx, `
		INSERT INTO releases (
			release_id,
			guid,
			provider_id,
			source_release_key,
			release_family_key,
			release_key,
			group_name,
			title,
			source_title,
			deobfuscated_title,
			matched_media_title,
			title_source,
			title_confidence,
			search_title,
			category_id,
			category,
			classification,
			poster,
			size_bytes,
			posted_at,
			file_count,
			expected_file_count,
			expected_archive_file_count,
			par_file_count,
			completion_pct,
			match_confidence,
			identity_status,
			passworded,
			passworded_known,
			passworded_unknown,
			password_state,
			preferred_password_id,
			encrypted,
			has_par2,
			has_nfo,
			archive_count,
			video_count,
			audio_count,
			sample_present,
			availability_score,
			availability_tier,
			media_quality_score,
			media_quality_tier,
			identity_confidence_score,
			runtime_seconds,
			primary_resolution,
			primary_video_codec,
			primary_audio_codec,
			subtitle_languages_json,
			media_tags_json,
			metadata_updated_at,
			source_kind,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,$44,$45,$46,$47,$48,$49,$50,$51,'usenet_index',NOW())
		ON CONFLICT (provider_id, group_name) DO UPDATE
		SET guid = EXCLUDED.guid,
		    source_release_key = EXCLUDED.source_release_key,
		    release_family_key = EXCLUDED.release_family_key,
		    release_key = EXCLUDED.release_key,
		    title = CASE
		    	WHEN releases.title_source <> ''
		    	 AND releases.title_source <> 'source'
		    	 AND (EXCLUDED.title_source = '' OR EXCLUDED.title_source = 'source')
		    	THEN releases.title
		    	ELSE EXCLUDED.title
		    END,
		    source_title = EXCLUDED.source_title,
		    deobfuscated_title = CASE
		    	WHEN releases.title_source <> ''
		    	 AND releases.title_source <> 'source'
		    	 AND (EXCLUDED.title_source = '' OR EXCLUDED.title_source = 'source')
		    	THEN releases.deobfuscated_title
		    	ELSE EXCLUDED.deobfuscated_title
		    END,
		    matched_media_title = CASE
		    	WHEN EXCLUDED.matched_media_title <> '' THEN EXCLUDED.matched_media_title
		    	ELSE releases.matched_media_title
		    END,
		    title_source = CASE
		    	WHEN releases.title_source <> ''
		    	 AND releases.title_source <> 'source'
		    	 AND (EXCLUDED.title_source = '' OR EXCLUDED.title_source = 'source')
		    	THEN releases.title_source
		    	ELSE EXCLUDED.title_source
		    END,
		    title_confidence = CASE
		    	WHEN releases.title_source <> ''
		    	 AND releases.title_source <> 'source'
		    	 AND (EXCLUDED.title_source = '' OR EXCLUDED.title_source = 'source')
		    	THEN releases.title_confidence
		    	ELSE EXCLUDED.title_confidence
		    END,
		    search_title = CASE
		    	WHEN releases.title_source <> ''
		    	 AND releases.title_source <> 'source'
		    	 AND (EXCLUDED.title_source = '' OR EXCLUDED.title_source = 'source')
		    	THEN releases.search_title
		    	ELSE EXCLUDED.search_title
		    END,
		    category_id = EXCLUDED.category_id,
		    category = EXCLUDED.category,
		    classification = EXCLUDED.classification,
		    poster = EXCLUDED.poster,
		    size_bytes = EXCLUDED.size_bytes,
		    posted_at = EXCLUDED.posted_at,
		    file_count = EXCLUDED.file_count,
		    expected_file_count = GREATEST(releases.expected_file_count, EXCLUDED.expected_file_count),
		    expected_archive_file_count = GREATEST(releases.expected_archive_file_count, EXCLUDED.expected_archive_file_count),
		    par_file_count = EXCLUDED.par_file_count,
		    completion_pct = EXCLUDED.completion_pct,
		    match_confidence = EXCLUDED.match_confidence,
		    identity_status = EXCLUDED.identity_status,
		    passworded = releases.passworded OR EXCLUDED.passworded,
		    passworded_known = releases.passworded_known OR EXCLUDED.passworded_known,
		    passworded_unknown = CASE
		    	WHEN releases.passworded_known OR EXCLUDED.passworded_known THEN FALSE
		    	ELSE releases.passworded_unknown OR EXCLUDED.passworded_unknown
		    END,
		    password_state = CASE
		    	WHEN releases.passworded_known OR EXCLUDED.passworded_known THEN 'passworded_known'
		    	WHEN releases.passworded_unknown OR EXCLUDED.passworded_unknown THEN 'passworded_unknown'
		    	WHEN releases.passworded OR EXCLUDED.passworded THEN 'passworded'
		    	WHEN EXCLUDED.password_state <> '' AND EXCLUDED.password_state <> 'unknown' THEN EXCLUDED.password_state
		    	ELSE releases.password_state
		    END,
		    preferred_password_id = EXCLUDED.preferred_password_id,
		    encrypted = releases.encrypted OR EXCLUDED.encrypted,
		    has_par2 = releases.has_par2 OR EXCLUDED.has_par2,
		    has_nfo = releases.has_nfo OR EXCLUDED.has_nfo,
		    archive_count = GREATEST(releases.archive_count, EXCLUDED.archive_count),
		    video_count = GREATEST(releases.video_count, EXCLUDED.video_count),
		    audio_count = GREATEST(releases.audio_count, EXCLUDED.audio_count),
		    sample_present = releases.sample_present OR EXCLUDED.sample_present,
		    availability_score = EXCLUDED.availability_score,
		    availability_tier = EXCLUDED.availability_tier,
		    media_quality_score = GREATEST(releases.media_quality_score, EXCLUDED.media_quality_score),
		    media_quality_tier = CASE
		    	WHEN EXCLUDED.media_quality_tier <> '' AND EXCLUDED.media_quality_tier <> 'unknown' THEN EXCLUDED.media_quality_tier
		    	ELSE releases.media_quality_tier
		    END,
		    identity_confidence_score = GREATEST(releases.identity_confidence_score, EXCLUDED.identity_confidence_score),
		    runtime_seconds = EXCLUDED.runtime_seconds,
		    primary_resolution = CASE
		    	WHEN EXCLUDED.primary_resolution <> '' THEN EXCLUDED.primary_resolution
		    	ELSE releases.primary_resolution
		    END,
		    primary_video_codec = CASE
		    	WHEN EXCLUDED.primary_video_codec <> '' THEN EXCLUDED.primary_video_codec
		    	ELSE releases.primary_video_codec
		    END,
		    primary_audio_codec = CASE
		    	WHEN EXCLUDED.primary_audio_codec <> '' THEN EXCLUDED.primary_audio_codec
		    	ELSE releases.primary_audio_codec
		    END,
		    subtitle_languages_json = CASE
		    	WHEN jsonb_array_length(EXCLUDED.subtitle_languages_json) > 0 THEN EXCLUDED.subtitle_languages_json
		    	ELSE releases.subtitle_languages_json
		    END,
		    media_tags_json = CASE
		    	WHEN jsonb_array_length(EXCLUDED.media_tags_json) > 0 THEN EXCLUDED.media_tags_json
		    	ELSE releases.media_tags_json
		    END,
		    metadata_updated_at = GREATEST(releases.metadata_updated_at, EXCLUDED.metadata_updated_at),
		    updated_at = NOW()
		RETURNING release_id`,
		in.ReleaseID,
		in.GUID,
		in.ProviderID,
		strings.TrimSpace(in.SourceReleaseKey),
		strings.TrimSpace(in.ReleaseFamilyKey),
		in.ReleaseKey,
		in.GroupName,
		strings.TrimSpace(in.Title),
		strings.TrimSpace(in.SourceTitle),
		strings.TrimSpace(in.DeobfuscatedTitle),
		strings.TrimSpace(in.MatchedMediaTitle),
		strings.TrimSpace(in.TitleSource),
		in.TitleConfidence,
		strings.TrimSpace(in.SearchTitle),
		in.CategoryID,
		strings.TrimSpace(in.Category),
		strings.TrimSpace(in.Classification),
		strings.TrimSpace(in.Poster),
		in.SizeBytes,
		postedAt,
		in.FileCount,
		in.ExpectedFileCount,
		in.ExpectedArchiveFileCount,
		in.ParFileCount,
		in.CompletionPct,
		in.MatchConfidence,
		strings.TrimSpace(in.IdentityStatus),
		in.Passworded,
		in.PasswordedKnown,
		in.PasswordedUnknown,
		strings.TrimSpace(in.PasswordState),
		preferredPasswordID,
		in.Encrypted,
		in.HasPAR2,
		in.HasNFO,
		in.ArchiveCount,
		in.VideoCount,
		in.AudioCount,
		in.SamplePresent,
		in.AvailabilityScore,
		strings.TrimSpace(in.AvailabilityTier),
		in.MediaQualityScore,
		strings.TrimSpace(in.MediaQualityTier),
		in.IdentityConfidenceScore,
		in.RuntimeSeconds,
		strings.TrimSpace(in.PrimaryResolution),
		strings.TrimSpace(in.PrimaryVideoCodec),
		strings.TrimSpace(in.PrimaryAudioCodec),
		subtitleJSON,
		mediaTagsJSON,
		metadataUpdatedAt,
	).Scan(&releaseID)
	if err != nil {
		return "", fmt.Errorf("upsert release %q: %w", in.GroupName, err)
	}
	if err := refreshReleaseCategoryRunner(ctx, runner, releaseID); err != nil {
		return "", err
	}

	return releaseID, nil
}

func (s *Store) DeleteStaleReleasesForSourceKey(ctx context.Context, providerID int64, keyKind, releaseFamilyKey string, keepGroupNames []string) error {
	if providerID <= 0 {
		return fmt.Errorf("provider id is required")
	}
	keyKind = strings.TrimSpace(keyKind)
	releaseFamilyKey = strings.TrimSpace(releaseFamilyKey)
	if releaseFamilyKey == "" {
		return fmt.Errorf("release family key is required")
	}

	keep := sanitizeStringSlice(keepGroupNames)
	if keyKind == ReleaseCandidateKeyKindRecoveredFileSet {
		return s.deleteStaleRecoveredFileSetReleases(ctx, providerID, releaseFamilyKey, keep)
	}
	if len(keep) == 0 {
		_, err := s.db.ExecContext(ctx, `
			DELETE FROM releases
			WHERE provider_id = $1
			  AND (
			  	release_family_key = $2
			  	OR source_release_key = $2
			  )`,
			providerID,
			releaseFamilyKey,
		)
		if err != nil {
			return fmt.Errorf("delete stale releases for provider=%d release_family_key=%q: %w", providerID, releaseFamilyKey, err)
		}
		return nil
	}

	args := make([]any, 0, len(keep)+2)
	args = append(args, providerID, releaseFamilyKey)

	placeholders := make([]string, 0, len(keep))
	for i, groupName := range keep {
		args = append(args, groupName)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+3))
	}

	query := `
		DELETE FROM releases
		WHERE provider_id = $1
		  AND (
		  	release_family_key = $2
		  	OR source_release_key = $2
		  )
		  AND group_name NOT IN (` + strings.Join(placeholders, ",") + `)`

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete stale releases for provider=%d release_family_key=%q keep=%v: %w", providerID, releaseFamilyKey, keep, err)
	}

	return nil
}

func (s *Store) DeleteAuxiliaryOnlySiblingReleases(ctx context.Context, providerID, newsgroupID int64, baseStem string, keepReleaseIDs []string) error {
	if providerID <= 0 {
		return fmt.Errorf("provider id is required")
	}
	if newsgroupID <= 0 {
		return fmt.Errorf("newsgroup id is required")
	}
	baseStem = strings.ToLower(strings.TrimSpace(baseStem))
	if baseStem == "" {
		return fmt.Errorf("base stem is required")
	}

	args := make([]any, 0, len(keepReleaseIDs)+3)
	args = append(args, providerID, newsgroupID, baseStem)
	query := `
		DELETE FROM releases r
		WHERE r.provider_id = $1
		  AND EXISTS (
		  	SELECT 1
		  	FROM release_files rf
			    JOIN binary_identity_current bic ON bic.binary_id = rf.binary_id
		  	WHERE rf.release_id = r.release_id
			      AND bic.provider_id = $1
			      AND bic.newsgroup_id = $2
			      AND LOWER(BTRIM(COALESCE(bic.base_stem, ''))) = $3
		  )
		  AND NOT EXISTS (
		  	SELECT 1
		  	FROM release_files rf
			    JOIN binary_identity_current bic ON bic.binary_id = rf.binary_id
		  	WHERE rf.release_id = r.release_id
			      AND (bic.is_main_payload = TRUE OR bic.is_auxiliary = FALSE)
		  )`
	if len(keepReleaseIDs) > 0 {
		placeholders := make([]string, 0, len(keepReleaseIDs))
		for i, releaseID := range keepReleaseIDs {
			args = append(args, strings.TrimSpace(releaseID))
			placeholders = append(placeholders, fmt.Sprintf("$%d", i+4))
		}
		query += `
		  AND r.release_id NOT IN (` + strings.Join(placeholders, ",") + `)`
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete auxiliary-only sibling releases for provider=%d group=%d base_stem=%q: %w", providerID, newsgroupID, baseStem, err)
	}
	return nil
}

func (s *Store) deleteStaleRecoveredFileSetReleases(ctx context.Context, providerID int64, fileSetKey string, keepGroupNames []string) error {
	if len(keepGroupNames) == 0 {
		_, err := s.db.ExecContext(ctx, `
			DELETE FROM releases r
			WHERE r.provider_id = $1
			  AND (
			  	r.release_family_key = $2
			  	OR EXISTS (
			  		SELECT 1
			  		FROM release_files rf
						    JOIN binary_identity_current bic ON bic.binary_id = rf.binary_id
			  		WHERE rf.release_id = r.release_id
						      AND bic.provider_id = $1
						      AND bic.file_set_key = $2
						      AND BTRIM(bic.file_set_key) <> ''
			  	)
			  )`,
			providerID,
			fileSetKey,
		)
		if err != nil {
			return fmt.Errorf("delete stale recovered-file-set releases for provider=%d file_set_key=%q: %w", providerID, fileSetKey, err)
		}
		return nil
	}

	args := make([]any, 0, len(keepGroupNames)+2)
	args = append(args, providerID, fileSetKey)
	placeholders := make([]string, 0, len(keepGroupNames))
	for i, groupName := range keepGroupNames {
		args = append(args, groupName)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+3))
	}

	_, err := s.db.ExecContext(ctx, `
		DELETE FROM releases r
		WHERE r.provider_id = $1
		  AND (
		  	r.release_family_key = $2
		  	OR EXISTS (
		  		SELECT 1
		  		FROM release_files rf
				    JOIN binary_identity_current bic ON bic.binary_id = rf.binary_id
		  		WHERE rf.release_id = r.release_id
				      AND bic.provider_id = $1
				      AND bic.file_set_key = $2
				      AND BTRIM(bic.file_set_key) <> ''
		  	)
		  )
		  AND r.group_name NOT IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("delete stale recovered-file-set releases for provider=%d file_set_key=%q keep=%v: %w", providerID, fileSetKey, keepGroupNames, err)
	}
	return nil
}

// CHANGED: replace release_files atomically for one release.
func (s *Store) ReplaceReleaseFiles(ctx context.Context, releaseID string, files []ReleaseFileRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := replaceReleaseFilesInRunner(ctx, tx, releaseID, files); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func replaceReleaseFilesInRunner(ctx context.Context, runner sqlExecQueryer, releaseID string, files []ReleaseFileRecord) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	if _, err := runner.ExecContext(ctx, `
		DELETE FROM release_files
		WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("delete release_files for %s: %w", releaseID, err)
	}

	binaryIDs := make([]int64, 0, len(files))
	seenBinaryIDs := make(map[int64]struct{}, len(files))
	for _, f := range files {
		if f.BinaryID <= 0 {
			continue
		}
		if _, ok := seenBinaryIDs[f.BinaryID]; ok {
			continue
		}
		seenBinaryIDs[f.BinaryID] = struct{}{}
		binaryIDs = append(binaryIDs, f.BinaryID)
	}
	for start := 0; start < len(binaryIDs); start += releaseFileBinaryIDDeleteChunk {
		end := start + releaseFileBinaryIDDeleteChunk
		if end > len(binaryIDs) {
			end = len(binaryIDs)
		}
		args := make([]any, 0, end-start+1)
		args = append(args, releaseID)
		placeholders := make([]string, 0, end-start)
		for _, binaryID := range binaryIDs[start:end] {
			args = append(args, binaryID)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		if len(args) > postgresBindParameterSoftLimit {
			return fmt.Errorf("release file stale-delete chunk has %d bind parameters", len(args))
		}
		if _, err := runner.ExecContext(ctx, `
			DELETE FROM release_files
			WHERE release_id <> $1
			  AND binary_id IN (`+strings.Join(placeholders, ",")+`)`, args...); err != nil {
			return fmt.Errorf("delete stale cross-release files for %s: %w", releaseID, err)
		}
	}

	for start := 0; start < len(files); start += releaseFileInsertBatchSize {
		end := start + releaseFileInsertBatchSize
		if end > len(files) {
			end = len(files)
		}
		batch := files[start:end]
		args := make([]any, 0, len(batch)*10)
		placeholders := make([]string, 0, len(batch))
		now := time.Now().UTC()
		for _, f := range batch {
			var postedAt any
			if f.PostedAt != nil {
				postedAt = f.PostedAt.UTC()
			}
			base := len(args)
			args = append(args,
				releaseID,
				nullIfZero(f.BinaryID),
				strings.TrimSpace(f.FileName),
				f.SizeBytes,
				f.FileIndex,
				f.IsPars,
				strings.TrimSpace(f.Subject),
				strings.TrimSpace(f.Poster),
				postedAt,
				now,
			)
			placeholders = append(placeholders, fmt.Sprintf(
				"($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
				base+1, base+2, base+3, base+4, base+5,
				base+6, base+7, base+8, base+9, base+10,
			))
		}

		if len(args) > postgresBindParameterSoftLimit {
			return fmt.Errorf("release file insert chunk has %d bind parameters", len(args))
		}
		if _, err := runner.ExecContext(ctx, `
			INSERT INTO release_files (
				release_id,
				binary_id,
				file_name,
				size_bytes,
				file_index,
				is_pars,
				subject,
				poster,
				posted_at,
				updated_at
			)
			VALUES `+strings.Join(placeholders, ","),
			args...,
		); err != nil {
			return fmt.Errorf("insert release files for %s: %w", releaseID, err)
		}
	}

	tx, ok := runner.(*sql.Tx)
	if !ok {
		return fmt.Errorf("replace release files for %s requires transactional runner", releaseID)
	}
	if err := syncReleaseCatalogFiles(ctx, tx, releaseID); err != nil {
		return err
	}

	return nil
}

// CHANGED: replace release_newsgroups atomically for one release.
func (s *Store) ReplaceReleaseNewsgroups(ctx context.Context, releaseID string, newsgroupIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := replaceReleaseNewsgroupsInRunner(ctx, tx, releaseID, newsgroupIDs); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func replaceReleaseNewsgroupsInRunner(ctx context.Context, runner sqlExecQueryer, releaseID string, newsgroupIDs []int64) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	if _, err := runner.ExecContext(ctx, `
		DELETE FROM release_newsgroups
		WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("delete release_newsgroups for %s: %w", releaseID, err)
	}

	seen := make(map[int64]struct{}, len(newsgroupIDs))
	uniqueIDs := make([]int64, 0, len(newsgroupIDs))
	for _, newsgroupID := range newsgroupIDs {
		if newsgroupID <= 0 {
			continue
		}
		if _, ok := seen[newsgroupID]; ok {
			continue
		}
		seen[newsgroupID] = struct{}{}
		uniqueIDs = append(uniqueIDs, newsgroupID)
	}

	for start := 0; start < len(uniqueIDs); start += releaseNewsgroupInsertBatchSize {
		end := start + releaseNewsgroupInsertBatchSize
		if end > len(uniqueIDs) {
			end = len(uniqueIDs)
		}
		args := make([]any, 0, (end-start)*2)
		placeholders := make([]string, 0, end-start)
		for _, newsgroupID := range uniqueIDs[start:end] {
			base := len(args)
			args = append(args, releaseID, newsgroupID)
			placeholders = append(placeholders, fmt.Sprintf("($%d,$%d)", base+1, base+2))
		}
		if len(args) > postgresBindParameterSoftLimit {
			return fmt.Errorf("release newsgroup insert chunk has %d bind parameters", len(args))
		}
		if _, err := runner.ExecContext(ctx, `
			INSERT INTO release_newsgroups (release_id, newsgroup_id)
			VALUES `+strings.Join(placeholders, ","),
			args...,
		); err != nil {
			return fmt.Errorf("insert release_newsgroups for %s: %w", releaseID, err)
		}
	}

	return nil
}

func (s *Store) PersistReleaseSnapshot(ctx context.Context, in ReleaseRecord, files []ReleaseFileRecord, newsgroupIDs []int64) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	releaseID, err := upsertReleaseWithRunner(ctx, tx, in)
	if err != nil {
		return "", err
	}
	if err := replaceReleaseFilesInRunner(ctx, tx, releaseID, files); err != nil {
		return "", err
	}
	if err := replaceReleaseNewsgroupsInRunner(ctx, tx, releaseID, newsgroupIDs); err != nil {
		return "", err
	}
	if err := upsertNZBCacheWithRunner(ctx, tx, releaseID, "pending", "", ""); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return releaseID, nil
}
