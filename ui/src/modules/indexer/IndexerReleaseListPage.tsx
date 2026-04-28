import { useEffect, useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import { Link, NavLink, useParams, useSearchParams } from 'react-router-dom'
import { listPublicReleases } from '../../shared/api/indexer'
import { formatBytes, formatRelativeAge } from '../../shared/lib/format'
import type { PublicReleaseListResponse } from '../../shared/types'
import { browseCategories, findBrowseCategory, releaseCategoryLabel } from './browse'

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
  const { category = '', subcategory = '' } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const [data, setData] = useState<PublicReleaseListResponse>(defaultResponse)
  const [query, setQuery] = useState(searchParams.get('q') ?? '')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const currentCategory = findBrowseCategory(category)
  const currentSubcategory = subcategory || 'all'
  const offset = Number(searchParams.get('offset') ?? '0') || 0
  const limit = 25

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    void listPublicReleases({
      q: searchParams.get('q') ?? '',
      browse_category: category,
      browse_subcategory: category ? currentSubcategory : '',
      sort: 'posted_at_desc',
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
  }, [category, currentSubcategory, limit, offset, searchParams])

  const pageTitle = useMemo(() => {
    if (!currentCategory) {
      return 'Browse NZB-ready releases'
    }
    const sub = currentCategory.subcategories.find((item) => item.slug === currentSubcategory)
    return sub && sub.slug !== 'all' ? `${currentCategory.label} / ${sub.label}` : currentCategory.label
  }, [currentCategory, currentSubcategory])

  function handleSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const params = new URLSearchParams(searchParams)
    if (query) {
      params.set('q', query)
    } else {
      params.delete('q')
    }
    params.set('offset', '0')
    setSearchParams(params)
  }

  function handlePage(direction: 'prev' | 'next') {
    const nextOffset = direction === 'next' ? offset + limit : Math.max(0, offset - limit)
    const params = new URLSearchParams(searchParams)
    params.set('offset', String(nextOffset))
    setSearchParams(params)
  }

  return (
    <div className="page-section public-catalog">
      <div className="page-card catalog-hero">
        <div>
          <p className="eyebrow">Browse</p>
          <h1 className="page-title">{pageTitle}</h1>
          <p className="muted-copy">
            {currentCategory?.description || 'Simple public catalog browsing with category-first navigation.'}
          </p>
        </div>
        <form className="catalog-search" onSubmit={handleSearch}>
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search release names"
          />
          <button className="primary-button" type="submit">
            Search
          </button>
        </form>
      </div>

      <div className="page-card category-browser">
        <div className="category-browser__roots">
          <NavLink className={({ isActive }) => `browse-chip${!category && isActive ? ' active' : ''}`} to="/indexer/releases">
            All
          </NavLink>
          {browseCategories.map((item) => (
            <NavLink
              className={({ isActive }) => `browse-chip${isActive ? ' active' : ''}`}
              key={item.slug}
              to={`/indexer/browse/${item.slug}`}
            >
              {item.label}
            </NavLink>
          ))}
        </div>
        {currentCategory ? (
          <div className="category-browser__subs">
            {currentCategory.subcategories.map((item) => (
              <NavLink
                className={({ isActive }) => `browse-subchip${isActive ? ' active' : ''}`}
                key={item.slug}
                to={item.slug === 'all' ? `/indexer/browse/${currentCategory.slug}` : `/indexer/browse/${currentCategory.slug}/${item.slug}`}
              >
                {item.label}
              </NavLink>
            ))}
          </div>
        ) : null}
      </div>

      {error ? <div className="banner error">{error}</div> : null}
      {loading ? <div className="banner">Loading releases...</div> : null}

      <div className="page-card">
        <div className="table-shell">
          <table className="data-table public-data-table">
            <thead>
              <tr>
                <th>Title</th>
                <th>Age</th>
                <th>Category</th>
                <th>Files</th>
                <th>Size</th>
              </tr>
            </thead>
            <tbody>
              {data.items.map((item) => (
                <tr key={item.release_id}>
                  <td>
                    <Link className="table-link" to={`/indexer/releases/${item.release_id}`}>
                      {item.title}
                    </Link>
                  </td>
                  <td>{formatRelativeAge(item.posted_at)}</td>
                  <td>{releaseCategoryLabel(item)}</td>
                  <td>{item.file_count}</td>
                  <td>{formatBytes(item.size_bytes)}</td>
                </tr>
              ))}
              {!loading && data.items.length === 0 ? (
                <tr>
                  <td colSpan={5}>
                    <div className="empty-state">No releases matched this browse view.</div>
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        <div className="pagination-row">
          <div className="button-row">
            <button className="secondary-button" onClick={() => handlePage('prev')} disabled={offset === 0}>
              Previous
            </button>
            <button className="secondary-button" onClick={() => handlePage('next')} disabled={!data.has_more}>
              Next
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
