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
	}
}
