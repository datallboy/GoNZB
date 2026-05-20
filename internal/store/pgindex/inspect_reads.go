package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/categories/newsnab"
)

type IndexerOverview struct {
	ReleaseCount          int64 `json:"release_count"`
	BinaryCount           int64 `json:"binary_count"`
	FileCount             int64 `json:"file_count"`
	InspectionCount       int64 `json:"inspection_count"`
	ReadyNZBCount         int64 `json:"ready_nzb_count"`
	ReadyReleaseCount     int64 `json:"ready_release_count"`
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

type IndexerDashboardStat struct {
	Key                string     `json:"key"`
	Label              string     `json:"label"`
	Description        string     `json:"description"`
	Value              int64      `json:"value"`
	Available          bool       `json:"available"`
	Exact              bool       `json:"exact"`
	Capped             bool       `json:"capped"`
	UpdatedAt          *time.Time `json:"updated_at,omitempty"`
	RefreshAttemptedAt *time.Time `json:"refresh_attempted_at,omitempty"`
	LastError          string     `json:"last_error,omitempty"`
}

type IndexerDashboardStats struct {
	Items []IndexerDashboardStat `json:"items"`
	Count int                    `json:"count"`
}

type IndexerBackfillProgressItem struct {
	GroupName                   string     `json:"group_name"`
	ConfiguredCutoffDate        *time.Time `json:"configured_cutoff_date,omitempty"`
	CutoffReached               bool       `json:"cutoff_reached"`
	BackfillCursorArticleNumber int64      `json:"backfill_cursor_article_number"`
	LatestArticleNumber         int64      `json:"latest_article_number"`
	OldestScrapedArticleDate    *time.Time `json:"oldest_scraped_article_date,omitempty"`
	LatestScrapedArticleDate    *time.Time `json:"latest_scraped_article_date,omitempty"`
	ProviderCount               int        `json:"provider_count"`
	LastCheckpointUpdatedAt     *time.Time `json:"last_checkpoint_updated_at,omitempty"`
}

type IndexerBackfillProgress struct {
	Items []IndexerBackfillProgressItem `json:"items"`
	Count int                           `json:"count"`
}

type IndexerStageThroughputWindow struct {
	WindowHours      int     `json:"window_hours"`
	CompletedRuns    int     `json:"completed_runs"`
	FailedRuns       int     `json:"failed_runs"`
	ItemsProcessed   int64   `json:"items_processed"`
	ItemsPerSecond   float64 `json:"items_per_second"`
	ItemsPerMinute   float64 `json:"items_per_minute"`
	ItemsPerHour     float64 `json:"items_per_hour"`
	AvgRunDurationMS float64 `json:"avg_run_duration_ms"`
}

type IndexerStageThroughputItem struct {
	StageName string                         `json:"stage_name"`
	Label     string                         `json:"label"`
	ItemLabel string                         `json:"item_label"`
	Windows   []IndexerStageThroughputWindow `json:"windows"`
}

type IndexerStageThroughput struct {
	Items []IndexerStageThroughputItem `json:"items"`
	Count int                          `json:"count"`
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
	CategoryID              int        `json:"category_id"`
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
	Hidden                  bool       `json:"hidden"`
	PublicVisible           bool       `json:"public_visible"`
	PasswordCandidateCount  int        `json:"password_candidate_count"`
}

type AdminIndexerReleaseListParams struct {
	Query              string
	Newsgroup          string
	Limit              int
	Offset             int
	Sort               string
	CategoryID         int
	Classification     string
	ExternalMediaType  string
	IdentityStatus     string
	PasswordState      string
	MediaQualityTier   string
	Hidden             string
	PublicState        string
	Inspected          string
	Enriched           string
	Uncategorized      string
	PasswordCandidates string
	MetadataMismatch   string
	LowConfidence      string
	CompletionState    string
	HasNFO             *bool
	HasPAR2            *bool
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

func (s *Store) GetIndexerOverview(ctx context.Context) (*IndexerOverview, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM releases),
			(SELECT GREATEST(COALESCE(reltuples, 0), 0)::bigint FROM pg_class WHERE oid = 'binaries'::regclass),
			(SELECT COUNT(*) FROM release_files),
			(SELECT COUNT(*) FROM binary_inspections),
			(SELECT COUNT(*) FROM nzb_cache WHERE generation_status = 'ready'),
			(SELECT COUNT(*)
			 FROM releases r
			 LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
			 WHERE `+publicIndexerReleaseVisibilityClause("r")+`),
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
		&item.ReadyReleaseCount,
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

type indexerDashboardStatDefinition struct {
	Key         string
	Label       string
	Description string
	Exact       bool
	Limit       int64
}

const dashboardBacklogEstimateLimit = 1000

var indexerDashboardStatDefinitions = []indexerDashboardStatDefinition{
	{
		Key:         "unassembled_headers",
		Label:       "Assemble Backlog",
		Description: "Planner-estimated article headers still waiting for assemble processing.",
		Exact:       false,
	},
	{
		Key:         "pending_release_candidate_families",
		Label:       "Release Backlog",
		Description: "Dirty release families still waiting for release processing.",
		Exact:       true,
	},
	{
		Key:         "pending_yenc_recovery_binaries",
		Label:       "yEnc Recovery Backlog",
		Description: "Exact count of binaries that recover_yenc can inspect now.",
		Exact:       true,
	},
	{
		Key:         "pending_inspect_discovery_binaries",
		Label:       "Discovery Backlog",
		Description: "Bounded count of binaries inspect_discovery can claim now. Values at the cap mean there may be more work behind the current snapshot.",
		Exact:       false,
		Limit:       dashboardBacklogEstimateLimit,
	},
	{
		Key:         "pending_inspect_par2_binaries",
		Label:       "PAR2 Inspection Backlog",
		Description: "Exact count of PAR2 sets inspect_par2 can claim now.",
		Exact:       true,
	},
	{
		Key:         "pending_inspect_nfo_binaries",
		Label:       "NFO Inspection Backlog",
		Description: "Bounded count of binaries inspect_nfo can claim now. Values at the cap mean there may be more work behind the current snapshot.",
		Exact:       false,
		Limit:       dashboardBacklogEstimateLimit,
	},
	{
		Key:         "pending_inspect_archive_binaries",
		Label:       "Archive Inspection Backlog",
		Description: "Bounded count of archive families inspect_archive can claim now. Values at the cap mean there may be more work behind the current snapshot.",
		Exact:       false,
		Limit:       dashboardBacklogEstimateLimit,
	},
	{
		Key:         "pending_inspect_password_binaries",
		Label:       "Password Inspection Backlog",
		Description: "Bounded count of encrypted archive binaries inspect_password can claim now. Values at the cap mean there may be more work behind the current snapshot.",
		Exact:       false,
		Limit:       dashboardBacklogEstimateLimit,
	},
	{
		Key:         "pending_inspect_media_binaries",
		Label:       "Media Inspection Backlog",
		Description: "Bounded count of media binaries inspect_media can claim now. Values at the cap mean there may be more work behind the current snapshot.",
		Exact:       false,
		Limit:       dashboardBacklogEstimateLimit,
	},
}

func (s *Store) GetIndexerDashboardStats(ctx context.Context) (*IndexerDashboardStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT stat_key, int_value, updated_at, refresh_attempted_at, last_error
		FROM indexer_dashboard_stats
		WHERE stat_key = ANY($1)
		ORDER BY stat_key`, dashboardStatKeys())
	if err != nil {
		return nil, fmt.Errorf("get indexer dashboard stats: %w", err)
	}
	defer rows.Close()

	type statRow struct {
		value              int64
		updatedAt          sql.NullTime
		refreshAttemptedAt sql.NullTime
		lastError          sql.NullString
	}
	rowByKey := make(map[string]statRow, len(indexerDashboardStatDefinitions))
	for rows.Next() {
		var (
			key string
			row statRow
		)
		if err := rows.Scan(&key, &row.value, &row.updatedAt, &row.refreshAttemptedAt, &row.lastError); err != nil {
			return nil, fmt.Errorf("scan indexer dashboard stat: %w", err)
		}
		rowByKey[key] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexer dashboard stats: %w", err)
	}

	items := make([]IndexerDashboardStat, 0, len(indexerDashboardStatDefinitions))
	for _, def := range indexerDashboardStatDefinitions {
		row, ok := rowByKey[def.Key]
		item := IndexerDashboardStat{
			Key:         def.Key,
			Label:       def.Label,
			Description: def.Description,
			Exact:       def.Exact,
		}
		if ok {
			item.Value = row.value
			if def.Limit > 0 && row.value >= def.Limit {
				item.Capped = true
			}
			if row.updatedAt.Valid {
				ts := row.updatedAt.Time.UTC()
				item.UpdatedAt = &ts
				item.Available = true
			}
			if row.refreshAttemptedAt.Valid {
				ts := row.refreshAttemptedAt.Time.UTC()
				item.RefreshAttemptedAt = &ts
			}
			if row.lastError.Valid {
				item.LastError = strings.TrimSpace(row.lastError.String)
			}
		}
		items = append(items, item)
	}

	return &IndexerDashboardStats{
		Items: items,
		Count: len(items),
	}, nil
}

func (s *Store) GetIndexerBackfillProgress(ctx context.Context) (*IndexerBackfillProgress, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH checkpoint_rollup AS (
			SELECT
				sc.newsgroup_id,
				MAX(sc.backfill_until_date) AS configured_cutoff_date,
				BOOL_OR(sc.backfill_cutoff_reached) AS cutoff_reached,
				MIN(NULLIF(sc.backfill_article_number, 0)) AS backfill_cursor_article_number,
				MAX(sc.last_article_number) AS latest_article_number,
				COUNT(DISTINCT sc.provider_id) AS provider_count,
				MAX(sc.updated_at) AS last_checkpoint_updated_at
			FROM scrape_checkpoints sc
			GROUP BY sc.newsgroup_id
		)
		SELECT
			ng.group_name,
			cr.configured_cutoff_date,
			cr.cutoff_reached,
			COALESCE(cr.backfill_cursor_article_number, 0) AS backfill_cursor_article_number,
			COALESCE(cr.latest_article_number, 0) AS latest_article_number,
			oldest.date_utc AS oldest_scraped_article_date,
			latest.date_utc AS latest_scraped_article_date,
			cr.provider_count,
			cr.last_checkpoint_updated_at
		FROM checkpoint_rollup cr
		JOIN newsgroups ng ON ng.id = cr.newsgroup_id
		LEFT JOIN LATERAL (
			SELECT ah.date_utc
			FROM article_headers ah
			WHERE ah.newsgroup_id = cr.newsgroup_id
			  AND ah.date_utc IS NOT NULL
			ORDER BY ah.date_utc ASC
			LIMIT 1
		) oldest ON true
		LEFT JOIN LATERAL (
			SELECT ah.date_utc
			FROM article_headers ah
			WHERE ah.newsgroup_id = cr.newsgroup_id
			  AND ah.date_utc IS NOT NULL
			ORDER BY ah.date_utc DESC
			LIMIT 1
		) latest ON true
		ORDER BY
			CASE
				WHEN cr.configured_cutoff_date IS NULL THEN 1
				WHEN cr.cutoff_reached THEN 1
				ELSE 0
			END,
			ng.group_name`)
	if err != nil {
		return nil, fmt.Errorf("get indexer backfill progress: %w", err)
	}
	defer rows.Close()

	items := make([]IndexerBackfillProgressItem, 0)
	for rows.Next() {
		var (
			item                    IndexerBackfillProgressItem
			configuredCutoffDate    sql.NullTime
			oldestScrapedArticle    sql.NullTime
			latestScrapedArticle    sql.NullTime
			lastCheckpointUpdatedAt sql.NullTime
		)
		if err := rows.Scan(
			&item.GroupName,
			&configuredCutoffDate,
			&item.CutoffReached,
			&item.BackfillCursorArticleNumber,
			&item.LatestArticleNumber,
			&oldestScrapedArticle,
			&latestScrapedArticle,
			&item.ProviderCount,
			&lastCheckpointUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan indexer backfill progress: %w", err)
		}
		if configuredCutoffDate.Valid {
			ts := configuredCutoffDate.Time.UTC()
			item.ConfiguredCutoffDate = &ts
		}
		if oldestScrapedArticle.Valid {
			ts := oldestScrapedArticle.Time.UTC()
			item.OldestScrapedArticleDate = &ts
		}
		if latestScrapedArticle.Valid {
			ts := latestScrapedArticle.Time.UTC()
			item.LatestScrapedArticleDate = &ts
		}
		if lastCheckpointUpdatedAt.Valid {
			ts := lastCheckpointUpdatedAt.Time.UTC()
			item.LastCheckpointUpdatedAt = &ts
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexer backfill progress: %w", err)
	}

	return &IndexerBackfillProgress{
		Items: items,
		Count: len(items),
	}, nil
}

type stageThroughputDefinition struct {
	StageName string
	Label     string
	ItemLabel string
}

var stageThroughputDefinitions = []stageThroughputDefinition{
	{StageName: "scrape_latest", Label: "Scrape Latest", ItemLabel: "headers"},
	{StageName: "scrape_backfill", Label: "Scrape Backfill", ItemLabel: "headers"},
	{StageName: "assemble", Label: "Assemble", ItemLabel: "headers"},
	{StageName: "assemble_lane_a", Label: "Assemble Lane A", ItemLabel: "headers"},
	{StageName: "assemble_lane_b", Label: "Assemble Lane B", ItemLabel: "headers"},
	{StageName: "recover_yenc", Label: "Recover yEnc", ItemLabel: "binaries"},
	{StageName: "release", Label: "Release", ItemLabel: "families"},
	{StageName: "inspect_discovery", Label: "Inspect Discovery", ItemLabel: "binaries"},
	{StageName: "inspect_par2", Label: "Inspect PAR2", ItemLabel: "binaries"},
	{StageName: "inspect_nfo", Label: "Inspect NFO", ItemLabel: "binaries"},
	{StageName: "inspect_archive", Label: "Inspect Archive", ItemLabel: "binaries"},
	{StageName: "inspect_password", Label: "Inspect Password", ItemLabel: "binaries"},
	{StageName: "inspect_media", Label: "Inspect Media", ItemLabel: "binaries"},
	{StageName: "enrich_predb", Label: "Enrich Predb", ItemLabel: "releases"},
	{StageName: "enrich_tmdb", Label: "Enrich TMDB", ItemLabel: "releases"},
}

type stageThroughputAccumulator struct {
	completedRuns   int
	failedRuns      int
	itemsProcessed  int64
	totalDurationMS float64
}

func (s *Store) GetIndexerStageThroughput(ctx context.Context) (*IndexerStageThroughput, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT stage_name, status, started_at, finished_at, metrics_json
		FROM indexer_stage_runs
		WHERE started_at >= NOW() - INTERVAL '24 hours'
		ORDER BY started_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("get indexer stage throughput: %w", err)
	}
	defer rows.Close()

	windows := []int{1, 6, 24}
	now := time.Now().UTC()
	accumulators := make(map[string]map[int]*stageThroughputAccumulator, len(stageThroughputDefinitions))
	defByStage := make(map[string]stageThroughputDefinition, len(stageThroughputDefinitions))
	for _, def := range stageThroughputDefinitions {
		defByStage[def.StageName] = def
		accumulators[def.StageName] = make(map[int]*stageThroughputAccumulator, len(windows))
		for _, windowHours := range windows {
			accumulators[def.StageName][windowHours] = &stageThroughputAccumulator{}
		}
	}

	for rows.Next() {
		var (
			stageName  string
			status     string
			startedAt  time.Time
			finishedAt sql.NullTime
			metricsRaw []byte
		)
		if err := rows.Scan(&stageName, &status, &startedAt, &finishedAt, &metricsRaw); err != nil {
			return nil, fmt.Errorf("scan indexer stage throughput row: %w", err)
		}
		if _, ok := defByStage[stageName]; !ok {
			continue
		}
		age := now.Sub(startedAt.UTC())
		var items int64
		var durationMS float64
		if strings.EqualFold(status, "completed") {
			items = stageThroughputMetricValue(stageName, metricsRaw)
			if finishedAt.Valid {
				durationMS = finishedAt.Time.Sub(startedAt).Seconds() * 1000
			}
		}
		for _, windowHours := range windows {
			if age > time.Duration(windowHours)*time.Hour {
				continue
			}
			acc := accumulators[stageName][windowHours]
			switch strings.ToLower(strings.TrimSpace(status)) {
			case "completed":
				acc.completedRuns++
				acc.itemsProcessed += items
				if durationMS > 0 {
					acc.totalDurationMS += durationMS
				}
			case "failed":
				acc.failedRuns++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexer stage throughput rows: %w", err)
	}

	items := make([]IndexerStageThroughputItem, 0, len(stageThroughputDefinitions))
	for _, def := range stageThroughputDefinitions {
		windowsOut := make([]IndexerStageThroughputWindow, 0, len(windows))
		for _, windowHours := range windows {
			acc := accumulators[def.StageName][windowHours]
			window := IndexerStageThroughputWindow{
				WindowHours:    windowHours,
				CompletedRuns:  acc.completedRuns,
				FailedRuns:     acc.failedRuns,
				ItemsProcessed: acc.itemsProcessed,
			}
			if acc.totalDurationMS > 0 {
				window.ItemsPerSecond = float64(acc.itemsProcessed) / (acc.totalDurationMS / 1000.0)
				window.ItemsPerMinute = window.ItemsPerSecond * 60.0
				window.ItemsPerHour = window.ItemsPerMinute * 60.0
				window.AvgRunDurationMS = acc.totalDurationMS / float64(maxInt(acc.completedRuns, 1))
			}
			windowsOut = append(windowsOut, window)
		}
		items = append(items, IndexerStageThroughputItem{
			StageName: def.StageName,
			Label:     def.Label,
			ItemLabel: def.ItemLabel,
			Windows:   windowsOut,
		})
	}

	return &IndexerStageThroughput{
		Items: items,
		Count: len(items),
	}, nil
}

func stageThroughputMetricValue(stageName string, metricsRaw []byte) int64 {
	if len(metricsRaw) == 0 {
		return 0
	}
	var metrics map[string]any
	if err := json.Unmarshal(metricsRaw, &metrics); err != nil {
		return 0
	}
	for _, key := range stageThroughputMetricKeys(stageName) {
		if value, ok := metricInt64(metrics[key]); ok {
			return value
		}
	}
	return 0
}

func stageThroughputMetricKeys(stageName string) []string {
	switch stageName {
	case "scrape_latest", "scrape_backfill":
		return []string{"articles_inserted", "article_headers_seen"}
	case "assemble", "assemble_lane_a", "assemble_lane_b":
		return []string{"processed_headers"}
	case "recover_yenc":
		return []string{"recovered", "attempted", "candidates"}
	case "release":
		return []string{"candidate_families_inspected", "candidate_families"}
	case "inspect_discovery", "inspect_par2", "inspect_nfo", "inspect_archive", "inspect_password", "inspect_media":
		return []string{"processed_count", "candidate_count"}
	case "enrich_predb", "enrich_tmdb":
		return []string{"processed_count", "candidate_count"}
	default:
		return nil
	}
}

func metricInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case json.Number:
		n, err := v.Int64()
		if err == nil {
			return n, true
		}
		f, err := v.Float64()
		if err == nil {
			return int64(f), true
		}
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

func maxInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func (s *Store) RefreshIndexerDashboardStats(ctx context.Context) (*IndexerDashboardStats, error) {
	for _, def := range indexerDashboardStatDefinitions {
		now := time.Now().UTC()
		value, err := s.computeIndexerDashboardStat(ctx, def.Key)
		if err != nil {
			if persistErr := s.persistIndexerDashboardStatFailure(ctx, def.Key, now, err); persistErr != nil {
				return nil, persistErr
			}
			continue
		}
		if err := s.persistIndexerDashboardStatSuccess(ctx, def.Key, value, now); err != nil {
			return nil, err
		}
	}

	return s.GetIndexerDashboardStats(ctx)
}

func dashboardStatKeys() []string {
	keys := make([]string, 0, len(indexerDashboardStatDefinitions))
	for _, def := range indexerDashboardStatDefinitions {
		keys = append(keys, def.Key)
	}
	return keys
}

func (s *Store) computeIndexerDashboardStat(ctx context.Context, key string) (int64, error) {
	switch key {
	case "unassembled_headers":
		return s.EstimateUnassembledArticleHeaders(ctx)
	case "pending_release_candidate_families":
		return s.CountPendingReleaseCandidateFamilies(ctx)
	case "pending_yenc_recovery_binaries":
		return s.CountPendingYEncRecoveryBinaries(ctx)
	case "pending_inspect_discovery_binaries":
		return s.CountPendingBinaryInspectionBacklog(ctx, "inspect_discovery")
	case "pending_inspect_par2_binaries":
		return s.CountPendingBinaryInspectionBacklog(ctx, "inspect_par2")
	case "pending_inspect_nfo_binaries":
		return s.CountPendingBinaryInspectionBacklog(ctx, "inspect_nfo")
	case "pending_inspect_archive_binaries":
		return s.CountPendingBinaryInspectionBacklog(ctx, "inspect_archive")
	case "pending_inspect_password_binaries":
		return s.CountPendingBinaryInspectionBacklog(ctx, "inspect_password")
	case "pending_inspect_media_binaries":
		return s.CountPendingBinaryInspectionBacklog(ctx, "inspect_media")
	default:
		return 0, fmt.Errorf("unsupported indexer dashboard stat %q", key)
	}
}

func (s *Store) countTableRows(ctx context.Context, table string) (int64, error) {
	query, err := dashboardTableCountQuery(table)
	if err != nil {
		return 0, err
	}
	var count int64
	if err := s.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count rows for %s: %w", table, err)
	}
	return count, nil
}

func (s *Store) tableTotalBytes(ctx context.Context, table string) (int64, error) {
	if _, err := dashboardTableCountQuery(table); err != nil {
		return 0, err
	}
	var bytes int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(pg_total_relation_size($1::regclass), 0)`, table,
	).Scan(&bytes); err != nil {
		return 0, fmt.Errorf("table bytes for %s: %w", table, err)
	}
	return bytes, nil
}

func (s *Store) tableDeadTuples(ctx context.Context, table string) (int64, error) {
	if _, err := dashboardTableCountQuery(table); err != nil {
		return 0, err
	}
	var dead int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(n_dead_tup, 0)
		FROM pg_stat_user_tables
		WHERE relname = $1`, table,
	).Scan(&dead); err != nil {
		return 0, fmt.Errorf("dead tuples for %s: %w", table, err)
	}
	return dead, nil
}

func dashboardTableCountQuery(table string) (string, error) {
	switch table {
	case "article_header_ingest_payloads":
		return `SELECT COUNT(*) FROM article_header_ingest_payloads`, nil
	case "binary_grouping_evidence":
		return `SELECT COUNT(*) FROM binary_grouping_evidence`, nil
	case "release_family_readiness_summaries":
		return `SELECT COUNT(*) FROM release_family_readiness_summaries`, nil
	default:
		return "", fmt.Errorf("unsupported dashboard table %q", table)
	}
}

func (s *Store) persistIndexerDashboardStatSuccess(ctx context.Context, key string, value int64, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO indexer_dashboard_stats (stat_key, int_value, updated_at, refresh_attempted_at, last_error)
		VALUES ($1, $2, $3, $3, '')
		ON CONFLICT (stat_key)
		DO UPDATE SET
			int_value = EXCLUDED.int_value,
			updated_at = EXCLUDED.updated_at,
			refresh_attempted_at = EXCLUDED.refresh_attempted_at,
			last_error = ''`, key, value, now); err != nil {
		return fmt.Errorf("persist dashboard stat %s: %w", key, err)
	}
	return nil
}

func (s *Store) persistIndexerDashboardStatFailure(ctx context.Context, key string, now time.Time, cause error) error {
	msg := strings.TrimSpace(cause.Error())
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO indexer_dashboard_stats (stat_key, refresh_attempted_at, last_error)
		VALUES ($1, $2, $3)
		ON CONFLICT (stat_key)
		DO UPDATE SET
			refresh_attempted_at = EXCLUDED.refresh_attempted_at,
			last_error = EXCLUDED.last_error`, key, now, msg); err != nil {
		return fmt.Errorf("persist dashboard stat failure %s: %w", key, err)
	}
	return nil
}

func (s *Store) CountPendingReleaseCandidateFamilies(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_family_readiness_summaries
		WHERE updated_at > COALESCE(processed_at, updated_at)`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending release candidate families: %w", err)
	}
	return count, nil
}

func (s *Store) CountPendingYEncRecoveryBinaries(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM binaries b
		JOIN release_family_readiness_summaries s
		  ON s.provider_id = b.provider_id
		 AND s.newsgroup_id = b.newsgroup_id
		 AND s.key_kind = 'release_family'
		 AND s.family_key = b.release_family_key
		JOIN LATERAL (
			SELECT bp.article_header_id
			FROM binary_parts bp
			WHERE bp.binary_id = b.id
			ORDER BY bp.part_number, bp.id
			LIMIT 1
		) bp ON true
		JOIN article_header_ingest_payloads p ON p.article_header_id = bp.article_header_id
		WHERE s.readiness_bucket IN ('overgrouped_contextual', 'weak_single_binary', 'weak_obfuscated_set')
		  AND b.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
		  AND b.is_main_payload = TRUE
		  AND COALESCE(b.recovered_source, '') <> 'yenc_header'
		  AND p.subject_file_name = ''
		  AND (p.yenc_recovery_retry_after IS NULL OR p.yenc_recovery_retry_after <= NOW())`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending yenc recovery backlog: %w", err)
	}
	return count, nil
}

func (s *Store) CountPendingBinaryInspectionBacklog(ctx context.Context, stageName string) (int64, error) {
	if strings.TrimSpace(stageName) == "inspect_par2" {
		return s.CountPendingPAR2InspectionBacklog(ctx)
	}
	candidates, err := s.ListBinaryInspectionCandidates(ctx, stageName, dashboardBacklogEstimateLimit)
	if err != nil {
		return 0, err
	}
	return int64(len(candidates)), nil
}

func (s *Store) CountPendingPAR2InspectionBacklog(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM binaries b
		LEFT JOIN binary_inspections bi
			ON bi.stage_name = 'inspect_par2'
			AND bi.binary_id = b.id
		WHERE b.observed_parts > 0
		  AND (
			LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '')) LIKE '%.par2' OR
			COALESCE(b.recovered_kind, '') = 'par2' OR
			COALESCE(b.recovered_extension, '') = '.par2'
		  )
		  AND (
			bi.id IS NULL OR
			bi.status = 'failed' OR
			(
				bi.status = 'running' AND
				bi.inspection_claimed_until IS NOT NULL AND
				bi.inspection_claimed_until < NOW()
			) OR
			b.updated_at > bi.updated_at OR
			NOT EXISTS (
				SELECT 1
				FROM binary_par2_targets bpt
				WHERE bpt.binary_id = b.id
			)
		  )
		  AND (
			bi.inspection_claimed_until IS NULL OR
			bi.inspection_claimed_until < NOW()
		  )`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending inspect_par2 backlog: %w", err)
	}
	return count, nil
}

func (s *Store) CountPendingInspectMediaBinaries(ctx context.Context) (int64, error) {
	filter, err := inspectCandidateFilter("inspect_media")
	if err != nil {
		return 0, err
	}

	errorRerunPredicate := `
			COALESCE(bi.summary_json->>'probe_error', '') <> '' OR
			COALESCE(bi.summary_json->>'ffprobe_error', '') <> '' OR
			COALESCE(bi.summary_json->>'extract_error', '') <> '' OR
			COALESCE(bi.summary_json->>'archive_extract_error', '') <> ''`
	rerunPredicate := `
			bi.id IS NULL OR
			bi.status = 'failed' OR
			(
				bi.status = 'running' AND
				bi.inspection_claimed_until IS NOT NULL AND
				bi.inspection_claimed_until < NOW()
			) OR
			b.updated_at > bi.updated_at OR
			` + errorRerunPredicate + `
			OR (
				abi.updated_at IS NOT NULL AND (
					bi.id IS NULL OR
					abi.updated_at > bi.updated_at
				)
			)`

	var count int64
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT b.id)
		FROM binaries b
		JOIN release_files rf ON rf.binary_id = b.id
		JOIN releases r ON r.release_id = rf.release_id
		LEFT JOIN binary_inspections bi
			ON bi.stage_name = 'inspect_media'
			AND bi.binary_id = b.id
		LEFT JOIN binary_inspections abi
			ON abi.stage_name = 'inspect_archive'
			AND abi.binary_id = b.id
		WHERE `+filter+`
		  AND (
			`+rerunPredicate+`
		  )
		  AND (
			bi.inspection_claimed_until IS NULL OR
			bi.inspection_claimed_until < NOW()
		  )`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending inspect_media binaries: %w", err)
	}
	return count, nil
}

func normalizeAdminReleaseSort(sort string) string {
	switch strings.TrimSpace(sort) {
	case "", "posted_desc":
		return "posted_desc"
	case "posted_asc", "size_desc", "size_asc", "title_asc", "updated_desc", "quality_desc", "completion_desc":
		return sort
	default:
		return "posted_desc"
	}
}

func adminReleaseSortClause(sort string) string {
	switch normalizeAdminReleaseSort(sort) {
	case "posted_asc":
		return "r.posted_at ASC NULLS LAST, r.updated_at DESC, r.title"
	case "size_desc":
		return "r.size_bytes DESC, r.posted_at DESC NULLS LAST, r.title"
	case "size_asc":
		return "r.size_bytes ASC, r.posted_at DESC NULLS LAST, r.title"
	case "title_asc":
		return "r.title ASC, r.posted_at DESC NULLS LAST"
	case "updated_desc":
		return "r.updated_at DESC, r.posted_at DESC NULLS LAST, r.title"
	case "quality_desc":
		return "r.media_quality_score DESC, r.posted_at DESC NULLS LAST, r.title"
	case "completion_desc":
		return "r.completion_pct DESC, r.posted_at DESC NULLS LAST, r.title"
	default:
		return "r.posted_at DESC NULLS LAST, r.updated_at DESC, r.title"
	}
}

func buildAdminIndexerReleaseFilterSQL(params AdminIndexerReleaseListParams) (string, []any) {
	clauses := []string{"1=1"}
	args := make([]any, 0, 16)
	arg := 1
	add := func(clause string, values ...any) {
		clauses = append(clauses, clause)
		args = append(args, values...)
		arg += len(values)
	}

	if query := strings.TrimSpace(params.Query); query != "" {
		add(fmt.Sprintf("(r.search_title ILIKE '%%' || $%d || '%%' OR r.group_name ILIKE '%%' || $%d || '%%')", arg, arg), query)
	}
	if v := strings.TrimSpace(params.Newsgroup); v != "" {
		add(fmt.Sprintf(`EXISTS (
			SELECT 1
			FROM release_newsgroups rng
			JOIN newsgroups ng ON ng.id = rng.newsgroup_id
			WHERE rng.release_id = r.release_id
			  AND ng.group_name ILIKE '%%' || $%d || '%%'
		)`, arg), v)
	}
	if params.CategoryID > 0 {
		add(fmt.Sprintf("r.category_id = $%d", arg), params.CategoryID)
	}
	if v := strings.TrimSpace(params.Classification); v != "" {
		add(fmt.Sprintf("r.classification = $%d", arg), v)
	}
	if v := strings.TrimSpace(params.ExternalMediaType); v != "" {
		add(fmt.Sprintf("r.external_media_type = $%d", arg), v)
	}
	if v := strings.TrimSpace(params.IdentityStatus); v != "" {
		add(fmt.Sprintf("r.identity_status = $%d", arg), v)
	}
	if v := strings.TrimSpace(params.PasswordState); v != "" {
		add(fmt.Sprintf("r.password_state = $%d", arg), v)
	}
	if v := strings.TrimSpace(params.MediaQualityTier); v != "" {
		add(fmt.Sprintf("r.media_quality_tier = $%d", arg), v)
	}
	switch strings.TrimSpace(params.Hidden) {
	case "hidden":
		add("COALESCE(ro.hidden, FALSE) = TRUE")
	case "visible":
		add("COALESCE(ro.hidden, FALSE) = FALSE")
	}
	switch strings.TrimSpace(params.PublicState) {
	case "public":
		add("(" + publicIndexerReleaseVisibilityClause("r") + ")")
	case "internal_only":
		add("NOT (" + publicIndexerReleaseVisibilityClause("r") + ") AND COALESCE(ro.hidden, FALSE) = FALSE")
	case "hidden":
		add("COALESCE(ro.hidden, FALSE) = TRUE")
	}
	switch strings.TrimSpace(params.Inspected) {
	case "yes":
		add("(r.runtime_seconds > 0 OR r.primary_resolution <> '' OR r.primary_video_codec <> '' OR r.primary_audio_codec <> '' OR r.has_nfo = TRUE OR r.has_par2 = TRUE)")
	case "no":
		add("(r.runtime_seconds = 0 AND r.primary_resolution = '' AND r.primary_video_codec = '' AND r.primary_audio_codec = '' AND r.has_nfo = FALSE AND r.has_par2 = FALSE)")
	}
	switch strings.TrimSpace(params.Enriched) {
	case "yes":
		add("(r.tmdb_id > 0 OR r.tvdb_id > 0 OR r.external_media_type <> '' OR r.matched_media_title <> '')")
	case "no":
		add("(r.tmdb_id = 0 AND r.tvdb_id = 0 AND r.external_media_type = '' AND r.matched_media_title = '')")
	}
	switch strings.TrimSpace(params.Uncategorized) {
	case "yes":
		add(fmt.Sprintf("r.category_id = %d", newsnab.OtherMisc))
	case "no":
		add(fmt.Sprintf("r.category_id <> %d", newsnab.OtherMisc))
	}
	switch strings.TrimSpace(params.PasswordCandidates) {
	case "yes":
		add("EXISTS (SELECT 1 FROM release_password_candidates rpc WHERE rpc.release_id = r.release_id)")
	case "no":
		add("NOT EXISTS (SELECT 1 FROM release_password_candidates rpc WHERE rpc.release_id = r.release_id)")
	}
	switch strings.TrimSpace(params.MetadataMismatch) {
	case "yes":
		add(`(
			(r.external_media_type = 'tv' AND r.matched_media_title <> '' AND r.tvdb_id = 0)
			OR (r.external_media_type <> '' AND r.matched_media_title <> '' AND r.tmdb_id = 0 AND r.tvdb_id = 0)
		)`)
	case "no":
		add(`NOT (
			(r.external_media_type = 'tv' AND r.matched_media_title <> '' AND r.tvdb_id = 0)
			OR (r.external_media_type <> '' AND r.matched_media_title <> '' AND r.tmdb_id = 0 AND r.tvdb_id = 0)
		)`)
	}
	switch strings.TrimSpace(params.LowConfidence) {
	case "yes":
		add("COALESCE(r.identity_confidence_score, 0) < 0.80")
	case "no":
		add("COALESCE(r.identity_confidence_score, 0) >= 0.80")
	}
	switch strings.TrimSpace(params.CompletionState) {
	case "exact_100":
		add("r.completion_pct = 100")
	case "below_100":
		add("r.completion_pct < 100")
	}
	if params.HasNFO != nil {
		add(fmt.Sprintf("r.has_nfo = $%d", arg), *params.HasNFO)
	}
	if params.HasPAR2 != nil {
		add(fmt.Sprintf("r.has_par2 = $%d", arg), *params.HasPAR2)
	}

	return strings.Join(clauses, "\n  AND "), args
}

func (s *Store) ListIndexerReleases(ctx context.Context, params AdminIndexerReleaseListParams) ([]IndexerReleaseSummary, int, error) {
	params.Query = strings.TrimSpace(params.Query)
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Offset < 0 {
		params.Offset = 0
	}

	filterSQL, args := buildAdminIndexerReleaseFilterSQL(params)

	var total int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM releases r
		LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
		WHERE %s`, filterSQL), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count indexer releases: %w", err)
	}

	args = append(args, params.Limit, params.Offset)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
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
			r.category_id,
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
			COALESCE(n.generation_status, 'pending'),
			COALESCE(ro.hidden, FALSE),
			CASE WHEN `+publicIndexerReleaseVisibilityClause("r")+` THEN TRUE ELSE FALSE END,
			(SELECT COUNT(*) FROM release_password_candidates rpc WHERE rpc.release_id = r.release_id)
		FROM releases r
		LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
		LEFT JOIN nzb_cache n ON n.release_id = r.release_id
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`,
		filterSQL,
		adminReleaseSortClause(params.Sort),
		len(args)-1,
		len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list indexer releases: %w", err)
	}
	defer rows.Close()

	out := make([]IndexerReleaseSummary, 0, params.Limit)
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
			r.category_id,
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
			COALESCE(n.generation_status, 'pending'),
			COALESCE(ro.hidden, FALSE),
			CASE WHEN `+publicIndexerReleaseVisibilityClause("r")+` THEN TRUE ELSE FALSE END,
			(SELECT COUNT(*) FROM release_password_candidates rpc WHERE rpc.release_id = r.release_id)
		FROM releases r
		LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
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
			COUNT(bp.id) AS article_count,
			COALESCE(b.total_parts, 0),
			COALESCE(b.observed_parts, 0),
			COALESCE(b.match_confidence, 0),
			COALESCE(b.match_status, '')
		FROM release_files rf
		LEFT JOIN binary_parts bp ON bp.binary_id = rf.binary_id
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
			CASE
				WHEN bge.binary_id IS NOT NULL THEN COALESCE(bge.payload_json, '{}'::jsonb)
				ELSE COALESCE(b.grouping_evidence_json, '{}'::jsonb)
			END,
			COALESCE(r.encrypted, FALSE),
			COALESCE(r.password_state, '')
		FROM binaries b
		LEFT JOIN binary_grouping_evidence bge ON bge.binary_id = b.id
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
			CASE
				WHEN bge.binary_id IS NOT NULL THEN COALESCE(bge.payload_json, '{}'::jsonb)
				ELSE COALESCE(b.grouping_evidence_json, '{}'::jsonb)
			END,
			(SELECT COUNT(*) FROM binary_parts WHERE binary_id = rf.binary_id)
		FROM release_files rf
		JOIN releases r ON r.release_id = rf.release_id
		LEFT JOIN binaries b ON b.id = rf.binary_id
		LEFT JOIN binary_grouping_evidence bge ON bge.binary_id = b.id
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
		&item.CategoryID,
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
		&item.Hidden,
		&item.PublicVisible,
		&item.PasswordCandidateCount,
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

func ParseAdminCategoryID(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}
