package app

type RuntimeSettings struct {
	Servers         []ServerRuntimeSettings         `json:"servers,omitempty"`
	Indexers        []IndexerRuntimeSettings        `json:"indexers,omitempty"`
	Download        *DownloadRuntimeSettings        `json:"download,omitempty"`
	Indexing        *IndexingRuntimeSettings        `json:"indexing,omitempty"`
	ArrIntegrations []ArrIntegrationRuntimeSettings `json:"arr_integrations,omitempty"`
	Revision        int64                           `json:"revision,omitempty"`
}

type RuntimeSettingsPatch struct {
	Servers         *[]ServerRuntimeSettings         `json:"servers,omitempty"`
	Indexers        *[]IndexerRuntimeSettings        `json:"indexers,omitempty"`
	Download        *DownloadRuntimeSettings         `json:"download,omitempty"`
	Indexing        *IndexingRuntimeSettings         `json:"indexing,omitempty"`
	ArrIntegrations *[]ArrIntegrationRuntimeSettings `json:"arr_integrations,omitempty"`
}

type ServerRuntimeSettings struct {
	ID            string `json:"id"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	TLS           bool   `json:"tls"`
	MaxConnection int    `json:"max_connections"`
	Priority      int    `json:"priority"`
}

type IndexerRuntimeSettings struct {
	ID       string `json:"id"`
	BaseURL  string `json:"base_url"`
	APIPath  string `json:"api_path"`
	APIKey   string `json:"api_key"`
	Redirect bool   `json:"redirect"`
}

type DownloadRuntimeSettings struct {
	OutDir            string   `json:"out_dir"`
	CompletedDir      string   `json:"completed_dir"`
	CleanupExtensions []string `json:"cleanup_extensions"`
}

type IndexingStageRuntimeSettings struct {
	Enabled         bool    `json:"enabled,omitempty"`
	IntervalMinutes float64 `json:"interval_minutes,omitempty"`
	BatchSize       int     `json:"batch_size,omitempty"`
	Concurrency     int     `json:"concurrency,omitempty"`
	BackoffSeconds  int     `json:"backoff_seconds,omitempty"`
}

type IndexingReleaseRuntimeSettings struct {
	Enabled          bool    `json:"enabled,omitempty"`
	IntervalMinutes  float64 `json:"interval_minutes,omitempty"`
	BatchSize        int     `json:"batch_size,omitempty"`
	Concurrency      int     `json:"concurrency,omitempty"`
	BackoffSeconds   int     `json:"backoff_seconds,omitempty"`
	MinConfidence    float64 `json:"min_confidence,omitempty"`
	MinCompletionPct float64 `json:"min_completion_pct,omitempty"`
}

type IndexingMatchRuntimeSettings struct {
	HighConfidenceThreshold     float64 `json:"high_confidence_threshold,omitempty"`
	ProbableConfidenceThreshold float64 `json:"probable_confidence_threshold,omitempty"`
	ArticleBucketSize           int64   `json:"article_bucket_size,omitempty"`
}

type IndexingInspectRuntimeSettings struct {
	WorkDir         string `json:"work_dir,omitempty"`
	MaxBytes        int64  `json:"max_bytes,omitempty"`
	MaxArchiveDepth int    `json:"max_archive_depth,omitempty"`
	ToolTimeoutSecs int    `json:"tool_timeout_seconds,omitempty"`
	FFProbePath     string `json:"ffprobe_path,omitempty"`
	SevenZipPath    string `json:"seven_zip_path,omitempty"`
	UnrarPath       string `json:"unrar_path,omitempty"`
	PAR2Path        string `json:"par2_path,omitempty"`
}

type IndexingPreDBRuntimeSettings struct {
	Enabled            bool    `json:"enabled,omitempty"`
	IntervalMinutes    float64 `json:"interval_minutes,omitempty"`
	BatchSize          int     `json:"batch_size,omitempty"`
	Concurrency        int     `json:"concurrency,omitempty"`
	BackoffSeconds     int     `json:"backoff_seconds,omitempty"`
	Provider           string  `json:"provider,omitempty"`
	BaseURL            string  `json:"base_url,omitempty"`
	FeedURL            string  `json:"feed_url,omitempty"`
	DumpURL            string  `json:"dump_url,omitempty"`
	HTTPTimeoutSeconds int     `json:"http_timeout_seconds,omitempty"`
	BackfillPageSize   int     `json:"backfill_page_size,omitempty"`
	MaxBackfillPages   int     `json:"max_backfill_pages,omitempty"`
}

type IndexingTMDBRuntimeSettings struct {
	Enabled            bool    `json:"enabled,omitempty"`
	IntervalMinutes    float64 `json:"interval_minutes,omitempty"`
	BatchSize          int     `json:"batch_size,omitempty"`
	Concurrency        int     `json:"concurrency,omitempty"`
	BackoffSeconds     int     `json:"backoff_seconds,omitempty"`
	HTTPTimeoutSeconds int     `json:"http_timeout_seconds,omitempty"`
	TMDBAPIKey         string  `json:"tmdb_api_key,omitempty"`
	TMDBAccessToken    string  `json:"tmdb_access_token,omitempty"`
	TMDBBaseURL        string  `json:"tmdb_base_url,omitempty"`
	TVDBAPIKey         string  `json:"tvdb_api_key,omitempty"`
	TVDBPIN            string  `json:"tvdb_pin,omitempty"`
	TVDBBaseURL        string  `json:"tvdb_base_url,omitempty"`
}

type IndexingRuntimeSettings struct {
	Newsgroups              []string                       `json:"newsgroups,omitempty"`
	ScrapeBatchSize         int64                          `json:"scrape_batch_size,omitempty"`
	ScheduleIntervalMinutes float64                        `json:"schedule_interval_minutes,omitempty"`
	ReleaseMinConfidence    float64                        `json:"release_min_confidence,omitempty"`
	ReleaseMinCompletionPct float64                        `json:"release_min_completion_pct,omitempty"`
	InspectWorkDir          string                         `json:"inspect_work_dir,omitempty"`
	InspectMaxBytes         int64                          `json:"inspect_max_bytes,omitempty"`
	InspectMaxArchiveDepth  int                            `json:"inspect_max_archive_depth,omitempty"`
	InspectToolTimeoutSecs  int                            `json:"inspect_tool_timeout_seconds,omitempty"`
	EnableInspectPAR2       bool                           `json:"enable_inspect_par2,omitempty"`
	EnableInspectNFO        bool                           `json:"enable_inspect_nfo,omitempty"`
	EnableInspectArchive    bool                           `json:"enable_inspect_archive,omitempty"`
	EnableInspectPassword   bool                           `json:"enable_inspect_password,omitempty"`
	EnableInspectMedia      bool                           `json:"enable_inspect_media,omitempty"`
	EnableEnrichPreDB       bool                           `json:"enable_enrich_predb,omitempty"`
	EnableEnrichTMDB        bool                           `json:"enable_enrich_tmdb,omitempty"`
	PreDBProvider           string                         `json:"predb_provider,omitempty"`
	PreDBBaseURL            string                         `json:"predb_base_url,omitempty"`
	PreDBFeedURL            string                         `json:"predb_feed_url,omitempty"`
	PreDBDumpURL            string                         `json:"predb_dump_url,omitempty"`
	TMDBAPIKey              string                         `json:"tmdb_api_key,omitempty"`
	TMDBAccessToken         string                         `json:"tmdb_access_token,omitempty"`
	TMDBBaseURL             string                         `json:"tmdb_base_url,omitempty"`
	TVDBAPIKey              string                         `json:"tvdb_api_key,omitempty"`
	TVDBPIN                 string                         `json:"tvdb_pin,omitempty"`
	TVDBBaseURL             string                         `json:"tvdb_base_url,omitempty"`
	FFProbePath             string                         `json:"ffprobe_path,omitempty"`
	SevenZipPath            string                         `json:"seven_zip_path,omitempty"`
	UnrarPath               string                         `json:"unrar_path,omitempty"`
	PAR2Path                string                         `json:"par2_path,omitempty"`
	ScrapeLatest            IndexingStageRuntimeSettings   `json:"scrape_latest,omitempty"`
	ScrapeBackfill          IndexingStageRuntimeSettings   `json:"scrape_backfill,omitempty"`
	Assemble                IndexingStageRuntimeSettings   `json:"assemble,omitempty"`
	Release                 IndexingReleaseRuntimeSettings `json:"release,omitempty"`
	Match                   IndexingMatchRuntimeSettings   `json:"match,omitempty"`
	Inspect                 IndexingInspectRuntimeSettings `json:"inspect,omitempty"`
	InspectPAR2             IndexingStageRuntimeSettings   `json:"inspect_par2,omitempty"`
	InspectNFO              IndexingStageRuntimeSettings   `json:"inspect_nfo,omitempty"`
	InspectArchive          IndexingStageRuntimeSettings   `json:"inspect_archive,omitempty"`
	InspectPassword         IndexingStageRuntimeSettings   `json:"inspect_password,omitempty"`
	InspectMedia            IndexingStageRuntimeSettings   `json:"inspect_media,omitempty"`
	EnrichPreDB             IndexingPreDBRuntimeSettings   `json:"enrich_predb,omitempty"`
	EnrichTMDB              IndexingTMDBRuntimeSettings    `json:"enrich_tmdb,omitempty"`
}

type ArrIntegrationRuntimeSettings struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Enabled    bool   `json:"enabled"`
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
	ClientName string `json:"client_name,omitempty"`
	Category   string `json:"category,omitempty"`
}
