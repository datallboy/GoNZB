import { useEffect, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { getCapabilities, getSettings, updateSettings } from '../../shared/api/settings'
import type {
  AdminStageConfigPatch,
  ArrIntegrationRuntimeSettings,
  ControlPlaneCapabilities,
  IndexerRuntimeSettings,
  IndexingRuntimeSettings,
  RuntimeSettings,
  ServerRuntimeSettings,
} from '../../shared/types'

type StageKey =
  | 'scrape_latest'
  | 'scrape_backfill'
  | 'assemble'
  | 'assemble_lane_a'
  | 'assemble_lane_b'
  | 'recover_yenc'
  | 'release_summary_refresh'
  | 'release_archive_nzb'
  | 'release_purge_archived_sources'
  | 'release'
  | 'inspect_discovery'
  | 'inspect_par2'
  | 'inspect_nfo'
  | 'inspect_archive'
  | 'inspect_password'
  | 'inspect_media'
  | 'enrich_predb'
  | 'enrich_tmdb'

type BackfillRow = { group: string; until: string }
type SettingsTab = 'nntp' | 'downloader' | 'aggregator' | 'indexer'

type StageDefinition = {
  key: StageKey
  label: string
  description: string
  supportsConcurrency: boolean
  showBinaryUpsertChunk?: boolean
  showMaxBatches?: boolean
}

const stageDefinitions: StageDefinition[] = [
  { key: 'scrape_latest', label: 'Scrape latest', supportsConcurrency: false, description: 'Fast forward scan for new article headers.' },
  { key: 'scrape_backfill', label: 'Scrape backfill', supportsConcurrency: false, description: 'Older article scan toward each group cutoff date.' },
  { key: 'assemble', label: 'Assemble', supportsConcurrency: true, showBinaryUpsertChunk: true, description: 'Legacy combined lane. Leave disabled if you switch to split lane scheduling.' },
  { key: 'assemble_lane_a', label: 'Assemble lane A', supportsConcurrency: true, showBinaryUpsertChunk: true, description: 'Priority path that feeds existing incomplete binaries first and should keep release backlogged.' },
  { key: 'assemble_lane_b', label: 'Assemble lane B', supportsConcurrency: true, showBinaryUpsertChunk: true, description: 'Backlog-drain path for recent unmatched headers. Usually slower and more write-heavy than lane A.' },
  { key: 'recover_yenc', label: 'Recover yEnc', supportsConcurrency: true, description: 'Post-assemble repair stage. Reads only the start of BODY for weak obfuscated binaries, extracts the yEnc file name, and re-groups binaries without slowing assemble.' },
  { key: 'release_summary_refresh', label: 'Release summary refresh', supportsConcurrency: false, showMaxBatches: true, description: 'Deferred readiness-summary drain. Keeps release-family summary backlog under control before release formation runs.' },
  { key: 'release', label: 'Release', supportsConcurrency: false, description: 'Clusters binaries into releasable families and persists releases.' },
  { key: 'release_archive_nzb', label: 'Archive NZB', supportsConcurrency: false, description: 'Copies release NZBs into the archive store before source purge begins.' },
  { key: 'release_purge_archived_sources', label: 'Purge archived sources', supportsConcurrency: false, description: 'Deletes source article rows only after the archived NZB is present and recorded.' },
  { key: 'inspect_discovery', label: 'Inspect discovery', supportsConcurrency: false, description: 'Opaque-binary inspection discovery pass.' },
  { key: 'inspect_par2', label: 'Inspect PAR2', supportsConcurrency: true, description: 'PAR2 inspection and recovery metadata extraction.' },
  { key: 'inspect_nfo', label: 'Inspect NFO', supportsConcurrency: false, description: 'NFO text extraction and evidence capture.' },
  { key: 'inspect_archive', label: 'Inspect archive', supportsConcurrency: true, description: 'Archive listing and encrypted/password detection.' },
  { key: 'inspect_password', label: 'Inspect password', supportsConcurrency: false, description: 'Password verification workflow.' },
  { key: 'inspect_media', label: 'Inspect media', supportsConcurrency: true, description: 'Media probe and stream metadata extraction.' },
  { key: 'enrich_predb', label: 'Enrich PreDB', supportsConcurrency: false, description: 'Scene-name and metadata enrichment from PreDB.' },
  { key: 'enrich_tmdb', label: 'Enrich TMDB', supportsConcurrency: false, description: 'TMDB and TVDB metadata enrichment.' },
]

const stageGroups: Array<{ title: string; keys: StageKey[] }> = [
  { title: 'Scrape commands', keys: ['scrape_latest', 'scrape_backfill'] },
  { title: 'Assemble and recovery commands', keys: ['assemble', 'assemble_lane_a', 'assemble_lane_b', 'recover_yenc'] },
  { title: 'Release commands', keys: ['release_summary_refresh', 'release', 'release_archive_nzb', 'release_purge_archived_sources'] },
  { title: 'Inspection commands', keys: ['inspect_discovery', 'inspect_par2', 'inspect_nfo', 'inspect_archive', 'inspect_password', 'inspect_media'] },
  { title: 'Enrichment commands', keys: ['enrich_predb', 'enrich_tmdb'] },
]

const settingsTabs: Array<{ key: SettingsTab; label: string }> = [
  { key: 'nntp', label: 'NNTP' },
  { key: 'downloader', label: 'Downloader' },
  { key: 'aggregator', label: 'Aggregator' },
  { key: 'indexer', label: 'Indexer' },
]

function defaultSettings(): RuntimeSettings {
  return {
    servers: [],
    downloader_servers: [],
    indexer_servers: [],
    indexers: [],
    aggregator: { sources: { local_blob: { enabled: false }, usenet_indexer: { enabled: false } } },
    download: {
      out_dir: './downloads',
      completed_dir: './downloads/completed',
      cleanup_extensions: ['nzb', 'par2', 'sfv', 'nfo'],
    },
    nntp_pool: {
      idle_borrow_enabled: true,
      indexer_max_percent: 80,
      downloader_reserve_percent: 20,
      demand_window_seconds: 30,
    },
    indexing: {
      newsgroups: [],
      backfill_until_date_by_group: {},
      scrape_latest: stageDefaults(5000),
      scrape_backfill: stageDefaults(5000),
      assemble: stageDefaults(5000, 1, { binary_upsert_db_chunk_size: 250 }),
      assemble_lane_a: stageDefaults(5000, 1, { binary_upsert_db_chunk_size: 250 }),
      assemble_lane_b: stageDefaults(2500, 1, { binary_upsert_db_chunk_size: 250 }),
      recover_yenc: stageDefaults(25, 1),
      release_summary_refresh: stageDefaults(10000, 0, { max_batches: 10 }),
      release: {
        ...stageDefaults(1000),
        min_confidence: 0.55,
        min_completion_pct: 0,
        min_expected_file_coverage_pct: 90,
        require_expected_file_count_for_contextual_obfuscated: true,
      },
      release_archive_nzb: stageDefaults(100),
      release_purge_archived_sources: stageDefaults(50),
      match: {
        high_confidence_threshold: 0.85,
        probable_confidence_threshold: 0.55,
        article_bucket_size: 5000,
      },
      inspect: {
        work_dir: '/store/indexer/inspect',
        workspace_backend: 'auto',
        memory_work_dir: '/dev/shm/gonzb-inspect',
        max_bytes: 2147483648,
        min_binary_bytes: 0,
        max_binary_bytes: 0,
        blocked_magic_hex: ['52434C4F4E45'],
        max_archive_depth: 3,
        tool_timeout_seconds: 30,
        ffprobe_path: 'ffprobe',
        seven_zip_path: '7z',
        unrar_path: 'unrar',
        par2_path: 'par2',
      },
      inspect_discovery: stageDefaults(100),
      inspect_par2: stageDefaults(100),
      inspect_nfo: stageDefaults(100),
      inspect_archive: stageDefaults(100, 1),
      inspect_password: stageDefaults(100),
      inspect_media: stageDefaults(100, 1),
      enrich_predb: {
        ...stageDefaults(100),
        provider: 'club,me',
        base_url: 'https://predb.club/api/v1',
        feed_url: 'https://predb.me/?rss=1',
        dump_url: '',
        http_timeout_seconds: 10,
        backfill_page_size: 1000,
        max_backfill_pages: 250,
      },
      enrich_tmdb: {
        ...stageDefaults(100),
        http_timeout_seconds: 15,
        tmdb_api_key: '',
        tmdb_access_token: '',
        tmdb_base_url: 'https://api.themoviedb.org/3',
        tvdb_api_key: '',
        tvdb_pin: '',
        tvdb_base_url: 'https://api4.thetvdb.com/v4',
      },
    },
    arr_integrations: [],
  }
}

function stageDefaults(batchSize: number, concurrency = 0, extras: Partial<AdminStageConfigPatch> = {}): AdminStageConfigPatch {
  return { enabled: false, interval_minutes: 10, batch_size: batchSize, concurrency, backoff_seconds: 0, ...extras }
}

function normalizeSettings(input?: RuntimeSettings): RuntimeSettings {
  const defaults = defaultSettings()
  const indexing = (input?.indexing ?? {}) as Partial<IndexingRuntimeSettings>
  return {
    ...defaults,
    ...input,
    servers: input?.servers ?? input?.downloader_servers ?? input?.indexer_servers ?? [],
    downloader_servers: input?.downloader_servers ?? input?.servers ?? [],
    indexer_servers: input?.indexer_servers ?? input?.servers ?? [],
    indexers: input?.indexers ?? [],
    arr_integrations: input?.arr_integrations ?? [],
    aggregator: {
      ...defaults.aggregator,
      ...input?.aggregator,
      sources: {
        ...defaults.aggregator?.sources,
        ...input?.aggregator?.sources,
        local_blob: { enabled: Boolean(input?.aggregator?.sources?.local_blob?.enabled) },
        usenet_indexer: { enabled: Boolean(input?.aggregator?.sources?.usenet_indexer?.enabled) },
      },
    },
    download: {
      ...defaults.download!,
      ...input?.download,
      cleanup_extensions: input?.download?.cleanup_extensions ?? defaults.download!.cleanup_extensions,
    },
    nntp_pool: {
      ...defaults.nntp_pool!,
      ...input?.nntp_pool,
    },
    indexing: {
      ...defaults.indexing!,
      ...indexing,
      newsgroups: indexing.newsgroups ?? [],
      backfill_until_date_by_group: indexing.backfill_until_date_by_group ?? {},
      scrape_latest: { ...defaults.indexing!.scrape_latest, ...indexing.scrape_latest },
      scrape_backfill: { ...defaults.indexing!.scrape_backfill, ...indexing.scrape_backfill },
      assemble: { ...defaults.indexing!.assemble, ...indexing.assemble },
      assemble_lane_a: { ...defaults.indexing!.assemble_lane_a, ...indexing.assemble_lane_a },
      assemble_lane_b: { ...defaults.indexing!.assemble_lane_b, ...indexing.assemble_lane_b },
      recover_yenc: { ...defaults.indexing!.recover_yenc, ...indexing.recover_yenc },
      release_summary_refresh: { ...defaults.indexing!.release_summary_refresh, ...indexing.release_summary_refresh },
      release: { ...defaults.indexing!.release, ...indexing.release },
      release_archive_nzb: { ...defaults.indexing!.release_archive_nzb, ...indexing.release_archive_nzb },
      release_purge_archived_sources: { ...defaults.indexing!.release_purge_archived_sources, ...indexing.release_purge_archived_sources },
      match: { ...defaults.indexing!.match, ...indexing.match },
      inspect: { ...defaults.indexing!.inspect, ...indexing.inspect },
      inspect_discovery: { ...defaults.indexing!.inspect_discovery, ...indexing.inspect_discovery },
      inspect_par2: { ...defaults.indexing!.inspect_par2, ...indexing.inspect_par2 },
      inspect_nfo: { ...defaults.indexing!.inspect_nfo, ...indexing.inspect_nfo },
      inspect_archive: { ...defaults.indexing!.inspect_archive, ...indexing.inspect_archive },
      inspect_password: { ...defaults.indexing!.inspect_password, ...indexing.inspect_password },
      inspect_media: { ...defaults.indexing!.inspect_media, ...indexing.inspect_media },
      enrich_predb: { ...defaults.indexing!.enrich_predb, ...indexing.enrich_predb },
      enrich_tmdb: { ...defaults.indexing!.enrich_tmdb, ...indexing.enrich_tmdb },
    },
  }
}

function serverDefaults(index: number): ServerRuntimeSettings {
  return {
    id: `server-${index + 1}`,
    host: '',
    port: 563,
    username: '',
    password: '',
    tls: true,
    max_connections: 10,
    priority: index + 1,
    dial_timeout_seconds: 10,
    tcp_keepalive_seconds: 30,
    pool_idle_timeout_seconds: 45,
    pool_max_age_seconds: 600,
    enable_pool_logging: false,
  }
}

function indexerDefaults(index: number): IndexerRuntimeSettings {
  return { id: `newznab-${index + 1}`, base_url: '', api_path: '/api', api_key: '', redirect: false }
}

function arrDefaults(index: number): ArrIntegrationRuntimeSettings {
  return { id: `arr-${index + 1}`, kind: 'sonarr', enabled: false, base_url: '', api_key: '', client_name: '', category: '' }
}

function fieldNumber(value: string) {
  return Number.isFinite(Number(value)) ? Number(value) : 0
}

function newsgroupRows(indexing: IndexingRuntimeSettings): BackfillRow[] {
  const cutoffs = indexing.backfill_until_date_by_group ?? {}
  const rows = indexing.newsgroups.map((group) => ({ group, until: cutoffs[group] ?? '' }))
  for (const [group, until] of Object.entries(cutoffs)) {
    if (!rows.some((row) => row.group === group)) {
      rows.push({ group, until })
    }
  }
  return rows
}

function rowsToBackfillMap(rows: BackfillRow[]) {
  return Object.fromEntries(rows.filter((row) => row.group.trim()).map((row) => [row.group.trim(), row.until.trim()]))
}

function applyNewsgroupRows(indexing: IndexingRuntimeSettings, rows: BackfillRow[]): IndexingRuntimeSettings {
  return {
    ...indexing,
    newsgroups: rows.map((row) => row.group.trim()).filter(Boolean),
    backfill_until_date_by_group: rowsToBackfillMap(rows),
  }
}

function cleanupExtensionsText(items: string[]) {
  return items.join(', ')
}

function parseCleanupExtensions(value: string) {
  return value.split(',').map((item) => item.trim()).filter(Boolean)
}

function parseCSV(value: string) {
  return value.split(',').map((item) => item.trim()).filter(Boolean)
}

function serversForSave(servers: ServerRuntimeSettings[], prefix: string) {
  return servers.map((server, index) => ({
    ...server,
    id: deriveServerID(server, index, prefix),
  }))
}

function deriveServerID(server: ServerRuntimeSettings, index: number, prefix: string) {
  const hostID = server.host.trim().toLowerCase().replace(/[^a-z0-9.-]+/g, '-').replace(/-+/g, '-').replace(/^-+|-+$/g, '')
  if (hostID) {
    return hostID
  }
  return server.id?.trim() || `${prefix}-${index + 1}`
}

function serverTitle(server: ServerRuntimeSettings, index: number) {
  const host = server.host.trim()
  if (host) {
    return host
  }
  return `Server ${index + 1}`
}

function sanitizeIndexingForSave(indexing: IndexingRuntimeSettings): IndexingRuntimeSettings {
  return {
    ...indexing,
    release: {
      enabled: indexing.release.enabled,
      interval_minutes: indexing.release.interval_minutes,
      batch_size: indexing.release.batch_size,
      backoff_seconds: indexing.release.backoff_seconds,
      min_confidence: indexing.release.min_confidence,
      min_completion_pct: indexing.release.min_completion_pct,
      min_expected_file_coverage_pct: indexing.release.min_expected_file_coverage_pct,
      require_expected_file_count_for_contextual_obfuscated: indexing.release.require_expected_file_count_for_contextual_obfuscated,
    },
    enrich_predb: {
      enabled: indexing.enrich_predb.enabled,
      interval_minutes: indexing.enrich_predb.interval_minutes,
      batch_size: indexing.enrich_predb.batch_size,
      backoff_seconds: indexing.enrich_predb.backoff_seconds,
      provider: indexing.enrich_predb.provider,
      base_url: indexing.enrich_predb.base_url,
      feed_url: indexing.enrich_predb.feed_url,
      dump_url: indexing.enrich_predb.dump_url,
      http_timeout_seconds: indexing.enrich_predb.http_timeout_seconds,
      backfill_page_size: indexing.enrich_predb.backfill_page_size,
      max_backfill_pages: indexing.enrich_predb.max_backfill_pages,
    },
    enrich_tmdb: {
      enabled: indexing.enrich_tmdb.enabled,
      interval_minutes: indexing.enrich_tmdb.interval_minutes,
      batch_size: indexing.enrich_tmdb.batch_size,
      backoff_seconds: indexing.enrich_tmdb.backoff_seconds,
      http_timeout_seconds: indexing.enrich_tmdb.http_timeout_seconds,
      tmdb_api_key: indexing.enrich_tmdb.tmdb_api_key,
      tmdb_access_token: indexing.enrich_tmdb.tmdb_access_token,
      tmdb_base_url: indexing.enrich_tmdb.tmdb_base_url,
      tvdb_api_key: indexing.enrich_tmdb.tvdb_api_key,
      tvdb_pin: indexing.enrich_tmdb.tvdb_pin,
      tvdb_base_url: indexing.enrich_tmdb.tvdb_base_url,
    },
  }
}

function buildTabPatch(tab: SettingsTab, settings: RuntimeSettings) {
  switch (tab) {
    case 'nntp':
      return {
        servers: serversForSave(settings.servers ?? [], 'nntp'),
        nntp_pool: settings.nntp_pool,
      }
    case 'downloader':
      return {
        download: settings.download,
        arr_integrations: settings.arr_integrations ?? [],
      }
    case 'aggregator':
      return {
        indexers: settings.indexers ?? [],
        aggregator: settings.aggregator,
      }
    case 'indexer':
      return {
        indexing: settings.indexing ? sanitizeIndexingForSave(settings.indexing) : settings.indexing,
      }
  }
}

export function AdminSettingsPage() {
  const [settings, setSettings] = useState<RuntimeSettings>(defaultSettings())
  const [capabilities, setCapabilities] = useState<ControlPlaneCapabilities | null>(null)
  const [activeTab, setActiveTab] = useState<SettingsTab>('nntp')
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [newsgroupDrafts, setNewsgroupDrafts] = useState<BackfillRow[]>([])
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      const [nextSettings, nextCapabilities] = await Promise.all([getSettings(), getCapabilities()])
      const normalized = normalizeSettings(nextSettings as RuntimeSettings)
      setSettings(normalized)
      setNewsgroupDrafts(newsgroupRows(normalized.indexing!))
      setCapabilities(nextCapabilities as ControlPlaneCapabilities)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load runtime settings')
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    await saveSettings(activeTab)
  }

  async function saveSettings(tab: SettingsTab) {
    setMessage(null)
    setError(null)
    try {
      const normalized = normalizeSettings(settings)
      const updated = (await updateSettings(buildTabPatch(tab, normalized) as Record<string, unknown>)) as RuntimeSettings
      const next = normalizeSettings(updated)
      setSettings(next)
      setNewsgroupDrafts(newsgroupRows(next.indexing!))
      setMessage(`${tabLabel(tab)} settings updated.`)
      void getCapabilities().then((next) => setCapabilities(next as ControlPlaneCapabilities))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update runtime settings')
    }
  }

  const normalized = normalizeSettings(settings)
  const indexing = normalized.indexing!
  const aggregator = normalized.aggregator!
  const download = normalized.download!
  const nntpPool = normalized.nntp_pool!
  const servers = normalized.servers ?? []
  const indexers = normalized.indexers ?? []
  const arrIntegrations = normalized.arr_integrations ?? []
  const lockNNTPServers = Boolean(capabilities?.modules.downloader?.ready || capabilities?.modules.usenet_indexer?.ready)
  const lockIndexers = Boolean(capabilities?.modules.aggregator?.ready)
  const lockIndexerLists = Boolean(capabilities?.modules.usenet_indexer?.ready)
  const lockArr = Boolean(capabilities?.modules.downloader?.ready)
  const requirements = capabilityRequirements(capabilities)

  function setIndexing(next: IndexingRuntimeSettings) {
    setSettings((current) => ({ ...current, indexing: next }))
  }

  function updateStage(key: StageKey, patch: AdminStageConfigPatch) {
    setIndexing({ ...indexing, [key]: { ...indexing[key], ...patch } })
  }

  function stageDefinition(key: StageKey) {
    return stageDefinitions.find((item) => item.key === key)!
  }

  return (
    <div className="page-section stack">
      <div className="page-card">
        <p className="eyebrow">Runtime Settings</p>
        <h1 className="page-title">Settings</h1>
      </div>

      <form className="stack" onSubmit={handleSubmit}>
        <div className="settings-tabs" role="tablist" aria-label="Runtime settings modules">
          {settingsTabs.map((tab) => (
            <button
              key={tab.key}
              type="button"
              className={tab.key === activeTab ? 'settings-tab is-active' : 'settings-tab'}
              aria-selected={tab.key === activeTab}
              onClick={() => {
                setActiveTab(tab.key)
                setMessage(null)
                setError(null)
              }}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {requirements.length > 0 ? (
          <div className="banner">
            <strong>Configuration required</strong>
            <ul>
              {requirements.map((requirement) => (
                <li key={requirement}>{requirement}</li>
              ))}
            </ul>
          </div>
        ) : null}

        {activeTab === 'downloader' ? (
        <ModuleGroup title="Downloader settings">
          <SettingsSection title="Paths">
            <div className="toolbar-grid">
              <TextField label="Output directory" value={download.out_dir} onChange={(value) => setSettings((current) => ({ ...current, download: { ...download, out_dir: value } }))} />
              <TextField label="Completed directory" value={download.completed_dir} onChange={(value) => setSettings((current) => ({ ...current, download: { ...download, completed_dir: value } }))} />
              <TextField label="Cleanup extensions" value={cleanupExtensionsText(download.cleanup_extensions)} onChange={(value) => setSettings((current) => ({ ...current, download: { ...download, cleanup_extensions: parseCleanupExtensions(value) } }))} />
            </div>
          </SettingsSection>

          <SettingsSection
            title="ARR integrations"
            locked={lockArr}
            lockedMessage="ARR integration removal is disabled while downloader is ready."
            onAdd={() => setSettings((current) => ({ ...current, arr_integrations: [...arrIntegrations, arrDefaults(arrIntegrations.length)] }))}
          >
            {arrIntegrations.map((integration, index) => (
              <div className="settings-row stack" key={`${integration.id}-${index}`}>
                <div className="button-row">
                  <strong>Integration {index + 1}</strong>
                  <RemoveButton locked={lockArr} onClick={() => setSettings((current) => ({ ...current, arr_integrations: arrIntegrations.filter((_, i) => i !== index) }))} />
                </div>
                <div className="toolbar-grid">
                  <TextField label="ID" value={integration.id} required={integration.enabled} onChange={(value) => updateArr(index, { id: value })} />
                  <TextField label="Kind" value={integration.kind} required={integration.enabled} onChange={(value) => updateArr(index, { kind: value })} />
                  <TextField label="Base URL" value={integration.base_url} required={integration.enabled} onChange={(value) => updateArr(index, { base_url: value })} />
                  <TextField label="API key" type="password" value={integration.api_key} required={integration.enabled} onChange={(value) => updateArr(index, { api_key: value })} />
                  <TextField label="Client name" value={integration.client_name ?? ''} onChange={(value) => updateArr(index, { client_name: value })} />
                  <TextField label="Category" value={integration.category ?? ''} onChange={(value) => updateArr(index, { category: value })} />
                  <CheckboxField label="Enabled" checked={integration.enabled} onChange={(value) => updateArr(index, { enabled: value })} />
                </div>
              </div>
            ))}
          </SettingsSection>
          <SettingsActions onReload={() => void refresh()} />
        </ModuleGroup>
        ) : null}

        {activeTab === 'nntp' ? (
        <ModuleGroup title="NNTP settings">
          <SettingsSection
            title="Providers"
            locked={lockNNTPServers}
            lockedMessage="NNTP provider removal is disabled while downloader or indexer runtime is ready."
            onAdd={() => setSettings((current) => ({ ...current, servers: [...servers, serverDefaults(servers.length)] }))}
          >
            {servers.map((server, index) => (
              <ServerFields
                key={index}
                title={serverTitle(server, index)}
                server={server}
                locked={lockNNTPServers}
                onRemove={() => setSettings((current) => ({ ...current, servers: servers.filter((_, i) => i !== index) }))}
                onChange={(patch) => updateServer(index, patch)}
              />
            ))}
          </SettingsSection>

          <SettingsSection title="Pool sharing">
            <div className="toolbar-grid">
              <CheckboxField
                label="Idle borrow"
                checked={Boolean(nntpPool.idle_borrow_enabled)}
                onChange={(value) => setSettings((current) => ({ ...current, nntp_pool: { ...nntpPool, idle_borrow_enabled: value } }))}
                helpText="Allows indexer work to use the full NNTP pool while downloader demand is quiet."
              />
              <NumberField
                label="Indexer max %"
                min={1}
                max={100}
                value={nntpPool.indexer_max_percent}
                onChange={(value) => setSettings((current) => ({ ...current, nntp_pool: { ...nntpPool, indexer_max_percent: value } }))}
                helpText="Maximum indexer share while downloader demand is active, or always when idle borrow is off."
              />
              <NumberField
                label="Downloader reserve %"
                min={1}
                max={100}
                value={nntpPool.downloader_reserve_percent}
                onChange={(value) => setSettings((current) => ({ ...current, nntp_pool: { ...nntpPool, downloader_reserve_percent: value } }))}
                helpText="Reserved downloader share used when deriving pool behavior."
              />
              <NumberField
                label="Demand window seconds"
                min={1}
                value={nntpPool.demand_window_seconds}
                onChange={(value) => setSettings((current) => ({ ...current, nntp_pool: { ...nntpPool, demand_window_seconds: value } }))}
                helpText="How long recent downloader demand keeps indexer borrowing capped."
              />
            </div>
          </SettingsSection>
          <SettingsActions onReload={() => void refresh()} />
        </ModuleGroup>
        ) : null}

        {activeTab === 'aggregator' ? (
        <ModuleGroup title="Aggregator settings">
          <SettingsSection title="Sources">
            <div className="toolbar-grid">
              <CheckboxField
                label="Local blob cache"
                checked={Boolean(aggregator.sources?.local_blob?.enabled)}
                onChange={(enabled) => setSettings((current) => ({ ...current, aggregator: { sources: { ...aggregator.sources, local_blob: { enabled } } } }))}
              />
              <CheckboxField
                label="Local indexer"
                checked={Boolean(aggregator.sources?.usenet_indexer?.enabled)}
                onChange={(enabled) => setSettings((current) => ({ ...current, aggregator: { sources: { ...aggregator.sources, usenet_indexer: { enabled } } } }))}
              />
            </div>
          </SettingsSection>

          <SettingsSection
            title="External Newznab"
            locked={lockIndexers}
            lockedMessage="Source removal is disabled while aggregator is ready."
            onAdd={() => setSettings((current) => ({ ...current, indexers: [...indexers, indexerDefaults(indexers.length)] }))}
          >
            {indexers.map((indexer, index) => (
              <div className="settings-row stack" key={`${indexer.id}-${index}`}>
                <div className="button-row">
                  <strong>Source {index + 1}</strong>
                  <RemoveButton locked={lockIndexers} onClick={() => setSettings((current) => ({ ...current, indexers: indexers.filter((_, i) => i !== index) }))} />
                </div>
                <div className="toolbar-grid">
                  <TextField label="ID" value={indexer.id} required onChange={(value) => updateIndexer(index, { id: value })} />
                  <TextField label="Base URL" value={indexer.base_url} required onChange={(value) => updateIndexer(index, { base_url: value })} />
                  <TextField label="API path" value={indexer.api_path} required onChange={(value) => updateIndexer(index, { api_path: value })} />
                  <TextField label="API key" type="password" value={indexer.api_key} onChange={(value) => updateIndexer(index, { api_key: value })} />
                  <CheckboxField label="Redirect downloads" checked={indexer.redirect} onChange={(value) => updateIndexer(index, { redirect: value })} />
                </div>
              </div>
            ))}
          </SettingsSection>
          <SettingsActions onReload={() => void refresh()} />
        </ModuleGroup>
        ) : null}

        {activeTab === 'indexer' ? (
        <ModuleGroup title="Indexer settings">
          <SettingsSection title="Newsgroups">
            <div className="button-row">
              <strong>Groups</strong>
              <button
                className="secondary-button"
                type="button"
                onClick={() => setNewsgroupDrafts((current) => [...current, { group: '', until: '' }])}
              >
                Add Newsgroup
              </button>
            </div>
            {newsgroupDrafts.map((row, index) => (
              <div className="newsgroup-row" key={index}>
                <TextField
                  label="Newsgroup"
                  value={row.group}
                  required
                  onChange={(value) => updateNewsgroup(index, { group: value })}
                />
                <DateField
                  label="Backfill until date"
                  value={row.until}
                  onChange={(value) => updateNewsgroup(index, { until: value })}
                  helpText="Uses YYYY-MM-DD. Example: 2026-04-01 means April 1, 2026. Backfill stops once the group reaches articles on or before that date."
                />
                <button
                  className="secondary-button newsgroup-row__remove"
                  type="button"
                  disabled={lockIndexerLists}
                  onClick={() => {
                    const rows = newsgroupDrafts.filter((_, i) => i !== index)
                    setNewsgroupDrafts(rows)
                    setIndexing(applyNewsgroupRows(indexing, rows))
                  }}
                >
                  Remove
                </button>
              </div>
            ))}
          </SettingsSection>

        <SettingsSection title="Runtime stage controls">
          <div className="banner">
            Each command has its own runtime controls here. Batch size controls claim size per pass. Concurrency only appears on commands that support parallel workers.
          </div>
          <div className="toolbar-grid toolbar-grid--compact">
            <CheckboxField
              label="Show advanced settings"
              checked={showAdvanced}
              helpText="Advanced controls expose lower-level persistence tuning that should usually stay at the default."
              onChange={setShowAdvanced}
            />
          </div>
          <div className="settings-stage-groups">
            {stageGroups.map((group) => (
              <div className="stack" key={group.title}>
                <h3 className="section-subtitle">{group.title}</h3>
                <div className="settings-stage-list">
                  {group.keys.map((key) => {
                    const definition = stageDefinition(key)
                    const value = indexing[key] as AdminStageConfigPatch
                    return (
                      <div className="settings-row settings-stage-card stack" key={key}>
                        <div className="settings-stage-card__header">
                          <div>
                            <strong>{definition.label}</strong>
                            <div className="muted-copy">{definition.description}</div>
                          </div>
                        </div>
                        <div className="toolbar-grid">
                          <CheckboxField label="Enabled" checked={Boolean(value.enabled)} onChange={(next) => updateStage(key, { enabled: next })} />
                          <NumberField label="Interval minutes" min={1} step="0.5" value={value.interval_minutes ?? 0} onChange={(next) => updateStage(key, { interval_minutes: next })} />
                          <NumberField label="Batch size" min={1} value={value.batch_size ?? 0} onChange={(next) => updateStage(key, { batch_size: next })} />
                          {definition.showMaxBatches ? (
                            <NumberField label="Max batches" min={1} value={value.max_batches ?? 10} onChange={(next) => updateStage(key, { max_batches: next })} />
                          ) : null}
                          <NumberField label="Backoff seconds" min={0} value={value.backoff_seconds ?? 0} onChange={(next) => updateStage(key, { backoff_seconds: next })} />
                          {definition.supportsConcurrency ? (
                            <NumberField label="Concurrency" min={1} value={value.concurrency ?? 1} onChange={(next) => updateStage(key, { concurrency: next })} />
                          ) : null}
                        </div>
                        {showAdvanced && definition.showBinaryUpsertChunk ? (
                          <div className="toolbar-grid toolbar-grid--compact">
                            <NumberField
                              label="Binary upsert DB chunk size"
                              min={1}
                              value={value.binary_upsert_db_chunk_size ?? 250}
                              helpText="Internal binary-upsert chunk size for assemble writes. Default 250. Use this only when tuning Postgres lock pressure versus write throughput."
                              onChange={(next) => updateStage(key, { binary_upsert_db_chunk_size: next })}
                            />
                          </div>
                        ) : null}
                      </div>
                    )
                  })}
                </div>
              </div>
            ))}
          </div>
        </SettingsSection>

        <SettingsSection title="Release candidate selection and matching">
          <div className="banner">
            Release settings below affect two different parts of the pipeline. Candidate selection decides which release families are worth processing now. Matching settings affect how article headers are grouped into binaries during assemble.
          </div>
          <div className="toolbar-grid">
            <NumberField
              label="Minimum expected file coverage %"
              min={0}
              max={100}
              value={indexing.release.min_expected_file_coverage_pct}
              helpText="Used during release candidate selection. When a family has an expected file count, this percent of expected files must be complete before release prioritizes the family for formation."
              onChange={(value) => setIndexing({ ...indexing, release: { ...indexing.release, min_expected_file_coverage_pct: value } })}
            />
            <NumberField
              label="Minimum confidence"
              step="0.01"
              min={0}
              max={1}
              value={indexing.release.min_confidence}
              helpText="Final release persistence gate. Lower values allow weaker release identities to be saved after clustering."
              onChange={(value) => setIndexing({ ...indexing, release: { ...indexing.release, min_confidence: value } })}
            />
            <NumberField
              label="Minimum completion %"
              min={0}
              max={100}
              value={indexing.release.min_completion_pct}
              helpText="Final release persistence gate. Applies after a family is selected and clustered, so it does not improve queue quality by itself."
              onChange={(value) => setIndexing({ ...indexing, release: { ...indexing.release, min_completion_pct: value } })}
            />
            <CheckboxField
              label="Require expected file count for contextual obfuscated releases"
              helpText="Conservative guardrail for heavily obfuscated multi-file releases. Keeps release formation from trusting weak contextual file groups when the total expected file count is unknown."
              checked={Boolean(indexing.release.require_expected_file_count_for_contextual_obfuscated)}
              onChange={(value) => setIndexing({ ...indexing, release: { ...indexing.release, require_expected_file_count_for_contextual_obfuscated: value } })}
            />
            <NumberField
              label="High confidence threshold"
              step="0.01"
              min={0}
              max={1}
              value={indexing.match.high_confidence_threshold}
              helpText="Assemble matcher short-circuit threshold. Higher values make binary identity matching more conservative."
              onChange={(value) => setIndexing({ ...indexing, match: { ...indexing.match, high_confidence_threshold: value } })}
            />
            <NumberField
              label="Probable confidence threshold"
              step="0.01"
              min={0}
              max={1}
              value={indexing.match.probable_confidence_threshold}
              helpText="Assemble matcher fallback threshold for weaker but still plausible identity matches."
              onChange={(value) => setIndexing({ ...indexing, match: { ...indexing.match, probable_confidence_threshold: value } })}
            />
            <NumberField
              label="Article bucket size"
              min={1}
              value={indexing.match.article_bucket_size}
              helpText="Assemble matching proximity window. Larger buckets help correlate more distant multipart posts, but they can increase noisy grouping."
              onChange={(value) => setIndexing({ ...indexing, match: { ...indexing.match, article_bucket_size: value } })}
            />
          </div>
        </SettingsSection>

        <SettingsSection title="Inspection tools">
          <div className="banner">
            Content filters are conservative inspection guardrails. They mark completed opaque binaries as filtered so later inspect stages do not keep spending time on known-unwanted payloads.
          </div>
          <div className="toolbar-grid">
            <NumberField
              label="Max inspect bytes"
              value={indexing.inspect.max_bytes}
              helpText="Safety cap for materializing a binary during deep inspection. This is not a release size filter."
              onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, max_bytes: value } })}
            />
            <NumberField
              label="Minimum binary bytes"
              min={0}
              value={indexing.inspect.min_binary_bytes}
              helpText="0 disables. Completed opaque binaries smaller than this are marked content-filtered during inspect discovery."
              onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, min_binary_bytes: value } })}
            />
            <NumberField
              label="Maximum binary bytes"
              min={0}
              value={indexing.inspect.max_binary_bytes}
              helpText="0 disables. Completed opaque binaries larger than this are marked content-filtered during inspect discovery."
              onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, max_binary_bytes: value } })}
            />
            <TextField
              label="Blocked magic bytes"
              value={(indexing.inspect.blocked_magic_hex ?? []).join(', ')}
              helpText="Comma-separated hex prefixes to filter after sampling. Default RCLONE magic is 52434C4F4E45."
              onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, blocked_magic_hex: parseCSV(value) } })}
            />
            <NumberField label="Max archive depth" value={indexing.inspect.max_archive_depth} onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, max_archive_depth: value } })} />
            <NumberField label="Tool timeout seconds" value={indexing.inspect.tool_timeout_seconds} onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, tool_timeout_seconds: value } })} />
            <TextField label="ffprobe path" value={indexing.inspect.ffprobe_path} onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, ffprobe_path: value } })} />
            <TextField label="7z path" value={indexing.inspect.seven_zip_path} onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, seven_zip_path: value } })} />
            <TextField label="unrar path" value={indexing.inspect.unrar_path} onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, unrar_path: value } })} />
            <TextField label="par2 path" value={indexing.inspect.par2_path} onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, par2_path: value } })} />
          </div>
          <div className="stack">
            <div className="banner">
              Archive/media inspection workspaces can use RAM or disk. `Auto` prefers RAM when available and falls back to disk.
            </div>
            <div className="page-card stack">
              <h3 className="section-subtitle">Workspace Storage</h3>
              <p className="muted-copy">
                Use `Work dir` for normal disk-backed workspaces and fallback storage. Use `Memory work dir` only for RAM-backed workspaces.
              </p>
              <div className="toolbar-grid">
                <label>
                  <span>Workspace backend</span>
                  <select
                    value={indexing.inspect.workspace_backend}
                    onChange={(event) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, workspace_backend: event.target.value } })}
                  >
                    <option value="auto">Auto</option>
                    <option value="memory">Memory</option>
                    <option value="disk">Disk</option>
                  </select>
                </label>
                <TextField label="Work dir" value={indexing.inspect.work_dir} onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, work_dir: value } })} />
                <TextField label="Memory work dir" value={indexing.inspect.memory_work_dir} onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, memory_work_dir: value } })} />
              </div>
            </div>
          </div>
        </SettingsSection>

        <SettingsSection title="Enrichment">
          <div className="toolbar-grid">
            <TextField label="PreDB provider" value={indexing.enrich_predb.provider} onChange={(value) => setIndexing({ ...indexing, enrich_predb: { ...indexing.enrich_predb, provider: value } })} />
            <TextField label="PreDB base URL" value={indexing.enrich_predb.base_url} onChange={(value) => setIndexing({ ...indexing, enrich_predb: { ...indexing.enrich_predb, base_url: value } })} />
            <TextField label="PreDB feed URL" value={indexing.enrich_predb.feed_url} onChange={(value) => setIndexing({ ...indexing, enrich_predb: { ...indexing.enrich_predb, feed_url: value } })} />
            <TextField label="PreDB dump URL" value={indexing.enrich_predb.dump_url} onChange={(value) => setIndexing({ ...indexing, enrich_predb: { ...indexing.enrich_predb, dump_url: value } })} />
            <NumberField label="PreDB HTTP timeout" value={indexing.enrich_predb.http_timeout_seconds} onChange={(value) => setIndexing({ ...indexing, enrich_predb: { ...indexing.enrich_predb, http_timeout_seconds: value } })} />
            <NumberField label="PreDB backfill page size" value={indexing.enrich_predb.backfill_page_size} onChange={(value) => setIndexing({ ...indexing, enrich_predb: { ...indexing.enrich_predb, backfill_page_size: value } })} />
            <NumberField label="PreDB max backfill pages" value={indexing.enrich_predb.max_backfill_pages} onChange={(value) => setIndexing({ ...indexing, enrich_predb: { ...indexing.enrich_predb, max_backfill_pages: value } })} />
            <TextField label="TMDB API key" type="password" value={indexing.enrich_tmdb.tmdb_api_key} onChange={(value) => setIndexing({ ...indexing, enrich_tmdb: { ...indexing.enrich_tmdb, tmdb_api_key: value } })} />
            <TextField label="TMDB access token" type="password" value={indexing.enrich_tmdb.tmdb_access_token} onChange={(value) => setIndexing({ ...indexing, enrich_tmdb: { ...indexing.enrich_tmdb, tmdb_access_token: value } })} />
            <TextField label="TMDB base URL" value={indexing.enrich_tmdb.tmdb_base_url} onChange={(value) => setIndexing({ ...indexing, enrich_tmdb: { ...indexing.enrich_tmdb, tmdb_base_url: value } })} />
            <TextField label="TVDB API key" type="password" value={indexing.enrich_tmdb.tvdb_api_key} onChange={(value) => setIndexing({ ...indexing, enrich_tmdb: { ...indexing.enrich_tmdb, tvdb_api_key: value } })} />
            <TextField label="TVDB PIN" type="password" value={indexing.enrich_tmdb.tvdb_pin} onChange={(value) => setIndexing({ ...indexing, enrich_tmdb: { ...indexing.enrich_tmdb, tvdb_pin: value } })} />
            <TextField label="TVDB base URL" value={indexing.enrich_tmdb.tvdb_base_url} onChange={(value) => setIndexing({ ...indexing, enrich_tmdb: { ...indexing.enrich_tmdb, tvdb_base_url: value } })} />
            <NumberField label="TMDB/TVDB HTTP timeout" value={indexing.enrich_tmdb.http_timeout_seconds} onChange={(value) => setIndexing({ ...indexing, enrich_tmdb: { ...indexing.enrich_tmdb, http_timeout_seconds: value } })} />
          </div>
        </SettingsSection>
          <SettingsActions onReload={() => void refresh()} />
        </ModuleGroup>
        ) : null}

        {message ? <div className="banner">{message}</div> : null}
        {error ? (
          <div className="banner error">
            {formatError(error).map((line, index) => (
              <div key={`${line}-${index}`}>{line}</div>
            ))}
          </div>
        ) : null}
      </form>
    </div>
  )

  function updateServer(index: number, patch: Partial<ServerRuntimeSettings>) {
    setSettings((current) => ({ ...current, servers: servers.map((item, i) => (i === index ? { ...item, ...patch } : item)) }))
  }

  function updateIndexer(index: number, patch: Partial<IndexerRuntimeSettings>) {
    setSettings((current) => ({ ...current, indexers: indexers.map((item, i) => (i === index ? { ...item, ...patch } : item)) }))
  }

  function updateArr(index: number, patch: Partial<ArrIntegrationRuntimeSettings>) {
    setSettings((current) => ({ ...current, arr_integrations: arrIntegrations.map((item, i) => (i === index ? { ...item, ...patch } : item)) }))
  }

  function updateNewsgroup(index: number, patch: Partial<BackfillRow>) {
    const rows = newsgroupDrafts.map((row, i) => (i === index ? { ...row, ...patch } : row))
    setNewsgroupDrafts(rows)
    setIndexing(applyNewsgroupRows(indexing, rows))
  }
}

function capabilityRequirements(capabilities: ControlPlaneCapabilities | null) {
  if (!capabilities) {
    return []
  }
  return Object.entries(capabilities.modules ?? {}).flatMap(([moduleName, capability]) =>
    (capability.requirements ?? []).map((requirement) => `${moduleName}: ${requirement}`),
  )
}

function formatError(error: string) {
  return error.split('; ').filter(Boolean)
}

function tabLabel(tab: SettingsTab) {
  return settingsTabs.find((item) => item.key === tab)?.label ?? 'Runtime'
}

function SettingsActions({ onReload }: { onReload: () => void }) {
  return (
    <div className="settings-actions">
      <button className="secondary-button" type="button" onClick={onReload}>
        Reload
      </button>
      <button className="primary-button" type="submit">
        Save Settings
      </button>
    </div>
  )
}

function ModuleGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="module-settings-group stack">
      <div className="module-settings-heading">
        <h2>{title}</h2>
      </div>
      {children}
    </section>
  )
}

function SettingsSection({
  title,
  children,
  locked,
  lockedMessage,
  onAdd,
}: {
  title: string
  children: ReactNode
  locked?: boolean
  lockedMessage?: string
  onAdd?: () => void
}) {
  return (
    <div className="settings-subsection stack">
      <div className="button-row">
        <h2 className="section-title">{title}</h2>
        {onAdd ? (
          <button className="secondary-button" type="button" onClick={onAdd}>
            Add
          </button>
        ) : null}
      </div>
      {locked && lockedMessage ? <div className="banner">{lockedMessage}</div> : null}
      {children}
    </div>
  )
}

function ServerFields({
  title,
  server,
  locked,
  onRemove,
  onChange,
}: {
  title: string
  server: ServerRuntimeSettings
  locked?: boolean
  onRemove: () => void
  onChange: (patch: Partial<ServerRuntimeSettings>) => void
}) {
  return (
    <div className="settings-row stack">
      <div className="button-row">
        <strong>{title}</strong>
        <RemoveButton locked={locked} onClick={onRemove} />
      </div>
      <div className="toolbar-grid">
        <TextField label="Host" value={server.host} required onChange={(value) => onChange({ host: value })} />
        <NumberField label="Port" value={server.port} required min={1} max={65535} onChange={(value) => onChange({ port: value })} />
        <TextField label="Username" value={server.username} onChange={(value) => onChange({ username: value })} />
        <TextField label="Password" type="password" value={server.password} onChange={(value) => onChange({ password: value })} />
        <NumberField label="Max connections" value={server.max_connections} onChange={(value) => onChange({ max_connections: value })} />
        <NumberField label="Priority" value={server.priority} onChange={(value) => onChange({ priority: value })} />
        <NumberField label="Dial timeout seconds" value={server.dial_timeout_seconds} onChange={(value) => onChange({ dial_timeout_seconds: value })} />
        <NumberField label="TCP keepalive seconds" value={server.tcp_keepalive_seconds} onChange={(value) => onChange({ tcp_keepalive_seconds: value })} />
        <NumberField label="Pool idle timeout seconds" value={server.pool_idle_timeout_seconds} onChange={(value) => onChange({ pool_idle_timeout_seconds: value })} />
        <NumberField label="Pool max age seconds" value={server.pool_max_age_seconds} onChange={(value) => onChange({ pool_max_age_seconds: value })} />
        <CheckboxField label="TLS" checked={server.tls} onChange={(value) => onChange({ tls: value })} />
        <CheckboxField label="Pool logging" checked={server.enable_pool_logging} onChange={(value) => onChange({ enable_pool_logging: value })} />
      </div>
    </div>
  )
}

function TextField({
  label,
  value,
  type = 'text',
  required,
  helpText,
  onChange,
}: {
  label: string
  value: string
  type?: string
  required?: boolean
  helpText?: string
  onChange: (value: string) => void
}) {
  return (
    <label className="field">
      <span>{label}</span>
      <input type={type} value={value} required={required} onChange={(event) => onChange(event.target.value)} />
      {helpText ? <small>{helpText}</small> : null}
    </label>
  )
}

function DateField({
  label,
  value,
  required,
  helpText,
  onChange,
}: {
  label: string
  value: string
  required?: boolean
  helpText?: string
  onChange: (value: string) => void
}) {
  return (
    <label className="field">
      <span>{label}</span>
      <input type="date" value={value} required={required} onChange={(event) => onChange(event.target.value)} />
      {helpText ? <small>{helpText}</small> : null}
    </label>
  )
}

function NumberField({
  label,
  value,
  step,
  min,
  max,
  required,
  helpText,
  onChange,
}: {
  label: string
  value: number
  step?: string
  min?: number
  max?: number
  required?: boolean
  helpText?: string
  onChange: (value: number) => void
}) {
  return (
    <label className="field">
      <span>{label}</span>
      <input type="number" step={step} min={min} max={max} required={required} value={value} onChange={(event) => onChange(fieldNumber(event.target.value))} />
      {helpText ? <small>{helpText}</small> : null}
    </label>
  )
}

function CheckboxField({
  label,
  checked,
  helpText,
  onChange,
}: {
  label: string
  checked: boolean
  helpText?: string
  onChange: (value: boolean) => void
}) {
  return (
    <label className="field checkbox-field">
      <span>{label}</span>
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} />
      {helpText ? <small>{helpText}</small> : null}
    </label>
  )
}

function RemoveButton({ locked, onClick }: { locked?: boolean; onClick: () => void }) {
  return (
    <button className="secondary-button" type="button" disabled={locked} onClick={onClick}>
      Remove
    </button>
  )
}
