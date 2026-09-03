import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { getAdminReleaseCandidates } from '../../shared/api/admin'
import { formatBytes, formatDateTime } from '../../shared/lib/format'
import type { AdminReleaseCandidate, AdminReleaseCandidateListParams, AdminReleaseCandidateListResponse } from '../../shared/types'

const defaultFilters: AdminReleaseCandidateListParams = {
  q: '',
  newsgroup: '',
  evaluation_state: '',
  ready_reason: '',
  key_kind: '',
  sort: 'updated_desc',
  limit: 100,
  offset: 0,
}

function formatPercent(value: number) {
  return `${Math.max(0, Math.min(100, value)).toFixed(1)}%`
}

function evaluationLabel(item: AdminReleaseCandidate) {
  if (item.evaluation_state === 'formed') return 'Formed'
  if (item.evaluation_state === 'pending') return 'Pending evaluation'
  return 'Evaluated'
}

function evaluationNoteLabel(value: string) {
  switch (value) {
    case 'formed_release':
      return 'Release formed'
    case 'awaiting_release_stage':
      return 'Awaiting release stage'
    case 'recovery_pending':
      return 'Recovery is still pending'
    case 'no_complete_main_payload':
      return 'No complete main payload'
    case 'archive_file_set_incomplete':
      return 'Archive file set incomplete'
    case 'expected_file_set_incomplete':
      return 'Expected file set incomplete'
    default:
      return 'Evaluated; candidate must change before retry'
  }
}

export function AdminReleaseCandidatesPage() {
  const [filters, setFilters] = useState<AdminReleaseCandidateListParams>(defaultFilters)
  const [submittedFilters, setSubmittedFilters] = useState<AdminReleaseCandidateListParams>(defaultFilters)
  const [data, setData] = useState<AdminReleaseCandidateListResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getAdminReleaseCandidates(submittedFilters)
      .then((response) => {
        setData(response)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load release candidates'))
  }, [submittedFilters])

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmittedFilters({ ...filters, offset: 0 })
  }

  function reset() {
    setFilters(defaultFilters)
    setSubmittedFilters(defaultFilters)
  }

  function page(nextOffset: number) {
    const next = { ...submittedFilters, offset: Math.max(0, nextOffset) }
    setFilters(next)
    setSubmittedFilters(next)
  }

  function pageSize(limit: number) {
    const next = { ...submittedFilters, limit, offset: 0 }
    setFilters(next)
    setSubmittedFilters(next)
  }

  const limit = Number(submittedFilters.limit ?? 100)
  const offset = Number(submittedFilters.offset ?? 0)
  const total = data?.total ?? 0
  const pageStart = total > 0 ? offset + 1 : 0
  const pageEnd = Math.min(offset + (data?.items.length ?? 0), total)

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <div className="page-header">
          <div>
            <p className="eyebrow">Release Formation</p>
            <h1 className="page-title">Release candidates</h1>
            <p className="muted-copy">Materialized candidate families that the release stage may form, defer, or reevaluate as binary evidence changes.</p>
          </div>
          <div className="stack">
            <div className="release-list-summary">
              <strong>{total}</strong>
              <span>candidate rows</span>
            </div>
            <Link className="secondary-button" to="/admin/indexer/releases">View formed releases</Link>
          </div>
        </div>
        <form className="toolbar-grid" onSubmit={submit}>
          <label className="field">
            <span>Search candidates</span>
            <input value={filters.q ?? ''} onChange={(event) => setFilters((current) => ({ ...current, q: event.target.value }))} />
          </label>
          <label className="field">
            <span>Newsgroup</span>
            <input value={filters.newsgroup ?? ''} onChange={(event) => setFilters((current) => ({ ...current, newsgroup: event.target.value }))} placeholder="alt.binaries.example" />
          </label>
          <label className="field">
            <span>Evaluation</span>
            <select value={filters.evaluation_state ?? ''} onChange={(event) => setFilters((current) => ({ ...current, evaluation_state: event.target.value }))}>
              <option value="">Any</option>
              <option value="pending">Pending</option>
              <option value="evaluated">Evaluated</option>
              <option value="formed">Formed</option>
            </select>
          </label>
          <label className="field">
            <span>Candidate key</span>
            <select value={filters.key_kind ?? ''} onChange={(event) => setFilters((current) => ({ ...current, key_kind: event.target.value }))}>
              <option value="">Any</option>
              <option value="recovered_file_set">Recovered file set</option>
              <option value="base_stem">Base stem</option>
              <option value="release_family">Release family</option>
            </select>
          </label>
          <label className="field">
            <span>Readiness</span>
            <select value={filters.ready_reason ?? ''} onChange={(event) => setFilters((current) => ({ ...current, ready_reason: event.target.value }))}>
              <option value="">Any</option>
              <option value="actionable">Actionable</option>
              <option value="fragment_only">Fragment only</option>
              <option value="weak_single_binary">Weak single binary</option>
              <option value="weak_obfuscated_set">Weak obfuscated set</option>
              <option value="overgrouped_contextual">Overgrouped</option>
              <option value="prefer_base_stem">Prefer base stem</option>
              <option value="stale_cleanup_only">Stale cleanup</option>
            </select>
          </label>
          <label className="field">
            <span>Sort</span>
            <select value={filters.sort ?? 'updated_desc'} onChange={(event) => setFilters((current) => ({ ...current, sort: event.target.value }))}>
              <option value="updated_desc">Recently updated</option>
              <option value="updated_asc">Oldest update</option>
              <option value="posted_desc">Newest posted</option>
              <option value="posted_asc">Oldest posted</option>
              <option value="coverage_desc">Best coverage</option>
              <option value="completion_desc">Most complete binaries</option>
              <option value="name_asc">Name</option>
            </select>
          </label>
          <button className="primary-button align-end" type="submit">Apply filters</button>
          <button className="secondary-button align-end" type="button" onClick={reset}>Reset</button>
        </form>
      </div>

      {error ? <div className="banner error">{error}</div> : null}

      <div className="page-card stack">
        <div className="release-table-toolbar">
          <div className="muted-copy">Rows can represent different grouping strategies for the same logical release key.</div>
          <label className="field release-page-size">
            <span>Rows</span>
            <select value={limit} onChange={(event) => pageSize(Number(event.target.value))}>
              <option value={25}>25</option>
              <option value={50}>50</option>
              <option value={100}>100</option>
              <option value={200}>200</option>
            </select>
          </label>
        </div>
        <div className="pagination-row">
          <span className="muted-copy">Showing {pageStart}-{pageEnd} of {total.toLocaleString()}</span>
          <span className="muted-copy">Page {Math.floor(offset / limit) + 1}</span>
        </div>
        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <th>Candidate</th>
                <th>Evaluation</th>
                <th>Binaries</th>
                <th>File coverage</th>
                <th>Posted</th>
                <th>Size</th>
                <th>Updated</th>
              </tr>
            </thead>
            <tbody>
              {(data?.items ?? []).map((item) => (
                <tr key={`${item.source_posted_at}:${item.provider_id}:${item.newsgroup_id}:${item.key_kind}:${item.family_key}`}>
                  <td>
                    <div>{item.release_name || item.release_key || item.family_key}</div>
                    <div className="muted-row">
                      <span>{item.newsgroup_name || `group ${item.newsgroup_id}`}</span>
                      <span>{item.key_kind.replaceAll('_', ' ')}</span>
                      <span>{item.ready_reason.replaceAll('_', ' ')}</span>
                    </div>
                  </td>
                  <td>
                    <div>{evaluationLabel(item)}</div>
                    <div className="muted-row">
                      <span>{evaluationNoteLabel(item.evaluation_note)}</span>
                      {item.recover_pending ? <span>recovery pending</span> : null}
                    </div>
                    {item.formed_release_id ? (
                      <Link className="table-link" to={`/admin/indexer/releases/${item.formed_release_id}`}>
                        {item.formed_release_title || 'Open release'}
                      </Link>
                    ) : null}
                  </td>
                  <td>
                    <div>{item.complete_binary_count}/{item.binary_count} complete</div>
                    <div className="muted-row"><span>{item.complete_main_payload_binary_count} complete main payload</span></div>
                  </td>
                  <td>
                    <div>{formatPercent(item.expected_file_coverage_pct)} expected</div>
                    <div className="muted-row">
                      <span>{item.expected_file_count || 'unknown'} files</span>
                      {item.has_expected_archive_file_count ? <span>{formatPercent(item.archive_file_coverage_pct)} archive</span> : null}
                    </div>
                  </td>
                  <td>{formatDateTime(item.posted_at)}</td>
                  <td>{formatBytes(item.total_bytes)}</td>
                  <td>
                    <div>{formatDateTime(item.updated_at)}</div>
                    {item.evaluated_at ? <div className="muted-row"><span>evaluated {formatDateTime(item.evaluated_at)}</span></div> : null}
                  </td>
                </tr>
              ))}
              {(data?.items.length ?? 0) === 0 ? (
                <tr>
                  <td colSpan={7}><div className="empty-state">No release candidates matched the current filters.</div></td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        <div className="pagination-row">
          <button className="secondary-button" type="button" disabled={offset <= 0} onClick={() => page(offset - limit)}>Previous</button>
          <span className="muted-copy">Showing {pageStart}-{pageEnd} of {total.toLocaleString()}</span>
          <button className="secondary-button" type="button" disabled={!data?.has_more} onClick={() => page(offset + limit)}>Next</button>
        </div>
      </div>
    </div>
  )
}
