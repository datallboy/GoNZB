package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Servers  []ServerConfig  `mapstructure:"servers" yaml:"servers"`
	Indexers []IndexerConfig `mapstructure:"indexers" yaml:"indexers"`
	Download DownloadConfig  `mapstructure:"download" yaml:"download"`
	Log      LogConfig       `mapstructure:"log" yaml:"log"`
	Store    StoreConfig     `mapstructure:"store" yaml:"store"`
	API      APIConfig       `mapstructure:"api" yaml:"api"`

	Indexing   IndexingConfig   `mapstructure:"indexing" yaml:"indexing"`
	Aggregator AggregatorConfig `mapstructure:"aggregator" yaml:"aggregator"`
	GoNZBNet   GoNZBNetConfig   `mapstructure:"gonzbnet" yaml:"gonzbnet"`
	Modules    ModulesConfig    `mapstructure:"modules" yaml:"modules"`

	Port string `mapstructure:"port" yaml:"port"`
}

type ServerConfig struct {
	ID                     string   `mapstructure:"id" yaml:"id"`
	Host                   string   `mapstructure:"host" yaml:"host"`
	Port                   int      `mapstructure:"port" yaml:"port"`
	Username               string   `mapstructure:"username" yaml:"username"`
	Password               string   `mapstructure:"password" yaml:"password"`
	TLS                    bool     `mapstructure:"tls" yaml:"tls"`
	MaxConnection          int      `mapstructure:"max_connections" yaml:"max_connections"`
	Priority               int      `mapstructure:"priority" yaml:"priority"`
	DialTimeoutSeconds     int      `mapstructure:"dial_timeout_seconds" yaml:"dial_timeout_seconds"`
	TCPKeepAliveSeconds    int      `mapstructure:"tcp_keepalive_seconds" yaml:"tcp_keepalive_seconds"`
	PoolIdleTimeoutSeconds int      `mapstructure:"pool_idle_timeout_seconds" yaml:"pool_idle_timeout_seconds"`
	PoolMaxAgeSeconds      int      `mapstructure:"pool_max_age_seconds" yaml:"pool_max_age_seconds"`
	EnablePoolLogging      bool     `mapstructure:"enable_pool_logging" yaml:"enable_pool_logging"`
	Roles                  []string `mapstructure:"roles" yaml:"roles"`
}

type IndexerConfig struct {
	ID       string `mapstructure:"id" yaml:"id"`
	BaseUrl  string `mapstructure:"base_url" yaml:"base_url"`
	ApiPath  string `mapstructure:"api_path" yaml:"api_path"`
	ApiKey   string `mapstructure:"api_key" yaml:"api_key"`
	Redirect bool   `mapstructure:"redirect" yaml:"redirect"`
}

type DownloadConfig struct {
	OutDir            string   `mapstructure:"out_dir" yaml:"out_dir"`
	CompletedDir      string   `mapstructure:"completed_dir" yaml:"completed_dir"`
	CleanupExtensions []string `mapstructure:"cleanup_extensions" yaml:"cleanup_extensions"`
}

type LogConfig struct {
	Path          string `mapstructure:"path" yaml:"path"`
	Level         string `mapstructure:"level" yaml:"level"`
	IncludeStdout bool   `mapstructure:"include_stdout" yaml:"include_stdout"`
	MaxSizeMB     int    `mapstructure:"max_size_mb" yaml:"max_size_mb"`
	MaxBackups    int    `mapstructure:"max_backups" yaml:"max_backups"`
}

type StoreConfig struct {
	SQLitePath               string `mapstructure:"sqlite_path" yaml:"sqlite_path"`
	BlobDir                  string `mapstructure:"blob_dir" yaml:"blob_dir"`
	PayloadCacheEnabled      bool   `mapstructure:"payload_cache_enabled" yaml:"payload_cache_enabled"`
	SearchPersistenceEnabled bool   `mapstructure:"search_persistence_enabled" yaml:"search_persistence_enabled"`

	// PostgreSQL DSN for Usenet/NZB Indexer module.
	PGDSN            string `mapstructure:"pg_dsn" yaml:"pg_dsn"`
	PGMaintenanceDSN string `mapstructure:"pg_maintenance_dsn" yaml:"pg_maintenance_dsn"`
}
type APIConfig struct {
	CORSAllowedOrigins []string `mapstructure:"cors_allowed_origins" yaml:"cors_allowed_origins"`
}

type AggregatorConfig struct {
	Sources AggregatorSourcesConfig `mapstructure:"sources" yaml:"sources"`
}

type AggregatorSourcesConfig struct {
	LocalBlob     ModuleToggle `mapstructure:"local_blob" yaml:"local_blob"`
	UsenetIndexer ModuleToggle `mapstructure:"usenet_indexer" yaml:"usenet_indexer"`
}

type GoNZBNetConfig struct {
	Mode                           string   `mapstructure:"mode" yaml:"mode"`
	NodeAlias                      string   `mapstructure:"node_alias" yaml:"node_alias"`
	AdvertiseURL                   string   `mapstructure:"advertise_url" yaml:"advertise_url"`
	KeysDir                        string   `mapstructure:"keys_dir" yaml:"keys_dir"`
	KeyPassword                    string   `mapstructure:"key_password" yaml:"key_password"`
	SpecVersion                    string   `mapstructure:"spec_version" yaml:"spec_version"`
	HTTPEnabled                    bool     `mapstructure:"http_enabled" yaml:"http_enabled"`
	HTTPBasePath                   string   `mapstructure:"http_base_path" yaml:"http_base_path"`
	PrivateNetwork                 bool     `mapstructure:"private_network" yaml:"private_network"`
	NetworkID                      string   `mapstructure:"network_id" yaml:"network_id"`
	LocalPoolID                    string   `mapstructure:"local_pool_id" yaml:"local_pool_id"`
	ManualPeers                    []string `mapstructure:"manual_peers" yaml:"manual_peers"`
	PublishReleaseCardsEnabled     bool     `mapstructure:"publish_release_cards_enabled" yaml:"publish_release_cards_enabled"`
	PublishReleaseCardsBatchSize   int      `mapstructure:"publish_release_cards_batch_size" yaml:"publish_release_cards_batch_size"`
	PublishReleaseCardsIntervalMin float64  `mapstructure:"publish_release_cards_interval_minutes" yaml:"publish_release_cards_interval_minutes"`
	PullSyncEnabled                bool     `mapstructure:"pull_sync_enabled" yaml:"pull_sync_enabled"`
	PullSyncIntervalMin            float64  `mapstructure:"pull_sync_interval_minutes" yaml:"pull_sync_interval_minutes"`
	PushSyncEnabled                bool     `mapstructure:"push_sync_enabled" yaml:"push_sync_enabled"`
	PushSyncIntervalMin            float64  `mapstructure:"push_sync_interval_minutes" yaml:"push_sync_interval_minutes"`
	PushSyncBatchSize              int      `mapstructure:"push_sync_batch_size" yaml:"push_sync_batch_size"`
	MaxEventBytes                  int      `mapstructure:"max_event_bytes" yaml:"max_event_bytes"`
	MaxManifestBytes               int      `mapstructure:"max_manifest_bytes" yaml:"max_manifest_bytes"`
	MaxBatchEvents                 int      `mapstructure:"max_batch_events" yaml:"max_batch_events"`
	TimeToleranceSeconds           int      `mapstructure:"time_tolerance_seconds" yaml:"time_tolerance_seconds"`
	NonceTTLSeconds                int      `mapstructure:"nonce_ttl_seconds" yaml:"nonce_ttl_seconds"`
	LiveQueryEnabled               bool     `mapstructure:"live_query_enabled" yaml:"live_query_enabled"`
	SendUserContext                bool     `mapstructure:"send_user_context" yaml:"send_user_context"`
	ShareProviderBackbone          bool     `mapstructure:"share_provider_backbone_hash" yaml:"share_provider_backbone_hash"`
	ShareSourceIndexer             bool     `mapstructure:"share_source_indexer_hash" yaml:"share_source_indexer_hash"`
}

type IndexingConfig struct {
	Newsgroups                   []string                   `mapstructure:"newsgroups" yaml:"newsgroups"`
	BackfillUntilDateByGroup     map[string]string          `mapstructure:"backfill_until_date_by_group" yaml:"backfill_until_date_by_group"`
	ScrapeLatest                 IndexingStageConfig        `mapstructure:"scrape_latest" yaml:"scrape_latest"`
	ScrapeBackfill               IndexingStageConfig        `mapstructure:"scrape_backfill" yaml:"scrape_backfill"`
	PosterMaterialize            IndexingStageConfig        `mapstructure:"poster_materialize" yaml:"poster_materialize"`
	CrosspostPopularityRefresh   IndexingStageConfig        `mapstructure:"crosspost_popularity_refresh" yaml:"crosspost_popularity_refresh"`
	Assemble                     IndexingStageConfig        `mapstructure:"assemble" yaml:"assemble"`
	RecoverYEnc                  IndexingStageConfig        `mapstructure:"recover_yenc" yaml:"recover_yenc"`
	ReleaseSummaryRefresh        IndexingStageConfig        `mapstructure:"release_summary_refresh" yaml:"release_summary_refresh"`
	Release                      IndexingReleaseConfig      `mapstructure:"release" yaml:"release"`
	ReleaseGenerateNZB           IndexingStageConfig        `mapstructure:"release_generate_nzb" yaml:"release_generate_nzb"`
	ReleaseArchiveNZB            IndexingStageConfig        `mapstructure:"release_archive_nzb" yaml:"release_archive_nzb"`
	ReleasePurgeArchivedSources  IndexingStageConfig        `mapstructure:"release_purge_archived_sources" yaml:"release_purge_archived_sources"`
	InspectDiscoveryReadyRefresh IndexingStageConfig        `mapstructure:"inspect_discovery_ready_refresh" yaml:"inspect_discovery_ready_refresh"`
	InspectPAR2ReadyRefresh      IndexingStageConfig        `mapstructure:"inspect_par2_ready_refresh" yaml:"inspect_par2_ready_refresh"`
	InspectArchiveReadyRefresh   IndexingStageConfig        `mapstructure:"inspect_archive_ready_refresh" yaml:"inspect_archive_ready_refresh"`
	InspectMediaReadyRefresh     IndexingStageConfig        `mapstructure:"inspect_media_ready_refresh" yaml:"inspect_media_ready_refresh"`
	Match                        IndexingMatchConfig        `mapstructure:"match" yaml:"match"`
	Inspect                      IndexingInspectConfig      `mapstructure:"inspect" yaml:"inspect"`
	StorageGuard                 IndexingStorageGuardConfig `mapstructure:"storage_guard" yaml:"storage_guard"`
	MemoryGuard                  IndexingMemoryGuardConfig  `mapstructure:"memory_guard" yaml:"memory_guard"`
	InspectDiscovery             IndexingStageConfig        `mapstructure:"inspect_discovery" yaml:"inspect_discovery"`
	InspectPAR2                  IndexingStageConfig        `mapstructure:"inspect_par2" yaml:"inspect_par2"`
	InspectNFO                   IndexingStageConfig        `mapstructure:"inspect_nfo" yaml:"inspect_nfo"`
	InspectArchive               IndexingStageConfig        `mapstructure:"inspect_archive" yaml:"inspect_archive"`
	InspectPassword              IndexingStageConfig        `mapstructure:"inspect_password" yaml:"inspect_password"`
	InspectMedia                 IndexingStageConfig        `mapstructure:"inspect_media" yaml:"inspect_media"`
	EnrichPreDB                  IndexingPreDBConfig        `mapstructure:"enrich_predb" yaml:"enrich_predb"`
	EnrichTMDB                   IndexingTMDBConfig         `mapstructure:"enrich_tmdb" yaml:"enrich_tmdb"`
}

type IndexingStageConfig struct {
	Enabled                 *bool    `mapstructure:"enabled" yaml:"enabled"`
	IntervalMinutes         *float64 `mapstructure:"interval_minutes" yaml:"interval_minutes"`
	BatchSize               *int     `mapstructure:"batch_size" yaml:"batch_size"`
	MaxBatches              *int     `mapstructure:"max_batches" yaml:"max_batches"`
	Concurrency             *int     `mapstructure:"concurrency" yaml:"concurrency"`
	MaxEffectiveConcurrency *int     `mapstructure:"max_effective_concurrency" yaml:"max_effective_concurrency"`
	BackoffSeconds          *int     `mapstructure:"backoff_seconds" yaml:"backoff_seconds"`
	BinaryUpsertDBChunkSize *int     `mapstructure:"binary_upsert_db_chunk_size" yaml:"binary_upsert_db_chunk_size"`
	LaneATargetPct          *int     `mapstructure:"lane_a_target_pct" yaml:"lane_a_target_pct"`
	LaneBMinPct             *int     `mapstructure:"lane_b_min_pct" yaml:"lane_b_min_pct"`
	LaneATimeWindowMinutes  *int     `mapstructure:"lane_a_time_window_minutes" yaml:"lane_a_time_window_minutes"`
	TargetWindowEnabled     *bool    `mapstructure:"target_window_enabled" yaml:"target_window_enabled"`
	TargetWindowStart       *string  `mapstructure:"target_window_start" yaml:"target_window_start"`
	TargetWindowEnd         *string  `mapstructure:"target_window_end" yaml:"target_window_end"`
	TargetWindowPct         *int     `mapstructure:"target_window_pct" yaml:"target_window_pct"`
	NewestPct               *int     `mapstructure:"newest_pct" yaml:"newest_pct"`
}

type IndexingMatchConfig struct {
	HighConfidenceThreshold     *float64 `mapstructure:"high_confidence_threshold" yaml:"high_confidence_threshold"`
	ProbableConfidenceThreshold *float64 `mapstructure:"probable_confidence_threshold" yaml:"probable_confidence_threshold"`
	ArticleBucketSize           *int64   `mapstructure:"article_bucket_size" yaml:"article_bucket_size"`
}

type IndexingReleaseConfig struct {
	Enabled                                         *bool    `mapstructure:"enabled" yaml:"enabled"`
	IntervalMinutes                                 *float64 `mapstructure:"interval_minutes" yaml:"interval_minutes"`
	BatchSize                                       *int     `mapstructure:"batch_size" yaml:"batch_size"`
	AutoReformBatchSize                             *int     `mapstructure:"auto_reform_batch_size" yaml:"auto_reform_batch_size"`
	BackoffSeconds                                  *int     `mapstructure:"backoff_seconds" yaml:"backoff_seconds"`
	MinConfidence                                   *float64 `mapstructure:"min_confidence" yaml:"min_confidence"`
	MinCompletionPct                                *float64 `mapstructure:"min_completion_pct" yaml:"min_completion_pct"`
	MinExpectedFileCoveragePct                      *float64 `mapstructure:"min_expected_file_coverage_pct" yaml:"min_expected_file_coverage_pct"`
	RequireExpectedFileCountForContextualObfuscated *bool    `mapstructure:"require_expected_file_count_for_contextual_obfuscated" yaml:"require_expected_file_count_for_contextual_obfuscated"`
	PublicMinMatchConfidence                        *float64 `mapstructure:"public_min_match_confidence" yaml:"public_min_match_confidence"`
	PublicMinCompletionPct                          *float64 `mapstructure:"public_min_completion_pct" yaml:"public_min_completion_pct"`
	PublicMinIdentityStatus                         string   `mapstructure:"public_min_identity_status" yaml:"public_min_identity_status"`
	PublicRequireInspection                         *bool    `mapstructure:"public_require_inspection" yaml:"public_require_inspection"`
	PublicRequireEnrichment                         *bool    `mapstructure:"public_require_enrichment" yaml:"public_require_enrichment"`
	PublicRequireClearTitle                         *bool    `mapstructure:"public_require_clear_title" yaml:"public_require_clear_title"`
	PublicRequirePayloadComplete                    *bool    `mapstructure:"public_require_payload_complete" yaml:"public_require_payload_complete"`
	PublicRequireExpectedFileCountComplete          *bool    `mapstructure:"public_require_expected_file_count_complete" yaml:"public_require_expected_file_count_complete"`
	PublicRequirePAR2                               *bool    `mapstructure:"public_require_par2" yaml:"public_require_par2"`
	PublicRequireNFO                                *bool    `mapstructure:"public_require_nfo" yaml:"public_require_nfo"`
	PublicRequireSFV                                *bool    `mapstructure:"public_require_sfv" yaml:"public_require_sfv"`
	RetainUntilExpectedFileCountComplete            *bool    `mapstructure:"retain_until_expected_file_count_complete" yaml:"retain_until_expected_file_count_complete"`
	RetainRequirePAR2                               *bool    `mapstructure:"retain_require_par2" yaml:"retain_require_par2"`
	RetainRequireNFO                                *bool    `mapstructure:"retain_require_nfo" yaml:"retain_require_nfo"`
	RetainRequireSFV                                *bool    `mapstructure:"retain_require_sfv" yaml:"retain_require_sfv"`
	ReopenArchivedNZBOnReleaseChange                *bool    `mapstructure:"reopen_archived_nzb_on_release_change" yaml:"reopen_archived_nzb_on_release_change"`
}

type IndexingInspectConfig struct {
	WorkDir                  string   `mapstructure:"work_dir" yaml:"work_dir"`
	WorkspaceBackend         string   `mapstructure:"workspace_backend" yaml:"workspace_backend"`
	MemoryWorkDir            string   `mapstructure:"memory_work_dir" yaml:"memory_work_dir"`
	MaxBytes                 int64    `mapstructure:"max_bytes" yaml:"max_bytes"`
	MinBinaryBytes           int64    `mapstructure:"min_binary_bytes" yaml:"min_binary_bytes"`
	MaxBinaryBytes           int64    `mapstructure:"max_binary_bytes" yaml:"max_binary_bytes"`
	RequireExpectedFileCount bool     `mapstructure:"require_expected_file_count" yaml:"require_expected_file_count"`
	BlockedMagicHex          []string `mapstructure:"blocked_magic_hex" yaml:"blocked_magic_hex"`
	MaxArchiveDepth          int      `mapstructure:"max_archive_depth" yaml:"max_archive_depth"`
	ToolTimeoutSecs          int      `mapstructure:"tool_timeout_seconds" yaml:"tool_timeout_seconds"`
	FFmpegPath               string   `mapstructure:"ffmpeg_path" yaml:"ffmpeg_path"`
	FFProbePath              string   `mapstructure:"ffprobe_path" yaml:"ffprobe_path"`
	SevenZipPath             string   `mapstructure:"seven_zip_path" yaml:"seven_zip_path"`
	UnrarPath                string   `mapstructure:"unrar_path" yaml:"unrar_path"`
	PAR2Path                 string   `mapstructure:"par2_path" yaml:"par2_path"`
}

type IndexingStorageGuardConfig struct {
	Enabled        *bool    `mapstructure:"enabled" yaml:"enabled"`
	DataDirectory  string   `mapstructure:"data_directory" yaml:"data_directory"`
	MinFreeBytes   *int64   `mapstructure:"min_free_bytes" yaml:"min_free_bytes"`
	MinFreePercent *float64 `mapstructure:"min_free_percent" yaml:"min_free_percent"`
}

type IndexingMemoryGuardConfig struct {
	Enabled             *bool    `mapstructure:"enabled" yaml:"enabled"`
	MinAvailableBytes   *int64   `mapstructure:"min_available_bytes" yaml:"min_available_bytes"`
	MinAvailablePercent *float64 `mapstructure:"min_available_percent" yaml:"min_available_percent"`
	MinSwapFreeBytes    *int64   `mapstructure:"min_swap_free_bytes" yaml:"min_swap_free_bytes"`
}

type IndexingPreDBConfig struct {
	Enabled            *bool    `mapstructure:"enabled" yaml:"enabled"`
	IntervalMinutes    *float64 `mapstructure:"interval_minutes" yaml:"interval_minutes"`
	BatchSize          *int     `mapstructure:"batch_size" yaml:"batch_size"`
	BackoffSeconds     *int     `mapstructure:"backoff_seconds" yaml:"backoff_seconds"`
	Provider           string   `mapstructure:"provider" yaml:"provider"`
	BaseURL            string   `mapstructure:"base_url" yaml:"base_url"`
	FeedURL            string   `mapstructure:"feed_url" yaml:"feed_url"`
	DumpURL            string   `mapstructure:"dump_url" yaml:"dump_url"`
	HTTPTimeoutSeconds *int     `mapstructure:"http_timeout_seconds" yaml:"http_timeout_seconds"`
	BackfillPageSize   *int     `mapstructure:"backfill_page_size" yaml:"backfill_page_size"`
	MaxBackfillPages   *int     `mapstructure:"max_backfill_pages" yaml:"max_backfill_pages"`
}

type IndexingTMDBConfig struct {
	Enabled            *bool    `mapstructure:"enabled" yaml:"enabled"`
	IntervalMinutes    *float64 `mapstructure:"interval_minutes" yaml:"interval_minutes"`
	BatchSize          *int     `mapstructure:"batch_size" yaml:"batch_size"`
	BackoffSeconds     *int     `mapstructure:"backoff_seconds" yaml:"backoff_seconds"`
	HTTPTimeoutSeconds *int     `mapstructure:"http_timeout_seconds" yaml:"http_timeout_seconds"`
	TMDBAPIKey         string   `mapstructure:"tmdb_api_key" yaml:"tmdb_api_key"`
	TMDBAccessToken    string   `mapstructure:"tmdb_access_token" yaml:"tmdb_access_token"`
	TMDBBaseURL        string   `mapstructure:"tmdb_base_url" yaml:"tmdb_base_url"`
	TVDBAPIKey         string   `mapstructure:"tvdb_api_key" yaml:"tvdb_api_key"`
	TVDBPIN            string   `mapstructure:"tvdb_pin" yaml:"tvdb_pin"`
	TVDBBaseURL        string   `mapstructure:"tvdb_base_url" yaml:"tvdb_base_url"`
}

// ModuleConfig is used to enable or disable certain modules within the application
type ModulesConfig struct {
	Downloader    ModuleToggle `mapstructure:"downloader" yaml:"downloader"`
	Aggregator    ModuleToggle `mapstructure:"aggregator" yaml:"aggregator"`
	UsenetIndexer ModuleToggle `mapstructure:"usenet_indexer" yaml:"usenet_indexer"`
	GoNZBNet      ModuleToggle `mapstructure:"gonzbnet" yaml:"gonzbnet"`
	WebUI         ModuleToggle `mapstructure:"web_ui" yaml:"web_ui"`
	API           ModuleToggle `mapstructure:"api" yaml:"api"`
}

type ModuleToggle struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
}

const (
	defaultServerMaxConnections      = 10
	defaultServerPriority            = 1
	defaultServerDialTimeoutSeconds  = 10
	defaultServerTCPKeepAliveSeconds = 30
	defaultServerPoolIdleTimeoutSecs = 45
	defaultServerPoolMaxAgeSeconds   = 600
)

func Load(path string) (*Config, error) {

	if path == "" {
		path = "config.yaml"
	}

	// 1. Check if the file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// FALLBACK: If we are in Docker (or similar) and didn't provide a flag, check /config/config.yaml
		if path == "config.yaml" {
			if _, errEx := os.Stat("/config/config.yaml"); errEx == nil {
				path = "/config/config.yaml"
			} else if _, errEx := os.Stat("config.yaml.example"); errEx == nil {
				// If config.yaml is missing but example exists, give a helpful error
				return nil, fmt.Errorf("configuration file 'config.yaml' not found\n\n" +
					"To fix this, run:\n" +
					"  cp config.yaml.example config.yaml\n" +
					"Then edit it with your Usenet credentials.")
			} else {
				return nil, fmt.Errorf("config file not found: %s", path)
			}
		} else {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
	}

	v := viper.New()

	// Set Defaults
	v.SetDefault("port", "8080")
	v.SetDefault("download.out_dir", "./downloads")
	v.SetDefault("download.completed_dir", "./downloads/completed")
	v.SetDefault("download.cleanup_extensions", []string{"nzb", "par2", "sfv", "nfo"}) // sane default for completed cleanup
	v.SetDefault("log.level", "info")
	v.SetDefault("log.include_stdout", true)
	v.SetDefault("log.max_size_mb", 32)
	v.SetDefault("log.max_backups", 5)
	v.SetDefault("store.payload_cache_enabled", true)
	v.SetDefault("store.search_persistence_enabled", true)

	v.SetDefault("store.pg_dsn", "")
	v.SetDefault("store.pg_maintenance_dsn", "")
	v.SetDefault("indexing.newsgroups", []string{})
	v.SetDefault("indexing.backfill_until_date_by_group", map[string]string{})
	v.SetDefault("indexing.scrape_latest.enabled", false)
	v.SetDefault("indexing.scrape_latest.interval_minutes", 10.0)
	v.SetDefault("indexing.scrape_latest.batch_size", 5000)
	v.SetDefault("indexing.scrape_latest.concurrency", 1)
	v.SetDefault("indexing.scrape_latest.max_batches", 1)
	v.SetDefault("indexing.scrape_latest.backoff_seconds", 0)
	v.SetDefault("indexing.scrape_backfill.enabled", false)
	v.SetDefault("indexing.scrape_backfill.interval_minutes", 10.0)
	v.SetDefault("indexing.scrape_backfill.batch_size", 5000)
	v.SetDefault("indexing.scrape_backfill.concurrency", 1)
	v.SetDefault("indexing.scrape_backfill.max_batches", 1)
	v.SetDefault("indexing.scrape_backfill.backoff_seconds", 0)
	v.SetDefault("indexing.assemble.binary_upsert_db_chunk_size", 1000)
	v.SetDefault("indexing.assemble.lane_a_target_pct", 70)
	v.SetDefault("indexing.assemble.lane_b_min_pct", 30)
	v.SetDefault("indexing.assemble.lane_a_time_window_minutes", 15)
	v.SetDefault("indexing.release.enabled", false)
	v.SetDefault("indexing.release.interval_minutes", 10.0)
	v.SetDefault("indexing.release.batch_size", 1000)
	v.SetDefault("indexing.release.auto_reform_batch_size", 25)
	v.SetDefault("indexing.release.backoff_seconds", 0)
	v.SetDefault("indexing.release.min_confidence", 0.55)
	v.SetDefault("indexing.release.min_completion_pct", 0.0)
	v.SetDefault("indexing.release.min_expected_file_coverage_pct", 90.0)
	v.SetDefault("indexing.release.require_expected_file_count_for_contextual_obfuscated", true)
	v.SetDefault("indexing.release.public_min_match_confidence", 0.55)
	v.SetDefault("indexing.release.public_min_completion_pct", 100.0)
	v.SetDefault("indexing.release.public_min_identity_status", "probable")
	v.SetDefault("indexing.release.public_require_inspection", true)
	v.SetDefault("indexing.release.public_require_enrichment", false)
	v.SetDefault("indexing.release.public_require_clear_title", true)
	v.SetDefault("indexing.release.public_require_payload_complete", true)
	v.SetDefault("indexing.release.public_require_expected_file_count_complete", false)
	v.SetDefault("indexing.release.public_require_par2", false)
	v.SetDefault("indexing.release.public_require_nfo", false)
	v.SetDefault("indexing.release.public_require_sfv", false)
	v.SetDefault("indexing.release.retain_until_expected_file_count_complete", false)
	v.SetDefault("indexing.release.retain_require_par2", false)
	v.SetDefault("indexing.release.retain_require_nfo", false)
	v.SetDefault("indexing.release.retain_require_sfv", false)
	v.SetDefault("indexing.release.reopen_archived_nzb_on_release_change", false)
	v.SetDefault("indexing.inspect.require_expected_file_count", false)
	v.SetDefault("indexing.release_generate_nzb.enabled", false)
	v.SetDefault("indexing.release_generate_nzb.interval_minutes", 10.0)
	v.SetDefault("indexing.release_generate_nzb.batch_size", 100)
	v.SetDefault("indexing.release_generate_nzb.backoff_seconds", 0)
	v.SetDefault("indexing.release_archive_nzb.enabled", false)
	v.SetDefault("indexing.release_archive_nzb.interval_minutes", 10.0)
	v.SetDefault("indexing.release_archive_nzb.batch_size", 100)
	v.SetDefault("indexing.release_archive_nzb.backoff_seconds", 0)
	v.SetDefault("indexing.release_purge_archived_sources.enabled", false)
	v.SetDefault("indexing.release_purge_archived_sources.interval_minutes", 10.0)
	v.SetDefault("indexing.release_purge_archived_sources.batch_size", 50)
	v.SetDefault("indexing.release_purge_archived_sources.backoff_seconds", 0)
	v.SetDefault("indexing.inspect_discovery_ready_refresh.enabled", false)
	v.SetDefault("indexing.inspect_discovery_ready_refresh.interval_minutes", 10.0)
	v.SetDefault("indexing.inspect_discovery_ready_refresh.batch_size", 10000)
	v.SetDefault("indexing.inspect_discovery_ready_refresh.backoff_seconds", 0)
	v.SetDefault("indexing.inspect_par2_ready_refresh.enabled", false)
	v.SetDefault("indexing.inspect_par2_ready_refresh.interval_minutes", 10.0)
	v.SetDefault("indexing.inspect_par2_ready_refresh.batch_size", 10000)
	v.SetDefault("indexing.inspect_par2_ready_refresh.backoff_seconds", 0)
	v.SetDefault("indexing.inspect_archive_ready_refresh.enabled", false)
	v.SetDefault("indexing.inspect_archive_ready_refresh.interval_minutes", 10.0)
	v.SetDefault("indexing.inspect_archive_ready_refresh.batch_size", 10000)
	v.SetDefault("indexing.inspect_archive_ready_refresh.backoff_seconds", 0)
	v.SetDefault("indexing.inspect_media_ready_refresh.enabled", false)
	v.SetDefault("indexing.inspect_media_ready_refresh.interval_minutes", 10.0)
	v.SetDefault("indexing.inspect_media_ready_refresh.batch_size", 10000)
	v.SetDefault("indexing.inspect_media_ready_refresh.backoff_seconds", 0)
	v.SetDefault("indexing.match.high_confidence_threshold", 0.85)
	v.SetDefault("indexing.match.probable_confidence_threshold", 0.55)
	v.SetDefault("indexing.match.article_bucket_size", int64(5000))
	v.SetDefault("indexing.inspect.work_dir", "/store/indexer/inspect")
	v.SetDefault("indexing.inspect.workspace_backend", "auto")
	v.SetDefault("indexing.inspect.memory_work_dir", "/dev/shm/gonzb-inspect")
	v.SetDefault("indexing.inspect.max_bytes", int64(2*1024*1024*1024))
	v.SetDefault("indexing.inspect.min_binary_bytes", int64(0))
	v.SetDefault("indexing.inspect.max_binary_bytes", int64(0))
	v.SetDefault("indexing.inspect.blocked_magic_hex", []string{"52434C4F4E45"})
	v.SetDefault("indexing.inspect.max_archive_depth", 3)
	v.SetDefault("indexing.inspect.tool_timeout_seconds", 30)
	v.SetDefault("indexing.inspect.ffmpeg_path", "ffmpeg")
	v.SetDefault("indexing.inspect.ffprobe_path", "ffprobe")
	v.SetDefault("indexing.inspect.seven_zip_path", "7z")
	v.SetDefault("indexing.inspect.unrar_path", "unrar")
	v.SetDefault("indexing.inspect.par2_path", "par2")
	v.SetDefault("indexing.storage_guard.enabled", true)
	v.SetDefault("indexing.storage_guard.data_directory", "")
	v.SetDefault("indexing.storage_guard.min_free_bytes", int64(0))
	v.SetDefault("indexing.storage_guard.min_free_percent", 15.0)
	v.SetDefault("indexing.inspect_discovery.enabled", false)
	v.SetDefault("indexing.inspect_discovery.interval_minutes", 10.0)
	v.SetDefault("indexing.inspect_discovery.batch_size", 100)
	v.SetDefault("indexing.inspect_discovery.backoff_seconds", 0)
	v.SetDefault("indexing.inspect_par2.enabled", false)
	v.SetDefault("indexing.inspect_par2.interval_minutes", 10.0)
	v.SetDefault("indexing.inspect_par2.batch_size", 100)
	v.SetDefault("indexing.inspect_par2.concurrency", 4)
	v.SetDefault("indexing.inspect_par2.backoff_seconds", 0)
	v.SetDefault("indexing.inspect_nfo.enabled", false)
	v.SetDefault("indexing.inspect_nfo.interval_minutes", 10.0)
	v.SetDefault("indexing.inspect_nfo.batch_size", 100)
	v.SetDefault("indexing.inspect_nfo.backoff_seconds", 0)
	v.SetDefault("indexing.inspect_archive.enabled", false)
	v.SetDefault("indexing.inspect_archive.interval_minutes", 10.0)
	v.SetDefault("indexing.inspect_archive.batch_size", 100)
	v.SetDefault("indexing.inspect_archive.concurrency", 1)
	v.SetDefault("indexing.inspect_archive.backoff_seconds", 0)
	v.SetDefault("indexing.inspect_password.enabled", false)
	v.SetDefault("indexing.inspect_password.interval_minutes", 10.0)
	v.SetDefault("indexing.inspect_password.batch_size", 100)
	v.SetDefault("indexing.inspect_password.backoff_seconds", 0)
	v.SetDefault("indexing.inspect_media.enabled", false)
	v.SetDefault("indexing.inspect_media.interval_minutes", 10.0)
	v.SetDefault("indexing.inspect_media.batch_size", 100)
	v.SetDefault("indexing.inspect_media.concurrency", 1)
	v.SetDefault("indexing.inspect_media.backoff_seconds", 0)
	v.SetDefault("indexing.recover_yenc.enabled", false)
	v.SetDefault("indexing.recover_yenc.interval_minutes", 10.0)
	v.SetDefault("indexing.recover_yenc.batch_size", 25)
	v.SetDefault("indexing.recover_yenc.concurrency", 1)
	v.SetDefault("indexing.recover_yenc.backoff_seconds", 0)
	v.SetDefault("indexing.enrich_predb.enabled", false)
	v.SetDefault("indexing.enrich_predb.interval_minutes", 10.0)
	v.SetDefault("indexing.enrich_predb.batch_size", 100)
	v.SetDefault("indexing.enrich_predb.backoff_seconds", 0)
	v.SetDefault("indexing.enrich_predb.provider", "club,me")
	v.SetDefault("indexing.enrich_predb.base_url", "https://predb.club/api/v1")
	v.SetDefault("indexing.enrich_predb.feed_url", "https://predb.me/?rss=1")
	v.SetDefault("indexing.enrich_predb.dump_url", "")
	v.SetDefault("indexing.enrich_predb.http_timeout_seconds", 10)
	v.SetDefault("indexing.enrich_predb.backfill_page_size", 1000)
	v.SetDefault("indexing.enrich_predb.max_backfill_pages", 250)
	v.SetDefault("indexing.enrich_tmdb.enabled", false)
	v.SetDefault("indexing.enrich_tmdb.interval_minutes", 10.0)
	v.SetDefault("indexing.enrich_tmdb.batch_size", 100)
	v.SetDefault("indexing.enrich_tmdb.backoff_seconds", 0)
	v.SetDefault("indexing.enrich_tmdb.http_timeout_seconds", 15)
	v.SetDefault("indexing.enrich_tmdb.tmdb_api_key", "")
	v.SetDefault("indexing.enrich_tmdb.tmdb_access_token", "")
	v.SetDefault("indexing.enrich_tmdb.tmdb_base_url", "https://api.themoviedb.org/3")
	v.SetDefault("indexing.enrich_tmdb.tvdb_api_key", "")
	v.SetDefault("indexing.enrich_tmdb.tvdb_pin", "")
	v.SetDefault("indexing.enrich_tmdb.tvdb_base_url", "https://api4.thetvdb.com/v4")

	v.SetDefault("modules.downloader.enabled", true)
	v.SetDefault("modules.aggregator.enabled", true)
	v.SetDefault("modules.usenet_indexer.enabled", true)
	v.SetDefault("modules.gonzbnet.enabled", false)
	v.SetDefault("modules.web_ui.enabled", true)
	v.SetDefault("modules.api.enabled", true)
	v.SetDefault("aggregator.sources.local_blob.enabled", false)
	v.SetDefault("aggregator.sources.usenet_indexer.enabled", false)
	v.SetDefault("gonzbnet.mode", "integrated")
	v.SetDefault("gonzbnet.node_alias", "")
	v.SetDefault("gonzbnet.advertise_url", "")
	v.SetDefault("gonzbnet.keys_dir", "data/gonzbnet/keys")
	v.SetDefault("gonzbnet.key_password", "")
	v.SetDefault("gonzbnet.spec_version", "gonzbnet/1.0")
	v.SetDefault("gonzbnet.http_enabled", true)
	v.SetDefault("gonzbnet.http_base_path", "/gonzbnet/v1")
	v.SetDefault("gonzbnet.private_network", true)
	v.SetDefault("gonzbnet.network_id", "default")
	v.SetDefault("gonzbnet.local_pool_id", "pool.local")
	v.SetDefault("gonzbnet.manual_peers", []string{})
	v.SetDefault("gonzbnet.publish_release_cards_enabled", false)
	v.SetDefault("gonzbnet.publish_release_cards_batch_size", 50)
	v.SetDefault("gonzbnet.publish_release_cards_interval_minutes", 10.0)
	v.SetDefault("gonzbnet.pull_sync_enabled", false)
	v.SetDefault("gonzbnet.pull_sync_interval_minutes", 10.0)
	v.SetDefault("gonzbnet.push_sync_enabled", false)
	v.SetDefault("gonzbnet.push_sync_interval_minutes", 10.0)
	v.SetDefault("gonzbnet.push_sync_batch_size", 100)
	v.SetDefault("gonzbnet.max_event_bytes", 262144)
	v.SetDefault("gonzbnet.max_manifest_bytes", 10485760)
	v.SetDefault("gonzbnet.max_batch_events", 100)
	v.SetDefault("gonzbnet.time_tolerance_seconds", 120)
	v.SetDefault("gonzbnet.nonce_ttl_seconds", 600)
	v.SetDefault("gonzbnet.live_query_enabled", false)
	v.SetDefault("gonzbnet.send_user_context", false)
	v.SetDefault("gonzbnet.share_provider_backbone_hash", false)
	v.SetDefault("gonzbnet.share_source_indexer_hash", false)

	v.SetDefault("api.cors_allowed_origins", []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
	})

	// Read config File
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file %s: %w", path, err)
	}

	// Support Environment Variables
	v.SetEnvPrefix("GONZB")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// exported validation entrypoint for effective bootstrap+runtime settings.
func (c *Config) ValidateEffective() error {
	return c.validate()
}

func (c *Config) validate() error {

	if c.Download.OutDir == "" {
		c.Download.OutDir = "./downloads"
	}
	if c.GoNZBNet.SendUserContext {
		return errors.New("gonzbnet.send_user_context must remain false; federation must not send local user context")
	}
	if err := validateIndexingStageConfig("indexing.scrape_latest", c.Indexing.ScrapeLatest); err != nil {
		return err
	}
	for group, rawDate := range c.Indexing.BackfillUntilDateByGroup {
		if strings.TrimSpace(group) == "" {
			return errors.New("indexing.backfill_until_date_by_group keys must not be blank")
		}
		if strings.TrimSpace(rawDate) == "" {
			return fmt.Errorf("indexing.backfill_until_date_by_group[%s] must not be blank", group)
		}
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(rawDate)); err != nil {
			return fmt.Errorf("indexing.backfill_until_date_by_group[%s] must be in YYYY-MM-DD format: %w", group, err)
		}
	}
	if err := validateIndexingStageConfig("indexing.scrape_backfill", c.Indexing.ScrapeBackfill); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.poster_materialize", c.Indexing.PosterMaterialize); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.crosspost_popularity_refresh", c.Indexing.CrosspostPopularityRefresh); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.assemble", c.Indexing.Assemble); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.recover_yenc", c.Indexing.RecoverYEnc); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.inspect_discovery_ready_refresh", c.Indexing.InspectDiscoveryReadyRefresh); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.inspect_par2_ready_refresh", c.Indexing.InspectPAR2ReadyRefresh); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.inspect_archive_ready_refresh", c.Indexing.InspectArchiveReadyRefresh); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.inspect_media_ready_refresh", c.Indexing.InspectMediaReadyRefresh); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.release", IndexingStageConfig{
		Enabled:         c.Indexing.Release.Enabled,
		IntervalMinutes: c.Indexing.Release.IntervalMinutes,
		BatchSize:       c.Indexing.Release.BatchSize,
		BackoffSeconds:  c.Indexing.Release.BackoffSeconds,
	}); err != nil {
		return err
	}
	if c.Indexing.Release.MinConfidence != nil {
		if *c.Indexing.Release.MinConfidence <= 0 || *c.Indexing.Release.MinConfidence > 1 {
			return errors.New("indexing.release.min_confidence must be between 0 and 1")
		}
	}
	if c.Indexing.Release.MinCompletionPct != nil {
		if *c.Indexing.Release.MinCompletionPct < 0 || *c.Indexing.Release.MinCompletionPct > 100 {
			return errors.New("indexing.release.min_completion_pct must be between 0 and 100")
		}
	}
	if c.Indexing.Release.MinExpectedFileCoveragePct != nil {
		if *c.Indexing.Release.MinExpectedFileCoveragePct < 0 || *c.Indexing.Release.MinExpectedFileCoveragePct > 100 {
			return errors.New("indexing.release.min_expected_file_coverage_pct must be between 0 and 100")
		}
	}
	if c.Indexing.Release.PublicMinMatchConfidence != nil {
		if *c.Indexing.Release.PublicMinMatchConfidence < 0 || *c.Indexing.Release.PublicMinMatchConfidence > 1 {
			return errors.New("indexing.release.public_min_match_confidence must be between 0 and 1")
		}
	}
	if c.Indexing.Release.PublicMinCompletionPct != nil {
		if *c.Indexing.Release.PublicMinCompletionPct < 0 || *c.Indexing.Release.PublicMinCompletionPct > 100 {
			return errors.New("indexing.release.public_min_completion_pct must be between 0 and 100")
		}
	}
	switch strings.TrimSpace(c.Indexing.Release.PublicMinIdentityStatus) {
	case "", "probable", "identified":
	default:
		return errors.New("indexing.release.public_min_identity_status must be one of: probable, identified")
	}
	if err := validateIndexingStageConfig("indexing.inspect_par2", c.Indexing.InspectPAR2); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.inspect_nfo", c.Indexing.InspectNFO); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.inspect_archive", c.Indexing.InspectArchive); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.inspect_password", c.Indexing.InspectPassword); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.inspect_media", c.Indexing.InspectMedia); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.enrich_predb", IndexingStageConfig{
		Enabled:         c.Indexing.EnrichPreDB.Enabled,
		IntervalMinutes: c.Indexing.EnrichPreDB.IntervalMinutes,
		BatchSize:       c.Indexing.EnrichPreDB.BatchSize,
		BackoffSeconds:  c.Indexing.EnrichPreDB.BackoffSeconds,
	}); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.enrich_tmdb", IndexingStageConfig{
		Enabled:         c.Indexing.EnrichTMDB.Enabled,
		IntervalMinutes: c.Indexing.EnrichTMDB.IntervalMinutes,
		BatchSize:       c.Indexing.EnrichTMDB.BatchSize,
		BackoffSeconds:  c.Indexing.EnrichTMDB.BackoffSeconds,
	}); err != nil {
		return err
	}
	if c.Indexing.Match.HighConfidenceThreshold != nil {
		if *c.Indexing.Match.HighConfidenceThreshold <= 0 || *c.Indexing.Match.HighConfidenceThreshold > 1 {
			return errors.New("indexing.match.high_confidence_threshold must be between 0 and 1")
		}
	}
	if c.Indexing.Match.ProbableConfidenceThreshold != nil {
		if *c.Indexing.Match.ProbableConfidenceThreshold <= 0 || *c.Indexing.Match.ProbableConfidenceThreshold > 1 {
			return errors.New("indexing.match.probable_confidence_threshold must be between 0 and 1")
		}
	}
	if c.Indexing.Match.HighConfidenceThreshold != nil && c.Indexing.Match.ProbableConfidenceThreshold != nil &&
		*c.Indexing.Match.ProbableConfidenceThreshold > *c.Indexing.Match.HighConfidenceThreshold {
		return errors.New("indexing.match.probable_confidence_threshold must be less than or equal to indexing.match.high_confidence_threshold")
	}
	if c.Indexing.Match.ArticleBucketSize != nil && *c.Indexing.Match.ArticleBucketSize <= 0 {
		return errors.New("indexing.match.article_bucket_size must be greater than 0")
	}
	if c.Indexing.Inspect.MaxBytes < 0 {
		return errors.New("indexing.inspect.max_bytes must be greater than or equal to 0")
	}
	if c.Indexing.Inspect.MinBinaryBytes < 0 {
		return errors.New("indexing.inspect.min_binary_bytes must be greater than or equal to 0")
	}
	if c.Indexing.Inspect.MaxBinaryBytes < 0 {
		return errors.New("indexing.inspect.max_binary_bytes must be greater than or equal to 0")
	}
	if c.Indexing.Inspect.MinBinaryBytes > 0 && c.Indexing.Inspect.MaxBinaryBytes > 0 && c.Indexing.Inspect.MinBinaryBytes > c.Indexing.Inspect.MaxBinaryBytes {
		return errors.New("indexing.inspect.min_binary_bytes must be less than or equal to indexing.inspect.max_binary_bytes")
	}
	for i, rule := range c.Indexing.Inspect.BlockedMagicHex {
		clean := strings.ToUpper(strings.TrimSpace(rule))
		clean = strings.ReplaceAll(clean, "0X", "")
		clean = strings.ReplaceAll(clean, " ", "")
		clean = strings.ReplaceAll(clean, ":", "")
		clean = strings.ReplaceAll(clean, "-", "")
		if clean == "" {
			continue
		}
		if len(clean)%2 != 0 {
			return fmt.Errorf("indexing.inspect.blocked_magic_hex[%d] must contain an even number of hex characters", i)
		}
		if _, err := hex.DecodeString(clean); err != nil {
			return fmt.Errorf("indexing.inspect.blocked_magic_hex[%d] must be hex encoded", i)
		}
	}
	if c.Indexing.Inspect.MaxArchiveDepth < 0 {
		return errors.New("indexing.inspect.max_archive_depth must be greater than or equal to 0")
	}
	if c.Indexing.Inspect.ToolTimeoutSecs < 0 {
		return errors.New("indexing.inspect.tool_timeout_seconds must be greater than or equal to 0")
	}
	if c.Indexing.StorageGuard.MinFreeBytes != nil && *c.Indexing.StorageGuard.MinFreeBytes < 0 {
		return errors.New("indexing.storage_guard.min_free_bytes must be greater than or equal to 0")
	}
	if c.Indexing.StorageGuard.MinFreePercent != nil && (*c.Indexing.StorageGuard.MinFreePercent < 0 || *c.Indexing.StorageGuard.MinFreePercent > 100) {
		return errors.New("indexing.storage_guard.min_free_percent must be between 0 and 100")
	}
	if c.Indexing.MemoryGuard.MinAvailableBytes != nil && *c.Indexing.MemoryGuard.MinAvailableBytes < 0 {
		return errors.New("indexing.memory_guard.min_available_bytes must be greater than or equal to 0")
	}
	if c.Indexing.MemoryGuard.MinAvailablePercent != nil && (*c.Indexing.MemoryGuard.MinAvailablePercent < 0 || *c.Indexing.MemoryGuard.MinAvailablePercent > 100) {
		return errors.New("indexing.memory_guard.min_available_percent must be between 0 and 100")
	}
	if c.Indexing.MemoryGuard.MinSwapFreeBytes != nil && *c.Indexing.MemoryGuard.MinSwapFreeBytes < 0 {
		return errors.New("indexing.memory_guard.min_swap_free_bytes must be greater than or equal to 0")
	}
	if c.Indexing.EnrichPreDB.HTTPTimeoutSeconds != nil && *c.Indexing.EnrichPreDB.HTTPTimeoutSeconds <= 0 {
		return errors.New("indexing.enrich_predb.http_timeout_seconds must be greater than 0")
	}
	if c.Indexing.EnrichPreDB.BackfillPageSize != nil && *c.Indexing.EnrichPreDB.BackfillPageSize <= 0 {
		return errors.New("indexing.enrich_predb.backfill_page_size must be greater than 0")
	}
	if c.Indexing.EnrichPreDB.MaxBackfillPages != nil && *c.Indexing.EnrichPreDB.MaxBackfillPages <= 0 {
		return errors.New("indexing.enrich_predb.max_backfill_pages must be greater than 0")
	}
	if c.Indexing.EnrichTMDB.HTTPTimeoutSeconds != nil && *c.Indexing.EnrichTMDB.HTTPTimeoutSeconds <= 0 {
		return errors.New("indexing.enrich_tmdb.http_timeout_seconds must be greater than 0")
	}

	// startup must have at least one meaningful runtime surface.
	if !c.Modules.Downloader.Enabled &&
		!c.Modules.Aggregator.Enabled &&
		!c.Modules.UsenetIndexer.Enabled &&
		!c.Modules.API.Enabled &&
		!c.Modules.WebUI.Enabled {
		return errors.New("at least one module must be enabled")
	}

	// web_ui is transport-only and requires API.
	if c.Modules.WebUI.Enabled && !c.Modules.API.Enabled {
		return errors.New("modules.web_ui.enabled requires modules.api.enabled")
	}

	// Usenet/NZB Indexer requires PostgreSQL.
	if c.Modules.UsenetIndexer.Enabled && strings.TrimSpace(c.Store.PGDSN) == "" {
		return errors.New("store.pg_dsn is required when modules.usenet_indexer.enabled is true")
	}

	for i, s := range c.Servers {
		if strings.TrimSpace(s.ID) != "" || strings.TrimSpace(s.Host) != "" || s.Port != 0 {
			if s.ID == "" {
				return fmt.Errorf("server[%d] requires a unique ID", i)
			}
			if s.Host == "" {
				return fmt.Errorf("server %s: host is required", s.ID)
			}
			if s.Port == 0 {
				return fmt.Errorf("server %s: port is required", s.ID)
			}
			if s.TLS && s.Port == 119 {
				fmt.Println("Warning: TLS is enabled but port is set to 119 (standard non-TLS)")
			}
			if s.MaxConnection <= 0 {
				c.Servers[i].MaxConnection = defaultServerMaxConnections
			}
			if s.Priority == 0 {
				c.Servers[i].Priority = defaultServerPriority
			}
			if s.DialTimeoutSeconds <= 0 {
				c.Servers[i].DialTimeoutSeconds = defaultServerDialTimeoutSeconds
			}
			if s.TCPKeepAliveSeconds <= 0 {
				c.Servers[i].TCPKeepAliveSeconds = defaultServerTCPKeepAliveSeconds
			}
			if s.PoolIdleTimeoutSeconds <= 0 {
				c.Servers[i].PoolIdleTimeoutSeconds = defaultServerPoolIdleTimeoutSecs
			}
			if s.PoolMaxAgeSeconds <= 0 {
				c.Servers[i].PoolMaxAgeSeconds = defaultServerPoolMaxAgeSeconds
			}
		}
	}

	return nil
}

func validateIndexingStageConfig(name string, cfg IndexingStageConfig) error {
	if cfg.IntervalMinutes != nil && *cfg.IntervalMinutes <= 0 {
		return fmt.Errorf("%s.interval_minutes must be greater than 0", name)
	}
	if cfg.BatchSize != nil && *cfg.BatchSize <= 0 {
		return fmt.Errorf("%s.batch_size must be greater than 0", name)
	}
	if cfg.MaxBatches != nil && *cfg.MaxBatches <= 0 {
		return fmt.Errorf("%s.max_batches must be greater than 0", name)
	}
	if cfg.Concurrency != nil && *cfg.Concurrency <= 0 {
		return fmt.Errorf("%s.concurrency must be greater than 0", name)
	}
	if cfg.MaxEffectiveConcurrency != nil && *cfg.MaxEffectiveConcurrency <= 0 {
		return fmt.Errorf("%s.max_effective_concurrency must be greater than 0", name)
	}
	if cfg.BackoffSeconds != nil && *cfg.BackoffSeconds < 0 {
		return fmt.Errorf("%s.backoff_seconds must be greater than or equal to 0", name)
	}
	if cfg.BinaryUpsertDBChunkSize != nil && *cfg.BinaryUpsertDBChunkSize <= 0 {
		return fmt.Errorf("%s.binary_upsert_db_chunk_size must be greater than 0", name)
	}
	if cfg.LaneATimeWindowMinutes != nil && *cfg.LaneATimeWindowMinutes <= 0 {
		return fmt.Errorf("%s.lane_a_time_window_minutes must be greater than 0", name)
	}
	return nil
}
