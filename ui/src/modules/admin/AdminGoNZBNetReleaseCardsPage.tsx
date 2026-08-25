import { useEffect, useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  createGoNZBNetTombstone,
  getGoNZBNetNodeCapabilities,
  getGoNZBNetNodeProfile,
  getGoNZBNetReleaseLedger,
  getGoNZBNetTrustPools,
} from '../../shared/api/admin'
import { useAuth } from '../../shared/auth/useAuth'
import { formatDateTime, formatNumber } from '../../shared/lib/format'
import type {
  GoNZBNetNodeCapability,
  GoNZBNetReleaseLedgerItem,
  GoNZBNetReleaseLedgerParams,
  GoNZBNetTrustPool,
} from '../../shared/types'

const pageSize = 100

type ReleaseCardFilters = Pick<GoNZBNetReleaseLedgerParams, 'q' | 'pool_id' | 'node_id' | 'source_kind' | 'state'>

type TombstoneDraft = {
  item: GoNZBNetReleaseLedgerItem
  reason: string
  severity: string
  expires_at: string
  confirmation: string
}

function filtersFromSearchParams(params: URLSearchParams): ReleaseCardFilters {
  return {
    q: params.get('q') ?? '',
    pool_id: params.get('pool_id') ?? '',
    node_id: params.get('node_id') ?? '',
    source_kind: params.get('source_kind') ?? '',
    state: params.get('state') ?? '',
  }
}

function shortID(value?: string) {
  if (!value) return 'n/a'
  if (value.length <= 18) return value
  return `${value.slice(0, 10)}...${value.slice(-6)}`
}

function sourceKindLabel(value: string) {
  switch (value) {
    case 'local_uploader':
      return 'Uploader'
    case 'local_indexer_cache':
      return 'Indexer release'
    case 'local_scan_output':
      return 'Scanner output'
    default:
      return value || 'Unknown'
  }
}

function correctionLink(item: GoNZBNetReleaseLedgerItem) {
  const title = item.signed_title || item.projected_title
  if (item.source_kind === 'local_uploader') {
    return `/uploader?q=${encodeURIComponent(title)}`
  }
  if (item.source_kind === 'local_indexer_cache') {
    return `/admin/indexer/releases?q=${encodeURIComponent(title)}`
  }
  return ''
}

export function AdminGoNZBNetReleaseCardsPage() {
  const { hasPermission } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()
  const [filters, setFilters] = useState<ReleaseCardFilters>(() => filtersFromSearchParams(searchParams))
  const [submittedFilters, setSubmittedFilters] = useState<ReleaseCardFilters>(() => filtersFromSearchParams(searchParams))
  const [items, setItems] = useState<GoNZBNetReleaseLedgerItem[]>([])
  const [nextCursor, setNextCursor] = useState('')
  const [pools, setPools] = useState<GoNZBNetTrustPool[]>([])
  const [nodes, setNodes] = useState<GoNZBNetNodeCapability[]>([])
  const [localNodeID, setLocalNodeID] = useState('')
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [reloadToken, setReloadToken] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [message, setMessage] = useState<string | null>(null)
  const [tombstone, setTombstone] = useState<TombstoneDraft | null>(null)
  const canModerate = hasPermission('gonzbnet.admin.moderation')
  const canCorrectUploader = hasPermission('uploader.submissions.read') && hasPermission('uploader.submissions.review') && hasPermission('uploader.publications.manage')
  const canCorrectIndexer = hasPermission('indexer.releases.read') && hasPermission('indexer.releases.override')

  useEffect(() => {
    let active = true
    void Promise.all([
      getGoNZBNetTrustPools().catch(() => ({ items: [], count: 0 })),
      getGoNZBNetNodeCapabilities().catch(() => ({ items: [], count: 0 })),
      getGoNZBNetNodeProfile().catch(() => null),
    ]).then(([poolResponse, nodeResponse, profileResponse]) => {
      if (!active) return
      setPools(poolResponse.items ?? [])
      setNodes(nodeResponse.items ?? [])
      setLocalNodeID(profileResponse?.node_id ?? '')
    })
    return () => {
      active = false
    }
  }, [])

  useEffect(() => {
    let active = true
    void getGoNZBNetReleaseLedger({ ...submittedFilters, limit: pageSize })
      .then((response) => {
        if (!active) return
        setItems(response.items ?? [])
        setNextCursor(response.next_cursor ?? '')
        setError(null)
      })
      .catch((err) => {
        if (!active) return
        setError(err instanceof Error ? err.message : 'Failed to load ReleaseCards')
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [submittedFilters, reloadToken])

  const nodeAliases = useMemo(() => new Map(nodes.map((node) => [node.node_id, node.alias || shortID(node.node_id)])), [nodes])
  const releaseCount = useMemo(() => new Set(items.map((item) => item.release_id)).size, [items])

  function submitFilters(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const normalized = Object.fromEntries(
      Object.entries(filters).map(([key, value]) => [key, value?.trim() ?? '']),
    ) as ReleaseCardFilters
    const next = new URLSearchParams()
    Object.entries(normalized).forEach(([key, value]) => {
      if (value) next.set(key, value)
    })
    setSearchParams(next, { replace: true })
    setLoading(true)
    setSubmittedFilters(normalized)
    setMessage(null)
  }

  function resetFilters() {
    const empty: ReleaseCardFilters = { q: '', pool_id: '', node_id: '', source_kind: '', state: '' }
    setFilters(empty)
    setLoading(true)
    setSubmittedFilters(empty)
    setSearchParams({}, { replace: true })
    setMessage(null)
  }

  async function loadMore() {
    if (!nextCursor || loadingMore) return
    setLoadingMore(true)
    try {
      const response = await getGoNZBNetReleaseLedger({
        ...submittedFilters,
        cursor: nextCursor,
        limit: pageSize,
      })
      setItems((current) => [...current, ...(response.items ?? [])])
      setNextCursor(response.next_cursor ?? '')
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load more ReleaseCards')
    } finally {
      setLoadingMore(false)
    }
  }

  function beginTombstone(item: GoNZBNetReleaseLedgerItem) {
    setTombstone({ item, reason: '', severity: 'hide', expires_at: '', confirmation: '' })
    setMessage(null)
    setError(null)
  }

  async function submitTombstone(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!tombstone) return
    if (tombstone.confirmation !== 'CONFIRM') {
      setError('Type CONFIRM before signing the ReleaseCard tombstone')
      return
    }
    try {
      const response = await createGoNZBNetTombstone({
        target_type: 'release',
        target_id: tombstone.item.release_id,
        pool_id: tombstone.item.pool_id,
        severity: tombstone.severity,
        reason: tombstone.reason.trim(),
        expires_at: tombstone.expires_at.trim() || undefined,
      })
      setMessage(`ReleaseCard tombstone signed ${shortID(response.event_id)}`)
      setTombstone(null)
      setLoading(true)
      setReloadToken((current) => current + 1)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to tombstone ReleaseCard')
    }
  }

  return (
    <div className="page-section stack">
      <section className="page-card stack">
        <div className="page-header">
          <div>
            <p className="eyebrow">Federated catalog</p>
            <h1 className="page-title">ReleaseCards</h1>
            <p className="muted-copy">This node's observed signed ReleaseCards and source records. Indexer catalog rows remain under Indexer → Releases.</p>
          </div>
          <div className="release-list-summary">
            <strong>{formatNumber(releaseCount)}</strong>
            <span>{formatNumber(items.length)} source records loaded</span>
          </div>
        </div>
        <div className="banner">
          Signed cards are immutable. Correct a locally authored source and let its publisher create a new card, then withdraw or tombstone the old card. Remote cards must be corrected by their author or suppressed by pool governance.
        </div>
        <form className="toolbar-grid" onSubmit={submitFilters}>
          <label className="field">
            <span>Search ReleaseCards</span>
            <input value={filters.q ?? ''} onChange={(event) => setFilters({ ...filters, q: event.target.value })} placeholder="Title, release ID, or manifest ID" />
          </label>
          <label className="field">
            <span>Pool</span>
            <select value={filters.pool_id ?? ''} onChange={(event) => setFilters({ ...filters, pool_id: event.target.value })}>
              <option value="">All pools</option>
              {pools.map((pool) => <option key={pool.pool_id} value={pool.pool_id}>{pool.display_name || pool.pool_id} ({pool.pool_id})</option>)}
            </select>
          </label>
          <label className="field">
            <span>Source node</span>
            <input value={filters.node_id ?? ''} onChange={(event) => setFilters({ ...filters, node_id: event.target.value })} placeholder="node_..." />
          </label>
          <label className="field">
            <span>Origin</span>
            <select value={filters.source_kind ?? ''} onChange={(event) => setFilters({ ...filters, source_kind: event.target.value })}>
              <option value="">All origins</option>
              <option value="local_uploader">Uploader</option>
              <option value="local_indexer_cache">Indexer release</option>
              <option value="local_scan_output">Scanner output</option>
            </select>
          </label>
          <label className="field">
            <span>Effective state</span>
            <select value={filters.state ?? ''} onChange={(event) => setFilters({ ...filters, state: event.target.value })}>
              <option value="">All states</option>
              <option value="active">Active</option>
              <option value="withdrawn">Withdrawn</option>
              <option value="blocked">Blocked</option>
              <option value="revoked">Revoked</option>
              <option value="tombstoned">Tombstoned</option>
              <option value="projection_mismatch">Projection mismatch</option>
            </select>
          </label>
          <div className="button-row align-end">
            <button className="primary-button" type="submit">Apply filters</button>
            <button className="secondary-button" type="button" onClick={resetFilters}>Reset</button>
          </div>
        </form>
        {error ? <div className="banner error">{error}</div> : null}
        {message ? <div className="banner">{message}</div> : null}
      </section>

      <section className="page-card stack">
        {loading ? <div className="banner">Loading ReleaseCards...</div> : null}
        {!loading && items.length === 0 ? <div className="banner">No ReleaseCards match this view.</div> : null}
        {items.length > 0 ? (
          <div className="table-scroll">
            <table className="data-table">
              <thead>
                <tr>
                  <th>ReleaseCard</th>
                  <th>Origin</th>
                  <th>Source node / pool</th>
                  <th>Publication</th>
                  <th>Integrity / moderation</th>
                  <th>Effective</th>
                  <th>Last seen</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {items.map((item) => {
                  const title = item.signed_title || item.projected_title || item.release_id
                  const isLocal = Boolean(localNodeID) && item.source_node_id === localNodeID
                  const canCorrect = (item.source_kind === 'local_uploader' && canCorrectUploader)
                    || (item.source_kind === 'local_indexer_cache' && canCorrectIndexer)
                  const correctURL = isLocal && canCorrect ? correctionLink(item) : ''
                  return (
                    <tr key={`${item.release_id}-${item.source_node_id}-${item.pool_id}`}>
                      <td className="breakable-value" title={item.release_id}>
                        <strong>{title}</strong>
                        <div className="muted-copy mono-cell">{shortID(item.release_id)}</div>
                        <div className="muted-copy mono-cell" title={item.manifest_id}>manifest {shortID(item.manifest_id)}</div>
                        <div className="muted-copy">posted {formatDateTime(item.posted_at)}</div>
                      </td>
                      <td>
                        <span className="status-pill status-pill--table">{sourceKindLabel(item.source_kind)}</span>
                        {isLocal ? <div className="muted-copy">Locally authored</div> : null}
                      </td>
                      <td className="breakable-value">
                        <span title={item.source_node_id}>{nodeAliases.get(item.source_node_id) || shortID(item.source_node_id)}</span>
                        <div className="muted-copy mono-cell">{shortID(item.source_node_id)}</div>
                        <div className="muted-copy">{item.pool_id}</div>
                        <div className="muted-copy">{item.node_status} / {item.membership_status}</div>
                      </td>
                      <td>
                        <span className="status-pill status-pill--table">{item.publication_state}</span>
                        {item.publication_reason ? <div className="muted-copy breakable-value">{item.publication_reason}</div> : null}
                        {item.publication_changed_at ? <div className="muted-copy">{formatDateTime(item.publication_changed_at)}</div> : null}
                      </td>
                      <td>
                        <span className="status-pill status-pill--table">{item.projection_matches_signed_event ? 'verified' : 'mismatch'}</span>
                        <div className="muted-copy">{item.tombstone_severity ? `tombstone: ${item.tombstone_severity}` : 'no tombstone'}</div>
                        <div className="muted-copy mono-cell" title={item.source_event_id}>event {shortID(item.source_event_id)}</div>
                        <div className="muted-copy mono-cell" title={item.source_body_hash}>body {shortID(item.source_body_hash)}</div>
                      </td>
                      <td><span className="status-pill status-pill--table">{item.effective_state}</span></td>
                      <td>{formatDateTime(item.last_seen_at)}</td>
                      <td>
                        <div className="table-actions">
                          {correctURL ? <Link className="secondary-button secondary-button--small" to={correctURL}>Correct &amp; republish</Link> : null}
                          {canModerate && item.effective_state !== 'tombstoned' ? <button className="danger-button danger-button--small" type="button" onClick={() => beginTombstone(item)}>Tombstone</button> : null}
                          {!correctURL && !canModerate ? <span className="muted-copy">Read only</span> : null}
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        ) : null}
        {nextCursor ? <button className="secondary-button" type="button" onClick={() => void loadMore()} disabled={loadingMore}>{loadingMore ? 'Loading...' : 'Load more'}</button> : null}
      </section>

      {tombstone ? (
        <form className="danger-zone stack" onSubmit={submitTombstone}>
          <div>
            <p className="eyebrow">Pool governance</p>
            <h2 className="section-title">Tombstone ReleaseCard</h2>
            <p className="muted-copy">{tombstone.item.signed_title || tombstone.item.projected_title} · {tombstone.item.pool_id}</p>
            <p className="muted-copy mono-cell">{tombstone.item.release_id}</p>
          </div>
          <div className="toolbar-grid">
            <label className="field">
              <span>Severity</span>
              <select value={tombstone.severity} onChange={(event) => setTombstone({ ...tombstone, severity: event.target.value })}>
                <option value="hide">Hide from pool views</option>
                <option value="reject">Reject for the pool</option>
                <option value="local_only">Suppress only on this node</option>
              </select>
            </label>
            <label className="field">
              <span>Expires at (optional RFC3339)</span>
              <input value={tombstone.expires_at} onChange={(event) => setTombstone({ ...tombstone, expires_at: event.target.value })} placeholder="2026-12-31T23:59:59Z" />
            </label>
          </div>
          <label className="field">
            <span>Reason</span>
            <input required value={tombstone.reason} onChange={(event) => setTombstone({ ...tombstone, reason: event.target.value })} />
          </label>
          <label className="field danger-zone__confirmation">
            <span>Type CONFIRM to sign this governance event</span>
            <input autoComplete="off" value={tombstone.confirmation} onChange={(event) => setTombstone({ ...tombstone, confirmation: event.target.value })} />
          </label>
          <div className="button-row">
            <button className="danger-button" type="submit">Sign tombstone</button>
            <button className="secondary-button" type="button" onClick={() => setTombstone(null)}>Cancel</button>
          </div>
        </form>
      ) : null}
    </div>
  )
}
