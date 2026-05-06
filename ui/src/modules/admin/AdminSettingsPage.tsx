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
type SettingsTab = 'downloader' | 'aggregator' | 'indexer'

const stageRows: Array<{ key: StageKey; label: string; concurrency: boolean }> = [
  { key: 'scrape_latest', label: 'Scrape latest', concurrency: false },
  { key: 'scrape_backfill', label: 'Scrape backfill', concurrency: false },
  { key: 'assemble', label: 'Assemble', concurrency: true },
  { key: 'release', label: 'Release', concurrency: false },
  { key: 'inspect_discovery', label: 'Inspect discovery', concurrency: false },
  { key: 'inspect_par2', label: 'Inspect PAR2', concurrency: false },
  { key: 'inspect_nfo', label: 'Inspect NFO', concurrency: false },
  { key: 'inspect_archive', label: 'Inspect archive', concurrency: true },
  { key: 'inspect_password', label: 'Inspect password', concurrency: false },
  { key: 'inspect_media', label: 'Inspect media', concurrency: true },
  { key: 'enrich_predb', label: 'Enrich PreDB', concurrency: false },
  { key: 'enrich_tmdb', label: 'Enrich TMDB', concurrency: false },
]

const settingsTabs: Array<{ key: SettingsTab; label: string }> = [
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
    indexing: {
      newsgroups: [],
      backfill_until_date_by_group: {},
      scrape_latest: stageDefaults(5000),
      scrape_backfill: stageDefaults(5000),
      assemble: stageDefaults(5000, 1),
      release: {
        ...stageDefaults(1000),
        min_confidence: 0.55,
        min_completion_pct: 0,
        require_expected_file_count_for_contextual_obfuscated: true,
      },
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

function stageDefaults(batchSize: number, concurrency = 0): AdminStageConfigPatch {
  return { enabled: false, interval_minutes: 10, batch_size: batchSize, concurrency, backoff_seconds: 0 }
}

function normalizeSettings(input?: RuntimeSettings): RuntimeSettings {
  const defaults = defaultSettings()
  const indexing = (input?.indexing ?? {}) as Partial<IndexingRuntimeSettings>
  return {
    ...defaults,
    ...input,
    servers: input?.servers ?? [],
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
    indexing: {
      ...defaults.indexing!,
      ...indexing,
      newsgroups: indexing.newsgroups ?? [],
      backfill_until_date_by_group: indexing.backfill_until_date_by_group ?? {},
      scrape_latest: { ...defaults.indexing!.scrape_latest, ...indexing.scrape_latest },
      scrape_backfill: { ...defaults.indexing!.scrape_backfill, ...indexing.scrape_backfill },
      assemble: { ...defaults.indexing!.assemble, ...indexing.assemble },
      release: { ...defaults.indexing!.release, ...indexing.release },
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

function serversForSave(servers: ServerRuntimeSettings[], prefix: string) {
  return servers.map((server, index) => ({
    ...server,
    id: server.id?.trim() || `${prefix}-${index + 1}`,
  }))
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
    case 'downloader':
      return {
        downloader_servers: serversForSave(settings.downloader_servers ?? [], 'downloader'),
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
        indexer_servers: serversForSave(settings.indexer_servers ?? [], 'indexer'),
        indexing: settings.indexing ? sanitizeIndexingForSave(settings.indexing) : settings.indexing,
      }
  }
}

export function AdminSettingsPage() {
  const [settings, setSettings] = useState<RuntimeSettings>(defaultSettings())
  const [capabilities, setCapabilities] = useState<ControlPlaneCapabilities | null>(null)
  const [activeTab, setActiveTab] = useState<SettingsTab>('downloader')
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
  const downloaderServers = normalized.downloader_servers ?? []
  const indexerServers = normalized.indexer_servers ?? []
  const indexers = normalized.indexers ?? []
  const arrIntegrations = normalized.arr_integrations ?? []
  const lockDownloaderServers = Boolean(capabilities?.modules.downloader?.ready)
  const lockIndexerServers = Boolean(capabilities?.modules.usenet_indexer?.ready)
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
            title="NNTP servers"
            locked={lockDownloaderServers}
            lockedMessage="Downloader server removal is disabled while the downloader is ready."
            onAdd={() => setSettings((current) => ({ ...current, downloader_servers: [...downloaderServers, serverDefaults(downloaderServers.length)] }))}
          >
            {downloaderServers.map((server, index) => (
              <ServerFields
                key={index}
                title={`Server ${index + 1}`}
                server={server}
                locked={lockDownloaderServers}
                onRemove={() => setSettings((current) => ({ ...current, downloader_servers: downloaderServers.filter((_, i) => i !== index) }))}
                onChange={(patch) => updateDownloaderServer(index, patch)}
              />
            ))}
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
          <SettingsSection
            title="NNTP scrape servers"
            locked={lockIndexerServers}
            lockedMessage="Indexer server removal is disabled while the indexer is ready."
            onAdd={() => setSettings((current) => ({ ...current, indexer_servers: [...indexerServers, serverDefaults(indexerServers.length)] }))}
          >
            {indexerServers.map((server, index) => (
              <ServerFields
                key={index}
                title={`Server ${index + 1}`}
                server={server}
                locked={lockIndexerServers}
                onRemove={() => setSettings((current) => ({ ...current, indexer_servers: indexerServers.filter((_, i) => i !== index) }))}
                onChange={(patch) => updateIndexerServer(index, patch)}
              />
            ))}
          </SettingsSection>

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
              <div className="toolbar-grid" key={index}>
                <TextField
                  label="Newsgroup"
                  value={row.group}
                  required
                  onChange={(value) => updateNewsgroup(index, { group: value })}
                />
                <TextField
                  label="Scrape until"
                  value={row.until}
                  onChange={(value) => updateNewsgroup(index, { until: value })}
                />
                <button
                  className="secondary-button align-end"
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

        <SettingsSection title="Indexer stages">
          <div className="table-shell">
            <table className="data-table">
              <thead>
                <tr>
                  <th>Stage</th>
                  <th>Enabled</th>
                  <th>Interval min</th>
                  <th>Batch</th>
                  <th>Backoff sec</th>
                  <th>Concurrency</th>
                </tr>
              </thead>
              <tbody>
                {stageRows.map((stage) => {
                  const value = indexing[stage.key] as AdminStageConfigPatch
                  return (
                    <tr key={stage.key}>
                      <td>{stage.label}</td>
                      <td><input type="checkbox" checked={Boolean(value.enabled)} onChange={(event) => updateStage(stage.key, { enabled: event.target.checked })} /></td>
                      <td><input type="number" value={value.interval_minutes ?? 0} onChange={(event) => updateStage(stage.key, { interval_minutes: fieldNumber(event.target.value) })} /></td>
                      <td><input type="number" value={value.batch_size ?? 0} onChange={(event) => updateStage(stage.key, { batch_size: fieldNumber(event.target.value) })} /></td>
                      <td><input type="number" value={value.backoff_seconds ?? 0} onChange={(event) => updateStage(stage.key, { backoff_seconds: fieldNumber(event.target.value) })} /></td>
                      <td><input type="number" disabled={!stage.concurrency} value={stage.concurrency ? value.concurrency ?? 0 : 0} onChange={(event) => updateStage(stage.key, { concurrency: fieldNumber(event.target.value) })} /></td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </SettingsSection>

        <SettingsSection title="Release and matching">
          <div className="toolbar-grid">
            <NumberField label="Minimum confidence" step="0.01" value={indexing.release.min_confidence} onChange={(value) => setIndexing({ ...indexing, release: { ...indexing.release, min_confidence: value } })} />
            <NumberField label="Minimum completion %" value={indexing.release.min_completion_pct} onChange={(value) => setIndexing({ ...indexing, release: { ...indexing.release, min_completion_pct: value } })} />
            <NumberField label="High confidence threshold" step="0.01" value={indexing.match.high_confidence_threshold} onChange={(value) => setIndexing({ ...indexing, match: { ...indexing.match, high_confidence_threshold: value } })} />
            <NumberField label="Probable confidence threshold" step="0.01" value={indexing.match.probable_confidence_threshold} onChange={(value) => setIndexing({ ...indexing, match: { ...indexing.match, probable_confidence_threshold: value } })} />
            <NumberField label="Article bucket size" value={indexing.match.article_bucket_size} onChange={(value) => setIndexing({ ...indexing, match: { ...indexing.match, article_bucket_size: value } })} />
            <CheckboxField label="Require expected file count" checked={Boolean(indexing.release.require_expected_file_count_for_contextual_obfuscated)} onChange={(value) => setIndexing({ ...indexing, release: { ...indexing.release, require_expected_file_count_for_contextual_obfuscated: value } })} />
          </div>
        </SettingsSection>

        <SettingsSection title="Inspection tools">
          <div className="toolbar-grid">
            <NumberField label="Max bytes" value={indexing.inspect.max_bytes} onChange={(value) => setIndexing({ ...indexing, inspect: { ...indexing.inspect, max_bytes: value } })} />
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

  function updateDownloaderServer(index: number, patch: Partial<ServerRuntimeSettings>) {
    setSettings((current) => ({ ...current, downloader_servers: downloaderServers.map((item, i) => (i === index ? { ...item, ...patch } : item)) }))
  }

  function updateIndexerServer(index: number, patch: Partial<ServerRuntimeSettings>) {
    setSettings((current) => ({ ...current, indexer_servers: indexerServers.map((item, i) => (i === index ? { ...item, ...patch } : item)) }))
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
  onChange,
}: {
  label: string
  value: string
  type?: string
  required?: boolean
  onChange: (value: string) => void
}) {
  return (
    <label className="field">
      <span>{label}</span>
      <input type={type} value={value} required={required} onChange={(event) => onChange(event.target.value)} />
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
  onChange,
}: {
  label: string
  value: number
  step?: string
  min?: number
  max?: number
  required?: boolean
  onChange: (value: number) => void
}) {
  return (
    <label className="field">
      <span>{label}</span>
      <input type="number" step={step} min={min} max={max} required={required} value={value} onChange={(event) => onChange(fieldNumber(event.target.value))} />
    </label>
  )
}

function CheckboxField({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return (
    <label className="field checkbox-field">
      <span>{label}</span>
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} />
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
