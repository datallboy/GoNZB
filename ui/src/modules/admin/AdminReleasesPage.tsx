import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { getAdminReleases } from '../../shared/api/admin'
import { formatBytes, formatDateTime } from '../../shared/lib/format'
import type { AdminReleaseListResponse } from '../../shared/types'

export function AdminReleasesPage() {
  const [query, setQuery] = useState('')
  const [submittedQuery, setSubmittedQuery] = useState('')
  const [data, setData] = useState<AdminReleaseListResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getAdminReleases(submittedQuery)
      .then((response) => {
        setData(response)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load admin releases'))
  }, [submittedQuery])

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmittedQuery(query)
  }

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <p className="eyebrow">Release Moderation</p>
        <h1 className="page-title">Curate release overrides without mutating generated rows.</h1>
        <form className="toolbar-grid" onSubmit={handleSubmit}>
          <label className="field">
            <span>Search Releases</span>
            <input value={query} onChange={(event) => setQuery(event.target.value)} />
          </label>
          <button className="primary-button align-end" type="submit">
            Search
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
                <th>Posted</th>
                <th>Size</th>
                <th>Password</th>
                <th>Quality</th>
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
                  <td>{formatDateTime(item.posted_at)}</td>
                  <td>{formatBytes(item.size_bytes)}</td>
                  <td>{item.password_state || 'unknown'}</td>
                  <td>{item.media_quality_tier || 'n/a'}</td>
                </tr>
              ))}
              {(data?.items.length ?? 0) === 0 ? (
                <tr>
                  <td colSpan={5}>
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
