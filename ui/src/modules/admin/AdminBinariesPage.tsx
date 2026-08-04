import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { getAdminBinaries } from '../../shared/api/admin'
import { formatDateTime, formatNumber, formatPercent } from '../../shared/lib/format'
import type { AdminBinaryListParams, AdminBinaryListResponse } from '../../shared/types'

const defaultFilters: AdminBinaryListParams = {
  q: '',
  newsgroup: '',
  identity_strength: '',
  readiness_bucket: '',
  match_status: '',
  release_state: '',
  sort: 'updated_desc',
  limit: 100,
  offset: 0,
}

function label(value: string | undefined, fallback = 'unknown') {
  return value && value.trim() ? value : fallback
}

function completionLabel(item: { observed_parts: number; total_parts: number; completion_pct?: number }) {
  if (!item.total_parts) return `${formatNumber(item.observed_parts)} observed`
  const pct = item.completion_pct ?? Math.min(100, (item.observed_parts / item.total_parts) * 100)
  return `${item.observed_parts.toLocaleString()} / ${item.total_parts.toLocaleString()} (${formatPercent(pct)})`
}

const sortByColumn: Record<string, { asc: string; desc: string }> = {
  binary: { asc: 'updated_asc', desc: 'updated_desc' },
  parts: { asc: 'parts_asc', desc: 'parts_desc' },
  completion: { asc: 'completion_asc', desc: 'completion_desc' },
}

export function AdminBinariesPage() {
  const [filters, setFilters] = useState<AdminBinaryListParams>(defaultFilters)
  const [submittedFilters, setSubmittedFilters] = useState<AdminBinaryListParams>(defaultFilters)
  const [data, setData] = useState<AdminBinaryListResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    const timer = window.setTimeout(() => {
      setLoading(true)
      void getAdminBinaries(submittedFilters)
        .then((response) => {
          if (cancelled) return
          setData(response)
          setError(null)
        })
        .catch((err) => {
          if (cancelled) return
          setError(err instanceof Error ? err.message : 'Failed to load binaries')
        })
        .finally(() => {
          if (!cancelled) setLoading(false)
        })
    }, 0)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [submittedFilters])

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmittedFilters({ ...filters, offset: 0 })
  }

  function handlePage(nextOffset: number) {
    const next = { ...submittedFilters, offset: Math.max(0, nextOffset) }
    setFilters(next)
    setSubmittedFilters(next)
  }

  function setSort(sort: string) {
    const next = { ...submittedFilters, sort, offset: 0 }
    setFilters(next)
    setSubmittedFilters(next)
  }

  function toggleColumnSort(column: keyof typeof sortByColumn) {
    const options = sortByColumn[column]
    setSort(submittedFilters.sort === options.desc ? options.asc : options.desc)
  }

  function sortMarker(column: keyof typeof sortByColumn) {
    const options = sortByColumn[column]
    if (submittedFilters.sort === options.asc) return ' ↑'
    if (submittedFilters.sort === options.desc) return ' ↓'
    return ''
  }

  const rows = data?.items ?? []

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <div className="page-header">
          <div>
            <p className="eyebrow">Admin Binary Workbench</p>
            <h1 className="page-title">Binary diagnostics</h1>
            <p className="muted-copy">Inspect weak, incomplete, recovered, and unformed binaries with article and recovery evidence.</p>
          </div>
        </div>
        <form className="release-table-search binary-filter-grid" onSubmit={handleSubmit}>
          <input className="table-input" placeholder="Binary id, file, release, key" value={filters.q ?? ''} onChange={(event) => setFilters({ ...filters, q: event.target.value })} />
          <input className="table-input" placeholder="Newsgroup" value={filters.newsgroup ?? ''} onChange={(event) => setFilters({ ...filters, newsgroup: event.target.value })} />
          <select className="table-input" value={filters.identity_strength ?? ''} onChange={(event) => setFilters({ ...filters, identity_strength: event.target.value })}>
            <option value="">Any strength</option>
            <option value="weak">weak</option>
            <option value="provisional">provisional</option>
            <option value="strong">strong</option>
          </select>
          <select className="table-input" value={filters.match_status ?? ''} onChange={(event) => setFilters({ ...filters, match_status: event.target.value })}>
            <option value="">Any match</option>
            <option value="low_confidence">low confidence</option>
            <option value="probable">probable</option>
            <option value="strong">strong</option>
          </select>
          <select className="table-input" value={filters.readiness_bucket ?? ''} onChange={(event) => setFilters({ ...filters, readiness_bucket: event.target.value })}>
            <option value="">Any readiness</option>
            <option value="weak_single_binary">weak single binary</option>
            <option value="weak_obfuscated_set">weak obfuscated set</option>
            <option value="low_coverage">low coverage</option>
            <option value="release_ready">release ready</option>
          </select>
          <select className="table-input" value={filters.release_state ?? ''} onChange={(event) => setFilters({ ...filters, release_state: event.target.value })}>
            <option value="">Any release state</option>
            <option value="unformed">unformed</option>
            <option value="formed">formed</option>
          </select>
          <select className="table-input" value={filters.sort ?? 'updated_desc'} onChange={(event) => setFilters({ ...filters, sort: event.target.value })}>
            <option value="updated_desc">Updated desc</option>
            <option value="updated_asc">Updated asc</option>
            <option value="completion_asc">Completion asc</option>
            <option value="completion_desc">Completion desc</option>
            <option value="parts_desc">Parts desc</option>
            <option value="parts_asc">Parts asc</option>
          </select>
          <button className="primary-button" type="submit">Apply</button>
        </form>
        {error ? <div className="banner error">{error}</div> : null}
        {loading && !data ? <div className="banner"><span className="loading-dot" /> Loading binaries...</div> : null}
      </div>

      <div className="page-card stack">
        <div className="release-table-toolbar">
          <span className="muted-copy">
            {loading ? 'Refreshing binaries...' : `Showing ${rows.length.toLocaleString()} of ${(data?.total ?? 0).toLocaleString()}`}
          </span>
          <div className="button-row">
            <button className="secondary-button" type="button" disabled={(data?.offset ?? 0) <= 0} onClick={() => handlePage((data?.offset ?? 0) - (data?.limit ?? 100))}>Previous</button>
            <button className="secondary-button" type="button" disabled={!data?.has_more} onClick={() => handlePage((data?.offset ?? 0) + (data?.limit ?? 100))}>Next</button>
          </div>
        </div>
        <div className="table-shell">
          <table className="data-table data-table--compact">
            <thead>
              <tr>
                <th><button className="table-sort-button" type="button" onClick={() => toggleColumnSort('binary')}>Binary{sortMarker('binary')}</button></th>
                <th>Group</th>
                <th><button className="table-sort-button" type="button" onClick={() => toggleColumnSort('parts')}>Parts{sortMarker('parts')}</button></th>
                <th>Identity</th>
                <th>Recovery</th>
                <th>Release</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => (
                <tr key={item.binary_id}>
                  <td>
                    <Link className="table-link" to={`/admin/indexer/binaries/${item.binary_id}`}>
                      {item.file_name || item.binary_name || item.release_name || item.binary_id}
                    </Link>
                    <div className="muted-copy">{item.binary_id} · {formatDateTime(item.posted_at)}</div>
                  </td>
                  <td>{item.group_name}</td>
                  <td>{completionLabel(item)}</td>
                  <td>
                    <span className="status-pill status-pill--table">{label(item.identity_strength)}</span>
                    <div className="muted-copy">{label(item.family_kind)} · {formatPercent(item.match_confidence * 100)}</div>
                  </td>
                  <td>
                    <span>{label(item.yenc_status, 'none')}</span>
                    <div className="muted-copy">{label(item.recovered_source, 'not recovered')}</div>
                  </td>
                  <td>
                    {item.release_id ? <Link className="table-link" to={`/admin/indexer/releases/${item.release_id}`}>{item.release_title || item.release_id}</Link> : <span className="muted-copy">Unformed</span>}
                  </td>
                </tr>
              ))}
              {!loading && rows.length === 0 ? (
                <tr><td colSpan={6}>No binaries match the current filters.</td></tr>
              ) : null}
              {loading && data ? (
                <tr><td colSpan={6}>Refreshing...</td></tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
