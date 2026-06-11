import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { getAdminReleases } from '../../shared/api/admin'
import { formatBytes, formatDateTime } from '../../shared/lib/format'
import type { AdminReleaseListParams, AdminReleaseListResponse } from '../../shared/types'

const defaultFilters: AdminReleaseListParams = {
  q: '',
  newsgroup: '',
  sort: 'posted_desc',
  category_id: '',
  classification: '',
  external_media_type: '',
  identity_status: '',
  password_state: '',
  media_quality_tier: '',
  hidden: '',
  public_state: '',
  inspected: '',
  enriched: '',
  uncategorized: '',
  password_candidates: '',
  metadata_mismatch: '',
  low_confidence: '',
  completion_state: '',
  has_nfo: '',
  has_par2: '',
  limit: 100,
  offset: 0,
}

function formatNZBStatus(value: string) {
  switch (value) {
    case 'legacy_pending':
    case 'pending':
      return 'NZB pending'
    case 'legacy_ready':
    case 'ready':
      return 'NZB ready'
    case 'legacy_failed':
    case 'failed':
      return 'NZB failed'
    case 'archived':
      return 'Archived'
    case 'purge_pending':
      return 'Archived, purge pending'
    case 'purged':
      return 'Archived, sources purged'
    default:
      return value || 'NZB pending'
  }
}

export function AdminReleasesPage() {
  const [filters, setFilters] = useState<AdminReleaseListParams>(defaultFilters)
  const [submittedFilters, setSubmittedFilters] = useState<AdminReleaseListParams>(defaultFilters)
  const [data, setData] = useState<AdminReleaseListResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getAdminReleases(submittedFilters)
      .then((response) => {
        setData(response)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load admin releases'))
  }, [submittedFilters])

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmittedFilters({ ...filters, offset: 0 })
  }

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <p className="eyebrow">Release Moderation</p>
        <h1 className="page-title">Curate release overrides without mutating generated rows.</h1>
        <form className="toolbar-grid" onSubmit={handleSubmit}>
          <label className="field">
            <span>Search Releases</span>
            <input value={filters.q ?? ''} onChange={(event) => setFilters((current) => ({ ...current, q: event.target.value }))} />
          </label>
          <label className="field">
            <span>Newsgroup</span>
            <input
              value={filters.newsgroup ?? ''}
              onChange={(event) => setFilters((current) => ({ ...current, newsgroup: event.target.value }))}
              placeholder="alt.binaries.wood"
            />
          </label>
          <label className="field">
            <span>Sort</span>
            <select value={filters.sort ?? 'posted_desc'} onChange={(event) => setFilters((current) => ({ ...current, sort: event.target.value }))}>
              <option value="posted_desc">Newest Posted</option>
              <option value="posted_asc">Oldest Posted</option>
              <option value="updated_desc">Recently Updated</option>
              <option value="quality_desc">Best Quality</option>
              <option value="completion_desc">Best Completion</option>
              <option value="size_desc">Largest</option>
              <option value="size_asc">Smallest</option>
              <option value="title_asc">Title</option>
            </select>
          </label>
          <label className="field">
            <span>Category ID</span>
            <input value={filters.category_id ?? ''} onChange={(event) => setFilters((current) => ({ ...current, category_id: event.target.value }))} placeholder="2040" />
          </label>
          <label className="field">
            <span>Classification</span>
            <select value={filters.classification ?? ''} onChange={(event) => setFilters((current) => ({ ...current, classification: event.target.value }))}>
              <option value="">Any</option>
              <option value="video">video</option>
              <option value="video_archive">video_archive</option>
              <option value="tv">tv</option>
              <option value="movie">movie</option>
              <option value="audio">audio</option>
              <option value="ebook">ebook</option>
              <option value="archive">archive</option>
              <option value="misc">misc</option>
            </select>
          </label>
          <label className="field">
            <span>Media Type</span>
            <select value={filters.external_media_type ?? ''} onChange={(event) => setFilters((current) => ({ ...current, external_media_type: event.target.value }))}>
              <option value="">Any</option>
              <option value="movie">movie</option>
              <option value="tv">tv</option>
              <option value="audio">audio</option>
            </select>
          </label>
          <label className="field">
            <span>Identity</span>
            <select value={filters.identity_status ?? ''} onChange={(event) => setFilters((current) => ({ ...current, identity_status: event.target.value }))}>
              <option value="">Any</option>
              <option value="identified">identified</option>
              <option value="probable">probable</option>
              <option value="unknown">unknown</option>
            </select>
          </label>
          <label className="field">
            <span>Password</span>
            <select value={filters.password_state ?? ''} onChange={(event) => setFilters((current) => ({ ...current, password_state: event.target.value }))}>
              <option value="">Any</option>
              <option value="not_passworded">not_passworded</option>
              <option value="passworded_known">passworded_known</option>
              <option value="passworded_unknown">passworded_unknown</option>
            </select>
          </label>
          <label className="field">
            <span>Quality</span>
            <select value={filters.media_quality_tier ?? ''} onChange={(event) => setFilters((current) => ({ ...current, media_quality_tier: event.target.value }))}>
              <option value="">Any</option>
              <option value="premium">premium</option>
              <option value="good">good</option>
              <option value="fair">fair</option>
              <option value="unknown">unknown</option>
            </select>
          </label>
          <label className="field">
            <span>Override</span>
            <select value={filters.hidden ?? ''} onChange={(event) => setFilters((current) => ({ ...current, hidden: event.target.value }))}>
              <option value="">Any</option>
              <option value="visible">visible</option>
              <option value="hidden">hidden</option>
            </select>
          </label>
          <label className="field">
            <span>Public State</span>
            <select value={filters.public_state ?? ''} onChange={(event) => setFilters((current) => ({ ...current, public_state: event.target.value }))}>
              <option value="">Any</option>
              <option value="public">public</option>
              <option value="internal_only">internal only</option>
              <option value="hidden">hidden override</option>
            </select>
          </label>
          <label className="field">
            <span>Inspected</span>
            <select value={filters.inspected ?? ''} onChange={(event) => setFilters((current) => ({ ...current, inspected: event.target.value }))}>
              <option value="">Any</option>
              <option value="yes">yes</option>
              <option value="no">no</option>
            </select>
          </label>
          <label className="field">
            <span>Enriched</span>
            <select value={filters.enriched ?? ''} onChange={(event) => setFilters((current) => ({ ...current, enriched: event.target.value }))}>
              <option value="">Any</option>
              <option value="yes">yes</option>
              <option value="no">no</option>
            </select>
          </label>
          <label className="field">
            <span>Uncategorized</span>
            <select value={filters.uncategorized ?? ''} onChange={(event) => setFilters((current) => ({ ...current, uncategorized: event.target.value }))}>
              <option value="">Any</option>
              <option value="yes">yes</option>
              <option value="no">no</option>
            </select>
          </label>
          <label className="field">
            <span>Password Candidates</span>
            <select value={filters.password_candidates ?? ''} onChange={(event) => setFilters((current) => ({ ...current, password_candidates: event.target.value }))}>
              <option value="">Any</option>
              <option value="yes">yes</option>
              <option value="no">no</option>
            </select>
          </label>
          <label className="field">
            <span>Metadata Mismatch</span>
            <select value={filters.metadata_mismatch ?? ''} onChange={(event) => setFilters((current) => ({ ...current, metadata_mismatch: event.target.value }))}>
              <option value="">Any</option>
              <option value="yes">yes</option>
              <option value="no">no</option>
            </select>
          </label>
          <label className="field">
            <span>Low Confidence</span>
            <select value={filters.low_confidence ?? ''} onChange={(event) => setFilters((current) => ({ ...current, low_confidence: event.target.value }))}>
              <option value="">Any</option>
              <option value="yes">yes</option>
              <option value="no">no</option>
            </select>
          </label>
          <label className="field">
            <span>Completion</span>
            <select value={filters.completion_state ?? ''} onChange={(event) => setFilters((current) => ({ ...current, completion_state: event.target.value }))}>
              <option value="">Any</option>
              <option value="exact_100">100%</option>
              <option value="below_100">Below 100%</option>
            </select>
          </label>
          <label className="field">
            <span>Has NFO</span>
            <select value={filters.has_nfo ?? ''} onChange={(event) => setFilters((current) => ({ ...current, has_nfo: event.target.value }))}>
              <option value="">Any</option>
              <option value="true">yes</option>
              <option value="false">no</option>
            </select>
          </label>
          <label className="field">
            <span>Has PAR2</span>
            <select value={filters.has_par2 ?? ''} onChange={(event) => setFilters((current) => ({ ...current, has_par2: event.target.value }))}>
              <option value="">Any</option>
              <option value="true">yes</option>
              <option value="false">no</option>
            </select>
          </label>
          <button className="primary-button align-end" type="submit">
            Apply Filters
          </button>
          <button
            className="secondary-button align-end"
            type="button"
            onClick={() => {
              setFilters(defaultFilters)
              setSubmittedFilters(defaultFilters)
            }}
          >
            Reset
          </button>
        </form>
      </div>
      {error ? <div className="banner error">{error}</div> : null}
      <div className="page-card">
        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <th>Release</th>
                <th>Category</th>
                <th>Posted</th>
                <th>Size</th>
                <th>Files</th>
                <th>Password</th>
                <th>Quality</th>
                <th>State</th>
              </tr>
            </thead>
            <tbody>
              {(data?.items ?? []).map((item) => (
                <tr key={item.release_id}>
                  <td>
                    <Link className="table-link" to={`/admin/indexer/releases/${item.release_id}`}>
                      {item.title}
                    </Link>
                    <div className="muted-row">
                      <span>{item.group_name}</span>
                      <span>{item.identity_status}</span>
                    </div>
                  </td>
                  <td>
                    <div>{item.category || 'n/a'}</div>
                    <div className="muted-row">
                      <span>{item.category_id || 'n/a'}</span>
                      <span>{item.external_media_type || item.classification || 'n/a'}</span>
                    </div>
                  </td>
                  <td>{formatDateTime(item.posted_at)}</td>
                  <td>{formatBytes(item.size_bytes)}</td>
                  <td>
                    <div>{item.file_count}</div>
                    <div className="muted-row">
                      <span>{item.has_nfo ? 'NFO' : 'No NFO'}</span>
                      <span>{item.has_par2 ? 'PAR2' : 'No PAR2'}</span>
                    </div>
                  </td>
                  <td>{item.password_state || 'unknown'}</td>
                  <td>{item.media_quality_tier || 'n/a'}</td>
                  <td>
                    <div>{item.hidden ? 'hidden' : item.public_visible ? 'public' : 'internal-only'}</div>
                    <div className="muted-row">
                      <span>{formatNZBStatus(item.nzb_generation_status || 'pending')}</span>
                      <span>{Math.floor(item.completion_pct)}%</span>
                      <span>{item.password_candidate_count} pwd</span>
                    </div>
                  </td>
                </tr>
              ))}
              {(data?.items.length ?? 0) === 0 ? (
                <tr>
                  <td colSpan={8}>
                    <div className="empty-state">No admin releases matched the current search.</div>
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
