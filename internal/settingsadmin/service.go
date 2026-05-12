package settingsadmin

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/app"
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

	issues = append(issues, validateServers("servers", runtime.Servers)...)
	issues = append(issues, validateServers("downloader_servers", runtime.DownloaderServers)...)
	issues = append(issues, validateServers("indexer_servers", runtime.IndexerServers)...)
	issues = append(issues, validateIndexers(runtime.Indexers)...)
	issues = append(issues, validateDownload(runtime.Download)...)
	issues = append(issues, validateIndexing(runtime.Indexing)...)

	indexingEnabled := anyIndexerStageEnabled(runtime.Indexing)
	if indexingEnabled {
		if len(app.IndexerNNTPServers(runtime)) == 0 {
			issues = append(issues, "indexing stages require at least one NNTP server in indexer_servers")
		}
		if runtime.Indexing == nil || len(runtime.Indexing.Newsgroups) == 0 {
			issues = append(issues, "indexing stages require at least one newsgroup in indexing.newsgroups")
		}
	}
	if len(issues) > 0 {
		return fmt.Errorf("runtime settings validation failed: %s", strings.Join(issues, "; "))
	}
	return nil
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
	}
	return issues
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

type namedStage struct {
	name   string
	config app.IndexingStageRuntimeSettings
}

func indexingStages(indexing *app.IndexingRuntimeSettings) []namedStage {
	return []namedStage{
		{name: "scrape_latest", config: indexing.ScrapeLatest},
		{name: "scrape_backfill", config: indexing.ScrapeBackfill},
		{name: "assemble", config: indexing.Assemble},
		{name: "assemble_lane_a", config: indexing.AssembleLaneA},
		{name: "assemble_lane_b", config: indexing.AssembleLaneB},
		{name: "recover_yenc", config: indexing.RecoverYEnc},
		{name: "release", config: app.IndexingStageRuntimeSettings{
			Enabled:         indexing.Release.Enabled,
			IntervalMinutes: indexing.Release.IntervalMinutes,
			BatchSize:       indexing.Release.BatchSize,
			BackoffSeconds:  indexing.Release.BackoffSeconds,
		}},
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
		len(app.DownloaderNNTPServers(next)) < len(app.DownloaderNNTPServers(current)) &&
		downloaderConfigured(current) {
		return fmt.Errorf("removing downloader NNTP servers while downloader runtime is configured requires a restart")
	}
	if base != nil && base.Modules.UsenetIndexer.Enabled &&
		len(app.IndexerNNTPServers(next)) < len(app.IndexerNNTPServers(current)) &&
		anyIndexerStageEnabled(current.Indexing) {
		return fmt.Errorf("removing indexer NNTP servers while indexer stages are enabled requires a restart")
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
	if anyIndexerStageEnabled(current.Indexing) && current.Indexing != nil && next.Indexing != nil &&
		len(next.Indexing.Newsgroups) < len(current.Indexing.Newsgroups) {
		return fmt.Errorf("removing indexer newsgroups while indexer stages are enabled requires a restart")
	}
	return nil
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
		(runtime.Aggregator.Sources.LocalBlob.Enabled || runtime.Aggregator.Sources.UsenetIndexer.Enabled)
	return hasExternal || hasLocal
}

func aggregatorRequirements(base *config.Config, runtime *app.RuntimeSettings) []string {
	reqs := make([]string, 0, 2)
	if !aggregatorConfigured(runtime) {
		reqs = append(reqs, "enable local blob, local indexer, or an external Newznab source")
	}
	if runtime != nil && runtime.Aggregator != nil && runtime.Aggregator.Sources.UsenetIndexer.Enabled &&
		(base == nil || !base.Modules.UsenetIndexer.Enabled) {
		reqs = append(reqs, "enable usenet_indexer module in config.yaml")
	}
	return reqs
}

func indexerConfigured(runtime *app.RuntimeSettings) bool {
	return runtime != nil && len(app.IndexerNNTPServers(runtime)) > 0 && runtime.Indexing != nil && len(runtime.Indexing.Newsgroups) > 0
}

func indexerRequirements(runtime *app.RuntimeSettings) []string {
	reqs := make([]string, 0, 2)
	if runtime == nil || len(app.IndexerNNTPServers(runtime)) == 0 {
		reqs = append(reqs, "configure at least one indexer NNTP server")
	}
	if runtime == nil || runtime.Indexing == nil || len(runtime.Indexing.Newsgroups) == 0 {
		reqs = append(reqs, "configure at least one newsgroup")
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
		indexing.Release.Enabled ||
		indexing.InspectDiscovery.Enabled ||
		indexing.InspectPAR2.Enabled ||
		indexing.InspectNFO.Enabled ||
		indexing.InspectArchive.Enabled ||
		indexing.InspectPassword.Enabled ||
		indexing.InspectMedia.Enabled ||
		indexing.EnrichPreDB.Enabled ||
		indexing.EnrichTMDB.Enabled
}
