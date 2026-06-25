import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { getAdminBinaries, getIndexerBinary } from '../../shared/api/admin'
import { formatBytes, formatDateTime, formatNumber, formatPercent } from '../../shared/lib/format'
import type { AdminBinaryDetail, AdminBinaryListParams, AdminBinaryListResponse, AdminBinarySummary } from '../../shared/types'

const defaultFilters: AdminBinaryListParams = {
  q: '',
  newsgroup: '',
  identity_strength: 'weak',
  readiness_bucket: '',
  match_status: '',
  release_state: 'unformed',
  sort: 'updated_desc',
  limit: 100,
  offset: 0,
}

function stringifyJSON(value: unknown) {
  if (value == null) return 'None'
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function label(value: string | undefined, fallback = 'unknown') {
  return value && value.trim() ? value : fallback
}

function completionLabel(item: { observed_parts: number; total_parts: number; completion_pct?: number }) {
  if (!item.total_parts) return `${formatNumber(item.observed_parts)} observed`
  const pct = item.completion_pct ?? Math.min(100, (item.observed_parts / item.total_parts) * 100)
  return `${item.observed_parts.toLocaleString()} / ${item.total_parts.toLocaleString()} (${formatPercent(pct)})`
}

export function AdminBinariesPage() {
  const [filters, setFilters] = useState<AdminBinaryListParams>(defaultFilters)
  const [submittedFilters, setSubmittedFilters] = useState<AdminBinaryListParams>(defaultFilters)
  const [data, setData] = useState<AdminBinaryListResponse | null>(null)
  const [selected, setSelected] = useState<AdminBinarySummary | null>(null)
  const [detail, setDetail] = useState<AdminBinaryDetail | null>(null)
  const [loadingDetail, setLoadingDetail] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getAdminBinaries(submittedFilters)
      .then((response) => {
        setData(response)
        setError(null)
        if (!selected && response.items.length > 0) {
          void selectBinary(response.items[0])
        }
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load binaries'))
  }, [submittedFilters])

  async function selectBinary(item: AdminBinarySummary) {
    setSelected(item)
    setDetail(null)
    setLoadingDetail(true)
    try {
      setDetail(await getIndexerBinary(item.binary_id))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load binary detail')
    } finally {
      setLoadingDetail(false)
    }
  }

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
            <p className="eyebrow">Admin Binary Workbench</p>
            <h1 className="page-title">Binary diagnostics</h1>
            <p className="muted-copy">Inspect weak, incomplete, recovered, and unformed binaries with article and recovery evidence.</p>
          </div>
        </div>
        <form className="release-table-search" onSubmit={handleSubmit}>
          <input className="table-input" placeholder="Binary id, file, release, key" value={filters.q ?? ''} onChange={(event) => setFilters({ ...filters, q: event.target.value })} />
          <input className="table-input" placeholder="Newsgroup" value={filters.newsgroup ?? ''} onChange={(event) => setFilters({ ...filters, newsgroup: event.target.value })} />
          <select className="table-input" value={filters.identity_strength ?? ''} onChange={(event) => setFilters({ ...filters, identity_strength: event.target.value })}>
            <option value="">Any strength</option>
            <option value="weak">weak</option>
            <option value="provisional">provisional</option>
            <option value="strong">strong</option>
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
      </div>

      <div className="dashboard-grid dashboard-grid--wide">
        <div className="page-card stack">
          <div className="release-table-toolbar">
            <span className="muted-copy">
              Showing {rows.length.toLocaleString()} of {(data?.total ?? 0).toLocaleString()}
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
                  <th>Binary</th>
                  <th>Group</th>
                  <th>Parts</th>
                  <th>Identity</th>
                  <th>Recovery</th>
                  <th>Release</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((item) => (
                  <tr key={item.binary_id}>
                    <td>
                      <button className="table-sort-button" type="button" onClick={() => void selectBinary(item)}>
                        {item.file_name || item.binary_name || item.binary_id}
                      </button>
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
                {rows.length === 0 ? (
                  <tr><td colSpan={6}>No binaries match the current filters.</td></tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </div>

        <div className="page-card stack">
          <h2 className="section-title">Selected Binary</h2>
          {loadingDetail ? <div className="banner">Loading binary detail...</div> : null}
          {!selected ? <div className="banner">Select a binary.</div> : null}
          {detail ? (
            <>
              <dl className="detail-grid">
                <div><dt>ID</dt><dd>{detail.binary_id}</dd></div>
                <div><dt>File</dt><dd>{detail.file_name || detail.binary_name}</dd></div>
                <div><dt>Parts</dt><dd>{completionLabel(detail)}</dd></div>
                <div><dt>Size</dt><dd>{formatBytes(detail.total_bytes)}</dd></div>
                <div><dt>Article Range</dt><dd>{detail.first_article_number} - {detail.last_article_number}</dd></div>
                <div><dt>Match</dt><dd>{label(detail.match_status)} · {formatPercent(detail.match_confidence * 100)}</dd></div>
                <div><dt>Release</dt><dd>{detail.release_id ? <Link to={`/admin/indexer/releases/${detail.release_id}`}>{detail.release_title || detail.release_id}</Link> : 'Unformed'}</dd></div>
                <div><dt>Poster</dt><dd>{label(detail.poster)}</dd></div>
              </dl>

              <details className="detail-block" open>
                <summary>Article Headers</summary>
                <div className="table-shell">
                  <table className="data-table data-table--compact">
                    <thead><tr><th>Part</th><th>Article</th><th>Subject</th><th>yEnc</th><th>Recovery</th></tr></thead>
                    <tbody>
                      {detail.parts.map((part) => (
                        <tr key={part.article_header_id}>
                          <td>{part.part_number} / {part.total_parts}</td>
                          <td>{part.article_number}<div className="muted-copy">{part.message_id}</div></td>
                          <td>{part.subject}<div className="muted-copy">{part.poster}</div></td>
                          <td>{part.yenc_part_number} / {part.yenc_total_parts}<div className="muted-copy">{formatBytes(part.yenc_file_size)}</div></td>
                          <td>{label(part.yenc_recovery_status, 'none')}<div className="muted-copy">{label(part.recovered_source, '')} {part.recovered_file_name}</div></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </details>

              <details className="detail-block">
                <summary>Inspections</summary>
                {detail.inspections.map((inspection) => (
                  <div className="detail-block" key={inspection.stage_name}>
                    <strong>{inspection.stage_name}</strong> · {inspection.status}
                    {inspection.error_text ? <div className="banner error">{inspection.error_text}</div> : null}
                    <pre className="json-block">{stringifyJSON(inspection.summary_json)}</pre>
                  </div>
                ))}
              </details>

              <details className="detail-block">
                <summary>Archive, PAR2, Text, Media Evidence</summary>
                <pre className="json-block">{stringifyJSON({
                  artifacts: detail.artifacts,
                  archive_entries: detail.archive_entries,
                  par2_sets: detail.par2_sets,
                  text_evidence: detail.text_evidence,
                  media_streams: detail.media_streams,
                })}</pre>
              </details>

              <details className="detail-block">
                <summary>Grouping Evidence</summary>
                <pre className="json-block">{stringifyJSON(detail.grouping_evidence_json)}</pre>
              </details>
            </>
          ) : null}
        </div>
      </div>
    </div>
  )
}
