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
	ArchivedNZBCount      int64 `json:"archived_nzb_count"`
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
	WindowHours        int     `json:"window_hours"`
	CompletedRuns      int     `json:"completed_runs"`
	FailedRuns         int     `json:"failed_runs"`
	ItemsProcessed     int64   `json:"items_processed"`
	ItemsPerSecond     float64 `json:"items_per_second"`
	ItemsPerMinute     float64 `json:"items_per_minute"`
	ItemsPerHour       float64 `json:"items_per_hour"`
	AvgRunDurationMS   float64 `json:"avg_run_duration_ms"`
	AvgWorkersUsed     float64 `json:"avg_workers_used,omitempty"`
	MaxWorkersUsed     int     `json:"max_workers_used,omitempty"`
	AvgGroupsScheduled float64 `json:"avg_groups_scheduled,omitempty"`
	MaxGroupsScheduled int     `json:"max_groups_scheduled,omitempty"`
	AvgRangesFetched   float64 `json:"avg_ranges_fetched,omitempty"`
	MaxRangesFetched   int     `json:"max_ranges_fetched,omitempty"`
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
	ReleaseID                string     `json:"release_id"`
	GUID                     string     `json:"guid"`
	ProviderID               int64      `json:"provider_id"`
	ReleaseKey               string     `json:"release_key"`
	GroupName                string     `json:"group_name"`
	Title                    string     `json:"title"`
	SourceTitle              string     `json:"source_title"`
	DeobfuscatedTitle        string     `json:"deobfuscated_title"`
	MatchedMediaTitle        string     `json:"matched_media_title"`
	OriginalMediaTitle       string     `json:"original_media_title"`
	TMDBID                   int64      `json:"tmdb_id"`
	TVDBID                   int64      `json:"tvdb_id"`
	ExternalMediaType        string     `json:"external_media_type"`
	ExternalYear             int        `json:"external_year"`
	SeasonNumber             int        `json:"season_number"`
	EpisodeNumber            int        `json:"episode_number"`
	SeasonEpisodeSource      string     `json:"season_episode_source"`
	SeasonEpisodeConfidence  float64    `json:"season_episode_confidence"`
	TitleSource              string     `json:"title_source"`
	TitleConfidence          float64    `json:"title_confidence"`
	CategoryID               int        `json:"category_id"`
	Category                 string     `json:"category"`
	Classification           string     `json:"classification"`
	Poster                   string     `json:"poster"`
	SizeBytes                int64      `json:"size_bytes"`
	PostedAt                 *time.Time `json:"posted_at,omitempty"`
	FileCount                int        `json:"file_count"`
	ExpectedFileCount        int        `json:"expected_file_count"`
	ExpectedArchiveFileCount int        `json:"expected_archive_file_count"`
	ParFileCount             int        `json:"par_file_count"`
	CompletionPct            float64    `json:"completion_pct"`
	MatchConfidence          float64    `json:"match_confidence"`
	IdentityStatus           string     `json:"identity_status"`
	Passworded               bool       `json:"passworded"`
	PasswordedKnown          bool       `json:"passworded_known"`
	PasswordedUnknown        bool       `json:"passworded_unknown"`
	PasswordState            string     `json:"password_state"`
	PreferredPasswordID      int64      `json:"preferred_password_id"`
	Encrypted                bool       `json:"encrypted"`
	HasPAR2                  bool       `json:"has_par2"`
	HasNFO                   bool       `json:"has_nfo"`
	ArchiveCount             int        `json:"archive_count"`
	VideoCount               int        `json:"video_count"`
	AudioCount               int        `json:"audio_count"`
	SamplePresent            bool       `json:"sample_present"`
	AvailabilityScore        float64    `json:"availability_score"`
	AvailabilityTier         string     `json:"availability_tier"`
	MediaQualityScore        float64    `json:"media_quality_score"`
	MediaQualityTier         string     `json:"media_quality_tier"`
	IdentityConfidenceScore  float64    `json:"identity_confidence_score"`
	RuntimeSeconds           int        `json:"runtime_seconds"`
	PrimaryResolution        string     `json:"primary_resolution"`
	PrimaryVideoCodec        string     `json:"primary_video_codec"`
	PrimaryAudioCodec        string     `json:"primary_audio_codec"`
	SubtitleLanguages        []string   `json:"subtitle_languages"`
	MediaTags                []string   `json:"media_tags"`
	MetadataUpdatedAt        *time.Time `json:"metadata_updated_at,omitempty"`
	NZBGenerationStatus      string     `json:"nzb_generation_status"`
	Hidden                   bool       `json:"hidden"`
	PublicVisible            bool       `json:"public_visible"`
	PasswordCandidateCount   int        `json:"password_candidate_count"`
	PayloadCompletionState   string     `json:"payload_completion_state"`
}

type AdminIndexerReleaseListParams struct {
	Query                    string
	Newsgroup                string
	Limit                    int
	Offset                   int
	Sort                     string
	CategoryID               int
	Classification           string
	ExternalMediaType        string
	IdentityStatus           string
	PasswordState            string
	MediaQualityTier         string
	Hidden                   string
	PublicState              string
	Inspected                string
	Enriched                 string
	Uncategorized            string
	PasswordCandidates       string
	MetadataMismatch         string
	LowConfidence            string
	CompletionState          string
	PayloadCompletionInclude string
	PayloadCompletionExclude string
	HasNFO                   *bool
	HasPAR2                  *bool
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
	Diagnostics        ReleaseDetailDiagnostics          `json:"diagnostics"`
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
			(SELECT GREATEST(COALESCE(reltuples, 0), 0)::bigint FROM pg_class WHERE oid = 'binary_core'::regclass),
			(SELECT COUNT(*) FROM release_files),
			(SELECT COUNT(*) FROM binary_inspections),
			(SELECT COUNT(*)
			 FROM release_archive_state
			 WHERE object_key <> ''
			   AND archive_status IN ('purge_pending', 'purged')),
			(SELECT COUNT(*)
			 FROM releases r
			 LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
			 WHERE `+publicIndexerReleaseVisibilityClause("r", DefaultReleaseReadyPolicy())+`),
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
		&item.ArchivedNZBCount,
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

const dashboardStatRefreshTimeout = 45 * time.Second

var indexerDashboardStatDefinitions = []indexerDashboardStatDefinition{
	{
		Key:         "unassembled_headers",
		Label:       "Unassembled Header Inventory",
		Description: "Exact count of article headers still waiting in the active assemble queue.",
		Exact:       true,
	},
	{
		Key:         "pending_release_summary_refresh_summaries",
		Label:       "Release Summary Refresh Backlog",
		Description: "Exact count of deferred readiness summaries still waiting for release_summary_refresh processing.",
		Exact:       true,
	},
	{
		Key:         "pending_release_candidate_families",
		Label:       "Release Backlog",
		Description: "Exact count of ready release candidates still waiting for release processing.",
		Exact:       true,
	},
	{
		Key:         "generate_nzb_pending_releases",
		Label:       "Generate NZB Backlog",
		Description: "Exact count of public-ready releases still waiting for direct archive generation.",
		Exact:       true,
	},
	{
		Key:         "archive_pending_releases",
		Label:       "Legacy Archive Backlog",
		Description: "Exact count of legacy nzb_cache-ready releases still waiting for transitional release_archive_nzb processing.",
		Exact:       true,
	},
	{
		Key:         "archived_waiting_for_purge_releases",
		Label:       "Purge Backlog",
		Description: "Exact count of archived releases still waiting for maintenance.release_source_purge processing.",
		Exact:       true,
	},
	{
		Key:         "purged_archived_releases",
		Label:       "Purged Archived Releases",
		Description: "Exact count of archived releases whose heavy source lineage has already been purged.",
		Exact:       true,
	},
	{
		Key:         "blob_backed_archived_releases",
		Label:       "Blob Archived Releases",
		Description: "Exact count of releases with durable blob-backed NZB archival metadata.",
		Exact:       true,
	},
	{
		Key:         "pending_yenc_recovery_binaries",
		Label:       "yEnc Recovery Backlog",
		Description: "Exact count of ready yEnc recovery work items recover_yenc can inspect now.",
		Exact:       true,
	},
	{
		Key:         "pending_inspect_discovery_binaries",
		Label:       "Discovery Backlog",
		Description: "Exact count of binaries inspect_discovery can claim now.",
		Exact:       true,
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
		Description: "Exact count of binaries inspect_nfo can claim now.",
		Exact:       true,
	},
	{
		Key:         "pending_inspect_archive_binaries",
		Label:       "Archive Inspection Backlog",
		Description: "Exact count of archive-family work units inspect_archive can claim now.",
		Exact:       true,
	},
	{
		Key:         "pending_inspect_password_binaries",
		Label:       "Password Inspection Backlog",
		Description: "Exact count of encrypted archive binaries inspect_password can claim now.",
		Exact:       true,
	},
	{
		Key:         "pending_inspect_media_binaries",
		Label:       "Media Inspection Backlog",
		Description: "Exact count of media binaries inspect_media can claim now.",
		Exact:       true,
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
		WITH latest_cutoff AS (
			SELECT
				sc.newsgroup_id,
				MAX(sc.backfill_until_date) AS configured_cutoff_date
			FROM scrape_checkpoints sc
			GROUP BY sc.newsgroup_id
		),
		checkpoint_rollup AS (
			SELECT
				sc.newsgroup_id,
				lc.configured_cutoff_date,
				BOOL_OR(
					sc.backfill_cutoff_reached = TRUE
					AND (
						(lc.configured_cutoff_date IS NULL AND sc.backfill_until_date IS NULL)
						OR sc.backfill_until_date = lc.configured_cutoff_date
					)
				) AS cutoff_reached,
				MIN(NULLIF(sc.backfill_article_number, 0)) AS backfill_cursor_article_number,
				MAX(sc.last_article_number) AS latest_article_number,
				COUNT(DISTINCT sc.provider_id) AS provider_count,
				MAX(sc.updated_at) AS last_checkpoint_updated_at
			FROM scrape_checkpoints sc
			JOIN latest_cutoff lc ON lc.newsgroup_id = sc.newsgroup_id
			GROUP BY sc.newsgroup_id, lc.configured_cutoff_date
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
	{StageName: "poster_materialize", Label: "Poster Materialize", ItemLabel: "headers"},
	{StageName: "crosspost_popularity_refresh", Label: "Crosspost Popularity", ItemLabel: "groups"},
	{StageName: "assemble", Label: "Assemble", ItemLabel: "headers"},
	{StageName: "recover_yenc", Label: "Recover yEnc", ItemLabel: "binaries"},
	{StageName: "maintenance.dashboard_stats_refresh", Label: "Dashboard Stats Refresh", ItemLabel: "stats"},
	{StageName: "release_summary_refresh", Label: "Release Summary Refresh", ItemLabel: "summaries"},
	{StageName: "release", Label: "Release", ItemLabel: "families"},
	{StageName: "release_generate_nzb", Label: "Generate NZB", ItemLabel: "releases"},
	{StageName: "release_archive_nzb", Label: "Archive NZB", ItemLabel: "releases"},
	{StageName: "maintenance.release_source_purge", Label: "Source Purge", ItemLabel: "releases"},
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
	completedRuns        int
	failedRuns           int
	itemsProcessed       int64
	totalDurationMS      float64
	totalWorkersUsed     int64
	maxWorkersUsed       int
	totalGroupsScheduled int64
	maxGroupsScheduled   int
	totalRangesFetched   int64
	maxRangesFetched     int
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
				if scrapeMetrics, ok := stageThroughputScrapeMetrics(stageName, metricsRaw); ok {
					acc.totalWorkersUsed += int64(scrapeMetrics.workersUsed)
					if scrapeMetrics.workersUsed > acc.maxWorkersUsed {
						acc.maxWorkersUsed = scrapeMetrics.workersUsed
					}
					acc.totalGroupsScheduled += int64(scrapeMetrics.groupsScheduled)
					if scrapeMetrics.groupsScheduled > acc.maxGroupsScheduled {
						acc.maxGroupsScheduled = scrapeMetrics.groupsScheduled
					}
					acc.totalRangesFetched += int64(scrapeMetrics.rangesFetched)
					if scrapeMetrics.rangesFetched > acc.maxRangesFetched {
						acc.maxRangesFetched = scrapeMetrics.rangesFetched
					}
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
			if def.StageName == "scrape_latest" || def.StageName == "scrape_backfill" {
				completedRuns := float64(maxInt(acc.completedRuns, 1))
				if acc.totalWorkersUsed > 0 {
					window.AvgWorkersUsed = float64(acc.totalWorkersUsed) / completedRuns
					window.MaxWorkersUsed = acc.maxWorkersUsed
				}
				if acc.totalGroupsScheduled > 0 {
					window.AvgGroupsScheduled = float64(acc.totalGroupsScheduled) / completedRuns
					window.MaxGroupsScheduled = acc.maxGroupsScheduled
				}
				if acc.totalRangesFetched > 0 {
					window.AvgRangesFetched = float64(acc.totalRangesFetched) / completedRuns
					window.MaxRangesFetched = acc.maxRangesFetched
				}
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

type scrapeThroughputMetrics struct {
	workersUsed     int
	groupsScheduled int
	rangesFetched   int
}

func stageThroughputScrapeMetrics(stageName string, metricsRaw []byte) (scrapeThroughputMetrics, bool) {
	if stageName != "scrape_latest" && stageName != "scrape_backfill" {
		return scrapeThroughputMetrics{}, false
	}
	if len(metricsRaw) == 0 {
		return scrapeThroughputMetrics{}, false
	}
	var metrics map[string]any
	if err := json.Unmarshal(metricsRaw, &metrics); err != nil {
		return scrapeThroughputMetrics{}, false
	}
	var out scrapeThroughputMetrics
	if value, ok := metricInt64(metrics["workers_used"]); ok && value > 0 {
		out.workersUsed = int(value)
	}
	if value, ok := metricInt64(metrics["groups_scheduled"]); ok && value > 0 {
		out.groupsScheduled = int(value)
	}
	if value, ok := metricInt64(metrics["ranges_fetched"]); ok && value > 0 {
		out.rangesFetched = int(value)
	}
	return out, out.workersUsed > 0 || out.groupsScheduled > 0 || out.rangesFetched > 0
}

func stageThroughputMetricKeys(stageName string) []string {
	switch stageName {
	case "scrape_latest", "scrape_backfill":
		return []string{"articles_inserted", "article_headers_seen"}
	case "poster_materialize":
		return []string{"claimed", "refs_upserted", "posters"}
	case "crosspost_popularity_refresh":
		return []string{"groups_refreshed", "claimed"}
	case "assemble":
		return []string{"processed_headers"}
	case "recover_yenc":
		return []string{"recovered", "attempted", "candidates"}
	case "maintenance.dashboard_stats_refresh":
		return []string{"available_count", "stat_count"}
	case "release":
		return []string{"candidate_families_inspected", "candidate_families"}
	case "release_generate_nzb":
		return []string{"generated_ready_count", "generate_attempted", "generate_candidates"}
	case "release_archive_nzb":
		return []string{"archived_count", "archive_claimed", "archive_candidates"}
	case "release_purge_archived_sources", "maintenance.release_source_purge":
		return []string{"purged_count", "purge_candidates"}
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
		statCtx, cancel := context.WithTimeout(ctx, dashboardStatRefreshTimeout)
		value, err := s.computeIndexerDashboardStat(statCtx, def.Key)
		cancel()
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
		return s.CountUnassembledArticleHeaders(ctx)
	case "pending_release_candidate_families":
		return s.CountPendingReleaseCandidateFamilies(ctx)
	case "generate_nzb_pending_releases":
		return s.countGenerateNZBBacklog(ctx)
	case "archive_pending_releases":
		return s.countArchiveBacklog(ctx)
	case "archived_waiting_for_purge_releases":
		return s.countArchiveState(ctx, "purge_pending")
	case "purged_archived_releases":
		return s.countArchiveState(ctx, "purged")
	case "blob_backed_archived_releases":
		return s.countBlobArchivedReleases(ctx)
	case "pending_release_summary_refresh_summaries":
		count, err := s.CountQueuedReleaseFamilySummaries(ctx)
		return int64(count), err
	case "pending_yenc_recovery_binaries":
		return s.CountPendingYEncRecoveryBinaries(ctx)
	case "pending_inspect_discovery_binaries":
		return s.CountPendingBinaryInspectionBacklog(ctx, "inspect_discovery")
	case "pending_inspect_par2_binaries":
		return s.CountPendingBinaryInspectionBacklog(ctx, "inspect_par2")
	case "pending_inspect_nfo_binaries":
		return s.CountPendingBinaryInspectionBacklog(ctx, "inspect_nfo")
	case "pending_inspect_archive_binaries":
		return s.CountPendingInspectArchiveBinaries(ctx)
	case "pending_inspect_password_binaries":
		return s.CountPendingBinaryInspectionBacklog(ctx, "inspect_password")
	case "pending_inspect_media_binaries":
		return s.CountPendingInspectMediaBinaries(ctx)
	default:
		return 0, fmt.Errorf("unsupported indexer dashboard stat %q", key)
	}
}

func (s *Store) countArchiveBacklog(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM releases r
		JOIN nzb_cache n ON n.release_id = r.release_id
		LEFT JOIN release_archive_state ras ON ras.release_id = r.release_id
		WHERE r.source_kind = 'usenet_index'
		  AND n.generation_status = 'ready'
		  AND EXISTS (SELECT 1 FROM release_files rf WHERE rf.release_id = r.release_id)
		  AND EXISTS (SELECT 1 FROM release_newsgroups rng WHERE rng.release_id = r.release_id)
		  AND COALESCE(ras.archive_status, 'active') IN ('active', 'archive_failed')`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count archive backlog: %w", err)
	}
	return count, nil
}

func (s *Store) countGenerateNZBBacklog(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM releases r
		LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
		LEFT JOIN release_archive_state ras ON ras.release_id = r.release_id
		WHERE r.source_kind = 'usenet_index'
		  AND EXISTS (SELECT 1 FROM release_files rf WHERE rf.release_id = r.release_id)
		  AND EXISTS (SELECT 1 FROM release_newsgroups rng WHERE rng.release_id = r.release_id)
		  AND EXISTS (
			SELECT 1
			FROM release_files rf
			JOIN binary_inspections bai
			  ON bai.binary_id = rf.binary_id
			 AND bai.stage_name = 'inspect_archive'
			 AND bai.status = 'completed'
			WHERE rf.release_id = r.release_id
		  )
		  AND EXISTS (
			SELECT 1
			FROM release_files rf
			JOIN binary_inspections bmi
			  ON bmi.binary_id = rf.binary_id
			 AND bmi.stage_name = 'inspect_media'
			 AND bmi.status = 'completed'
			WHERE rf.release_id = r.release_id
		  )
		  AND COALESCE(ras.archive_status, 'active') IN ('active', 'archive_failed')
		  AND (`+releaseReadyVisibilityClause("r", DefaultReleaseReadyPolicy())+`)`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count generate nzb backlog: %w", err)
	}
	return count, nil
}

func (s *Store) countArchiveState(ctx context.Context, state string) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_archive_state
		WHERE archive_status = $1`, strings.TrimSpace(state)).Scan(&count); err != nil {
		return 0, fmt.Errorf("count archive state %s: %w", state, err)
	}
	return count, nil
}

func (s *Store) countBlobArchivedReleases(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_archive_state
		WHERE object_key <> ''
		  AND archive_status IN ('archived', 'purge_pending', 'purged')`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count blob archived releases: %w", err)
	}
	return count, nil
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
		FROM release_ready_candidates c
		LEFT JOIN release_ready_candidate_acks a
		  ON a.provider_id = c.provider_id
		 AND a.newsgroup_id = c.newsgroup_id
		 AND a.key_kind = c.key_kind
		 AND a.family_key = c.family_key
		WHERE c.updated_at > COALESCE(a.processed_at, TIMESTAMPTZ 'epoch')`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending release candidate families: %w", err)
	}
	return count, nil
}

func (s *Store) CountPendingYEncRecoveryBinaries(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM yenc_recovery_work_items
		WHERE status = 'ready'
		  AND ready_at <= NOW()
		  AND BTRIM(COALESCE(message_id, '')) <> ''`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending yenc recovery backlog: %w", err)
	}
	return count, nil
}

func (s *Store) CountClaimableAssembleBacklog(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM article_headers
		WHERE assembled_at IS NULL
		  AND (
			assembly_claimed_until IS NULL
			OR assembly_claimed_until < NOW()
		  )`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count claimable assemble backlog: %w", err)
	}
	return count, nil
}

func (s *Store) CountPendingBinaryInspectionBacklog(ctx context.Context, stageName string) (int64, error) {
	stageName = strings.TrimSpace(stageName)
	if stageName == "inspect_par2" {
		return s.CountPendingPAR2InspectionBacklog(ctx)
	}
	count, err := s.countPendingBinaryInspectionBacklog(ctx, stageName)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) countPendingBinaryInspectionBacklog(ctx context.Context, stageName string) (int64, error) {
	filter, err := inspectCandidateFilter(stageName, false)
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
				(
					bi.inspection_claimed_until IS NULL OR
					bi.inspection_claimed_until < NOW()
				)
			) OR
			b.updated_at > bi.updated_at OR
			` + errorRerunPredicate
	filteredPredicate := `
		  AND NOT EXISTS (
			SELECT 1
			FROM binary_inspections cfi
			WHERE cfi.stage_name = 'inspect_discovery'
			  AND cfi.binary_id = b.id
			  AND cfi.status = 'completed'
			  AND COALESCE(cfi.summary_json->>'content_filtered', '') = 'true'
		  )`

	var count int64
	if stageName == "inspect_discovery" {
		if err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM binary_identity_current bic
			JOIN binary_core bc ON bc.binary_id = bic.binary_id
			JOIN binary_observation_stats bos ON bos.binary_id = bic.binary_id
			LEFT JOIN binary_recovery_current brc ON brc.binary_id = bic.binary_id
			LEFT JOIN binary_inspections bi
				ON bi.stage_name = $1
				AND bi.binary_id = bic.binary_id
			WHERE COALESCE(brc.recovered_extension, '') = ''
			  AND (bic.is_main_payload = TRUE OR bic.is_auxiliary = FALSE)
			  AND (
				LOWER(COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.bin' OR
				COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '') !~ '\.[A-Za-z0-9]{1,8}$'
			  )
			  AND (
				bi.id IS NULL OR
				bi.status = 'failed' OR
				(
					bi.status = 'running' AND
					(
						bi.inspection_claimed_until IS NULL OR
						bi.inspection_claimed_until < NOW()
					)
				) OR
				GREATEST(
					bc.updated_at,
					bic.updated_at,
					bos.updated_at,
					COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')
				) > bi.updated_at OR
				`+errorRerunPredicate+`
			  )
			  AND (
				bi.inspection_claimed_until IS NULL OR
				bi.inspection_claimed_until < NOW()
			  )`, stageName).Scan(&count); err != nil {
			return 0, fmt.Errorf("count pending %s backlog: %w", stageName, err)
		}
		return count, nil
	}

	if err := s.db.QueryRowContext(ctx, `
		WITH `+binaryInspectionCandidateStateCTE+`
		SELECT COUNT(DISTINCT b.id)
		FROM binary_state b
		JOIN release_files rf ON rf.binary_id = b.id
		JOIN releases r ON r.release_id = rf.release_id
		LEFT JOIN binary_inspections bi
			ON bi.stage_name = $1
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
		  )`+filteredPredicate, stageName).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending %s backlog: %w", stageName, err)
	}
	return count, nil
}

func (s *Store) CountPendingPAR2InspectionBacklog(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		WITH `+binaryInspectionCandidateStateCTE+`,
		candidate_rows AS (
			SELECT
				b.id,
				b.updated_at AS source_updated_at,
				COALESCE(bi.status, '') AS current_status,
				COALESCE(bi.summary_json, '{}'::jsonb) AS current_summary_json,
				CASE
					WHEN LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '')) ~ '\.vol[0-9]+(?:\+| )[0-9]+\.par2$'
					THEN regexp_replace(
						LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '')),
						'\.vol[0-9]+(?:\+| )[0-9]+\.par2$',
						'.par2'
					)
					ELSE LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), ''))
				END AS par2_set_name,
				CASE
					WHEN LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '')) ~ '\.vol[0-9]+(?:\+| )[0-9]+\.par2$'
					THEN 1
					ELSE 0
				END AS volume_rank,
				CASE
					WHEN LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), '')) ~ '\.vol([0-9]+)(?:\+| )[0-9]+\.par2$'
					THEN COALESCE(NULLIF(substring(
						LOWER(COALESCE(NULLIF(b.file_name, ''), NULLIF(b.binary_name, ''), ''))
						FROM '\.vol([0-9]+)(?:\+| )[0-9]+\.par2$'
					), ''), '0')::integer
					ELSE 0
				END AS volume_number,
				EXISTS (
					SELECT 1
					FROM binary_par2_targets bpt
					WHERE bpt.binary_id = b.id
				) AS has_targets,
				(
					bi.id IS NULL OR
					bi.status = 'failed' OR
					(
						bi.status = 'running' AND
						(
							bi.inspection_claimed_until IS NULL OR
							bi.inspection_claimed_until < NOW()
						)
					) OR
					b.updated_at > bi.updated_at
				) AS needs_rerun
			FROM binary_state b
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
				bi.inspection_claimed_until IS NULL OR
				bi.inspection_claimed_until < NOW()
			  )
		),
		set_state AS (
			SELECT
				par2_set_name,
				BOOL_OR(volume_rank = 0) AS has_manifest,
				BOOL_OR(has_targets) AS has_any_targets,
				BOOL_OR(CASE WHEN volume_rank = 0 THEN needs_rerun ELSE FALSE END) AS manifest_needs_rerun,
				BOOL_OR(
					current_status = 'completed' AND
					CASE
						WHEN COALESCE(current_summary_json->>'target_count', '') ~ '^[0-9]+$'
						THEN (current_summary_json->>'target_count')::integer = 0
						ELSE FALSE
					END
				) AS has_completed_zero_targets
			FROM candidate_rows
			GROUP BY par2_set_name
		),
		eligible_rows AS (
			SELECT cr.*
			FROM candidate_rows cr
			JOIN set_state ss ON ss.par2_set_name = cr.par2_set_name
			WHERE (
				cr.needs_rerun OR
				(
					NOT ss.has_any_targets AND
					NOT ss.has_completed_zero_targets
				)
			)
			  AND (
				NOT ss.has_manifest OR
				cr.volume_rank = 0 OR
				(
					NOT ss.manifest_needs_rerun AND
					NOT ss.has_any_targets
				)
			  )
		)
		SELECT COUNT(*)
		FROM (
			SELECT DISTINCT ON (par2_set_name) par2_set_name
			FROM eligible_rows
			ORDER BY par2_set_name, volume_rank, volume_number, source_updated_at DESC, id DESC
		) chosen`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending inspect_par2 backlog: %w", err)
	}
	return count, nil
}

func (s *Store) CountPendingInspectArchiveBinaries(ctx context.Context) (int64, error) {
	filter, err := inspectCandidateFilter("inspect_archive", false)
	if err != nil {
		return 0, err
	}

	errorRerunPredicate := `
			COALESCE(bi.summary_json->>'probe_error', '') <> '' OR
			COALESCE(bi.summary_json->>'ffprobe_error', '') <> '' OR
			COALESCE(bi.summary_json->>'extract_error', '') <> '' OR
			COALESCE(bi.summary_json->>'archive_extract_error', '') <> ''
			OR COALESCE(bi.summary_json->>'probe_error_detail', '') ILIKE '%has no articles%'
			OR (
				COALESCE(bi.summary_json->>'probe_strategy', '') = 'metadata_only' AND
				CASE
					WHEN jsonb_typeof(bi.summary_json->'archive_entries') = 'array' THEN jsonb_array_length(bi.summary_json->'archive_entries')
					ELSE 0
				END = 0 AND (
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.7z' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.001$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.zip' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.001$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.part01\.rar$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.part1\.rar$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.r00$' OR
					(
						LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.rar' AND
						LOWER(COALESCE(rf.file_name, b.file_name, '')) !~ '\.part\d+\.rar$' AND
						LOWER(COALESCE(rf.file_name, b.file_name, '')) !~ '\.r\d{2,3}$'
					)
				)
			)`
	representativePredicate := `
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.001$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.001$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.part0*1\.rar$' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.r00$' OR
					(
						LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.rar' AND
						LOWER(COALESCE(rf.file_name, b.file_name, '')) !~ '\.part\d+\.rar$'
					) OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.7z' OR
					LOWER(COALESCE(rf.file_name, b.file_name, '')) LIKE '%.zip'`

	var count int64
	err = s.db.QueryRowContext(ctx, `
		WITH `+binaryInspectionCandidateStateCTE+`,
		candidate_keys AS (
			SELECT
				r.release_id,
				CASE
					WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.7z\.\d{3}$'
						THEN REGEXP_REPLACE(LOWER(COALESCE(rf.file_name, b.file_name, '')), '\.7z\.\d{3}$', '.7z')
					WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.zip\.\d{3}$'
						THEN REGEXP_REPLACE(LOWER(COALESCE(rf.file_name, b.file_name, '')), '\.zip\.\d{3}$', '.zip')
					WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.part\d+\.rar$'
						THEN REGEXP_REPLACE(LOWER(COALESCE(rf.file_name, b.file_name, '')), '\.part\d+\.rar$', '.rar')
					WHEN LOWER(COALESCE(rf.file_name, b.file_name, '')) ~ '\.r\d{2,3}$'
						THEN REGEXP_REPLACE(LOWER(COALESCE(rf.file_name, b.file_name, '')), '\.r\d{2,3}$', '.rar')
					ELSE LOWER(COALESCE(rf.file_name, b.file_name, ''))
				END AS archive_key
			FROM binary_state b
			JOIN release_files rf ON rf.binary_id = b.id
			JOIN releases r ON r.release_id = rf.release_id
			LEFT JOIN binary_inspections bi
				ON bi.stage_name = 'inspect_archive'
				AND bi.binary_id = b.id
			WHERE `+filter+`
			  AND (`+representativePredicate+`)
			  AND (
				bi.id IS NULL OR
				bi.status = 'failed' OR
				(
					bi.status = 'running' AND
					(
						bi.inspection_claimed_until IS NULL OR
						bi.inspection_claimed_until < NOW()
					)
				) OR
				b.updated_at > bi.updated_at OR
				`+errorRerunPredicate+`
			  )
			  AND (
				bi.inspection_claimed_until IS NULL OR
				bi.inspection_claimed_until < NOW()
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM binary_inspections cfi
				WHERE cfi.stage_name = 'inspect_discovery'
				  AND cfi.binary_id = b.id
				  AND cfi.status = 'completed'
				  AND COALESCE(cfi.summary_json->>'content_filtered', '') = 'true'
			  )
		)
		SELECT COUNT(*)
		FROM (
			SELECT release_id, archive_key
			FROM candidate_keys
			GROUP BY release_id, archive_key
		) candidates`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending inspect_archive binaries: %w", err)
	}
	return count, nil
}

func (s *Store) CountPendingInspectMediaBinaries(ctx context.Context) (int64, error) {
	filter, err := inspectCandidateFilter("inspect_media", false)
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
				(
					bi.inspection_claimed_until IS NULL OR
					bi.inspection_claimed_until < NOW()
				)
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
		WITH `+binaryInspectionCandidateStateCTE+`
		SELECT COUNT(DISTINCT b.id)
		FROM binary_state b
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
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM binary_inspections cfi
			WHERE cfi.stage_name = 'inspect_discovery'
			  AND cfi.binary_id = b.id
			  AND cfi.status = 'completed'
			  AND COALESCE(cfi.summary_json->>'content_filtered', '') = 'true'
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
	case "posted_asc", "size_desc", "size_asc", "title_asc", "title_desc", "updated_desc", "quality_desc", "quality_asc", "completion_desc",
		"category_asc", "category_desc", "files_desc", "files_asc", "password_asc", "password_desc", "state_asc", "state_desc":
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
	case "title_desc":
		return "r.title DESC, r.posted_at DESC NULLS LAST"
	case "updated_desc":
		return "r.updated_at DESC, r.posted_at DESC NULLS LAST, r.title"
	case "quality_desc":
		return "r.media_quality_score DESC, r.posted_at DESC NULLS LAST, r.title"
	case "quality_asc":
		return "r.media_quality_score ASC, r.posted_at DESC NULLS LAST, r.title"
	case "completion_desc":
		return "r.completion_pct DESC, r.posted_at DESC NULLS LAST, r.title"
	case "category_asc":
		return "r.category ASC, r.category_id ASC, r.posted_at DESC NULLS LAST, r.title"
	case "category_desc":
		return "r.category DESC, r.category_id DESC, r.posted_at DESC NULLS LAST, r.title"
	case "files_desc":
		return "r.file_count DESC, r.posted_at DESC NULLS LAST, r.title"
	case "files_asc":
		return "r.file_count ASC, r.posted_at DESC NULLS LAST, r.title"
	case "password_asc":
		return "r.password_state ASC, r.posted_at DESC NULLS LAST, r.title"
	case "password_desc":
		return "r.password_state DESC, r.posted_at DESC NULLS LAST, r.title"
	case "state_asc":
		return "COALESCE(ro.hidden, FALSE) ASC, " + publicIndexerReleaseVisibilityClause("r", DefaultReleaseReadyPolicy()) + " ASC, r.posted_at DESC NULLS LAST, r.title"
	case "state_desc":
		return "COALESCE(ro.hidden, FALSE) DESC, " + publicIndexerReleaseVisibilityClause("r", DefaultReleaseReadyPolicy()) + " DESC, r.posted_at DESC NULLS LAST, r.title"
	default:
		return "r.posted_at DESC NULLS LAST, r.updated_at DESC, r.title"
	}
}

func adminReleasePayloadCompletionStateSQL(alias string) string {
	return fmt.Sprintf(`CASE
		WHEN %[1]s.archive_count > 0 AND %[1]s.expected_archive_file_count <= 0 THEN 'unknown'
		WHEN %[1]s.expected_archive_file_count > 0
		 AND GREATEST(COALESCE(%[1]s.file_count, 0) - COALESCE(%[1]s.par_file_count, 0), 0) >= %[1]s.expected_archive_file_count THEN 'complete'
		WHEN %[1]s.expected_archive_file_count <= 0
		 AND %[1]s.archive_count <= 0
		 AND %[1]s.completion_pct >= 100 THEN 'complete'
		ELSE 'incomplete'
	END`, alias)
}

func parseAdminFilterValues(raw string, allowed ...string) []string {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		allowedSet[value] = struct{}{}
	}
	seen := make(map[string]struct{}, len(allowed))
	out := make([]string, 0, len(allowed))
	for _, part := range strings.Split(raw, ",") {
		value := strings.ToLower(strings.TrimSpace(part))
		if _, ok := allowedSet[value]; !ok {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func adminReleaseStatePlaceholders(start, count int) string {
	parts := make([]string, 0, count)
	for i := 0; i < count; i++ {
		parts = append(parts, fmt.Sprintf("$%d", start+i))
	}
	return strings.Join(parts, ", ")
}

func anyStrings(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func adminReleaseInClause(column string, start int, values []string) (string, []any) {
	return fmt.Sprintf("%s IN (%s)", column, adminReleaseStatePlaceholders(start, len(values))), anyStrings(values)
}

func adminReleaseAnyPredicateClause(values []string, predicates map[string]string) string {
	clauses := make([]string, 0, len(values))
	for _, value := range values {
		if predicate := predicates[value]; predicate != "" {
			clauses = append(clauses, "("+predicate+")")
		}
	}
	if len(clauses) == 0 {
		return ""
	}
	return "(" + strings.Join(clauses, " OR ") + ")"
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
	if values := parseAdminFilterValues(params.Classification, "video", "video_archive", "tv", "movie", "audio", "ebook", "archive", "misc"); len(values) > 0 {
		clause, values := adminReleaseInClause("r.classification", arg, values)
		add(clause, values...)
	}
	if values := parseAdminFilterValues(params.ExternalMediaType, "movie", "tv", "audio"); len(values) > 0 {
		clause, values := adminReleaseInClause("r.external_media_type", arg, values)
		add(clause, values...)
	}
	if values := parseAdminFilterValues(params.IdentityStatus, "identified", "probable", "unknown"); len(values) > 0 {
		clause, values := adminReleaseInClause("r.identity_status", arg, values)
		add(clause, values...)
	}
	if values := parseAdminFilterValues(params.PasswordState, "not_passworded", "passworded_known", "passworded_unknown"); len(values) > 0 {
		clause, values := adminReleaseInClause("r.password_state", arg, values)
		add(clause, values...)
	}
	if values := parseAdminFilterValues(params.MediaQualityTier, "premium", "good", "fair", "unknown"); len(values) > 0 {
		clause, values := adminReleaseInClause("r.media_quality_tier", arg, values)
		add(clause, values...)
	}
	if clause := adminReleaseAnyPredicateClause(parseAdminFilterValues(params.Hidden, "hidden", "visible"), map[string]string{
		"hidden":  "COALESCE(ro.hidden, FALSE) = TRUE",
		"visible": "COALESCE(ro.hidden, FALSE) = FALSE",
	}); clause != "" {
		add(clause)
	}
	publicClause := publicIndexerReleaseVisibilityClause("r", DefaultReleaseReadyPolicy())
	if clause := adminReleaseAnyPredicateClause(parseAdminFilterValues(params.PublicState, "public", "internal_only", "hidden"), map[string]string{
		"public":        publicClause,
		"internal_only": "NOT (" + publicClause + ") AND COALESCE(ro.hidden, FALSE) = FALSE",
		"hidden":        "COALESCE(ro.hidden, FALSE) = TRUE",
	}); clause != "" {
		add(clause)
	}
	if clause := adminReleaseAnyPredicateClause(parseAdminFilterValues(params.Inspected, "yes", "no"), map[string]string{
		"yes": "r.runtime_seconds > 0 OR r.primary_resolution <> '' OR r.primary_video_codec <> '' OR r.primary_audio_codec <> '' OR r.has_nfo = TRUE OR r.has_par2 = TRUE",
		"no":  "r.runtime_seconds = 0 AND r.primary_resolution = '' AND r.primary_video_codec = '' AND r.primary_audio_codec = '' AND r.has_nfo = FALSE AND r.has_par2 = FALSE",
	}); clause != "" {
		add(clause)
	}
	if clause := adminReleaseAnyPredicateClause(parseAdminFilterValues(params.Enriched, "yes", "no"), map[string]string{
		"yes": "r.tmdb_id > 0 OR r.tvdb_id > 0 OR r.external_media_type <> '' OR r.matched_media_title <> ''",
		"no":  "r.tmdb_id = 0 AND r.tvdb_id = 0 AND r.external_media_type = '' AND r.matched_media_title = ''",
	}); clause != "" {
		add(clause)
	}
	if clause := adminReleaseAnyPredicateClause(parseAdminFilterValues(params.Uncategorized, "yes", "no"), map[string]string{
		"yes": fmt.Sprintf("r.category_id = %d", newsnab.OtherMisc),
		"no":  fmt.Sprintf("r.category_id <> %d", newsnab.OtherMisc),
	}); clause != "" {
		add(clause)
	}
	if clause := adminReleaseAnyPredicateClause(parseAdminFilterValues(params.PasswordCandidates, "yes", "no"), map[string]string{
		"yes": "EXISTS (SELECT 1 FROM release_password_candidates rpc WHERE rpc.release_id = r.release_id)",
		"no":  "NOT EXISTS (SELECT 1 FROM release_password_candidates rpc WHERE rpc.release_id = r.release_id)",
	}); clause != "" {
		add(clause)
	}
	metadataMismatchClause := `(
			(r.external_media_type = 'tv' AND r.matched_media_title <> '' AND r.tvdb_id = 0)
			OR (r.external_media_type <> '' AND r.matched_media_title <> '' AND r.tmdb_id = 0 AND r.tvdb_id = 0)
		)`
	if clause := adminReleaseAnyPredicateClause(parseAdminFilterValues(params.MetadataMismatch, "yes", "no"), map[string]string{
		"yes": metadataMismatchClause,
		"no":  "NOT " + metadataMismatchClause,
	}); clause != "" {
		add(clause)
	}
	if clause := adminReleaseAnyPredicateClause(parseAdminFilterValues(params.LowConfidence, "yes", "no"), map[string]string{
		"yes": "COALESCE(r.identity_confidence_score, 0) < 0.80",
		"no":  "COALESCE(r.identity_confidence_score, 0) >= 0.80",
	}); clause != "" {
		add(clause)
	}
	if clause := adminReleaseAnyPredicateClause(parseAdminFilterValues(params.CompletionState, "exact_100", "below_100"), map[string]string{
		"exact_100": "r.completion_pct = 100",
		"below_100": "r.completion_pct < 100",
	}); clause != "" {
		add(clause)
	}
	payloadStateSQL := adminReleasePayloadCompletionStateSQL("r")
	if states := parseAdminFilterValues(params.PayloadCompletionInclude, "complete", "incomplete", "unknown"); len(states) > 0 {
		add(fmt.Sprintf("(%s) IN (%s)", payloadStateSQL, adminReleaseStatePlaceholders(arg, len(states))), anyStrings(states)...)
	}
	if states := parseAdminFilterValues(params.PayloadCompletionExclude, "complete", "incomplete", "unknown"); len(states) > 0 {
		add(fmt.Sprintf("(%s) NOT IN (%s)", payloadStateSQL, adminReleaseStatePlaceholders(arg, len(states))), anyStrings(states)...)
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
			r.expected_archive_file_count,
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
			CASE
				WHEN COALESCE(ras.object_key, '') <> ''
				  AND ras.archive_status IN ('archived', 'purge_pending', 'purged')
				THEN ras.archive_status
				WHEN COALESCE(n.generation_status, '') <> ''
				THEN 'legacy_' || n.generation_status
				ELSE 'pending'
			END,
			COALESCE(ro.hidden, FALSE),
			CASE WHEN `+publicIndexerReleaseVisibilityClause("r", DefaultReleaseReadyPolicy())+` THEN TRUE ELSE FALSE END,
			(SELECT COUNT(*) FROM release_password_candidates rpc WHERE rpc.release_id = r.release_id),
			`+adminReleasePayloadCompletionStateSQL("r")+`
		FROM releases r
		LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
		LEFT JOIN release_archive_state ras ON ras.release_id = r.release_id
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
			r.expected_archive_file_count,
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
			CASE
				WHEN COALESCE(ras.object_key, '') <> ''
				  AND ras.archive_status IN ('archived', 'purge_pending', 'purged')
				THEN ras.archive_status
				WHEN COALESCE(n.generation_status, '') <> ''
				THEN 'legacy_' || n.generation_status
				ELSE 'pending'
			END,
			COALESCE(ro.hidden, FALSE),
			CASE WHEN `+publicIndexerReleaseVisibilityClause("r", DefaultReleaseReadyPolicy())+` THEN TRUE ELSE FALSE END,
			(SELECT COUNT(*) FROM release_password_candidates rpc WHERE rpc.release_id = r.release_id),
			`+adminReleasePayloadCompletionStateSQL("r")+`
		FROM releases r
		LEFT JOIN release_overrides ro ON ro.release_id = r.release_id
		LEFT JOIN release_archive_state ras ON ras.release_id = r.release_id
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
			COALESCE(rf.id, 0),
			COALESCE(rf.binary_id, 0),
			cf.file_name,
			cf.size_bytes,
			cf.file_index,
			cf.is_pars,
			cf.subject,
			cf.poster,
			cf.posted_at,
			cf.article_count,
			cf.total_parts,
			cf.observed_parts,
			cf.match_confidence,
			cf.match_status
		FROM release_catalog_files cf
		LEFT JOIN release_files rf
		  ON rf.release_id = cf.release_id
		 AND rf.file_name = cf.file_name
		WHERE cf.release_id = $1
		ORDER BY cf.file_index, cf.id`, releaseID)
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
		Diagnostics:        buildReleaseDetailDiagnostics(release, files),
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
		WITH binary_detail_state AS (
			SELECT
				bc.binary_id AS id,
				bc.provider_id,
				bc.newsgroup_id,
				bc.poster_id,
				bc.binary_key,
				bic.release_key,
				bic.release_name,
				bic.binary_name,
				bic.file_name,
				bic.file_index,
				bic.expected_file_count,
				bic.match_confidence,
				bic.match_status,
				bic.grouping_summary_kind,
				bic.grouping_summary_status,
				bic.grouping_summary_fallback_used,
				bos.posted_at,
				bos.total_parts,
				bos.observed_parts,
				bos.total_bytes,
				bos.first_article_number,
				bos.last_article_number
			FROM binary_core bc
			JOIN binary_identity_current bic ON bic.binary_id = bc.binary_id
			JOIN binary_observation_stats bos ON bos.binary_id = bc.binary_id
		)
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
			COALESCE(NULLIF(p.poster_name, ''), raw_meta.poster, ''),
			COALESCE(b.posted_at, raw_meta.posted_at),
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
				ELSE jsonb_strip_nulls(jsonb_build_object(
					'summary', CASE
						WHEN COALESCE(b.grouping_summary_kind, '') <> ''
						  OR COALESCE(b.grouping_summary_status, '') <> ''
						  OR COALESCE(b.grouping_summary_fallback_used, FALSE)
						THEN jsonb_build_object(
							'kind', NULLIF(b.grouping_summary_kind, ''),
							'status', NULLIF(b.grouping_summary_status, ''),
							'fallback_used', b.grouping_summary_fallback_used
						)
						ELSE NULL
					END
				))
			END,
			COALESCE(r.encrypted, FALSE),
			COALESCE(r.password_state, '')
		FROM binary_detail_state b
		LEFT JOIN binary_grouping_evidence bge ON bge.binary_id = b.id
		LEFT JOIN posters p ON p.id = b.poster_id
		LEFT JOIN release_files rf ON rf.binary_id = b.id
		LEFT JOIN releases r ON r.release_id = rf.release_id
		LEFT JOIN LATERAL (
			SELECT
				COALESCE(NULLIF(pp.poster_name, ''), NULLIF(aip.poster, '')) AS poster,
				MIN(ah.date_utc) AS posted_at
			FROM binary_parts bp
			JOIN article_headers ah ON ah.id = bp.article_header_id
			LEFT JOIN article_header_poster_refs apr ON apr.article_header_id = ah.id
			LEFT JOIN posters pp ON pp.id = apr.poster_id
			LEFT JOIN article_header_ingest_payloads aip ON aip.article_header_id = ah.id
			WHERE bp.binary_id = b.id
			GROUP BY COALESCE(NULLIF(pp.poster_name, ''), NULLIF(aip.poster, ''))
			ORDER BY COUNT(*) DESC, COALESCE(NULLIF(pp.poster_name, ''), NULLIF(aip.poster, ''))
			LIMIT 1
		) raw_meta ON TRUE
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
		WITH binary_detail_state AS (
			SELECT
				bc.binary_id AS id,
				bic.match_confidence,
				bic.match_status,
				bic.grouping_summary_kind,
				bic.grouping_summary_status,
				bic.grouping_summary_fallback_used,
				bos.total_parts,
				bos.observed_parts
			FROM binary_core bc
			JOIN binary_identity_current bic ON bic.binary_id = bc.binary_id
			JOIN binary_observation_stats bos ON bos.binary_id = bc.binary_id
		)
		SELECT
			cf.id,
			cf.release_id,
			COALESCE(r.title, ''),
			COALESCE(r.group_name, ''),
			COALESCE(rf.binary_id, 0),
			cf.file_name,
			cf.size_bytes,
			cf.file_index,
			cf.is_pars,
			cf.subject,
			COALESCE(NULLIF(cf.poster, ''), raw_meta.poster, ''),
			COALESCE(cf.posted_at, raw_meta.posted_at),
			COALESCE(b.total_parts, cf.total_parts, 0),
			COALESCE(b.observed_parts, cf.observed_parts, 0),
			COALESCE(b.match_confidence, 0),
			COALESCE(b.match_status, ''),
			CASE
				WHEN bge.binary_id IS NOT NULL THEN COALESCE(bge.payload_json, '{}'::jsonb)
				ELSE jsonb_strip_nulls(jsonb_build_object(
					'summary', CASE
						WHEN COALESCE(b.grouping_summary_kind, '') <> ''
						  OR COALESCE(b.grouping_summary_status, '') <> ''
						  OR COALESCE(b.grouping_summary_fallback_used, FALSE)
						THEN jsonb_build_object(
							'kind', NULLIF(b.grouping_summary_kind, ''),
							'status', NULLIF(b.grouping_summary_status, ''),
							'fallback_used', b.grouping_summary_fallback_used
						)
						ELSE NULL
					END
				))
			END,
			COALESCE(cf.article_count, 0)
		FROM release_catalog_files cf
		JOIN releases r ON r.release_id = cf.release_id
		LEFT JOIN release_files rf
		  ON rf.release_id = cf.release_id
		 AND rf.file_index = cf.file_index
		 AND rf.file_name = cf.file_name
		LEFT JOIN binary_detail_state b ON b.id = rf.binary_id
		LEFT JOIN binary_grouping_evidence bge ON bge.binary_id = b.id
		LEFT JOIN LATERAL (
			SELECT
				COALESCE(NULLIF(p.poster_name, ''), NULLIF(aip.poster, '')) AS poster,
				MIN(ah.date_utc) AS posted_at
			FROM binary_parts bp
			JOIN article_headers ah ON ah.id = bp.article_header_id
			LEFT JOIN article_header_poster_refs apr ON apr.article_header_id = ah.id
			LEFT JOIN posters p ON p.id = apr.poster_id
			LEFT JOIN article_header_ingest_payloads aip ON aip.article_header_id = ah.id
			WHERE bp.binary_id = rf.binary_id
			GROUP BY COALESCE(NULLIF(p.poster_name, ''), NULLIF(aip.poster, ''))
			ORDER BY COUNT(*) DESC, COALESCE(NULLIF(p.poster_name, ''), NULLIF(aip.poster, ''))
			LIMIT 1
		) raw_meta ON TRUE
		WHERE cf.id = $1`, fileID)

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
	if len(newsgroups) == 0 && item.BinaryID > 0 {
		newsgroups, err = s.ListCatalogBinaryNewsgroups(ctx, item.BinaryID)
		if err != nil {
			return nil, err
		}
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
		&item.ExpectedArchiveFileCount,
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
		&item.PayloadCompletionState,
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
