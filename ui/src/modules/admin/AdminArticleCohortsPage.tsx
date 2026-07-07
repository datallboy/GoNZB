import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { getAdminArticleCohorts } from '../../shared/api/admin'
import { formatDateTime, formatNumber } from '../../shared/lib/format'
import type { AdminArticleCohort, AdminArticleCohortListResponse } from '../../shared/types'

type CohortFilters = {
  kind: string
  status: string
  limit: number
  offset: number
}

const defaultFilters: CohortFilters = {
  kind: '',
  status: '',
  limit: 100,
  offset: 0,
}

function label(value: string | undefined, fallback = 'unknown') {
  return value && value.trim() ? value : fallback
}

function partLabel(item: AdminArticleCohort) {
  if (item.subject_file_total > 0) {
    return `${formatNumber(item.subject_file_index)} / ${formatNumber(item.subject_file_total)}`
  }
  if (item.yenc_total_parts > 0) {
    return `${formatNumber(item.yenc_total_parts)} yEnc parts`
  }
  return 'n/a'
}

export function AdminArticleCohortsPage() {
  const [filters, setFilters] = useState<CohortFilters>(defaultFilters)
  const [submittedFilters, setSubmittedFilters] = useState<CohortFilters>(defaultFilters)
  const [data, setData] = useState<AdminArticleCohortListResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getAdminArticleCohorts(submittedFilters)
      .then((response) => {
        setData(response)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load article cohorts'))
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
  const offset = submittedFilters.offset
  const limit = submittedFilters.limit

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <div className="page-header">
          <div>
            <p className="eyebrow">Indexer Work</p>
            <h1 className="page-title">Article cohorts</h1>
          </div>
        </div>
        <form className="release-table-search" onSubmit={handleSubmit}>
          <select className="table-input" value={filters.kind} onChange={(event) => setFilters({ ...filters, kind: event.target.value })}>
            <option value="">Any kind</option>
            <option value="subject_complete">subject_complete</option>
            <option value="opaque_near_time">opaque_near_time</option>
            <option value="yenc_proven">yenc_proven</option>
          </select>
          <select className="table-input" value={filters.status} onChange={(event) => setFilters({ ...filters, status: event.target.value })}>
            <option value="">Any status</option>
            <option value="ready">ready</option>
            <option value="active">active</option>
            <option value="cooldown">cooldown</option>
            <option value="done">done</option>
          </select>
          <select className="table-input" value={filters.limit} onChange={(event) => setFilters({ ...filters, limit: Number(event.target.value), offset: 0 })}>
            <option value={50}>50</option>
            <option value={100}>100</option>
            <option value={250}>250</option>
            <option value={500}>500</option>
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
            <button className="secondary-button" type="button" disabled={offset <= 0} onClick={() => handlePage(offset - limit)}>Previous</button>
            <button className="secondary-button" type="button" disabled={offset + rows.length >= (data?.total ?? 0)} onClick={() => handlePage(offset + limit)}>Next</button>
          </div>
        </div>
        <div className="table-shell">
          <table className="data-table data-table--compact">
            <thead>
              <tr>
                <th>Cohort</th>
                <th>Group</th>
                <th>Bucket</th>
                <th>Articles</th>
                <th>Queues</th>
                <th>yEnc</th>
                <th>Subject</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => (
                <tr key={`${item.source_posted_at}-${item.cohort_key}`}>
                  <td>
                    <span className="status-pill status-pill--table">{item.cohort_kind}</span>
                    <div className="muted-copy">p{item.priority_rank} · {label(item.status)} · {Math.round(item.score).toLocaleString()}</div>
                  </td>
                  <td>
                    {label(item.newsgroup_name, String(item.newsgroup_id))}
                    <div className="muted-copy">provider {item.provider_id}</div>
                  </td>
                  <td>
                    {formatDateTime(item.bucket_start)}
                    <div className="muted-copy">{formatDateTime(item.bucket_end)}</div>
                  </td>
                  <td>
                    {formatNumber(item.article_count)}
                    <div className="muted-copy">{formatNumber(item.singleton_count)} singletons · {formatNumber(item.unassembled_count)} unassembled</div>
                  </td>
                  <td>
                    {formatNumber(item.assembly_queue_ready)} assemble
                    <div className="muted-copy">{formatNumber(item.recovery_queue_ready)} yEnc ready · {formatNumber(item.recovery_queue_admitted)} admitted</div>
                  </td>
                  <td>
                    {formatNumber(item.yenc_recovered_count)} recovered
                    <div className="muted-copy">{formatNumber(item.yenc_ready_count)} ready · {formatNumber(item.yenc_running_count)} running · {formatNumber(item.yenc_done_count)} done</div>
                  </td>
                  <td>
                    {item.subject_file_name || <span className="muted-copy">n/a</span>}
                    <div className="muted-copy">{partLabel(item)}</div>
                  </td>
                </tr>
              ))}
              {rows.length === 0 ? (
                <tr><td colSpan={7}>No cohorts match the current filters.</td></tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
