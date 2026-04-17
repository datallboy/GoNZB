package pgindex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type ArticleHeader struct {
	ArticleNumber int64
	MessageID     string
	Subject       string
	Poster        string
	DateUTC       *time.Time
	Bytes         int64
	Lines         int
	Xref          string
	RawOverview   map[string]any
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type BinaryInspectionCandidate struct {
	StageName          string
	BinaryID           int64
	ReleaseID          string
	ReleaseTitle       string
	SourceTitle        string
	DeobfuscatedTitle  string
	GroupName          string
	FileName           string
	BinaryName         string
	ReleaseName        string
	Poster             string
	PostedAt           *time.Time
	TotalBytes         int64
	TotalParts         int
	MatchConfidence    float64
	SourceUpdatedAt    *time.Time
	CurrentStatus      string
	CurrentUpdatedAt   *time.Time
	CurrentSummaryJSON json.RawMessage
	ArchiveSummaryJSON json.RawMessage
}

type BinaryInspectionRecord struct {
	StageName         string
	BinaryID          int64
	ReleaseID         string
	Status            string
	ErrorText         string
	MaterializedBytes int64
	ToolProvenance    map[string]any
	Summary           map[string]any
	SourceUpdatedAt   *time.Time
}

type BinaryInspectionArtifactRecord struct {
	BinaryID     int64
	ReleaseID    string
	StageName    string
	ArtifactRole string
	ArtifactName string
	ArtifactPath string
	BytesTotal   int64
	MIMEType     string
	Signature    string
	SourceKind   string
	Metadata     map[string]any
}

type BinaryArchiveEntryRecord struct {
	BinaryID          int64
	ReleaseID         string
	EntryName         string
	IsDir             bool
	UncompressedBytes int64
	CompressedBytes   int64
	Encrypted         bool
	Comment           string
	MediaType         string
	Signature         string
	Metadata          map[string]any
}

type BinaryMediaStreamRecord struct {
	BinaryID           int64
	ReleaseID          string
	StreamIndex        int
	StreamType         string
	CodecName          string
	CodecLongName      string
	Profile            string
	Width              int
	Height             int
	Channels           int
	Language           string
	DurationSeconds    float64
	BitRate            int64
	DefaultDisposition bool
	ForcedDisposition  bool
	Metadata           map[string]any
}

type BinaryTextEvidenceRecord struct {
	BinaryID     int64
	ReleaseID    string
	StageName    string
	EvidenceKind string
	TextValue    string
	Tokens       []string
	Metadata     map[string]any
}

type BinaryPAR2SetRecord struct {
	BinaryID       int64
	ReleaseID      string
	SetName        string
	BaseName       string
	IsVolume       bool
	VolumeNumber   int
	RecoveryBlocks int
	SignatureOK    bool
	Metadata       map[string]any
}

type PasswordVerificationCandidate struct {
	ID                 int64
	ReleaseID          string
	BinaryID           int64
	ArtifactID         int64
	Title              string
	SourceTitle        string
	DeobfuscatedTitle  string
	PasswordValue      string
	SourceKind         string
	SourceRef          string
	Confidence         float64
	VerificationStatus string
	LastError          string
}

type ReleasePasswordCandidateRecord struct {
	ReleaseID          string
	BinaryID           int64
	ArtifactID         int64
	PasswordValue      string
	SourceKind         string
	SourceRef          string
	Confidence         float64
	VerificationStatus string
	LastVerifiedAt     *time.Time
	LastError          string
}

type ReleaseInspectionUpdate struct {
	ReleaseID           string
	Encrypted           *bool
	HasPAR2             *bool
	HasNFO              *bool
	Passworded          *bool
	PasswordedKnown     *bool
	PasswordedUnknown   *bool
	PasswordState       string
	PreferredPasswordID *int64
	ArchiveCount        *int
	VideoCount          *int
	AudioCount          *int
	RuntimeSeconds      *int
	SamplePresent       *bool
	PrimaryResolution   string
	PrimaryVideoCodec   string
	PrimaryAudioCodec   string
	SubtitleLanguages   []string
	MediaTags           []string
	MediaQualityScore   *float64
	MediaQualityTier    string
	MetadataUpdatedAt   *time.Time
}

type ReleaseTMDBMatchRecord struct {
	ReleaseID     string
	TMDBID        int64
	MediaType     string
	Title         string
	OriginalTitle string
	Year          int
	Confidence    float64
	Chosen        bool
	Payload       map[string]any
}

type ReleaseTVDBMatchRecord struct {
	ReleaseID     string
	TVDBID        int64
	MediaType     string
	Title         string
	OriginalTitle string
	Year          int
	Confidence    float64
	Chosen        bool
	Payload       map[string]any
}

type ReleasePredbMatchRecord struct {
	ReleaseID       string
	ExternalID      int64
	NormalizedTitle string
	Title           string
	Category        string
	Source          string
	Team            string
	Genre           string
	URL             string
	SizeKB          float64
	FileCount       int
	PostedAt        *time.Time
	Confidence      float64
	Chosen          bool
	Payload         map[string]any
}

type PredbEntryRecord struct {
	ExternalID      int64
	NormalizedTitle string
	Title           string
	Category        string
	Source          string
	Team            string
	Genre           string
	URL             string
	SizeKB          float64
	FileCount       int
	PostedAt        *time.Time
	Payload         map[string]any
}

type PredbEntrySummary struct {
	EntryID         int64
	ExternalID      int64
	NormalizedTitle string
	Title           string
	Category        string
	Source          string
	Team            string
	Genre           string
	URL             string
	SizeKB          float64
	FileCount       int
	PostedAt        *time.Time
	Payload         map[string]any
}

type PredbBackfillWindow struct {
	From *time.Time
	To   *time.Time
}

type PredbBackfillCheckpoint struct {
	Provider              string
	OffsetHint            int
	OldestPostedAt        *time.Time
	OldestNormalizedTitle string
}

type ReleaseEnrichmentUpdate struct {
	ReleaseID               string
	MatchedMediaTitle       string
	OriginalMediaTitle      string
	TMDBID                  int64
	TVDBID                  int64
	ExternalMediaType       string
	ExternalYear            int
	SeasonNumber            int
	EpisodeNumber           int
	SeasonEpisodeSource     string
	SeasonEpisodeConfidence float64
	IdentityStatus          string
	IdentityConfidenceScore float64
	MetadataUpdatedAt       *time.Time
}

type ReleasePredbUpdate struct {
	ReleaseID               string
	Title                   string
	DeobfuscatedTitle       string
	TitleSource             string
	TitleConfidence         float64
	IdentityStatus          string
	IdentityConfidenceScore float64
	MetadataUpdatedAt       *time.Time
}

type ReleaseTitleCandidate struct {
	BinaryID   int64
	Source     string
	Value      string
	Confidence float64
}

type ReleaseEnrichmentCandidate struct {
	ReleaseID               string
	Title                   string
	SourceTitle             string
	DeobfuscatedTitle       string
	MatchedMediaTitle       string
	TitleSource             string
	Classification          string
	IdentityStatus          string
	MatchConfidence         float64
	IdentityConfidenceScore float64
	TMDBID                  int64
	TVDBID                  int64
	ExternalMediaType       string
	ExternalYear            int
	SeasonNumber            int
	EpisodeNumber           int
	PostedAt                *time.Time
	RuntimeSeconds          int
	PrimaryResolution       string
	PrimaryVideoCodec       string
	PrimaryAudioCodec       string
}

// StableReleaseGUID returns a deterministic GUID for PG release rows.
// This keeps release identity stable across repeat formation passes.
func StableReleaseGUID(providerID int64, releaseKey string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", providerID, strings.TrimSpace(strings.ToLower(releaseKey)))))
	return hex.EncodeToString(sum[:])
}

// EnsureProvider creates/updates a provider row and returns its id.
func (s *Store) EnsureProvider(ctx context.Context, providerKey, displayName string) (int64, error) {
	providerKey = strings.TrimSpace(providerKey)
	displayName = strings.TrimSpace(displayName)

	if providerKey == "" {
		return 0, fmt.Errorf("provider key is required")
	}
	if displayName == "" {
		displayName = providerKey
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO usenet_providers (provider_key, display_name)
		VALUES ($1, $2)
		ON CONFLICT (provider_key) DO UPDATE
		SET display_name = EXCLUDED.display_name
		RETURNING id`,
		providerKey, displayName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure provider %q: %w", providerKey, err)
	}

	return id, nil
}

// EnsureNewsgroup creates/updates a newsgroup row and returns its id.
func (s *Store) EnsureNewsgroup(ctx context.Context, groupName string) (int64, error) {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		return 0, fmt.Errorf("newsgroup name is required")
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO newsgroups (group_name)
		VALUES ($1)
		ON CONFLICT (group_name) DO UPDATE
		SET group_name = EXCLUDED.group_name
		RETURNING id`,
		groupName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure newsgroup %q: %w", groupName, err)
	}

	return id, nil
}

// StartScrapeRun creates a running scrape_run row.
func (s *Store) StartScrapeRun(ctx context.Context, providerID int64) (int64, error) {
	if providerID <= 0 {
		return 0, fmt.Errorf("provider id is required")
	}

	var runID int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO scrape_runs (provider_id, status, started_at)
		VALUES ($1, 'running', NOW())
		RETURNING id`,
		providerID,
	).Scan(&runID)
	if err != nil {
		return 0, fmt.Errorf("start scrape run: %w", err)
	}

	return runID, nil
}

// FinishScrapeRun closes a scrape_run row.
func (s *Store) FinishScrapeRun(ctx context.Context, runID int64, status, errorText string) error {
	if runID <= 0 {
		return fmt.Errorf("run id is required")
	}

	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		status = "completed"
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE scrape_runs
		SET status = $2,
		    error_text = $3,
		    finished_at = NOW()
		WHERE id = $1`,
		runID, status, errorText,
	)
	if err != nil {
		return fmt.Errorf("finish scrape run %d: %w", runID, err)
	}

	return nil
}

// GetCheckpoint returns the last article number for provider+group.
// Returns 0 when no checkpoint exists.
func (s *Store) GetCheckpoint(ctx context.Context, providerID, newsgroupID int64) (int64, error) {
	if providerID <= 0 || newsgroupID <= 0 {
		return 0, fmt.Errorf("provider id and newsgroup id are required")
	}

	var last int64
	err := s.db.QueryRowContext(ctx, `
		SELECT last_article_number
		FROM scrape_checkpoints
		WHERE provider_id = $1 AND newsgroup_id = $2`,
		providerID, newsgroupID,
	).Scan(&last)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get checkpoint p=%d g=%d: %w", providerID, newsgroupID, err)
	}

	return last, nil
}

// UpsertCheckpoint stores/advances checkpoint for provider+group.
func (s *Store) UpsertCheckpoint(ctx context.Context, providerID, newsgroupID, lastArticleNumber int64) error {
	if providerID <= 0 || newsgroupID <= 0 {
		return fmt.Errorf("provider id and newsgroup id are required")
	}
	if lastArticleNumber < 0 {
		lastArticleNumber = 0
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO scrape_checkpoints (provider_id, newsgroup_id, last_article_number, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (provider_id, newsgroup_id) DO UPDATE
		SET last_article_number = GREATEST(scrape_checkpoints.last_article_number, EXCLUDED.last_article_number),
		    updated_at = NOW()`,
		providerID, newsgroupID, lastArticleNumber,
	)
	if err != nil {
		return fmt.Errorf("upsert checkpoint p=%d g=%d: %w", providerID, newsgroupID, err)
	}

	return nil
}

// explicit latest/forward checkpoint accessor for Milestone 8.5.
func (s *Store) GetLatestCheckpoint(ctx context.Context, providerID, newsgroupID int64) (int64, error) {
	return s.GetCheckpoint(ctx, providerID, newsgroupID)
}

// explicit latest/forward checkpoint upsert for Milestone 8.5.
func (s *Store) UpsertLatestCheckpoint(ctx context.Context, providerID, newsgroupID, lastArticleNumber int64) error {
	return s.UpsertCheckpoint(ctx, providerID, newsgroupID, lastArticleNumber)
}

// separate backward cursor for historical scrape mode.
// Returns 0 when no backfill checkpoint exists yet.
func (s *Store) GetBackfillCheckpoint(ctx context.Context, providerID, newsgroupID int64) (int64, error) {
	var backfillArticleNumber int64
	err := s.db.QueryRowContext(ctx, `
		SELECT backfill_article_number
		FROM scrape_checkpoints
		WHERE provider_id = $1 AND newsgroup_id = $2`,
		providerID, newsgroupID,
	).Scan(&backfillArticleNumber)

	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get backfill checkpoint p=%d g=%d: %w", providerID, newsgroupID, err)
	}

	return backfillArticleNumber, nil
}

// persist backward/historical scrape cursor independently of latest cursor.
func (s *Store) UpsertBackfillCheckpoint(ctx context.Context, providerID, newsgroupID, backfillArticleNumber int64) error {
	if providerID <= 0 {
		return fmt.Errorf("provider id is required")
	}
	if newsgroupID <= 0 {
		return fmt.Errorf("newsgroup id is required")
	}
	if backfillArticleNumber < 0 {
		backfillArticleNumber = 0
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO scrape_checkpoints (
			provider_id,
			newsgroup_id,
			last_article_number,
			backfill_article_number,
			updated_at
		)
		VALUES ($1, $2, 0, $3, NOW())
		ON CONFLICT (provider_id, newsgroup_id)
		DO UPDATE SET
			backfill_article_number = EXCLUDED.backfill_article_number,
			updated_at = NOW()`,
		providerID, newsgroupID, backfillArticleNumber,
	)
	if err != nil {
		return fmt.Errorf("upsert backfill checkpoint p=%d g=%d: %w", providerID, newsgroupID, err)
	}

	return nil
}

// InsertArticleHeaders inserts header rows with ingest constraints enforced by DB.
// Returns number of inserted rows (conflicts are ignored via DO NOTHING).
func (s *Store) InsertArticleHeaders(ctx context.Context, providerID, newsgroupID int64, headers []ArticleHeader) (int64, error) {
	if providerID <= 0 || newsgroupID <= 0 {
		return 0, fmt.Errorf("provider id and newsgroup id are required")
	}
	if len(headers) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO article_headers (
			provider_id,
			newsgroup_id,
			article_number,
			message_id,
			subject,
			poster,
			date_utc,
			bytes,
			lines,
			xref,
			raw_overview_json,
			scraped_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,NOW())
		ON CONFLICT DO NOTHING`)
	if err != nil {
		return 0, fmt.Errorf("prepare article_headers insert: %w", err)
	}
	defer stmt.Close()

	var inserted int64
	for _, h := range headers {
		msgID := sanitizeUTF8(h.MessageID)
		if h.ArticleNumber <= 0 || msgID == "" {
			continue
		}

		subject := sanitizeUTF8(h.Subject)
		poster := sanitizeUTF8(h.Poster)
		xref := sanitizeUTF8(h.Xref)

		raw := "{}"
		if len(h.RawOverview) > 0 {
			cleanRaw := sanitizeStringMap(h.RawOverview)
			b, marshalErr := json.Marshal(cleanRaw)
			if marshalErr != nil {
				return inserted, fmt.Errorf("marshal raw_overview for article %d: %w", h.ArticleNumber, marshalErr)
			}
			raw = string(bytes.ToValidUTF8(b, []byte{}))
		}

		var date any
		if h.DateUTC != nil {
			date = h.DateUTC.UTC()
		} else {
			date = nil
		}

		res, execErr := stmt.ExecContext(
			ctx,
			providerID,
			newsgroupID,
			h.ArticleNumber,
			msgID,
			subject,
			poster,
			date,
			h.Bytes,
			h.Lines,
			xref,
			raw,
		)
		if execErr != nil {
			return inserted, fmt.Errorf("insert article header %d: %w", h.ArticleNumber, execErr)
		}

		affected, affErr := res.RowsAffected()
		if affErr == nil {
			inserted += affected
		}
	}

	if err := tx.Commit(); err != nil {
		return inserted, err
	}

	return inserted, nil
}

// CHANGED: seed/update PG nzb cache metadata row for a release.
func (s *Store) UpsertNZBCache(ctx context.Context, releaseID, generationStatus, hashSHA256, lastError string) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}
	generationStatus = strings.TrimSpace(strings.ToLower(generationStatus))
	if generationStatus == "" {
		generationStatus = "pending"
	}

	var generatedAt any
	if generationStatus == "ready" {
		generatedAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO nzb_cache (
			release_id,
			generation_status,
			nzb_hash_sha256,
			generated_at,
			last_error,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,NOW())
		ON CONFLICT (release_id) DO UPDATE
		SET generation_status = EXCLUDED.generation_status,
		    nzb_hash_sha256 = EXCLUDED.nzb_hash_sha256,
		    generated_at = COALESCE(EXCLUDED.generated_at, nzb_cache.generated_at),
		    last_error = EXCLUDED.last_error,
		    updated_at = NOW()`,
		releaseID,
		generationStatus,
		strings.TrimSpace(hashSHA256),
		generatedAt,
		strings.TrimSpace(lastError),
	)
	if err != nil {
		return fmt.Errorf("upsert nzb_cache for %s: %w", releaseID, err)
	}

	return nil
}

func (s *Store) ListReleaseEnrichmentCandidates(ctx context.Context, stageName string, limit int) ([]ReleaseEnrichmentCandidate, error) {
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return nil, fmt.Errorf("stage name is required")
	}
	if limit <= 0 {
		limit = 100
	}

	where := "TRUE"
	switch stageName {
	case "enrich_predb", "enrich_predb_scene_name_recovery":
		where = `(
			title_source = 'source'
			OR deobfuscated_title = ''
		) AND (
			matched_media_title <> ''
			OR (
				title_source <> 'source'
				AND deobfuscated_title <> ''
			)
		)`
	case "enrich_predb_metadata_only_fallback":
		where = `(
			title_source = 'source'
			OR deobfuscated_title = ''
		) AND (
			external_media_type <> ''
			OR external_year > 0
			OR season_number > 0
			OR episode_number > 0
			OR runtime_seconds > 0
			OR primary_resolution <> ''
			OR primary_video_codec <> ''
			OR primary_audio_codec <> ''
		)`
	case "enrich_tmdb":
		where = `(
			classification IN ('video', 'video_archive')
			OR (
				classification = 'archive'
				AND (
					video_count > 0
					OR runtime_seconds > 0
					OR primary_resolution <> ''
					OR primary_video_codec <> ''
					OR matched_media_title <> ''
					OR title_source = 'archive_entry'
				)
			)
		) AND (
			(tmdb_id = 0 AND tvdb_id = 0)
			OR (
				(tvdb_id > 0 OR external_media_type = 'tv')
				AND season_number = 0
				AND episode_number = 0
			)
		)`
	default:
		return nil, fmt.Errorf("unsupported enrichment stage %q", stageName)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			release_id,
			title,
			source_title,
			deobfuscated_title,
			matched_media_title,
			title_source,
			classification,
			identity_status,
			match_confidence,
			identity_confidence_score,
			tmdb_id,
			tvdb_id,
			external_media_type,
			external_year,
			season_number,
			episode_number,
			posted_at,
			runtime_seconds,
			primary_resolution,
			primary_video_codec,
			primary_audio_codec
		FROM releases
		WHERE `+where+`
		ORDER BY updated_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list release enrichment candidates %s: %w", stageName, err)
	}
	defer rows.Close()

	out := make([]ReleaseEnrichmentCandidate, 0, limit)
	for rows.Next() {
		var item ReleaseEnrichmentCandidate
		var postedAt sql.NullTime
		if err := rows.Scan(
			&item.ReleaseID,
			&item.Title,
			&item.SourceTitle,
			&item.DeobfuscatedTitle,
			&item.MatchedMediaTitle,
			&item.TitleSource,
			&item.Classification,
			&item.IdentityStatus,
			&item.MatchConfidence,
			&item.IdentityConfidenceScore,
			&item.TMDBID,
			&item.TVDBID,
			&item.ExternalMediaType,
			&item.ExternalYear,
			&item.SeasonNumber,
			&item.EpisodeNumber,
			&postedAt,
			&item.RuntimeSeconds,
			&item.PrimaryResolution,
			&item.PrimaryVideoCodec,
			&item.PrimaryAudioCodec,
		); err != nil {
			return nil, fmt.Errorf("scan release enrichment candidate: %w", err)
		}
		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release enrichment candidates %s: %w", stageName, err)
	}

	return out, nil
}

func (s *Store) ReplaceReleaseTMDBMatches(ctx context.Context, releaseID string, rows []ReleaseTMDBMatchRecord) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tmdb match replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM release_tmdb_matches WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("delete tmdb matches for %s: %w", releaseID, err)
	}

	for _, row := range rows {
		payloadJSON, err := json.Marshal(jsonOrEmptyMap(row.Payload))
		if err != nil {
			return fmt.Errorf("marshal tmdb payload for %s/%d: %w", releaseID, row.TMDBID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO release_tmdb_matches (
				release_id,
				tmdb_id,
				media_type,
				title,
				original_title,
				year,
				confidence,
				chosen,
				payload_json,
				updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())`,
			releaseID,
			row.TMDBID,
			strings.TrimSpace(row.MediaType),
			strings.TrimSpace(row.Title),
			strings.TrimSpace(row.OriginalTitle),
			row.Year,
			row.Confidence,
			row.Chosen,
			string(payloadJSON),
		); err != nil {
			return fmt.Errorf("insert tmdb match for %s/%d: %w", releaseID, row.TMDBID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tmdb match replace tx: %w", err)
	}
	return nil
}

func (s *Store) ReplaceReleasePredbMatches(ctx context.Context, releaseID string, rows []ReleasePredbMatchRecord) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin predb match replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM release_predb_matches WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("delete predb matches for %s: %w", releaseID, err)
	}

	for _, row := range rows {
		normalized := strings.TrimSpace(row.NormalizedTitle)
		if normalized == "" {
			normalized = normalizePredbTitle(strings.TrimSpace(row.Title))
		}
		if normalized == "" || strings.TrimSpace(row.Title) == "" {
			continue
		}
		payloadJSON, err := json.Marshal(jsonOrEmptyMap(row.Payload))
		if err != nil {
			return fmt.Errorf("marshal predb payload for %s/%s: %w", releaseID, normalized, err)
		}
		var entryID int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO predb_entries (
				normalized_title,
				title,
				category,
				source,
				external_id,
				team,
				genre,
				url,
				size_kb,
				file_count,
				posted_at,
				payload_json,
				updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW())
			ON CONFLICT (normalized_title) DO UPDATE
			SET title = EXCLUDED.title,
			    category = EXCLUDED.category,
			    source = EXCLUDED.source,
			    external_id = CASE WHEN EXCLUDED.external_id > 0 THEN EXCLUDED.external_id ELSE predb_entries.external_id END,
			    team = CASE WHEN EXCLUDED.team <> '' THEN EXCLUDED.team ELSE predb_entries.team END,
			    genre = CASE WHEN EXCLUDED.genre <> '' THEN EXCLUDED.genre ELSE predb_entries.genre END,
			    url = CASE WHEN EXCLUDED.url <> '' THEN EXCLUDED.url ELSE predb_entries.url END,
			    size_kb = CASE WHEN EXCLUDED.size_kb > 0 THEN EXCLUDED.size_kb ELSE predb_entries.size_kb END,
			    file_count = CASE WHEN EXCLUDED.file_count > 0 THEN EXCLUDED.file_count ELSE predb_entries.file_count END,
			    posted_at = COALESCE(EXCLUDED.posted_at, predb_entries.posted_at),
			    payload_json = CASE
			    	WHEN EXCLUDED.payload_json <> '{}'::jsonb THEN EXCLUDED.payload_json
			    	ELSE predb_entries.payload_json
			    END,
			    updated_at = NOW()
			RETURNING id`,
			normalized,
			strings.TrimSpace(row.Title),
			strings.TrimSpace(row.Category),
			strings.TrimSpace(row.Source),
			row.ExternalID,
			strings.TrimSpace(row.Team),
			strings.TrimSpace(row.Genre),
			strings.TrimSpace(row.URL),
			row.SizeKB,
			row.FileCount,
			row.PostedAt,
			string(payloadJSON),
		).Scan(&entryID); err != nil {
			return fmt.Errorf("upsert predb entry for %s/%s: %w", releaseID, normalized, err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO release_predb_matches (
				release_id,
				predb_entry_id,
				confidence,
				chosen,
				updated_at
			)
			VALUES ($1,$2,$3,$4,NOW())`,
			releaseID,
			entryID,
			row.Confidence,
			row.Chosen,
		); err != nil {
			return fmt.Errorf("insert predb match for %s/%d: %w", releaseID, entryID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit predb match replace tx: %w", err)
	}
	return nil
}

func (s *Store) UpsertPredbEntries(ctx context.Context, rows []PredbEntryRecord) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin predb entry upsert tx: %w", err)
	}
	defer rollbackTx(tx)

	for _, row := range rows {
		normalized := strings.TrimSpace(row.NormalizedTitle)
		if normalized == "" {
			normalized = normalizePredbTitle(strings.TrimSpace(row.Title))
		}
		if normalized == "" || strings.TrimSpace(row.Title) == "" {
			continue
		}
		payloadJSON, err := json.Marshal(jsonOrEmptyMap(row.Payload))
		if err != nil {
			return fmt.Errorf("marshal predb entry payload for %s: %w", normalized, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO predb_entries (
				normalized_title,
				title,
				category,
				source,
				external_id,
				team,
				genre,
				url,
				size_kb,
				file_count,
				posted_at,
				payload_json,
				updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW())
			ON CONFLICT (normalized_title) DO UPDATE
			SET title = EXCLUDED.title,
			    category = CASE WHEN EXCLUDED.category <> '' THEN EXCLUDED.category ELSE predb_entries.category END,
			    source = CASE WHEN EXCLUDED.source <> '' THEN EXCLUDED.source ELSE predb_entries.source END,
			    external_id = CASE WHEN EXCLUDED.external_id > 0 THEN EXCLUDED.external_id ELSE predb_entries.external_id END,
			    team = CASE WHEN EXCLUDED.team <> '' THEN EXCLUDED.team ELSE predb_entries.team END,
			    genre = CASE WHEN EXCLUDED.genre <> '' THEN EXCLUDED.genre ELSE predb_entries.genre END,
			    url = CASE WHEN EXCLUDED.url <> '' THEN EXCLUDED.url ELSE predb_entries.url END,
			    size_kb = CASE WHEN EXCLUDED.size_kb > 0 THEN EXCLUDED.size_kb ELSE predb_entries.size_kb END,
			    file_count = CASE WHEN EXCLUDED.file_count > 0 THEN EXCLUDED.file_count ELSE predb_entries.file_count END,
			    posted_at = COALESCE(EXCLUDED.posted_at, predb_entries.posted_at),
			    payload_json = CASE
			    	WHEN EXCLUDED.payload_json <> '{}'::jsonb THEN EXCLUDED.payload_json
			    	ELSE predb_entries.payload_json
			    END,
			    updated_at = NOW()`,
			normalized,
			strings.TrimSpace(row.Title),
			strings.TrimSpace(row.Category),
			strings.TrimSpace(row.Source),
			row.ExternalID,
			strings.TrimSpace(row.Team),
			strings.TrimSpace(row.Genre),
			strings.TrimSpace(row.URL),
			row.SizeKB,
			row.FileCount,
			row.PostedAt,
			string(payloadJSON),
		); err != nil {
			return fmt.Errorf("upsert predb entry %s: %w", normalized, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit predb entry upsert tx: %w", err)
	}
	return nil
}

func (s *Store) GetPredbBackfillWindow(ctx context.Context) (*PredbBackfillWindow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT MIN(date_utc) FROM article_headers WHERE date_utc IS NOT NULL) AS article_min,
			(SELECT MAX(date_utc) FROM article_headers WHERE date_utc IS NOT NULL) AS article_max,
			(SELECT MIN(posted_at) FROM releases WHERE posted_at IS NOT NULL) AS release_min,
			(SELECT MAX(posted_at) FROM releases WHERE posted_at IS NOT NULL) AS release_max`)

	var articleMin sql.NullTime
	var articleMax sql.NullTime
	var releaseMin sql.NullTime
	var releaseMax sql.NullTime
	if err := row.Scan(&articleMin, &articleMax, &releaseMin, &releaseMax); err != nil {
		return nil, fmt.Errorf("get predb backfill window: %w", err)
	}

	var from *time.Time
	var to *time.Time
	applyMin := func(v sql.NullTime) {
		if !v.Valid {
			return
		}
		t := v.Time.UTC()
		if from == nil || t.Before(*from) {
			from = &t
		}
	}
	applyMax := func(v sql.NullTime) {
		if !v.Valid {
			return
		}
		t := v.Time.UTC()
		if to == nil || t.After(*to) {
			to = &t
		}
	}
	applyMin(articleMin)
	applyMin(releaseMin)
	applyMax(articleMax)
	applyMax(releaseMax)

	if from == nil && to == nil {
		return nil, nil
	}
	return &PredbBackfillWindow{
		From: from,
		To:   to,
	}, nil
}

func (s *Store) GetPredbEntryWindow(ctx context.Context) (*PredbBackfillWindow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			MIN(posted_at) AS oldest_posted_at,
			MAX(posted_at) AS newest_posted_at
		FROM predb_entries
		WHERE posted_at IS NOT NULL`)

	var oldest sql.NullTime
	var newest sql.NullTime
	if err := row.Scan(&oldest, &newest); err != nil {
		return nil, fmt.Errorf("get predb entry window: %w", err)
	}

	var from *time.Time
	var to *time.Time
	if oldest.Valid {
		t := oldest.Time.UTC()
		from = &t
	}
	if newest.Valid {
		t := newest.Time.UTC()
		to = &t
	}
	if from == nil && to == nil {
		return nil, nil
	}
	return &PredbBackfillWindow{
		From: from,
		To:   to,
	}, nil
}

func (s *Store) GetPredbBackfillCheckpoint(ctx context.Context, provider string) (*PredbBackfillCheckpoint, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil, fmt.Errorf("predb backfill provider is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT
			provider,
			offset_hint,
			oldest_posted_at,
			oldest_normalized_title
		FROM predb_backfill_checkpoints
		WHERE provider = $1`, provider)

	var item PredbBackfillCheckpoint
	var oldest sql.NullTime
	if err := row.Scan(&item.Provider, &item.OffsetHint, &oldest, &item.OldestNormalizedTitle); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get predb backfill checkpoint %s: %w", provider, err)
	}
	if oldest.Valid {
		t := oldest.Time.UTC()
		item.OldestPostedAt = &t
	}
	return &item, nil
}

func (s *Store) UpsertPredbBackfillCheckpoint(ctx context.Context, in PredbBackfillCheckpoint) error {
	in.Provider = strings.TrimSpace(in.Provider)
	if in.Provider == "" {
		return fmt.Errorf("predb backfill provider is required")
	}
	in.OldestNormalizedTitle = strings.TrimSpace(in.OldestNormalizedTitle)
	if in.OffsetHint < 0 {
		in.OffsetHint = 0
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO predb_backfill_checkpoints (
			provider,
			offset_hint,
			oldest_posted_at,
			oldest_normalized_title,
			updated_at
		)
		VALUES ($1,$2,$3,$4,NOW())
		ON CONFLICT (provider) DO UPDATE
		SET offset_hint = EXCLUDED.offset_hint,
		    oldest_posted_at = EXCLUDED.oldest_posted_at,
		    oldest_normalized_title = EXCLUDED.oldest_normalized_title,
		    updated_at = NOW()`,
		in.Provider,
		in.OffsetHint,
		in.OldestPostedAt,
		in.OldestNormalizedTitle,
	)
	if err != nil {
		return fmt.Errorf("upsert predb backfill checkpoint %s: %w", in.Provider, err)
	}
	return nil
}

func (s *Store) ListPredbEntriesForWindow(ctx context.Context, from, to *time.Time, categoryHint string, limit int) ([]PredbEntrySummary, error) {
	if limit <= 0 {
		limit = 200
	}
	categoryHint = strings.TrimSpace(categoryHint)
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			COALESCE(external_id, 0),
			normalized_title,
			title,
			category,
			source,
			team,
			genre,
			url,
			size_kb,
			file_count,
			posted_at,
			COALESCE(payload_json, '{}'::jsonb)
		FROM predb_entries
		WHERE ($1::timestamptz IS NULL OR posted_at >= $1)
		  AND ($2::timestamptz IS NULL OR posted_at <= $2)
		  AND ($3 = '' OR category ILIKE $3 || '%')
		ORDER BY posted_at DESC NULLS LAST, updated_at DESC
		LIMIT $4`,
		from,
		to,
		categoryHint,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list predb entries for window: %w", err)
	}
	defer rows.Close()

	out := make([]PredbEntrySummary, 0, limit)
	for rows.Next() {
		var item PredbEntrySummary
		var postedAt sql.NullTime
		var payloadJSON []byte
		if err := rows.Scan(
			&item.EntryID,
			&item.ExternalID,
			&item.NormalizedTitle,
			&item.Title,
			&item.Category,
			&item.Source,
			&item.Team,
			&item.Genre,
			&item.URL,
			&item.SizeKB,
			&item.FileCount,
			&postedAt,
			&payloadJSON,
		); err != nil {
			return nil, fmt.Errorf("scan predb entry summary: %w", err)
		}
		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}
		if len(payloadJSON) > 0 {
			_ = json.Unmarshal(payloadJSON, &item.Payload)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate predb entries for window: %w", err)
	}
	return out, nil
}

func (s *Store) ReplaceReleaseTVDBMatches(ctx context.Context, releaseID string, rows []ReleaseTVDBMatchRecord) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tvdb match replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM release_tvdb_matches WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("delete tvdb matches for %s: %w", releaseID, err)
	}

	for _, row := range rows {
		payloadJSON, err := json.Marshal(jsonOrEmptyMap(row.Payload))
		if err != nil {
			return fmt.Errorf("marshal tvdb payload for %s/%d: %w", releaseID, row.TVDBID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO release_tvdb_matches (
				release_id,
				tvdb_id,
				media_type,
				title,
				original_title,
				year,
				confidence,
				chosen,
				payload_json,
				updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())`,
			releaseID,
			row.TVDBID,
			strings.TrimSpace(row.MediaType),
			strings.TrimSpace(row.Title),
			strings.TrimSpace(row.OriginalTitle),
			row.Year,
			row.Confidence,
			row.Chosen,
			string(payloadJSON),
		); err != nil {
			return fmt.Errorf("insert tvdb match for %s/%d: %w", releaseID, row.TVDBID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tvdb match replace tx: %w", err)
	}
	return nil
}

func normalizePredbTitle(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "_", ".")
	v = strings.Join(strings.Fields(v), ".")
	return strings.Trim(v, ".")
}

func (s *Store) ApplyReleaseEnrichmentUpdate(ctx context.Context, in ReleaseEnrichmentUpdate) error {
	in.ReleaseID = strings.TrimSpace(in.ReleaseID)
	if in.ReleaseID == "" {
		return fmt.Errorf("release id is required")
	}

	var metadataUpdated any
	if in.MetadataUpdatedAt != nil {
		metadataUpdated = in.MetadataUpdatedAt.UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE releases
		SET matched_media_title = CASE
		    	WHEN $2 <> '' THEN $2
		    	ELSE matched_media_title
		    END,
		    original_media_title = CASE
		    	WHEN $3 <> '' THEN $3
		    	ELSE original_media_title
		    END,
		    tmdb_id = CASE
		    	WHEN $4 > 0 THEN $4
		    	ELSE tmdb_id
		    END,
		    tvdb_id = CASE
		    	WHEN $5 > 0 THEN $5
		    	ELSE tvdb_id
		    END,
		    external_media_type = CASE
		    	WHEN $6 <> '' THEN $6
		    	ELSE external_media_type
		    END,
		    external_year = CASE
		    	WHEN $7 > 0 THEN $7
		    	ELSE external_year
		    END,
		    season_number = CASE
		    	WHEN $8 > 0 THEN $8
		    	ELSE season_number
		    END,
		    episode_number = CASE
		    	WHEN $9 > 0 THEN $9
		    	ELSE episode_number
		    END,
		    season_episode_source = CASE
		    	WHEN $10 <> '' THEN $10
		    	ELSE season_episode_source
		    END,
		    season_episode_confidence = GREATEST(season_episode_confidence, $11),
		    identity_status = CASE
		    	WHEN $12 <> '' THEN $12
		    	ELSE identity_status
		    END,
		    identity_confidence_score = GREATEST(identity_confidence_score, $13),
		    metadata_updated_at = COALESCE($14, metadata_updated_at),
		    updated_at = NOW()
		WHERE release_id = $1`,
		in.ReleaseID,
		strings.TrimSpace(in.MatchedMediaTitle),
		strings.TrimSpace(in.OriginalMediaTitle),
		in.TMDBID,
		in.TVDBID,
		strings.TrimSpace(in.ExternalMediaType),
		in.ExternalYear,
		in.SeasonNumber,
		in.EpisodeNumber,
		strings.TrimSpace(in.SeasonEpisodeSource),
		in.SeasonEpisodeConfidence,
		strings.TrimSpace(in.IdentityStatus),
		in.IdentityConfidenceScore,
		metadataUpdated,
	)
	if err != nil {
		return fmt.Errorf("apply release enrichment update %s: %w", in.ReleaseID, err)
	}
	return nil
}

func (s *Store) ApplyReleasePredbUpdate(ctx context.Context, in ReleasePredbUpdate) error {
	releaseID := strings.TrimSpace(in.ReleaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	var metadataUpdated any
	if in.MetadataUpdatedAt != nil && !in.MetadataUpdatedAt.IsZero() {
		metadataUpdated = in.MetadataUpdatedAt.UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE releases
		SET title = CASE
		    	WHEN $2 <> '' AND (title_source = '' OR title_source = 'source') THEN $2
		    	ELSE title
		    END,
		    deobfuscated_title = CASE
		    	WHEN $3 <> '' THEN $3
		    	ELSE deobfuscated_title
		    END,
		    title_source = CASE
		    	WHEN $4 <> '' AND (title_source = '' OR title_source = 'source') THEN $4
		    	ELSE title_source
		    END,
		    title_confidence = CASE
		    	WHEN $5 > title_confidence AND (title_source = '' OR title_source = 'source') THEN $5
		    	ELSE title_confidence
		    END,
		    identity_status = CASE
		    	WHEN $6 <> '' AND identity_status <> 'identified' THEN $6
		    	ELSE identity_status
		    END,
		    identity_confidence_score = GREATEST(identity_confidence_score, $7),
		    search_title = CASE
		    	WHEN $2 <> '' AND (title_source = '' OR title_source = 'source') THEN LOWER($2)
		    	ELSE search_title
		    END,
		    metadata_updated_at = COALESCE($8, metadata_updated_at),
		    updated_at = NOW()
		WHERE release_id = $1`,
		releaseID,
		strings.TrimSpace(in.Title),
		strings.TrimSpace(in.DeobfuscatedTitle),
		strings.TrimSpace(in.TitleSource),
		in.TitleConfidence,
		strings.TrimSpace(in.IdentityStatus),
		in.IdentityConfidenceScore,
		metadataUpdated,
	)
	if err != nil {
		return fmt.Errorf("apply release predb update %s: %w", releaseID, err)
	}
	return nil
}

// PostgreSQL text/jsonb columns require valid UTF-8.
// Use byte-level repair so raw NNTP bytes cannot leak through.
func sanitizeUTF8(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if utf8.ValidString(s) {
		return s
	}
	return string(bytes.ToValidUTF8([]byte(s), []byte{}))
}

func sanitizeStringMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}

	out := make(map[string]any, len(in))
	for k, v := range in {
		cleanKey := sanitizeUTF8(k)

		switch tv := v.(type) {
		case string:
			out[cleanKey] = sanitizeUTF8(tv)
		case map[string]any:
			out[cleanKey] = sanitizeStringMap(tv)
		case []any:
			out[cleanKey] = sanitizeAnySlice(tv)
		default:
			out[cleanKey] = v
		}
	}

	return out
}

func sanitizeJSONMap(in map[string]any) map[string]any {
	return sanitizeStringMap(in)
}

func sanitizeAnySlice(in []any) []any {
	if len(in) == 0 {
		return []any{}
	}

	out := make([]any, 0, len(in))
	for _, v := range in {
		switch tv := v.(type) {
		case string:
			out = append(out, sanitizeUTF8(tv))
		case map[string]any:
			out = append(out, sanitizeStringMap(tv))
		case []any:
			out = append(out, sanitizeAnySlice(tv))
		default:
			out = append(out, v)
		}
	}

	return out
}

func jsonOrEmptyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	return sanitizeStringMap(in)
}

func rollbackTx(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}

func sanitizeStringSlice(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}

	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, item := range in {
		clean := sanitizeUTF8(item)
		if clean == "" {
			continue
		}
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}

	return out
}

func decodeJSONStringSlice(raw []byte) []string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []string{}
	}

	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return []string{}
	}
	return sanitizeStringSlice(out)
}

func cloneRawJSON(raw []byte) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(append([]byte(nil), raw...))
}
