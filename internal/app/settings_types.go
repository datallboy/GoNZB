package app

type RuntimeSettings struct {
	Servers           []ServerRuntimeSettings         `json:"servers,omitempty"`
	DownloaderServers []ServerRuntimeSettings         `json:"downloader_servers,omitempty"`
	IndexerServers    []ServerRuntimeSettings         `json:"indexer_servers,omitempty"`
	Indexers          []IndexerRuntimeSettings        `json:"indexers,omitempty"`
	Aggregator        *AggregatorRuntimeSettings      `json:"aggregator,omitempty"`
	Download          *DownloadRuntimeSettings        `json:"download,omitempty"`
	Indexing          *IndexingRuntimeSettings        `json:"indexing,omitempty"`
	ArrIntegrations   []ArrIntegrationRuntimeSettings `json:"arr_integrations,omitempty"`
	Revision          int64                           `json:"revision,omitempty"`
}

type RuntimeSettingsPatch struct {
	Servers           *[]ServerRuntimeSettings         `json:"servers,omitempty"`
	DownloaderServers *[]ServerRuntimeSettings         `json:"downloader_servers,omitempty"`
	IndexerServers    *[]ServerRuntimeSettings         `json:"indexer_servers,omitempty"`
	Indexers          *[]IndexerRuntimeSettings        `json:"indexers,omitempty"`
	Aggregator        *AggregatorRuntimeSettings       `json:"aggregator,omitempty"`
	Download          *DownloadRuntimeSettings         `json:"download,omitempty"`
	Indexing          *IndexingRuntimeSettings         `json:"indexing,omitempty"`
	ArrIntegrations   *[]ArrIntegrationRuntimeSettings `json:"arr_integrations,omitempty"`
}

type ServerRuntimeSettings struct {
	ID                     string `json:"id"`
	Host                   string `json:"host"`
	Port                   int    `json:"port"`
	Username               string `json:"username"`
	Password               string `json:"password"`
	TLS                    bool   `json:"tls"`
	MaxConnection          int    `json:"max_connections"`
	Priority               int    `json:"priority"`
	DialTimeoutSeconds     int    `json:"dial_timeout_seconds"`
	TCPKeepAliveSeconds    int    `json:"tcp_keepalive_seconds"`
	PoolIdleTimeoutSeconds int    `json:"pool_idle_timeout_seconds"`
	PoolMaxAgeSeconds      int    `json:"pool_max_age_seconds"`
	EnablePoolLogging      bool   `json:"enable_pool_logging"`
}

type IndexerRuntimeSettings struct {
	ID       string `json:"id"`
	BaseURL  string `json:"base_url"`
	APIPath  string `json:"api_path"`
	APIKey   string `json:"api_key"`
	Redirect bool   `json:"redirect"`
}

type AggregatorRuntimeSettings struct {
	Sources AggregatorSourcesRuntimeSettings `json:"sources,omitempty"`
}

type AggregatorSourcesRuntimeSettings struct {
	LocalBlob     RuntimeToggle `json:"local_blob,omitempty"`
	UsenetIndexer RuntimeToggle `json:"usenet_indexer,omitempty"`
}

type RuntimeToggle struct {
	Enabled bool `json:"enabled"`
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
	Enabled                                         bool    `json:"enabled,omitempty"`
	IntervalMinutes                                 float64 `json:"interval_minutes,omitempty"`
	BatchSize                                       int     `json:"batch_size,omitempty"`
	BackoffSeconds                                  int     `json:"backoff_seconds,omitempty"`
	MinConfidence                                   float64 `json:"min_confidence,omitempty"`
	MinCompletionPct                                float64 `json:"min_completion_pct,omitempty"`
	MinExpectedFileCoveragePct                      float64 `json:"min_expected_file_coverage_pct,omitempty"`
	RequireExpectedFileCountForContextualObfuscated bool    `json:"require_expected_file_count_for_contextual_obfuscated,omitempty"`
}

type IndexingMatchRuntimeSettings struct {
	HighConfidenceThreshold     float64 `json:"high_confidence_threshold,omitempty"`
	ProbableConfidenceThreshold float64 `json:"probable_confidence_threshold,omitempty"`
	ArticleBucketSize           int64   `json:"article_bucket_size,omitempty"`
}

type IndexingInspectRuntimeSettings struct {
	WorkDir          string   `json:"work_dir,omitempty"`
	WorkspaceBackend string   `json:"workspace_backend,omitempty"`
	MemoryWorkDir    string   `json:"memory_work_dir,omitempty"`
	MaxBytes         int64    `json:"max_bytes,omitempty"`
	MinBinaryBytes   int64    `json:"min_binary_bytes,omitempty"`
	MaxBinaryBytes   int64    `json:"max_binary_bytes,omitempty"`
	BlockedMagicHex  []string `json:"blocked_magic_hex,omitempty"`
	MaxArchiveDepth  int      `json:"max_archive_depth,omitempty"`
	ToolTimeoutSecs  int      `json:"tool_timeout_seconds,omitempty"`
	FFProbePath      string   `json:"ffprobe_path,omitempty"`
	SevenZipPath     string   `json:"seven_zip_path,omitempty"`
	UnrarPath        string   `json:"unrar_path,omitempty"`
	PAR2Path         string   `json:"par2_path,omitempty"`
}

type IndexingPreDBRuntimeSettings struct {
	Enabled            bool    `json:"enabled,omitempty"`
	IntervalMinutes    float64 `json:"interval_minutes,omitempty"`
	BatchSize          int     `json:"batch_size,omitempty"`
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
	Newsgroups               []string                       `json:"newsgroups,omitempty"`
	BackfillUntilDateByGroup map[string]string              `json:"backfill_until_date_by_group,omitempty"`
	ScrapeLatest             IndexingStageRuntimeSettings   `json:"scrape_latest,omitempty"`
	ScrapeBackfill           IndexingStageRuntimeSettings   `json:"scrape_backfill,omitempty"`
	Assemble                 IndexingStageRuntimeSettings   `json:"assemble,omitempty"`
	AssembleLaneA            IndexingStageRuntimeSettings   `json:"assemble_lane_a,omitempty"`
	AssembleLaneB            IndexingStageRuntimeSettings   `json:"assemble_lane_b,omitempty"`
	RecoverYEnc              IndexingStageRuntimeSettings   `json:"recover_yenc,omitempty"`
	Release                  IndexingReleaseRuntimeSettings `json:"release,omitempty"`
	Match                    IndexingMatchRuntimeSettings   `json:"match,omitempty"`
	Inspect                  IndexingInspectRuntimeSettings `json:"inspect,omitempty"`
	InspectDiscovery         IndexingStageRuntimeSettings   `json:"inspect_discovery,omitempty"`
	InspectPAR2              IndexingStageRuntimeSettings   `json:"inspect_par2,omitempty"`
	InspectNFO               IndexingStageRuntimeSettings   `json:"inspect_nfo,omitempty"`
	InspectArchive           IndexingStageRuntimeSettings   `json:"inspect_archive,omitempty"`
	InspectPassword          IndexingStageRuntimeSettings   `json:"inspect_password,omitempty"`
	InspectMedia             IndexingStageRuntimeSettings   `json:"inspect_media,omitempty"`
	EnrichPreDB              IndexingPreDBRuntimeSettings   `json:"enrich_predb,omitempty"`
	EnrichTMDB               IndexingTMDBRuntimeSettings    `json:"enrich_tmdb,omitempty"`
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

type ControlPlaneCapabilities struct {
	Modules  map[string]ModuleCapability `json:"modules"`
	Settings SettingsCapability          `json:"settings"`
	Revision int64                       `json:"revision,omitempty"`
}

type ModuleCapability struct {
	Enabled      bool     `json:"enabled"`
	Configured   bool     `json:"configured"`
	Ready        bool     `json:"ready"`
	Visible      bool     `json:"visible"`
	Reason       string   `json:"reason,omitempty"`
	Requirements []string `json:"requirements,omitempty"`
}

type SettingsCapability struct {
	RuntimeConfigured bool `json:"runtime_configured"`
}
