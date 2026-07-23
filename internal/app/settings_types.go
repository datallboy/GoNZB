package app

type RuntimeSettings struct {
	Servers           []ServerRuntimeSettings         `json:"servers,omitempty"`
	DownloaderServers []ServerRuntimeSettings         `json:"downloader_servers,omitempty"`
	IndexerServers    []ServerRuntimeSettings         `json:"indexer_servers,omitempty"`
	Indexers          []IndexerRuntimeSettings        `json:"indexers,omitempty"`
	Aggregator        *AggregatorRuntimeSettings      `json:"aggregator,omitempty"`
	GoNZBNet          *GoNZBNetRuntimeSettings        `json:"gonzbnet,omitempty"`
	Download          *DownloadRuntimeSettings        `json:"download,omitempty"`
	NNTPPool          *NNTPPoolRuntimeSettings        `json:"nntp_pool,omitempty"`
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
	GoNZBNet          *GoNZBNetRuntimeSettings         `json:"gonzbnet,omitempty"`
	Download          *DownloadRuntimeSettings         `json:"download,omitempty"`
	NNTPPool          *NNTPPoolRuntimeSettings         `json:"nntp_pool,omitempty"`
	Indexing          *IndexingRuntimeSettings         `json:"indexing,omitempty"`
	ArrIntegrations   *[]ArrIntegrationRuntimeSettings `json:"arr_integrations,omitempty"`
}

type ServerRuntimeSettings struct {
	ID                     string   `json:"id"`
	Host                   string   `json:"host"`
	Port                   int      `json:"port"`
	Username               string   `json:"username"`
	Password               string   `json:"password"`
	TLS                    bool     `json:"tls"`
	MaxConnection          int      `json:"max_connections"`
	Priority               int      `json:"priority"`
	DialTimeoutSeconds     int      `json:"dial_timeout_seconds"`
	TCPKeepAliveSeconds    int      `json:"tcp_keepalive_seconds"`
	PoolIdleTimeoutSeconds int      `json:"pool_idle_timeout_seconds"`
	PoolMaxAgeSeconds      int      `json:"pool_max_age_seconds"`
	EnablePoolLogging      bool     `json:"enable_pool_logging"`
	Roles                  []string `json:"roles,omitempty"`
}

type IndexerRuntimeSettings struct {
	ID                    string   `json:"id"`
	BaseURL               string   `json:"base_url"`
	APIPath               string   `json:"api_path"`
	APIKey                string   `json:"api_key"`
	Redirect              bool     `json:"redirect"`
	AllowPrivateAddresses bool     `json:"allow_private_addresses"`
	AllowedCIDRs          []string `json:"allowed_cidrs"`
}

type AggregatorRuntimeSettings struct {
	Sources AggregatorSourcesRuntimeSettings `json:"sources,omitempty"`
}

type AggregatorSourcesRuntimeSettings struct {
	LocalBlob     RuntimeToggle `json:"local_blob,omitempty"`
	UsenetIndexer RuntimeToggle `json:"usenet_indexer,omitempty"`
	GoNZBNet      RuntimeToggle `json:"gonzbnet,omitempty"`
}

type RuntimeToggle struct {
	Enabled bool `json:"enabled"`
}

// GoNZBNetRuntimeSettings contains operational federation settings that can be
// safely persisted and applied without changing listener, database, protocol,
// network identity, or private-key bootstrap boundaries.
type GoNZBNetRuntimeSettings struct {
	NodeAlias                      string   `json:"node_alias"`
	AdvertiseURL                   string   `json:"advertise_url"`
	AllowInsecurePeerHTTP          bool     `json:"allow_insecure_peer_http"`
	PublishPoolIDs                 []string `json:"publish_pool_ids"`
	ManualPeers                    []string `json:"manual_peers"`
	Visibility                     string   `json:"visibility"`
	AllowPoolCreation              bool     `json:"allow_pool_creation"`
	AllowJoinRequests              bool     `json:"allow_join_requests"`
	AdmissionRelayEnabled          bool     `json:"admission_relay_enabled"`
	ConsumerEnabled                bool     `json:"consumer_enabled"`
	ScannerEnabled                 bool     `json:"scanner_enabled"`
	IndexProjectionEnabled         bool     `json:"index_projection_enabled"`
	ManifestBuilderEnabled         bool     `json:"manifest_builder_enabled"`
	ManifestCacheEnabled           bool     `json:"manifest_cache_enabled"`
	ValidatorEnabled               bool     `json:"validator_enabled"`
	HealthCheckerEnabled           bool     `json:"health_checker_enabled"`
	CoverageEnabled                bool     `json:"coverage_enabled"`
	SchedulerEnabled               bool     `json:"scheduler_enabled"`
	PublishReleaseCardsEnabled     bool     `json:"publish_release_cards_enabled"`
	PublishReleaseCardsBatchSize   int      `json:"publish_release_cards_batch_size"`
	PublishReleaseCardsIntervalMin float64  `json:"publish_release_cards_interval_minutes"`
	ManifestAvailabilityEnabled    bool     `json:"manifest_availability_enabled"`
	HealthAttestationsEnabled      bool     `json:"health_attestations_enabled"`
	HealthAttestationsBatchSize    int      `json:"health_attestations_batch_size"`
	HealthAttestationsIntervalMin  float64  `json:"health_attestations_interval_minutes"`
	ScannerMaxGroups               int      `json:"scanner_max_groups"`
	ScannerMaxArticlesPerHour      int64    `json:"scanner_max_articles_per_hour"`
	ScannerClaimTTLMinutes         int      `json:"scanner_claim_ttl_minutes"`
	ScannerCheckpointIntervalSecs  int      `json:"scanner_checkpoint_interval_seconds"`
	ScannerRespectRemoteClaims     bool     `json:"scanner_respect_remote_claims"`
	ScannerAllowUnassignedWork     bool     `json:"scanner_allow_unassigned_work"`
	CoverageMode                   string   `json:"coverage_mode"`
	CoverageMinTrustForClaim       float64  `json:"coverage_min_trust_for_claim"`
	CoverageValidationOverlapPct   int      `json:"coverage_validation_overlap_percent"`
	CoverageStaleClaimPenalty      bool     `json:"coverage_stale_claim_penalty"`
	CoverageProviderScopeMode      string   `json:"coverage_provider_scope_mode"`
	ValidationBatchSize            int      `json:"validation_batch_size"`
	ValidationIntervalMin          float64  `json:"validation_interval_minutes"`
	ValidationTiers                []string `json:"validation_tiers"`
	ValidationMaxManifestsPerHour  int      `json:"validation_max_manifests_per_hour"`
	ValidationSamplePercent        int      `json:"validation_sample_percent"`
	ValidationAllowSamplePayload   bool     `json:"validation_allow_sample_payload_fetch"`
	ValidationAllowPAR2            bool     `json:"validation_allow_par2_validation"`
	ValidationPublishProviderScope bool     `json:"validation_publish_provider_scope_hash"`
	ChecksumValidationEnabled      bool     `json:"checksum_validation_enabled"`
	ManifestCacheMaxBytes          int64    `json:"manifest_cache_max_bytes"`
	ManifestCacheTTLDays           int      `json:"manifest_cache_ttl_days"`
	ManifestCacheServeTrustedPools bool     `json:"manifest_cache_serve_to_trusted_pools"`
	PullSyncEnabled                bool     `json:"pull_sync_enabled"`
	PullSyncIntervalMin            float64  `json:"pull_sync_interval_minutes"`
	PushSyncEnabled                bool     `json:"push_sync_enabled"`
	PushSyncIntervalMin            float64  `json:"push_sync_interval_minutes"`
	PushSyncBatchSize              int      `json:"push_sync_batch_size"`
	WebSocketGossipEnabled         bool     `json:"websocket_gossip_enabled"`
	GossipIntervalMin              float64  `json:"gossip_interval_minutes"`
	GossipBatchSize                int      `json:"gossip_batch_size"`
	GossipTTL                      int      `json:"gossip_ttl"`
	GossipFanout                   int      `json:"gossip_fanout"`
	PeerExchangeEnabled            bool     `json:"peer_exchange_enabled"`
	RelayEnabled                   bool     `json:"relay_enabled"`
	MaxEventBytes                  int      `json:"max_event_bytes"`
	MaxManifestBytes               int      `json:"max_manifest_bytes"`
	ManifestFetchTimeoutSeconds    int      `json:"manifest_fetch_timeout_seconds"`
	MaxBatchEvents                 int      `json:"max_batch_events"`
	RateLimitEventsPerMinute       int      `json:"rate_limit_events_per_minute"`
	TimeToleranceSeconds           int      `json:"time_tolerance_seconds"`
	MaxEventAgeHours               int      `json:"max_event_age_hours"`
	NonceTTLSeconds                int      `json:"nonce_ttl_seconds"`
	ShareProviderBackbone          bool     `json:"share_provider_backbone_hash"`
	ShareSourceIndexer             bool     `json:"share_source_indexer_hash"`
}

type DownloadRuntimeSettings struct {
	OutDir            string   `json:"out_dir"`
	CompletedDir      string   `json:"completed_dir"`
	CleanupExtensions []string `json:"cleanup_extensions"`
}

type NNTPPoolRuntimeSettings struct {
	IdleBorrowEnabled         bool `json:"idle_borrow_enabled"`
	IndexerMaxPercent         int  `json:"indexer_max_percent"`
	IndexerStageTargetPercent int  `json:"indexer_stage_target_percent"`
	DownloaderReservePercent  int  `json:"downloader_reserve_percent"`
	DemandWindowSeconds       int  `json:"demand_window_seconds"`
}

type IndexingStageRuntimeSettings struct {
	Enabled                 bool    `json:"enabled,omitempty"`
	IntervalMinutes         float64 `json:"interval_minutes,omitempty"`
	BatchSize               int     `json:"batch_size,omitempty"`
	MaxBatches              int     `json:"max_batches,omitempty"`
	Concurrency             int     `json:"concurrency,omitempty"`
	MaxEffectiveConcurrency int     `json:"max_effective_concurrency,omitempty"`
	BackoffSeconds          int     `json:"backoff_seconds,omitempty"`
	BinaryUpsertDBChunkSize int     `json:"binary_upsert_db_chunk_size,omitempty"`
	LaneATargetPct          int     `json:"lane_a_target_pct,omitempty"`
	LaneBMinPct             int     `json:"lane_b_min_pct,omitempty"`
	LaneATimeWindowMinutes  int     `json:"lane_a_time_window_minutes,omitempty"`
	TargetWindowEnabled     bool    `json:"target_window_enabled,omitempty"`
	TargetWindowStart       string  `json:"target_window_start,omitempty"`
	TargetWindowEnd         string  `json:"target_window_end,omitempty"`
	TargetWindowPct         int     `json:"target_window_pct,omitempty"`
	FetchTimeoutSeconds     int     `json:"fetch_timeout_seconds,omitempty"`
	NewestPct               int     `json:"newest_pct"`
}

type IndexingSourceWindowRuntimeSettings struct {
	Enabled            bool `json:"enabled,omitempty"`
	WindowMinutes      int  `json:"window_minutes,omitempty"`
	BackfillWindowDays int  `json:"backfill_window_days,omitempty"`
	MaxOpenHeaders     int  `json:"max_open_headers,omitempty"`
	ResumeOpenHeaders  int  `json:"resume_open_headers,omitempty"`
	MaxBlockingYEnc    int  `json:"max_blocking_yenc,omitempty"`
	ResumeBlockingYEnc int  `json:"resume_blocking_yenc,omitempty"`
}

type IndexingRetentionRuntimeSettings struct {
	RawStageHotHours                int  `json:"raw_stage_hot_hours,omitempty"`
	RawStageWarmHours               int  `json:"raw_stage_warm_hours,omitempty"`
	RawStageColdHours               int  `json:"raw_stage_cold_hours,omitempty"`
	FailedProbeHours                int  `json:"failed_probe_hours,omitempty"`
	ArchivedReleaseDetailGraceHours int  `json:"archived_release_detail_grace_hours,omitempty"`
	MetadataIncompleteReleaseHours  int  `json:"metadata_incomplete_release_hours,omitempty"`
	CreatePartitionsDaysBefore      int  `json:"create_partitions_days_before,omitempty"`
	CreatePartitionsDaysAhead       int  `json:"create_partitions_days_ahead,omitempty"`
	SourceSettleHours               int  `json:"source_settle_hours,omitempty"`
	NoYieldGraceDays                int  `json:"no_yield_grace_days,omitempty"`
	YEncTerminalAttempts            int  `json:"yenc_terminal_attempts,omitempty"`
	ExecuteOutcomePurge             bool `json:"execute_outcome_purge,omitempty"`
	PurgeDryRunDefault              bool `json:"purge_dry_run_default,omitempty"`
}

type IndexingPartitionRuntimeSettings struct {
	PrecreateDaysAhead      int `json:"precreate_days_ahead,omitempty"`
	MaxNewSourceDaysPerPass int `json:"max_new_source_days_per_pass,omitempty"`
	DDLLockTimeoutSeconds   int `json:"ddl_lock_timeout_seconds,omitempty"`
}

type IndexingRecoveryAdmissionRuntimeSettings struct {
	TargetHotLagHours           int `json:"target_hot_lag_hours,omitempty"`
	TargetWarmLagHours          int `json:"target_warm_lag_hours,omitempty"`
	SoftQueueHours              int `json:"soft_queue_hours,omitempty"`
	HardQueueMultiplier         int `json:"hard_queue_multiplier,omitempty"`
	AbsoluteHardQueueCap        int `json:"absolute_hard_queue_cap,omitempty"`
	EWMAWindowMinutes           int `json:"ewma_window_minutes,omitempty"`
	BootstrapProbesPerHour      int `json:"bootstrap_probes_per_hour,omitempty"`
	Priority0OverflowCap        int `json:"priority0_overflow_cap,omitempty"`
	Priority0ReservoirBatches   int `json:"priority0_reservoir_batches,omitempty"`
	NearTimeCohortBucketMinutes int `json:"near_time_cohort_bucket_minutes,omitempty"`
	LatestReservePercent        int `json:"latest_reserve_percent,omitempty"`
}

type IndexingScrapeTierRuntimeSettings struct {
	HotWindowMinutes          int  `json:"hot_window_minutes,omitempty"`
	WarmWindowMinutes         int  `json:"warm_window_minutes,omitempty"`
	ColdSampleHeaders         int  `json:"cold_sample_headers,omitempty"`
	MaxArticlesPerGroupWindow int  `json:"max_articles_per_group_window,omitempty"`
	AssembleBacklogHighWater  int  `json:"assemble_backlog_high_water,omitempty"`
	AssembleBacklogLowWater   int  `json:"assemble_backlog_low_water,omitempty"`
	AllowGlobalDailyGate      bool `json:"allow_global_daily_gate,omitempty"`
}

type IndexingDeferredBackfillRuntimeSettings struct {
	Enabled                  bool    `json:"enabled,omitempty"`
	MaxRangesPerRun          int     `json:"max_ranges_per_run,omitempty"`
	MaxArticlesPerRangeChunk int     `json:"max_articles_per_range_chunk,omitempty"`
	RunOnlyBelowQueueRatio   float64 `json:"run_only_below_queue_ratio,omitempty"`
}

type IndexingReleaseRuntimeSettings struct {
	Enabled                                         bool    `json:"enabled,omitempty"`
	IntervalMinutes                                 float64 `json:"interval_minutes,omitempty"`
	BatchSize                                       int     `json:"batch_size,omitempty"`
	AutoReformBatchSize                             int     `json:"auto_reform_batch_size,omitempty"`
	BackoffSeconds                                  int     `json:"backoff_seconds,omitempty"`
	MinConfidence                                   float64 `json:"min_confidence,omitempty"`
	MinCompletionPct                                float64 `json:"min_completion_pct,omitempty"`
	MinExpectedFileCoveragePct                      float64 `json:"min_expected_file_coverage_pct,omitempty"`
	RequireExpectedFileCountForContextualObfuscated bool    `json:"require_expected_file_count_for_contextual_obfuscated,omitempty"`
	PublicMinMatchConfidence                        float64 `json:"public_min_match_confidence,omitempty"`
	PublicMinCompletionPct                          float64 `json:"public_min_completion_pct,omitempty"`
	PublicMinIdentityStatus                         string  `json:"public_min_identity_status,omitempty"`
	PublicRequireInspection                         bool    `json:"public_require_inspection,omitempty"`
	PublicRequireEnrichment                         bool    `json:"public_require_enrichment,omitempty"`
	PublicRequireClearTitle                         bool    `json:"public_require_clear_title,omitempty"`
	PublicRequirePayloadComplete                    bool    `json:"public_require_payload_complete,omitempty"`
	PublicRequireExpectedFileCountComplete          bool    `json:"public_require_expected_file_count_complete,omitempty"`
	PublicRequirePAR2                               bool    `json:"public_require_par2,omitempty"`
	PublicRequireNFO                                bool    `json:"public_require_nfo,omitempty"`
	PublicRequireSFV                                bool    `json:"public_require_sfv,omitempty"`
	RetainUntilExpectedFileCountComplete            bool    `json:"retain_until_expected_file_count_complete,omitempty"`
	RetainRequirePAR2                               bool    `json:"retain_require_par2,omitempty"`
	RetainRequireNFO                                bool    `json:"retain_require_nfo,omitempty"`
	RetainRequireSFV                                bool    `json:"retain_require_sfv,omitempty"`
	ReopenArchivedNZBOnReleaseChange                bool    `json:"reopen_archived_nzb_on_release_change,omitempty"`
}

type IndexingMatchRuntimeSettings struct {
	HighConfidenceThreshold     float64 `json:"high_confidence_threshold,omitempty"`
	ProbableConfidenceThreshold float64 `json:"probable_confidence_threshold,omitempty"`
	ArticleBucketSize           int64   `json:"article_bucket_size,omitempty"`
}

type IndexingInspectRuntimeSettings struct {
	WorkDir                  string   `json:"work_dir,omitempty"`
	WorkspaceBackend         string   `json:"workspace_backend,omitempty"`
	MemoryWorkDir            string   `json:"memory_work_dir,omitempty"`
	MaxBytes                 int64    `json:"max_bytes,omitempty"`
	MinBinaryBytes           int64    `json:"min_binary_bytes,omitempty"`
	MaxBinaryBytes           int64    `json:"max_binary_bytes,omitempty"`
	RequireExpectedFileCount bool     `json:"require_expected_file_count,omitempty"`
	BlockedMagicHex          []string `json:"blocked_magic_hex,omitempty"`
	MaxArchiveDepth          int      `json:"max_archive_depth,omitempty"`
	ToolTimeoutSecs          int      `json:"tool_timeout_seconds,omitempty"`
	FFmpegPath               string   `json:"ffmpeg_path,omitempty"`
	FFProbePath              string   `json:"ffprobe_path,omitempty"`
	SevenZipPath             string   `json:"seven_zip_path,omitempty"`
	UnrarPath                string   `json:"unrar_path,omitempty"`
	PAR2Path                 string   `json:"par2_path,omitempty"`
}

type IndexingStorageGuardRuntimeSettings struct {
	Enabled        bool    `json:"enabled,omitempty"`
	DataDirectory  string  `json:"data_directory,omitempty"`
	MinFreeBytes   int64   `json:"min_free_bytes,omitempty"`
	MinFreePercent float64 `json:"min_free_percent,omitempty"`
}

type IndexingMemoryGuardRuntimeSettings struct {
	Enabled             bool    `json:"enabled,omitempty"`
	MinAvailableBytes   int64   `json:"min_available_bytes,omitempty"`
	MinAvailablePercent float64 `json:"min_available_percent,omitempty"`
	MinSwapFreeBytes    int64   `json:"min_swap_free_bytes,omitempty"`
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

type IndexingScrapeGroupRuntimeSettings struct {
	GroupName         string `json:"group_name,omitempty"`
	Enabled           bool   `json:"enabled,omitempty"`
	BackfillUntilDate string `json:"backfill_until_date,omitempty"`
	Source            string `json:"source,omitempty"`
}

type IndexingScrapeTimeframeRuntimeSettings struct {
	ID        string `json:"id,omitempty"`
	GroupName string `json:"group_name,omitempty"`
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
	Enabled   bool   `json:"enabled,omitempty"`
}

type IndexingWildcardRuleRuntimeSettings struct {
	ID      string `json:"id,omitempty"`
	Pattern string `json:"pattern,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}

type IndexingProviderGroupInventoryRuntimeSettings struct {
	ProviderID   string `json:"provider_id,omitempty"`
	ProviderName string `json:"provider_name,omitempty"`
	GroupName    string `json:"group_name,omitempty"`
	High         int64  `json:"high,omitempty"`
	Low          int64  `json:"low,omitempty"`
	Status       string `json:"status,omitempty"`
	ScannedAt    string `json:"scanned_at,omitempty"`
}

type IndexingMaterializedGroupRuntimeSettings struct {
	GroupName         string   `json:"group_name,omitempty"`
	Enabled           bool     `json:"enabled,omitempty"`
	BackfillUntilDate string   `json:"backfill_until_date,omitempty"`
	ProviderIDs       []string `json:"provider_ids,omitempty"`
	RuleIDs           []string `json:"rule_ids,omitempty"`
}

type IndexingRuntimeSettings struct {
	Newsgroups                  []string                                          `json:"newsgroups,omitempty"`
	BackfillUntilDateByGroup    map[string]string                                 `json:"backfill_until_date_by_group,omitempty"`
	ExplicitGroups              []IndexingScrapeGroupRuntimeSettings              `json:"explicit_groups"`
	WildcardRules               []IndexingWildcardRuleRuntimeSettings             `json:"wildcard_rules"`
	ProviderGroupInventory      []IndexingProviderGroupInventoryRuntimeSettings   `json:"provider_group_inventory"`
	MaterializedGroups          []IndexingMaterializedGroupRuntimeSettings        `json:"materialized_groups"`
	ScrapeTimeframes            []IndexingScrapeTimeframeRuntimeSettings          `json:"scrape_timeframes"`
	ScrapeLatest                IndexingStageRuntimeSettings                      `json:"scrape_latest,omitempty"`
	ScrapeBackfill              IndexingStageRuntimeSettings                      `json:"scrape_backfill,omitempty"`
	ScrapeTimeframe             IndexingStageRuntimeSettings                      `json:"scrape_timeframe,omitempty"`
	ScrapeDeferred              IndexingStageRuntimeSettings                      `json:"scrape_deferred,omitempty"`
	PosterMaterialize           IndexingStageRuntimeSettings                      `json:"poster_materialize,omitempty"`
	CrosspostPopularityRefresh  IndexingStageRuntimeSettings                      `json:"crosspost_popularity_refresh,omitempty"`
	ArticleCohortSchedule       IndexingStageRuntimeSettings                      `json:"article_cohort_schedule,omitempty"`
	Assemble                    IndexingStageRuntimeSettings                      `json:"assemble,omitempty"`
	RecoverYEnc                 IndexingStageRuntimeSettings                      `json:"recover_yenc,omitempty"`
	SourceWindow                IndexingSourceWindowRuntimeSettings               `json:"source_window,omitempty"`
	Retention                   IndexingRetentionRuntimeSettings                  `json:"retention,omitempty"`
	Partitions                  IndexingPartitionRuntimeSettings                  `json:"partitions,omitempty"`
	RecoveryAdmission           IndexingRecoveryAdmissionRuntimeSettings          `json:"recovery_admission,omitempty"`
	ScrapeTiers                 IndexingScrapeTierRuntimeSettings                 `json:"scrape_tiers,omitempty"`
	DeferredBackfill            IndexingDeferredBackfillRuntimeSettings           `json:"deferred_backfill,omitempty"`
	ReleaseSummaryRefresh       IndexingStageRuntimeSettings                      `json:"release_summary_refresh,omitempty"`
	Release                     IndexingReleaseRuntimeSettings                    `json:"release,omitempty"`
	ReleaseGenerateNZB          IndexingStageRuntimeSettings                      `json:"release_generate_nzb,omitempty"`
	ReleaseArchiveNZB           IndexingStageRuntimeSettings                      `json:"release_archive_nzb,omitempty"`
	ReleasePurgeArchivedSources IndexingStageRuntimeSettings                      `json:"release_purge_archived_sources,omitempty"`
	MaintenanceTasks            map[string]IndexingMaintenanceTaskRuntimeSettings `json:"maintenance_tasks,omitempty"`
	Match                       IndexingMatchRuntimeSettings                      `json:"match,omitempty"`
	Inspect                     IndexingInspectRuntimeSettings                    `json:"inspect,omitempty"`
	StorageGuard                IndexingStorageGuardRuntimeSettings               `json:"storage_guard,omitempty"`
	MemoryGuard                 IndexingMemoryGuardRuntimeSettings                `json:"memory_guard,omitempty"`
	InspectDiscovery            IndexingStageRuntimeSettings                      `json:"inspect_discovery,omitempty"`
	InspectPAR2                 IndexingStageRuntimeSettings                      `json:"inspect_par2,omitempty"`
	InspectNFO                  IndexingStageRuntimeSettings                      `json:"inspect_nfo,omitempty"`
	InspectArchive              IndexingStageRuntimeSettings                      `json:"inspect_archive,omitempty"`
	InspectPassword             IndexingStageRuntimeSettings                      `json:"inspect_password,omitempty"`
	InspectMedia                IndexingStageRuntimeSettings                      `json:"inspect_media,omitempty"`
	EnrichPreDB                 IndexingPreDBRuntimeSettings                      `json:"enrich_predb,omitempty"`
	EnrichTMDB                  IndexingTMDBRuntimeSettings                       `json:"enrich_tmdb,omitempty"`
}

type IndexingMaintenanceTaskRuntimeSettings struct {
	Enabled         bool   `json:"enabled,omitempty"`
	ScheduleEnabled bool   `json:"schedule_enabled,omitempty"`
	IntervalHours   int    `json:"interval_hours,omitempty"`
	BatchSize       int    `json:"batch_size,omitempty"`
	LastDryRunAt    string `json:"last_dry_run_at,omitempty"`
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
