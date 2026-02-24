import { useEffect, useMemo, useState } from 'react'
import type { SyntheticEvent } from 'react'
import {
  cancelQueueItem,
  cancelQueueItems,
  clearQueueHistory,
  connectQueueEvents,
  deleteQueueItems,
  enqueueByReleaseId,
  enqueueByUpload,
  fetchHistory,
  fetchQueue,
  fetchQueueItemEvents,
  fetchQueueItemFiles,
  searchReleases,
  type QueueEventStats,
} from './api/queue'
import type { QueueEvent, QueueFile, QueueItem, ReleaseSearchItem } from './types/queue'
import { formatBytes, formatDate } from './lib/format'

type StatusFilter = '' | 'pending' | 'downloading' | 'processing' | 'completed' | 'failed'

function App() {
  const [baseUrl, setBaseUrl] = useState(import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080')
  const [apiKey, setApiKey] = useState(import.meta.env.VITE_API_KEY ?? '')

  const [active, setActive] = useState<QueueItem[]>([])
  const [history, setHistory] = useState<QueueItem[]>([])
  const [historyStatus, setHistoryStatus] = useState<StatusFilter>('')
  const [historyLimit, setHistoryLimit] = useState(25)

  const [selectedActiveIDs, setSelectedActiveIDs] = useState<string[]>([])
  const [selectedHistoryIDs, setSelectedHistoryIDs] = useState<string[]>([])

  const [releaseId, setReleaseId] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<ReleaseSearchItem[]>([])
  const [searching, setSearching] = useState(false)

  const [selectedItem, setSelectedItem] = useState<QueueItem | null>(null)
  const [selectedItemFiles, setSelectedItemFiles] = useState<QueueFile[]>([])
  const [selectedItemEvents, setSelectedItemEvents] = useState<QueueEvent[]>([])

  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [eventStats, setEventStats] = useState<QueueEventStats | null>(null)

  const config = useMemo(() => ({ baseUrl, apiKey }), [baseUrl, apiKey])

  async function refresh() {
    setLoading(true)
    setError(null)
    try {
      const [queueResp, historyResp] = await Promise.all([
        fetchQueue(config),
        fetchHistory(config, { limit: historyLimit, status: historyStatus }),
      ])
      setActive(queueResp.items)
      setHistory(historyResp.items)
      setSelectedActiveIDs((prev) => prev.filter((id) => queueResp.items.some((item) => item.id === id)))
      setSelectedHistoryIDs((prev) => prev.filter((id) => historyResp.items.some((item) => item.id === id)))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed')
    } finally {
      setLoading(false)
    }
  }

  async function loadDetails(item: QueueItem) {
    setSelectedItem(item)
    try {
      const [files, events] = await Promise.all([
        fetchQueueItemFiles(config, item.id),
        fetchQueueItemEvents(config, item.id),
      ])
      setSelectedItemFiles(files)
      setSelectedItemEvents(events)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load item details')
    }
  }

  useEffect(() => {
    void refresh()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [config, historyLimit, historyStatus])

  useEffect(() => {
    const source = connectQueueEvents(config, {
      onStats: (stats) => {
        setEventStats(stats)
        if (!stats.active_item) return

        let found = false
        setActive((prev) =>
          prev.map((item) => {
            if (item.id !== stats.active_item?.id) return item
            found = true
            return {
              ...item,
              status:
                item.status === 'failed' || item.status === 'completed'
                  ? item.status
                  : (stats.active_item.status as QueueItem['status']),
              progress: {
                bytes_written: stats.active_item.bytes,
              },
              release: item.release
                ? {
                    ...item.release,
                    title: stats.active_item.title || item.release.title,
                    size: stats.active_item.size || item.release.size,
                  }
                : undefined,
            }
          }),
        )
        if (!found) {
          void refresh()
        }
      },
    })
    return () => source.close()
  }, [config])

  async function onAddByReleaseId(e: SyntheticEvent<HTMLFormElement>) {
    e.preventDefault()
    setError(null)
    try {
      await enqueueByReleaseId(config, { release_id: releaseId })
      setReleaseId('')
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add queue item')
    }
  }

  async function onAddByUpload(e: SyntheticEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!file) {
      setError('Select an NZB file first')
      return
    }
    setError(null)
    try {
      await enqueueByUpload(config, file)
      setFile(null)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to upload NZB')
    }
  }

  async function onCancel(id: string) {
    setError(null)
    try {
      await cancelQueueItem(config, id)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to cancel queue item')
    }
  }

  async function onSearchReleases(e: SyntheticEvent<HTMLFormElement>) {
    e.preventDefault()
    setError(null)
    setSearching(true)
    try {
      const items = await searchReleases(config, searchQuery)
      setSearchResults(items)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to search releases')
    } finally {
      setSearching(false)
    }
  }

  async function onQueueFromSearch(id: string) {
    setError(null)
    try {
      await enqueueByReleaseId(config, { release_id: id })
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to queue release')
    }
  }

  async function onBulkCancel() {
    if (selectedActiveIDs.length === 0) return
    setError(null)
    try {
      await cancelQueueItems(config, selectedActiveIDs)
      setSelectedActiveIDs([])
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to cancel selected')
    }
  }

  async function onBulkDelete() {
    const ids = [...selectedActiveIDs, ...selectedHistoryIDs]
    if (ids.length === 0) return
    setError(null)
    try {
      await deleteQueueItems(config, ids)
      setSelectedActiveIDs([])
      setSelectedHistoryIDs([])
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete selected')
    }
  }

  async function onClearHistory() {
    setError(null)
    try {
      await clearQueueHistory(config)
      setSelectedHistoryIDs([])
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to clear history')
    }
  }

  return (
    <main className="min-h-screen bg-neutral-950 text-neutral-100">
      <div className="mx-auto max-w-7xl px-4 py-8">
        <header className="mb-8">
          <h1 className="text-3xl font-semibold tracking-tight">GoNZB Queue UI</h1>
          <p className="mt-2 text-neutral-400">Queue, history, details, and bulk actions.</p>
        </header>

        <section className="mb-6 grid gap-3 rounded border border-neutral-800 bg-neutral-900 p-4 md:grid-cols-2">
          <label className="text-sm">
            <div className="mb-1 text-neutral-300">API Base URL</div>
            <input
              className="w-full rounded border border-neutral-700 bg-neutral-950 px-3 py-2"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              placeholder="http://localhost:8080"
            />
          </label>
          <label className="text-sm">
            <div className="mb-1 text-neutral-300">API Key (optional)</div>
            <input
              className="w-full rounded border border-neutral-700 bg-neutral-950 px-3 py-2"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder="X-API-Key"
            />
          </label>
        </section>

        <section className="mb-6 grid gap-3 rounded border border-neutral-800 bg-neutral-900 p-4 md:grid-cols-3">
          <div className="rounded border border-neutral-800 bg-neutral-950 p-3">
            <div className="text-sm text-neutral-400">Active Jobs</div>
            <div className="mt-2 text-2xl font-semibold">{eventStats?.active_jobs ?? active.length}</div>
          </div>
          <div className="rounded border border-neutral-800 bg-neutral-950 p-3">
            <div className="text-sm text-neutral-400">Current Speed</div>
            <div className="mt-2 text-2xl font-semibold">{formatBytes(eventStats?.bps ?? 0)}/s</div>
          </div>
          <div className="rounded border border-neutral-800 bg-neutral-950 p-3">
            <div className="text-sm text-neutral-400">Current Progress</div>
            <div className="mt-2 text-2xl font-semibold">{(eventStats?.progress ?? 0).toFixed(1)}%</div>
          </div>
        </section>

        <section className="mb-4 flex flex-wrap gap-2">
          <button
            className="rounded border border-amber-500 px-3 py-2 text-amber-300 hover:bg-amber-950"
            onClick={() => void onBulkCancel()}
          >
            Cancel Selected ({selectedActiveIDs.length})
          </button>
          <button
            className="rounded border border-red-500 px-3 py-2 text-red-300 hover:bg-red-950"
            onClick={() => void onBulkDelete()}
          >
            Delete Selected ({selectedActiveIDs.length + selectedHistoryIDs.length})
          </button>
          <button
            className="rounded border border-red-500 px-3 py-2 text-red-300 hover:bg-red-950"
            onClick={() => void onClearHistory()}
          >
            Delete History
          </button>
        </section>

        {error ? (
          <div className="mb-6 rounded border border-red-600 bg-red-950 px-3 py-2 text-red-200">{error}</div>
        ) : null}

        <section className="mb-8 grid gap-4 md:grid-cols-2">
          <form onSubmit={onAddByReleaseId} className="rounded border border-neutral-800 bg-neutral-900 p-4">
            <h2 className="mb-3 text-lg font-medium">Add by Release ID</h2>
            <div className="mb-3">
              <label className="mb-1 block text-sm text-neutral-300">Release ID</label>
              <input
                className="w-full rounded border border-neutral-700 bg-neutral-950 px-3 py-2"
                value={releaseId}
                onChange={(e) => setReleaseId(e.target.value)}
                required
              />
            </div>
            <button className="rounded bg-sky-500 px-3 py-2 font-medium text-neutral-950 hover:bg-sky-400">
              Queue Release
            </button>
          </form>

          <form onSubmit={onAddByUpload} className="rounded border border-neutral-800 bg-neutral-900 p-4">
            <h2 className="mb-3 text-lg font-medium">Add by NZB Upload</h2>
            <div className="mb-4">
              <label className="mb-1 block text-sm text-neutral-300">NZB File</label>
              <input
                type="file"
                accept=".nzb,application/xml,text/xml"
                onChange={(e) => setFile(e.currentTarget.files?.[0] ?? null)}
                className="block w-full text-sm text-neutral-300"
              />
            </div>
            <button className="rounded bg-emerald-500 px-3 py-2 font-medium text-neutral-950 hover:bg-emerald-400">
              Upload & Queue
            </button>
          </form>
        </section>

        <section className="mb-8 rounded border border-neutral-800 bg-neutral-900 p-4">
          <h2 className="mb-3 text-lg font-medium">Search Local Store</h2>
          <form onSubmit={onSearchReleases} className="mb-4 flex gap-2">
            <input
              className="w-full rounded border border-neutral-700 bg-neutral-950 px-3 py-2"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search cached releases by title"
              minLength={2}
              required
            />
            <button className="rounded bg-amber-400 px-3 py-2 font-medium text-neutral-950 hover:bg-amber-300">
              {searching ? 'Searching...' : 'Search'}
            </button>
          </form>
          <div className="overflow-x-auto rounded border border-neutral-800">
            <table className="min-w-full bg-neutral-950 text-sm">
              <thead className="bg-neutral-800 text-left text-neutral-300">
                <tr>
                  <th className="px-3 py-2">Title</th>
                  <th className="px-3 py-2">Size</th>
                  <th className="px-3 py-2">Cache</th>
                  <th className="px-3 py-2">Source</th>
                  <th className="px-3 py-2">Category</th>
                  <th className="px-3 py-2">Action</th>
                </tr>
              </thead>
              <tbody>
                {searchResults.map((item) => (
                  <tr key={item.id} className="border-t border-neutral-800">
                    <td className="px-3 py-2">{item.title}</td>
                    <td className="px-3 py-2">{formatBytes(item.size)}</td>
                    <td className="px-3 py-2">
                      {item.cache_present ? (
                        <span className="rounded border border-emerald-500 px-2 py-1 text-xs text-emerald-300">
                          Cached ({formatBytes(item.cache_blob_size)})
                        </span>
                      ) : (
                        <span className="rounded border border-neutral-600 px-2 py-1 text-xs text-neutral-300">
                          Metadata Only
                        </span>
                      )}
                    </td>
                    <td className="px-3 py-2">{item.source || '-'}</td>
                    <td className="px-3 py-2">{item.category || '-'}</td>
                    <td className="px-3 py-2">
                      <button
                        className="rounded border border-sky-500 px-2 py-1 text-sky-300 hover:bg-sky-950"
                        onClick={() => void onQueueFromSearch(item.id)}
                      >
                        Queue
                      </button>
                    </td>
                  </tr>
                ))}
                {searchResults.length === 0 ? (
                  <tr>
                    <td className="px-3 py-3 text-neutral-500" colSpan={6}>
                      No results
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </section>

        <section className="mb-8">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-xl font-semibold">Active Queue</h2>
            <button
              className="rounded border border-neutral-700 px-3 py-1 text-sm hover:bg-neutral-800"
              onClick={() => void refresh()}
            >
              {loading ? 'Refreshing...' : 'Refresh'}
            </button>
          </div>
          <QueueTable
            items={active}
            selectedIDs={selectedActiveIDs}
            onToggleSelect={(id, checked) => setSelectedActiveIDs((prev) => updateSelection(prev, id, checked))}
            onCancel={onCancel}
            onOpenDetails={loadDetails}
            showCancel
          />
        </section>

        <section>
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <h2 className="mr-2 text-xl font-semibold">Queue History</h2>
            <select
              className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm"
              value={historyStatus}
              onChange={(e) => setHistoryStatus(e.target.value as StatusFilter)}
            >
              <option value="">All statuses</option>
              <option value="completed">Completed</option>
              <option value="failed">Failed</option>
              <option value="pending">Pending</option>
              <option value="downloading">Downloading</option>
              <option value="processing">Processing</option>
            </select>
            <select
              className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm"
              value={historyLimit}
              onChange={(e) => setHistoryLimit(Number(e.target.value))}
            >
              <option value={10}>10</option>
              <option value={25}>25</option>
              <option value={50}>50</option>
              <option value={100}>100</option>
            </select>
          </div>
          <QueueTable
            items={history}
            selectedIDs={selectedHistoryIDs}
            onToggleSelect={(id, checked) => setSelectedHistoryIDs((prev) => updateSelection(prev, id, checked))}
            onCancel={onCancel}
            onOpenDetails={loadDetails}
            showHistory
          />
        </section>

        {selectedItem ? (
          <section className="mt-8 rounded border border-neutral-800 bg-neutral-900 p-4">
            <div className="mb-4 flex items-center justify-between">
              <h2 className="text-xl font-semibold">Queue Item Details</h2>
              <button
                className="rounded border border-neutral-700 px-2 py-1 text-sm hover:bg-neutral-800"
                onClick={() => setSelectedItem(null)}
              >
                Close
              </button>
            </div>
            <div className="mb-4 text-sm text-neutral-300">
              <div>ID: {selectedItem.id}</div>
              <div>Release: {selectedItem.release?.title ?? selectedItem.release_id}</div>
              <div>Status: {selectedItem.status}</div>
              <div>Started: {formatDate(selectedItem.started_at)}</div>
              <div>Completed: {formatDate(selectedItem.completed_at)}</div>
            </div>
            <div className="grid gap-4 md:grid-cols-2">
              <div>
                <h3 className="mb-2 font-medium">Files</h3>
                <div className="max-h-80 overflow-auto rounded border border-neutral-800">
                  <table className="min-w-full bg-neutral-950 text-xs">
                    <thead className="bg-neutral-800 text-left text-neutral-300">
                      <tr>
                        <th className="px-2 py-1">Name</th>
                        <th className="px-2 py-1">Size</th>
                        <th className="px-2 py-1">PAR2</th>
                      </tr>
                    </thead>
                    <tbody>
                      {selectedItemFiles.map((f) => (
                        <tr key={f.id} className="border-t border-neutral-800">
                          <td className="px-2 py-1">{f.filename}</td>
                          <td className="px-2 py-1">{formatBytes(f.size)}</td>
                          <td className="px-2 py-1">{f.is_pars ? 'yes' : 'no'}</td>
                        </tr>
                      ))}
                      {selectedItemFiles.length === 0 ? (
                        <tr>
                          <td className="px-2 py-2 text-neutral-500" colSpan={3}>
                            No files
                          </td>
                        </tr>
                      ) : null}
                    </tbody>
                  </table>
                </div>
              </div>
              <div>
                <h3 className="mb-2 font-medium">Stages</h3>
                <div className="max-h-80 overflow-auto rounded border border-neutral-800 bg-neutral-950 p-2 text-xs">
                  {selectedItemEvents.map((ev) => (
                    <div key={ev.id} className="mb-2 border-b border-neutral-800 pb-2">
                      <div className="font-medium text-neutral-200">
                        {ev.stage} / {ev.status}
                      </div>
                      <div className="text-neutral-400">{ev.message}</div>
                      <div className="text-neutral-500">{formatDate(ev.created_at)}</div>
                    </div>
                  ))}
                  {selectedItemEvents.length === 0 ? <div className="text-neutral-500">No events</div> : null}
                </div>
              </div>
            </div>
          </section>
        ) : null}
      </div>
    </main>
  )
}

function updateSelection(current: string[], id: string, checked: boolean): string[] {
  if (checked) {
    if (current.includes(id)) return current
    return [...current, id]
  }
  return current.filter((v) => v !== id)
}

function QueueTable({
  items,
  selectedIDs,
  onToggleSelect,
  onCancel,
  onOpenDetails,
  showCancel = false,
  showHistory = false,
}: {
  items: QueueItem[]
  selectedIDs: string[]
  onToggleSelect: (id: string, checked: boolean) => void
  onCancel: (id: string) => Promise<void>
  onOpenDetails: (item: QueueItem) => void
  showCancel?: boolean
  showHistory?: boolean
}) {
  return (
    <div className="overflow-x-auto rounded border border-neutral-800">
      <table className="min-w-full bg-neutral-900 text-sm">
        <thead className="bg-neutral-800 text-left text-neutral-300">
          <tr>
            <th className="px-3 py-2">Sel</th>
            <th className="px-3 py-2">Status</th>
            <th className="px-3 py-2">Title</th>
            <th className="px-3 py-2">Progress</th>
            {showHistory ? <th className="px-3 py-2">Avg Speed</th> : null}
            {showHistory ? <th className="px-3 py-2">Duration</th> : null}
            <th className="px-3 py-2">Details</th>
            <th className="px-3 py-2">{showHistory ? 'Completed' : 'Created'}</th>
            <th className="px-3 py-2">Error</th>
            {showCancel ? <th className="px-3 py-2">Action</th> : null}
          </tr>
        </thead>
        <tbody>
          {items.map((item) => {
            const size = item.release?.size ?? 0
            const written = item.progress.bytes_written
            const pct = size > 0 ? (written / size) * 100 : 0

            return (
              <tr key={item.id} className="border-t border-neutral-800">
                <td className="px-3 py-2">
                  <input
                    type="checkbox"
                    checked={selectedIDs.includes(item.id)}
                    onChange={(e) => onToggleSelect(item.id, e.target.checked)}
                  />
                </td>
                <td className="px-3 py-2">{item.status}</td>
                <td className="px-3 py-2">{item.release?.title ?? item.release_id}</td>
                <td className="px-3 py-2">
                  {formatBytes(written)} / {formatBytes(size)} ({pct.toFixed(1)}%)
                </td>
                {showHistory ? (
                  <td className="px-3 py-2">
                    {item.metrics.avg_bps > 0 ? `${formatBytes(item.metrics.avg_bps)}/s` : '-'}
                  </td>
                ) : null}
                {showHistory ? (
                  <td className="px-3 py-2">
                    {item.metrics.download_seconds > 0
                      ? `${item.metrics.download_seconds}s + ${item.metrics.postprocess_seconds}s`
                      : '-'}
                  </td>
                ) : null}
                <td className="px-3 py-2">
                  <button
                    className="rounded border border-neutral-700 px-2 py-1 text-xs hover:bg-neutral-800"
                    onClick={() => onOpenDetails(item)}
                  >
                    View
                  </button>
                </td>
                <td className="px-3 py-2">
                  {showHistory ? formatDate(item.completed_at) : formatDate(item.created_at)}
                </td>
                <td className="px-3 py-2 text-red-300">{item.error ?? '-'}</td>
                {showCancel ? (
                  <td className="px-3 py-2">
                    <button
                      className="rounded border border-red-500 px-2 py-1 text-red-300 hover:bg-red-950"
                      onClick={() => void onCancel(item.id)}
                    >
                      Cancel
                    </button>
                  </td>
                ) : null}
              </tr>
            )
          })}
          {items.length === 0 ? (
            <tr>
              <td
                className="px-3 py-4 text-neutral-500"
                colSpan={(showCancel ? 1 : 0) + (showHistory ? 9 : 7)}
              >
                No items
              </td>
            </tr>
          ) : null}
        </tbody>
      </table>
    </div>
  )
}

export default App
