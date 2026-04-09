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
			ID:            s.ID,
			Host:          s.Host,
			Port:          s.Port,
			Username:      s.Username,
			Password:      s.Password,
			TLS:           s.TLS,
			MaxConnection: s.MaxConnection,
			Priority:      s.Priority,
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
		Newsgroups:              append([]string(nil), cfg.Newsgroups...),
		ScrapeBatchSize:         cfg.ScrapeBatchSize,
		ScheduleIntervalMinutes: cfg.ScheduleIntervalMinutes,
		ReleaseMinConfidence:    cfg.ReleaseMinConfidence,
		ReleaseMinCompletionPct: cfg.ReleaseMinCompletionPct,
	}

	out.ScrapeLatest = indexStageRuntimeFromConfig(cfg.ScrapeLatest, true, cfg.ScheduleIntervalMinutes, int(cfg.ScrapeBatchSize))
	out.ScrapeBackfill = indexStageRuntimeFromConfig(cfg.ScrapeBackfill, true, cfg.ScheduleIntervalMinutes, int(cfg.ScrapeBatchSize))
	out.Assemble = indexStageRuntimeFromConfig(cfg.Assemble, true, cfg.ScheduleIntervalMinutes, int(cfg.ScrapeBatchSize))
	out.Release = IndexingReleaseRuntimeSettings{
		Enabled:          boolValue(cfg.Release.Enabled, true),
		IntervalMinutes:  float64Value(cfg.Release.IntervalMinutes, cfg.ScheduleIntervalMinutes),
		BatchSize:        intValue(cfg.Release.BatchSize, 1000),
		Concurrency:      intValue(cfg.Release.Concurrency, 1),
		BackoffSeconds:   intValue(cfg.Release.BackoffSeconds, 0),
		MinConfidence:    float64Value(cfg.Release.MinConfidence, cfg.ReleaseMinConfidence),
		MinCompletionPct: float64Value(cfg.Release.MinCompletionPct, cfg.ReleaseMinCompletionPct),
	}
	out.Match = IndexingMatchRuntimeSettings{
		HighConfidenceThreshold:     float64Value(cfg.Match.HighConfidenceThreshold, 0.85),
		ProbableConfidenceThreshold: float64Value(cfg.Match.ProbableConfidenceThreshold, 0.55),
		ArticleBucketSize:           int64Value(cfg.Match.ArticleBucketSize, 5000),
	}
	out.Inspect = IndexingInspectRuntimeSettings{
		WorkDir:         firstNonEmpty(cfg.Inspect.WorkDir, cfg.InspectWorkDir, "/store/indexer/inspect"),
		MaxBytes:        firstNonZeroInt64(cfg.Inspect.MaxBytes, cfg.InspectMaxBytes, 2*1024*1024*1024),
		MaxArchiveDepth: firstNonZeroInt(cfg.Inspect.MaxArchiveDepth, cfg.InspectMaxArchiveDepth, 3),
		ToolTimeoutSecs: firstNonZeroInt(cfg.Inspect.ToolTimeoutSecs, cfg.InspectToolTimeoutSecs, 30),
		FFProbePath:     firstNonEmpty(cfg.Inspect.FFProbePath, cfg.FFProbePath, "ffprobe"),
		SevenZipPath:    firstNonEmpty(cfg.Inspect.SevenZipPath, cfg.SevenZipPath, "7z"),
		UnrarPath:       firstNonEmpty(cfg.Inspect.UnrarPath, cfg.UnrarPath, "unrar"),
		PAR2Path:        firstNonEmpty(cfg.Inspect.PAR2Path, cfg.PAR2Path, "par2"),
	}
	out.InspectPAR2 = indexStageRuntimeFromConfig(cfg.InspectPAR2, cfg.EnableInspectPAR2, cfg.ScheduleIntervalMinutes, 100)
	out.InspectNFO = indexStageRuntimeFromConfig(cfg.InspectNFO, cfg.EnableInspectNFO, cfg.ScheduleIntervalMinutes, 100)
	out.InspectArchive = indexStageRuntimeFromConfig(cfg.InspectArchive, cfg.EnableInspectArchive, cfg.ScheduleIntervalMinutes, 100)
	out.InspectPassword = indexStageRuntimeFromConfig(cfg.InspectPassword, cfg.EnableInspectPassword, cfg.ScheduleIntervalMinutes, 100)
	out.InspectMedia = indexStageRuntimeFromConfig(cfg.InspectMedia, cfg.EnableInspectMedia, cfg.ScheduleIntervalMinutes, 100)
	out.EnrichPreDB = IndexingPreDBRuntimeSettings{
		Enabled:            boolValue(cfg.EnrichPreDB.Enabled, cfg.EnableEnrichPreDB),
		IntervalMinutes:    float64Value(cfg.EnrichPreDB.IntervalMinutes, cfg.ScheduleIntervalMinutes),
		BatchSize:          intValue(cfg.EnrichPreDB.BatchSize, 100),
		Concurrency:        intValue(cfg.EnrichPreDB.Concurrency, 1),
		BackoffSeconds:     intValue(cfg.EnrichPreDB.BackoffSeconds, 0),
		Provider:           firstNonEmpty(cfg.EnrichPreDB.Provider, cfg.PreDBProvider, "club,me"),
		BaseURL:            firstNonEmpty(cfg.EnrichPreDB.BaseURL, cfg.PreDBBaseURL, "https://predb.club/api/v1"),
		FeedURL:            firstNonEmpty(cfg.EnrichPreDB.FeedURL, cfg.PreDBFeedURL, "https://predb.me/?rss=1"),
		DumpURL:            firstNonEmpty(cfg.EnrichPreDB.DumpURL, cfg.PreDBDumpURL),
		HTTPTimeoutSeconds: intValue(cfg.EnrichPreDB.HTTPTimeoutSeconds, 10),
		BackfillPageSize:   intValue(cfg.EnrichPreDB.BackfillPageSize, 1000),
		MaxBackfillPages:   intValue(cfg.EnrichPreDB.MaxBackfillPages, 250),
	}
	out.EnrichTMDB = IndexingTMDBRuntimeSettings{
		Enabled:            boolValue(cfg.EnrichTMDB.Enabled, cfg.EnableEnrichTMDB),
		IntervalMinutes:    float64Value(cfg.EnrichTMDB.IntervalMinutes, cfg.ScheduleIntervalMinutes),
		BatchSize:          intValue(cfg.EnrichTMDB.BatchSize, 100),
		Concurrency:        intValue(cfg.EnrichTMDB.Concurrency, 1),
		BackoffSeconds:     intValue(cfg.EnrichTMDB.BackoffSeconds, 0),
		HTTPTimeoutSeconds: intValue(cfg.EnrichTMDB.HTTPTimeoutSeconds, 15),
		TMDBAPIKey:         firstNonEmpty(cfg.EnrichTMDB.TMDBAPIKey, cfg.TMDBAPIKey),
		TMDBAccessToken:    firstNonEmpty(cfg.EnrichTMDB.TMDBAccessToken, cfg.TMDBAccessToken),
		TMDBBaseURL:        firstNonEmpty(cfg.EnrichTMDB.TMDBBaseURL, cfg.TMDBBaseURL, "https://api.themoviedb.org/3"),
		TVDBAPIKey:         firstNonEmpty(cfg.EnrichTMDB.TVDBAPIKey, cfg.TVDBAPIKey),
		TVDBPIN:            firstNonEmpty(cfg.EnrichTMDB.TVDBPIN, cfg.TVDBPIN),
		TVDBBaseURL:        firstNonEmpty(cfg.EnrichTMDB.TVDBBaseURL, cfg.TVDBBaseURL, "https://api4.thetvdb.com/v4"),
	}

	out.InspectWorkDir = out.Inspect.WorkDir
	out.ReleaseMinConfidence = out.Release.MinConfidence
	out.ReleaseMinCompletionPct = out.Release.MinCompletionPct
	out.InspectMaxBytes = out.Inspect.MaxBytes
	out.InspectMaxArchiveDepth = out.Inspect.MaxArchiveDepth
	out.InspectToolTimeoutSecs = out.Inspect.ToolTimeoutSecs
	out.EnableInspectPAR2 = out.InspectPAR2.Enabled
	out.EnableInspectNFO = out.InspectNFO.Enabled
	out.EnableInspectArchive = out.InspectArchive.Enabled
	out.EnableInspectPassword = out.InspectPassword.Enabled
	out.EnableInspectMedia = out.InspectMedia.Enabled
	out.EnableEnrichPreDB = out.EnrichPreDB.Enabled
	out.EnableEnrichTMDB = out.EnrichTMDB.Enabled
	out.PreDBProvider = out.EnrichPreDB.Provider
	out.PreDBBaseURL = out.EnrichPreDB.BaseURL
	out.PreDBFeedURL = out.EnrichPreDB.FeedURL
	out.PreDBDumpURL = out.EnrichPreDB.DumpURL
	out.TMDBAPIKey = out.EnrichTMDB.TMDBAPIKey
	out.TMDBAccessToken = out.EnrichTMDB.TMDBAccessToken
	out.TMDBBaseURL = out.EnrichTMDB.TMDBBaseURL
	out.TVDBAPIKey = out.EnrichTMDB.TVDBAPIKey
	out.TVDBPIN = out.EnrichTMDB.TVDBPIN
	out.TVDBBaseURL = out.EnrichTMDB.TVDBBaseURL
	out.FFProbePath = out.Inspect.FFProbePath
	out.SevenZipPath = out.Inspect.SevenZipPath
	out.UnrarPath = out.Inspect.UnrarPath
	out.PAR2Path = out.Inspect.PAR2Path

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
				ID:            strings.TrimSpace(s.ID),
				Host:          strings.TrimSpace(s.Host),
				Port:          s.Port,
				Username:      s.Username,
				Password:      s.Password,
				TLS:           s.TLS,
				MaxConnection: s.MaxConnection,
				Priority:      s.Priority,
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
		if indexing.ScrapeBatchSize > 0 {
			effective.Indexing.ScrapeBatchSize = indexing.ScrapeBatchSize
		}
		if indexing.ScheduleIntervalMinutes > 0 {
			effective.Indexing.ScheduleIntervalMinutes = indexing.ScheduleIntervalMinutes
		}
		if indexing.ReleaseMinConfidence > 0 {
			effective.Indexing.ReleaseMinConfidence = indexing.ReleaseMinConfidence
		}
		if indexing.ReleaseMinCompletionPct >= 0 {
			effective.Indexing.ReleaseMinCompletionPct = indexing.ReleaseMinCompletionPct
		}
		if strings.TrimSpace(indexing.InspectWorkDir) != "" {
			effective.Indexing.InspectWorkDir = indexing.InspectWorkDir
		}
		if indexing.InspectMaxBytes > 0 {
			effective.Indexing.InspectMaxBytes = indexing.InspectMaxBytes
		}
		if indexing.InspectMaxArchiveDepth > 0 {
			effective.Indexing.InspectMaxArchiveDepth = indexing.InspectMaxArchiveDepth
		}
		if indexing.InspectToolTimeoutSecs > 0 {
			effective.Indexing.InspectToolTimeoutSecs = indexing.InspectToolTimeoutSecs
		}
		effective.Indexing.EnableInspectPAR2 = indexing.EnableInspectPAR2
		effective.Indexing.EnableInspectNFO = indexing.EnableInspectNFO
		effective.Indexing.EnableInspectArchive = indexing.EnableInspectArchive
		effective.Indexing.EnableInspectPassword = indexing.EnableInspectPassword
		effective.Indexing.EnableInspectMedia = indexing.EnableInspectMedia
		effective.Indexing.EnableEnrichPreDB = indexing.EnableEnrichPreDB
		effective.Indexing.EnableEnrichTMDB = indexing.EnableEnrichTMDB
		if strings.TrimSpace(indexing.PreDBProvider) != "" {
			effective.Indexing.PreDBProvider = indexing.PreDBProvider
		}
		if strings.TrimSpace(indexing.PreDBBaseURL) != "" {
			effective.Indexing.PreDBBaseURL = indexing.PreDBBaseURL
		}
		if strings.TrimSpace(indexing.PreDBFeedURL) != "" {
			effective.Indexing.PreDBFeedURL = indexing.PreDBFeedURL
		}
		if strings.TrimSpace(indexing.PreDBDumpURL) != "" {
			effective.Indexing.PreDBDumpURL = indexing.PreDBDumpURL
		}
		if strings.TrimSpace(indexing.TMDBAPIKey) != "" {
			effective.Indexing.TMDBAPIKey = indexing.TMDBAPIKey
		}
		if strings.TrimSpace(indexing.TMDBAccessToken) != "" {
			effective.Indexing.TMDBAccessToken = indexing.TMDBAccessToken
		}
		if strings.TrimSpace(indexing.TMDBBaseURL) != "" {
			effective.Indexing.TMDBBaseURL = indexing.TMDBBaseURL
		}
		if strings.TrimSpace(indexing.TVDBAPIKey) != "" {
			effective.Indexing.TVDBAPIKey = indexing.TVDBAPIKey
		}
		if strings.TrimSpace(indexing.TVDBPIN) != "" {
			effective.Indexing.TVDBPIN = indexing.TVDBPIN
		}
		if strings.TrimSpace(indexing.TVDBBaseURL) != "" {
			effective.Indexing.TVDBBaseURL = indexing.TVDBBaseURL
		}
		if strings.TrimSpace(indexing.FFProbePath) != "" {
			effective.Indexing.FFProbePath = indexing.FFProbePath
		}
		if strings.TrimSpace(indexing.SevenZipPath) != "" {
			effective.Indexing.SevenZipPath = indexing.SevenZipPath
		}
		if strings.TrimSpace(indexing.UnrarPath) != "" {
			effective.Indexing.UnrarPath = indexing.UnrarPath
		}
		if strings.TrimSpace(indexing.PAR2Path) != "" {
			effective.Indexing.PAR2Path = indexing.PAR2Path
		}

		if strings.TrimSpace(indexing.Inspect.WorkDir) != "" {
			effective.Indexing.Inspect.WorkDir = indexing.Inspect.WorkDir
			effective.Indexing.InspectWorkDir = indexing.Inspect.WorkDir
		}
		if indexing.Inspect.MaxBytes > 0 {
			effective.Indexing.Inspect.MaxBytes = indexing.Inspect.MaxBytes
			effective.Indexing.InspectMaxBytes = indexing.Inspect.MaxBytes
		}
		if indexing.Inspect.MaxArchiveDepth > 0 {
			effective.Indexing.Inspect.MaxArchiveDepth = indexing.Inspect.MaxArchiveDepth
			effective.Indexing.InspectMaxArchiveDepth = indexing.Inspect.MaxArchiveDepth
		}
		if indexing.Inspect.ToolTimeoutSecs > 0 {
			effective.Indexing.Inspect.ToolTimeoutSecs = indexing.Inspect.ToolTimeoutSecs
			effective.Indexing.InspectToolTimeoutSecs = indexing.Inspect.ToolTimeoutSecs
		}
		if strings.TrimSpace(indexing.Inspect.FFProbePath) != "" {
			effective.Indexing.Inspect.FFProbePath = indexing.Inspect.FFProbePath
			effective.Indexing.FFProbePath = indexing.Inspect.FFProbePath
		}
		if strings.TrimSpace(indexing.Inspect.SevenZipPath) != "" {
			effective.Indexing.Inspect.SevenZipPath = indexing.Inspect.SevenZipPath
			effective.Indexing.SevenZipPath = indexing.Inspect.SevenZipPath
		}
		if strings.TrimSpace(indexing.Inspect.UnrarPath) != "" {
			effective.Indexing.Inspect.UnrarPath = indexing.Inspect.UnrarPath
			effective.Indexing.UnrarPath = indexing.Inspect.UnrarPath
		}
		if strings.TrimSpace(indexing.Inspect.PAR2Path) != "" {
			effective.Indexing.Inspect.PAR2Path = indexing.Inspect.PAR2Path
			effective.Indexing.PAR2Path = indexing.Inspect.PAR2Path
		}

		effective.Indexing.ScrapeLatest = toStageConfig(indexing.ScrapeLatest)
		effective.Indexing.ScrapeBackfill = toStageConfig(indexing.ScrapeBackfill)
		effective.Indexing.Assemble = toStageConfig(indexing.Assemble)
		effective.Indexing.Release = config.IndexingReleaseConfig{
			Enabled:          boolPtr(indexing.Release.Enabled),
			IntervalMinutes:  float64Ptr(indexing.Release.IntervalMinutes),
			BatchSize:        intPtr(indexing.Release.BatchSize),
			Concurrency:      intPtr(indexing.Release.Concurrency),
			BackoffSeconds:   intPtr(indexing.Release.BackoffSeconds),
			MinConfidence:    float64Ptr(indexing.Release.MinConfidence),
			MinCompletionPct: float64Ptr(indexing.Release.MinCompletionPct),
		}
		if indexing.Release.MinConfidence > 0 {
			effective.Indexing.ReleaseMinConfidence = indexing.Release.MinConfidence
		}
		if indexing.Release.MinCompletionPct >= 0 {
			effective.Indexing.ReleaseMinCompletionPct = indexing.Release.MinCompletionPct
		}
		effective.Indexing.Match = config.IndexingMatchConfig{
			HighConfidenceThreshold:     float64Ptr(indexing.Match.HighConfidenceThreshold),
			ProbableConfidenceThreshold: float64Ptr(indexing.Match.ProbableConfidenceThreshold),
			ArticleBucketSize:           int64Ptr(indexing.Match.ArticleBucketSize),
		}
		effective.Indexing.InspectPAR2 = toStageConfig(indexing.InspectPAR2)
		effective.Indexing.InspectNFO = toStageConfig(indexing.InspectNFO)
		effective.Indexing.InspectArchive = toStageConfig(indexing.InspectArchive)
		effective.Indexing.InspectPassword = toStageConfig(indexing.InspectPassword)
		effective.Indexing.InspectMedia = toStageConfig(indexing.InspectMedia)
		effective.Indexing.EnrichPreDB = config.IndexingPreDBConfig{
			Enabled:            boolPtr(indexing.EnrichPreDB.Enabled),
			IntervalMinutes:    float64Ptr(indexing.EnrichPreDB.IntervalMinutes),
			BatchSize:          intPtr(indexing.EnrichPreDB.BatchSize),
			Concurrency:        intPtr(indexing.EnrichPreDB.Concurrency),
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
			Concurrency:        intPtr(indexing.EnrichTMDB.Concurrency),
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

	return next
}

// CloneRuntimeSettings returns a deep copy of runtime settings.
func CloneRuntimeSettings(in *RuntimeSettings) *RuntimeSettings {
	if in == nil {
		return &RuntimeSettings{}
	}

	return &RuntimeSettings{
		Servers:         append([]ServerRuntimeSettings(nil), in.Servers...),
		Indexers:        append([]IndexerRuntimeSettings(nil), in.Indexers...),
		ArrIntegrations: append([]ArrIntegrationRuntimeSettings(nil), in.ArrIntegrations...),
		Download:        cloneDownload(in.Download),
		Indexing:        cloneIndexing(in.Indexing),
		Revision:        in.Revision,
	}
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
		out.Indexing.TMDBAPIKey = ""
		out.Indexing.TMDBAccessToken = ""
		out.Indexing.TVDBAPIKey = ""
		out.Indexing.TVDBPIN = ""
		out.Indexing.EnrichTMDB.TMDBAPIKey = ""
		out.Indexing.EnrichTMDB.TMDBAccessToken = ""
		out.Indexing.EnrichTMDB.TVDBAPIKey = ""
		out.Indexing.EnrichTMDB.TVDBPIN = ""
	}
	return out
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
		Newsgroups:              append([]string(nil), in.Newsgroups...),
		ScrapeBatchSize:         in.ScrapeBatchSize,
		ScheduleIntervalMinutes: in.ScheduleIntervalMinutes,
		ReleaseMinConfidence:    in.ReleaseMinConfidence,
		ReleaseMinCompletionPct: in.ReleaseMinCompletionPct,
		InspectWorkDir:          in.InspectWorkDir,
		InspectMaxBytes:         in.InspectMaxBytes,
		InspectMaxArchiveDepth:  in.InspectMaxArchiveDepth,
		InspectToolTimeoutSecs:  in.InspectToolTimeoutSecs,
		EnableInspectPAR2:       in.EnableInspectPAR2,
		EnableInspectNFO:        in.EnableInspectNFO,
		EnableInspectArchive:    in.EnableInspectArchive,
		EnableInspectPassword:   in.EnableInspectPassword,
		EnableInspectMedia:      in.EnableInspectMedia,
		EnableEnrichPreDB:       in.EnableEnrichPreDB,
		EnableEnrichTMDB:        in.EnableEnrichTMDB,
		PreDBProvider:           in.PreDBProvider,
		PreDBBaseURL:            in.PreDBBaseURL,
		PreDBFeedURL:            in.PreDBFeedURL,
		PreDBDumpURL:            in.PreDBDumpURL,
		TMDBAPIKey:              in.TMDBAPIKey,
		TMDBAccessToken:         in.TMDBAccessToken,
		TMDBBaseURL:             in.TMDBBaseURL,
		TVDBAPIKey:              in.TVDBAPIKey,
		TVDBPIN:                 in.TVDBPIN,
		TVDBBaseURL:             in.TVDBBaseURL,
		FFProbePath:             in.FFProbePath,
		SevenZipPath:            in.SevenZipPath,
		UnrarPath:               in.UnrarPath,
		PAR2Path:                in.PAR2Path,
		ScrapeLatest:            in.ScrapeLatest,
		ScrapeBackfill:          in.ScrapeBackfill,
		Assemble:                in.Assemble,
		Release:                 in.Release,
		Match:                   in.Match,
		Inspect:                 in.Inspect,
		InspectPAR2:             in.InspectPAR2,
		InspectNFO:              in.InspectNFO,
		InspectArchive:          in.InspectArchive,
		InspectPassword:         in.InspectPassword,
		InspectMedia:            in.InspectMedia,
		EnrichPreDB:             in.EnrichPreDB,
		EnrichTMDB:              in.EnrichTMDB,
	}
}

func indexStageRuntimeFromConfig(cfg config.IndexingStageConfig, defaultEnabled bool, defaultInterval float64, defaultBatch int) IndexingStageRuntimeSettings {
	return IndexingStageRuntimeSettings{
		Enabled:         boolValue(cfg.Enabled, defaultEnabled),
		IntervalMinutes: float64Value(cfg.IntervalMinutes, defaultInterval),
		BatchSize:       intValue(cfg.BatchSize, defaultBatch),
		Concurrency:     intValue(cfg.Concurrency, 1),
		BackoffSeconds:  intValue(cfg.BackoffSeconds, 0),
	}
}

func toStageConfig(in IndexingStageRuntimeSettings) config.IndexingStageConfig {
	return config.IndexingStageConfig{
		Enabled:         boolPtr(in.Enabled),
		IntervalMinutes: float64Ptr(in.IntervalMinutes),
		BatchSize:       intPtr(in.BatchSize),
		Concurrency:     intPtr(in.Concurrency),
		BackoffSeconds:  intPtr(in.BackoffSeconds),
	}
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
