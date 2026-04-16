package pgindex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/segmentio/ksuid"
)

var (
	releaseTitleMultiSpaceRE = regexp.MustCompile(`\s+`)
	releaseTitleSeparatorRE  = regexp.MustCompile(`[._\-]+`)
	releaseTitleResolutionRE = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|576p|480p)\b`)
	releaseTitleVideoCodecRE = regexp.MustCompile(`(?i)\b(x265|h265|hevc|av1|x264|h264|xvid)\b`)
	releaseTitleAudioCodecRE = regexp.MustCompile(`(?i)\b(truehd|atmos|dts[- ]?hd|dts|ddp|eac3|ac3|aac|flac|mp3)\b`)
	releaseTitleSourceTagRE  = regexp.MustCompile(`(?i)\b(remux|bluray|bdrip|webrip|web[- ]?dl|hdtv|dvdrip|cam)\b`)
	releaseTitleNumericNoise = regexp.MustCompile(`^[a-f0-9]{8,}$`)
	releaseTitleLongOpaqueRE = regexp.MustCompile(`(?i)^[a-z0-9]{12,}$`)
	releaseTitleDotsRE       = regexp.MustCompile(`\.+`)
	releaseTitleYearLineRE   = regexp.MustCompile(`\b(19|20)\d{2}\b`)
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
	ProviderID        int64
	NewsgroupID       int64
	PosterID          int64
	SourceReleaseKey  string
	ReleaseFamilyKey  string
	FileFamilyKey     string
	FamilyKind        string
	BaseStem          string
	PostingBucket     string
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
	PostedAt          *time.Time
	MatchConfidence   float64
	MatchStatus       string
	GroupingEvidence  map[string]any
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
	SourceReleaseKey   string
	ReleaseFamilyKey   string
	FileFamilyKey      string
	FamilyKind         string
	BaseStem           string
	PostingBucket      string
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

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeReleaseIdentity(in *ReleaseRecord) {
	if in == nil {
		return
	}
	in.ReleaseKey = firstNonBlank(in.ReleaseKey)
	in.GroupName = firstNonBlank(in.GroupName)
	in.ReleaseFamilyKey = firstNonBlank(in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey, in.GroupName)
	in.SourceReleaseKey = firstNonBlank(in.SourceReleaseKey, in.ReleaseFamilyKey, in.ReleaseKey, in.GroupName)
}

func normalizeBinaryIdentity(in *BinaryRecord) {
	if in == nil {
		return
	}
	in.ReleaseFamilyKey = firstNonBlank(in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey)
	in.SourceReleaseKey = firstNonBlank(in.SourceReleaseKey, in.ReleaseFamilyKey, in.ReleaseKey)
	// Keep legacy release_key as a compatibility mirror of release_family_key during cutover.
	in.ReleaseKey = firstNonBlank(in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey)
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

type inspectionTitleCandidate struct {
	ReleaseTitle string
	DisplayTitle string
	Source       string
	Confidence   float64
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

// read DTOs for PG-backed NZB materialization in Milestone 7.
type CatalogReleaseFile struct {
	ID        int64
	BinaryID  int64
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
	StageName       string     `json:"stage_name"`
	Enabled         bool       `json:"enabled"`
	Paused          bool       `json:"paused"`
	IntervalSeconds int        `json:"interval_seconds"`
	BatchSize       int        `json:"batch_size"`
	Concurrency     int        `json:"concurrency"`
	BackoffSeconds  int        `json:"backoff_seconds"`
	LeaseOwner      string     `json:"lease_owner"`
	LeaseExpiresAt  *time.Time `json:"lease_expires_at,omitempty"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	LastRunID       int64      `json:"last_run_id"`
	LastSuccessAt   *time.Time `json:"last_success_at,omitempty"`
	LastError       string     `json:"last_error"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type IndexerStageRun struct {
	ID          int64           `json:"id"`
	StageName   string          `json:"stage_name"`
	TriggerKind string          `json:"trigger_kind"`
	Status      string          `json:"status"`
	ClaimedBy   string          `json:"claimed_by"`
	StartedAt   time.Time       `json:"started_at"`
	HeartbeatAt *time.Time      `json:"heartbeat_at,omitempty"`
	FinishedAt  *time.Time      `json:"finished_at,omitempty"`
	ErrorText   string          `json:"error_text"`
	MetricsJSON json.RawMessage `json:"metrics_json"`
}

type IndexerStageFinishRequest struct {
	RunID       int64
	Owner       string
	ErrorText   string
	MetricsJSON json.RawMessage
}

type IndexerOverview struct {
	ReleaseCount          int64 `json:"release_count"`
	BinaryCount           int64 `json:"binary_count"`
	FileCount             int64 `json:"file_count"`
	InspectionCount       int64 `json:"inspection_count"`
	ReadyNZBCount         int64 `json:"ready_nzb_count"`
	CompletedReleaseCount int64 `json:"completed_release_count"`
	EncryptedReleaseCount int64 `json:"encrypted_release_count"`
	PasswordKnownCount    int64 `json:"password_known_count"`
	PasswordUnknownCount  int64 `json:"password_unknown_count"`
	PAR2ReleaseCount      int64 `json:"par2_release_count"`
	NFOReleaseCount       int64 `json:"nfo_release_count"`
	MediaProbedCount      int64 `json:"media_probed_count"`
	RunningStageCount     int64 `json:"running_stage_count"`
	PausedStageCount      int64 `json:"paused_stage_count"`
	FailedRunCount        int64 `json:"failed_run_count"`
}

type IndexerReleaseSummary struct {
	ReleaseID               string     `json:"release_id"`
	GUID                    string     `json:"guid"`
	ProviderID              int64      `json:"provider_id"`
	ReleaseKey              string     `json:"release_key"`
	GroupName               string     `json:"group_name"`
	Title                   string     `json:"title"`
	SourceTitle             string     `json:"source_title"`
	DeobfuscatedTitle       string     `json:"deobfuscated_title"`
	MatchedMediaTitle       string     `json:"matched_media_title"`
	OriginalMediaTitle      string     `json:"original_media_title"`
	TMDBID                  int64      `json:"tmdb_id"`
	TVDBID                  int64      `json:"tvdb_id"`
	ExternalMediaType       string     `json:"external_media_type"`
	ExternalYear            int        `json:"external_year"`
	SeasonNumber            int        `json:"season_number"`
	EpisodeNumber           int        `json:"episode_number"`
	SeasonEpisodeSource     string     `json:"season_episode_source"`
	SeasonEpisodeConfidence float64    `json:"season_episode_confidence"`
	TitleSource             string     `json:"title_source"`
	TitleConfidence         float64    `json:"title_confidence"`
	Category                string     `json:"category"`
	Classification          string     `json:"classification"`
	Poster                  string     `json:"poster"`
	SizeBytes               int64      `json:"size_bytes"`
	PostedAt                *time.Time `json:"posted_at,omitempty"`
	FileCount               int        `json:"file_count"`
	ExpectedFileCount       int        `json:"expected_file_count"`
	ParFileCount            int        `json:"par_file_count"`
	CompletionPct           float64    `json:"completion_pct"`
	MatchConfidence         float64    `json:"match_confidence"`
	IdentityStatus          string     `json:"identity_status"`
	Passworded              bool       `json:"passworded"`
	PasswordedKnown         bool       `json:"passworded_known"`
	PasswordedUnknown       bool       `json:"passworded_unknown"`
	PasswordState           string     `json:"password_state"`
	PreferredPasswordID     int64      `json:"preferred_password_id"`
	Encrypted               bool       `json:"encrypted"`
	HasPAR2                 bool       `json:"has_par2"`
	HasNFO                  bool       `json:"has_nfo"`
	ArchiveCount            int        `json:"archive_count"`
	VideoCount              int        `json:"video_count"`
	AudioCount              int        `json:"audio_count"`
	SamplePresent           bool       `json:"sample_present"`
	AvailabilityScore       float64    `json:"availability_score"`
	AvailabilityTier        string     `json:"availability_tier"`
	MediaQualityScore       float64    `json:"media_quality_score"`
	MediaQualityTier        string     `json:"media_quality_tier"`
	IdentityConfidenceScore float64    `json:"identity_confidence_score"`
	RuntimeSeconds          int        `json:"runtime_seconds"`
	PrimaryResolution       string     `json:"primary_resolution"`
	PrimaryVideoCodec       string     `json:"primary_video_codec"`
	PrimaryAudioCodec       string     `json:"primary_audio_codec"`
	SubtitleLanguages       []string   `json:"subtitle_languages"`
	MediaTags               []string   `json:"media_tags"`
	MetadataUpdatedAt       *time.Time `json:"metadata_updated_at,omitempty"`
	NZBGenerationStatus     string     `json:"nzb_generation_status"`
}

type IndexerReleaseFileSummary struct {
	FileID          int64      `json:"file_id"`
	BinaryID        int64      `json:"binary_id"`
	FileName        string     `json:"file_name"`
	SizeBytes       int64      `json:"size_bytes"`
	FileIndex       int        `json:"file_index"`
	IsPars          bool       `json:"is_pars"`
	Subject         string     `json:"subject"`
	Poster          string     `json:"poster"`
	PostedAt        *time.Time `json:"posted_at,omitempty"`
	ArticleCount    int        `json:"article_count"`
	TotalParts      int        `json:"total_parts"`
	ObservedParts   int        `json:"observed_parts"`
	MatchConfidence float64    `json:"match_confidence"`
	MatchStatus     string     `json:"match_status"`
}

type IndexerPasswordCandidateSummary struct {
	ID                 int64      `json:"id"`
	BinaryID           int64      `json:"binary_id"`
	ArtifactID         int64      `json:"artifact_id"`
	SourceKind         string     `json:"source_kind"`
	SourceRef          string     `json:"source_ref"`
	Confidence         float64    `json:"confidence"`
	VerificationStatus string     `json:"verification_status"`
	LastVerifiedAt     *time.Time `json:"last_verified_at,omitempty"`
	LastError          string     `json:"last_error"`
}

type IndexerInspectionSummary struct {
	StageName         string          `json:"stage_name"`
	BinaryID          int64           `json:"binary_id"`
	ReleaseID         string          `json:"release_id"`
	Status            string          `json:"status"`
	ErrorText         string          `json:"error_text"`
	MaterializedBytes int64           `json:"materialized_bytes"`
	ToolProvenance    json.RawMessage `json:"tool_provenance_json"`
	Summary           json.RawMessage `json:"summary_json"`
	StartedAt         *time.Time      `json:"started_at,omitempty"`
	FinishedAt        *time.Time      `json:"finished_at,omitempty"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type IndexerBinaryInspectionArtifactSummary struct {
	StageName    string          `json:"stage_name"`
	ArtifactRole string          `json:"artifact_role"`
	ArtifactName string          `json:"artifact_name"`
	ArtifactPath string          `json:"artifact_path"`
	BytesTotal   int64           `json:"bytes_total"`
	MIMEType     string          `json:"mime_type"`
	Signature    string          `json:"signature"`
	SourceKind   string          `json:"source_kind"`
	Metadata     json.RawMessage `json:"metadata_json"`
}

type IndexerArchiveEntrySummary struct {
	EntryName         string          `json:"entry_name"`
	IsDir             bool            `json:"is_dir"`
	UncompressedBytes int64           `json:"uncompressed_bytes"`
	CompressedBytes   int64           `json:"compressed_bytes"`
	Encrypted         bool            `json:"encrypted"`
	Comment           string          `json:"comment"`
	MediaType         string          `json:"media_type"`
	Signature         string          `json:"signature"`
	Metadata          json.RawMessage `json:"metadata_json"`
}

type IndexerMediaStreamSummary struct {
	StreamIndex        int             `json:"stream_index"`
	StreamType         string          `json:"stream_type"`
	CodecName          string          `json:"codec_name"`
	CodecLongName      string          `json:"codec_long_name"`
	Profile            string          `json:"profile"`
	Width              int             `json:"width"`
	Height             int             `json:"height"`
	Channels           int             `json:"channels"`
	Language           string          `json:"language"`
	DurationSeconds    float64         `json:"duration_seconds"`
	BitRate            int64           `json:"bit_rate"`
	DefaultDisposition bool            `json:"default_disposition"`
	ForcedDisposition  bool            `json:"forced_disposition"`
	Metadata           json.RawMessage `json:"metadata_json"`
}

type IndexerTextEvidenceSummary struct {
	StageName    string          `json:"stage_name"`
	EvidenceKind string          `json:"evidence_kind"`
	TextValue    string          `json:"text_value"`
	Tokens       []string        `json:"tokens"`
	Metadata     json.RawMessage `json:"metadata_json"`
}

type IndexerPAR2SetSummary struct {
	SetName        string          `json:"set_name"`
	BaseName       string          `json:"base_name"`
	IsVolume       bool            `json:"is_volume"`
	VolumeNumber   int             `json:"volume_number"`
	RecoveryBlocks int             `json:"recovery_blocks"`
	SignatureOK    bool            `json:"signature_ok"`
	Metadata       json.RawMessage `json:"metadata_json"`
}

type IndexerBinaryPartSummary struct {
	ArticleHeaderID int64  `json:"article_header_id"`
	MessageID       string `json:"message_id"`
	PartNumber      int    `json:"part_number"`
	TotalParts      int    `json:"total_parts"`
	SegmentBytes    int64  `json:"segment_bytes"`
	FileName        string `json:"file_name"`
}

type IndexerFileArticleSummary struct {
	MessageID  string `json:"message_id"`
	Bytes      int64  `json:"bytes"`
	PartNumber int    `json:"part_number"`
}

type IndexerReleaseDetail struct {
	Release            IndexerReleaseSummary             `json:"release"`
	Newsgroups         []string                          `json:"newsgroups"`
	Files              []IndexerReleaseFileSummary       `json:"files"`
	PasswordCandidates []IndexerPasswordCandidateSummary `json:"password_candidates"`
	Inspections        []IndexerInspectionSummary        `json:"inspections"`
	PredbMatches       []IndexerPredbMatchSummary        `json:"predb_matches"`
	TMDBMatches        []IndexerExternalMatchSummary     `json:"tmdb_matches"`
	TVDBMatches        []IndexerExternalMatchSummary     `json:"tvdb_matches"`
}

type IndexerPredbMatchSummary struct {
	EntryID    int64           `json:"entry_id"`
	Title      string          `json:"title"`
	Category   string          `json:"category"`
	Source     string          `json:"source"`
	Team       string          `json:"team"`
	Genre      string          `json:"genre"`
	URL        string          `json:"url"`
	SizeKB     float64         `json:"size_kb"`
	FileCount  int             `json:"file_count"`
	PostedAt   *time.Time      `json:"posted_at,omitempty"`
	Confidence float64         `json:"confidence"`
	Chosen     bool            `json:"chosen"`
	Payload    json.RawMessage `json:"payload_json"`
}

type IndexerExternalMatchSummary struct {
	Source        string          `json:"source"`
	ExternalID    int64           `json:"external_id"`
	MediaType     string          `json:"media_type"`
	Title         string          `json:"title"`
	OriginalTitle string          `json:"original_title"`
	Year          int             `json:"year"`
	Confidence    float64         `json:"confidence"`
	Chosen        bool            `json:"chosen"`
	Payload       json.RawMessage `json:"payload_json"`
}

type IndexerBinaryDetail struct {
	BinaryID           int64                                    `json:"binary_id"`
	ReleaseID          string                                   `json:"release_id"`
	ReleaseTitle       string                                   `json:"release_title"`
	GroupName          string                                   `json:"group_name"`
	ReleaseKey         string                                   `json:"release_key"`
	ReleaseName        string                                   `json:"release_name"`
	BinaryKey          string                                   `json:"binary_key"`
	BinaryName         string                                   `json:"binary_name"`
	FileID             int64                                    `json:"file_id"`
	FileName           string                                   `json:"file_name"`
	ProviderID         int64                                    `json:"provider_id"`
	NewsgroupID        int64                                    `json:"newsgroup_id"`
	Poster             string                                   `json:"poster"`
	PostedAt           *time.Time                               `json:"posted_at,omitempty"`
	FileIndex          int                                      `json:"file_index"`
	ExpectedFileCount  int                                      `json:"expected_file_count"`
	TotalParts         int                                      `json:"total_parts"`
	ObservedParts      int                                      `json:"observed_parts"`
	TotalBytes         int64                                    `json:"total_bytes"`
	FirstArticleNumber int64                                    `json:"first_article_number"`
	LastArticleNumber  int64                                    `json:"last_article_number"`
	MatchConfidence    float64                                  `json:"match_confidence"`
	MatchStatus        string                                   `json:"match_status"`
	GroupingEvidence   json.RawMessage                          `json:"grouping_evidence_json"`
	Encrypted          bool                                     `json:"encrypted"`
	PasswordState      string                                   `json:"password_state"`
	Inspections        []IndexerInspectionSummary               `json:"inspections"`
	Artifacts          []IndexerBinaryInspectionArtifactSummary `json:"artifacts"`
	ArchiveEntries     []IndexerArchiveEntrySummary             `json:"archive_entries"`
	MediaStreams       []IndexerMediaStreamSummary              `json:"media_streams"`
	TextEvidence       []IndexerTextEvidenceSummary             `json:"text_evidence"`
	PAR2Sets           []IndexerPAR2SetSummary                  `json:"par2_sets"`
	Parts              []IndexerBinaryPartSummary               `json:"parts"`
}

type IndexerFileDetail struct {
	FileID           int64                       `json:"file_id"`
	ReleaseID        string                      `json:"release_id"`
	ReleaseTitle     string                      `json:"release_title"`
	GroupName        string                      `json:"group_name"`
	BinaryID         int64                       `json:"binary_id"`
	FileName         string                      `json:"file_name"`
	SizeBytes        int64                       `json:"size_bytes"`
	FileIndex        int                         `json:"file_index"`
	IsPars           bool                        `json:"is_pars"`
	Subject          string                      `json:"subject"`
	Poster           string                      `json:"poster"`
	PostedAt         *time.Time                  `json:"posted_at,omitempty"`
	ArticleCount     int                         `json:"article_count"`
	TotalParts       int                         `json:"total_parts"`
	ObservedParts    int                         `json:"observed_parts"`
	MatchConfidence  float64                     `json:"match_confidence"`
	MatchStatus      string                      `json:"match_status"`
	GroupingEvidence json.RawMessage             `json:"grouping_evidence_json"`
	Newsgroups       []string                    `json:"newsgroups"`
	Articles         []IndexerFileArticleSummary `json:"articles"`
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

	normalizeBinaryIdentity(&in)
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
			source_release_key,
			release_family_key,
			file_family_key,
			family_kind,
			base_stem,
			posting_bucket,
			is_auxiliary,
			is_main_payload,
			release_key,
			release_name,
			binary_key,
			binary_name,
			file_name,
			file_index,
			expected_file_count,
			total_parts,
			posted_at,
			match_confidence,
			match_status,
			grouping_evidence_json,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,NOW())
		ON CONFLICT (provider_id, newsgroup_id, binary_key) DO UPDATE
		SET poster_id = COALESCE(EXCLUDED.poster_id, binaries.poster_id),
		    source_release_key = EXCLUDED.source_release_key,
		    release_family_key = EXCLUDED.release_family_key,
		    file_family_key = EXCLUDED.file_family_key,
		    family_kind = EXCLUDED.family_kind,
		    base_stem = EXCLUDED.base_stem,
		    posting_bucket = EXCLUDED.posting_bucket,
		    is_auxiliary = EXCLUDED.is_auxiliary,
		    is_main_payload = EXCLUDED.is_main_payload,
		    release_key = EXCLUDED.release_key,
		    release_name = EXCLUDED.release_name,
		    binary_name = EXCLUDED.binary_name,
		    file_name = EXCLUDED.file_name,
		    file_index = CASE
		    	WHEN EXCLUDED.file_index > 0 THEN EXCLUDED.file_index
		    	ELSE binaries.file_index
		    END,
		    expected_file_count = GREATEST(binaries.expected_file_count, EXCLUDED.expected_file_count),
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
		strings.TrimSpace(in.SourceReleaseKey),
		strings.TrimSpace(in.ReleaseFamilyKey),
		strings.TrimSpace(in.FileFamilyKey),
		strings.TrimSpace(in.FamilyKind),
		strings.TrimSpace(in.BaseStem),
		strings.TrimSpace(in.PostingBucket),
		in.IsAuxiliary,
		in.IsMainPayload,
		in.ReleaseKey,
		strings.TrimSpace(in.ReleaseName),
		in.BinaryKey,
		strings.TrimSpace(in.BinaryName),
		strings.TrimSpace(in.FileName),
		in.FileIndex,
		in.ExpectedFileCount,
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
		    posted_at = COALESCE(agg.posted_at, b.posted_at),
		    updated_at = NOW()
		FROM (
			SELECT
				bp.binary_id,
				COUNT(*)::INTEGER AS observed_parts,
				COALESCE(SUM(bp.segment_bytes), 0)::BIGINT AS total_bytes,
				COALESCE(MIN(ah.article_number), 0)::BIGINT AS first_article_number,
				COALESCE(MAX(ah.article_number), 0)::BIGINT AS last_article_number,
				MIN(ah.date_utc) AS posted_at
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
			b.newsgroup_id,
			MAX(b.source_release_key) AS source_release_key,
			b.effective_release_family_key,
			b.effective_release_family_key,
			MAX(b.release_name) AS release_name,
			MIN(b.posted_at) AS posted_at,
			COUNT(*)::INTEGER AS binary_count,
			COALESCE(SUM(b.total_bytes), 0)::BIGINT AS total_bytes
		FROM candidate_binaries b
		LEFT JOIN (
			SELECT provider_id, release_family_key, MAX(updated_at) AS updated_at
			FROM releases
			GROUP BY provider_id, release_family_key
		) r
			ON r.provider_id = b.provider_id
			AND r.release_family_key = b.effective_release_family_key
		GROUP BY b.provider_id, b.newsgroup_id, b.effective_release_family_key, r.updated_at
		HAVING (r.updated_at IS NULL OR MAX(b.updated_at) > r.updated_at)
		   AND (
			COUNT(*) FILTER (WHERE b.is_main_payload OR NOT b.is_auxiliary) >= 2
			OR COALESCE(MAX(b.expected_file_count), 0) <= 1
		   )
		ORDER BY MIN(b.posted_at) NULLS LAST, b.effective_release_family_key
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
			b.effective_release_family_key,
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
func (s *Store) ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, releaseKey string) ([]BinarySummary, error) {
	if providerID <= 0 {
		return nil, fmt.Errorf("provider id is required")
	}
	releaseKey = strings.TrimSpace(releaseKey)
	if releaseKey == "" {
		return nil, fmt.Errorf("release key is required")
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
			b.posting_bucket,
			b.is_auxiliary,
			b.is_main_payload,
			CASE
				WHEN NULLIF(BTRIM(b.base_stem), '') IS NOT NULL
				 AND b.expected_file_count > 1
				 AND LOWER(BTRIM(b.base_stem)) = $2
				THEN LOWER(BTRIM(b.base_stem))
				ELSE b.release_family_key
			END AS effective_release_key,
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
	args := []any{providerID, releaseKey}
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
			&item.PostingBucket,
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
	if in.ReleaseKey == "" {
		return "", fmt.Errorf("release key is required")
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
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,$44,$45,$46,$47,$48,$49,'usenet_index',NOW())
		ON CONFLICT (provider_id, group_name) DO UPDATE
		SET guid = EXCLUDED.guid,
		    source_release_key = EXCLUDED.source_release_key,
		    release_family_key = EXCLUDED.release_family_key,
		    release_key = EXCLUDED.release_key,
		    title = EXCLUDED.title,
		    source_title = EXCLUDED.source_title,
		    deobfuscated_title = EXCLUDED.deobfuscated_title,
		    matched_media_title = CASE
		    	WHEN EXCLUDED.matched_media_title <> '' THEN EXCLUDED.matched_media_title
		    	ELSE releases.matched_media_title
		    END,
		    title_source = EXCLUDED.title_source,
		    title_confidence = EXCLUDED.title_confidence,
		    search_title = EXCLUDED.search_title,
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
			  AND release_family_key = $2`,
			providerID,
			releaseKey,
		)
		if err != nil {
			return fmt.Errorf("delete stale releases for provider=%d release_family_key=%q: %w", providerID, releaseKey, err)
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
		  AND release_family_key = $2
		  AND group_name NOT IN (` + strings.Join(placeholders, ",") + `)`

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete stale releases for provider=%d release_family_key=%q keep=%v: %w", providerID, releaseKey, keep, err)
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
			binary_id,
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
			&item.BinaryID,
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

func (s *Store) GetIndexerOverview(ctx context.Context) (*IndexerOverview, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM releases),
			(SELECT COUNT(*) FROM binaries),
			(SELECT COUNT(*) FROM release_files),
			(SELECT COUNT(*) FROM binary_inspections),
			(SELECT COUNT(*) FROM nzb_cache WHERE generation_status = 'ready'),
			(SELECT COUNT(*) FROM releases WHERE completion_pct >= 100),
			(SELECT COUNT(*) FROM releases WHERE encrypted = TRUE),
			(SELECT COUNT(*) FROM releases WHERE passworded_known = TRUE),
			(SELECT COUNT(*) FROM releases WHERE passworded_unknown = TRUE),
			(SELECT COUNT(*) FROM releases WHERE has_par2 = TRUE),
			(SELECT COUNT(*) FROM releases WHERE has_nfo = TRUE),
			(SELECT COUNT(*) FROM releases WHERE runtime_seconds > 0 OR primary_resolution <> '' OR primary_video_codec <> '' OR primary_audio_codec <> ''),
			(SELECT COUNT(*) FROM indexer_stage_state WHERE lease_owner <> '' AND (lease_expires_at IS NULL OR lease_expires_at > NOW())),
			(SELECT COUNT(*) FROM indexer_stage_state WHERE paused = TRUE),
			(SELECT COUNT(*) FROM indexer_stage_runs WHERE status = 'failed')`)

	var item IndexerOverview
	if err := row.Scan(
		&item.ReleaseCount,
		&item.BinaryCount,
		&item.FileCount,
		&item.InspectionCount,
		&item.ReadyNZBCount,
		&item.CompletedReleaseCount,
		&item.EncryptedReleaseCount,
		&item.PasswordKnownCount,
		&item.PasswordUnknownCount,
		&item.PAR2ReleaseCount,
		&item.NFOReleaseCount,
		&item.MediaProbedCount,
		&item.RunningStageCount,
		&item.PausedStageCount,
		&item.FailedRunCount,
	); err != nil {
		return nil, fmt.Errorf("get indexer overview: %w", err)
	}

	return &item, nil
}

func (s *Store) ListIndexerReleases(ctx context.Context, query string, limit, offset int) ([]IndexerReleaseSummary, int, error) {
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM releases r
		WHERE ($1 = '' OR r.search_title ILIKE '%' || $1 || '%')`, query,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count indexer releases: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.release_id,
			r.guid,
			r.provider_id,
			r.release_key,
			r.group_name,
			r.title,
			r.source_title,
			r.deobfuscated_title,
			r.matched_media_title,
			r.original_media_title,
			r.tmdb_id,
			r.tvdb_id,
			r.external_media_type,
			r.external_year,
			r.season_number,
			r.episode_number,
			r.season_episode_source,
			r.season_episode_confidence,
			r.title_source,
			r.title_confidence,
			r.category,
			r.classification,
			r.poster,
			r.size_bytes,
			r.posted_at,
			r.file_count,
			r.expected_file_count,
			r.par_file_count,
			r.completion_pct,
			r.match_confidence,
			r.identity_status,
			r.passworded,
			r.passworded_known,
			r.passworded_unknown,
			r.password_state,
			COALESCE(r.preferred_password_id, 0),
			r.encrypted,
			r.has_par2,
			r.has_nfo,
			r.archive_count,
			r.video_count,
			r.audio_count,
			r.sample_present,
			r.availability_score,
			r.availability_tier,
			r.media_quality_score,
			r.media_quality_tier,
			r.identity_confidence_score,
			r.runtime_seconds,
			r.primary_resolution,
			r.primary_video_codec,
			r.primary_audio_codec,
			r.subtitle_languages_json,
			r.media_tags_json,
			r.metadata_updated_at,
			COALESCE(n.generation_status, 'pending')
		FROM releases r
		LEFT JOIN nzb_cache n ON n.release_id = r.release_id
		WHERE ($1 = '' OR r.search_title ILIKE '%' || $1 || '%')
		ORDER BY r.posted_at DESC NULLS LAST, r.updated_at DESC, r.title
		LIMIT $2 OFFSET $3`,
		query,
		limit,
		offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list indexer releases: %w", err)
	}
	defer rows.Close()

	out := make([]IndexerReleaseSummary, 0, limit)
	for rows.Next() {
		item, err := scanIndexerReleaseSummary(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan indexer release summary: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate indexer releases: %w", err)
	}

	return out, total, nil
}

func (s *Store) GetIndexerReleaseDetail(ctx context.Context, releaseID string) (*IndexerReleaseDetail, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT
			r.release_id,
			r.guid,
			r.provider_id,
			r.release_key,
			r.group_name,
			r.title,
			r.source_title,
			r.deobfuscated_title,
			r.matched_media_title,
			r.original_media_title,
			r.tmdb_id,
			r.tvdb_id,
			r.external_media_type,
			r.external_year,
			r.season_number,
			r.episode_number,
			r.season_episode_source,
			r.season_episode_confidence,
			r.title_source,
			r.title_confidence,
			r.category,
			r.classification,
			r.poster,
			r.size_bytes,
			r.posted_at,
			r.file_count,
			r.expected_file_count,
			r.par_file_count,
			r.completion_pct,
			r.match_confidence,
			r.identity_status,
			r.passworded,
			r.passworded_known,
			r.passworded_unknown,
			r.password_state,
			COALESCE(r.preferred_password_id, 0),
			r.encrypted,
			r.has_par2,
			r.has_nfo,
			r.archive_count,
			r.video_count,
			r.audio_count,
			r.sample_present,
			r.availability_score,
			r.availability_tier,
			r.media_quality_score,
			r.media_quality_tier,
			r.identity_confidence_score,
			r.runtime_seconds,
			r.primary_resolution,
			r.primary_video_codec,
			r.primary_audio_codec,
			r.subtitle_languages_json,
			r.media_tags_json,
			r.metadata_updated_at,
			COALESCE(n.generation_status, 'pending')
		FROM releases r
		LEFT JOIN nzb_cache n ON n.release_id = r.release_id
		WHERE r.release_id = $1`, releaseID)

	release, err := scanIndexerReleaseSummary(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get indexer release detail %s: %w", releaseID, err)
	}

	newsgroups, err := s.ListCatalogReleaseNewsgroups(ctx, releaseID)
	if err != nil {
		return nil, err
	}

	filesRows, err := s.db.QueryContext(ctx, `
		SELECT
			rf.id,
			COALESCE(rf.binary_id, 0),
			rf.file_name,
			rf.size_bytes,
			rf.file_index,
			rf.is_pars,
			rf.subject,
			rf.poster,
			rf.posted_at,
			COUNT(rfa.id) AS article_count,
			COALESCE(b.total_parts, 0),
			COALESCE(b.observed_parts, 0),
			COALESCE(b.match_confidence, 0),
			COALESCE(b.match_status, '')
		FROM release_files rf
		LEFT JOIN release_file_articles rfa ON rfa.release_file_id = rf.id
		LEFT JOIN binaries b ON b.id = rf.binary_id
		WHERE rf.release_id = $1
		GROUP BY rf.id, rf.binary_id, rf.file_name, rf.size_bytes, rf.file_index, rf.is_pars, rf.subject, rf.poster, rf.posted_at, b.total_parts, b.observed_parts, b.match_confidence, b.match_status
		ORDER BY rf.file_index, rf.id`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list release files for %s: %w", releaseID, err)
	}
	defer filesRows.Close()

	files := make([]IndexerReleaseFileSummary, 0, 32)
	for filesRows.Next() {
		var item IndexerReleaseFileSummary
		var postedAt sql.NullTime
		if err := filesRows.Scan(
			&item.FileID,
			&item.BinaryID,
			&item.FileName,
			&item.SizeBytes,
			&item.FileIndex,
			&item.IsPars,
			&item.Subject,
			&item.Poster,
			&postedAt,
			&item.ArticleCount,
			&item.TotalParts,
			&item.ObservedParts,
			&item.MatchConfidence,
			&item.MatchStatus,
		); err != nil {
			return nil, fmt.Errorf("scan release file summary: %w", err)
		}
		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}
		files = append(files, item)
	}
	if err := filesRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release files for %s: %w", releaseID, err)
	}

	passwordCandidates, err := s.listIndexerPasswordCandidates(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	inspections, err := s.listIndexerInspectionSummaries(ctx, "release_id = $1", releaseID)
	if err != nil {
		return nil, err
	}
	predbMatches, err := s.listIndexerPredbMatches(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	tmdbMatches, err := s.listIndexerExternalMatches(ctx, "tmdb", releaseID)
	if err != nil {
		return nil, err
	}
	tvdbMatches, err := s.listIndexerExternalMatches(ctx, "tvdb", releaseID)
	if err != nil {
		return nil, err
	}

	return &IndexerReleaseDetail{
		Release:            release,
		Newsgroups:         newsgroups,
		Files:              files,
		PasswordCandidates: passwordCandidates,
		Inspections:        inspections,
		PredbMatches:       predbMatches,
		TMDBMatches:        tmdbMatches,
		TVDBMatches:        tvdbMatches,
	}, nil
}

func (s *Store) GetIndexerBinaryDetail(ctx context.Context, binaryID int64) (*IndexerBinaryDetail, error) {
	if binaryID <= 0 {
		return nil, fmt.Errorf("binary id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT
			b.id,
			COALESCE(r.release_id, ''),
			COALESCE(r.title, ''),
			COALESCE(r.group_name, ''),
			b.release_key,
			b.release_name,
			b.binary_key,
			b.binary_name,
			COALESCE(rf.id, 0),
			b.file_name,
			b.provider_id,
			b.newsgroup_id,
			COALESCE(p.poster_name, ''),
			b.posted_at,
			b.file_index,
			b.expected_file_count,
			b.total_parts,
			b.observed_parts,
			b.total_bytes,
			b.first_article_number,
			b.last_article_number,
			b.match_confidence,
			b.match_status,
			b.grouping_evidence_json,
			COALESCE(r.encrypted, FALSE),
			COALESCE(r.password_state, '')
		FROM binaries b
		LEFT JOIN posters p ON p.id = b.poster_id
		LEFT JOIN release_files rf ON rf.binary_id = b.id
		LEFT JOIN releases r ON r.release_id = rf.release_id
		WHERE b.id = $1`, binaryID)

	var item IndexerBinaryDetail
	var postedAt sql.NullTime
	var groupingJSON []byte
	if err := row.Scan(
		&item.BinaryID,
		&item.ReleaseID,
		&item.ReleaseTitle,
		&item.GroupName,
		&item.ReleaseKey,
		&item.ReleaseName,
		&item.BinaryKey,
		&item.BinaryName,
		&item.FileID,
		&item.FileName,
		&item.ProviderID,
		&item.NewsgroupID,
		&item.Poster,
		&postedAt,
		&item.FileIndex,
		&item.ExpectedFileCount,
		&item.TotalParts,
		&item.ObservedParts,
		&item.TotalBytes,
		&item.FirstArticleNumber,
		&item.LastArticleNumber,
		&item.MatchConfidence,
		&item.MatchStatus,
		&groupingJSON,
		&item.Encrypted,
		&item.PasswordState,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get indexer binary detail %d: %w", binaryID, err)
	}
	if postedAt.Valid {
		t := postedAt.Time.UTC()
		item.PostedAt = &t
	}
	item.GroupingEvidence = cloneRawJSON(groupingJSON)

	inspections, err := s.listIndexerInspectionSummaries(ctx, "binary_id = $1", binaryID)
	if err != nil {
		return nil, err
	}
	artifacts, err := s.listIndexerInspectionArtifacts(ctx, binaryID)
	if err != nil {
		return nil, err
	}
	archiveEntries, err := s.listIndexerArchiveEntries(ctx, binaryID)
	if err != nil {
		return nil, err
	}
	mediaStreams, err := s.listIndexerMediaStreams(ctx, binaryID)
	if err != nil {
		return nil, err
	}
	textEvidence, err := s.listIndexerTextEvidence(ctx, binaryID)
	if err != nil {
		return nil, err
	}
	par2Sets, err := s.listIndexerPAR2Sets(ctx, binaryID)
	if err != nil {
		return nil, err
	}
	parts, err := s.listIndexerBinaryParts(ctx, binaryID)
	if err != nil {
		return nil, err
	}

	item.Inspections = inspections
	item.Artifacts = artifacts
	item.ArchiveEntries = archiveEntries
	item.MediaStreams = mediaStreams
	item.TextEvidence = textEvidence
	item.PAR2Sets = par2Sets
	item.Parts = parts

	return &item, nil
}

func (s *Store) GetIndexerFileDetail(ctx context.Context, fileID int64) (*IndexerFileDetail, error) {
	if fileID <= 0 {
		return nil, fmt.Errorf("file id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT
			rf.id,
			rf.release_id,
			COALESCE(r.title, ''),
			COALESCE(r.group_name, ''),
			COALESCE(rf.binary_id, 0),
			rf.file_name,
			rf.size_bytes,
			rf.file_index,
			rf.is_pars,
			rf.subject,
			rf.poster,
			rf.posted_at,
			COALESCE(b.total_parts, 0),
			COALESCE(b.observed_parts, 0),
			COALESCE(b.match_confidence, 0),
			COALESCE(b.match_status, ''),
			COALESCE(b.grouping_evidence_json, '{}'::jsonb),
			(SELECT COUNT(*) FROM release_file_articles WHERE release_file_id = rf.id)
		FROM release_files rf
		JOIN releases r ON r.release_id = rf.release_id
		LEFT JOIN binaries b ON b.id = rf.binary_id
		WHERE rf.id = $1`, fileID)

	var item IndexerFileDetail
	var postedAt sql.NullTime
	var groupingJSON []byte
	if err := row.Scan(
		&item.FileID,
		&item.ReleaseID,
		&item.ReleaseTitle,
		&item.GroupName,
		&item.BinaryID,
		&item.FileName,
		&item.SizeBytes,
		&item.FileIndex,
		&item.IsPars,
		&item.Subject,
		&item.Poster,
		&postedAt,
		&item.TotalParts,
		&item.ObservedParts,
		&item.MatchConfidence,
		&item.MatchStatus,
		&groupingJSON,
		&item.ArticleCount,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get indexer file detail %d: %w", fileID, err)
	}
	if postedAt.Valid {
		t := postedAt.Time.UTC()
		item.PostedAt = &t
	}
	item.GroupingEvidence = cloneRawJSON(groupingJSON)

	newsgroups, err := s.ListCatalogReleaseNewsgroups(ctx, item.ReleaseID)
	if err != nil {
		return nil, err
	}
	item.Newsgroups = newsgroups

	articles, err := s.ListCatalogReleaseFileArticles(ctx, fileID)
	if err != nil {
		return nil, err
	}
	item.Articles = make([]IndexerFileArticleSummary, 0, len(articles))
	for _, article := range articles {
		item.Articles = append(item.Articles, IndexerFileArticleSummary{
			MessageID:  article.MessageID,
			Bytes:      article.Bytes,
			PartNumber: article.PartNumber,
		})
	}

	return &item, nil
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

func scanIndexerReleaseSummary(scanner releaseScanner) (IndexerReleaseSummary, error) {
	var (
		item              IndexerReleaseSummary
		postedAt          sql.NullTime
		metadataUpdatedAt sql.NullTime
		subtitleJSON      []byte
		mediaTagsJSON     []byte
	)

	if err := scanner.Scan(
		&item.ReleaseID,
		&item.GUID,
		&item.ProviderID,
		&item.ReleaseKey,
		&item.GroupName,
		&item.Title,
		&item.SourceTitle,
		&item.DeobfuscatedTitle,
		&item.MatchedMediaTitle,
		&item.OriginalMediaTitle,
		&item.TMDBID,
		&item.TVDBID,
		&item.ExternalMediaType,
		&item.ExternalYear,
		&item.SeasonNumber,
		&item.EpisodeNumber,
		&item.SeasonEpisodeSource,
		&item.SeasonEpisodeConfidence,
		&item.TitleSource,
		&item.TitleConfidence,
		&item.Category,
		&item.Classification,
		&item.Poster,
		&item.SizeBytes,
		&postedAt,
		&item.FileCount,
		&item.ExpectedFileCount,
		&item.ParFileCount,
		&item.CompletionPct,
		&item.MatchConfidence,
		&item.IdentityStatus,
		&item.Passworded,
		&item.PasswordedKnown,
		&item.PasswordedUnknown,
		&item.PasswordState,
		&item.PreferredPasswordID,
		&item.Encrypted,
		&item.HasPAR2,
		&item.HasNFO,
		&item.ArchiveCount,
		&item.VideoCount,
		&item.AudioCount,
		&item.SamplePresent,
		&item.AvailabilityScore,
		&item.AvailabilityTier,
		&item.MediaQualityScore,
		&item.MediaQualityTier,
		&item.IdentityConfidenceScore,
		&item.RuntimeSeconds,
		&item.PrimaryResolution,
		&item.PrimaryVideoCodec,
		&item.PrimaryAudioCodec,
		&subtitleJSON,
		&mediaTagsJSON,
		&metadataUpdatedAt,
		&item.NZBGenerationStatus,
	); err != nil {
		return IndexerReleaseSummary{}, err
	}

	if postedAt.Valid {
		t := postedAt.Time.UTC()
		item.PostedAt = &t
	}
	if metadataUpdatedAt.Valid {
		t := metadataUpdatedAt.Time.UTC()
		item.MetadataUpdatedAt = &t
	}
	item.SubtitleLanguages = decodeJSONStringSlice(subtitleJSON)
	item.MediaTags = decodeJSONStringSlice(mediaTagsJSON)

	return item, nil
}

func (s *Store) ListReleaseTitleCandidates(ctx context.Context, binaryIDs []int64) ([]ReleaseTitleCandidate, error) {
	if len(binaryIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, 0, len(binaryIDs))
	args := make([]any, 0, len(binaryIDs))
	for idx, binaryID := range binaryIDs {
		placeholders = append(placeholders, fmt.Sprintf("$%d", idx+1))
		args = append(args, binaryID)
	}
	filter := strings.Join(placeholders, ",")

	out := make([]ReleaseTitleCandidate, 0, len(binaryIDs)*3)
	appendRows := func(query string, source string, confidence float64) error {
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var item ReleaseTitleCandidate
			item.Source = source
			item.Confidence = confidence
			if err := rows.Scan(&item.BinaryID, &item.Value); err != nil {
				return err
			}
			item.Value = strings.TrimSpace(item.Value)
			if item.Value == "" {
				continue
			}
			out = append(out, item)
		}
		return rows.Err()
	}

	if err := appendRows(`
		SELECT binary_id, summary_json->>'archive_entry'
		FROM binary_inspections
		WHERE stage_name = 'inspect_media'
		  AND binary_id IN (`+filter+`)
		  AND COALESCE(summary_json->>'archive_entry', '') <> ''`,
		"archive_entry", 0.98); err != nil {
		return nil, fmt.Errorf("list inspect_media title candidates: %w", err)
	}

	if err := appendRows(`
		SELECT binary_id, entry_name
		FROM binary_archive_entries
		WHERE binary_id IN (`+filter+`)
		  AND is_dir = FALSE
		  AND (
			media_type IN ('video', 'audio')
			OR lower(entry_name) ~ '\\.(mkv|mp4|avi|ts|flac|mp3|m4a)$'
		  )`,
		"archive_entry", 0.92); err != nil {
		return nil, fmt.Errorf("list archive entry title candidates: %w", err)
	}

	if err := appendRows(`
		SELECT binary_id, text_value
		FROM binary_text_evidence
		WHERE stage_name = 'inspect_nfo'
		  AND evidence_kind = 'nfo_text'
		  AND binary_id IN (`+filter+`)
		  AND text_value <> ''`,
		"nfo", 0.84); err != nil {
		return nil, fmt.Errorf("list nfo title candidates: %w", err)
	}

	return out, nil
}

func (s *Store) listIndexerPasswordCandidates(ctx context.Context, releaseID string) ([]IndexerPasswordCandidateSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			COALESCE(binary_id, 0),
			COALESCE(artifact_id, 0),
			source_kind,
			source_ref,
			confidence,
			verification_status,
			last_verified_at,
			last_error
		FROM release_password_candidates
		WHERE release_id = $1
		ORDER BY verification_status DESC, confidence DESC, updated_at DESC, id DESC`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list password candidates for %s: %w", releaseID, err)
	}
	defer rows.Close()

	out := make([]IndexerPasswordCandidateSummary, 0, 8)
	for rows.Next() {
		var item IndexerPasswordCandidateSummary
		var lastVerifiedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.BinaryID,
			&item.ArtifactID,
			&item.SourceKind,
			&item.SourceRef,
			&item.Confidence,
			&item.VerificationStatus,
			&lastVerifiedAt,
			&item.LastError,
		); err != nil {
			return nil, fmt.Errorf("scan password candidate summary: %w", err)
		}
		if lastVerifiedAt.Valid {
			t := lastVerifiedAt.Time.UTC()
			item.LastVerifiedAt = &t
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate password candidates for %s: %w", releaseID, err)
	}

	return out, nil
}

func chooseBestInspectionTitleCandidate(sourceTitle string, candidates []ReleaseTitleCandidate) (inspectionTitleCandidate, bool) {
	best := inspectionTitleCandidate{}
	for _, candidate := range candidates {
		item, ok := normalizeInspectionTitleCandidate(candidate)
		if !ok {
			continue
		}
		if best.ReleaseTitle == "" || item.Confidence > best.Confidence || (item.Confidence == best.Confidence && inspectionTitleLooksCloserToSource(item.DisplayTitle, sourceTitle, best.DisplayTitle)) {
			best = item
		}
	}
	return best, best.ReleaseTitle != ""
}

func normalizeInspectionTitleCandidate(candidate ReleaseTitleCandidate) (inspectionTitleCandidate, bool) {
	switch strings.TrimSpace(candidate.Source) {
	case "archive_entry":
		releaseTitle, displayTitle, ok := normalizeInspectionPathTitleCandidate(candidate.Value)
		if !ok {
			return inspectionTitleCandidate{}, false
		}
		return inspectionTitleCandidate{
			ReleaseTitle: releaseTitle,
			DisplayTitle: displayTitle,
			Source:       "archive_entry",
			Confidence:   clampInspectionConfidence(candidate.Confidence),
		}, true
	case "nfo":
		releaseTitle, displayTitle, ok := extractInspectionNFOTitleCandidate(candidate.Value)
		if !ok {
			return inspectionTitleCandidate{}, false
		}
		return inspectionTitleCandidate{
			ReleaseTitle: releaseTitle,
			DisplayTitle: displayTitle,
			Source:       "nfo",
			Confidence:   clampInspectionConfidence(candidate.Confidence),
		}, true
	default:
		return inspectionTitleCandidate{}, false
	}
}

func normalizeInspectionPathTitleCandidate(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", false
	}
	clean := strings.ReplaceAll(value, "\\", "/")
	base := filepath.Base(clean)
	parent := filepath.Base(filepath.Dir(clean))
	lowerPath := strings.ToLower(clean)
	lowerBase := strings.ToLower(base)
	if strings.Contains(lowerPath, "/sample/") || strings.Contains(lowerBase, "sample") {
		return "", "", false
	}

	stem := inspectionMediaTitleStem(base)
	if stem == "" && parent != "" && parent != "." {
		stem = inspectionMediaTitleStem(parent)
	}
	if stem == "" {
		return "", "", false
	}

	title := displayReleaseTitleStyle(stem)
	if !looksReadableInspectionReleaseTitle(title) {
		return "", "", false
	}
	return releaseTitleStyle(stem), title, true
}

func inspectionMediaTitleStem(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	base := filepath.Base(value)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}

func extractInspectionNFOTitleCandidate(text string) (string, string, bool) {
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		line = strings.Trim(line, "-_=*#[]() ")
		if line == "" || len(line) > 140 {
			continue
		}
		if !looksReadableInspectionReleaseTitle(line) {
			continue
		}
		if releaseTitleResolutionRE.MatchString(line) || releaseTitleSourceTagRE.MatchString(line) || releaseTitleVideoCodecRE.MatchString(line) || strings.Contains(strings.ToLower(line), "s0") || releaseTitleYearLineRE.MatchString(line) {
			display := displayReleaseTitleStyle(line)
			return releaseTitleStyle(line), display, true
		}
	}
	return "", "", false
}

func shouldAdoptInspectionTitleCandidate(sourceTitle string, candidate inspectionTitleCandidate) bool {
	if candidate.ReleaseTitle == "" || candidate.DisplayTitle == "" {
		return false
	}
	if candidate.Confidence >= 0.82 {
		return true
	}
	return candidate.Confidence >= 0.70 && (strings.TrimSpace(sourceTitle) == "" || looksObfuscatedInspectionReleaseTitle(sourceTitle) || !looksReadableInspectionReleaseTitle(sourceTitle))
}

func inspectionTitleLooksCloserToSource(candidateTitle, sourceTitle, currentBest string) bool {
	if inspectionTitlesLookRelated(candidateTitle, sourceTitle) && !inspectionTitlesLookRelated(currentBest, sourceTitle) {
		return true
	}
	if currentBest == "" {
		return true
	}
	return len(normalizeReleaseSearchTitle(candidateTitle)) > len(normalizeReleaseSearchTitle(currentBest))
}

func looksObfuscatedInspectionReleaseTitle(title string) bool {
	normalized := normalizeReleaseSearchTitle(title)
	if normalized == "" {
		return false
	}
	condensed := strings.ReplaceAll(normalized, " ", "")
	if condensed == "" {
		return false
	}
	if releaseTitleNumericNoise.MatchString(condensed) {
		return true
	}
	parts := strings.Fields(normalized)
	if len(parts) == 1 && releaseTitleLongOpaqueRE.MatchString(parts[0]) {
		return true
	}
	hasSemanticToken := releaseTitleResolutionRE.MatchString(normalized) ||
		releaseTitleVideoCodecRE.MatchString(normalized) ||
		releaseTitleAudioCodecRE.MatchString(normalized) ||
		releaseTitleSourceTagRE.MatchString(normalized)
	return !hasSemanticToken && len(parts) <= 2 && len(parts) > 0 && releaseTitleLongOpaqueRE.MatchString(parts[0])
}

func looksReadableInspectionReleaseTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return false
	}
	if looksObfuscatedInspectionReleaseTitle(title) {
		return false
	}
	normalized := normalizeReleaseSearchTitle(title)
	if normalized == "" {
		return false
	}
	parts := strings.Fields(normalized)
	if len(parts) >= 2 {
		return true
	}
	return releaseTitleResolutionRE.MatchString(normalized) ||
		releaseTitleVideoCodecRE.MatchString(normalized) ||
		releaseTitleAudioCodecRE.MatchString(normalized) ||
		releaseTitleSourceTagRE.MatchString(normalized)
}

func inspectionTitlesLookRelated(a, b string) bool {
	a = normalizeReleaseSearchTitle(a)
	b = normalizeReleaseSearchTitle(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	aFields := strings.Fields(a)
	bFields := strings.Fields(b)
	if len(aFields) == 0 || len(bFields) == 0 {
		return false
	}
	matches := 0
	for _, left := range aFields {
		for _, right := range bFields {
			if left == right {
				matches++
				break
			}
		}
	}
	minFields := len(aFields)
	if len(bFields) < minFields {
		minFields = len(bFields)
	}
	return minFields > 0 && float64(matches)/float64(minFields) >= 0.6
}

func normalizeReleaseSearchTitle(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "_", " ")
	v = strings.ReplaceAll(v, ".", " ")
	v = strings.ReplaceAll(v, "-", " ")
	return strings.Join(strings.Fields(v), " ")
}

func displayReleaseTitleStyle(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	v = strings.ReplaceAll(v, "_", " ")
	v = strings.ReplaceAll(v, ".", " ")
	v = releaseTitleMultiSpaceRE.ReplaceAllString(v, " ")
	return strings.TrimSpace(v)
}

func releaseTitleStyle(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	v = strings.ReplaceAll(v, "\\", ".")
	v = strings.ReplaceAll(v, "/", ".")
	v = strings.ReplaceAll(v, "_", ".")
	v = strings.ReplaceAll(v, " ", ".")
	v = releaseTitleDotsRE.ReplaceAllString(v, ".")
	return strings.Trim(v, ".")
}

func clampInspectionConfidence(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

func (s *Store) listIndexerExternalMatches(ctx context.Context, source, releaseID string) ([]IndexerExternalMatchSummary, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	var query string
	switch strings.TrimSpace(source) {
	case "tmdb":
		query = `
			SELECT
				tmdb_id,
				media_type,
				title,
				original_title,
				year,
				confidence,
				chosen,
				payload_json
			FROM release_tmdb_matches
			WHERE release_id = $1
			ORDER BY chosen DESC, confidence DESC, title`
	case "tvdb":
		query = `
			SELECT
				tvdb_id,
				media_type,
				title,
				original_title,
				year,
				confidence,
				chosen,
				payload_json
			FROM release_tvdb_matches
			WHERE release_id = $1
			ORDER BY chosen DESC, confidence DESC, title`
	default:
		return nil, fmt.Errorf("unsupported external match source %q", source)
	}

	rows, err := s.db.QueryContext(ctx, query, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list %s matches for %s: %w", source, releaseID, err)
	}
	defer rows.Close()

	out := []IndexerExternalMatchSummary{}
	for rows.Next() {
		var item IndexerExternalMatchSummary
		item.Source = source
		if err := rows.Scan(
			&item.ExternalID,
			&item.MediaType,
			&item.Title,
			&item.OriginalTitle,
			&item.Year,
			&item.Confidence,
			&item.Chosen,
			&item.Payload,
		); err != nil {
			return nil, fmt.Errorf("scan %s match summary: %w", source, err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s matches for %s: %w", source, releaseID, err)
	}

	return out, nil
}

func (s *Store) listIndexerPredbMatches(ctx context.Context, releaseID string) ([]IndexerPredbMatchSummary, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			p.id,
			p.title,
			p.category,
			p.source,
			COALESCE(p.team, ''),
			COALESCE(p.genre, ''),
			COALESCE(p.url, ''),
			COALESCE(p.size_kb, 0),
			COALESCE(p.file_count, 0),
			p.posted_at,
			rpm.confidence,
			rpm.chosen,
			COALESCE(p.payload_json, '{}'::jsonb)
		FROM release_predb_matches rpm
		JOIN predb_entries p ON p.id = rpm.predb_entry_id
		WHERE rpm.release_id = $1
		ORDER BY rpm.chosen DESC, rpm.confidence DESC, p.title`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list predb matches for %s: %w", releaseID, err)
	}
	defer rows.Close()

	out := []IndexerPredbMatchSummary{}
	for rows.Next() {
		var item IndexerPredbMatchSummary
		var postedAt sql.NullTime
		if err := rows.Scan(
			&item.EntryID,
			&item.Title,
			&item.Category,
			&item.Source,
			&item.Team,
			&item.Genre,
			&item.URL,
			&item.SizeKB,
			&item.FileCount,
			&postedAt,
			&item.Confidence,
			&item.Chosen,
			&item.Payload,
		); err != nil {
			return nil, fmt.Errorf("scan predb match summary: %w", err)
		}
		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate predb matches for %s: %w", releaseID, err)
	}
	return out, nil
}

func (s *Store) listIndexerInspectionSummaries(ctx context.Context, whereClause string, args ...any) ([]IndexerInspectionSummary, error) {
	query := `
		SELECT
			stage_name,
			binary_id,
			COALESCE(release_id, ''),
			status,
			error_text,
			materialized_bytes,
			tool_provenance_json,
			summary_json,
			started_at,
			finished_at,
			updated_at
		FROM binary_inspections`
	if strings.TrimSpace(whereClause) != "" {
		query += ` WHERE ` + whereClause
	}
	query += ` ORDER BY updated_at DESC, stage_name, binary_id`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list inspection summaries: %w", err)
	}
	defer rows.Close()

	out := make([]IndexerInspectionSummary, 0, 16)
	for rows.Next() {
		var (
			item        IndexerInspectionSummary
			startedAt   sql.NullTime
			finishedAt  sql.NullTime
			toolJSON    []byte
			summaryJSON []byte
		)
		if err := rows.Scan(
			&item.StageName,
			&item.BinaryID,
			&item.ReleaseID,
			&item.Status,
			&item.ErrorText,
			&item.MaterializedBytes,
			&toolJSON,
			&summaryJSON,
			&startedAt,
			&finishedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan inspection summary: %w", err)
		}
		item.ToolProvenance = cloneRawJSON(toolJSON)
		item.Summary = cloneRawJSON(summaryJSON)
		if startedAt.Valid {
			t := startedAt.Time.UTC()
			item.StartedAt = &t
		}
		if finishedAt.Valid {
			t := finishedAt.Time.UTC()
			item.FinishedAt = &t
		}
		item.UpdatedAt = item.UpdatedAt.UTC()
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inspection summaries: %w", err)
	}

	return out, nil
}

func (s *Store) listIndexerInspectionArtifacts(ctx context.Context, binaryID int64) ([]IndexerBinaryInspectionArtifactSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			stage_name,
			artifact_role,
			artifact_name,
			artifact_path,
			bytes_total,
			mime_type,
			signature,
			source_kind,
			metadata_json
		FROM binary_inspection_artifacts
		WHERE binary_id = $1
		ORDER BY updated_at DESC, stage_name, artifact_role, artifact_name`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list inspection artifacts for binary %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]IndexerBinaryInspectionArtifactSummary, 0, 8)
	for rows.Next() {
		var item IndexerBinaryInspectionArtifactSummary
		var metadataJSON []byte
		if err := rows.Scan(
			&item.StageName,
			&item.ArtifactRole,
			&item.ArtifactName,
			&item.ArtifactPath,
			&item.BytesTotal,
			&item.MIMEType,
			&item.Signature,
			&item.SourceKind,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan inspection artifact summary: %w", err)
		}
		item.Metadata = cloneRawJSON(metadataJSON)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inspection artifacts for binary %d: %w", binaryID, err)
	}

	return out, nil
}

func (s *Store) listIndexerArchiveEntries(ctx context.Context, binaryID int64) ([]IndexerArchiveEntrySummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			entry_name,
			is_dir,
			uncompressed_bytes,
			compressed_bytes,
			encrypted,
			comment_text,
			media_type,
			signature,
			metadata_json
		FROM binary_archive_entries
		WHERE binary_id = $1
		ORDER BY entry_name`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list archive entries for binary %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]IndexerArchiveEntrySummary, 0, 16)
	for rows.Next() {
		var item IndexerArchiveEntrySummary
		var metadataJSON []byte
		if err := rows.Scan(
			&item.EntryName,
			&item.IsDir,
			&item.UncompressedBytes,
			&item.CompressedBytes,
			&item.Encrypted,
			&item.Comment,
			&item.MediaType,
			&item.Signature,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan archive entry summary: %w", err)
		}
		item.Metadata = cloneRawJSON(metadataJSON)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate archive entries for binary %d: %w", binaryID, err)
	}

	return out, nil
}

func (s *Store) listIndexerMediaStreams(ctx context.Context, binaryID int64) ([]IndexerMediaStreamSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			stream_index,
			stream_type,
			codec_name,
			codec_long_name,
			profile,
			width,
			height,
			channels,
			language,
			duration_seconds,
			bit_rate,
			default_disposition,
			forced_disposition,
			metadata_json
		FROM binary_media_streams
		WHERE binary_id = $1
		ORDER BY stream_index, stream_type`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list media streams for binary %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]IndexerMediaStreamSummary, 0, 8)
	for rows.Next() {
		var item IndexerMediaStreamSummary
		var metadataJSON []byte
		if err := rows.Scan(
			&item.StreamIndex,
			&item.StreamType,
			&item.CodecName,
			&item.CodecLongName,
			&item.Profile,
			&item.Width,
			&item.Height,
			&item.Channels,
			&item.Language,
			&item.DurationSeconds,
			&item.BitRate,
			&item.DefaultDisposition,
			&item.ForcedDisposition,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan media stream summary: %w", err)
		}
		item.Metadata = cloneRawJSON(metadataJSON)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media streams for binary %d: %w", binaryID, err)
	}

	return out, nil
}

func (s *Store) listIndexerTextEvidence(ctx context.Context, binaryID int64) ([]IndexerTextEvidenceSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			stage_name,
			evidence_kind,
			text_value,
			tokens_json,
			metadata_json
		FROM binary_text_evidence
		WHERE binary_id = $1
		ORDER BY updated_at DESC, stage_name, evidence_kind`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list text evidence for binary %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]IndexerTextEvidenceSummary, 0, 8)
	for rows.Next() {
		var item IndexerTextEvidenceSummary
		var tokensJSON []byte
		var metadataJSON []byte
		if err := rows.Scan(
			&item.StageName,
			&item.EvidenceKind,
			&item.TextValue,
			&tokensJSON,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan text evidence summary: %w", err)
		}
		item.Tokens = decodeJSONStringSlice(tokensJSON)
		item.Metadata = cloneRawJSON(metadataJSON)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate text evidence for binary %d: %w", binaryID, err)
	}

	return out, nil
}

func (s *Store) listIndexerPAR2Sets(ctx context.Context, binaryID int64) ([]IndexerPAR2SetSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			set_name,
			base_name,
			is_volume,
			volume_number,
			recovery_blocks,
			signature_ok,
			metadata_json
		FROM binary_par2_sets
		WHERE binary_id = $1
		ORDER BY set_name`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list par2 sets for binary %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]IndexerPAR2SetSummary, 0, 4)
	for rows.Next() {
		var item IndexerPAR2SetSummary
		var metadataJSON []byte
		if err := rows.Scan(
			&item.SetName,
			&item.BaseName,
			&item.IsVolume,
			&item.VolumeNumber,
			&item.RecoveryBlocks,
			&item.SignatureOK,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan par2 set summary: %w", err)
		}
		item.Metadata = cloneRawJSON(metadataJSON)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate par2 sets for binary %d: %w", binaryID, err)
	}

	return out, nil
}

func (s *Store) listIndexerBinaryParts(ctx context.Context, binaryID int64) ([]IndexerBinaryPartSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			article_header_id,
			message_id,
			part_number,
			total_parts,
			segment_bytes,
			file_name
		FROM binary_parts
		WHERE binary_id = $1
		ORDER BY part_number, id`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list binary parts for binary %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]IndexerBinaryPartSummary, 0, 128)
	for rows.Next() {
		var item IndexerBinaryPartSummary
		if err := rows.Scan(
			&item.ArticleHeaderID,
			&item.MessageID,
			&item.PartNumber,
			&item.TotalParts,
			&item.SegmentBytes,
			&item.FileName,
		); err != nil {
			return nil, fmt.Errorf("scan binary part summary: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate binary parts for binary %d: %w", binaryID, err)
	}

	return out, nil
}

func (s *Store) ListBinaryInspectionCandidates(ctx context.Context, stageName string, limit int) ([]BinaryInspectionCandidate, error) {
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return nil, fmt.Errorf("stage name is required")
	}
	if limit <= 0 {
		limit = 100
	}

	filter, err := inspectCandidateFilter(stageName)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT
			$1,
			b.id,
			r.release_id,
			r.title,
			r.source_title,
			r.deobfuscated_title,
			r.group_name,
			COALESCE(rf.file_name, b.file_name, ''),
			b.binary_name,
			b.release_name,
			COALESCE(r.poster, ''),
			b.posted_at,
			b.total_bytes,
			b.total_parts,
			b.match_confidence,
			b.updated_at,
			COALESCE(bi.status, ''),
			bi.updated_at,
			COALESCE(bi.summary_json, '{}'::jsonb),
			COALESCE(abi.summary_json, '{}'::jsonb)
		FROM binaries b
		JOIN release_files rf ON rf.binary_id = b.id
		JOIN releases r ON r.release_id = rf.release_id
		LEFT JOIN binary_inspections bi
			ON bi.stage_name = $1
			AND bi.binary_id = b.id
		LEFT JOIN binary_inspections abi
			ON abi.stage_name = 'inspect_archive'
			AND abi.binary_id = b.id
		WHERE ` + filter + `
		  AND (
			bi.id IS NULL OR
			bi.status = 'failed' OR
			b.updated_at > bi.updated_at OR
			COALESCE(bi.summary_json->>'probe_error', '') <> ''
		  )
		ORDER BY r.updated_at DESC, b.updated_at DESC, b.id
		LIMIT $2`

	rows, err := s.db.QueryContext(ctx, query, stageName, limit)
	if err != nil {
		return nil, fmt.Errorf("list binary inspection candidates %s: %w", stageName, err)
	}
	defer rows.Close()

	out := make([]BinaryInspectionCandidate, 0, limit)
	for rows.Next() {
		var item BinaryInspectionCandidate
		var postedAt sql.NullTime
		var sourceUpdatedAt time.Time
		var currentUpdatedAt sql.NullTime

		if err := rows.Scan(
			&item.StageName,
			&item.BinaryID,
			&item.ReleaseID,
			&item.ReleaseTitle,
			&item.SourceTitle,
			&item.DeobfuscatedTitle,
			&item.GroupName,
			&item.FileName,
			&item.BinaryName,
			&item.ReleaseName,
			&item.Poster,
			&postedAt,
			&item.TotalBytes,
			&item.TotalParts,
			&item.MatchConfidence,
			&sourceUpdatedAt,
			&item.CurrentStatus,
			&currentUpdatedAt,
			&item.CurrentSummaryJSON,
			&item.ArchiveSummaryJSON,
		); err != nil {
			return nil, fmt.Errorf("scan binary inspection candidate: %w", err)
		}

		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}
		t := sourceUpdatedAt.UTC()
		item.SourceUpdatedAt = &t
		if currentUpdatedAt.Valid {
			ct := currentUpdatedAt.Time.UTC()
			item.CurrentUpdatedAt = &ct
		}

		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate binary inspection candidates %s: %w", stageName, err)
	}

	return out, nil
}

func (s *Store) StartBinaryInspection(ctx context.Context, stageName string, binaryID int64, releaseID string, sourceUpdatedAt *time.Time) error {
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return fmt.Errorf("stage name is required")
	}
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	var sourceUpdated any
	if sourceUpdatedAt != nil {
		sourceUpdated = sourceUpdatedAt.UTC()
	}
	var release any
	if strings.TrimSpace(releaseID) != "" {
		release = strings.TrimSpace(releaseID)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO binary_inspections (
			stage_name,
			binary_id,
			release_id,
			status,
			started_at,
			finished_at,
			error_text,
			materialized_bytes,
			tool_provenance_json,
			summary_json,
			source_updated_at,
			updated_at
		)
		VALUES ($1,$2,$3,'running',NOW(),NULL,'',0,'{}'::jsonb,'{}'::jsonb,$4,NOW())
		ON CONFLICT (stage_name, binary_id) DO UPDATE
		SET release_id = EXCLUDED.release_id,
		    status = 'running',
		    started_at = NOW(),
		    finished_at = NULL,
		    error_text = '',
		    materialized_bytes = 0,
		    tool_provenance_json = '{}'::jsonb,
		    summary_json = '{}'::jsonb,
		    source_updated_at = EXCLUDED.source_updated_at,
		    updated_at = NOW()`,
		stageName,
		binaryID,
		release,
		sourceUpdated,
	)
	if err != nil {
		return fmt.Errorf("start binary inspection %s/%d: %w", stageName, binaryID, err)
	}

	return nil
}

func (s *Store) CompleteBinaryInspection(ctx context.Context, in BinaryInspectionRecord) error {
	return s.finishBinaryInspection(ctx, in, "completed")
}

func (s *Store) FailBinaryInspection(ctx context.Context, in BinaryInspectionRecord) error {
	return s.finishBinaryInspection(ctx, in, "failed")
}

func (s *Store) UpsertReleasePasswordCandidate(ctx context.Context, in ReleasePasswordCandidateRecord) (int64, error) {
	in.ReleaseID = strings.TrimSpace(in.ReleaseID)
	if in.ReleaseID == "" {
		return 0, fmt.Errorf("release id is required")
	}
	in.PasswordValue = strings.TrimSpace(in.PasswordValue)
	if in.PasswordValue == "" {
		return 0, fmt.Errorf("password value is required")
	}
	in.SourceKind = strings.TrimSpace(in.SourceKind)
	if in.SourceKind == "" {
		in.SourceKind = "inspect_hint"
	}
	in.SourceRef = strings.TrimSpace(in.SourceRef)
	in.VerificationStatus = strings.TrimSpace(in.VerificationStatus)
	if in.VerificationStatus == "" {
		in.VerificationStatus = "pending"
	}

	var binaryID any
	if in.BinaryID > 0 {
		binaryID = in.BinaryID
	}
	var artifactID any
	if in.ArtifactID > 0 {
		artifactID = in.ArtifactID
	}
	var verifiedAt any
	if in.LastVerifiedAt != nil {
		verifiedAt = in.LastVerifiedAt.UTC()
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO release_password_candidates (
			release_id,
			binary_id,
			artifact_id,
			password_value,
			source_kind,
			source_ref,
			confidence,
			verification_status,
			last_verified_at,
			last_error,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW())
		ON CONFLICT (release_id, password_value, source_kind, source_ref) DO UPDATE
		SET binary_id = COALESCE(EXCLUDED.binary_id, release_password_candidates.binary_id),
		    artifact_id = COALESCE(EXCLUDED.artifact_id, release_password_candidates.artifact_id),
		    confidence = GREATEST(release_password_candidates.confidence, EXCLUDED.confidence),
		    verification_status = CASE
		    	WHEN release_password_candidates.verification_status = 'verified' THEN release_password_candidates.verification_status
		    	ELSE EXCLUDED.verification_status
		    END,
		    last_verified_at = COALESCE(EXCLUDED.last_verified_at, release_password_candidates.last_verified_at),
		    last_error = EXCLUDED.last_error,
		    updated_at = NOW()
		RETURNING id`,
		in.ReleaseID,
		binaryID,
		artifactID,
		in.PasswordValue,
		in.SourceKind,
		in.SourceRef,
		in.Confidence,
		in.VerificationStatus,
		verifiedAt,
		strings.TrimSpace(in.LastError),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert release password candidate %s/%s: %w", in.ReleaseID, in.PasswordValue, err)
	}

	return id, nil
}

func (s *Store) ListPasswordVerificationCandidates(ctx context.Context, limit int) ([]PasswordVerificationCandidate, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			rpc.id,
			rpc.release_id,
			COALESCE(rpc.binary_id, 0),
			COALESCE(rpc.artifact_id, 0),
			r.title,
			r.source_title,
			r.deobfuscated_title,
			rpc.password_value,
			rpc.source_kind,
			rpc.source_ref,
			rpc.confidence,
			rpc.verification_status,
			rpc.last_error
		FROM release_password_candidates rpc
		JOIN releases r ON r.release_id = rpc.release_id
		WHERE r.encrypted = TRUE
		  AND rpc.verification_status <> 'verified'
		ORDER BY rpc.confidence DESC, rpc.updated_at DESC, rpc.id
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list password verification candidates: %w", err)
	}
	defer rows.Close()

	out := make([]PasswordVerificationCandidate, 0, limit)
	for rows.Next() {
		var item PasswordVerificationCandidate
		if err := rows.Scan(
			&item.ID,
			&item.ReleaseID,
			&item.BinaryID,
			&item.ArtifactID,
			&item.Title,
			&item.SourceTitle,
			&item.DeobfuscatedTitle,
			&item.PasswordValue,
			&item.SourceKind,
			&item.SourceRef,
			&item.Confidence,
			&item.VerificationStatus,
			&item.LastError,
		); err != nil {
			return nil, fmt.Errorf("scan password verification candidate: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate password verification candidates: %w", err)
	}

	return out, nil
}

func (s *Store) UpdateReleasePasswordCandidateStatus(ctx context.Context, candidateID int64, status string, verifiedAt *time.Time, lastError string) error {
	if candidateID <= 0 {
		return fmt.Errorf("candidate id is required")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return fmt.Errorf("status is required")
	}

	var verified any
	if verifiedAt != nil {
		verified = verifiedAt.UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE release_password_candidates
		SET verification_status = $2,
		    last_verified_at = COALESCE($3, last_verified_at),
		    last_error = $4,
		    updated_at = NOW()
		WHERE id = $1`,
		candidateID,
		status,
		verified,
		strings.TrimSpace(lastError),
	)
	if err != nil {
		return fmt.Errorf("update password candidate status %d: %w", candidateID, err)
	}

	return nil
}

func (s *Store) ApplyReleaseInspectionUpdate(ctx context.Context, in ReleaseInspectionUpdate) error {
	in.ReleaseID = strings.TrimSpace(in.ReleaseID)
	if in.ReleaseID == "" {
		return fmt.Errorf("release id is required")
	}

	adjustedAvailability, adjustedTier, err := s.deriveAdjustedAvailability(ctx, in)
	if err != nil {
		return err
	}

	subtitlesJSON, err := json.Marshal(sanitizeStringSlice(in.SubtitleLanguages))
	if err != nil {
		return fmt.Errorf("marshal subtitle languages for %s: %w", in.ReleaseID, err)
	}
	mediaTagsJSON, err := json.Marshal(sanitizeStringSlice(in.MediaTags))
	if err != nil {
		return fmt.Errorf("marshal media tags for %s: %w", in.ReleaseID, err)
	}

	var metadataUpdated any
	if in.MetadataUpdatedAt != nil {
		metadataUpdated = in.MetadataUpdatedAt.UTC()
	}
	var preferredPasswordID any
	if in.PreferredPasswordID != nil && *in.PreferredPasswordID > 0 {
		preferredPasswordID = *in.PreferredPasswordID
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE releases
		SET encrypted = COALESCE($2, encrypted),
		    has_par2 = COALESCE($3, has_par2),
		    has_nfo = COALESCE($4, has_nfo),
		    passworded = COALESCE($5, passworded),
		    passworded_known = COALESCE($6, passworded_known),
		    passworded_unknown = COALESCE($7, passworded_unknown),
		    password_state = CASE WHEN $8 <> '' THEN $8 ELSE password_state END,
		    preferred_password_id = COALESCE($9, preferred_password_id),
		    archive_count = GREATEST(archive_count, COALESCE($10, archive_count)),
		    video_count = GREATEST(video_count, COALESCE($11, video_count)),
		    audio_count = GREATEST(audio_count, COALESCE($12, audio_count)),
		    runtime_seconds = COALESCE($13, runtime_seconds),
		    sample_present = COALESCE($14, sample_present),
		    primary_resolution = CASE WHEN $15 <> '' THEN $15 ELSE primary_resolution END,
		    primary_video_codec = CASE WHEN $16 <> '' THEN $16 ELSE primary_video_codec END,
		    primary_audio_codec = CASE WHEN $17 <> '' THEN $17 ELSE primary_audio_codec END,
		    subtitle_languages_json = CASE
		    	WHEN jsonb_array_length($18::jsonb) > 0 THEN $18::jsonb
		    	ELSE subtitle_languages_json
		    END,
		    media_tags_json = CASE
		    	WHEN jsonb_array_length($19::jsonb) > 0 THEN $19::jsonb
		    	ELSE media_tags_json
		    END,
		    media_quality_score = GREATEST(media_quality_score, COALESCE($20, media_quality_score)),
		    media_quality_tier = CASE WHEN $21 <> '' THEN $21 ELSE media_quality_tier END,
		    metadata_updated_at = COALESCE($22, metadata_updated_at),
		    updated_at = NOW()
		WHERE release_id = $1`,
		in.ReleaseID,
		nullableBool(in.Encrypted),
		nullableBool(in.HasPAR2),
		nullableBool(in.HasNFO),
		nullableBool(in.Passworded),
		nullableBool(in.PasswordedKnown),
		nullableBool(in.PasswordedUnknown),
		strings.TrimSpace(in.PasswordState),
		preferredPasswordID,
		nullableInt(in.ArchiveCount),
		nullableInt(in.VideoCount),
		nullableInt(in.AudioCount),
		nullableInt(in.RuntimeSeconds),
		nullableBool(in.SamplePresent),
		strings.TrimSpace(in.PrimaryResolution),
		strings.TrimSpace(in.PrimaryVideoCodec),
		strings.TrimSpace(in.PrimaryAudioCodec),
		string(subtitlesJSON),
		string(mediaTagsJSON),
		nullableFloat64(in.MediaQualityScore),
		strings.TrimSpace(in.MediaQualityTier),
		metadataUpdated,
	)
	if err != nil {
		return fmt.Errorf("apply release inspection update %s: %w", in.ReleaseID, err)
	}

	if adjustedAvailability != nil {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE releases
			SET availability_score = $2,
			    availability_tier = $3,
			    updated_at = NOW()
			WHERE release_id = $1`,
			in.ReleaseID,
			*adjustedAvailability,
			adjustedTier,
		); err != nil {
			return fmt.Errorf("apply availability adjustment %s: %w", in.ReleaseID, err)
		}
	}

	if err := s.applyDerivedInspectionTitleUpdate(ctx, in.ReleaseID, in.MetadataUpdatedAt); err != nil {
		return err
	}

	return nil
}

func (s *Store) applyDerivedInspectionTitleUpdate(ctx context.Context, releaseID string, metadataUpdatedAt *time.Time) error {
	sourceTitle, currentTitleSource, currentTitleConfidence, binaryIDs, err := s.loadReleaseTitleInputs(ctx, releaseID)
	if err != nil {
		return err
	}
	if len(binaryIDs) == 0 {
		return nil
	}

	candidates, err := s.ListReleaseTitleCandidates(ctx, binaryIDs)
	if err != nil {
		return err
	}
	best, ok := chooseBestInspectionTitleCandidate(sourceTitle, candidates)
	if !ok {
		return nil
	}
	if !shouldAdoptInspectionTitleCandidate(sourceTitle, best) {
		return nil
	}

	displaySource := displayReleaseTitleStyle(sourceTitle)
	if normalizeReleaseSearchTitle(best.DisplayTitle) == normalizeReleaseSearchTitle(displaySource) {
		return nil
	}
	if currentTitleSource != "" && currentTitleSource != "source" && currentTitleConfidence >= best.Confidence {
		return nil
	}

	var metadataUpdated any
	if metadataUpdatedAt != nil {
		metadataUpdated = metadataUpdatedAt.UTC()
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE releases
		SET title = $2,
		    deobfuscated_title = $3,
		    title_source = $4,
		    title_confidence = $5,
		    search_title = $6,
		    metadata_updated_at = COALESCE($7, metadata_updated_at),
		    updated_at = NOW()
		WHERE release_id = $1`,
		releaseID,
		strings.TrimSpace(best.DisplayTitle),
		strings.TrimSpace(best.ReleaseTitle),
		strings.TrimSpace(best.Source),
		best.Confidence,
		normalizeReleaseSearchTitle(best.DisplayTitle),
		metadataUpdated,
	)
	if err != nil {
		return fmt.Errorf("apply derived inspection title update %s: %w", releaseID, err)
	}
	return nil
}

func (s *Store) loadReleaseTitleInputs(ctx context.Context, releaseID string) (string, string, float64, []int64, error) {
	var (
		sourceTitle       string
		currentTitleFrom  string
		currentConfidence float64
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(source_title, ''), COALESCE(title_source, ''), COALESCE(title_confidence, 0)
		FROM releases
		WHERE release_id = $1`,
		releaseID,
	).Scan(&sourceTitle, &currentTitleFrom, &currentConfidence); err != nil {
		if err == sql.ErrNoRows {
			return "", "", 0, nil, fmt.Errorf("release %s not found for title inputs", releaseID)
		}
		return "", "", 0, nil, fmt.Errorf("load release title inputs %s: %w", releaseID, err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT binary_id
		FROM release_files
		WHERE release_id = $1
		  AND binary_id IS NOT NULL
		ORDER BY binary_id`, releaseID)
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("load release file binary ids %s: %w", releaseID, err)
	}
	defer rows.Close()

	binaryIDs := make([]int64, 0, 16)
	for rows.Next() {
		var binaryID int64
		if err := rows.Scan(&binaryID); err != nil {
			return "", "", 0, nil, fmt.Errorf("scan release file binary id %s: %w", releaseID, err)
		}
		if binaryID > 0 {
			binaryIDs = append(binaryIDs, binaryID)
		}
	}
	if err := rows.Err(); err != nil {
		return "", "", 0, nil, fmt.Errorf("iterate release file binary ids %s: %w", releaseID, err)
	}

	return sourceTitle, currentTitleFrom, currentConfidence, binaryIDs, nil
}

func (s *Store) deriveAdjustedAvailability(ctx context.Context, in ReleaseInspectionUpdate) (*float64, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("pgindex store is not initialized")
	}

	var (
		completionPct     float64
		availabilityScore float64
		passwordedKnown   bool
		passwordedUnknown bool
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT completion_pct, availability_score, passworded_known, passworded_unknown
		FROM releases
		WHERE release_id = $1`,
		in.ReleaseID,
	).Scan(&completionPct, &availabilityScore, &passwordedKnown, &passwordedUnknown); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", fmt.Errorf("release %s not found", in.ReleaseID)
		}
		return nil, "", fmt.Errorf("load current availability for %s: %w", in.ReleaseID, err)
	}

	finalKnown := passwordedKnown
	if in.PasswordedKnown != nil {
		finalKnown = *in.PasswordedKnown
	}
	finalUnknown := passwordedUnknown
	if in.PasswordedUnknown != nil {
		finalUnknown = *in.PasswordedUnknown
	}
	if finalKnown {
		finalUnknown = false
	}

	switch {
	case finalUnknown:
		capped := availabilityScore
		unknownCap := completionPct * 0.6
		if unknownCap < 25 {
			unknownCap = 25
		}
		if capped > unknownCap {
			capped = unknownCap
		}
		capped = clampStoreScore(capped)
		return &capped, storeAvailabilityTier(capped), nil
	case finalKnown && availabilityScore < completionPct:
		restored := clampStoreScore(completionPct)
		return &restored, storeAvailabilityTier(restored), nil
	default:
		return nil, "", nil
	}
}

func clampStoreScore(score float64) float64 {
	switch {
	case score < 0:
		return 0
	case score > 100:
		return 100
	default:
		return score
	}
}

func storeAvailabilityTier(score float64) string {
	switch {
	case score >= 85:
		return "excellent"
	case score >= 70:
		return "good"
	case score >= 50:
		return "partial"
	default:
		return "low"
	}
}

func (s *Store) ReplaceBinaryInspectionArtifacts(ctx context.Context, stageName string, binaryID int64, rows []BinaryInspectionArtifactRecord) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return fmt.Errorf("stage name is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin artifact replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM binary_inspection_artifacts
		WHERE binary_id = $1 AND stage_name = $2`,
		binaryID,
		stageName,
	); err != nil {
		return fmt.Errorf("delete inspection artifacts %s/%d: %w", stageName, binaryID, err)
	}

	for _, row := range rows {
		metadataJSON, err := json.Marshal(sanitizeJSONMap(row.Metadata))
		if err != nil {
			return fmt.Errorf("marshal inspection artifact metadata %s/%d: %w", stageName, binaryID, err)
		}
		var releaseID any
		if strings.TrimSpace(row.ReleaseID) != "" {
			releaseID = strings.TrimSpace(row.ReleaseID)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO binary_inspection_artifacts (
				binary_id,
				release_id,
				stage_name,
				artifact_role,
				artifact_name,
				artifact_path,
				bytes_total,
				mime_type,
				signature,
				source_kind,
				metadata_json,
				updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())`,
			binaryID,
			releaseID,
			stageName,
			strings.TrimSpace(row.ArtifactRole),
			strings.TrimSpace(row.ArtifactName),
			strings.TrimSpace(row.ArtifactPath),
			row.BytesTotal,
			strings.TrimSpace(row.MIMEType),
			strings.TrimSpace(row.Signature),
			strings.TrimSpace(row.SourceKind),
			string(metadataJSON),
		); err != nil {
			return fmt.Errorf("insert inspection artifact %s/%d: %w", stageName, binaryID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit artifact replace tx: %w", err)
	}
	return nil
}

func (s *Store) ReplaceBinaryArchiveEntries(ctx context.Context, binaryID int64, rows []BinaryArchiveEntryRecord) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin archive entry replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM binary_archive_entries WHERE binary_id = $1`, binaryID); err != nil {
		return fmt.Errorf("delete archive entries %d: %w", binaryID, err)
	}

	for _, row := range rows {
		metadataJSON, err := json.Marshal(sanitizeJSONMap(row.Metadata))
		if err != nil {
			return fmt.Errorf("marshal archive entry metadata %d: %w", binaryID, err)
		}
		var releaseID any
		if strings.TrimSpace(row.ReleaseID) != "" {
			releaseID = strings.TrimSpace(row.ReleaseID)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO binary_archive_entries (
				binary_id,
				release_id,
				entry_name,
				is_dir,
				uncompressed_bytes,
				compressed_bytes,
				encrypted,
				comment_text,
				media_type,
				signature,
				metadata_json,
				updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())`,
			binaryID,
			releaseID,
			strings.TrimSpace(row.EntryName),
			row.IsDir,
			row.UncompressedBytes,
			row.CompressedBytes,
			row.Encrypted,
			strings.TrimSpace(row.Comment),
			strings.TrimSpace(row.MediaType),
			strings.TrimSpace(row.Signature),
			string(metadataJSON),
		); err != nil {
			return fmt.Errorf("insert archive entry %d/%s: %w", binaryID, row.EntryName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit archive entry replace tx: %w", err)
	}
	return nil
}

func (s *Store) ReplaceBinaryMediaStreams(ctx context.Context, binaryID int64, rows []BinaryMediaStreamRecord) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin media stream replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM binary_media_streams WHERE binary_id = $1`, binaryID); err != nil {
		return fmt.Errorf("delete media streams %d: %w", binaryID, err)
	}

	for _, row := range rows {
		metadataJSON, err := json.Marshal(sanitizeJSONMap(row.Metadata))
		if err != nil {
			return fmt.Errorf("marshal media stream metadata %d: %w", binaryID, err)
		}
		var releaseID any
		if strings.TrimSpace(row.ReleaseID) != "" {
			releaseID = strings.TrimSpace(row.ReleaseID)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO binary_media_streams (
				binary_id,
				release_id,
				stream_index,
				stream_type,
				codec_name,
				codec_long_name,
				profile,
				width,
				height,
				channels,
				language,
				duration_seconds,
				bit_rate,
				default_disposition,
				forced_disposition,
				metadata_json,
				updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,NOW())`,
			binaryID,
			releaseID,
			row.StreamIndex,
			strings.TrimSpace(row.StreamType),
			strings.TrimSpace(row.CodecName),
			strings.TrimSpace(row.CodecLongName),
			strings.TrimSpace(row.Profile),
			row.Width,
			row.Height,
			row.Channels,
			strings.TrimSpace(row.Language),
			row.DurationSeconds,
			row.BitRate,
			row.DefaultDisposition,
			row.ForcedDisposition,
			string(metadataJSON),
		); err != nil {
			return fmt.Errorf("insert media stream %d/%d: %w", binaryID, row.StreamIndex, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit media stream replace tx: %w", err)
	}
	return nil
}

func (s *Store) ReplaceBinaryTextEvidence(ctx context.Context, stageName string, binaryID int64, rows []BinaryTextEvidenceRecord) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return fmt.Errorf("stage name is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin text evidence replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM binary_text_evidence
		WHERE binary_id = $1 AND stage_name = $2`,
		binaryID,
		stageName,
	); err != nil {
		return fmt.Errorf("delete text evidence %s/%d: %w", stageName, binaryID, err)
	}

	for _, row := range rows {
		tokensJSON, err := json.Marshal(sanitizeStringSlice(row.Tokens))
		if err != nil {
			return fmt.Errorf("marshal text evidence tokens %s/%d: %w", stageName, binaryID, err)
		}
		metadataJSON, err := json.Marshal(sanitizeJSONMap(row.Metadata))
		if err != nil {
			return fmt.Errorf("marshal text evidence metadata %s/%d: %w", stageName, binaryID, err)
		}
		var releaseID any
		if strings.TrimSpace(row.ReleaseID) != "" {
			releaseID = strings.TrimSpace(row.ReleaseID)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO binary_text_evidence (
				binary_id,
				release_id,
				stage_name,
				evidence_kind,
				text_value,
				tokens_json,
				metadata_json,
				updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())`,
			binaryID,
			releaseID,
			stageName,
			strings.TrimSpace(row.EvidenceKind),
			row.TextValue,
			string(tokensJSON),
			string(metadataJSON),
		); err != nil {
			return fmt.Errorf("insert text evidence %s/%d: %w", stageName, binaryID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit text evidence replace tx: %w", err)
	}
	return nil
}

func (s *Store) ReplaceBinaryPAR2Sets(ctx context.Context, binaryID int64, rows []BinaryPAR2SetRecord) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin par2 set replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM binary_par2_sets WHERE binary_id = $1`, binaryID); err != nil {
		return fmt.Errorf("delete par2 sets %d: %w", binaryID, err)
	}

	for _, row := range rows {
		metadataJSON, err := json.Marshal(sanitizeJSONMap(row.Metadata))
		if err != nil {
			return fmt.Errorf("marshal par2 set metadata %d: %w", binaryID, err)
		}
		var releaseID any
		if strings.TrimSpace(row.ReleaseID) != "" {
			releaseID = strings.TrimSpace(row.ReleaseID)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO binary_par2_sets (
				binary_id,
				release_id,
				set_name,
				base_name,
				is_volume,
				volume_number,
				recovery_blocks,
				signature_ok,
				metadata_json,
				updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())`,
			binaryID,
			releaseID,
			strings.TrimSpace(row.SetName),
			strings.TrimSpace(row.BaseName),
			row.IsVolume,
			row.VolumeNumber,
			row.RecoveryBlocks,
			row.SignatureOK,
			string(metadataJSON),
		); err != nil {
			return fmt.Errorf("insert par2 set %d/%s: %w", binaryID, row.SetName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit par2 set replace tx: %w", err)
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

func (s *Store) finishBinaryInspection(ctx context.Context, in BinaryInspectionRecord, fallbackStatus string) error {
	in.StageName = strings.TrimSpace(in.StageName)
	if in.StageName == "" {
		return fmt.Errorf("stage name is required")
	}
	if in.BinaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = fallbackStatus
	}

	toolJSON, err := json.Marshal(sanitizeStringMap(in.ToolProvenance))
	if err != nil {
		return fmt.Errorf("marshal tool provenance for %s/%d: %w", in.StageName, in.BinaryID, err)
	}
	summaryJSON, err := json.Marshal(sanitizeStringMap(in.Summary))
	if err != nil {
		return fmt.Errorf("marshal inspection summary for %s/%d: %w", in.StageName, in.BinaryID, err)
	}

	var sourceUpdated any
	if in.SourceUpdatedAt != nil {
		sourceUpdated = in.SourceUpdatedAt.UTC()
	}
	var releaseID any
	if strings.TrimSpace(in.ReleaseID) != "" {
		releaseID = strings.TrimSpace(in.ReleaseID)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO binary_inspections (
			stage_name,
			binary_id,
			release_id,
			status,
			started_at,
			finished_at,
			error_text,
			materialized_bytes,
			tool_provenance_json,
			summary_json,
			source_updated_at,
			updated_at
		)
		VALUES ($1,$2,$3,$4,NOW(),NOW(),$5,$6,$7,$8,$9,NOW())
		ON CONFLICT (stage_name, binary_id) DO UPDATE
		SET release_id = COALESCE(EXCLUDED.release_id, binary_inspections.release_id),
		    status = EXCLUDED.status,
		    finished_at = NOW(),
		    error_text = EXCLUDED.error_text,
		    materialized_bytes = EXCLUDED.materialized_bytes,
		    tool_provenance_json = EXCLUDED.tool_provenance_json,
		    summary_json = EXCLUDED.summary_json,
		    source_updated_at = COALESCE(EXCLUDED.source_updated_at, binary_inspections.source_updated_at),
		    updated_at = NOW()`,
		in.StageName,
		in.BinaryID,
		releaseID,
		status,
		strings.TrimSpace(in.ErrorText),
		in.MaterializedBytes,
		toolJSON,
		summaryJSON,
		sourceUpdated,
	)
	if err != nil {
		return fmt.Errorf("finish binary inspection %s/%d: %w", in.StageName, in.BinaryID, err)
	}

	return nil
}

func inspectCandidateFilter(stageName string) (string, error) {
	switch stageName {
	case "inspect_par2":
		return "rf.is_pars = TRUE", nil
	case "inspect_nfo":
		return "LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.nfo'", nil
	case "inspect_archive":
		return `r.completion_pct >= 100 AND
		(r.expected_file_count <= 0 OR r.file_count >= r.expected_file_count) AND
		(
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.7z' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.001$' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.zip' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.001$' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.rar' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.part01.rar' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.part1.rar'
		)`, nil
	case "inspect_password":
		return `r.encrypted = TRUE AND
		r.completion_pct >= 100 AND
		(r.expected_file_count <= 0 OR r.file_count >= r.expected_file_count) AND
		(
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.7z' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.001$' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.zip' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.001$' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.rar' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.part01.rar' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.part1.rar'
		)`, nil
	case "inspect_media":
		return `r.completion_pct >= 100 AND
		(r.expected_file_count <= 0 OR r.file_count >= r.expected_file_count) AND
		(
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.mkv' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.mp4' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.avi' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.ts' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.flac' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.mp3' OR
			LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.m4a' OR
			(
				abi.status = 'completed' AND (
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.7z' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.001$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.zip' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.001$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.rar' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.part01.rar' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.part1.rar'
				)
			)
		)`, nil
	default:
		return "", fmt.Errorf("unsupported inspection stage %q", stageName)
	}
}

func nullableBool(v *bool) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableFloat64(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
