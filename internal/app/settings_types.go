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

type IndexingRuntimeSettings struct {
	Newsgroups              []string `json:"newsgroups,omitempty"`
	ScrapeBatchSize         int64    `json:"scrape_batch_size,omitempty"`
	ScheduleIntervalMinutes int      `json:"schedule_interval_minutes,omitempty"`
	ReleaseMinConfidence    float64  `json:"release_min_confidence,omitempty"`
	InspectWorkDir          string   `json:"inspect_work_dir,omitempty"`
	InspectMaxBytes         int64    `json:"inspect_max_bytes,omitempty"`
	InspectMaxArchiveDepth  int      `json:"inspect_max_archive_depth,omitempty"`
	InspectToolTimeoutSecs  int      `json:"inspect_tool_timeout_seconds,omitempty"`
	EnableInspectPAR2       bool     `json:"enable_inspect_par2,omitempty"`
	EnableInspectNFO        bool     `json:"enable_inspect_nfo,omitempty"`
	EnableInspectArchive    bool     `json:"enable_inspect_archive,omitempty"`
	EnableInspectPassword   bool     `json:"enable_inspect_password,omitempty"`
	EnableInspectMedia      bool     `json:"enable_inspect_media,omitempty"`
	EnableEnrichPreDB       bool     `json:"enable_enrich_predb,omitempty"`
	EnableEnrichTMDB        bool     `json:"enable_enrich_tmdb,omitempty"`
	FFProbePath             string   `json:"ffprobe_path,omitempty"`
	SevenZipPath            string   `json:"seven_zip_path,omitempty"`
	UnrarPath               string   `json:"unrar_path,omitempty"`
	PAR2Path                string   `json:"par2_path,omitempty"`
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
