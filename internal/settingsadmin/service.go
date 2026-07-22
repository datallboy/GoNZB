package settingsadmin

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/transportpolicy"
	"github.com/datallboy/gonzb/internal/infra/config"
)

var ErrUnavailable = errors.New("runtime settings are not configured")

type ValidationError struct {
	message string
}

func (e ValidationError) Error() string {
	return e.message
}

type DependencyProvider struct {
	SettingsStore   func() app.SettingsStore
	BootstrapConfig func() *config.Config
}

type Service struct {
	provider DependencyProvider
}

func NewService(provider DependencyProvider) *Service {
	return &Service{provider: provider}
}

func (s *Service) Get(ctx context.Context) (*app.RuntimeSettings, error) {
	store := s.provider.SettingsStore()
	if store == nil {
		return nil, ErrUnavailable
	}

	runtime, err := store.GetRuntimeSettings(ctx, s.provider.BootstrapConfig())
	if err != nil {
		return nil, fmt.Errorf("load runtime settings: %w", err)
	}

	return runtime, nil
}

func (s *Service) Capabilities(ctx context.Context) (*app.ControlPlaneCapabilities, error) {
	store := s.provider.SettingsStore()
	if store == nil {
		return nil, ErrUnavailable
	}

	runtime, err := store.GetRuntimeSettings(ctx, s.provider.BootstrapConfig())
	if err != nil {
		return nil, fmt.Errorf("load runtime settings: %w", err)
	}

	return BuildCapabilities(s.provider.BootstrapConfig(), runtime), nil
}

func (s *Service) Update(ctx context.Context, patch *app.RuntimeSettingsPatch) (*app.RuntimeSettings, error) {
	store := s.provider.SettingsStore()
	if store == nil {
		return nil, ErrUnavailable
	}
	if patch == nil {
		return nil, ValidationError{message: "settings patch is required"}
	}

	base := s.provider.BootstrapConfig()
	current, err := store.GetRuntimeSettings(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("load runtime settings: %w", err)
	}

	next := app.ApplyPatch(current, patch)
	preserveRuntimeSecrets(current, next)
	if err := ValidateRuntimeSettingsMutation(base, current, next); err != nil {
		return nil, ValidationError{message: err.Error()}
	}
	if err := app.ValidateArrIntegrations(next.ArrIntegrations); err != nil {
		return nil, ValidationError{message: err.Error()}
	}
	if err := ValidateRuntimeSettings(base, next); err != nil {
		return nil, ValidationError{message: err.Error()}
	}

	effective := app.ApplyToConfig(base, next)
	if effective == nil {
		return nil, ValidationError{message: "failed to build effective config"}
	}
	if err := effective.ValidateEffective(); err != nil {
		return nil, ValidationError{message: err.Error()}
	}

	if err := store.UpdateSettings(ctx, next); err != nil {
		return nil, fmt.Errorf("persist runtime settings: %w", err)
	}

	return next, nil
}

func preserveRuntimeSecrets(current, next *app.RuntimeSettings) {
	if current == nil || next == nil {
		return
	}

	preserveServerPasswords(current.Servers, next.Servers)
	preserveServerPasswords(current.DownloaderServers, next.Servers)
	preserveServerPasswords(current.IndexerServers, next.Servers)
	preserveServerPasswords(current.DownloaderServers, next.DownloaderServers)
	preserveServerPasswords(current.IndexerServers, next.IndexerServers)

	indexerKeys := make(map[string]string, len(current.Indexers))
	for _, indexer := range current.Indexers {
		indexerKeys[indexer.ID] = indexer.APIKey
	}
	for i := range next.Indexers {
		if strings.TrimSpace(next.Indexers[i].APIKey) == "" {
			next.Indexers[i].APIKey = indexerKeys[next.Indexers[i].ID]
		}
	}

	arrKeys := make(map[string]string, len(current.ArrIntegrations))
	for _, integration := range current.ArrIntegrations {
		arrKeys[integration.ID] = integration.APIKey
	}
	for i := range next.ArrIntegrations {
		if strings.TrimSpace(next.ArrIntegrations[i].APIKey) == "" {
			next.ArrIntegrations[i].APIKey = arrKeys[next.ArrIntegrations[i].ID]
		}
	}

	if current.Indexing != nil && next.Indexing != nil {
		if strings.TrimSpace(next.Indexing.EnrichTMDB.TMDBAPIKey) == "" {
			next.Indexing.EnrichTMDB.TMDBAPIKey = current.Indexing.EnrichTMDB.TMDBAPIKey
		}
		if strings.TrimSpace(next.Indexing.EnrichTMDB.TMDBAccessToken) == "" {
			next.Indexing.EnrichTMDB.TMDBAccessToken = current.Indexing.EnrichTMDB.TMDBAccessToken
		}
		if strings.TrimSpace(next.Indexing.EnrichTMDB.TVDBAPIKey) == "" {
			next.Indexing.EnrichTMDB.TVDBAPIKey = current.Indexing.EnrichTMDB.TVDBAPIKey
		}
		if strings.TrimSpace(next.Indexing.EnrichTMDB.TVDBPIN) == "" {
			next.Indexing.EnrichTMDB.TVDBPIN = current.Indexing.EnrichTMDB.TVDBPIN
		}
	}
}

func ValidateRuntimeSettings(base *config.Config, runtime *app.RuntimeSettings) error {
	if runtime == nil {
		return nil
	}
	var issues []string

	if base != nil && base.Modules.Aggregator.Enabled && runtime.Aggregator != nil &&
		runtime.Aggregator.Sources.UsenetIndexer.Enabled && !base.Modules.UsenetIndexer.Enabled {
		issues = append(issues, "aggregator.sources.usenet_indexer.enabled requires modules.usenet_indexer.enabled in config.yaml")
	}
	if base != nil && base.Modules.Aggregator.Enabled && runtime.Aggregator != nil &&
		runtime.Aggregator.Sources.GoNZBNet.Enabled && !base.Modules.GoNZBNet.Enabled {
		issues = append(issues, "aggregator.sources.gonzbnet.enabled requires modules.gonzbnet.enabled in config.yaml")
	}

	issues = append(issues, validateServers("servers", runtime.Servers)...)
	issues = append(issues, validateIndexers(runtime.Indexers)...)
	issues = append(issues, validateDownload(runtime.Download)...)
	issues = append(issues, validateNNTPPool(runtime.NNTPPool)...)
	issues = append(issues, validateIndexing(runtime.Indexing)...)
	issues = append(issues, validateGoNZBNet(runtime.GoNZBNet)...)

	indexingEnabled := anyIndexerStageEnabled(runtime.Indexing)
	if indexingEnabled {
		if len(app.IndexerNNTPServers(runtime)) == 0 {
			issues = append(issues, "indexing stages require at least one NNTP server in servers")
		}
	}
	if len(issues) > 0 {
		return fmt.Errorf("runtime settings validation failed: %s", strings.Join(issues, "; "))
	}
	return nil
}

func validateGoNZBNet(in *app.GoNZBNetRuntimeSettings) []string {
	if in == nil {
		return nil
	}
	issues := make([]string, 0)
	positiveInt := func(field string, value int) {
		if value <= 0 {
			issues = append(issues, fmt.Sprintf("gonzbnet.%s must be greater than 0", field))
		}
	}
	positiveFloat := func(field string, value float64) {
		if value <= 0 {
			issues = append(issues, fmt.Sprintf("gonzbnet.%s must be greater than 0", field))
		}
	}

	positiveInt("publish_release_cards_batch_size", in.PublishReleaseCardsBatchSize)
	positiveFloat("publish_release_cards_interval_minutes", in.PublishReleaseCardsIntervalMin)
	positiveInt("health_attestations_batch_size", in.HealthAttestationsBatchSize)
	positiveFloat("health_attestations_interval_minutes", in.HealthAttestationsIntervalMin)
	positiveInt("validation_batch_size", in.ValidationBatchSize)
	positiveFloat("validation_interval_minutes", in.ValidationIntervalMin)
	positiveFloat("pull_sync_interval_minutes", in.PullSyncIntervalMin)
	positiveFloat("push_sync_interval_minutes", in.PushSyncIntervalMin)
	positiveInt("push_sync_batch_size", in.PushSyncBatchSize)
	positiveFloat("gossip_interval_minutes", in.GossipIntervalMin)
	positiveInt("gossip_batch_size", in.GossipBatchSize)
	positiveInt("gossip_ttl", in.GossipTTL)
	positiveInt("gossip_fanout", in.GossipFanout)
	positiveInt("max_event_bytes", in.MaxEventBytes)
	positiveInt("max_manifest_bytes", in.MaxManifestBytes)
	positiveInt("manifest_fetch_timeout_seconds", in.ManifestFetchTimeoutSeconds)
	positiveInt("max_batch_events", in.MaxBatchEvents)
	positiveInt("rate_limit_events_per_minute", in.RateLimitEventsPerMinute)
	positiveInt("time_tolerance_seconds", in.TimeToleranceSeconds)
	positiveInt("max_event_age_hours", in.MaxEventAgeHours)
	positiveInt("nonce_ttl_seconds", in.NonceTTLSeconds)
	if in.ScannerMaxGroups < 0 || in.ScannerMaxArticlesPerHour < 0 || in.ScannerClaimTTLMinutes < 0 || in.ScannerCheckpointIntervalSecs < 0 {
		issues = append(issues, "gonzbnet scanner limits must be greater than or equal to 0")
	}
	if in.ManifestCacheMaxBytes < 0 || in.ManifestCacheTTLDays < 0 {
		issues = append(issues, "gonzbnet manifest cache limits must be greater than or equal to 0")
	}
	for index, peerURL := range in.ManualPeers {
		if err := transportpolicy.ValidateHTTPURL(peerURL, in.AllowInsecurePeerHTTP); err != nil {
			issues = append(issues, fmt.Sprintf("gonzbnet.manual_peers[%d]: %s", index, err))
		}
	}
	return issues
}

func validateNNTPPool(pool *app.NNTPPoolRuntimeSettings) []string {
	if pool == nil {
		return nil
	}
	issues := make([]string, 0)
	if pool.IndexerMaxPercent < 1 || pool.IndexerMaxPercent > 100 {
		issues = append(issues, "nntp_pool.indexer_max_percent must be between 1 and 100")
	}
	if pool.DownloaderReservePercent < 1 || pool.DownloaderReservePercent > 100 {
		issues = append(issues, "nntp_pool.downloader_reserve_percent must be between 1 and 100")
	}
	if pool.DemandWindowSeconds < 1 {
		issues = append(issues, "nntp_pool.demand_window_seconds must be at least 1")
	}
	return issues
}

func preserveServerPasswords(current, next []app.ServerRuntimeSettings) {
	serverPasswords := make(map[string]string, len(current))
	hostPasswords := make(map[string]string, len(current))
	for _, server := range current {
		serverPasswords[server.ID] = server.Password
		if host := strings.TrimSpace(server.Host); host != "" {
			hostPasswords[strings.ToLower(host)] = server.Password
		}
	}
	for i := range next {
		if strings.TrimSpace(next[i].Password) == "" {
			next[i].Password = serverPasswords[next[i].ID]
			if next[i].Password == "" {
				if host := strings.TrimSpace(next[i].Host); host != "" {
					next[i].Password = hostPasswords[strings.ToLower(host)]
				}
			}
		}
	}
}

func validateServers(field string, servers []app.ServerRuntimeSettings) []string {
	issues := make([]string, 0)
	seen := make(map[string]int, len(servers))
	for i, server := range servers {
		prefix := fmt.Sprintf("%s[%d]", field, i)
		id := strings.TrimSpace(server.ID)
		if id == "" {
			issues = append(issues, prefix+".id is required")
		} else if first, exists := seen[id]; exists {
			issues = append(issues, fmt.Sprintf("%s.id duplicates %s[%d].id %q", prefix, field, first, id))
		} else {
			seen[id] = i
		}
		if strings.TrimSpace(server.Host) == "" {
			issues = append(issues, prefix+".host is required")
		}
		if server.Port <= 0 || server.Port > 65535 {
			issues = append(issues, prefix+".port must be between 1 and 65535")
		}
		if server.MaxConnection < 0 {
			issues = append(issues, prefix+".max_connections must be 0 or greater")
		}
		for j, role := range server.Roles {
			if !validNNTPProviderRole(role) {
				issues = append(issues, fmt.Sprintf("%s.roles[%d] must be one of scrape, yenc_recovery, inspection, download", prefix, j))
			}
		}
	}
	return issues
}

func validNNTPProviderRole(role string) bool {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "scrape", "yenc_recovery", "inspection", "download":
		return true
	default:
		return false
	}
}

func validateIndexers(indexers []app.IndexerRuntimeSettings) []string {
	issues := make([]string, 0)
	seen := make(map[string]int, len(indexers))
	for i, indexer := range indexers {
		prefix := fmt.Sprintf("indexers[%d]", i)
		id := strings.TrimSpace(indexer.ID)
		if id == "" {
			issues = append(issues, prefix+".id is required")
		} else if first, exists := seen[id]; exists {
			issues = append(issues, fmt.Sprintf("%s.id duplicates indexers[%d].id %q", prefix, first, id))
		} else {
			seen[id] = i
		}
		if strings.TrimSpace(indexer.BaseURL) == "" {
			issues = append(issues, prefix+".base_url is required")
		}
		if strings.TrimSpace(indexer.APIPath) == "" {
			issues = append(issues, prefix+".api_path is required")
		}
	}
	return issues
}

func validateDownload(download *app.DownloadRuntimeSettings) []string {
	if download == nil {
		return nil
	}
	issues := make([]string, 0, 2)
	if strings.TrimSpace(download.OutDir) == "" {
		issues = append(issues, "download.out_dir is required when downloader runtime settings are present")
	}
	if strings.TrimSpace(download.CompletedDir) == "" {
		issues = append(issues, "download.completed_dir is required when downloader runtime settings are present")
	}
	return issues
}

func validateIndexing(indexing *app.IndexingRuntimeSettings) []string {
	if indexing == nil {
		return nil
	}
	issues := make([]string, 0)
	for i, group := range indexing.Newsgroups {
		if strings.TrimSpace(group) == "" {
			issues = append(issues, fmt.Sprintf("indexing.newsgroups[%d] is required", i))
		}
	}
	for i, group := range indexing.ExplicitGroups {
		if strings.TrimSpace(group.GroupName) == "" {
			issues = append(issues, fmt.Sprintf("indexing.explicit_groups[%d].group_name is required", i))
		}
		if group.BackfillUntilDate != "" {
			if _, err := time.Parse("2006-01-02", group.BackfillUntilDate); err != nil {
				issues = append(issues, fmt.Sprintf("indexing.explicit_groups[%d].backfill_until_date must be YYYY-MM-DD", i))
			}
		}
	}
	for i, rule := range indexing.WildcardRules {
		if strings.TrimSpace(rule.Pattern) == "" {
			issues = append(issues, fmt.Sprintf("indexing.wildcard_rules[%d].pattern is required", i))
		}
	}
	for i, group := range indexing.MaterializedGroups {
		if strings.TrimSpace(group.GroupName) == "" {
			issues = append(issues, fmt.Sprintf("indexing.materialized_groups[%d].group_name is required", i))
		}
		if group.BackfillUntilDate != "" {
			if _, err := time.Parse("2006-01-02", group.BackfillUntilDate); err != nil {
				issues = append(issues, fmt.Sprintf("indexing.materialized_groups[%d].backfill_until_date must be YYYY-MM-DD", i))
			}
		}
	}
	for group, until := range indexing.BackfillUntilDateByGroup {
		if strings.TrimSpace(group) == "" {
			issues = append(issues, "indexing.backfill_until_date_by_group contains an empty group name")
		}
		if strings.TrimSpace(until) == "" {
			issues = append(issues, fmt.Sprintf("indexing.backfill_until_date_by_group[%q] is required", group))
		}
	}
	for _, stage := range indexingStages(indexing) {
		if !stage.config.Enabled {
			continue
		}
		if stage.config.IntervalMinutes <= 0 {
			issues = append(issues, "indexing."+stage.name+".interval_minutes must be greater than 0 when enabled")
		}
		if stage.config.BatchSize <= 0 {
			issues = append(issues, "indexing."+stage.name+".batch_size must be greater than 0 when enabled")
		}
		if stage.config.MaxBatches < 0 {
			issues = append(issues, "indexing."+stage.name+".max_batches must be greater than or equal to 0")
		}
		if stage.config.BinaryUpsertDBChunkSize < 0 {
			issues = append(issues, "indexing."+stage.name+".binary_upsert_db_chunk_size must be greater than or equal to 0")
		}
		if strings.HasPrefix(stage.name, "assemble") && stage.config.BinaryUpsertDBChunkSize == 0 {
			issues = append(issues, "indexing."+stage.name+".binary_upsert_db_chunk_size must be greater than 0 when enabled")
		}
		if stage.name == "recover_yenc" {
			if stage.config.FetchTimeoutSeconds < 0 {
				issues = append(issues, "indexing."+stage.name+".fetch_timeout_seconds must be greater than or equal to 0")
			}
			if stage.config.FetchTimeoutSeconds > 120 {
				issues = append(issues, "indexing."+stage.name+".fetch_timeout_seconds must be less than or equal to 120")
			}
			issues = append(issues, validateYEncRecoveryTargetWindow(stage.config)...)
		}
	}
	if indexing.Inspect.MinBinaryBytes < 0 {
		issues = append(issues, "indexing.inspect.min_binary_bytes must be greater than or equal to 0")
	}
	if indexing.Inspect.MaxBinaryBytes < 0 {
		issues = append(issues, "indexing.inspect.max_binary_bytes must be greater than or equal to 0")
	}
	if indexing.Inspect.MinBinaryBytes > 0 && indexing.Inspect.MaxBinaryBytes > 0 && indexing.Inspect.MinBinaryBytes > indexing.Inspect.MaxBinaryBytes {
		issues = append(issues, "indexing.inspect.min_binary_bytes must be less than or equal to indexing.inspect.max_binary_bytes")
	}
	if indexing.StorageGuard.MinFreeBytes < 0 {
		issues = append(issues, "indexing.storage_guard.min_free_bytes must be greater than or equal to 0")
	}
	if indexing.StorageGuard.MinFreePercent < 0 || indexing.StorageGuard.MinFreePercent > 100 {
		issues = append(issues, "indexing.storage_guard.min_free_percent must be between 0 and 100")
	}
	if indexing.MemoryGuard.MinAvailableBytes < 0 {
		issues = append(issues, "indexing.memory_guard.min_available_bytes must be greater than or equal to 0")
	}
	if indexing.MemoryGuard.MinAvailablePercent < 0 || indexing.MemoryGuard.MinAvailablePercent > 100 {
		issues = append(issues, "indexing.memory_guard.min_available_percent must be between 0 and 100")
	}
	if indexing.MemoryGuard.MinSwapFreeBytes < 0 {
		issues = append(issues, "indexing.memory_guard.min_swap_free_bytes must be greater than or equal to 0")
	}
	if indexing.RecoveryAdmission.NearTimeCohortBucketMinutes < 0 {
		issues = append(issues, "indexing.recovery_admission.near_time_cohort_bucket_minutes must be greater than or equal to 0")
	}
	if indexing.RecoveryAdmission.NearTimeCohortBucketMinutes > 24*60 {
		issues = append(issues, "indexing.recovery_admission.near_time_cohort_bucket_minutes must be less than or equal to 1440")
	}
	if indexing.RecoveryAdmission.Priority0ReservoirBatches < 0 {
		issues = append(issues, "indexing.recovery_admission.priority0_reservoir_batches must be greater than or equal to 0")
	}
	if indexing.RecoveryAdmission.Priority0ReservoirBatches > 20 {
		issues = append(issues, "indexing.recovery_admission.priority0_reservoir_batches must be less than or equal to 20")
	}
	if indexing.ScrapeTiers.AssembleBacklogHighWater < 0 {
		issues = append(issues, "indexing.scrape_tiers.assemble_backlog_high_water must be greater than or equal to 0")
	}
	if indexing.ScrapeTiers.AssembleBacklogLowWater < 0 {
		issues = append(issues, "indexing.scrape_tiers.assemble_backlog_low_water must be greater than or equal to 0")
	}
	if indexing.ScrapeTiers.AssembleBacklogHighWater > 0 &&
		indexing.ScrapeTiers.AssembleBacklogLowWater > 0 &&
		indexing.ScrapeTiers.AssembleBacklogLowWater >= indexing.ScrapeTiers.AssembleBacklogHighWater {
		issues = append(issues, "indexing.scrape_tiers.assemble_backlog_low_water must be less than indexing.scrape_tiers.assemble_backlog_high_water")
	}
	for i, rule := range indexing.Inspect.BlockedMagicHex {
		clean := strings.ToUpper(strings.TrimSpace(rule))
		clean = strings.ReplaceAll(clean, "0X", "")
		clean = strings.ReplaceAll(clean, " ", "")
		clean = strings.ReplaceAll(clean, ":", "")
		clean = strings.ReplaceAll(clean, "-", "")
		if clean == "" {
			continue
		}
		if len(clean)%2 != 0 {
			issues = append(issues, fmt.Sprintf("indexing.inspect.blocked_magic_hex[%d] must contain an even number of hex characters", i))
			continue
		}
		if _, err := hex.DecodeString(clean); err != nil {
			issues = append(issues, fmt.Sprintf("indexing.inspect.blocked_magic_hex[%d] must be hex encoded", i))
		}
	}
	return issues
}

func validateYEncRecoveryTargetWindow(stage app.IndexingStageRuntimeSettings) []string {
	if !stage.TargetWindowEnabled {
		return nil
	}
	issues := make([]string, 0)
	start, startErr := time.Parse(time.RFC3339, strings.TrimSpace(stage.TargetWindowStart))
	end, endErr := time.Parse(time.RFC3339, strings.TrimSpace(stage.TargetWindowEnd))
	if startErr != nil {
		issues = append(issues, "indexing.recover_yenc.target_window_start must be RFC3339 when target window is enabled")
	}
	if endErr != nil {
		issues = append(issues, "indexing.recover_yenc.target_window_end must be RFC3339 when target window is enabled")
	}
	if startErr == nil && endErr == nil && !start.Before(end) {
		issues = append(issues, "indexing.recover_yenc.target_window_start must be before target_window_end")
	}
	if stage.TargetWindowPct < 0 || stage.TargetWindowPct > 100 {
		issues = append(issues, "indexing.recover_yenc.target_window_pct must be between 0 and 100 when target window is enabled")
	}
	if stage.NewestPct < 0 || stage.NewestPct > 100 {
		issues = append(issues, "indexing.recover_yenc.newest_pct must be between 0 and 100 when target window is enabled")
	}
	if stage.TargetWindowPct+stage.NewestPct != 100 {
		issues = append(issues, "indexing.recover_yenc.target_window_pct and newest_pct must total 100 when target window is enabled")
	}
	return issues
}

type namedStage struct {
	name   string
	config app.IndexingStageRuntimeSettings
}

func indexingStages(indexing *app.IndexingRuntimeSettings) []namedStage {
	return []namedStage{
		{name: "scrape_latest", config: indexing.ScrapeLatest},
		{name: "scrape_backfill", config: indexing.ScrapeBackfill},
		{name: "article_cohort_schedule", config: indexing.ArticleCohortSchedule},
		{name: "assemble", config: indexing.Assemble},
		{name: "recover_yenc", config: indexing.RecoverYEnc},
		{name: "release_summary_refresh", config: indexing.ReleaseSummaryRefresh},
		{name: "release", config: app.IndexingStageRuntimeSettings{
			Enabled:         indexing.Release.Enabled,
			IntervalMinutes: indexing.Release.IntervalMinutes,
			BatchSize:       indexing.Release.BatchSize,
			BackoffSeconds:  indexing.Release.BackoffSeconds,
		}},
		{name: "release_generate_nzb", config: indexing.ReleaseGenerateNZB},
		{name: "release_archive_nzb", config: indexing.ReleaseArchiveNZB},
		{name: "release_purge_archived_sources", config: indexing.ReleasePurgeArchivedSources},
		{name: "inspect_discovery", config: indexing.InspectDiscovery},
		{name: "inspect_par2", config: indexing.InspectPAR2},
		{name: "inspect_nfo", config: indexing.InspectNFO},
		{name: "inspect_archive", config: indexing.InspectArchive},
		{name: "inspect_password", config: indexing.InspectPassword},
		{name: "inspect_media", config: indexing.InspectMedia},
		{name: "enrich_predb", config: app.IndexingStageRuntimeSettings{
			Enabled:         indexing.EnrichPreDB.Enabled,
			IntervalMinutes: indexing.EnrichPreDB.IntervalMinutes,
			BatchSize:       indexing.EnrichPreDB.BatchSize,
			BackoffSeconds:  indexing.EnrichPreDB.BackoffSeconds,
		}},
		{name: "enrich_tmdb", config: app.IndexingStageRuntimeSettings{
			Enabled:         indexing.EnrichTMDB.Enabled,
			IntervalMinutes: indexing.EnrichTMDB.IntervalMinutes,
			BatchSize:       indexing.EnrichTMDB.BatchSize,
			BackoffSeconds:  indexing.EnrichTMDB.BackoffSeconds,
		}},
	}
}

func ValidateRuntimeSettingsMutation(base *config.Config, current, next *app.RuntimeSettings) error {
	if current == nil || next == nil {
		return nil
	}
	if base != nil && base.Modules.Downloader.Enabled &&
		len(next.Servers) < len(current.Servers) &&
		downloaderConfigured(current) {
		return fmt.Errorf("removing NNTP servers while downloader runtime is configured requires a restart")
	}
	if base != nil && base.Modules.Aggregator.Enabled &&
		len(next.Indexers) < len(current.Indexers) &&
		aggregatorConfigured(current) {
		return fmt.Errorf("removing external Newznab sources while aggregator runtime is configured requires a restart")
	}
	if base != nil && base.Modules.Downloader.Enabled &&
		len(next.ArrIntegrations) < len(current.ArrIntegrations) &&
		downloaderConfigured(current) {
		return fmt.Errorf("removing ARR integrations while downloader runtime is configured requires a restart")
	}
	if err := validateIndexerMaintenanceTasks(next); err != nil {
		return err
	}
	return nil
}

func validateIndexerMaintenanceTasks(next *app.RuntimeSettings) error {
	if next == nil || next.Indexing == nil {
		return nil
	}
	for key, cfg := range next.Indexing.MaintenanceTasks {
		minIntervalHours := maintenanceTaskMinIntervalHours(key)
		if cfg.ScheduleEnabled && cfg.IntervalHours < minIntervalHours {
			return fmt.Errorf("maintenance task %q scheduled interval must be at least %d hours", key, minIntervalHours)
		}
		if cfg.BatchSize < 0 {
			return fmt.Errorf("maintenance task %q batch size cannot be negative", key)
		}
	}
	return nil
}

func maintenanceTaskMinIntervalHours(taskKey string) int {
	switch strings.TrimSpace(strings.ToLower(taskKey)) {
	case "dashboard_stats_refresh", "group_profile_refresh", "outcome_reconcile":
		return 1
	case "raw_stage_retention", "partition_default_rehome", "stale_nonrelease_source_purge", "emergency_source_window_reset":
		return 24
	default:
		return 6
	}
}

func BuildCapabilities(base *config.Config, runtime *app.RuntimeSettings) *app.ControlPlaneCapabilities {
	if runtime == nil {
		runtime = app.DefaultRuntimeSettings()
	}
	if base == nil {
		base = &config.Config{}
	}

	modules := map[string]app.ModuleCapability{
		"downloader":     moduleCapability(base.Modules.Downloader.Enabled, downloaderConfigured(runtime), nil),
		"aggregator":     moduleCapability(base.Modules.Aggregator.Enabled, aggregatorConfigured(runtime), aggregatorRequirements(base, runtime)),
		"usenet_indexer": moduleCapability(base.Modules.UsenetIndexer.Enabled, indexerConfigured(runtime), indexerRequirements(runtime)),
		"gonzbnet":       moduleCapability(base.Modules.GoNZBNet.Enabled, gonzbnetConfigured(base), gonzbnetRequirements(base)),
		"web_ui":         moduleCapability(base.Modules.WebUI.Enabled, base.Modules.WebUI.Enabled, nil),
		"api":            moduleCapability(base.Modules.API.Enabled, base.Modules.API.Enabled, nil),
	}

	return &app.ControlPlaneCapabilities{
		Modules: modules,
		Settings: app.SettingsCapability{
			RuntimeConfigured: app.RuntimeConfigured(runtime),
		},
		Revision: runtime.Revision,
	}
}

func moduleCapability(enabled, configured bool, requirements []string) app.ModuleCapability {
	if !enabled {
		return app.ModuleCapability{
			Enabled: false,
			Visible: false,
			Reason:  "disabled by config.yaml",
		}
	}
	ready := configured && len(requirements) == 0
	reason := ""
	if !ready {
		reason = "configuration required"
	}
	return app.ModuleCapability{
		Enabled:      enabled,
		Configured:   configured,
		Ready:        ready,
		Visible:      enabled,
		Reason:       reason,
		Requirements: requirements,
	}
}

func downloaderConfigured(runtime *app.RuntimeSettings) bool {
	return runtime != nil && len(app.DownloaderNNTPServers(runtime)) > 0 && runtime.Download != nil &&
		strings.TrimSpace(runtime.Download.OutDir) != ""
}

func aggregatorConfigured(runtime *app.RuntimeSettings) bool {
	if runtime == nil {
		return false
	}
	hasExternal := len(runtime.Indexers) > 0
	hasLocal := runtime.Aggregator != nil &&
		(runtime.Aggregator.Sources.LocalBlob.Enabled || runtime.Aggregator.Sources.UsenetIndexer.Enabled || runtime.Aggregator.Sources.GoNZBNet.Enabled)
	return hasExternal || hasLocal
}

func aggregatorRequirements(base *config.Config, runtime *app.RuntimeSettings) []string {
	reqs := make([]string, 0, 2)
	if !aggregatorConfigured(runtime) {
		reqs = append(reqs, "enable local blob, local indexer, GoNZBNet, or an external Newznab source")
	}
	if runtime != nil && runtime.Aggregator != nil && runtime.Aggregator.Sources.UsenetIndexer.Enabled &&
		(base == nil || !base.Modules.UsenetIndexer.Enabled) {
		reqs = append(reqs, "enable usenet_indexer module in config.yaml")
	}
	if runtime != nil && runtime.Aggregator != nil && runtime.Aggregator.Sources.GoNZBNet.Enabled &&
		(base == nil || !base.Modules.GoNZBNet.Enabled) {
		reqs = append(reqs, "enable gonzbnet module in config.yaml")
	}
	return reqs
}

func gonzbnetConfigured(base *config.Config) bool {
	return base != nil && strings.TrimSpace(base.Store.PGDSN) != ""
}

func gonzbnetRequirements(base *config.Config) []string {
	if gonzbnetConfigured(base) {
		return nil
	}
	return []string{"configure store.pg_dsn in config.yaml"}
}

func indexerConfigured(runtime *app.RuntimeSettings) bool {
	return runtime != nil && len(app.IndexerNNTPServers(runtime)) > 0 && runtime.Indexing != nil && len(app.EffectiveNewsgroupNames(runtime.Indexing)) > 0
}

func indexerRequirements(runtime *app.RuntimeSettings) []string {
	reqs := make([]string, 0, 2)
	if runtime == nil || len(app.IndexerNNTPServers(runtime)) == 0 {
		reqs = append(reqs, "configure at least one NNTP server")
	}
	if runtime == nil || runtime.Indexing == nil || len(app.EffectiveNewsgroupNames(runtime.Indexing)) == 0 {
		reqs = append(reqs, "configure at least one scrape group")
	}
	return reqs
}

func anyIndexerStageEnabled(indexing *app.IndexingRuntimeSettings) bool {
	if indexing == nil {
		return false
	}
	return indexing.ScrapeLatest.Enabled ||
		indexing.ScrapeBackfill.Enabled ||
		indexing.Assemble.Enabled ||
		indexing.RecoverYEnc.Enabled ||
		indexing.ReleaseSummaryRefresh.Enabled ||
		indexing.Release.Enabled ||
		indexing.ReleaseGenerateNZB.Enabled ||
		indexing.ReleaseArchiveNZB.Enabled ||
		indexing.ReleasePurgeArchivedSources.Enabled ||
		indexing.InspectDiscovery.Enabled ||
		indexing.InspectPAR2.Enabled ||
		indexing.InspectNFO.Enabled ||
		indexing.InspectArchive.Enabled ||
		indexing.InspectPassword.Enabled ||
		indexing.InspectMedia.Enabled ||
		indexing.EnrichPreDB.Enabled ||
		indexing.EnrichTMDB.Enabled
}
