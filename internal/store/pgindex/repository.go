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

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/segmentio/ksuid"
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

// unassembled header row used by Milestone 6 assembly service.
type AssemblyCandidate struct {
	ID            int64
	ProviderID    int64
	NewsgroupID   int64
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

// binary upsert input for assembly service.
type BinaryRecord struct {
	ProviderID       int64
	NewsgroupID      int64
	PosterID         int64
	ReleaseKey       string
	ReleaseName      string
	BinaryKey        string
	BinaryName       string
	FileName         string
	TotalParts       int
	PostedAt         *time.Time
	MatchConfidence  float64
	MatchStatus      string
	GroupingEvidence map[string]any
}

// binary part upsert input for assembly service.
type BinaryPartRecord struct {
	BinaryID        int64
	ArticleHeaderID int64
	MessageID       string
	PartNumber      int
	TotalParts      int
	SegmentBytes    int64
	FileName        string
}

// binary summary returned to release formation.
type BinarySummary struct {
	BinaryID           int64
	ProviderID         int64
	NewsgroupID        int64
	ReleaseKey         string
	ReleaseName        string
	BinaryKey          string
	BinaryName         string
	FileName           string
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
	ProviderID  int64
	NewsgroupID int64
	ReleaseKey  string
	ReleaseName string
	PostedAt    *time.Time
	BinaryCount int
	TotalBytes  int64
}

// release catalog upsert input.
type ReleaseRecord struct {
	ReleaseID               string
	GUID                    string
	ProviderID              int64
	ReleaseKey              string
	GroupName               string
	Title                   string
	SourceTitle             string
	DeobfuscatedTitle       string
	SearchTitle             string
	Category                string
	Classification          string
	Poster                  string
	SizeBytes               int64
	PostedAt                *time.Time
	FileCount               int
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

// read DTOs for PG-backed NZB materialization in Milestone 7.
type CatalogReleaseFile struct {
	ID        int64
	FileName  string
	Subject   string
	Poster    string
	PostedAt  *time.Time
	SizeBytes int64
	IsPars    bool
	FileIndex int
}

type CatalogArticleRef struct {
	MessageID  string
	Bytes      int64
	PartNumber int
}

type IndexerStageClaimRequest struct {
	StageName     string
	TriggerKind   string
	Owner         string
	Enabled       bool
	Interval      time.Duration
	BatchSize     int
	Concurrency   int
	Backoff       time.Duration
	LeaseDuration time.Duration
}

type IndexerStageClaimResult struct {
	Claimed bool
	Reason  string
	Run     *IndexerStageRun
}

type IndexerStageState struct {
	StageName       string
	Enabled         bool
	Paused          bool
	IntervalSeconds int
	BatchSize       int
	Concurrency     int
	BackoffSeconds  int
	LeaseOwner      string
	LeaseExpiresAt  *time.Time
	LastHeartbeatAt *time.Time
	LastRunID       int64
	LastSuccessAt   *time.Time
	LastError       string
	UpdatedAt       time.Time
}

type IndexerStageRun struct {
	ID          int64
	StageName   string
	TriggerKind string
	Status      string
	ClaimedBy   string
	StartedAt   time.Time
	HeartbeatAt *time.Time
	FinishedAt  *time.Time
	ErrorText   string
	MetricsJSON json.RawMessage
}

type IndexerStageFinishRequest struct {
	RunID       int64
	Owner       string
	ErrorText   string
	MetricsJSON json.RawMessage
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

// CHANGED: list article headers not yet assembled into binary_parts.
func (s *Store) ListUnassembledArticleHeaders(ctx context.Context, limit int) ([]AssemblyCandidate, error) {
	if limit <= 0 {
		limit = 5000
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			ah.id,
			ah.provider_id,
			ah.newsgroup_id,
			ah.article_number,
			ah.message_id,
			ah.subject,
			ah.poster,
			ah.date_utc,
			ah.bytes,
			ah.lines,
			ah.xref,
			ah.raw_overview_json
		FROM article_headers ah
		LEFT JOIN binary_parts bp ON bp.article_header_id = ah.id
		WHERE bp.article_header_id IS NULL
		ORDER BY ah.newsgroup_id, ah.article_number
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list unassembled article headers: %w", err)
	}
	defer rows.Close()

	out := make([]AssemblyCandidate, 0, limit)
	for rows.Next() {
		var item AssemblyCandidate
		var date sql.NullTime
		var raw string

		if err := rows.Scan(
			&item.ID,
			&item.ProviderID,
			&item.NewsgroupID,
			&item.ArticleNumber,
			&item.MessageID,
			&item.Subject,
			&item.Poster,
			&date,
			&item.Bytes,
			&item.Lines,
			&item.Xref,
			&raw,
		); err != nil {
			return nil, fmt.Errorf("scan unassembled article header: %w", err)
		}

		if date.Valid {
			t := date.Time.UTC()
			item.DateUTC = &t
		}

		if raw != "" {
			_ = json.Unmarshal([]byte(raw), &item.RawOverview)
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unassembled article headers: %w", err)
	}

	return out, nil
}

// CHANGED: normalize posters into a dimension table.
func (s *Store) EnsurePoster(ctx context.Context, posterName string) (int64, error) {
	posterName = strings.TrimSpace(posterName)
	if posterName == "" {
		return 0, nil
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO posters (poster_name)
		VALUES ($1)
		ON CONFLICT (poster_name) DO UPDATE
		SET poster_name = EXCLUDED.poster_name
		RETURNING id`,
		posterName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure poster %q: %w", posterName, err)
	}

	return id, nil
}

// CHANGED: map article header -> poster id for later enrichment/debugging.
func (s *Store) LinkArticlePoster(ctx context.Context, articleHeaderID, posterID int64) error {
	if articleHeaderID <= 0 || posterID <= 0 {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO article_poster_map (article_header_id, poster_id)
		VALUES ($1, $2)
		ON CONFLICT (article_header_id) DO UPDATE
		SET poster_id = EXCLUDED.poster_id`,
		articleHeaderID, posterID,
	)
	if err != nil {
		return fmt.Errorf("link article %d to poster %d: %w", articleHeaderID, posterID, err)
	}

	return nil
}

// CHANGED: create/update a binary grouping row.
func (s *Store) UpsertBinary(ctx context.Context, in BinaryRecord) (int64, error) {
	if in.ProviderID <= 0 || in.NewsgroupID <= 0 {
		return 0, fmt.Errorf("provider id and newsgroup id are required")
	}

	in.ReleaseKey = strings.TrimSpace(in.ReleaseKey)
	in.BinaryKey = strings.TrimSpace(in.BinaryKey)
	if in.ReleaseKey == "" || in.BinaryKey == "" {
		return 0, fmt.Errorf("release key and binary key are required")
	}
	in.MatchStatus = strings.TrimSpace(in.MatchStatus)
	if in.MatchStatus == "" {
		in.MatchStatus = "low_confidence"
	}

	var postedAt any
	if in.PostedAt != nil {
		postedAt = in.PostedAt.UTC()
	}

	var posterID any
	if in.PosterID > 0 {
		posterID = in.PosterID
	}

	evidenceJSON := []byte(`{}`)
	if cleanEvidence := sanitizeStringMap(in.GroupingEvidence); len(cleanEvidence) > 0 {
		b, err := json.Marshal(cleanEvidence)
		if err != nil {
			return 0, fmt.Errorf("marshal binary grouping evidence %q: %w", in.BinaryKey, err)
		}
		evidenceJSON = b
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO binaries (
			provider_id,
			newsgroup_id,
			poster_id,
			release_key,
			release_name,
			binary_key,
			binary_name,
			file_name,
			total_parts,
			posted_at,
			match_confidence,
			match_status,
			grouping_evidence_json,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NOW())
		ON CONFLICT (provider_id, newsgroup_id, binary_key) DO UPDATE
		SET poster_id = COALESCE(EXCLUDED.poster_id, binaries.poster_id),
		    release_key = EXCLUDED.release_key,
		    release_name = EXCLUDED.release_name,
		    binary_name = EXCLUDED.binary_name,
		    file_name = EXCLUDED.file_name,
		    total_parts = GREATEST(binaries.total_parts, EXCLUDED.total_parts),
		    posted_at = COALESCE(binaries.posted_at, EXCLUDED.posted_at),
		    match_confidence = GREATEST(binaries.match_confidence, EXCLUDED.match_confidence),
		    match_status = CASE
		    	WHEN EXCLUDED.match_confidence >= binaries.match_confidence THEN EXCLUDED.match_status
		    	ELSE binaries.match_status
		    END,
		    grouping_evidence_json = CASE
		    	WHEN EXCLUDED.match_confidence >= binaries.match_confidence THEN EXCLUDED.grouping_evidence_json
		    	ELSE binaries.grouping_evidence_json
		    END,
		    updated_at = NOW()
		RETURNING id`,
		in.ProviderID,
		in.NewsgroupID,
		posterID,
		in.ReleaseKey,
		strings.TrimSpace(in.ReleaseName),
		in.BinaryKey,
		strings.TrimSpace(in.BinaryName),
		strings.TrimSpace(in.FileName),
		in.TotalParts,
		postedAt,
		in.MatchConfidence,
		in.MatchStatus,
		evidenceJSON,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert binary %q: %w", in.BinaryKey, err)
	}

	return id, nil
}

// CHANGED: add/update one binary part row.
func (s *Store) UpsertBinaryPart(ctx context.Context, in BinaryPartRecord) error {
	if in.BinaryID <= 0 || in.ArticleHeaderID <= 0 {
		return fmt.Errorf("binary id and article header id are required")
	}
	if in.PartNumber <= 0 {
		return fmt.Errorf("part number is required")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO binary_parts (
			binary_id,
			article_header_id,
			message_id,
			part_number,
			total_parts,
			segment_bytes,
			file_name,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
		ON CONFLICT (binary_id, part_number) DO UPDATE
		SET article_header_id = EXCLUDED.article_header_id,
		    message_id = EXCLUDED.message_id,
		    total_parts = GREATEST(binary_parts.total_parts, EXCLUDED.total_parts),
		    segment_bytes = EXCLUDED.segment_bytes,
		    file_name = EXCLUDED.file_name,
		    updated_at = NOW()`,
		in.BinaryID,
		in.ArticleHeaderID,
		strings.TrimSpace(in.MessageID),
		in.PartNumber,
		in.TotalParts,
		in.SegmentBytes,
		strings.TrimSpace(in.FileName),
	)
	if err != nil {
		return fmt.Errorf("upsert binary part binary=%d part=%d: %w", in.BinaryID, in.PartNumber, err)
	}

	return nil
}

// CHANGED: recompute binary aggregate stats after parts were inserted.
func (s *Store) RefreshBinaryStats(ctx context.Context, binaryID int64) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE binaries b
		SET observed_parts = agg.observed_parts,
		    total_bytes = agg.total_bytes,
		    first_article_number = agg.first_article_number,
		    last_article_number = agg.last_article_number,
		    updated_at = NOW()
		FROM (
			SELECT
				bp.binary_id,
				COUNT(*)::INTEGER AS observed_parts,
				COALESCE(SUM(bp.segment_bytes), 0)::BIGINT AS total_bytes,
				COALESCE(MIN(ah.article_number), 0)::BIGINT AS first_article_number,
				COALESCE(MAX(ah.article_number), 0)::BIGINT AS last_article_number
			FROM binary_parts bp
			JOIN article_headers ah ON ah.id = bp.article_header_id
			WHERE bp.binary_id = $1
			GROUP BY bp.binary_id
		) agg
		WHERE b.id = agg.binary_id`, binaryID)
	if err != nil {
		return fmt.Errorf("refresh binary stats %d: %w", binaryID, err)
	}

	return nil
}

// CHANGED: return release groups whose binaries are new or changed since last formation.
func (s *Store) ListReleaseCandidates(ctx context.Context, limit int) ([]ReleaseCandidate, error) {
	if limit <= 0 {
		limit = 1000
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			b.provider_id,
			b.newsgroup_id,
			b.release_key,
			MAX(b.release_name) AS release_name,
			MIN(b.posted_at) AS posted_at,
			COUNT(*)::INTEGER AS binary_count,
			COALESCE(SUM(b.total_bytes), 0)::BIGINT AS total_bytes
		FROM binaries b
		LEFT JOIN (
			SELECT provider_id, release_key, MAX(updated_at) AS updated_at
			FROM releases
			GROUP BY provider_id, release_key
		) r
			ON r.provider_id = b.provider_id
			AND r.release_key = b.release_key
		GROUP BY b.provider_id, b.newsgroup_id, b.release_key, r.updated_at
		HAVING r.updated_at IS NULL OR MAX(b.updated_at) > r.updated_at
		ORDER BY MIN(b.posted_at) NULLS LAST, b.release_key
		LIMIT $1`, limit)
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

// CHANGED: fetch binaries that belong to one release candidate.
func (s *Store) ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, releaseKey string) ([]BinarySummary, error) {
	if providerID <= 0 || newsgroupID <= 0 {
		return nil, fmt.Errorf("provider id and newsgroup id are required")
	}
	releaseKey = strings.TrimSpace(releaseKey)
	if releaseKey == "" {
		return nil, fmt.Errorf("release key is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			b.id,
			b.provider_id,
			b.newsgroup_id,
			b.release_key,
			b.release_name,
			b.binary_key,
			b.binary_name,
			b.file_name,
			COALESCE(p.poster_name, ''),
			b.posted_at,
			b.total_parts,
			b.observed_parts,
			b.total_bytes,
			b.first_article_number,
			b.last_article_number,
			b.match_confidence,
			b.match_status
		FROM binaries b
		LEFT JOIN posters p ON p.id = b.poster_id
		WHERE b.provider_id = $1
		  AND b.newsgroup_id = $2
		  AND b.release_key = $3
		ORDER BY b.file_name, b.first_article_number, b.id`,
		providerID, newsgroupID, releaseKey,
	)
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
			&item.ReleaseKey,
			&item.ReleaseName,
			&item.BinaryKey,
			&item.BinaryName,
			&item.FileName,
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
	in.ReleaseKey = strings.TrimSpace(in.ReleaseKey)
	if in.ReleaseKey == "" {
		return "", fmt.Errorf("release key is required")
	}
	in.GroupName = strings.TrimSpace(in.GroupName)
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
			release_key,
			group_name,
			title,
			source_title,
			deobfuscated_title,
			search_title,
			category,
			classification,
			poster,
			size_bytes,
			posted_at,
			file_count,
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
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,'usenet_index',NOW())
		ON CONFLICT (provider_id, group_name) DO UPDATE
		SET guid = EXCLUDED.guid,
		    release_key = EXCLUDED.release_key,
		    title = EXCLUDED.title,
		    source_title = EXCLUDED.source_title,
		    deobfuscated_title = EXCLUDED.deobfuscated_title,
		    search_title = EXCLUDED.search_title,
		    category = EXCLUDED.category,
		    classification = EXCLUDED.classification,
		    poster = EXCLUDED.poster,
		    size_bytes = EXCLUDED.size_bytes,
		    posted_at = EXCLUDED.posted_at,
		    file_count = EXCLUDED.file_count,
		    par_file_count = EXCLUDED.par_file_count,
		    completion_pct = EXCLUDED.completion_pct,
		    match_confidence = EXCLUDED.match_confidence,
		    identity_status = EXCLUDED.identity_status,
		    passworded = EXCLUDED.passworded,
		    passworded_known = EXCLUDED.passworded_known,
		    passworded_unknown = EXCLUDED.passworded_unknown,
		    password_state = EXCLUDED.password_state,
		    preferred_password_id = EXCLUDED.preferred_password_id,
		    encrypted = EXCLUDED.encrypted,
		    has_par2 = EXCLUDED.has_par2,
		    has_nfo = EXCLUDED.has_nfo,
		    archive_count = EXCLUDED.archive_count,
		    video_count = EXCLUDED.video_count,
		    audio_count = EXCLUDED.audio_count,
		    sample_present = EXCLUDED.sample_present,
		    availability_score = EXCLUDED.availability_score,
		    availability_tier = EXCLUDED.availability_tier,
		    media_quality_score = EXCLUDED.media_quality_score,
		    media_quality_tier = EXCLUDED.media_quality_tier,
		    identity_confidence_score = EXCLUDED.identity_confidence_score,
		    runtime_seconds = EXCLUDED.runtime_seconds,
		    primary_resolution = EXCLUDED.primary_resolution,
		    primary_video_codec = EXCLUDED.primary_video_codec,
		    primary_audio_codec = EXCLUDED.primary_audio_codec,
		    subtitle_languages_json = EXCLUDED.subtitle_languages_json,
		    media_tags_json = EXCLUDED.media_tags_json,
		    metadata_updated_at = EXCLUDED.metadata_updated_at,
		    updated_at = NOW()
		RETURNING release_id`,
		in.ReleaseID,
		in.GUID,
		in.ProviderID,
		in.ReleaseKey,
		in.GroupName,
		strings.TrimSpace(in.Title),
		strings.TrimSpace(in.SourceTitle),
		strings.TrimSpace(in.DeobfuscatedTitle),
		strings.TrimSpace(in.SearchTitle),
		strings.TrimSpace(in.Category),
		strings.TrimSpace(in.Classification),
		strings.TrimSpace(in.Poster),
		in.SizeBytes,
		postedAt,
		in.FileCount,
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

	return releaseID, nil
}

func (s *Store) DeleteStaleReleasesForSourceKey(ctx context.Context, providerID int64, releaseKey string, keepGroupNames []string) error {
	if providerID <= 0 {
		return fmt.Errorf("provider id is required")
	}
	releaseKey = strings.TrimSpace(releaseKey)
	if releaseKey == "" {
		return fmt.Errorf("release key is required")
	}

	keep := sanitizeStringSlice(keepGroupNames)
	if len(keep) == 0 {
		_, err := s.db.ExecContext(ctx, `
			DELETE FROM releases
			WHERE provider_id = $1
			  AND release_key = $2`,
			providerID,
			releaseKey,
		)
		if err != nil {
			return fmt.Errorf("delete stale releases for provider=%d release_key=%q: %w", providerID, releaseKey, err)
		}
		return nil
	}

	args := make([]any, 0, len(keep)+2)
	args = append(args, providerID, releaseKey)

	placeholders := make([]string, 0, len(keep))
	for i, groupName := range keep {
		args = append(args, groupName)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+3))
	}

	query := `
		DELETE FROM releases
		WHERE provider_id = $1
		  AND release_key = $2
		  AND group_name NOT IN (` + strings.Join(placeholders, ",") + `)`

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete stale releases for provider=%d release_key=%q keep=%v: %w", providerID, releaseKey, keep, err)
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

// CHANGED: PG release catalog read by id for later resolver work.
func (s *Store) GetCatalogReleaseByID(ctx context.Context, releaseID string) (*domain.Release, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT
			r.release_id,
			r.title,
			r.guid,
			r.source_kind,
			r.size_bytes,
			r.posted_at,
			r.category,
			r.poster,
			COALESCE(n.generation_status, 'pending')
		FROM releases r
		LEFT JOIN nzb_cache n ON n.release_id = r.release_id
		WHERE r.release_id = $1`, releaseID)

	rel, err := scanCatalogRelease(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get catalog release %s: %w", releaseID, err)
	}

	return rel, nil
}

// CHANGED: PG release catalog search for later aggregator/resolver integration.
func (s *Store) SearchCatalogReleases(ctx context.Context, query string, limit int) ([]*domain.Release, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []*domain.Release{}, nil
	}
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.release_id,
			r.title,
			r.guid,
			r.source_kind,
			r.size_bytes,
			r.posted_at,
			r.category,
			r.poster,
			COALESCE(n.generation_status, 'pending')
		FROM releases r
		LEFT JOIN nzb_cache n ON n.release_id = r.release_id
		WHERE r.search_title ILIKE '%' || $1 || '%'
		ORDER BY r.posted_at DESC NULLS LAST, r.title
		LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search catalog releases %q: %w", query, err)
	}
	defer rows.Close()

	out := make([]*domain.Release, 0, limit)
	for rows.Next() {
		rel, err := scanCatalogRelease(rows)
		if err != nil {
			return nil, fmt.Errorf("scan catalog search result: %w", err)
		}
		out = append(out, rel)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog search results: %w", err)
	}

	return out, nil
}

// CHANGED: list release files for one formed PG release.
func (s *Store) ListCatalogReleaseFiles(ctx context.Context, releaseID string) ([]CatalogReleaseFile, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			file_name,
			subject,
			poster,
			posted_at,
			size_bytes,
			is_pars,
			file_index
		FROM release_files
		WHERE release_id = $1
		ORDER BY file_index, id`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list catalog release files %s: %w", releaseID, err)
	}
	defer rows.Close()

	out := make([]CatalogReleaseFile, 0, 32)
	for rows.Next() {
		var item CatalogReleaseFile
		var postedAt sql.NullTime

		if err := rows.Scan(
			&item.ID,
			&item.FileName,
			&item.Subject,
			&item.Poster,
			&postedAt,
			&item.SizeBytes,
			&item.IsPars,
			&item.FileIndex,
		); err != nil {
			return nil, fmt.Errorf("scan catalog release file: %w", err)
		}

		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog release files: %w", err)
	}

	return out, nil
}

// CHANGED: list article refs for one release_file row.
func (s *Store) ListCatalogReleaseFileArticles(ctx context.Context, releaseFileID int64) ([]CatalogArticleRef, error) {
	if releaseFileID <= 0 {
		return nil, fmt.Errorf("release file id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			ah.message_id,
			ah.bytes,
			rfa.part_number
		FROM release_file_articles rfa
		JOIN article_headers ah ON ah.id = rfa.article_header_id
		WHERE rfa.release_file_id = $1
		ORDER BY rfa.part_number`, releaseFileID)
	if err != nil {
		return nil, fmt.Errorf("list catalog release file articles %d: %w", releaseFileID, err)
	}
	defer rows.Close()

	out := make([]CatalogArticleRef, 0, 128)
	for rows.Next() {
		var item CatalogArticleRef
		if err := rows.Scan(&item.MessageID, &item.Bytes, &item.PartNumber); err != nil {
			return nil, fmt.Errorf("scan catalog article ref: %w", err)
		}
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog article refs: %w", err)
	}

	return out, nil
}

// CHANGED: list newsgroups attached to a formed release.
func (s *Store) ListCatalogReleaseNewsgroups(ctx context.Context, releaseID string) ([]string, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ng.group_name
		FROM release_newsgroups rng
		JOIN newsgroups ng ON ng.id = rng.newsgroup_id
		WHERE rng.release_id = $1
		ORDER BY ng.group_name`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list catalog release newsgroups %s: %w", releaseID, err)
	}
	defer rows.Close()

	out := make([]string, 0, 8)
	for rows.Next() {
		var groupName string
		if err := rows.Scan(&groupName); err != nil {
			return nil, fmt.Errorf("scan catalog release newsgroup: %w", err)
		}
		out = append(out, strings.TrimSpace(groupName))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog release newsgroups: %w", err)
	}

	return out, nil
}

type releaseScanner interface {
	Scan(dest ...any) error
}

func scanCatalogRelease(scanner releaseScanner) (*domain.Release, error) {
	var rel domain.Release
	var source string
	var postedAt sql.NullTime
	var generationStatus string

	if err := scanner.Scan(
		&rel.ID,
		&rel.Title,
		&rel.GUID,
		&source,
		&rel.Size,
		&postedAt,
		&rel.Category,
		&rel.Poster,
		&generationStatus,
	); err != nil {
		return nil, err
	}

	if postedAt.Valid {
		rel.PublishDate = postedAt.Time.UTC()
	}
	rel.Source = source
	rel.CachePresent = strings.EqualFold(generationStatus, "ready")

	return &rel, nil
}

func (s *Store) ClaimIndexerStage(ctx context.Context, req IndexerStageClaimRequest) (*IndexerStageClaimResult, error) {
	stageName := strings.TrimSpace(req.StageName)
	if stageName == "" {
		return nil, fmt.Errorf("stage name is required")
	}

	owner := strings.TrimSpace(req.Owner)
	if owner == "" {
		return nil, fmt.Errorf("stage owner is required")
	}

	triggerKind := strings.TrimSpace(strings.ToLower(req.TriggerKind))
	if triggerKind == "" {
		triggerKind = "scheduled"
	}

	if req.Interval <= 0 {
		req.Interval = 10 * time.Minute
	}
	if req.Concurrency <= 0 {
		req.Concurrency = 1
	}
	if req.LeaseDuration <= 0 {
		req.LeaseDuration = 30 * time.Second
	}

	intervalSeconds := int(req.Interval / time.Second)
	if intervalSeconds <= 0 {
		intervalSeconds = 1
	}
	backoffSeconds := int(req.Backoff / time.Second)
	if backoffSeconds < 0 {
		backoffSeconds = 0
	}
	leaseExpiresAt := time.Now().UTC().Add(req.LeaseDuration)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim stage tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO indexer_stage_state (
			stage_name,
			enabled,
			interval_seconds,
			batch_size,
			concurrency,
			backoff_seconds,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (stage_name) DO UPDATE
		SET enabled = EXCLUDED.enabled,
		    interval_seconds = EXCLUDED.interval_seconds,
		    batch_size = EXCLUDED.batch_size,
		    concurrency = EXCLUDED.concurrency,
		    backoff_seconds = EXCLUDED.backoff_seconds,
		    updated_at = NOW()`,
		stageName,
		req.Enabled,
		intervalSeconds,
		req.BatchSize,
		req.Concurrency,
		backoffSeconds,
	); err != nil {
		return nil, fmt.Errorf("upsert indexer stage state %s: %w", stageName, err)
	}

	var (
		enabled      bool
		paused       bool
		leaseOwner   string
		leaseExpires sql.NullTime
		lastRunID    sql.NullInt64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT enabled, paused, lease_owner, lease_expires_at, last_run_id
		FROM indexer_stage_state
		WHERE stage_name = $1
		FOR UPDATE`,
		stageName,
	).Scan(&enabled, &paused, &leaseOwner, &leaseExpires, &lastRunID); err != nil {
		return nil, fmt.Errorf("lock indexer stage state %s: %w", stageName, err)
	}

	if !enabled {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit disabled stage claim %s: %w", stageName, err)
		}
		return &IndexerStageClaimResult{Claimed: false, Reason: "disabled"}, nil
	}

	if paused {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit paused stage claim %s: %w", stageName, err)
		}
		return &IndexerStageClaimResult{Claimed: false, Reason: "paused"}, nil
	}

	if leaseOwner != "" && leaseExpires.Valid && leaseExpires.Time.After(time.Now().UTC()) {
		if leaseOwner != owner {
			if err := tx.Commit(); err != nil {
				return nil, fmt.Errorf("commit leased stage claim %s: %w", stageName, err)
			}
			return &IndexerStageClaimResult{Claimed: false, Reason: "leased"}, nil
		}

		if lastRunID.Valid {
			var status string
			if err := tx.QueryRowContext(ctx, `
				SELECT status
				FROM indexer_stage_runs
				WHERE id = $1`,
				lastRunID.Int64,
			).Scan(&status); err == nil && strings.EqualFold(status, "running") {
				if err := tx.Commit(); err != nil {
					return nil, fmt.Errorf("commit running stage claim %s: %w", stageName, err)
				}
				return &IndexerStageClaimResult{Claimed: false, Reason: "running"}, nil
			}
		}
	}

	if lastRunID.Valid {
		if _, err := tx.ExecContext(ctx, `
			UPDATE indexer_stage_runs
			SET status = 'abandoned',
			    error_text = CASE
			        WHEN error_text = '' THEN 'lease expired before completion'
			        ELSE error_text
			    END,
			    heartbeat_at = COALESCE(heartbeat_at, NOW()),
			    finished_at = COALESCE(finished_at, NOW())
			WHERE id = $1
			  AND status = 'running'`,
			lastRunID.Int64,
		); err != nil {
			return nil, fmt.Errorf("mark stale stage run %d abandoned: %w", lastRunID.Int64, err)
		}
	}

	run := &IndexerStageRun{
		StageName:   stageName,
		TriggerKind: triggerKind,
		Status:      "running",
		ClaimedBy:   owner,
		StartedAt:   time.Now().UTC(),
	}

	if err := tx.QueryRowContext(ctx, `
		INSERT INTO indexer_stage_runs (
			stage_name,
			trigger_kind,
			status,
			claimed_by,
			started_at,
			heartbeat_at,
			error_text,
			metrics_json
		)
		VALUES ($1, $2, 'running', $3, NOW(), NOW(), '', '{}'::jsonb)
		RETURNING id, started_at, heartbeat_at`,
		stageName,
		triggerKind,
		owner,
	).Scan(&run.ID, &run.StartedAt, &leaseExpires); err != nil {
		return nil, fmt.Errorf("insert indexer stage run %s: %w", stageName, err)
	}
	if leaseExpires.Valid {
		t := leaseExpires.Time.UTC()
		run.HeartbeatAt = &t
	}
	run.MetricsJSON = json.RawMessage("{}")

	if _, err := tx.ExecContext(ctx, `
		UPDATE indexer_stage_state
		SET lease_owner = $2,
		    lease_expires_at = $3,
		    last_heartbeat_at = NOW(),
		    last_run_id = $4,
		    last_error = '',
		    updated_at = NOW()
		WHERE stage_name = $1`,
		stageName,
		owner,
		leaseExpiresAt,
		run.ID,
	); err != nil {
		return nil, fmt.Errorf("update claimed stage state %s: %w", stageName, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit stage claim %s: %w", stageName, err)
	}

	return &IndexerStageClaimResult{
		Claimed: true,
		Run:     run,
	}, nil
}

func (s *Store) HeartbeatIndexerStageRun(ctx context.Context, runID int64, owner string, leaseDuration time.Duration) error {
	if runID <= 0 {
		return fmt.Errorf("run id is required")
	}

	owner = strings.TrimSpace(owner)
	if owner == "" {
		return fmt.Errorf("stage owner is required")
	}
	if leaseDuration <= 0 {
		leaseDuration = 30 * time.Second
	}
	leaseSeconds := int(leaseDuration / time.Second)
	if leaseSeconds <= 0 {
		leaseSeconds = 1
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE indexer_stage_runs
		SET heartbeat_at = NOW()
		WHERE id = $1
		  AND claimed_by = $2
		  AND status = 'running'`,
		runID,
		owner,
	)
	if err != nil {
		return fmt.Errorf("heartbeat stage run %d: %w", runID, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("heartbeat stage run %d rows affected: %w", runID, err)
	}
	if rows == 0 {
		return fmt.Errorf("stage run %d is no longer running for owner %s", runID, owner)
	}

	stateResult, err := s.db.ExecContext(ctx, `
		UPDATE indexer_stage_state
		SET lease_expires_at = NOW() + make_interval(secs => $3),
		    last_heartbeat_at = NOW(),
		    updated_at = NOW()
		WHERE last_run_id = $1
		  AND lease_owner = $2`,
		runID,
		owner,
		leaseSeconds,
	)
	if err != nil {
		return fmt.Errorf("heartbeat stage state for run %d: %w", runID, err)
	}

	stateRows, err := stateResult.RowsAffected()
	if err != nil {
		return fmt.Errorf("heartbeat stage state rows affected for run %d: %w", runID, err)
	}
	if stateRows == 0 {
		return fmt.Errorf("stage state for run %d is no longer owned by %s", runID, owner)
	}

	return nil
}

func (s *Store) CompleteIndexerStageRun(ctx context.Context, req IndexerStageFinishRequest) error {
	return s.finishIndexerStageRun(ctx, req, "completed")
}

func (s *Store) FailIndexerStageRun(ctx context.Context, req IndexerStageFinishRequest) error {
	return s.finishIndexerStageRun(ctx, req, "failed")
}

func (s *Store) PauseIndexerStage(ctx context.Context, stageName string) error {
	return s.setIndexerStagePaused(ctx, stageName, true)
}

func (s *Store) ResumeIndexerStage(ctx context.Context, stageName string) error {
	return s.setIndexerStagePaused(ctx, stageName, false)
}

func (s *Store) ListIndexerStageStates(ctx context.Context) ([]IndexerStageState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			stage_name,
			enabled,
			paused,
			interval_seconds,
			batch_size,
			concurrency,
			backoff_seconds,
			lease_owner,
			lease_expires_at,
			last_heartbeat_at,
			last_run_id,
			last_success_at,
			last_error,
			updated_at
		FROM indexer_stage_state
		ORDER BY stage_name`)
	if err != nil {
		return nil, fmt.Errorf("list indexer stage states: %w", err)
	}
	defer rows.Close()

	out := make([]IndexerStageState, 0, 16)
	for rows.Next() {
		item, err := scanIndexerStageState(rows)
		if err != nil {
			return nil, fmt.Errorf("scan indexer stage state: %w", err)
		}
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexer stage states: %w", err)
	}

	return out, nil
}

func (s *Store) ListIndexerStageRuns(ctx context.Context, stageName string, limit int) ([]IndexerStageRun, error) {
	stageName = strings.TrimSpace(stageName)
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT
			id,
			stage_name,
			trigger_kind,
			status,
			claimed_by,
			started_at,
			heartbeat_at,
			finished_at,
			error_text,
			metrics_json
		FROM indexer_stage_runs`
	args := []any{}
	if stageName != "" {
		query += ` WHERE stage_name = $1`
		args = append(args, stageName)
		query += ` ORDER BY started_at DESC, id DESC LIMIT $2`
		args = append(args, limit)
	} else {
		query += ` ORDER BY started_at DESC, id DESC LIMIT $1`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list indexer stage runs: %w", err)
	}
	defer rows.Close()

	out := make([]IndexerStageRun, 0, limit)
	for rows.Next() {
		item, err := scanIndexerStageRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan indexer stage run: %w", err)
		}
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexer stage runs: %w", err)
	}

	return out, nil
}

func (s *Store) finishIndexerStageRun(ctx context.Context, req IndexerStageFinishRequest, status string) error {
	if req.RunID <= 0 {
		return fmt.Errorf("run id is required")
	}

	req.Owner = strings.TrimSpace(req.Owner)
	if req.Owner == "" {
		return fmt.Errorf("stage owner is required")
	}

	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		return fmt.Errorf("stage run status is required")
	}

	metrics := req.MetricsJSON
	if len(metrics) == 0 {
		metrics = json.RawMessage("{}")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin finish stage run tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var stageName string
	if err := tx.QueryRowContext(ctx, `
		SELECT stage_name
		FROM indexer_stage_runs
		WHERE id = $1
		  AND claimed_by = $2
		FOR UPDATE`,
		req.RunID,
		req.Owner,
	).Scan(&stageName); err != nil {
		return fmt.Errorf("lock stage run %d: %w", req.RunID, err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE indexer_stage_runs
		SET status = $2,
		    heartbeat_at = NOW(),
		    finished_at = NOW(),
		    error_text = $3,
		    metrics_json = $4
		WHERE id = $1`,
		req.RunID,
		status,
		req.ErrorText,
		metrics,
	); err != nil {
		return fmt.Errorf("finish stage run %d: %w", req.RunID, err)
	}

	success := status == "completed"
	if _, err := tx.ExecContext(ctx, `
		UPDATE indexer_stage_state
		SET lease_owner = CASE
		        WHEN last_run_id = $1 AND lease_owner = $2 THEN ''
		        ELSE lease_owner
		    END,
		    lease_expires_at = CASE
		        WHEN last_run_id = $1 AND lease_owner = $2 THEN NULL
		        ELSE lease_expires_at
		    END,
		    last_heartbeat_at = CASE
		        WHEN last_run_id = $1 THEN NOW()
		        ELSE last_heartbeat_at
		    END,
		    last_success_at = CASE
		        WHEN last_run_id = $1 AND $3 THEN NOW()
		        ELSE last_success_at
		    END,
		    last_error = CASE
		        WHEN last_run_id = $1 THEN $4
		        ELSE last_error
		    END,
		    updated_at = CASE
		        WHEN last_run_id = $1 THEN NOW()
		        ELSE updated_at
		    END
		WHERE stage_name = $5`,
		req.RunID,
		req.Owner,
		success,
		req.ErrorText,
		stageName,
	); err != nil {
		return fmt.Errorf("update stage state for run %d: %w", req.RunID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit finish stage run %d: %w", req.RunID, err)
	}

	return nil
}

func (s *Store) setIndexerStagePaused(ctx context.Context, stageName string, paused bool) error {
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return fmt.Errorf("stage name is required")
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO indexer_stage_state (stage_name, paused, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (stage_name) DO UPDATE
		SET paused = EXCLUDED.paused,
		    updated_at = NOW()`,
		stageName,
		paused,
	); err != nil {
		return fmt.Errorf("set paused=%t for stage %s: %w", paused, stageName, err)
	}

	return nil
}

func scanIndexerStageState(scanner releaseScanner) (IndexerStageState, error) {
	var (
		item            IndexerStageState
		leaseExpiresAt  sql.NullTime
		lastHeartbeatAt sql.NullTime
		lastRunID       sql.NullInt64
		lastSuccessAt   sql.NullTime
	)

	if err := scanner.Scan(
		&item.StageName,
		&item.Enabled,
		&item.Paused,
		&item.IntervalSeconds,
		&item.BatchSize,
		&item.Concurrency,
		&item.BackoffSeconds,
		&item.LeaseOwner,
		&leaseExpiresAt,
		&lastHeartbeatAt,
		&lastRunID,
		&lastSuccessAt,
		&item.LastError,
		&item.UpdatedAt,
	); err != nil {
		return IndexerStageState{}, err
	}

	if leaseExpiresAt.Valid {
		t := leaseExpiresAt.Time.UTC()
		item.LeaseExpiresAt = &t
	}
	if lastHeartbeatAt.Valid {
		t := lastHeartbeatAt.Time.UTC()
		item.LastHeartbeatAt = &t
	}
	if lastRunID.Valid {
		item.LastRunID = lastRunID.Int64
	}
	if lastSuccessAt.Valid {
		t := lastSuccessAt.Time.UTC()
		item.LastSuccessAt = &t
	}
	item.UpdatedAt = item.UpdatedAt.UTC()

	return item, nil
}

func scanIndexerStageRun(scanner releaseScanner) (IndexerStageRun, error) {
	var (
		item        IndexerStageRun
		heartbeatAt sql.NullTime
		finishedAt  sql.NullTime
		metricsJSON []byte
	)

	if err := scanner.Scan(
		&item.ID,
		&item.StageName,
		&item.TriggerKind,
		&item.Status,
		&item.ClaimedBy,
		&item.StartedAt,
		&heartbeatAt,
		&finishedAt,
		&item.ErrorText,
		&metricsJSON,
	); err != nil {
		return IndexerStageRun{}, err
	}

	item.StartedAt = item.StartedAt.UTC()
	if heartbeatAt.Valid {
		t := heartbeatAt.Time.UTC()
		item.HeartbeatAt = &t
	}
	if finishedAt.Valid {
		t := finishedAt.Time.UTC()
		item.FinishedAt = &t
	}
	if len(metricsJSON) == 0 {
		item.MetricsJSON = json.RawMessage("{}")
	} else {
		item.MetricsJSON = json.RawMessage(append([]byte(nil), metricsJSON...))
	}

	return item, nil
}

func nullIfZero(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
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
