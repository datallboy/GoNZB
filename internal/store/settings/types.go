package settings

import (
	"encoding/json"
	"strings"

	"github.com/datallboy/gonzb/internal/infra/config"
)

// runtime-editable settings model for Milestone 8.X chunk 1.
// Bootstrap-only fields stay in config.yaml/env and are not represented here.
type RuntimeSettings struct {
	Servers  []ServerRuntimeSettings  `json:"servers,omitempty"`
	Indexers []IndexerRuntimeSettings `json:"indexers,omitempty"`
	Download *DownloadRuntimeSettings `json:"download,omitempty"`
	Indexing *IndexingRuntimeSettings `json:"indexing,omitempty"`
	Revision int64                    `json:"revision,omitempty"`
}

type RuntimeSettingsPatch struct {
	Servers  *[]ServerRuntimeSettings  `json:"servers,omitempty"`
	Indexers *[]IndexerRuntimeSettings `json:"indexers,omitempty"`
	Download *DownloadRuntimeSettings  `json:"download,omitempty"`
	Indexing *IndexingRuntimeSettings  `json:"indexing,omitempty"`
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
}

// derive editable runtime state from current effective config.
func FromConfig(cfg *config.Config) *RuntimeSettings {
	if cfg == nil {
		return &RuntimeSettings{}
	}

	out := &RuntimeSettings{
		Servers:  make([]ServerRuntimeSettings, 0, len(cfg.Servers)),
		Indexers: make([]IndexerRuntimeSettings, 0, len(cfg.Indexers)),
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

// apply runtime-editable settings on top of bootstrap config.
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

// exported patch helper for admin API preview/validation path.
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

	return next
}

// explicit clone used when persisting a validated full snapshot.
func CloneRuntimeSettings(in *RuntimeSettings) *RuntimeSettings {
	if in == nil {
		return &RuntimeSettings{}
	}

	return &RuntimeSettings{
		Servers:  append([]ServerRuntimeSettings(nil), in.Servers...),
		Indexers: append([]IndexerRuntimeSettings(nil), in.Indexers...),
		Download: cloneDownload(in.Download),
		Indexing: cloneIndexing(in.Indexing),
		Revision: in.Revision,
	}
}

// redact runtime secrets before returning settings through API.
func RedactedCopy(in *RuntimeSettings) *RuntimeSettings {
	out := CloneRuntimeSettings(in)
	for i := range out.Servers {
		out.Servers[i].Password = ""
	}
	for i := range out.Indexers {
		out.Indexers[i].APIKey = ""
	}
	return out
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

func encodeRuntimeSettings(v *RuntimeSettings) ([]byte, error) {
	if v == nil {
		v = &RuntimeSettings{}
	}
	return json.Marshal(v)
}
