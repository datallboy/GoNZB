package pgindex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrBinaryNotFound  = errors.New("binary not found")
	ErrReleaseNotFound = errors.New("release not found")
)

func IsBinaryNotFound(err error) bool {
	return errors.Is(err, ErrBinaryNotFound)
}

func IsReleaseNotFound(err error) bool {
	return errors.Is(err, ErrReleaseNotFound)
}

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

const articleHeaderInsertBatchSize = 500

type preparedArticleHeaderInsert struct {
	ArticleNumber int64
	MessageID     string
	Subject       string
	Poster        string
	DateUTC       *time.Time
	Bytes         int64
	Lines         int
	Xref          string
	Parsed        parsedArticleMetadata
}

type payloadUpsertRow struct {
	ArticleHeaderID int64
	Subject         string
	PosterID        any
	Poster          string
	Xref            string
	FileName        string
	FileIndex       int
	FileTotal       int
	YEncPart        int
	YEncTotalParts  int
	FileSize        int64
}

type BackfillCheckpointState struct {
	ArticleNumber int64
	UntilDate     *time.Time
	CutoffReached bool
	StoppedReason string
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stringMapValue(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

type BinaryInspectionCandidate struct {
	StageName          string
	BinaryID           int64
	ReleaseID          string
	ProviderID         int64
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

type BinaryInspectionCandidateOptions struct {
	RequireExpectedFileCount bool
}

type BinaryInspectionClaimRequest struct {
	StageName     string
	Limit         int
	Owner         string
	LeaseDuration time.Duration
	Options       BinaryInspectionCandidateOptions
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

type BinaryRecoveryRecord struct {
	BinaryID     int64
	Kind         string
	Extension    string
	Source       string
	Confidence   float64
	Canonicalize bool
}

type YEncRecoveryCandidate struct {
	BinaryID                        int64
	ArticleHeaderID                 int64
	ProviderID                      int64
	NewsgroupID                     int64
	NewsgroupName                   string
	ArticleNumber                   int64
	MessageID                       string
	Subject                         string
	Poster                          string
	DateUTC                         *time.Time
	Bytes                           int64
	Lines                           int
	Xref                            string
	FileName                        string
	FileIndex                       int
	FileTotal                       int
	YEncPart                        int
	YEncTotal                       int
	YEncFileSize                    int64
	RawOverview                     map[string]any
	YEncRecoveryMissingCount        int
	YEncRecoveryRetryAfter          *time.Time
	CurrentBinaryKey                string
	CurrentReleaseFamilyKey         string
	CurrentBaseStem                 string
	CurrentReadinessBucket          string
	StructuredIdentityBinaryMatched bool
}

func (c YEncRecoveryCandidate) FetchGroups() []string {
	if strings.TrimSpace(c.NewsgroupName) == "" {
		return nil
	}
	return []string{strings.TrimSpace(c.NewsgroupName)}
}

func (c YEncRecoveryCandidate) CloneRawOverview() map[string]any {
	out := make(map[string]any, len(c.RawOverview)+8)
	for k, v := range c.RawOverview {
		out[k] = v
	}
	if strings.TrimSpace(c.FileName) != "" {
		out["name"] = c.FileName
	}
	if c.FileIndex > 0 {
		out["file_index"] = c.FileIndex
	}
	if c.FileTotal > 0 {
		out["file_total"] = c.FileTotal
	}
	if c.YEncPart > 0 {
		out["part"] = c.YEncPart
	}
	if c.YEncTotal > 0 {
		out["total"] = c.YEncTotal
	}
	if c.YEncFileSize > 0 {
		out["size"] = c.YEncFileSize
	}
	if c.Bytes > 0 {
		out["bytes"] = c.Bytes
	}
	return out
}

type YEncHeaderRecoveryRecord struct {
	BinaryID          int64
	ArticleHeaderID   int64
	SourceReleaseKey  string
	ReleaseFamilyKey  string
	FileSetKey        string
	FileFamilyKey     string
	IdentityStrength  string
	IdentityReason    string
	SubjectSetToken   string
	SubjectSetKind    string
	FamilyKind        string
	BaseStem          string
	IsAuxiliary       bool
	IsMainPayload     bool
	ReleaseKey        string
	ReleaseName       string
	BinaryKey         string
	BinaryName        string
	FileName          string
	FileIndex         int
	ExpectedFileCount int
	TotalParts        int
	MatchConfidence   float64
	MatchStatus       string
	GroupingEvidence  map[string]any
}

type YEncHeaderRecoveryResult struct {
	BinaryID       int64
	TargetBinaryID int64
	Merged         bool
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

type PAR2InspectionBatchRecord struct {
	StageName         string
	BinaryID          int64
	ReleaseID         string
	SourceUpdatedAt   *time.Time
	ArtifactRows      []BinaryInspectionArtifactRecord
	PAR2SetRows       []BinaryPAR2SetRecord
	PAR2TargetRows    []BinaryPAR2TargetRecord
	MaterializedBytes int64
	ToolProvenance    map[string]any
	Summary           map[string]any
}

type PAR2InspectionBatchResult struct {
	FlushedCandidates int
	RowsWritten       int64
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

type BinaryPAR2TargetRecord struct {
	BinaryID  int64
	ReleaseID string
	FileName  string
	FileSize  int64
	Metadata  map[string]any
}

type BinaryPAR2TargetCoverageResult struct {
	TargetCount        int
	MainTargetCount    int
	UpdatedBinaryCount int
	SummaryKeys        []releaseFamilySummaryKey
}

type CatalogReader interface {
	ListCatalogReleaseFiles(ctx context.Context, releaseID string) ([]CatalogReleaseFile, error)
	ListCatalogReleaseFileArticles(ctx context.Context, releaseFileID int64) ([]CatalogArticleRef, error)
	ListCatalogReleaseNewsgroups(ctx context.Context, releaseID string) ([]string, error)
	GetCatalogBinaryFile(ctx context.Context, binaryID int64) (*CatalogReleaseFile, error)
	ListCatalogBinaryArticles(ctx context.Context, binaryID int64) ([]CatalogArticleRef, error)
	ListCatalogBinaryNewsgroups(ctx context.Context, binaryID int64) ([]string, error)
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

func (s *Store) GetBackfillCheckpointState(ctx context.Context, providerID, newsgroupID int64) (*BackfillCheckpointState, error) {
	if providerID <= 0 {
		return nil, fmt.Errorf("provider id is required")
	}
	if newsgroupID <= 0 {
		return nil, fmt.Errorf("newsgroup id is required")
	}

	var (
		item      BackfillCheckpointState
		untilDate sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			backfill_article_number,
			backfill_until_date,
			backfill_cutoff_reached,
			backfill_stopped_reason
		FROM scrape_checkpoints
		WHERE provider_id = $1 AND newsgroup_id = $2`,
		providerID, newsgroupID,
	).Scan(&item.ArticleNumber, &untilDate, &item.CutoffReached, &item.StoppedReason)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get backfill checkpoint state p=%d g=%d: %w", providerID, newsgroupID, err)
	}
	if untilDate.Valid {
		t := untilDate.Time.UTC()
		item.UntilDate = &t
	}
	return &item, nil
}

func (s *Store) HasBackfillCutoffReachedForGroup(ctx context.Context, newsgroupID int64, untilDate time.Time) (bool, error) {
	if newsgroupID <= 0 {
		return false, fmt.Errorf("newsgroup id is required")
	}

	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM scrape_checkpoints
			WHERE newsgroup_id = $1
			  AND backfill_cutoff_reached = TRUE
			  AND backfill_until_date = $2
		)`,
		newsgroupID, untilDate.UTC(),
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check backfill cutoff reached for group %d: %w", newsgroupID, err)
	}
	return exists, nil
}

func (s *Store) SetBackfillCheckpointState(ctx context.Context, providerID, newsgroupID int64, untilDate *time.Time, cutoffReached bool, stoppedReason string) error {
	if providerID <= 0 {
		return fmt.Errorf("provider id is required")
	}
	if newsgroupID <= 0 {
		return fmt.Errorf("newsgroup id is required")
	}

	var until any
	if untilDate != nil {
		until = untilDate.UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO scrape_checkpoints (
			provider_id,
			newsgroup_id,
			last_article_number,
			backfill_article_number,
			backfill_until_date,
			backfill_cutoff_reached,
			backfill_stopped_reason,
			updated_at
		)
		VALUES ($1, $2, 0, 0, $3, $4, $5, NOW())
		ON CONFLICT (provider_id, newsgroup_id)
		DO UPDATE SET
			backfill_until_date = EXCLUDED.backfill_until_date,
			backfill_cutoff_reached = EXCLUDED.backfill_cutoff_reached,
			backfill_stopped_reason = EXCLUDED.backfill_stopped_reason,
			updated_at = NOW()`,
		providerID,
		newsgroupID,
		until,
		cutoffReached,
		strings.TrimSpace(stoppedReason),
	)
	if err != nil {
		return fmt.Errorf("set backfill checkpoint state p=%d g=%d: %w", providerID, newsgroupID, err)
	}

	return nil
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

// InsertArticleHeaders inserts header rows plus transient ingest payload side rows.
func (s *Store) InsertArticleHeaders(ctx context.Context, providerID, newsgroupID int64, headers []ArticleHeader) (int64, error) {
	if providerID <= 0 || newsgroupID <= 0 {
		return 0, fmt.Errorf("provider id and newsgroup id are required")
	}
	if len(headers) == 0 {
		return 0, nil
	}

	prepared := make([]preparedArticleHeaderInsert, 0, len(headers))
	for _, h := range headers {
		msgID := sanitizeUTF8(h.MessageID)
		if h.ArticleNumber <= 0 || msgID == "" {
			continue
		}

		subject := sanitizeUTF8(h.Subject)
		poster := sanitizeUTF8(h.Poster)
		xref := sanitizeUTF8(h.Xref)
		parsed := parseArticleIngestMetadata(subject)

		var dateUTC *time.Time
		if h.DateUTC != nil {
			normalized := h.DateUTC.UTC()
			dateUTC = &normalized
		}

		prepared = append(prepared, preparedArticleHeaderInsert{
			ArticleNumber: h.ArticleNumber,
			MessageID:     msgID,
			Subject:       subject,
			Poster:        poster,
			DateUTC:       dateUTC,
			Bytes:         h.Bytes,
			Lines:         h.Lines,
			Xref:          xref,
			Parsed:        parsed,
		})
	}
	if len(prepared) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	posterCache := make(map[string]int64, 64)

	var inserted int64
	for start := 0; start < len(prepared); start += articleHeaderInsertBatchSize {
		end := start + articleHeaderInsertBatchSize
		if end > len(prepared) {
			end = len(prepared)
		}
		batch := prepared[start:end]

		if err := ensurePostersBatch(ctx, tx, posterCache, batch); err != nil {
			return inserted, err
		}

		resolvedIDs, err := insertArticleHeadersBatch(ctx, tx, providerID, newsgroupID, batch)
		if err != nil {
			return inserted, err
		}

		payloadRows := make([]payloadUpsertRow, 0, len(batch))
		for idx, item := range batch {
			articleHeaderID := resolvedIDs[idx]
			if articleHeaderID <= 0 {
				return inserted, fmt.Errorf("insert article header %d: no article header id returned", item.ArticleNumber)
			}

			var posterID any
			payloadPoster := item.Poster
			if item.Poster != "" {
				if cachedID, ok := posterCache[item.Poster]; ok && cachedID > 0 {
					posterID = cachedID
					payloadPoster = ""
				}
			}

			payloadRows = append(payloadRows, payloadUpsertRow{
				ArticleHeaderID: articleHeaderID,
				Subject:         item.Subject,
				PosterID:        posterID,
				Poster:          payloadPoster,
				Xref:            item.Xref,
				FileName:        item.Parsed.FileName,
				FileIndex:       item.Parsed.FileIndex,
				FileTotal:       item.Parsed.FileTotal,
				YEncPart:        item.Parsed.YEncPart,
				YEncTotalParts:  item.Parsed.YEncTotalParts,
				FileSize:        item.Parsed.FileSize,
			})
		}

		if err := upsertArticleHeaderPayloadsBatch(ctx, tx, payloadRows); err != nil {
			return inserted, err
		}

		inserted += int64(len(batch))
	}

	if err := tx.Commit(); err != nil {
		return inserted, err
	}

	return inserted, nil
}

func ensurePostersBatch(ctx context.Context, tx *sql.Tx, posterCache map[string]int64, batch []preparedArticleHeaderInsert) error {
	posterNames := make([]string, 0, len(batch))
	seen := make(map[string]struct{}, len(batch))
	for _, item := range batch {
		if item.Poster == "" {
			continue
		}
		if _, ok := posterCache[item.Poster]; ok {
			continue
		}
		if _, ok := seen[item.Poster]; ok {
			continue
		}
		seen[item.Poster] = struct{}{}
		posterNames = append(posterNames, item.Poster)
	}
	if len(posterNames) == 0 {
		return nil
	}

	insertSQL := strings.Builder{}
	insertSQL.WriteString("INSERT INTO posters (poster_name) VALUES ")
	args := make([]any, 0, len(posterNames)*2)
	for idx, posterName := range posterNames {
		if idx > 0 {
			insertSQL.WriteString(",")
		}
		fmt.Fprintf(&insertSQL, "($%d::text)", len(args)+1)
		args = append(args, posterName)
	}
	insertSQL.WriteString(" ON CONFLICT (poster_name) DO NOTHING")
	if _, err := tx.ExecContext(ctx, insertSQL.String(), args...); err != nil {
		return fmt.Errorf("ensure poster batch during header insert: %w", err)
	}

	selectSQL := strings.Builder{}
	selectSQL.WriteString("SELECT id, poster_name FROM posters WHERE poster_name IN (")
	selectArgs := make([]any, 0, len(posterNames))
	for idx, posterName := range posterNames {
		if idx > 0 {
			selectSQL.WriteString(",")
		}
		fmt.Fprintf(&selectSQL, "$%d::text", len(selectArgs)+1)
		selectArgs = append(selectArgs, posterName)
	}
	selectSQL.WriteString(")")
	rows, err := tx.QueryContext(ctx, selectSQL.String(), selectArgs...)
	if err != nil {
		return fmt.Errorf("load poster ids during header insert: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id         int64
			posterName string
		)
		if err := rows.Scan(&id, &posterName); err != nil {
			return fmt.Errorf("scan poster ids during header insert: %w", err)
		}
		posterCache[posterName] = id
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate poster ids during header insert: %w", err)
	}

	for _, posterName := range posterNames {
		if posterCache[posterName] <= 0 {
			return fmt.Errorf("ensure poster %q during header insert: no poster id returned", posterName)
		}
	}

	return nil
}

func insertArticleHeadersBatch(ctx context.Context, tx *sql.Tx, providerID, newsgroupID int64, batch []preparedArticleHeaderInsert) ([]int64, error) {
	query := strings.Builder{}
	query.WriteString(`
		WITH requested (
			ord,
			provider_id,
			newsgroup_id,
			article_number,
			message_id,
			date_utc,
			bytes,
			lines
		) AS (VALUES `)

	args := make([]any, 0, len(batch)*8)
	for idx, item := range batch {
		if idx > 0 {
			query.WriteString(",")
		}
		query.WriteString("(")
		fmt.Fprintf(&query, "$%d::integer,", len(args)+1)
		args = append(args, idx)
		fmt.Fprintf(&query, "$%d::bigint,", len(args)+1)
		args = append(args, providerID)
		fmt.Fprintf(&query, "$%d::bigint,", len(args)+1)
		args = append(args, newsgroupID)
		fmt.Fprintf(&query, "$%d::bigint,", len(args)+1)
		args = append(args, item.ArticleNumber)
		fmt.Fprintf(&query, "$%d::text,", len(args)+1)
		args = append(args, item.MessageID)
		fmt.Fprintf(&query, "$%d::timestamptz,", len(args)+1)
		args = append(args, item.DateUTC)
		fmt.Fprintf(&query, "$%d::bigint,", len(args)+1)
		args = append(args, item.Bytes)
		fmt.Fprintf(&query, "$%d::integer", len(args)+1)
		args = append(args, item.Lines)
		query.WriteString(")")
	}

	query.WriteString(`
		),
		existing_candidates AS (
			SELECT
				r.ord,
				ah.id,
				CASE WHEN ah.article_number = r.article_number THEN 0 ELSE 1 END AS match_rank
			FROM requested r
			JOIN article_headers ah
			  ON ah.newsgroup_id = r.newsgroup_id
			 AND (
				ah.article_number = r.article_number
				OR ah.message_id = r.message_id
			 )
		),
		inserted AS (
			INSERT INTO article_headers (
				provider_id,
				newsgroup_id,
				article_number,
				message_id,
				date_utc,
				bytes,
				lines,
				scraped_at
			)
			SELECT
				r.provider_id,
				r.newsgroup_id,
				r.article_number,
				r.message_id,
				r.date_utc,
				r.bytes,
				r.lines,
				NOW()
			FROM requested r
			WHERE NOT EXISTS (
				SELECT 1
				FROM existing_candidates ec
				WHERE ec.ord = r.ord
			)
			ON CONFLICT DO NOTHING
			RETURNING id, newsgroup_id, article_number, message_id
		),
		inserted_candidates AS (
			SELECT
				r.ord,
				i.id,
				CASE WHEN i.article_number = r.article_number THEN 0 ELSE 1 END AS match_rank
			FROM requested r
			JOIN inserted i
			  ON i.newsgroup_id = r.newsgroup_id
			 AND (
				i.article_number = r.article_number
				OR i.message_id = r.message_id
			 )
		),
		resolved AS (
			SELECT DISTINCT ON (candidate.ord)
				candidate.ord,
				candidate.id
			FROM (
				SELECT
					ic.ord,
					ic.id,
					0 AS source_rank,
					ic.match_rank
				FROM inserted_candidates ic

				UNION ALL

				SELECT
					ec.ord,
					ec.id,
					1 AS source_rank,
					ec.match_rank
				FROM existing_candidates ec
			) AS candidate
			ORDER BY candidate.ord, candidate.source_rank, candidate.match_rank, candidate.id
		)
		SELECT
			ord,
			id
		FROM resolved
		ORDER BY ord`)

	rows, err := tx.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("insert article headers batch: %w", err)
	}
	defer rows.Close()

	resolvedIDs := make([]int64, len(batch))
	var count int
	for rows.Next() {
		var (
			ord int
			id  int64
		)
		if err := rows.Scan(&ord, &id); err != nil {
			return nil, fmt.Errorf("scan article header ids batch: %w", err)
		}
		if ord < 0 || ord >= len(batch) {
			return nil, fmt.Errorf("scan article header ids batch: invalid ord %d", ord)
		}
		resolvedIDs[ord] = id
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate article header ids batch: %w", err)
	}
	if count != len(batch) {
		return nil, fmt.Errorf("insert article headers batch: resolved %d ids for %d rows", count, len(batch))
	}

	return resolvedIDs, nil
}

func upsertArticleHeaderPayloadsBatch(ctx context.Context, tx *sql.Tx, rows []payloadUpsertRow) error {
	if len(rows) == 0 {
		return nil
	}

	lastByArticleHeaderID := make(map[int64]payloadUpsertRow, len(rows))
	order := make([]int64, 0, len(rows))
	for _, row := range rows {
		if _, seen := lastByArticleHeaderID[row.ArticleHeaderID]; !seen {
			order = append(order, row.ArticleHeaderID)
		}
		lastByArticleHeaderID[row.ArticleHeaderID] = row
	}

	query := strings.Builder{}
	query.WriteString(`
		INSERT INTO article_header_ingest_payloads (
			article_header_id,
			subject,
			poster_id,
			poster,
			xref,
			subject_file_name,
			subject_file_index,
			subject_file_total,
			yenc_part_number,
			yenc_total_parts,
			yenc_file_size,
			created_at
		)
		VALUES `)

	args := make([]any, 0, len(order)*11)
	for idx, articleHeaderID := range order {
		row := lastByArticleHeaderID[articleHeaderID]
		if idx > 0 {
			query.WriteString(",")
		}
		query.WriteString("(")
		fmt.Fprintf(&query, "$%d::bigint,", len(args)+1)
		args = append(args, row.ArticleHeaderID)
		fmt.Fprintf(&query, "$%d::text,", len(args)+1)
		args = append(args, row.Subject)
		fmt.Fprintf(&query, "$%d::bigint,", len(args)+1)
		args = append(args, row.PosterID)
		fmt.Fprintf(&query, "$%d::text,", len(args)+1)
		args = append(args, row.Poster)
		fmt.Fprintf(&query, "$%d::text,", len(args)+1)
		args = append(args, row.Xref)
		fmt.Fprintf(&query, "$%d::text,", len(args)+1)
		args = append(args, row.FileName)
		fmt.Fprintf(&query, "$%d::integer,", len(args)+1)
		args = append(args, row.FileIndex)
		fmt.Fprintf(&query, "$%d::integer,", len(args)+1)
		args = append(args, row.FileTotal)
		fmt.Fprintf(&query, "$%d::integer,", len(args)+1)
		args = append(args, row.YEncPart)
		fmt.Fprintf(&query, "$%d::integer,", len(args)+1)
		args = append(args, row.YEncTotalParts)
		fmt.Fprintf(&query, "$%d::bigint,", len(args)+1)
		args = append(args, row.FileSize)
		query.WriteString("NOW())")
	}

	query.WriteString(`
		ON CONFLICT (article_header_id) DO UPDATE
		SET subject = EXCLUDED.subject,
		    poster_id = COALESCE(EXCLUDED.poster_id, article_header_ingest_payloads.poster_id),
		    poster = EXCLUDED.poster,
		    xref = EXCLUDED.xref,
		    subject_file_name = EXCLUDED.subject_file_name,
		    subject_file_index = EXCLUDED.subject_file_index,
		    subject_file_total = EXCLUDED.subject_file_total,
		    yenc_part_number = EXCLUDED.yenc_part_number,
		    yenc_total_parts = EXCLUDED.yenc_total_parts,
		    yenc_file_size = EXCLUDED.yenc_file_size`)

	if _, err := tx.ExecContext(ctx, query.String(), args...); err != nil {
		return fmt.Errorf("insert article header payload batch: %w", err)
	}

	return nil
}

// CHANGED: seed/update PG nzb cache metadata row for a release.
func (s *Store) UpsertNZBCache(ctx context.Context, releaseID, generationStatus, hashSHA256, lastError string) error {
	return upsertNZBCacheWithRunner(ctx, s.db, releaseID, generationStatus, hashSHA256, lastError)
}

func upsertNZBCacheWithRunner(ctx context.Context, runner sqlExecQueryer, releaseID, generationStatus, hashSHA256, lastError string) error {
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

	_, err := runner.ExecContext(ctx, `
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
		if strings.EqualFold(cleanKey, "line") {
			continue
		}

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
