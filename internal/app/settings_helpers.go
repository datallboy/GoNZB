package app

import (
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/infra/config"
)

// FromConfig derives editable runtime state from current effective config.
func FromConfig(cfg *config.Config) *RuntimeSettings {
	if cfg == nil {
		return &RuntimeSettings{}
	}

	out := &RuntimeSettings{
		Servers:         make([]ServerRuntimeSettings, 0, len(cfg.Servers)),
		Indexers:        make([]IndexerRuntimeSettings, 0, len(cfg.Indexers)),
		ArrIntegrations: []ArrIntegrationRuntimeSettings{},
		Download: &DownloadRuntimeSettings{
			OutDir:            cfg.Download.OutDir,
			CompletedDir:      cfg.Download.CompletedDir,
			CleanupExtensions: append([]string(nil), cfg.Download.CleanupExtensions...),
		},
		Indexing: func() *IndexingRuntimeSettings {
			indexing := IndexingRuntimeFromConfig(cfg.Indexing)
			return &indexing
		}(),
	}

	for _, s := range cfg.Servers {
		out.Servers = append(out.Servers, ServerRuntimeSettings{
			ID:                     s.ID,
			Host:                   s.Host,
			Port:                   s.Port,
			Username:               s.Username,
			Password:               s.Password,
			TLS:                    s.TLS,
			MaxConnection:          s.MaxConnection,
			Priority:               s.Priority,
			DialTimeoutSeconds:     s.DialTimeoutSeconds,
			TCPKeepAliveSeconds:    s.TCPKeepAliveSeconds,
			PoolIdleTimeoutSeconds: s.PoolIdleTimeoutSeconds,
			PoolMaxAgeSeconds:      s.PoolMaxAgeSeconds,
			EnablePoolLogging:      s.EnablePoolLogging,
		})
	}

	for _, idx := range cfg.Indexers {
		out.Indexers = append(out.Indexers, IndexerRuntimeSettings{
			ID:       idx.ID,
			BaseURL:  idx.BaseUrl,
			APIPath:  idx.ApiPath,
			APIKey:   idx.ApiKey,
			Redirect: idx.Redirect,
		})
	}

	return out
}

func IndexingRuntimeFromConfig(cfg config.IndexingConfig) IndexingRuntimeSettings {
	out := IndexingRuntimeSettings{
		Newsgroups: append([]string(nil), cfg.Newsgroups...),
	}

	out.ScrapeLatest = indexStageRuntimeFromConfig(cfg.ScrapeLatest, true, 10, 5000)
	out.ScrapeBackfill = indexStageRuntimeFromConfig(cfg.ScrapeBackfill, true, 10, 5000)
	out.Assemble = indexStageRuntimeFromConfigWithConcurrency(cfg.Assemble, true, 10, 5000)
	out.Release = IndexingReleaseRuntimeSettings{
		Enabled:          boolValue(cfg.Release.Enabled, true),
		IntervalMinutes:  float64Value(cfg.Release.IntervalMinutes, 10),
		BatchSize:        intValue(cfg.Release.BatchSize, 1000),
		BackoffSeconds:   intValue(cfg.Release.BackoffSeconds, 0),
		MinConfidence:    float64Value(cfg.Release.MinConfidence, 0.55),
		MinCompletionPct: float64Value(cfg.Release.MinCompletionPct, 0),
		RequireExpectedFileCountForContextualObfuscated: boolValue(cfg.Release.RequireExpectedFileCountForContextualObfuscated, true),
	}
	out.Match = IndexingMatchRuntimeSettings{
		HighConfidenceThreshold:     float64Value(cfg.Match.HighConfidenceThreshold, 0.85),
		ProbableConfidenceThreshold: float64Value(cfg.Match.ProbableConfidenceThreshold, 0.55),
		ArticleBucketSize:           int64Value(cfg.Match.ArticleBucketSize, 5000),
	}
	out.Inspect = IndexingInspectRuntimeSettings{
		WorkDir:         firstNonEmpty(cfg.Inspect.WorkDir, "/store/indexer/inspect"),
		MaxBytes:        firstNonZeroInt64(cfg.Inspect.MaxBytes, 2*1024*1024*1024),
		MaxArchiveDepth: firstNonZeroInt(cfg.Inspect.MaxArchiveDepth, 3),
		ToolTimeoutSecs: firstNonZeroInt(cfg.Inspect.ToolTimeoutSecs, 30),
		FFProbePath:     firstNonEmpty(cfg.Inspect.FFProbePath, "ffprobe"),
		SevenZipPath:    firstNonEmpty(cfg.Inspect.SevenZipPath, "7z"),
		UnrarPath:       firstNonEmpty(cfg.Inspect.UnrarPath, "unrar"),
		PAR2Path:        firstNonEmpty(cfg.Inspect.PAR2Path, "par2"),
	}
	out.InspectDiscovery = indexStageRuntimeFromConfig(cfg.InspectDiscovery, true, 10, 100)
	out.InspectPAR2 = indexStageRuntimeFromConfig(cfg.InspectPAR2, true, 10, 100)
	out.InspectNFO = indexStageRuntimeFromConfig(cfg.InspectNFO, true, 10, 100)
	out.InspectArchive = indexStageRuntimeFromConfigWithConcurrency(cfg.InspectArchive, true, 10, 100)
	out.InspectPassword = indexStageRuntimeFromConfig(cfg.InspectPassword, true, 10, 100)
	out.InspectMedia = indexStageRuntimeFromConfigWithConcurrency(cfg.InspectMedia, true, 10, 100)
	out.EnrichPreDB = IndexingPreDBRuntimeSettings{
		Enabled:            boolValue(cfg.EnrichPreDB.Enabled, true),
		IntervalMinutes:    float64Value(cfg.EnrichPreDB.IntervalMinutes, 10),
		BatchSize:          intValue(cfg.EnrichPreDB.BatchSize, 100),
		BackoffSeconds:     intValue(cfg.EnrichPreDB.BackoffSeconds, 0),
		Provider:           firstNonEmpty(cfg.EnrichPreDB.Provider, "club,me"),
		BaseURL:            firstNonEmpty(cfg.EnrichPreDB.BaseURL, "https://predb.club/api/v1"),
		FeedURL:            firstNonEmpty(cfg.EnrichPreDB.FeedURL, "https://predb.me/?rss=1"),
		DumpURL:            firstNonEmpty(cfg.EnrichPreDB.DumpURL),
		HTTPTimeoutSeconds: intValue(cfg.EnrichPreDB.HTTPTimeoutSeconds, 10),
		BackfillPageSize:   intValue(cfg.EnrichPreDB.BackfillPageSize, 1000),
		MaxBackfillPages:   intValue(cfg.EnrichPreDB.MaxBackfillPages, 250),
	}
	out.EnrichTMDB = IndexingTMDBRuntimeSettings{
		Enabled:            boolValue(cfg.EnrichTMDB.Enabled, true),
		IntervalMinutes:    float64Value(cfg.EnrichTMDB.IntervalMinutes, 10),
		BatchSize:          intValue(cfg.EnrichTMDB.BatchSize, 100),
		BackoffSeconds:     intValue(cfg.EnrichTMDB.BackoffSeconds, 0),
		HTTPTimeoutSeconds: intValue(cfg.EnrichTMDB.HTTPTimeoutSeconds, 15),
		TMDBAPIKey:         firstNonEmpty(cfg.EnrichTMDB.TMDBAPIKey),
		TMDBAccessToken:    firstNonEmpty(cfg.EnrichTMDB.TMDBAccessToken),
		TMDBBaseURL:        firstNonEmpty(cfg.EnrichTMDB.TMDBBaseURL, "https://api.themoviedb.org/3"),
		TVDBAPIKey:         firstNonEmpty(cfg.EnrichTMDB.TVDBAPIKey),
		TVDBPIN:            firstNonEmpty(cfg.EnrichTMDB.TVDBPIN),
		TVDBBaseURL:        firstNonEmpty(cfg.EnrichTMDB.TVDBBaseURL, "https://api4.thetvdb.com/v4"),
	}

	return out
}

// ApplyToConfig applies runtime-editable settings on top of bootstrap config.
func ApplyToConfig(base *config.Config, runtime *RuntimeSettings) *config.Config {
	if base == nil {
		return nil
	}

	effective := *base

	effective.Servers = append([]config.ServerConfig(nil), base.Servers...)
	effective.Indexers = append([]config.IndexerConfig(nil), base.Indexers...)
	effective.Download.CleanupExtensions = append([]string(nil), base.Download.CleanupExtensions...)
	effective.Indexing.Newsgroups = append([]string(nil), base.Indexing.Newsgroups...)

	if runtime == nil {
		return &effective
	}

	if len(runtime.Servers) > 0 {
		effective.Servers = make([]config.ServerConfig, 0, len(runtime.Servers))
		for _, s := range runtime.Servers {
			effective.Servers = append(effective.Servers, config.ServerConfig{
				ID:                     strings.TrimSpace(s.ID),
				Host:                   strings.TrimSpace(s.Host),
				Port:                   s.Port,
				Username:               s.Username,
				Password:               s.Password,
				TLS:                    s.TLS,
				MaxConnection:          s.MaxConnection,
				Priority:               s.Priority,
				DialTimeoutSeconds:     s.DialTimeoutSeconds,
				TCPKeepAliveSeconds:    s.TCPKeepAliveSeconds,
				PoolIdleTimeoutSeconds: s.PoolIdleTimeoutSeconds,
				PoolMaxAgeSeconds:      s.PoolMaxAgeSeconds,
				EnablePoolLogging:      s.EnablePoolLogging,
			})
		}
	}

	if len(runtime.Indexers) > 0 {
		effective.Indexers = make([]config.IndexerConfig, 0, len(runtime.Indexers))
		for _, idx := range runtime.Indexers {
			effective.Indexers = append(effective.Indexers, config.IndexerConfig{
				ID:       strings.TrimSpace(idx.ID),
				BaseUrl:  strings.TrimSpace(idx.BaseURL),
				ApiPath:  strings.TrimSpace(idx.APIPath),
				ApiKey:   idx.APIKey,
				Redirect: idx.Redirect,
			})
		}
	}

	if runtime.Download != nil {
		if runtime.Download.OutDir != "" {
			effective.Download.OutDir = runtime.Download.OutDir
		}
		if runtime.Download.CompletedDir != "" {
			effective.Download.CompletedDir = runtime.Download.CompletedDir
		}
		if runtime.Download.CleanupExtensions != nil {
			effective.Download.CleanupExtensions = append([]string(nil), runtime.Download.CleanupExtensions...)
		}
	}

	if runtime.Indexing != nil {
		indexing := cloneIndexing(runtime.Indexing)
		if indexing.Newsgroups != nil {
			effective.Indexing.Newsgroups = append([]string(nil), indexing.Newsgroups...)
		}

		effective.Indexing.ScrapeLatest = toStageConfigNoConcurrency(indexing.ScrapeLatest)
		effective.Indexing.ScrapeBackfill = toStageConfigNoConcurrency(indexing.ScrapeBackfill)
		effective.Indexing.Assemble = toStageConfig(indexing.Assemble)
		effective.Indexing.Release = config.IndexingReleaseConfig{
			Enabled:          boolPtr(indexing.Release.Enabled),
			IntervalMinutes:  float64Ptr(indexing.Release.IntervalMinutes),
			BatchSize:        intPtr(indexing.Release.BatchSize),
			BackoffSeconds:   intPtr(indexing.Release.BackoffSeconds),
			MinConfidence:    float64Ptr(indexing.Release.MinConfidence),
			MinCompletionPct: float64Ptr(indexing.Release.MinCompletionPct),
			RequireExpectedFileCountForContextualObfuscated: boolPtr(indexing.Release.RequireExpectedFileCountForContextualObfuscated),
		}
		effective.Indexing.Match = config.IndexingMatchConfig{
			HighConfidenceThreshold:     float64Ptr(indexing.Match.HighConfidenceThreshold),
			ProbableConfidenceThreshold: float64Ptr(indexing.Match.ProbableConfidenceThreshold),
			ArticleBucketSize:           int64Ptr(indexing.Match.ArticleBucketSize),
		}
		effective.Indexing.Inspect = config.IndexingInspectConfig{
			WorkDir:         indexing.Inspect.WorkDir,
			MaxBytes:        indexing.Inspect.MaxBytes,
			MaxArchiveDepth: indexing.Inspect.MaxArchiveDepth,
			ToolTimeoutSecs: indexing.Inspect.ToolTimeoutSecs,
			FFProbePath:     indexing.Inspect.FFProbePath,
			SevenZipPath:    indexing.Inspect.SevenZipPath,
			UnrarPath:       indexing.Inspect.UnrarPath,
			PAR2Path:        indexing.Inspect.PAR2Path,
		}
		effective.Indexing.InspectDiscovery = toStageConfigNoConcurrency(indexing.InspectDiscovery)
		effective.Indexing.InspectPAR2 = toStageConfigNoConcurrency(indexing.InspectPAR2)
		effective.Indexing.InspectNFO = toStageConfigNoConcurrency(indexing.InspectNFO)
		effective.Indexing.InspectArchive = toStageConfig(indexing.InspectArchive)
		effective.Indexing.InspectPassword = toStageConfigNoConcurrency(indexing.InspectPassword)
		effective.Indexing.InspectMedia = toStageConfig(indexing.InspectMedia)
		effective.Indexing.EnrichPreDB = config.IndexingPreDBConfig{
			Enabled:            boolPtr(indexing.EnrichPreDB.Enabled),
			IntervalMinutes:    float64Ptr(indexing.EnrichPreDB.IntervalMinutes),
			BatchSize:          intPtr(indexing.EnrichPreDB.BatchSize),
			BackoffSeconds:     intPtr(indexing.EnrichPreDB.BackoffSeconds),
			Provider:           indexing.EnrichPreDB.Provider,
			BaseURL:            indexing.EnrichPreDB.BaseURL,
			FeedURL:            indexing.EnrichPreDB.FeedURL,
			DumpURL:            indexing.EnrichPreDB.DumpURL,
			HTTPTimeoutSeconds: intPtr(indexing.EnrichPreDB.HTTPTimeoutSeconds),
			BackfillPageSize:   intPtr(indexing.EnrichPreDB.BackfillPageSize),
			MaxBackfillPages:   intPtr(indexing.EnrichPreDB.MaxBackfillPages),
		}
		effective.Indexing.EnrichTMDB = config.IndexingTMDBConfig{
			Enabled:            boolPtr(indexing.EnrichTMDB.Enabled),
			IntervalMinutes:    float64Ptr(indexing.EnrichTMDB.IntervalMinutes),
			BatchSize:          intPtr(indexing.EnrichTMDB.BatchSize),
			BackoffSeconds:     intPtr(indexing.EnrichTMDB.BackoffSeconds),
			HTTPTimeoutSeconds: intPtr(indexing.EnrichTMDB.HTTPTimeoutSeconds),
			TMDBAPIKey:         indexing.EnrichTMDB.TMDBAPIKey,
			TMDBAccessToken:    indexing.EnrichTMDB.TMDBAccessToken,
			TMDBBaseURL:        indexing.EnrichTMDB.TMDBBaseURL,
			TVDBAPIKey:         indexing.EnrichTMDB.TVDBAPIKey,
			TVDBPIN:            indexing.EnrichTMDB.TVDBPIN,
			TVDBBaseURL:        indexing.EnrichTMDB.TVDBBaseURL,
		}
	}

	return &effective
}

// ApplyPatch applies an incoming patch to the current runtime settings.
func ApplyPatch(current *RuntimeSettings, patch *RuntimeSettingsPatch) *RuntimeSettings {
	if current == nil {
		current = &RuntimeSettings{}
	}
	if patch == nil {
		return current
	}

	next := &RuntimeSettings{
		Servers:         append([]ServerRuntimeSettings(nil), current.Servers...),
		Indexers:        append([]IndexerRuntimeSettings(nil), current.Indexers...),
		ArrIntegrations: append([]ArrIntegrationRuntimeSettings(nil), current.ArrIntegrations...),
		Download:        cloneDownload(current.Download),
		Indexing:        cloneIndexing(current.Indexing),
		Revision:        current.Revision,
	}

	if patch.Servers != nil {
		next.Servers = append([]ServerRuntimeSettings(nil), (*patch.Servers)...)
	}
	if patch.Indexers != nil {
		next.Indexers = append([]IndexerRuntimeSettings(nil), (*patch.Indexers)...)
	}
	if patch.Download != nil {
		next.Download = cloneDownload(patch.Download)
	}
	if patch.Indexing != nil {
		next.Indexing = cloneIndexing(patch.Indexing)
	}
	if patch.ArrIntegrations != nil {
		next.ArrIntegrations = append([]ArrIntegrationRuntimeSettings(nil), (*patch.ArrIntegrations)...)
	}

	dropUnsupportedIndexingConcurrency(next)
	return next
}

// CloneRuntimeSettings returns a deep copy of runtime settings.
func CloneRuntimeSettings(in *RuntimeSettings) *RuntimeSettings {
	if in == nil {
		return &RuntimeSettings{}
	}

	out := &RuntimeSettings{
		Servers:         append([]ServerRuntimeSettings(nil), in.Servers...),
		Indexers:        append([]IndexerRuntimeSettings(nil), in.Indexers...),
		ArrIntegrations: append([]ArrIntegrationRuntimeSettings(nil), in.ArrIntegrations...),
		Download:        cloneDownload(in.Download),
		Indexing:        cloneIndexing(in.Indexing),
		Revision:        in.Revision,
	}
	dropUnsupportedIndexingConcurrency(out)
	return out
}

// RedactedCopy removes secrets before returning settings externally.
func RedactedCopy(in *RuntimeSettings) *RuntimeSettings {
	out := CloneRuntimeSettings(in)
	for i := range out.Servers {
		out.Servers[i].Password = ""
	}
	for i := range out.Indexers {
		out.Indexers[i].APIKey = ""
	}
	for i := range out.ArrIntegrations {
		out.ArrIntegrations[i].APIKey = ""
	}
	if out.Indexing != nil {
		dropUnsupportedIndexingConcurrency(out)
		out.Indexing.EnrichTMDB.TMDBAPIKey = ""
		out.Indexing.EnrichTMDB.TMDBAccessToken = ""
		out.Indexing.EnrichTMDB.TVDBAPIKey = ""
		out.Indexing.EnrichTMDB.TVDBPIN = ""
	}
	return out
}

func dropUnsupportedIndexingConcurrency(in *RuntimeSettings) {
	if in == nil || in.Indexing == nil {
		return
	}
	in.Indexing.ScrapeLatest.Concurrency = 0
	in.Indexing.ScrapeBackfill.Concurrency = 0
	in.Indexing.InspectDiscovery.Concurrency = 0
	in.Indexing.InspectPAR2.Concurrency = 0
	in.Indexing.InspectNFO.Concurrency = 0
	in.Indexing.InspectPassword.Concurrency = 0
}

func ValidateArrIntegrations(integrations []ArrIntegrationRuntimeSettings) error {
	seen := make(map[string]struct{}, len(integrations))

	for _, integration := range integrations {
		if !integration.Enabled {
			continue
		}

		id := strings.TrimSpace(integration.ID)
		if id == "" {
			return fmt.Errorf("arr integration id is required")
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate arr integration id %q", id)
		}
		seen[id] = struct{}{}

		kind := strings.ToLower(strings.TrimSpace(integration.Kind))
		if kind != "radarr" && kind != "sonarr" {
			return fmt.Errorf("arr integration %q kind must be radarr or sonarr", id)
		}
		if strings.TrimSpace(integration.BaseURL) == "" {
			return fmt.Errorf("arr integration %q base_url is required", id)
		}
		if strings.TrimSpace(integration.APIKey) == "" {
			return fmt.Errorf("arr integration %q api_key is required", id)
		}
	}

	return nil
}

func cloneDownload(in *DownloadRuntimeSettings) *DownloadRuntimeSettings {
	if in == nil {
		return nil
	}
	return &DownloadRuntimeSettings{
		OutDir:            in.OutDir,
		CompletedDir:      in.CompletedDir,
		CleanupExtensions: append([]string(nil), in.CleanupExtensions...),
	}
}

func cloneIndexing(in *IndexingRuntimeSettings) *IndexingRuntimeSettings {
	if in == nil {
		return nil
	}
	return &IndexingRuntimeSettings{
		Newsgroups:       append([]string(nil), in.Newsgroups...),
		ScrapeLatest:     in.ScrapeLatest,
		ScrapeBackfill:   in.ScrapeBackfill,
		Assemble:         in.Assemble,
		Release:          in.Release,
		Match:            in.Match,
		Inspect:          in.Inspect,
		InspectDiscovery: in.InspectDiscovery,
		InspectPAR2:      in.InspectPAR2,
		InspectNFO:       in.InspectNFO,
		InspectArchive:   in.InspectArchive,
		InspectPassword:  in.InspectPassword,
		InspectMedia:     in.InspectMedia,
		EnrichPreDB:      in.EnrichPreDB,
		EnrichTMDB:       in.EnrichTMDB,
	}
}

func indexStageRuntimeFromConfig(cfg config.IndexingStageConfig, defaultEnabled bool, defaultInterval float64, defaultBatch int) IndexingStageRuntimeSettings {
	return IndexingStageRuntimeSettings{
		Enabled:         boolValue(cfg.Enabled, defaultEnabled),
		IntervalMinutes: float64Value(cfg.IntervalMinutes, defaultInterval),
		BatchSize:       intValue(cfg.BatchSize, defaultBatch),
		BackoffSeconds:  intValue(cfg.BackoffSeconds, 0),
	}
}

func indexStageRuntimeFromConfigWithConcurrency(cfg config.IndexingStageConfig, defaultEnabled bool, defaultInterval float64, defaultBatch int) IndexingStageRuntimeSettings {
	out := indexStageRuntimeFromConfig(cfg, defaultEnabled, defaultInterval, defaultBatch)
	out.Concurrency = intValue(cfg.Concurrency, 1)
	return out
}

func toStageConfig(in IndexingStageRuntimeSettings) config.IndexingStageConfig {
	out := config.IndexingStageConfig{
		Enabled:         boolPtr(in.Enabled),
		IntervalMinutes: float64Ptr(in.IntervalMinutes),
		BatchSize:       intPtr(in.BatchSize),
		BackoffSeconds:  intPtr(in.BackoffSeconds),
	}
	if in.Concurrency > 0 {
		out.Concurrency = intPtr(in.Concurrency)
	}
	return out
}

func toStageConfigNoConcurrency(in IndexingStageRuntimeSettings) config.IndexingStageConfig {
	out := toStageConfig(in)
	out.Concurrency = nil
	return out
}

func boolValue(v *bool, fallback bool) bool {
	if v != nil {
		return *v
	}
	return fallback
}

func intValue(v *int, fallback int) int {
	if v != nil {
		return *v
	}
	return fallback
}

func int64Value(v *int64, fallback int64) int64 {
	if v != nil {
		return *v
	}
	return fallback
}

func float64Value(v *float64, fallback float64) float64 {
	if v != nil {
		return *v
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonZeroInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func boolPtr(v bool) *bool {
	return &v
}

func intPtr(v int) *int {
	return &v
}

func int64Ptr(v int64) *int64 {
	return &v
}

func float64Ptr(v float64) *float64 {
	return &v
}
