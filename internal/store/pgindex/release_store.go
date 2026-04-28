package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/categories/newsnab"
	"github.com/segmentio/ksuid"
)

// binary summary returned to release formation.
type BinarySummary struct {
	BinaryID           int64
	ProviderID         int64
	NewsgroupID        int64
	SourceReleaseKey   string
	ReleaseFamilyKey   string
	FileFamilyKey      string
	FamilyKind         string
	BaseStem           string
	IsAuxiliary        bool
	IsMainPayload      bool
	ReleaseKey         string
	ReleaseName        string
	BinaryKey          string
	BinaryName         string
	FileName           string
	FileIndex          int
	ExpectedFileCount  int
	Poster             string
	PostedAt           *time.Time
	TotalParts         int
	ObservedParts      int
	TotalBytes         int64
	FirstArticleNumber int64
	LastArticleNumber  int64
	MatchConfidence    float64
	MatchStatus        string
}

// grouped release candidate used by release formation.
type ReleaseCandidate struct {
	ProviderID       int64
	NewsgroupID      int64
	KeyKind          string
	SourceReleaseKey string
	ReleaseFamilyKey string
	ReleaseKey       string
	ReleaseName      string
	PostedAt         *time.Time
	BinaryCount      int
	TotalBytes       int64
}

// release catalog upsert input.
type ReleaseRecord struct {
	ReleaseID               string
	GUID                    string
	ProviderID              int64
	SourceReleaseKey        string
	ReleaseFamilyKey        string
	ReleaseKey              string
	GroupName               string
	Title                   string
	SourceTitle             string
	DeobfuscatedTitle       string
	MatchedMediaTitle       string
	TitleSource             string
	TitleConfidence         float64
	SearchTitle             string
	CategoryID              int
	Category                string
	Classification          string
	Poster                  string
	SizeBytes               int64
	PostedAt                *time.Time
	FileCount               int
	ExpectedFileCount       int
	ParFileCount            int
	CompletionPct           float64
	MatchConfidence         float64
	IdentityStatus          string
	Passworded              bool
	PasswordedKnown         bool
	PasswordedUnknown       bool
	PasswordState           string
	PreferredPasswordID     int64
	Encrypted               bool
	HasPAR2                 bool
	HasNFO                  bool
	ArchiveCount            int
	VideoCount              int
	AudioCount              int
	SamplePresent           bool
	AvailabilityScore       float64
	AvailabilityTier        string
	MediaQualityScore       float64
	MediaQualityTier        string
	IdentityConfidenceScore float64
	RuntimeSeconds          int
	PrimaryResolution       string
	PrimaryVideoCodec       string
	PrimaryAudioCodec       string
	SubtitleLanguages       []string
	MediaTags               []string
	MetadataUpdatedAt       *time.Time
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
func (s *Store) ListReleaseCandidates(ctx context.Context, limit int) ([]ReleaseCandidate, error) {
	if limit <= 0 {
		limit = 1000
	}

	query := fmt.Sprintf(`
		WITH limits AS (
			SELECT
				$1::integer AS total_limit,
				LEAST(GREATEST($1::integer * 100, $1::integer * 10), 20000) AS queue_window_limit
		),
		queue_window AS (
			SELECT
				provider_id,
				newsgroup_id,
				key_kind,
				family_key,
				updated_at
			FROM release_stage_dirty_families
			ORDER BY updated_at, family_key
			LIMIT (SELECT queue_window_limit FROM limits)
		),
		candidate_summaries AS (
			SELECT
				q.provider_id,
				q.newsgroup_id,
				q.key_kind,
				q.family_key,
				q.updated_at,
				COALESCE(s.source_release_key, '') AS source_release_key,
				COALESCE(s.release_key, '') AS release_key,
				COALESCE(s.release_name, '') AS release_name,
				s.earliest_posted_at AS posted_at,
				COALESCE(s.binary_count, 0) AS binary_count,
				COALESCE(s.total_bytes, 0)::BIGINT AS total_bytes,
				COALESCE(s.has_expected_file_count, FALSE) AS has_expected_file_count,
				COALESCE(s.complete_binary_count, 0) AS complete_binary_count,
				COALESCE(s.readiness_bucket, '%s') AS readiness_bucket
			FROM queue_window q
			LEFT JOIN release_family_readiness_summaries s
			  ON s.provider_id = q.provider_id
			 AND s.newsgroup_id = q.newsgroup_id
			 AND s.key_kind = q.key_kind
			 AND s.family_key = q.family_key
		),
		next_queue AS (
			SELECT
				provider_id,
				newsgroup_id,
				key_kind,
				family_key,
				updated_at,
				source_release_key,
				release_key,
				release_name,
				posted_at,
				binary_count,
				total_bytes,
				complete_binary_count,
				has_expected_file_count,
				readiness_bucket
			FROM candidate_summaries
			ORDER BY
				CASE
					WHEN readiness_bucket = '%s' THEN 0
					WHEN readiness_bucket = '%s' THEN 1
					ELSE 2
				END ASC,
				complete_binary_count DESC,
				CASE
					WHEN has_expected_file_count THEN 1
					ELSE 0
				END DESC,
				updated_at ASC,
				family_key ASC
			LIMIT (SELECT total_limit FROM limits)
		)
		SELECT
			q.provider_id,
			q.newsgroup_id,
			q.key_kind,
			q.source_release_key,
			q.family_key,
			q.release_key,
			q.release_name,
			q.posted_at,
			q.binary_count,
			q.total_bytes
		FROM next_queue q
		ORDER BY
			CASE
				WHEN q.readiness_bucket = '%s' THEN 0
				WHEN q.readiness_bucket = '%s' THEN 1
				ELSE 2
			END ASC,
			q.complete_binary_count DESC,
			CASE
				WHEN q.has_expected_file_count THEN 1
				ELSE 0
			END DESC,
			q.updated_at ASC,
			q.family_key ASC`,
		releaseReadinessStaleCleanupOnly,
		releaseReadinessActionable,
		releaseReadinessStaleCleanupOnly,
		releaseReadinessActionable,
		releaseReadinessStaleCleanupOnly,
	)
	rows, err := s.db.QueryContext(ctx, query, limit)
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
				b.*,
				CASE
					WHEN NULLIF(BTRIM(b.base_stem), '') IS NOT NULL
					 AND b.expected_file_count > 1
					 AND COUNT(*) OVER (
						PARTITION BY b.provider_id, b.newsgroup_id, LOWER(BTRIM(b.base_stem)), b.expected_file_count
					 ) > 1
					THEN LOWER(BTRIM(b.base_stem))
					ELSE b.release_family_key
				END AS effective_release_family_key
			FROM binaries b
		)
		SELECT
			b.provider_id,
			MIN(b.newsgroup_id)::BIGINT AS newsgroup_id,
			MAX(b.source_release_key) AS source_release_key,
			b.effective_release_family_key,
			MAX(b.release_key) AS release_key,
			MAX(b.release_name) AS release_name,
			MIN(b.posted_at) AS posted_at,
			COUNT(*)::INTEGER AS binary_count,
			COALESCE(SUM(b.total_bytes), 0)::BIGINT AS total_bytes
		FROM releases r
		JOIN candidate_binaries b
		  ON b.provider_id = r.provider_id
		 AND b.effective_release_family_key = r.release_family_key
		GROUP BY b.provider_id, b.effective_release_family_key
		HAVING COUNT(*) FILTER (WHERE b.is_main_payload OR NOT b.is_auxiliary) >= 2
		    OR COALESCE(MAX(b.expected_file_count), 0) <= 1
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
			&item.SourceReleaseKey,
			&item.ReleaseFamilyKey,
			&item.ReleaseKey,
			&item.ReleaseName,
			&postedAt,
			&item.BinaryCount,
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
func (s *Store) ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, releaseFamilyKey string) ([]BinarySummary, error) {
	if providerID <= 0 {
		return nil, fmt.Errorf("provider id is required")
	}
	releaseFamilyKey = strings.TrimSpace(releaseFamilyKey)
	if releaseFamilyKey == "" {
		return nil, fmt.Errorf("release family key is required")
	}

	query := `
		WITH candidate_binaries AS (
			SELECT b.*
			FROM binaries b
			WHERE b.provider_id = $1
			  AND b.release_family_key = $2
			UNION
			SELECT b.*
			FROM binaries b
			WHERE b.provider_id = $1
			  AND b.expected_file_count > 1
			  AND NULLIF(BTRIM(b.base_stem), '') IS NOT NULL
			  AND LOWER(BTRIM(b.base_stem)) = $2
		)
		SELECT
			b.id,
			b.provider_id,
			b.newsgroup_id,
			b.source_release_key,
			CASE
				WHEN NULLIF(BTRIM(b.base_stem), '') IS NOT NULL
				 AND b.expected_file_count > 1
				 AND LOWER(BTRIM(b.base_stem)) = $2
				THEN LOWER(BTRIM(b.base_stem))
				ELSE b.release_family_key
			END AS effective_release_family_key,
			b.file_family_key,
			b.family_kind,
			b.base_stem,
			b.is_auxiliary,
			b.is_main_payload,
			b.release_key,
			b.release_name,
			b.binary_key,
			b.binary_name,
			b.file_name,
			b.file_index,
			b.expected_file_count,
			COALESCE(p.poster_name, ''),
			b.posted_at,
			b.total_parts,
			b.observed_parts,
			b.total_bytes,
			b.first_article_number,
			b.last_article_number,
			b.match_confidence,
			b.match_status
		FROM candidate_binaries b
		LEFT JOIN posters p ON p.id = b.poster_id
		WHERE b.provider_id = $1`
	args := []any{providerID, releaseFamilyKey}
	if newsgroupID > 0 {
		query += `
		  AND b.newsgroup_id = $3`
		args = append(args, newsgroupID)
	}
	query += `
		ORDER BY b.file_index, b.file_name, b.first_article_number, b.id`

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
	if providerID <= 0 {
		return fmt.Errorf("provider id is required")
	}
	if newsgroupID <= 0 {
		return fmt.Errorf("newsgroup id is required")
	}
	keyKind = strings.TrimSpace(keyKind)
	familyKey = strings.TrimSpace(familyKey)
	if keyKind == "" || familyKey == "" {
		return fmt.Errorf("key kind and family key are required")
	}

	_, err := s.db.ExecContext(ctx, `
		DELETE FROM release_stage_dirty_families
		WHERE provider_id = $1
		  AND newsgroup_id = $2
		  AND key_kind = $3
		  AND family_key = $4`,
		providerID,
		newsgroupID,
		keyKind,
		familyKey,
	)
	if err != nil {
		return fmt.Errorf("ack release candidate provider=%d group=%d key_kind=%s family=%q: %w", providerID, newsgroupID, keyKind, familyKey, err)
	}
	return nil
}

// CHANGED: fetch article ids/part numbers for one binary to build release_file_articles.
func (s *Store) ListBinaryPartArticles(ctx context.Context, binaryID int64) ([]ReleaseFileArticleRecord, error) {
	if binaryID <= 0 {
		return nil, fmt.Errorf("binary id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT article_header_id, part_number
		FROM binary_parts
		WHERE binary_id = $1
		ORDER BY part_number`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list binary part articles %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]ReleaseFileArticleRecord, 0, 64)
	for rows.Next() {
		var item ReleaseFileArticleRecord
		if err := rows.Scan(&item.ArticleHeaderID, &item.PartNumber); err != nil {
			return nil, fmt.Errorf("scan binary part article: %w", err)
		}
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate binary part articles: %w", err)
	}

	return out, nil
}

// CHANGED: create/update a release row and keep its id stable.
func (s *Store) UpsertRelease(ctx context.Context, in ReleaseRecord) (string, error) {
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
	err = s.db.QueryRowContext(ctx, `
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
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,$44,$45,$46,$47,$48,$49,$50,'usenet_index',NOW())
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
	if err := s.refreshReleaseCategory(ctx, releaseID); err != nil {
		return "", err
	}

	return releaseID, nil
}

func (s *Store) DeleteStaleReleasesForSourceKey(ctx context.Context, providerID int64, releaseFamilyKey string, keepGroupNames []string) error {
	if providerID <= 0 {
		return fmt.Errorf("provider id is required")
	}
	releaseFamilyKey = strings.TrimSpace(releaseFamilyKey)
	if releaseFamilyKey == "" {
		return fmt.Errorf("release family key is required")
	}

	keep := sanitizeStringSlice(keepGroupNames)
	if len(keep) == 0 {
		_, err := s.db.ExecContext(ctx, `
			DELETE FROM releases
			WHERE provider_id = $1
			  AND release_family_key = $2`,
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
		  AND release_family_key = $2
		  AND group_name NOT IN (` + strings.Join(placeholders, ",") + `)`

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete stale releases for provider=%d release_family_key=%q keep=%v: %w", providerID, releaseFamilyKey, keep, err)
	}

	return nil
}

// CHANGED: replace release_files and release_file_articles atomically for one release.
func (s *Store) ReplaceReleaseFiles(ctx context.Context, releaseID string, files []ReleaseFileRecord) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM release_file_articles
		WHERE release_file_id IN (
			SELECT id FROM release_files WHERE release_id = $1
		)`, releaseID); err != nil {
		return fmt.Errorf("delete release_file_articles for %s: %w", releaseID, err)
	}

	if _, err := tx.ExecContext(ctx, `
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
	if len(binaryIDs) > 0 {
		args := make([]any, 0, len(binaryIDs)+1)
		args = append(args, releaseID)
		placeholders := make([]string, 0, len(binaryIDs))
		for idx, binaryID := range binaryIDs {
			placeholders = append(placeholders, fmt.Sprintf("$%d", idx+2))
			args = append(args, binaryID)
		}
		filter := strings.Join(placeholders, ",")
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM release_file_articles
			WHERE release_file_id IN (
				SELECT id
				FROM release_files
				WHERE release_id <> $1
				  AND binary_id IN (`+filter+`)
			)`, args...); err != nil {
			return fmt.Errorf("delete stale cross-release file articles for %s: %w", releaseID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM release_files
			WHERE release_id <> $1
			  AND binary_id IN (`+filter+`)`, args...); err != nil {
			return fmt.Errorf("delete stale cross-release files for %s: %w", releaseID, err)
		}
	}

	for _, f := range files {
		var postedAt any
		if f.PostedAt != nil {
			postedAt = f.PostedAt.UTC()
		}

		var releaseFileID int64
		if err := tx.QueryRowContext(ctx, `
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
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
			RETURNING id`,
			releaseID,
			nullIfZero(f.BinaryID),
			strings.TrimSpace(f.FileName),
			f.SizeBytes,
			f.FileIndex,
			f.IsPars,
			strings.TrimSpace(f.Subject),
			strings.TrimSpace(f.Poster),
			postedAt,
		).Scan(&releaseFileID); err != nil {
			return fmt.Errorf("insert release file %q for %s: %w", f.FileName, releaseID, err)
		}

		for _, article := range f.Articles {
			if article.ArticleHeaderID <= 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO release_file_articles (
					release_file_id,
					article_header_id,
					part_number
				)
				VALUES ($1,$2,$3)`,
				releaseFileID,
				article.ArticleHeaderID,
				article.PartNumber,
			); err != nil {
				return fmt.Errorf("insert release file article file=%d article=%d: %w", releaseFileID, article.ArticleHeaderID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

// CHANGED: replace release_newsgroups atomically for one release.
func (s *Store) ReplaceReleaseNewsgroups(ctx context.Context, releaseID string, newsgroupIDs []int64) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM release_newsgroups
		WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("delete release_newsgroups for %s: %w", releaseID, err)
	}

	for _, newsgroupID := range newsgroupIDs {
		if newsgroupID <= 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO release_newsgroups (release_id, newsgroup_id)
			VALUES ($1, $2)`,
			releaseID, newsgroupID,
		); err != nil {
			return fmt.Errorf("insert release_newsgroup release=%s group=%d: %w", releaseID, newsgroupID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
