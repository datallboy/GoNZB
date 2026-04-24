import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { listPublicReleases } from '../../shared/api/indexer'
import { formatBytes, formatDateTime, formatPercent } from '../../shared/lib/format'
import type { PublicReleaseListResponse } from '../../shared/types'

const defaultResponse: PublicReleaseListResponse = {
  items: [],
  total: 0,
  count: 0,
  limit: 25,
  offset: 0,
  sort: 'posted_at_desc',
  has_more: false,
  filters: {},
}

export function IndexerReleaseListPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [data, setData] = useState<PublicReleaseListResponse>(defaultResponse)
  const [query, setQuery] = useState(searchParams.get('q') ?? '')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const sort = searchParams.get('sort') ?? 'posted_at_desc'
  const classification = searchParams.get('classification') ?? ''
  const availabilityTier = searchParams.get('availability_tier') ?? ''
  const qualityTier = searchParams.get('media_quality_tier') ?? ''
  const offset = Number(searchParams.get('offset') ?? '0') || 0
  const limit = 25

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    void listPublicReleases({
      q: searchParams.get('q') ?? '',
      sort,
      classification,
      availability_tier: availabilityTier,
      media_quality_tier: qualityTier,
      limit,
      offset,
    })
      .then((response) => {
        if (!cancelled) {
          setData(response)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load releases')
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false)
        }
      })
    return () => {
      cancelled = true
    }
  }, [availabilityTier, classification, limit, offset, qualityTier, searchParams, sort])

  function updateParams(next: Record<string, string>) {
    const params = new URLSearchParams(searchParams)
    Object.entries(next).forEach(([key, value]) => {
      if (value) {
        params.set(key, value)
      } else {
        params.delete(key)
      }
    })
    params.set('offset', '0')
    setSearchParams(params)
  }

  function handleSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    updateParams({ q: query })
  }

  function handlePage(direction: 'prev' | 'next') {
    const nextOffset = direction === 'next' ? offset + limit : Math.max(0, offset - limit)
    const params = new URLSearchParams(searchParams)
    params.set('offset', String(nextOffset))
    setSearchParams(params)
  }

  return (
    <div className="page-section">
      <div className="page-hero">
        <div>
          <p className="eyebrow">Indexer Catalog</p>
          <h1 className="page-title">Stable release catalog for the indexer module.</h1>
        </div>
        <div className="hero-stat-grid">
          <div className="stat-card">
            <span>Total Matches</span>
            <strong>{data.total}</strong>
          </div>
          <div className="stat-card">
            <span>Sort</span>
            <strong>{sort.replaceAll('_', ' ')}</strong>
          </div>
        </div>
      </div>

      <div className="page-card stack">
        <form className="toolbar-grid" onSubmit={handleSearch}>
          <label className="field">
            <span>Search</span>
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Title, release name, scene hint"
            />
          </label>
          <label className="field">
            <span>Sort</span>
            <select value={sort} onChange={(event) => updateParams({ sort: event.target.value })}>
              <option value="posted_at_desc">Newest first</option>
              <option value="posted_at_asc">Oldest first</option>
              <option value="size_desc">Largest first</option>
              <option value="size_asc">Smallest first</option>
              <option value="title_asc">Title A-Z</option>
              <option value="availability_desc">Availability</option>
              <option value="quality_desc">Quality</option>
            </select>
          </label>
          <label className="field">
            <span>Classification</span>
            <select
              value={classification}
              onChange={(event) => updateParams({ classification: event.target.value })}
            >
              <option value="">All</option>
              <option value="movie">Movie</option>
              <option value="tv">TV</option>
              <option value="audio">Audio</option>
              <option value="ebook">Ebook</option>
            </select>
          </label>
          <label className="field">
            <span>Availability</span>
            <select
              value={availabilityTier}
              onChange={(event) => updateParams({ availability_tier: event.target.value })}
            >
              <option value="">All</option>
              <option value="excellent">Excellent</option>
              <option value="good">Good</option>
              <option value="fair">Fair</option>
              <option value="weak">Weak</option>
            </select>
          </label>
          <label className="field">
            <span>Quality</span>
            <select
              value={qualityTier}
              onChange={(event) => updateParams({ media_quality_tier: event.target.value })}
            >
              <option value="">All</option>
              <option value="excellent">Excellent</option>
              <option value="good">Good</option>
              <option value="fair">Fair</option>
              <option value="unknown">Unknown</option>
            </select>
          </label>
          <button className="primary-button align-end" type="submit">
            Apply Filters
          </button>
        </form>

        {error ? <div className="banner error">{error}</div> : null}
        {loading ? <div className="banner">Loading releases...</div> : null}

        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <th>Release</th>
                <th>Posted</th>
                <th>Size</th>
                <th>Completion</th>
                <th>Availability</th>
                <th>Quality</th>
                <th>Metadata</th>
              </tr>
            </thead>
            <tbody>
              {data.items.map((item) => (
                <tr key={item.release_id}>
                  <td>
                    <Link className="table-link" to={`/indexer/releases/${item.release_id}`}>
                      {item.title}
                    </Link>
                    <div className="muted-row">
                      <span>{item.classification || 'unclassified'}</span>
                      <span>{item.external_title || 'no external title'}</span>
                    </div>
                  </td>
                  <td>{formatDateTime(item.posted_at)}</td>
                  <td>{formatBytes(item.size_bytes)}</td>
                  <td>{formatPercent(item.completion_pct)}</td>
                  <td>
                    <span className={`pill tone-${item.availability_tier || 'default'}`}>
                      {item.availability_tier || 'n/a'}
                    </span>
                  </td>
                  <td>
                    <span className={`pill tone-${item.media_quality_tier || 'default'}`}>
                      {item.media_quality_tier || 'n/a'}
                    </span>
                  </td>
                  <td>
                    <div className="muted-row">
                      <span>{item.imdb_id || item.tmdb_id || item.tvdb_id || 'none'}</span>
                      <span>{formatDateTime(item.metadata_updated_at)}</span>
                    </div>
                  </td>
                </tr>
              ))}
              {!loading && data.items.length === 0 ? (
                <tr>
                  <td colSpan={7}>
                    <div className="empty-state">No releases matched the current filters.</div>
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>

        <div className="pagination-row">
          <span className="muted-copy">
            Showing {data.count} of {data.total} releases
          </span>
          <div className="button-row">
            <button className="secondary-button" onClick={() => handlePage('prev')} disabled={offset === 0}>
              Previous
            </button>
            <button
              className="secondary-button"
              onClick={() => handlePage('next')}
              disabled={!data.has_more}
            >
              Next
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
