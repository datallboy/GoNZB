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
		Indexing: &IndexingRuntimeSettings{
			Newsgroups:              append([]string(nil), cfg.Indexing.Newsgroups...),
			ScrapeBatchSize:         cfg.Indexing.ScrapeBatchSize,
			ScheduleIntervalMinutes: cfg.Indexing.ScheduleIntervalMinutes,
			ReleaseMinConfidence:    cfg.Indexing.ReleaseMinConfidence,
			ReleaseMinCompletionPct: cfg.Indexing.ReleaseMinCompletionPct,
			InspectWorkDir:          cfg.Indexing.InspectWorkDir,
			InspectMaxBytes:         cfg.Indexing.InspectMaxBytes,
			InspectMaxArchiveDepth:  cfg.Indexing.InspectMaxArchiveDepth,
			InspectToolTimeoutSecs:  cfg.Indexing.InspectToolTimeoutSecs,
			EnableInspectPAR2:       cfg.Indexing.EnableInspectPAR2,
			EnableInspectNFO:        cfg.Indexing.EnableInspectNFO,
			EnableInspectArchive:    cfg.Indexing.EnableInspectArchive,
			EnableInspectPassword:   cfg.Indexing.EnableInspectPassword,
			EnableInspectMedia:      cfg.Indexing.EnableInspectMedia,
			EnableEnrichPreDB:       cfg.Indexing.EnableEnrichPreDB,
			EnableEnrichTMDB:        cfg.Indexing.EnableEnrichTMDB,
			PreDBProvider:           cfg.Indexing.PreDBProvider,
			PreDBBaseURL:            cfg.Indexing.PreDBBaseURL,
			PreDBFeedURL:            cfg.Indexing.PreDBFeedURL,
			PreDBDumpURL:            cfg.Indexing.PreDBDumpURL,
			TMDBAPIKey:              cfg.Indexing.TMDBAPIKey,
			TMDBAccessToken:         cfg.Indexing.TMDBAccessToken,
			TMDBBaseURL:             cfg.Indexing.TMDBBaseURL,
			TVDBAPIKey:              cfg.Indexing.TVDBAPIKey,
			TVDBPIN:                 cfg.Indexing.TVDBPIN,
			TVDBBaseURL:             cfg.Indexing.TVDBBaseURL,
			FFProbePath:             cfg.Indexing.FFProbePath,
			SevenZipPath:            cfg.Indexing.SevenZipPath,
			UnrarPath:               cfg.Indexing.UnrarPath,
			PAR2Path:                cfg.Indexing.PAR2Path,
		},
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
		if runtime.Indexing.Newsgroups != nil {
			effective.Indexing.Newsgroups = append([]string(nil), runtime.Indexing.Newsgroups...)
		}
		if runtime.Indexing.ScrapeBatchSize > 0 {
			effective.Indexing.ScrapeBatchSize = runtime.Indexing.ScrapeBatchSize
		}
		if runtime.Indexing.ScheduleIntervalMinutes > 0 {
			effective.Indexing.ScheduleIntervalMinutes = runtime.Indexing.ScheduleIntervalMinutes
		}
		if runtime.Indexing.ReleaseMinConfidence > 0 {
			effective.Indexing.ReleaseMinConfidence = runtime.Indexing.ReleaseMinConfidence
		}
		if runtime.Indexing.ReleaseMinCompletionPct >= 0 {
			effective.Indexing.ReleaseMinCompletionPct = runtime.Indexing.ReleaseMinCompletionPct
		}
		if strings.TrimSpace(runtime.Indexing.InspectWorkDir) != "" {
			effective.Indexing.InspectWorkDir = runtime.Indexing.InspectWorkDir
		}
		if runtime.Indexing.InspectMaxBytes > 0 {
			effective.Indexing.InspectMaxBytes = runtime.Indexing.InspectMaxBytes
		}
		if runtime.Indexing.InspectMaxArchiveDepth > 0 {
			effective.Indexing.InspectMaxArchiveDepth = runtime.Indexing.InspectMaxArchiveDepth
		}
		if runtime.Indexing.InspectToolTimeoutSecs > 0 {
			effective.Indexing.InspectToolTimeoutSecs = runtime.Indexing.InspectToolTimeoutSecs
		}
		effective.Indexing.EnableInspectPAR2 = runtime.Indexing.EnableInspectPAR2
		effective.Indexing.EnableInspectNFO = runtime.Indexing.EnableInspectNFO
		effective.Indexing.EnableInspectArchive = runtime.Indexing.EnableInspectArchive
		effective.Indexing.EnableInspectPassword = runtime.Indexing.EnableInspectPassword
		effective.Indexing.EnableInspectMedia = runtime.Indexing.EnableInspectMedia
		effective.Indexing.EnableEnrichPreDB = runtime.Indexing.EnableEnrichPreDB
		effective.Indexing.EnableEnrichTMDB = runtime.Indexing.EnableEnrichTMDB
		if strings.TrimSpace(runtime.Indexing.PreDBProvider) != "" {
			effective.Indexing.PreDBProvider = runtime.Indexing.PreDBProvider
		}
		if strings.TrimSpace(runtime.Indexing.PreDBBaseURL) != "" {
			effective.Indexing.PreDBBaseURL = runtime.Indexing.PreDBBaseURL
		}
		if strings.TrimSpace(runtime.Indexing.PreDBFeedURL) != "" {
			effective.Indexing.PreDBFeedURL = runtime.Indexing.PreDBFeedURL
		}
		if strings.TrimSpace(runtime.Indexing.PreDBDumpURL) != "" {
			effective.Indexing.PreDBDumpURL = runtime.Indexing.PreDBDumpURL
		}
		if strings.TrimSpace(runtime.Indexing.TMDBAPIKey) != "" {
			effective.Indexing.TMDBAPIKey = runtime.Indexing.TMDBAPIKey
		}
		if strings.TrimSpace(runtime.Indexing.TMDBAccessToken) != "" {
			effective.Indexing.TMDBAccessToken = runtime.Indexing.TMDBAccessToken
		}
		if strings.TrimSpace(runtime.Indexing.TMDBBaseURL) != "" {
			effective.Indexing.TMDBBaseURL = runtime.Indexing.TMDBBaseURL
		}
		if strings.TrimSpace(runtime.Indexing.TVDBAPIKey) != "" {
			effective.Indexing.TVDBAPIKey = runtime.Indexing.TVDBAPIKey
		}
		if strings.TrimSpace(runtime.Indexing.TVDBPIN) != "" {
			effective.Indexing.TVDBPIN = runtime.Indexing.TVDBPIN
		}
		if strings.TrimSpace(runtime.Indexing.TVDBBaseURL) != "" {
			effective.Indexing.TVDBBaseURL = runtime.Indexing.TVDBBaseURL
		}
		if strings.TrimSpace(runtime.Indexing.FFProbePath) != "" {
			effective.Indexing.FFProbePath = runtime.Indexing.FFProbePath
		}
		if strings.TrimSpace(runtime.Indexing.SevenZipPath) != "" {
			effective.Indexing.SevenZipPath = runtime.Indexing.SevenZipPath
		}
		if strings.TrimSpace(runtime.Indexing.UnrarPath) != "" {
			effective.Indexing.UnrarPath = runtime.Indexing.UnrarPath
		}
		if strings.TrimSpace(runtime.Indexing.PAR2Path) != "" {
			effective.Indexing.PAR2Path = runtime.Indexing.PAR2Path
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
		Servers:  append([]ServerRuntimeSettings(nil), current.Servers...),
		Indexers: append([]IndexerRuntimeSettings(nil), current.Indexers...),
		Download: cloneDownload(current.Download),
		Indexing: cloneIndexing(current.Indexing),
		Revision: current.Revision,
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
	}
}
