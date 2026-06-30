import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getIndexerBinary } from '../../shared/api/admin'
import { formatBytes, formatDateTime, formatNumber, formatPercent } from '../../shared/lib/format'
import type { AdminBinaryDetail } from '../../shared/types'

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

function completionLabel(item: { observed_parts: number; total_parts: number }) {
  if (!item.total_parts) return `${formatNumber(item.observed_parts)} observed`
  const pct = Math.min(100, (item.observed_parts / item.total_parts) * 100)
  return `${item.observed_parts.toLocaleString()} / ${item.total_parts.toLocaleString()} (${formatPercent(pct)})`
}

function inspectionSummary(detail: AdminBinaryDetail) {
  return [
    `${detail.inspections.length} inspections`,
    `${detail.artifacts.length} artifacts`,
    `${detail.archive_entries.length} archive entries`,
    `${detail.media_streams.length} media streams`,
    `${detail.text_evidence.length} text evidence`,
    `${detail.par2_sets.length} PAR2 sets`,
  ].join(' · ')
}

export function AdminBinaryDetailPage() {
  const { id = '' } = useParams()
  const binaryID = Number(id)
  const [detail, setDetail] = useState<AdminBinaryDetail | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!Number.isFinite(binaryID) || binaryID <= 0) {
      setError('Invalid binary id.')
      return
    }
    setDetail(null)
    setError(null)
    void getIndexerBinary(binaryID)
      .then(setDetail)
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load binary detail'))
  }, [binaryID])

  if (error) {
    return <div className="banner error">{error}</div>
  }
  if (!detail) {
    return <div className="banner">Loading binary detail...</div>
  }

  return (
    <div className="page-section stack">
      <div className="page-card">
        <div className="page-header">
          <div>
            <p className="eyebrow">Admin Binary Detail</p>
            <h1 className="page-title">{detail.file_name || detail.binary_name || detail.binary_id}</h1>
            <p className="muted-copy">
              Binary {detail.binary_id} · {detail.group_name || 'unknown group'} · posted {formatDateTime(detail.posted_at)}
            </p>
          </div>
          <div className="button-row">
            <Link className="secondary-button" to="/admin/indexer/binaries">Back to Binaries</Link>
            {detail.release_id ? (
              <Link className="secondary-button" to={`/admin/indexer/releases/${detail.release_id}`}>Open Release</Link>
            ) : null}
          </div>
        </div>
      </div>

      <div className="dashboard-grid">
        <div className="page-card stack">
          <h2 className="section-title">Binary Completion</h2>
          <dl className="detail-grid">
            <div><dt>Parts</dt><dd>{completionLabel(detail)}</dd></div>
            <div><dt>Article Rows</dt><dd>{detail.parts.length.toLocaleString()}</dd></div>
            <div><dt>Total Bytes</dt><dd>{formatBytes(detail.total_bytes)}</dd></div>
            <div><dt>Article Range</dt><dd>{detail.first_article_number} - {detail.last_article_number}</dd></div>
            <div><dt>File Index</dt><dd>{detail.file_index || 'Unknown'} / {detail.expected_file_count || 'Unknown'}</dd></div>
            <div><dt>Match</dt><dd>{label(detail.match_status)} · {formatPercent(detail.match_confidence * 100)}</dd></div>
            <div><dt>Release</dt><dd>{detail.release_id ? <Link to={`/admin/indexer/releases/${detail.release_id}`}>{detail.release_title || detail.release_id}</Link> : 'Unformed'}</dd></div>
            <div><dt>Poster</dt><dd>{label(detail.poster)}</dd></div>
            <div><dt>Password</dt><dd>{label(detail.password_state)}</dd></div>
            <div><dt>Encrypted</dt><dd>{detail.encrypted ? 'Yes' : 'No'}</dd></div>
          </dl>
          <div className="muted-copy">{inspectionSummary(detail)}</div>
        </div>

        <div className="page-card stack">
          <h2 className="section-title">Identity</h2>
          <dl className="detail-grid">
            <div><dt>Release Key</dt><dd>{detail.release_key || 'None'}</dd></div>
            <div><dt>Release Name</dt><dd>{detail.release_name || 'None'}</dd></div>
            <div><dt>Binary Key</dt><dd>{detail.binary_key || 'None'}</dd></div>
            <div><dt>Binary Name</dt><dd>{detail.binary_name || 'None'}</dd></div>
            <div><dt>File Name</dt><dd>{detail.file_name || 'None'}</dd></div>
          </dl>
        </div>
      </div>

      <div className="page-card stack">
        <h2 className="section-title">Article Headers</h2>
        <p className="muted-copy">
          Authoritative binary-owned source segments, including yEnc recovery evidence when present.
        </p>
        <div className="table-shell">
          <table className="data-table data-table--compact">
            <thead>
              <tr>
                <th>Part</th>
                <th>Article</th>
                <th>Subject</th>
                <th>yEnc</th>
                <th>Recovery</th>
                <th>Bytes</th>
              </tr>
            </thead>
            <tbody>
              {detail.parts.map((part) => (
                <tr key={`${detail.binary_id}-${part.article_header_id}`}>
                  <td>{part.part_number}{part.total_parts > 0 ? ` / ${part.total_parts}` : ''}</td>
                  <td>
                    <div className="mono-cell">{part.article_number || part.article_header_id}</div>
                    <div className="muted-copy">{part.message_id}</div>
                    <div className="muted-copy">{part.group_name}</div>
                  </td>
                  <td>
                    <div>{part.file_name || '-'}</div>
                    {part.subject ? <div className="muted-copy mono-cell">{part.subject}</div> : null}
                    {part.poster ? <div className="muted-copy">{part.poster}</div> : null}
                  </td>
                  <td>
                    {part.yenc_total_parts > 0 ? `${part.yenc_part_number || part.part_number} / ${part.yenc_total_parts}` : '-'}
                    {part.yenc_file_size > 0 ? <div className="muted-copy">{formatBytes(part.yenc_file_size)}</div> : null}
                  </td>
                  <td>
                    {part.yenc_recovery_status || 'none'}
                    {part.recovered_source ? <div className="muted-copy">{part.recovered_source}</div> : null}
                    {part.recovered_file_name ? <div className="muted-copy">{part.recovered_file_name}</div> : null}
                    {part.yenc_recovery_error ? <div className="muted-copy">{part.yenc_recovery_error}</div> : null}
                  </td>
                  <td>{formatBytes(part.segment_bytes || part.article_bytes)}</td>
                </tr>
              ))}
              {detail.parts.length === 0 ? (
                <tr><td colSpan={6}>No binary parts are recorded.</td></tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      <div className="dashboard-grid">
        <div className="page-card stack">
          <h2 className="section-title">Inspection Runs</h2>
          {detail.inspections.map((inspection) => (
            <details className="detail-block" key={inspection.stage_name}>
              <summary>{inspection.stage_name} · {inspection.status}</summary>
              {inspection.error_text ? <div className="banner error">{inspection.error_text}</div> : null}
              <pre className="json-block">{stringifyJSON(inspection.summary_json)}</pre>
            </details>
          ))}
        </div>

        <div className="page-card stack">
          <h2 className="section-title">Derived Evidence</h2>
          <details className="detail-block" open>
            <summary>Archive Entries ({detail.archive_entries.length})</summary>
            <pre className="json-block">{stringifyJSON(detail.archive_entries)}</pre>
          </details>
          <details className="detail-block">
            <summary>PAR2 Sets ({detail.par2_sets.length})</summary>
            <pre className="json-block">{stringifyJSON(detail.par2_sets)}</pre>
          </details>
          <details className="detail-block">
            <summary>Media Streams ({detail.media_streams.length})</summary>
            <pre className="json-block">{stringifyJSON(detail.media_streams)}</pre>
          </details>
          <details className="detail-block">
            <summary>Text Evidence ({detail.text_evidence.length})</summary>
            <pre className="json-block">{stringifyJSON(detail.text_evidence)}</pre>
          </details>
          <details className="detail-block">
            <summary>Grouping Evidence</summary>
            <pre className="json-block">{stringifyJSON(detail.grouping_evidence_json)}</pre>
          </details>
        </div>
      </div>
    </div>
  )
}
