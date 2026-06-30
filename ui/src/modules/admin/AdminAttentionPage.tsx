import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { getAdminAttention } from '../../shared/api/admin'
import { formatBytes, formatDateTime, formatNumber } from '../../shared/lib/format'
import type { AdminAttentionListParams, AdminAttentionListResponse } from '../../shared/types'

const defaultFilters: AdminAttentionListParams = {
  reason: '',
  limit: 100,
  offset: 0,
}

const reasonOptions = [
  ['', 'All reasons'],
  ['manual_title_needed', 'Manual title needed'],
  ['predb_review', 'PreDB review'],
  ['external_metadata_review', 'TVDB/TMDB review'],
  ['sfv_sidecar_review', 'SFV sidecar review'],
  ['inspection_failed', 'Inspection failed'],
  ['public_blocked_title', 'Public blocked title'],
] as const

const reasonLabels: Record<string, string> = {
  manual_title_needed: 'Manual title',
  predb_review: 'PreDB',
  external_metadata_review: 'Metadata',
  sfv_sidecar_review: 'SFV',
  inspection_failed: 'Inspection failed',
  public_blocked_title: 'Public blocked',
}

function reasonLabel(reason: string) {
  return reasonLabels[reason] ?? reason
}

function titleLabel(value: string) {
  return value && value.trim() ? value : 'Untitled release'
}

export function AdminAttentionPage() {
  const [filters, setFilters] = useState<AdminAttentionListParams>(defaultFilters)
  const [submittedFilters, setSubmittedFilters] = useState<AdminAttentionListParams>(defaultFilters)
  const [data, setData] = useState<AdminAttentionListResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getAdminAttention(submittedFilters)
      .then((response) => {
        setData(response)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load attention queue'))
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

  const rows = data?.items ?? []

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <div className="page-header">
          <div>
            <p className="eyebrow">Admin Attention</p>
            <h1 className="page-title">Release review queue</h1>
            <p className="muted-copy">
              Releases that need manual title, metadata, sidecar, PreDB, or failed-inspection review before they should be trusted.
            </p>
          </div>
        </div>
        <form className="release-table-search" onSubmit={handleSubmit}>
          <select
            className="table-input"
            value={filters.reason ?? ''}
            onChange={(event) => setFilters({ ...filters, reason: event.target.value })}
          >
            {reasonOptions.map(([value, label]) => (
              <option key={value || 'all'} value={value}>{label}</option>
            ))}
          </select>
          <button className="primary-button" type="submit">Apply</button>
        </form>
        {error ? <div className="banner error">{error}</div> : null}
      </div>

      <div className="page-card stack">
        <div className="release-table-toolbar">
          <span className="muted-copy">
            Showing {formatNumber(rows.length)} of {formatNumber(data?.total ?? 0)}
          </span>
          <div className="button-row">
            <button
              className="secondary-button"
              type="button"
              disabled={(data?.offset ?? 0) <= 0}
              onClick={() => handlePage((data?.offset ?? 0) - (data?.limit ?? 100))}
            >
              Previous
            </button>
            <button
              className="secondary-button"
              type="button"
              disabled={!data?.has_more}
              onClick={() => handlePage((data?.offset ?? 0) + (data?.limit ?? 100))}
            >
              Next
            </button>
          </div>
        </div>
        <div className="table-shell">
          <table className="data-table data-table--compact">
            <thead>
              <tr>
                <th>Release</th>
                <th>Reasons</th>
                <th>Evidence</th>
                <th>Identity</th>
                <th>Posted</th>
                <th>Updated</th>
                <th>Action</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => (
                <tr key={item.release_id}>
                  <td>
                    <Link className="table-link" to={`/admin/indexer/releases/${item.release_id}`}>
                      {titleLabel(item.title)}
                    </Link>
                    <div className="muted-copy">{item.group_name || 'unknown group'}</div>
                    <div className="muted-copy">{formatBytes(item.size_bytes)}</div>
                  </td>
                  <td>
                    <div className="badge-row">
                      {item.reasons.map((reason) => (
                        <span className="status-pill warning" key={reason}>{reasonLabel(reason)}</span>
                      ))}
                    </div>
                    <div className="muted-copy">Priority {item.priority}</div>
                  </td>
                  <td>
                    <div className="badge-row">
                      {item.has_sfv ? <span className="status-pill">SFV</span> : null}
                      {item.has_par2 ? <span className="status-pill">PAR2</span> : null}
                      {item.has_nfo ? <span className="status-pill">NFO</span> : null}
                      {item.public_visible ? <span className="status-pill success">Public</span> : <span className="status-pill warning">Not public</span>}
                    </div>
                    <div className="muted-copy">Payload {item.payload_completion_state || 'unknown'}</div>
                    {item.predb_candidate_count > 0 ? (
                      <div className="muted-copy">{formatNumber(item.predb_candidate_count)} PreDB candidate{item.predb_candidate_count === 1 ? '' : 's'}</div>
                    ) : null}
                    {item.inspection_failure_count > 0 ? (
                      <div className="muted-copy">{formatNumber(item.inspection_failure_count)} inspection failure{item.inspection_failure_count === 1 ? '' : 's'}</div>
                    ) : null}
                  </td>
                  <td>
                    <div>{item.identity_status || 'unknown'}</div>
                    <div className="muted-copy">{item.title_source || 'unknown source'}</div>
                    <div className="muted-copy">{item.category || item.classification || 'uncategorized'}</div>
                  </td>
                  <td>{formatDateTime(item.posted_at)}</td>
                  <td>
                    {formatDateTime(item.updated_at)}
                    {item.latest_inspection_error ? (
                      <div className="muted-copy">{item.latest_inspection_error}</div>
                    ) : null}
                  </td>
                  <td>
                    <Link className="secondary-button" to={`/admin/indexer/releases/${item.release_id}`}>
                      Review
                    </Link>
                  </td>
                </tr>
              ))}
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={7} className="muted-row">No releases currently require attention for this filter.</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
