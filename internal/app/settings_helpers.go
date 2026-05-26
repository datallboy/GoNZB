package app

import (
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/infra/config"
)

func DefaultRuntimeSettings() *RuntimeSettings {
	return &RuntimeSettings{
		Servers:           []ServerRuntimeSettings{},
		DownloaderServers: []ServerRuntimeSettings{},
		IndexerServers:    []ServerRuntimeSettings{},
		Indexers:          []IndexerRuntimeSettings{},
		Aggregator:        &AggregatorRuntimeSettings{},
		Download: &DownloadRuntimeSettings{
			OutDir:            "./downloads",
			CompletedDir:      "./downloads/completed",
			CleanupExtensions: []string{"nzb", "par2", "sfv", "nfo"},
		},
		NNTPPool: DefaultNNTPPoolRuntimeSettings(),
		Indexing: &IndexingRuntimeSettings{
			Newsgroups:               []string{},
			BackfillUntilDateByGroup: map[string]string{},
			ScrapeLatest:             defaultStage(false, 10, 5000, 0),
			ScrapeBackfill:           defaultStage(false, 10, 5000, 0),
			Assemble:                 defaultAssembleStage(false, 10, 5000, 1),
			AssembleLaneA:            defaultAssembleStage(false, 2, 5000, 1),
			AssembleLaneB:            defaultAssembleStage(false, 10, 2500, 1),
			RecoverYEnc:              defaultStage(false, 10, 25, 1),
			Release:                  defaultReleaseStage(false),
			Match:                    IndexingMatchRuntimeSettings{HighConfidenceThreshold: 0.85, ProbableConfidenceThreshold: 0.55, ArticleBucketSize: 5000},
			Inspect:                  IndexingInspectRuntimeSettings{WorkDir: "/store/indexer/inspect", WorkspaceBackend: "auto", MemoryWorkDir: "/dev/shm/gonzb-inspect", MaxBytes: 2 * 1024 * 1024 * 1024, MinBinaryBytes: 0, MaxBinaryBytes: 0, BlockedMagicHex: []string{"52434C4F4E45"}, MaxArchiveDepth: 3, ToolTimeoutSecs: 30, FFProbePath: "ffprobe", SevenZipPath: "7z", UnrarPath: "unrar", PAR2Path: "par2"},
			InspectDiscovery:         defaultStage(false, 10, 100, 0),
			InspectPAR2:              defaultStage(false, 10, 100, 4),
			InspectNFO:               defaultStage(false, 10, 100, 0),
			InspectArchive:           defaultStage(false, 10, 100, 1),
			InspectPassword:          defaultStage(false, 10, 100, 0),
			InspectMedia:             defaultStage(false, 10, 100, 1),
			EnrichPreDB:              defaultPreDBStage(false),
			EnrichTMDB:               defaultTMDBStage(false),
		},
		ArrIntegrations: []ArrIntegrationRuntimeSettings{},
	}
}

func WithRuntimeDefaults(in *RuntimeSettings) *RuntimeSettings {
	defaults := DefaultRuntimeSettings()
	if in == nil {
		return defaults
	}
	out := CloneRuntimeSettings(in)
	if out.Servers == nil {
		out.Servers = []ServerRuntimeSettings{}
	}
	if out.DownloaderServers == nil {
		out.DownloaderServers = []ServerRuntimeSettings{}
	}
	if out.IndexerServers == nil {
		out.IndexerServers = []ServerRuntimeSettings{}
	}
	if out.Indexers == nil {
		out.Indexers = []IndexerRuntimeSettings{}
	}
	if out.ArrIntegrations == nil {
		out.ArrIntegrations = []ArrIntegrationRuntimeSettings{}
	}
	if out.Aggregator == nil {
		out.Aggregator = defaults.Aggregator
	}
	if out.Download == nil {
		out.Download = defaults.Download
	}
	out.NNTPPool = mergeNNTPPoolRuntimeSettings(defaults.NNTPPool, out.NNTPPool)
	if out.Indexing == nil {
		out.Indexing = defaults.Indexing
	}
	return out
}

func defaultStage(enabled bool, interval float64, batch, concurrency int) IndexingStageRuntimeSettings {
	return IndexingStageRuntimeSettings{Enabled: enabled, IntervalMinutes: interval, BatchSize: batch, Concurrency: concurrency}
}

func defaultAssembleStage(enabled bool, interval float64, batch, concurrency int) IndexingStageRuntimeSettings {
	stage := defaultStage(enabled, interval, batch, concurrency)
	stage.BinaryUpsertDBChunkSize = 250
	return stage
}

func defaultReleaseStage(enabled bool) IndexingReleaseRuntimeSettings {
	return IndexingReleaseRuntimeSettings{
		Enabled: enabled, IntervalMinutes: 10, BatchSize: 1000, MinConfidence: 0.55,
		MinCompletionPct: 0, MinExpectedFileCoveragePct: 90, RequireExpectedFileCountForContextualObfuscated: true,
	}
}

func defaultPreDBStage(enabled bool) IndexingPreDBRuntimeSettings {
	return IndexingPreDBRuntimeSettings{
		Enabled: enabled, IntervalMinutes: 10, BatchSize: 100, Provider: "club,me",
		BaseURL: "https://predb.club/api/v1", FeedURL: "https://predb.me/?rss=1",
		HTTPTimeoutSeconds: 10, BackfillPageSize: 1000, MaxBackfillPages: 250,
	}
}

func defaultTMDBStage(enabled bool) IndexingTMDBRuntimeSettings {
	return IndexingTMDBRuntimeSettings{
		Enabled: enabled, IntervalMinutes: 10, BatchSize: 100, HTTPTimeoutSeconds: 15,
		TMDBBaseURL: "https://api.themoviedb.org/3", TVDBBaseURL: "https://api4.thetvdb.com/v4",
	}
}

// FromConfig derives editable runtime state from current effective config.
func FromConfig(cfg *config.Config) *RuntimeSettings {
	if cfg == nil {
		return &RuntimeSettings{}
	}

	out := &RuntimeSettings{
		Servers:           make([]ServerRuntimeSettings, 0, len(cfg.Servers)),
		DownloaderServers: make([]ServerRuntimeSettings, 0, len(cfg.Servers)),
		IndexerServers:    make([]ServerRuntimeSettings, 0, len(cfg.Servers)),
		Indexers:          make([]IndexerRuntimeSettings, 0, len(cfg.Indexers)),
		Aggregator:        aggregatorRuntimeFromConfig(cfg.Aggregator),
		ArrIntegrations:   []ArrIntegrationRuntimeSettings{},
		Download: &DownloadRuntimeSettings{
			OutDir:            cfg.Download.OutDir,
			CompletedDir:      cfg.Download.CompletedDir,
			CleanupExtensions: append([]string(nil), cfg.Download.CleanupExtensions...),
		},
		NNTPPool: DefaultNNTPPoolRuntimeSettings(),
		Indexing: func() *IndexingRuntimeSettings {
			indexing := IndexingRuntimeFromConfig(cfg.Indexing)
			return &indexing
		}(),
	}

	for _, s := range cfg.Servers {
		server := ServerRuntimeSettings{
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
		}
		out.Servers = append(out.Servers, server)
		out.DownloaderServers = append(out.DownloaderServers, server)
		out.IndexerServers = append(out.IndexerServers, server)
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
		Newsgroups:               append([]string(nil), cfg.Newsgroups...),
		BackfillUntilDateByGroup: cloneStringMap(cfg.BackfillUntilDateByGroup),
	}

	out.ScrapeLatest = indexStageRuntimeFromConfig(cfg.ScrapeLatest, true, 10, 5000)
	out.ScrapeBackfill = indexStageRuntimeFromConfig(cfg.ScrapeBackfill, true, 10, 5000)
	out.Assemble = indexStageRuntimeFromConfigWithConcurrency(cfg.Assemble, true, 10, 5000)
	out.AssembleLaneA = indexStageRuntimeFromConfigWithConcurrency(cfg.AssembleLaneA, false, 2, 5000)
	out.AssembleLaneB = indexStageRuntimeFromConfigWithConcurrency(cfg.AssembleLaneB, false, 10, 2500)
	out.RecoverYEnc = indexStageRuntimeFromConfigWithConcurrency(cfg.RecoverYEnc, false, 10, 25)
	out.Release = IndexingReleaseRuntimeSettings{
		Enabled:                    boolValue(cfg.Release.Enabled, true),
		IntervalMinutes:            float64Value(cfg.Release.IntervalMinutes, 10),
		BatchSize:                  intValue(cfg.Release.BatchSize, 1000),
		BackoffSeconds:             intValue(cfg.Release.BackoffSeconds, 0),
		MinConfidence:              float64Value(cfg.Release.MinConfidence, 0.55),
		MinCompletionPct:           float64Value(cfg.Release.MinCompletionPct, 0),
		MinExpectedFileCoveragePct: float64Value(cfg.Release.MinExpectedFileCoveragePct, 90),
		RequireExpectedFileCountForContextualObfuscated: boolValue(cfg.Release.RequireExpectedFileCountForContextualObfuscated, true),
	}
	out.Match = IndexingMatchRuntimeSettings{
		HighConfidenceThreshold:     float64Value(cfg.Match.HighConfidenceThreshold, 0.85),
		ProbableConfidenceThreshold: float64Value(cfg.Match.ProbableConfidenceThreshold, 0.55),
		ArticleBucketSize:           int64Value(cfg.Match.ArticleBucketSize, 5000),
	}
	out.Inspect = IndexingInspectRuntimeSettings{
		WorkDir:          firstNonEmpty(cfg.Inspect.WorkDir, "/store/indexer/inspect"),
		WorkspaceBackend: firstNonEmpty(cfg.Inspect.WorkspaceBackend, "auto"),
		MemoryWorkDir:    firstNonEmpty(cfg.Inspect.MemoryWorkDir, "/dev/shm/gonzb-inspect"),
		MaxBytes:         firstNonZeroInt64(cfg.Inspect.MaxBytes, 2*1024*1024*1024),
		MinBinaryBytes:   cfg.Inspect.MinBinaryBytes,
		MaxBinaryBytes:   cfg.Inspect.MaxBinaryBytes,
		BlockedMagicHex:  append([]string(nil), cfg.Inspect.BlockedMagicHex...),
		MaxArchiveDepth:  firstNonZeroInt(cfg.Inspect.MaxArchiveDepth, 3),
		ToolTimeoutSecs:  firstNonZeroInt(cfg.Inspect.ToolTimeoutSecs, 30),
		FFProbePath:      firstNonEmpty(cfg.Inspect.FFProbePath, "ffprobe"),
		SevenZipPath:     firstNonEmpty(cfg.Inspect.SevenZipPath, "7z"),
		UnrarPath:        firstNonEmpty(cfg.Inspect.UnrarPath, "unrar"),
		PAR2Path:         firstNonEmpty(cfg.Inspect.PAR2Path, "par2"),
	}
	out.InspectDiscovery = indexStageRuntimeFromConfig(cfg.InspectDiscovery, true, 10, 100)
	out.InspectPAR2 = indexStageRuntimeFromConfigWithConcurrency(cfg.InspectPAR2, true, 10, 100)
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

func aggregatorRuntimeFromConfig(cfg config.AggregatorConfig) *AggregatorRuntimeSettings {
	return &AggregatorRuntimeSettings{
		Sources: AggregatorSourcesRuntimeSettings{
			LocalBlob:     RuntimeToggle{Enabled: cfg.Sources.LocalBlob.Enabled},
			UsenetIndexer: RuntimeToggle{Enabled: cfg.Sources.UsenetIndexer.Enabled},
		},
	}
}

// ApplyToConfig applies runtime-editable settings on top of bootstrap config.
func ApplyToConfig(base *config.Config, runtime *RuntimeSettings) *config.Config {
	if base == nil {
		return nil
	}

	effective := *base

	effective.Servers = append([]config.ServerConfig(nil), base.Servers...)
	effective.Indexers = append([]config.IndexerConfig(nil), base.Indexers...)
	effective.Aggregator = base.Aggregator
	effective.Download.CleanupExtensions = append([]string(nil), base.Download.CleanupExtensions...)
	effective.Indexing.Newsgroups = append([]string(nil), base.Indexing.Newsgroups...)

	if runtime == nil {
		return &effective
	}

	effective.Servers = toConfigServers(RuntimeServersForCompatibility(runtime))

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

	if runtime.Aggregator != nil {
		effective.Aggregator.Sources.LocalBlob.Enabled = runtime.Aggregator.Sources.LocalBlob.Enabled
		effective.Aggregator.Sources.UsenetIndexer.Enabled = runtime.Aggregator.Sources.UsenetIndexer.Enabled
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
		effective.Indexing.BackfillUntilDateByGroup = cloneStringMap(indexing.BackfillUntilDateByGroup)

		effective.Indexing.ScrapeLatest = toStageConfigNoConcurrency(indexing.ScrapeLatest)
		effective.Indexing.ScrapeBackfill = toStageConfigNoConcurrency(indexing.ScrapeBackfill)
		effective.Indexing.Assemble = toStageConfig(indexing.Assemble)
		effective.Indexing.AssembleLaneA = toStageConfig(indexing.AssembleLaneA)
		effective.Indexing.AssembleLaneB = toStageConfig(indexing.AssembleLaneB)
		effective.Indexing.RecoverYEnc = toStageConfig(indexing.RecoverYEnc)
		effective.Indexing.Release = config.IndexingReleaseConfig{
			Enabled:                    boolPtr(indexing.Release.Enabled),
			IntervalMinutes:            float64Ptr(indexing.Release.IntervalMinutes),
			BatchSize:                  intPtr(indexing.Release.BatchSize),
			BackoffSeconds:             intPtr(indexing.Release.BackoffSeconds),
			MinConfidence:              float64Ptr(indexing.Release.MinConfidence),
			MinCompletionPct:           float64Ptr(indexing.Release.MinCompletionPct),
			MinExpectedFileCoveragePct: float64Ptr(indexing.Release.MinExpectedFileCoveragePct),
			RequireExpectedFileCountForContextualObfuscated: boolPtr(indexing.Release.RequireExpectedFileCountForContextualObfuscated),
		}
		effective.Indexing.Match = config.IndexingMatchConfig{
			HighConfidenceThreshold:     float64Ptr(indexing.Match.HighConfidenceThreshold),
			ProbableConfidenceThreshold: float64Ptr(indexing.Match.ProbableConfidenceThreshold),
			ArticleBucketSize:           int64Ptr(indexing.Match.ArticleBucketSize),
		}
		effective.Indexing.Inspect = config.IndexingInspectConfig{
			WorkDir:          indexing.Inspect.WorkDir,
			WorkspaceBackend: indexing.Inspect.WorkspaceBackend,
			MemoryWorkDir:    indexing.Inspect.MemoryWorkDir,
			MaxBytes:         indexing.Inspect.MaxBytes,
			MinBinaryBytes:   indexing.Inspect.MinBinaryBytes,
			MaxBinaryBytes:   indexing.Inspect.MaxBinaryBytes,
			BlockedMagicHex:  append([]string(nil), indexing.Inspect.BlockedMagicHex...),
			MaxArchiveDepth:  indexing.Inspect.MaxArchiveDepth,
			ToolTimeoutSecs:  indexing.Inspect.ToolTimeoutSecs,
			FFProbePath:      indexing.Inspect.FFProbePath,
			SevenZipPath:     indexing.Inspect.SevenZipPath,
			UnrarPath:        indexing.Inspect.UnrarPath,
			PAR2Path:         indexing.Inspect.PAR2Path,
		}
		effective.Indexing.InspectDiscovery = toStageConfigNoConcurrency(indexing.InspectDiscovery)
		effective.Indexing.InspectPAR2 = toStageConfig(indexing.InspectPAR2)
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
		Servers:           append([]ServerRuntimeSettings(nil), current.Servers...),
		DownloaderServers: append([]ServerRuntimeSettings(nil), current.DownloaderServers...),
		IndexerServers:    append([]ServerRuntimeSettings(nil), current.IndexerServers...),
		Indexers:          append([]IndexerRuntimeSettings(nil), current.Indexers...),
		ArrIntegrations:   append([]ArrIntegrationRuntimeSettings(nil), current.ArrIntegrations...),
		Aggregator:        cloneAggregator(current.Aggregator),
		Download:          cloneDownload(current.Download),
		NNTPPool:          cloneNNTPPool(current.NNTPPool),
		Indexing:          cloneIndexing(current.Indexing),
		Revision:          current.Revision,
	}

	if patch.Servers != nil {
		next.Servers = append([]ServerRuntimeSettings(nil), (*patch.Servers)...)
	}
	if patch.DownloaderServers != nil {
		next.DownloaderServers = append([]ServerRuntimeSettings(nil), (*patch.DownloaderServers)...)
	}
	if patch.IndexerServers != nil {
		next.IndexerServers = append([]ServerRuntimeSettings(nil), (*patch.IndexerServers)...)
	}
	if patch.Indexers != nil {
		next.Indexers = append([]IndexerRuntimeSettings(nil), (*patch.Indexers)...)
	}
	if patch.Aggregator != nil {
		next.Aggregator = cloneAggregator(patch.Aggregator)
	}
	if patch.Download != nil {
		next.Download = cloneDownload(patch.Download)
	}
	if patch.NNTPPool != nil {
		next.NNTPPool = mergeNNTPPoolRuntimeSettings(DefaultNNTPPoolRuntimeSettings(), patch.NNTPPool)
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
		Servers:           append([]ServerRuntimeSettings(nil), in.Servers...),
		DownloaderServers: append([]ServerRuntimeSettings(nil), in.DownloaderServers...),
		IndexerServers:    append([]ServerRuntimeSettings(nil), in.IndexerServers...),
		Indexers:          append([]IndexerRuntimeSettings(nil), in.Indexers...),
		ArrIntegrations:   append([]ArrIntegrationRuntimeSettings(nil), in.ArrIntegrations...),
		Aggregator:        cloneAggregator(in.Aggregator),
		Download:          cloneDownload(in.Download),
		NNTPPool:          cloneNNTPPool(in.NNTPPool),
		Indexing:          cloneIndexing(in.Indexing),
		Revision:          in.Revision,
	}
	out.NNTPPool = mergeNNTPPoolRuntimeSettings(DefaultNNTPPoolRuntimeSettings(), out.NNTPPool)
	dropUnsupportedIndexingConcurrency(out)
	return out
}

// RedactedCopy removes secrets before returning settings externally.
func RedactedCopy(in *RuntimeSettings) *RuntimeSettings {
	out := CloneRuntimeSettings(in)
	for i := range out.Servers {
		out.Servers[i].Password = ""
	}
	for i := range out.DownloaderServers {
		out.DownloaderServers[i].Password = ""
	}
	for i := range out.IndexerServers {
		out.IndexerServers[i].Password = ""
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

func RuntimeConfigured(in *RuntimeSettings) bool {
	if in == nil {
		return false
	}
	return len(in.Servers) > 0 ||
		len(in.DownloaderServers) > 0 ||
		len(in.IndexerServers) > 0 ||
		len(in.Indexers) > 0 ||
		len(in.ArrIntegrations) > 0 ||
		in.Aggregator != nil && (in.Aggregator.Sources.LocalBlob.Enabled || in.Aggregator.Sources.UsenetIndexer.Enabled) ||
		downloadConfigured(in.Download) ||
		indexingConfigured(in.Indexing)
}

func DefaultNNTPPoolRuntimeSettings() *NNTPPoolRuntimeSettings {
	return &NNTPPoolRuntimeSettings{
		IdleBorrowEnabled:        true,
		IndexerMaxPercent:        80,
		DownloaderReservePercent: 20,
		DemandWindowSeconds:      30,
	}
}

func DownloaderNNTPServers(in *RuntimeSettings) []ServerRuntimeSettings {
	if in == nil {
		return nil
	}
	if len(in.DownloaderServers) > 0 {
		return in.DownloaderServers
	}
	return in.Servers
}

func IndexerNNTPServers(in *RuntimeSettings) []ServerRuntimeSettings {
	if in == nil {
		return nil
	}
	if len(in.IndexerServers) > 0 {
		return in.IndexerServers
	}
	return in.Servers
}

func RuntimeServersForCompatibility(in *RuntimeSettings) []ServerRuntimeSettings {
	if in == nil {
		return nil
	}
	if len(in.Servers) > 0 {
		return in.Servers
	}
	if len(in.DownloaderServers) > 0 {
		return in.DownloaderServers
	}
	return in.IndexerServers
}

func ToConfigServers(servers []ServerRuntimeSettings) []config.ServerConfig {
	return toConfigServers(servers)
}

func toConfigServers(servers []ServerRuntimeSettings) []config.ServerConfig {
	out := make([]config.ServerConfig, 0, len(servers))
	for _, s := range servers {
		out = append(out, config.ServerConfig{
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
	return out
}

func downloadConfigured(in *DownloadRuntimeSettings) bool {
	if in == nil {
		return false
	}
	return strings.TrimSpace(in.OutDir) != "" && strings.TrimSpace(in.OutDir) != "./downloads" ||
		strings.TrimSpace(in.CompletedDir) != "" && strings.TrimSpace(in.CompletedDir) != "./downloads/completed"
}

func indexingConfigured(in *IndexingRuntimeSettings) bool {
	if in == nil {
		return false
	}
	return len(in.Newsgroups) > 0 ||
		len(in.BackfillUntilDateByGroup) > 0 ||
		in.ScrapeLatest.Enabled ||
		in.ScrapeBackfill.Enabled ||
		in.Assemble.Enabled ||
		in.AssembleLaneA.Enabled ||
		in.AssembleLaneB.Enabled ||
		in.RecoverYEnc.Enabled ||
		in.Release.Enabled ||
		in.InspectDiscovery.Enabled ||
		in.InspectPAR2.Enabled ||
		in.InspectNFO.Enabled ||
		in.InspectArchive.Enabled ||
		in.InspectPassword.Enabled ||
		in.InspectMedia.Enabled ||
		in.EnrichPreDB.Enabled ||
		in.EnrichTMDB.Enabled
}

func dropUnsupportedIndexingConcurrency(in *RuntimeSettings) {
	if in == nil || in.Indexing == nil {
		return
	}
	in.Indexing.ScrapeLatest.Concurrency = 0
	in.Indexing.ScrapeBackfill.Concurrency = 0
	in.Indexing.InspectDiscovery.Concurrency = 0
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

func cloneNNTPPool(in *NNTPPoolRuntimeSettings) *NNTPPoolRuntimeSettings {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func mergeNNTPPoolRuntimeSettings(base, override *NNTPPoolRuntimeSettings) *NNTPPoolRuntimeSettings {
	if base == nil {
		base = DefaultNNTPPoolRuntimeSettings()
	}
	out := *base
	if override == nil {
		return &out
	}
	out.IdleBorrowEnabled = override.IdleBorrowEnabled
	if override.IndexerMaxPercent > 0 {
		out.IndexerMaxPercent = clampPercent(override.IndexerMaxPercent)
	}
	if override.DownloaderReservePercent > 0 {
		out.DownloaderReservePercent = clampPercent(override.DownloaderReservePercent)
	}
	if override.DemandWindowSeconds > 0 {
		out.DemandWindowSeconds = override.DemandWindowSeconds
	}
	return &out
}

func clampPercent(v int) int {
	if v < 1 {
		return 1
	}
	if v > 100 {
		return 100
	}
	return v
}

func cloneAggregator(in *AggregatorRuntimeSettings) *AggregatorRuntimeSettings {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func cloneIndexing(in *IndexingRuntimeSettings) *IndexingRuntimeSettings {
	if in == nil {
		return nil
	}
	out := &IndexingRuntimeSettings{
		Newsgroups:               append([]string(nil), in.Newsgroups...),
		BackfillUntilDateByGroup: cloneStringMap(in.BackfillUntilDateByGroup),
		ScrapeLatest:             in.ScrapeLatest,
		ScrapeBackfill:           in.ScrapeBackfill,
		Assemble:                 mergeStageRuntimeSettings(defaultAssembleStage(false, 10, 5000, 1), in.Assemble),
		AssembleLaneA:            mergeStageRuntimeSettings(defaultAssembleStage(false, 2, 5000, 1), in.AssembleLaneA),
		AssembleLaneB:            mergeStageRuntimeSettings(defaultAssembleStage(false, 10, 2500, 1), in.AssembleLaneB),
		RecoverYEnc:              mergeStageRuntimeSettings(defaultStage(false, 10, 25, 1), in.RecoverYEnc),
		Release:                  in.Release,
		Match:                    in.Match,
		Inspect:                  cloneInspectRuntimeSettings(in.Inspect),
		InspectDiscovery:         in.InspectDiscovery,
		InspectPAR2:              in.InspectPAR2,
		InspectNFO:               in.InspectNFO,
		InspectArchive:           in.InspectArchive,
		InspectPassword:          in.InspectPassword,
		InspectMedia:             in.InspectMedia,
		EnrichPreDB:              in.EnrichPreDB,
		EnrichTMDB:               in.EnrichTMDB,
	}
	return out
}

func cloneInspectRuntimeSettings(in IndexingInspectRuntimeSettings) IndexingInspectRuntimeSettings {
	out := in
	out.BlockedMagicHex = append([]string(nil), in.BlockedMagicHex...)
	return out
}

func mergeStageRuntimeSettings(base, override IndexingStageRuntimeSettings) IndexingStageRuntimeSettings {
	if override.Enabled {
		base.Enabled = true
	}
	if override.IntervalMinutes > 0 {
		base.IntervalMinutes = override.IntervalMinutes
	}
	if override.BatchSize > 0 {
		base.BatchSize = override.BatchSize
	}
	if override.Concurrency > 0 {
		base.Concurrency = override.Concurrency
	}
	if override.BackoffSeconds > 0 {
		base.BackoffSeconds = override.BackoffSeconds
	}
	if override.BinaryUpsertDBChunkSize > 0 {
		base.BinaryUpsertDBChunkSize = override.BinaryUpsertDBChunkSize
	}
	return base
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func indexStageRuntimeFromConfig(cfg config.IndexingStageConfig, defaultEnabled bool, defaultInterval float64, defaultBatch int) IndexingStageRuntimeSettings {
	return IndexingStageRuntimeSettings{
		Enabled:                 boolValue(cfg.Enabled, defaultEnabled),
		IntervalMinutes:         float64Value(cfg.IntervalMinutes, defaultInterval),
		BatchSize:               intValue(cfg.BatchSize, defaultBatch),
		BackoffSeconds:          intValue(cfg.BackoffSeconds, 0),
		BinaryUpsertDBChunkSize: intValue(cfg.BinaryUpsertDBChunkSize, 0),
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
	if in.BinaryUpsertDBChunkSize > 0 {
		out.BinaryUpsertDBChunkSize = intPtr(in.BinaryUpsertDBChunkSize)
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
