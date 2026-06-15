package app

import (
	"fmt"
	"slices"
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
			Newsgroups:                  []string{},
			BackfillUntilDateByGroup:    map[string]string{},
			ExplicitGroups:              []IndexingScrapeGroupRuntimeSettings{},
			WildcardRules:               []IndexingWildcardRuleRuntimeSettings{},
			ProviderGroupInventory:      []IndexingProviderGroupInventoryRuntimeSettings{},
			MaterializedGroups:          []IndexingMaterializedGroupRuntimeSettings{},
			ScrapeLatest:                defaultStage(false, 10, 5000, 0),
			ScrapeBackfill:              defaultStage(false, 10, 5000, 0),
			AssembleLaneA:               defaultAssembleStage(false, 2, 5000, 1),
			AssembleLaneB:               defaultAssembleStage(false, 10, 2500, 1),
			RecoverYEnc:                 defaultRecoverYEncStage(false),
			ReleaseSummaryRefresh:       defaultReleaseSummaryRefreshStage(false),
			Release:                     defaultReleaseStage(false),
			ReleaseGenerateNZB:          defaultStage(false, 10, 100, 0),
			ReleaseArchiveNZB:           defaultStage(false, 10, 100, 0),
			ReleasePurgeArchivedSources: defaultStage(false, 10, 50, 0),
			Match:                       IndexingMatchRuntimeSettings{HighConfidenceThreshold: 0.85, ProbableConfidenceThreshold: 0.55, ArticleBucketSize: 5000},
			Inspect:                     IndexingInspectRuntimeSettings{WorkDir: "/store/indexer/inspect", WorkspaceBackend: "auto", MemoryWorkDir: "/dev/shm/gonzb-inspect", MaxBytes: 2 * 1024 * 1024 * 1024, MinBinaryBytes: 0, MaxBinaryBytes: 0, RequireExpectedFileCount: false, BlockedMagicHex: []string{"52434C4F4E45"}, MaxArchiveDepth: 3, ToolTimeoutSecs: 30, FFmpegPath: "ffmpeg", FFProbePath: "ffprobe", SevenZipPath: "7z", UnrarPath: "unrar", PAR2Path: "par2"},
			StorageGuard:                IndexingStorageGuardRuntimeSettings{Enabled: true, MinFreeBytes: 8 * 1024 * 1024 * 1024, MinFreePercent: 5},
			MemoryGuard:                 IndexingMemoryGuardRuntimeSettings{Enabled: true, MinAvailableBytes: 2 * 1024 * 1024 * 1024, MinAvailablePercent: 10, MinSwapFreeBytes: 512 * 1024 * 1024},
			InspectDiscovery:            defaultStage(false, 10, 100, 0),
			InspectPAR2:                 defaultStage(false, 10, 100, 4),
			InspectNFO:                  defaultStage(false, 10, 100, 0),
			InspectArchive:              defaultStage(false, 10, 100, 1),
			InspectPassword:             defaultStage(false, 10, 100, 0),
			InspectMedia:                defaultStage(false, 10, 100, 1),
			EnrichPreDB:                 defaultPreDBStage(false),
			EnrichTMDB:                  defaultTMDBStage(false),
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
	} else {
		out.Indexing = cloneIndexing(out.Indexing)
	}
	return out
}

func defaultStage(enabled bool, interval float64, batch, concurrency int) IndexingStageRuntimeSettings {
	return IndexingStageRuntimeSettings{Enabled: enabled, IntervalMinutes: interval, BatchSize: batch, Concurrency: concurrency}
}

func defaultReleaseSummaryRefreshStage(enabled bool) IndexingStageRuntimeSettings {
	stage := defaultStage(enabled, 2, 10000, 0)
	stage.MaxBatches = 10
	return stage
}

func defaultAssembleStage(enabled bool, interval float64, batch, concurrency int) IndexingStageRuntimeSettings {
	stage := defaultStage(enabled, interval, batch, concurrency)
	stage.BinaryUpsertDBChunkSize = 250
	return stage
}

func defaultRecoverYEncStage(enabled bool) IndexingStageRuntimeSettings {
	stage := defaultStage(enabled, 10, 25, 1)
	stage.MaxEffectiveConcurrency = 4
	return stage
}

func defaultReleaseStage(enabled bool) IndexingReleaseRuntimeSettings {
	return IndexingReleaseRuntimeSettings{
		Enabled: enabled, IntervalMinutes: 10, BatchSize: 1000, AutoReformBatchSize: 25, MinConfidence: 0.55,
		MinCompletionPct: 0, MinExpectedFileCoveragePct: 90, RequireExpectedFileCountForContextualObfuscated: true,
		PublicMinMatchConfidence: 0.55, PublicMinCompletionPct: 100, PublicMinIdentityStatus: "probable",
		PublicRequireInspection: false, PublicRequireEnrichment: false,
		PublicRequirePayloadComplete: true, PublicRequireExpectedFileCountComplete: false,
		PublicRequirePAR2: false, PublicRequireNFO: false, PublicRequireSFV: false,
		RetainUntilExpectedFileCountComplete: false, RetainRequirePAR2: false, RetainRequireNFO: false, RetainRequireSFV: false,
		ReopenArchivedNZBOnReleaseChange: false,
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
		ExplicitGroups:           legacyExplicitGroupsFromConfig(cfg.Newsgroups, cfg.BackfillUntilDateByGroup),
		WildcardRules:            []IndexingWildcardRuleRuntimeSettings{},
		ProviderGroupInventory:   []IndexingProviderGroupInventoryRuntimeSettings{},
		MaterializedGroups:       []IndexingMaterializedGroupRuntimeSettings{},
	}

	out.ScrapeLatest = indexStageRuntimeFromConfigWithConcurrency(cfg.ScrapeLatest, true, 10, 5000)
	out.ScrapeBackfill = indexStageRuntimeFromConfigWithConcurrency(cfg.ScrapeBackfill, true, 10, 5000)
	out.AssembleLaneA = indexStageRuntimeFromConfigWithConcurrency(cfg.AssembleLaneA, false, 2, 5000)
	out.AssembleLaneB = indexStageRuntimeFromConfigWithConcurrency(cfg.AssembleLaneB, false, 10, 2500)
	out.RecoverYEnc = indexStageRuntimeFromConfigWithConcurrency(cfg.RecoverYEnc, false, 10, 25)
	out.ReleaseSummaryRefresh = mergeStageRuntimeSettings(
		defaultReleaseSummaryRefreshStage(boolValue(cfg.Release.Enabled, true)),
		indexStageRuntimeFromConfig(cfg.ReleaseSummaryRefresh, boolValue(cfg.Release.Enabled, true), 2, 10000),
	)
	out.Release = IndexingReleaseRuntimeSettings{
		Enabled:                    boolValue(cfg.Release.Enabled, true),
		IntervalMinutes:            float64Value(cfg.Release.IntervalMinutes, 10),
		BatchSize:                  intValue(cfg.Release.BatchSize, 1000),
		AutoReformBatchSize:        intValue(cfg.Release.AutoReformBatchSize, 25),
		BackoffSeconds:             intValue(cfg.Release.BackoffSeconds, 0),
		MinConfidence:              float64Value(cfg.Release.MinConfidence, 0.55),
		MinCompletionPct:           float64Value(cfg.Release.MinCompletionPct, 0),
		MinExpectedFileCoveragePct: float64Value(cfg.Release.MinExpectedFileCoveragePct, 90),
		RequireExpectedFileCountForContextualObfuscated: boolValue(cfg.Release.RequireExpectedFileCountForContextualObfuscated, true),
		PublicMinMatchConfidence:                        float64Value(cfg.Release.PublicMinMatchConfidence, 0.55),
		PublicMinCompletionPct:                          float64Value(cfg.Release.PublicMinCompletionPct, 100),
		PublicMinIdentityStatus:                         firstNonEmpty(cfg.Release.PublicMinIdentityStatus, "probable"),
		PublicRequireInspection:                         boolValue(cfg.Release.PublicRequireInspection, false),
		PublicRequireEnrichment:                         boolValue(cfg.Release.PublicRequireEnrichment, false),
		PublicRequirePayloadComplete:                    boolValue(cfg.Release.PublicRequirePayloadComplete, true),
		PublicRequireExpectedFileCountComplete:          boolValue(cfg.Release.PublicRequireExpectedFileCountComplete, false),
		PublicRequirePAR2:                               boolValue(cfg.Release.PublicRequirePAR2, false),
		PublicRequireNFO:                                boolValue(cfg.Release.PublicRequireNFO, false),
		PublicRequireSFV:                                boolValue(cfg.Release.PublicRequireSFV, false),
		RetainUntilExpectedFileCountComplete:            boolValue(cfg.Release.RetainUntilExpectedFileCountComplete, false),
		RetainRequirePAR2:                               boolValue(cfg.Release.RetainRequirePAR2, false),
		RetainRequireNFO:                                boolValue(cfg.Release.RetainRequireNFO, false),
		RetainRequireSFV:                                boolValue(cfg.Release.RetainRequireSFV, false),
		ReopenArchivedNZBOnReleaseChange:                boolValue(cfg.Release.ReopenArchivedNZBOnReleaseChange, false),
	}
	out.ReleaseGenerateNZB = indexStageRuntimeFromConfig(cfg.ReleaseGenerateNZB, false, 10, 100)
	out.ReleaseArchiveNZB = indexStageRuntimeFromConfig(cfg.ReleaseArchiveNZB, false, 10, 100)
	out.ReleasePurgeArchivedSources = indexStageRuntimeFromConfig(cfg.ReleasePurgeArchivedSources, false, 10, 50)
	out.Match = IndexingMatchRuntimeSettings{
		HighConfidenceThreshold:     float64Value(cfg.Match.HighConfidenceThreshold, 0.85),
		ProbableConfidenceThreshold: float64Value(cfg.Match.ProbableConfidenceThreshold, 0.55),
		ArticleBucketSize:           int64Value(cfg.Match.ArticleBucketSize, 5000),
	}
	out.Inspect = IndexingInspectRuntimeSettings{
		WorkDir:                  firstNonEmpty(cfg.Inspect.WorkDir, "/store/indexer/inspect"),
		WorkspaceBackend:         firstNonEmpty(cfg.Inspect.WorkspaceBackend, "auto"),
		MemoryWorkDir:            firstNonEmpty(cfg.Inspect.MemoryWorkDir, "/dev/shm/gonzb-inspect"),
		MaxBytes:                 firstNonZeroInt64(cfg.Inspect.MaxBytes, 2*1024*1024*1024),
		MinBinaryBytes:           cfg.Inspect.MinBinaryBytes,
		MaxBinaryBytes:           cfg.Inspect.MaxBinaryBytes,
		RequireExpectedFileCount: cfg.Inspect.RequireExpectedFileCount,
		BlockedMagicHex:          append([]string(nil), cfg.Inspect.BlockedMagicHex...),
		MaxArchiveDepth:          firstNonZeroInt(cfg.Inspect.MaxArchiveDepth, 3),
		ToolTimeoutSecs:          firstNonZeroInt(cfg.Inspect.ToolTimeoutSecs, 30),
		FFmpegPath:               firstNonEmpty(cfg.Inspect.FFmpegPath, "ffmpeg"),
		FFProbePath:              firstNonEmpty(cfg.Inspect.FFProbePath, "ffprobe"),
		SevenZipPath:             firstNonEmpty(cfg.Inspect.SevenZipPath, "7z"),
		UnrarPath:                firstNonEmpty(cfg.Inspect.UnrarPath, "unrar"),
		PAR2Path:                 firstNonEmpty(cfg.Inspect.PAR2Path, "par2"),
	}
	out.StorageGuard = IndexingStorageGuardRuntimeSettings{
		Enabled:        boolValue(cfg.StorageGuard.Enabled, true),
		MinFreeBytes:   int64Value(cfg.StorageGuard.MinFreeBytes, 8*1024*1024*1024),
		MinFreePercent: float64Value(cfg.StorageGuard.MinFreePercent, 5),
	}
	out.MemoryGuard = IndexingMemoryGuardRuntimeSettings{
		Enabled:             boolValue(cfg.MemoryGuard.Enabled, true),
		MinAvailableBytes:   int64Value(cfg.MemoryGuard.MinAvailableBytes, 2*1024*1024*1024),
		MinAvailablePercent: float64Value(cfg.MemoryGuard.MinAvailablePercent, 10),
		MinSwapFreeBytes:    int64Value(cfg.MemoryGuard.MinSwapFreeBytes, 512*1024*1024),
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

	normalizeIndexingScrapeConfig(&out)
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
		effective.Indexing.Newsgroups = EffectiveNewsgroupNames(indexing)
		effective.Indexing.BackfillUntilDateByGroup = EffectiveBackfillUntilDateByGroup(indexing)

		effective.Indexing.ScrapeLatest = toStageConfig(indexing.ScrapeLatest)
		effective.Indexing.ScrapeBackfill = toStageConfig(indexing.ScrapeBackfill)
		effective.Indexing.AssembleLaneA = toStageConfig(indexing.AssembleLaneA)
		effective.Indexing.AssembleLaneB = toStageConfig(indexing.AssembleLaneB)
		effective.Indexing.RecoverYEnc = toStageConfig(indexing.RecoverYEnc)
		effective.Indexing.ReleaseSummaryRefresh = toStageConfigNoConcurrency(indexing.ReleaseSummaryRefresh)
		effective.Indexing.Release = config.IndexingReleaseConfig{
			Enabled:                    boolPtr(indexing.Release.Enabled),
			IntervalMinutes:            float64Ptr(indexing.Release.IntervalMinutes),
			BatchSize:                  intPtr(indexing.Release.BatchSize),
			AutoReformBatchSize:        intPtr(indexing.Release.AutoReformBatchSize),
			BackoffSeconds:             intPtr(indexing.Release.BackoffSeconds),
			MinConfidence:              float64Ptr(indexing.Release.MinConfidence),
			MinCompletionPct:           float64Ptr(indexing.Release.MinCompletionPct),
			MinExpectedFileCoveragePct: float64Ptr(indexing.Release.MinExpectedFileCoveragePct),
			RequireExpectedFileCountForContextualObfuscated: boolPtr(indexing.Release.RequireExpectedFileCountForContextualObfuscated),
			PublicMinMatchConfidence:                        float64Ptr(indexing.Release.PublicMinMatchConfidence),
			PublicMinCompletionPct:                          float64Ptr(indexing.Release.PublicMinCompletionPct),
			PublicMinIdentityStatus:                         indexing.Release.PublicMinIdentityStatus,
			PublicRequireInspection:                         boolPtr(indexing.Release.PublicRequireInspection),
			PublicRequireEnrichment:                         boolPtr(indexing.Release.PublicRequireEnrichment),
			PublicRequirePayloadComplete:                    boolPtr(indexing.Release.PublicRequirePayloadComplete),
			PublicRequireExpectedFileCountComplete:          boolPtr(indexing.Release.PublicRequireExpectedFileCountComplete),
			PublicRequirePAR2:                               boolPtr(indexing.Release.PublicRequirePAR2),
			PublicRequireNFO:                                boolPtr(indexing.Release.PublicRequireNFO),
			PublicRequireSFV:                                boolPtr(indexing.Release.PublicRequireSFV),
			RetainUntilExpectedFileCountComplete:            boolPtr(indexing.Release.RetainUntilExpectedFileCountComplete),
			RetainRequirePAR2:                               boolPtr(indexing.Release.RetainRequirePAR2),
			RetainRequireNFO:                                boolPtr(indexing.Release.RetainRequireNFO),
			RetainRequireSFV:                                boolPtr(indexing.Release.RetainRequireSFV),
			ReopenArchivedNZBOnReleaseChange:                boolPtr(indexing.Release.ReopenArchivedNZBOnReleaseChange),
		}
		effective.Indexing.ReleaseGenerateNZB = toStageConfigNoConcurrency(indexing.ReleaseGenerateNZB)
		effective.Indexing.ReleaseArchiveNZB = toStageConfigNoConcurrency(indexing.ReleaseArchiveNZB)
		effective.Indexing.ReleasePurgeArchivedSources = toStageConfigNoConcurrency(indexing.ReleasePurgeArchivedSources)
		effective.Indexing.Match = config.IndexingMatchConfig{
			HighConfidenceThreshold:     float64Ptr(indexing.Match.HighConfidenceThreshold),
			ProbableConfidenceThreshold: float64Ptr(indexing.Match.ProbableConfidenceThreshold),
			ArticleBucketSize:           int64Ptr(indexing.Match.ArticleBucketSize),
		}
		effective.Indexing.Inspect = config.IndexingInspectConfig{
			WorkDir:                  indexing.Inspect.WorkDir,
			WorkspaceBackend:         indexing.Inspect.WorkspaceBackend,
			MemoryWorkDir:            indexing.Inspect.MemoryWorkDir,
			MaxBytes:                 indexing.Inspect.MaxBytes,
			MinBinaryBytes:           indexing.Inspect.MinBinaryBytes,
			MaxBinaryBytes:           indexing.Inspect.MaxBinaryBytes,
			RequireExpectedFileCount: indexing.Inspect.RequireExpectedFileCount,
			BlockedMagicHex:          append([]string(nil), indexing.Inspect.BlockedMagicHex...),
			MaxArchiveDepth:          indexing.Inspect.MaxArchiveDepth,
			ToolTimeoutSecs:          indexing.Inspect.ToolTimeoutSecs,
			FFmpegPath:               indexing.Inspect.FFmpegPath,
			FFProbePath:              indexing.Inspect.FFProbePath,
			SevenZipPath:             indexing.Inspect.SevenZipPath,
			UnrarPath:                indexing.Inspect.UnrarPath,
			PAR2Path:                 indexing.Inspect.PAR2Path,
		}
		effective.Indexing.StorageGuard = config.IndexingStorageGuardConfig{
			Enabled:        boolPtr(indexing.StorageGuard.Enabled),
			MinFreeBytes:   int64Ptr(indexing.StorageGuard.MinFreeBytes),
			MinFreePercent: float64Ptr(indexing.StorageGuard.MinFreePercent),
		}
		effective.Indexing.MemoryGuard = config.IndexingMemoryGuardConfig{
			Enabled:             boolPtr(indexing.MemoryGuard.Enabled),
			MinAvailableBytes:   int64Ptr(indexing.MemoryGuard.MinAvailableBytes),
			MinAvailablePercent: float64Ptr(indexing.MemoryGuard.MinAvailablePercent),
			MinSwapFreeBytes:    int64Ptr(indexing.MemoryGuard.MinSwapFreeBytes),
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
		IdleBorrowEnabled:         true,
		IndexerMaxPercent:         80,
		IndexerStageTargetPercent: 90,
		DownloaderReservePercent:  20,
		DemandWindowSeconds:       30,
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
		in.AssembleLaneA.Enabled ||
		in.AssembleLaneB.Enabled ||
		in.RecoverYEnc.Enabled ||
		in.ReleaseSummaryRefresh.Enabled ||
		in.Release.Enabled ||
		in.ReleaseGenerateNZB.Enabled ||
		in.ReleaseArchiveNZB.Enabled ||
		in.ReleasePurgeArchivedSources.Enabled ||
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
	if override.IndexerStageTargetPercent > 0 {
		out.IndexerStageTargetPercent = clampPercent(override.IndexerStageTargetPercent)
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
		Newsgroups:                  append([]string(nil), in.Newsgroups...),
		BackfillUntilDateByGroup:    cloneStringMap(in.BackfillUntilDateByGroup),
		ExplicitGroups:              cloneExplicitGroups(in.ExplicitGroups),
		WildcardRules:               cloneWildcardRules(in.WildcardRules),
		ProviderGroupInventory:      cloneProviderGroupInventory(in.ProviderGroupInventory),
		MaterializedGroups:          cloneMaterializedGroups(in.MaterializedGroups),
		ScrapeLatest:                in.ScrapeLatest,
		ScrapeBackfill:              in.ScrapeBackfill,
		AssembleLaneA:               mergeStageRuntimeSettings(defaultAssembleStage(false, 2, 5000, 1), in.AssembleLaneA),
		AssembleLaneB:               mergeStageRuntimeSettings(defaultAssembleStage(false, 10, 2500, 1), in.AssembleLaneB),
		RecoverYEnc:                 mergeStageRuntimeSettings(defaultRecoverYEncStage(false), in.RecoverYEnc),
		ReleaseSummaryRefresh:       mergeStageRuntimeSettings(defaultReleaseSummaryRefreshStage(false), in.ReleaseSummaryRefresh),
		Release:                     in.Release,
		ReleaseGenerateNZB:          mergeStageRuntimeSettings(defaultStage(false, 10, 100, 0), in.ReleaseGenerateNZB),
		ReleaseArchiveNZB:           mergeStageRuntimeSettings(defaultStage(false, 10, 100, 0), in.ReleaseArchiveNZB),
		ReleasePurgeArchivedSources: mergeStageRuntimeSettings(defaultStage(false, 10, 50, 0), in.ReleasePurgeArchivedSources),
		Match:                       in.Match,
		Inspect:                     cloneInspectRuntimeSettings(in.Inspect),
		StorageGuard:                normalizeStorageGuardRuntimeSettings(in.StorageGuard),
		MemoryGuard:                 normalizeMemoryGuardRuntimeSettings(in.MemoryGuard),
		InspectDiscovery:            in.InspectDiscovery,
		InspectPAR2:                 in.InspectPAR2,
		InspectNFO:                  in.InspectNFO,
		InspectArchive:              in.InspectArchive,
		InspectPassword:             in.InspectPassword,
		InspectMedia:                in.InspectMedia,
		EnrichPreDB:                 in.EnrichPreDB,
		EnrichTMDB:                  in.EnrichTMDB,
	}
	normalizeIndexingScrapeConfig(out)
	return out
}

func legacyExplicitGroupsFromConfig(newsgroups []string, cutoffs map[string]string) []IndexingScrapeGroupRuntimeSettings {
	seen := map[string]struct{}{}
	out := make([]IndexingScrapeGroupRuntimeSettings, 0, len(newsgroups)+len(cutoffs))
	for _, raw := range newsgroups {
		group := strings.TrimSpace(raw)
		if group == "" {
			continue
		}
		key := strings.ToLower(group)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, IndexingScrapeGroupRuntimeSettings{
			GroupName:         group,
			Enabled:           true,
			BackfillUntilDate: strings.TrimSpace(cutoffs[group]),
			Source:            "explicit",
		})
	}
	for rawGroup, rawDate := range cutoffs {
		group := strings.TrimSpace(rawGroup)
		if group == "" {
			continue
		}
		key := strings.ToLower(group)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, IndexingScrapeGroupRuntimeSettings{
			GroupName:         group,
			Enabled:           true,
			BackfillUntilDate: strings.TrimSpace(rawDate),
			Source:            "explicit",
		})
	}
	return out
}

func EffectiveScrapeGroups(indexing *IndexingRuntimeSettings) []IndexingScrapeGroupRuntimeSettings {
	if indexing == nil {
		return nil
	}
	out := make([]IndexingScrapeGroupRuntimeSettings, 0, len(indexing.ExplicitGroups)+len(indexing.MaterializedGroups))
	seen := make(map[string]int, len(indexing.ExplicitGroups)+len(indexing.MaterializedGroups))
	appendGroup := func(groupName, until, source string, enabled bool) {
		group := strings.TrimSpace(groupName)
		if group == "" {
			return
		}
		source = strings.TrimSpace(source)
		if source == "" {
			source = "explicit"
		}
		key := strings.ToLower(group)
		if idx, ok := seen[key]; ok {
			if until != "" {
				out[idx].BackfillUntilDate = until
			}
			out[idx].Enabled = out[idx].Enabled || enabled
			if out[idx].Source == "wildcard" && source == "explicit" {
				out[idx].Source = source
			}
			return
		}
		seen[key] = len(out)
		out = append(out, IndexingScrapeGroupRuntimeSettings{
			GroupName:         group,
			Enabled:           enabled,
			BackfillUntilDate: strings.TrimSpace(until),
			Source:            source,
		})
	}
	for _, item := range indexing.ExplicitGroups {
		appendGroup(item.GroupName, item.BackfillUntilDate, firstNonEmpty(item.Source, "explicit"), item.Enabled)
	}
	for _, item := range indexing.MaterializedGroups {
		appendGroup(item.GroupName, item.BackfillUntilDate, "wildcard", item.Enabled)
	}
	return out
}

func EffectiveNewsgroupNames(indexing *IndexingRuntimeSettings) []string {
	effective := EffectiveScrapeGroups(indexing)
	out := make([]string, 0, len(effective))
	for _, item := range effective {
		if !item.Enabled {
			continue
		}
		out = append(out, item.GroupName)
	}
	return out
}

func EffectiveBackfillUntilDateByGroup(indexing *IndexingRuntimeSettings) map[string]string {
	effective := EffectiveScrapeGroups(indexing)
	if len(effective) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(effective))
	for _, item := range effective {
		if !item.Enabled {
			continue
		}
		if until := strings.TrimSpace(item.BackfillUntilDate); until != "" {
			out[item.GroupName] = until
		}
	}
	return out
}

func normalizeIndexingScrapeConfig(indexing *IndexingRuntimeSettings) {
	if indexing == nil {
		return
	}
	if indexing.ExplicitGroups == nil &&
		indexing.WildcardRules == nil &&
		indexing.ProviderGroupInventory == nil &&
		indexing.MaterializedGroups == nil &&
		(len(indexing.Newsgroups) > 0 || len(indexing.BackfillUntilDateByGroup) > 0) {
		indexing.ExplicitGroups = legacyExplicitGroupsFromConfig(indexing.Newsgroups, indexing.BackfillUntilDateByGroup)
	}
	for i := range indexing.ExplicitGroups {
		indexing.ExplicitGroups[i].GroupName = strings.TrimSpace(indexing.ExplicitGroups[i].GroupName)
		indexing.ExplicitGroups[i].BackfillUntilDate = strings.TrimSpace(indexing.ExplicitGroups[i].BackfillUntilDate)
		indexing.ExplicitGroups[i].Source = firstNonEmpty(indexing.ExplicitGroups[i].Source, "explicit")
	}
	for i := range indexing.WildcardRules {
		indexing.WildcardRules[i].ID = strings.TrimSpace(indexing.WildcardRules[i].ID)
		indexing.WildcardRules[i].Pattern = strings.TrimSpace(indexing.WildcardRules[i].Pattern)
	}
	for i := range indexing.ProviderGroupInventory {
		indexing.ProviderGroupInventory[i].ProviderID = strings.TrimSpace(indexing.ProviderGroupInventory[i].ProviderID)
		indexing.ProviderGroupInventory[i].ProviderName = strings.TrimSpace(indexing.ProviderGroupInventory[i].ProviderName)
		indexing.ProviderGroupInventory[i].GroupName = strings.TrimSpace(indexing.ProviderGroupInventory[i].GroupName)
		indexing.ProviderGroupInventory[i].Status = firstNonEmpty(indexing.ProviderGroupInventory[i].Status, "y")
		indexing.ProviderGroupInventory[i].ScannedAt = strings.TrimSpace(indexing.ProviderGroupInventory[i].ScannedAt)
	}
	for i := range indexing.MaterializedGroups {
		indexing.MaterializedGroups[i].GroupName = strings.TrimSpace(indexing.MaterializedGroups[i].GroupName)
		indexing.MaterializedGroups[i].BackfillUntilDate = strings.TrimSpace(indexing.MaterializedGroups[i].BackfillUntilDate)
		indexing.MaterializedGroups[i].ProviderIDs = slices.Compact(indexing.MaterializedGroups[i].ProviderIDs)
		indexing.MaterializedGroups[i].RuleIDs = slices.Compact(indexing.MaterializedGroups[i].RuleIDs)
	}
	indexing.Newsgroups = EffectiveNewsgroupNames(indexing)
	indexing.BackfillUntilDateByGroup = EffectiveBackfillUntilDateByGroup(indexing)
}

func cloneMaterializedGroups(in []IndexingMaterializedGroupRuntimeSettings) []IndexingMaterializedGroupRuntimeSettings {
	if in == nil {
		return nil
	}
	out := make([]IndexingMaterializedGroupRuntimeSettings, 0, len(in))
	for _, item := range in {
		out = append(out, IndexingMaterializedGroupRuntimeSettings{
			GroupName:         item.GroupName,
			Enabled:           item.Enabled,
			BackfillUntilDate: item.BackfillUntilDate,
			ProviderIDs:       append([]string(nil), item.ProviderIDs...),
			RuleIDs:           append([]string(nil), item.RuleIDs...),
		})
	}
	return out
}

func cloneExplicitGroups(in []IndexingScrapeGroupRuntimeSettings) []IndexingScrapeGroupRuntimeSettings {
	if in == nil {
		return nil
	}
	out := make([]IndexingScrapeGroupRuntimeSettings, 0, len(in))
	out = append(out, in...)
	return out
}

func cloneWildcardRules(in []IndexingWildcardRuleRuntimeSettings) []IndexingWildcardRuleRuntimeSettings {
	if in == nil {
		return nil
	}
	out := make([]IndexingWildcardRuleRuntimeSettings, 0, len(in))
	out = append(out, in...)
	return out
}

func cloneProviderGroupInventory(in []IndexingProviderGroupInventoryRuntimeSettings) []IndexingProviderGroupInventoryRuntimeSettings {
	if in == nil {
		return nil
	}
	out := make([]IndexingProviderGroupInventoryRuntimeSettings, 0, len(in))
	out = append(out, in...)
	return out
}

func cloneInspectRuntimeSettings(in IndexingInspectRuntimeSettings) IndexingInspectRuntimeSettings {
	out := in
	out.BlockedMagicHex = append([]string(nil), in.BlockedMagicHex...)
	return out
}

func normalizeStorageGuardRuntimeSettings(in IndexingStorageGuardRuntimeSettings) IndexingStorageGuardRuntimeSettings {
	if !in.Enabled && in.MinFreeBytes == 0 && in.MinFreePercent == 0 {
		return IndexingStorageGuardRuntimeSettings{Enabled: true, MinFreeBytes: 8 * 1024 * 1024 * 1024, MinFreePercent: 5}
	}
	return in
}

func normalizeMemoryGuardRuntimeSettings(in IndexingMemoryGuardRuntimeSettings) IndexingMemoryGuardRuntimeSettings {
	if !in.Enabled && in.MinAvailableBytes == 0 && in.MinAvailablePercent == 0 && in.MinSwapFreeBytes == 0 {
		return IndexingMemoryGuardRuntimeSettings{Enabled: true, MinAvailableBytes: 2 * 1024 * 1024 * 1024, MinAvailablePercent: 10, MinSwapFreeBytes: 512 * 1024 * 1024}
	}
	if in.MinAvailableBytes < 0 {
		in.MinAvailableBytes = 0
	}
	if in.MinAvailablePercent < 0 {
		in.MinAvailablePercent = 0
	}
	if in.MinAvailablePercent > 100 {
		in.MinAvailablePercent = 100
	}
	if in.MinSwapFreeBytes < 0 {
		in.MinSwapFreeBytes = 0
	}
	return in
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
	if override.MaxBatches > 0 {
		base.MaxBatches = override.MaxBatches
	}
	if override.Concurrency > 0 {
		base.Concurrency = override.Concurrency
	}
	if override.MaxEffectiveConcurrency > 0 {
		base.MaxEffectiveConcurrency = override.MaxEffectiveConcurrency
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
		MaxBatches:              intValue(cfg.MaxBatches, 0),
		MaxEffectiveConcurrency: intValue(cfg.MaxEffectiveConcurrency, 0),
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
	if in.MaxBatches > 0 {
		out.MaxBatches = intPtr(in.MaxBatches)
	}
	if in.Concurrency > 0 {
		out.Concurrency = intPtr(in.Concurrency)
	}
	if in.MaxEffectiveConcurrency > 0 {
		out.MaxEffectiveConcurrency = intPtr(in.MaxEffectiveConcurrency)
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
