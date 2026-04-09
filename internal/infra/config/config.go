package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Servers  []ServerConfig  `mapstructure:"servers" yaml:"servers"`
	Indexers []IndexerConfig `mapstructure:"indexers" yaml:"indexers"`
	Download DownloadConfig  `mapstructure:"download" yaml:"download"`
	Log      LogConfig       `mapstructure:"log" yaml:"log"`
	Store    StoreConfig     `mapstructure:"store" yaml:"store"`
	API      APIConfig       `mapstructure:"api" yaml:"api"`

	Indexing IndexingConfig `mapstructure:"indexing" yaml:"indexing"`
	Modules  ModulesConfig  `mapstructure:"modules" yaml:"modules"`

	Port string `mapstructure:"port" yaml:"port"`
}

type ServerConfig struct {
	ID            string `mapstructure:"id" yaml:"id"`
	Host          string `mapstructure:"host" yaml:"host"`
	Port          int    `mapstructure:"port" yaml:"port"`
	Username      string `mapstructure:"username" yaml:"username"`
	Password      string `mapstructure:"password" yaml:"password"`
	TLS           bool   `mapstructure:"tls" yaml:"tls"`
	MaxConnection int    `mapstructure:"max_connections" yaml:"max_connections"`
	Priority      int    `mapstructure:"priority" yaml:"priority"`
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
}

type StoreConfig struct {
	SQLitePath               string `mapstructure:"sqlite_path" yaml:"sqlite_path"`
	BlobDir                  string `mapstructure:"blob_dir" yaml:"blob_dir"`
	PayloadCacheEnabled      bool   `mapstructure:"payload_cache_enabled" yaml:"payload_cache_enabled"`
	SearchPersistenceEnabled bool   `mapstructure:"search_persistence_enabled" yaml:"search_persistence_enabled"`

	// PostgreSQL DSN for Usenet/NZB Indexer module.
	PGDSN string `mapstructure:"pg_dsn" yaml:"pg_dsn"`
}

type APIConfig struct {
	Key                string   `mapstructure:"key" yaml:"key"`
	CORSAllowedOrigins []string `mapstructure:"cors_allowed_origins" yaml:"cors_allowed_origins"`
}

type IndexingConfig struct {
	Newsgroups              []string              `mapstructure:"newsgroups" yaml:"newsgroups"`
	ScrapeBatchSize         int64                 `mapstructure:"scrape_batch_size" yaml:"scrape_batch_size"`
	ScheduleIntervalMinutes float64               `mapstructure:"schedule_interval_minutes" yaml:"schedule_interval_minutes"`
	ReleaseMinConfidence    float64               `mapstructure:"release_min_confidence" yaml:"release_min_confidence"`
	ReleaseMinCompletionPct float64               `mapstructure:"release_min_completion_pct" yaml:"release_min_completion_pct"`
	InspectWorkDir          string                `mapstructure:"inspect_work_dir" yaml:"inspect_work_dir"`
	InspectMaxBytes         int64                 `mapstructure:"inspect_max_bytes" yaml:"inspect_max_bytes"`
	InspectMaxArchiveDepth  int                   `mapstructure:"inspect_max_archive_depth" yaml:"inspect_max_archive_depth"`
	InspectToolTimeoutSecs  int                   `mapstructure:"inspect_tool_timeout_seconds" yaml:"inspect_tool_timeout_seconds"`
	EnableInspectPAR2       bool                  `mapstructure:"enable_inspect_par2" yaml:"enable_inspect_par2"`
	EnableInspectNFO        bool                  `mapstructure:"enable_inspect_nfo" yaml:"enable_inspect_nfo"`
	EnableInspectArchive    bool                  `mapstructure:"enable_inspect_archive" yaml:"enable_inspect_archive"`
	EnableInspectPassword   bool                  `mapstructure:"enable_inspect_password" yaml:"enable_inspect_password"`
	EnableInspectMedia      bool                  `mapstructure:"enable_inspect_media" yaml:"enable_inspect_media"`
	EnableEnrichPreDB       bool                  `mapstructure:"enable_enrich_predb" yaml:"enable_enrich_predb"`
	EnableEnrichTMDB        bool                  `mapstructure:"enable_enrich_tmdb" yaml:"enable_enrich_tmdb"`
	PreDBProvider           string                `mapstructure:"predb_provider" yaml:"predb_provider"`
	PreDBBaseURL            string                `mapstructure:"predb_base_url" yaml:"predb_base_url"`
	PreDBFeedURL            string                `mapstructure:"predb_feed_url" yaml:"predb_feed_url"`
	PreDBDumpURL            string                `mapstructure:"predb_dump_url" yaml:"predb_dump_url"`
	TMDBAPIKey              string                `mapstructure:"tmdb_api_key" yaml:"tmdb_api_key"`
	TMDBAccessToken         string                `mapstructure:"tmdb_access_token" yaml:"tmdb_access_token"`
	TMDBBaseURL             string                `mapstructure:"tmdb_base_url" yaml:"tmdb_base_url"`
	TVDBAPIKey              string                `mapstructure:"tvdb_api_key" yaml:"tvdb_api_key"`
	TVDBPIN                 string                `mapstructure:"tvdb_pin" yaml:"tvdb_pin"`
	TVDBBaseURL             string                `mapstructure:"tvdb_base_url" yaml:"tvdb_base_url"`
	FFProbePath             string                `mapstructure:"ffprobe_path" yaml:"ffprobe_path"`
	SevenZipPath            string                `mapstructure:"seven_zip_path" yaml:"seven_zip_path"`
	UnrarPath               string                `mapstructure:"unrar_path" yaml:"unrar_path"`
	PAR2Path                string                `mapstructure:"par2_path" yaml:"par2_path"`
	ScrapeLatest            IndexingStageConfig   `mapstructure:"scrape_latest" yaml:"scrape_latest"`
	ScrapeBackfill          IndexingStageConfig   `mapstructure:"scrape_backfill" yaml:"scrape_backfill"`
	Assemble                IndexingStageConfig   `mapstructure:"assemble" yaml:"assemble"`
	Release                 IndexingReleaseConfig `mapstructure:"release" yaml:"release"`
	Match                   IndexingMatchConfig   `mapstructure:"match" yaml:"match"`
	Inspect                 IndexingInspectConfig `mapstructure:"inspect" yaml:"inspect"`
	InspectPAR2             IndexingStageConfig   `mapstructure:"inspect_par2" yaml:"inspect_par2"`
	InspectNFO              IndexingStageConfig   `mapstructure:"inspect_nfo" yaml:"inspect_nfo"`
	InspectArchive          IndexingStageConfig   `mapstructure:"inspect_archive" yaml:"inspect_archive"`
	InspectPassword         IndexingStageConfig   `mapstructure:"inspect_password" yaml:"inspect_password"`
	InspectMedia            IndexingStageConfig   `mapstructure:"inspect_media" yaml:"inspect_media"`
	EnrichPreDB             IndexingPreDBConfig   `mapstructure:"enrich_predb" yaml:"enrich_predb"`
	EnrichTMDB              IndexingTMDBConfig    `mapstructure:"enrich_tmdb" yaml:"enrich_tmdb"`
}

type IndexingStageConfig struct {
	Enabled         *bool    `mapstructure:"enabled" yaml:"enabled"`
	IntervalMinutes *float64 `mapstructure:"interval_minutes" yaml:"interval_minutes"`
	BatchSize       *int     `mapstructure:"batch_size" yaml:"batch_size"`
	Concurrency     *int     `mapstructure:"concurrency" yaml:"concurrency"`
	BackoffSeconds  *int     `mapstructure:"backoff_seconds" yaml:"backoff_seconds"`
}

type IndexingMatchConfig struct {
	HighConfidenceThreshold     *float64 `mapstructure:"high_confidence_threshold" yaml:"high_confidence_threshold"`
	ProbableConfidenceThreshold *float64 `mapstructure:"probable_confidence_threshold" yaml:"probable_confidence_threshold"`
	ArticleBucketSize           *int64   `mapstructure:"article_bucket_size" yaml:"article_bucket_size"`
}

type IndexingReleaseConfig struct {
	Enabled          *bool    `mapstructure:"enabled" yaml:"enabled"`
	IntervalMinutes  *float64 `mapstructure:"interval_minutes" yaml:"interval_minutes"`
	BatchSize        *int     `mapstructure:"batch_size" yaml:"batch_size"`
	Concurrency      *int     `mapstructure:"concurrency" yaml:"concurrency"`
	BackoffSeconds   *int     `mapstructure:"backoff_seconds" yaml:"backoff_seconds"`
	MinConfidence    *float64 `mapstructure:"min_confidence" yaml:"min_confidence"`
	MinCompletionPct *float64 `mapstructure:"min_completion_pct" yaml:"min_completion_pct"`
}

type IndexingInspectConfig struct {
	WorkDir         string `mapstructure:"work_dir" yaml:"work_dir"`
	MaxBytes        int64  `mapstructure:"max_bytes" yaml:"max_bytes"`
	MaxArchiveDepth int    `mapstructure:"max_archive_depth" yaml:"max_archive_depth"`
	ToolTimeoutSecs int    `mapstructure:"tool_timeout_seconds" yaml:"tool_timeout_seconds"`
	FFProbePath     string `mapstructure:"ffprobe_path" yaml:"ffprobe_path"`
	SevenZipPath    string `mapstructure:"seven_zip_path" yaml:"seven_zip_path"`
	UnrarPath       string `mapstructure:"unrar_path" yaml:"unrar_path"`
	PAR2Path        string `mapstructure:"par2_path" yaml:"par2_path"`
}

type IndexingPreDBConfig struct {
	Enabled            *bool    `mapstructure:"enabled" yaml:"enabled"`
	IntervalMinutes    *float64 `mapstructure:"interval_minutes" yaml:"interval_minutes"`
	BatchSize          *int     `mapstructure:"batch_size" yaml:"batch_size"`
	Concurrency        *int     `mapstructure:"concurrency" yaml:"concurrency"`
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
	Concurrency        *int     `mapstructure:"concurrency" yaml:"concurrency"`
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
	WebUI         ModuleToggle `mapstructure:"web_ui" yaml:"web_ui"`
	API           ModuleToggle `mapstructure:"api" yaml:"api"`
}

type ModuleToggle struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
}

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
	v.SetDefault("store.payload_cache_enabled", true)
	v.SetDefault("store.search_persistence_enabled", true)

	v.SetDefault("store.pg_dsn", "")
	v.SetDefault("indexing.newsgroups", []string{})
	v.SetDefault("indexing.scrape_batch_size", 5000)
	v.SetDefault("indexing.schedule_interval_minutes", 10.0)
	v.SetDefault("indexing.release_min_confidence", 0.55)
	v.SetDefault("indexing.release_min_completion_pct", 0.0)
	v.SetDefault("indexing.release.min_confidence", 0.55)
	v.SetDefault("indexing.release.min_completion_pct", 0.0)
	v.SetDefault("indexing.inspect_work_dir", "/store/indexer/inspect")
	v.SetDefault("indexing.inspect_max_bytes", int64(2*1024*1024*1024))
	v.SetDefault("indexing.inspect_max_archive_depth", 3)
	v.SetDefault("indexing.inspect_tool_timeout_seconds", 30)
	v.SetDefault("indexing.enable_inspect_par2", true)
	v.SetDefault("indexing.enable_inspect_nfo", true)
	v.SetDefault("indexing.enable_inspect_archive", true)
	v.SetDefault("indexing.enable_inspect_password", true)
	v.SetDefault("indexing.enable_inspect_media", true)
	v.SetDefault("indexing.enable_enrich_predb", true)
	v.SetDefault("indexing.enable_enrich_tmdb", true)
	v.SetDefault("indexing.predb_provider", "club,me")
	v.SetDefault("indexing.predb_base_url", "https://predb.club/api/v1")
	v.SetDefault("indexing.predb_feed_url", "https://predb.me/?rss=1")
	v.SetDefault("indexing.predb_dump_url", "")
	v.SetDefault("indexing.tmdb_api_key", "")
	v.SetDefault("indexing.tmdb_access_token", "")
	v.SetDefault("indexing.tmdb_base_url", "https://api.themoviedb.org/3")
	v.SetDefault("indexing.tvdb_api_key", "")
	v.SetDefault("indexing.tvdb_pin", "")
	v.SetDefault("indexing.tvdb_base_url", "https://api4.thetvdb.com/v4")
	v.SetDefault("indexing.ffprobe_path", "ffprobe")
	v.SetDefault("indexing.seven_zip_path", "7z")
	v.SetDefault("indexing.unrar_path", "unrar")
	v.SetDefault("indexing.par2_path", "par2")

	v.SetDefault("modules.downloader.enabled", true)
	v.SetDefault("modules.aggregator.enabled", true)
	v.SetDefault("modules.usenet_indexer.enabled", true)
	v.SetDefault("modules.web_ui.enabled", true)
	v.SetDefault("modules.api.enabled", true)

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

	if c.Indexing.ScrapeBatchSize <= 0 {
		c.Indexing.ScrapeBatchSize = 5000
	}

	if c.Indexing.ScheduleIntervalMinutes <= 0 {
		c.Indexing.ScheduleIntervalMinutes = 10
	}
	if c.Indexing.ReleaseMinConfidence <= 0 {
		c.Indexing.ReleaseMinConfidence = 0.55
	}
	if c.Indexing.ReleaseMinConfidence > 1 {
		return errors.New("indexing.release_min_confidence must be between 0 and 1")
	}
	if c.Indexing.ReleaseMinCompletionPct < 0 || c.Indexing.ReleaseMinCompletionPct > 100 {
		return errors.New("indexing.release_min_completion_pct must be between 0 and 100")
	}
	if strings.TrimSpace(c.Indexing.InspectWorkDir) == "" {
		c.Indexing.InspectWorkDir = "/store/indexer/inspect"
	}
	if c.Indexing.InspectMaxBytes <= 0 {
		c.Indexing.InspectMaxBytes = 2 * 1024 * 1024 * 1024
	}
	if c.Indexing.InspectMaxArchiveDepth <= 0 {
		c.Indexing.InspectMaxArchiveDepth = 3
	}
	if c.Indexing.InspectToolTimeoutSecs <= 0 {
		c.Indexing.InspectToolTimeoutSecs = 30
	}
	if strings.TrimSpace(c.Indexing.FFProbePath) == "" {
		c.Indexing.FFProbePath = "ffprobe"
	}
	if strings.TrimSpace(c.Indexing.SevenZipPath) == "" {
		c.Indexing.SevenZipPath = "7z"
	}
	if strings.TrimSpace(c.Indexing.UnrarPath) == "" {
		c.Indexing.UnrarPath = "unrar"
	}
	if strings.TrimSpace(c.Indexing.PAR2Path) == "" {
		c.Indexing.PAR2Path = "par2"
	}
	if err := validateIndexingStageConfig("indexing.scrape_latest", c.Indexing.ScrapeLatest); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.scrape_backfill", c.Indexing.ScrapeBackfill); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.assemble", c.Indexing.Assemble); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.release", IndexingStageConfig{
		Enabled:         c.Indexing.Release.Enabled,
		IntervalMinutes: c.Indexing.Release.IntervalMinutes,
		BatchSize:       c.Indexing.Release.BatchSize,
		Concurrency:     c.Indexing.Release.Concurrency,
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
		Concurrency:     c.Indexing.EnrichPreDB.Concurrency,
		BackoffSeconds:  c.Indexing.EnrichPreDB.BackoffSeconds,
	}); err != nil {
		return err
	}
	if err := validateIndexingStageConfig("indexing.enrich_tmdb", IndexingStageConfig{
		Enabled:         c.Indexing.EnrichTMDB.Enabled,
		IntervalMinutes: c.Indexing.EnrichTMDB.IntervalMinutes,
		BatchSize:       c.Indexing.EnrichTMDB.BatchSize,
		Concurrency:     c.Indexing.EnrichTMDB.Concurrency,
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
	if c.Indexing.Inspect.MaxArchiveDepth < 0 {
		return errors.New("indexing.inspect.max_archive_depth must be greater than or equal to 0")
	}
	if c.Indexing.Inspect.ToolTimeoutSecs < 0 {
		return errors.New("indexing.inspect.tool_timeout_seconds must be greater than or equal to 0")
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

	// validate configured newsgroups only when Usenet/NZB Indexer is enabled.
	if c.Modules.UsenetIndexer.Enabled {
		hasGroups := false
		for _, g := range c.Indexing.Newsgroups {
			if strings.TrimSpace(g) != "" {
				hasGroups = true
				break
			}
		}
		if !hasGroups {
			return errors.New("indexing.newsgroups is required when modules.usenet_indexer.enabled is true")
		}
	}

	// NNTP servers are required only when downloader or usenet indexer is enabled.
	if c.Modules.Downloader.Enabled || c.Modules.UsenetIndexer.Enabled {
		if len(c.Servers) == 0 {
			return errors.New("at least one server must be configured when downloader or usenet_indexer is enabled")
		}

		for i, s := range c.Servers {
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
				c.Servers[i].MaxConnection = 10
			}
			if s.Priority == 0 {
				c.Servers[i].Priority = 1
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
	if cfg.Concurrency != nil && *cfg.Concurrency <= 0 {
		return fmt.Errorf("%s.concurrency must be greater than 0", name)
	}
	if cfg.BackoffSeconds != nil && *cfg.BackoffSeconds < 0 {
		return fmt.Errorf("%s.backoff_seconds must be greater than or equal to 0", name)
	}
	return nil
}
