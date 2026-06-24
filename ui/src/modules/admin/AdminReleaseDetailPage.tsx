import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  getAdminRelease,
  getIndexerBinary,
  getIndexerFile,
  hideAdminRelease,
  patchAdminRelease,
  reenrichAdminRelease,
  reinspectAdminRelease,
  unhideAdminRelease,
} from '../../shared/api/admin'
import { formatBytes, formatDateTime, formatNumber, formatPercent, formatRuntime } from '../../shared/lib/format'
import type { AdminBinaryDetail, AdminFileDetail, AdminReleaseDetailResponse, ReleaseOverridePatch } from '../../shared/types'

function stringifyJSON(value: unknown) {
  if (value == null) return 'None'
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function fileKindLabel(fileName: string, isPars: boolean) {
  const name = fileName.toLowerCase()
  if (name.endsWith('.nzb')) return 'Posted NZB Sidecar'
  if (isPars || name.endsWith('.par2')) return 'PAR2'
  if (name.endsWith('.nfo')) return 'NFO'
  if (name.match(/\.(rar|zip|7z|tar|gz|bz2|xz)$/)) return 'Archive'
  if (name.match(/\.(mkv|mp4|avi|m2ts|ts|mp3|flac)$/)) return 'Media'
  return 'Payload'
}

function binaryEvidenceSummary(binary: AdminReleaseDetailResponse['binaries'][number]) {
  const items = [
    `${binary.inspections.length} inspections`,
    `${binary.artifacts.length} artifacts`,
    `${binary.archive_entries.length} archive entries`,
    `${binary.media_streams.length} media streams`,
    `${binary.text_evidence.length} text evidence`,
    `${binary.par2_sets.length} PAR2 sets`,
  ]
  return items.join(' · ')
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

function payloadCompletionLabel(diagnostics: AdminReleaseDetailResponse['release']['diagnostics']) {
  if (!diagnostics.payload_completeness_known) return 'Unknown'
  return formatPercent(diagnostics.payload_completion_pct)
}

function payloadCompleteLabel(diagnostics: AdminReleaseDetailResponse['release']['diagnostics']) {
  if (!diagnostics.payload_completeness_known) return 'Unknown'
  return diagnostics.payload_complete ? 'Yes' : 'No'
}

export function AdminReleaseDetailPage() {
  const { id = '' } = useParams()
  const [data, setData] = useState<AdminReleaseDetailResponse | null>(null)
  const [form, setForm] = useState<ReleaseOverridePatch>({})
  const [fileDetailsByID, setFileDetailsByID] = useState<Record<number, AdminFileDetail>>({})
  const [binaryDetailsByID, setBinaryDetailsByID] = useState<Record<number, AdminBinaryDetail>>({})
  const [loadingFileIDs, setLoadingFileIDs] = useState<Record<number, boolean>>({})
  const [loadingBinaryIDs, setLoadingBinaryIDs] = useState<Record<number, boolean>>({})
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      const response = await getAdminRelease(id)
      setData(response)
      setFileDetailsByID({})
      setBinaryDetailsByID({})
      setLoadingFileIDs({})
      setLoadingBinaryIDs({})
      setForm({
        display_title: response.override?.display_title ?? '',
        classification_override: response.override?.classification_override ?? '',
        tmdb_id_override: response.override?.tmdb_id_override ?? 0,
        tvdb_id_override: response.override?.tvdb_id_override ?? 0,
        imdb_id_override: response.override?.imdb_id_override ?? '',
        notes: response.override?.notes ?? '',
        tags: response.override?.tags ?? [],
        hidden: response.override?.hidden ?? false,
      })
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load release')
    }
  }

  useEffect(() => {
    void refresh()
  }, [id])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setMessage(null)
    try {
      await patchAdminRelease(id, form)
      await refresh()
      setMessage('Override saved.')
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to save override')
    }
  }

  async function toggleHidden(nextHidden: boolean) {
    setMessage(null)
    try {
      if (nextHidden) {
        await hideAdminRelease(id)
      } else {
        await unhideAdminRelease(id)
      }
      await refresh()
      setMessage(nextHidden ? 'Release hidden.' : 'Release unhidden.')
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to update visibility')
    }
  }

  async function handleAction(action: 'reinspect' | 'reenrich') {
    setMessage(null)
    try {
      if (action === 'reinspect') {
        await reinspectAdminRelease(id)
      } else {
        await reenrichAdminRelease(id)
      }
      setMessage(`${action} accepted.`)
    } catch (err) {
      setMessage(err instanceof Error ? err.message : `Failed to ${action} release`)
    }
  }

  async function loadFileDetail(fileID: number) {
    if (fileID <= 0 || fileDetailsByID[fileID] || loadingFileIDs[fileID]) return
    setLoadingFileIDs((current) => ({ ...current, [fileID]: true }))
    try {
      const detail = await getIndexerFile(fileID)
      setFileDetailsByID((current) => ({ ...current, [fileID]: detail }))
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to load file detail')
    } finally {
      setLoadingFileIDs((current) => ({ ...current, [fileID]: false }))
    }
  }

  async function loadBinaryDetail(binaryID: number) {
    if (binaryID <= 0 || binaryDetailsByID[binaryID] || loadingBinaryIDs[binaryID]) return
    setLoadingBinaryIDs((current) => ({ ...current, [binaryID]: true }))
    try {
      const detail = await getIndexerBinary(binaryID)
      setBinaryDetailsByID((current) => ({ ...current, [binaryID]: detail }))
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to load binary detail')
    } finally {
      setLoadingBinaryIDs((current) => ({ ...current, [binaryID]: false }))
    }
  }

  if (error) {
    return <div className="banner error">{error}</div>
  }
  if (!data) {
    return <div className="banner">Loading release...</div>
  }

  const release = data.release.release
  const diagnostics = data.release.diagnostics
  const subtitleLanguages = release.subtitle_languages ?? []
  const mediaTags = release.media_tags ?? []
  const archiveState = release.nzb_generation_status || 'pending'
  const isArchivedOrPurged =
    archiveState === 'archived' ||
    archiveState === 'purge_pending' ||
    archiveState === 'purged'
  const releaseFileRows = (data.release.files ?? []).map((summary) => {
    const detail = summary.file_id > 0 ? fileDetailsByID[summary.file_id] : undefined
    return { summary, detail }
  })
  const loadedBinaries = Object.values(binaryDetailsByID)
  const hasBinaryDetail = loadedBinaries.length > 0

  return (
    <div className="page-section stack">
      <div className="page-card">
        <div className="page-header">
          <div>
            <p className="eyebrow">Admin Release Detail</p>
            <h1 className="page-title">{release.title}</h1>
            <p className="muted-copy">
              {release.group_name} · {release.release_id} · posted {formatDateTime(release.posted_at)}
            </p>
          </div>
          <div className="button-row">
            <Link className="secondary-button" to="/admin/indexer/releases">
              Back to Admin Releases
            </Link>
          </div>
        </div>
      </div>

      <div className="dashboard-grid">
        <div className="page-card">
          <h2 className="section-title">Release Summary</h2>
          {diagnostics.readiness_note ? <div className="banner">{diagnostics.readiness_note}</div> : null}
          <dl className="detail-grid">
            <div>
              <dt>Known Binary Completion</dt>
              <dd>{formatPercent(diagnostics.known_binary_completion_pct)}</dd>
            </div>
            <div>
              <dt>Payload Completion</dt>
              <dd>{payloadCompletionLabel(diagnostics)}</dd>
            </div>
            <div>
              <dt>Identity</dt>
              <dd>{release.identity_status || 'unknown'}</dd>
            </div>
            <div>
              <dt>Password</dt>
              <dd>{release.password_state || 'unknown'}</dd>
            </div>
            <div>
              <dt>Availability</dt>
              <dd>{release.availability_tier || 'unknown'}</dd>
            </div>
            <div>
              <dt>Quality</dt>
              <dd>{release.media_quality_tier || 'unknown'}</dd>
            </div>
            <div>
              <dt>Size</dt>
              <dd>{formatBytes(release.size_bytes)}</dd>
            </div>
            <div>
              <dt>Runtime</dt>
              <dd>{formatRuntime(release.runtime_seconds)}</dd>
            </div>
            <div>
              <dt>NZB Cache</dt>
              <dd>{formatNZBStatus(release.nzb_generation_status || 'pending')}</dd>
            </div>
            <div>
              <dt>Archive State</dt>
              <dd>{isArchivedOrPurged ? formatNZBStatus(archiveState) : 'Not archived yet'}</dd>
            </div>
            <div>
              <dt>Payload Complete</dt>
              <dd>{payloadCompleteLabel(diagnostics)}</dd>
            </div>
            <div>
              <dt>Expected Files Complete</dt>
              <dd>{diagnostics.expected_file_count_complete ? 'Yes' : 'No'}</dd>
            </div>
            <div>
              <dt>Expected Archive Files</dt>
              <dd>{diagnostics.expected_archive_file_count_known ? release.expected_archive_file_count : 'Unknown'}</dd>
            </div>
            <div>
              <dt>Missing Expected Files</dt>
              <dd>{diagnostics.missing_expected_file_count}</dd>
            </div>
            <div>
              <dt>Missing Payload Files</dt>
              <dd>
                {diagnostics.expected_archive_file_count_known
                  ? diagnostics.missing_expected_archive_file_count
                  : 'Unknown'}
              </dd>
            </div>
            <div>
              <dt>Metadata Updated</dt>
              <dd>{formatDateTime(release.metadata_updated_at)}</dd>
            </div>
            <div>
              <dt>Newsgroups</dt>
              <dd>{data.release.newsgroups.join(', ') || 'None'}</dd>
            </div>
            <div>
              <dt>Subtitles</dt>
              <dd>{subtitleLanguages.join(', ') || 'None'}</dd>
            </div>
            <div>
              <dt>Media Tags</dt>
              <dd>{mediaTags.join(', ') || 'None'}</dd>
            </div>
          </dl>
        </div>

        <div className="page-card">
          <h2 className="section-title">Matched Metadata</h2>
          <dl className="detail-grid">
            <div>
              <dt>Source Title</dt>
              <dd>{release.source_title || 'None'}</dd>
            </div>
            <div>
              <dt>Deobfuscated</dt>
              <dd>{release.deobfuscated_title || 'None'}</dd>
            </div>
            <div>
              <dt>Matched Media</dt>
              <dd>{release.matched_media_title || 'None'}</dd>
            </div>
            <div>
              <dt>Original Media</dt>
              <dd>{release.original_media_title || 'None'}</dd>
            </div>
            <div>
              <dt>TMDb</dt>
              <dd>{release.tmdb_id || 'None'}</dd>
            </div>
            <div>
              <dt>TVDb</dt>
              <dd>{release.tvdb_id || 'None'}</dd>
            </div>
            <div>
              <dt>Media Type</dt>
              <dd>{release.external_media_type || 'Unknown'}</dd>
            </div>
            <div>
              <dt>Season / Episode</dt>
              <dd>
                {release.season_number || release.episode_number
                  ? `S${formatNumber(release.season_number)} E${formatNumber(release.episode_number)}`
                  : 'None'}
              </dd>
            </div>
            <div>
              <dt>Title Source</dt>
              <dd>{release.title_source || 'Unknown'}</dd>
            </div>
            <div>
              <dt>Title Confidence</dt>
              <dd>{release.title_confidence ? release.title_confidence.toFixed(2) : '0.00'}</dd>
            </div>
          </dl>
        </div>
      </div>

      <form className="page-card stack" onSubmit={handleSubmit}>
        <h2 className="section-title">Override Controls</h2>
        <div className="toolbar-grid">
          <label className="field">
            <span>Display Title</span>
            <input
              value={String(form.display_title ?? '')}
              onChange={(event) => setForm((current) => ({ ...current, display_title: event.target.value }))}
            />
          </label>
          <label className="field">
            <span>Classification Override</span>
            <input
              value={String(form.classification_override ?? '')}
              onChange={(event) =>
                setForm((current) => ({ ...current, classification_override: event.target.value }))
              }
            />
          </label>
          <label className="field">
            <span>IMDb</span>
            <input
              value={String(form.imdb_id_override ?? '')}
              onChange={(event) => setForm((current) => ({ ...current, imdb_id_override: event.target.value }))}
            />
          </label>
          <label className="field">
            <span>TMDb</span>
            <input
              type="number"
              value={Number(form.tmdb_id_override ?? 0)}
              onChange={(event) =>
                setForm((current) => ({ ...current, tmdb_id_override: Number(event.target.value) }))
              }
            />
          </label>
          <label className="field">
            <span>TVDb</span>
            <input
              type="number"
              value={Number(form.tvdb_id_override ?? 0)}
              onChange={(event) =>
                setForm((current) => ({ ...current, tvdb_id_override: Number(event.target.value) }))
              }
            />
          </label>
        </div>
        <label className="field">
          <span>Tags</span>
          <input
            value={Array.isArray(form.tags) ? form.tags.join(', ') : ''}
            onChange={(event) =>
              setForm((current) => ({
                ...current,
                tags: event.target.value
                  .split(',')
                  .map((value) => value.trim())
                  .filter(Boolean),
              }))
            }
          />
        </label>
        <label className="field">
          <span>Notes</span>
          <textarea
            rows={5}
            value={String(form.notes ?? '')}
            onChange={(event) => setForm((current) => ({ ...current, notes: event.target.value }))}
          />
        </label>
        <div className="button-row">
          <button className="secondary-button" type="button" onClick={() => void handleAction('reinspect')}>
            Reinspect Release
          </button>
          <button className="secondary-button" type="button" onClick={() => void handleAction('reenrich')}>
            Reenrich Release
          </button>
          <button
            className="secondary-button"
            type="button"
            onClick={() => toggleHidden(!(data.override?.hidden ?? false))}
          >
            {data.override?.hidden ? 'Unhide Release' : 'Hide Release'}
          </button>
          <button className="primary-button" type="submit">
            Save Override
          </button>
        </div>
        {message ? <div className="banner">{message}</div> : null}
      </form>

      <div className="page-card stack">
        <h2 className="section-title">Release-Level Evidence</h2>
        <div className="dashboard-grid">
          <div className="stack">
            <h3 className="section-subtitle">Inspection Runs</h3>
            {(data.release.inspections ?? []).map((inspection) => (
              <details className="detail-block" key={`${inspection.stage_name}-${inspection.binary_id}`}>
                <summary>
                  {inspection.stage_name} · {inspection.status}
                </summary>
                <div className="stack">
                  <div className="muted-row">
                    <span>Binary {inspection.binary_id}</span>
                    <span>Updated {formatDateTime(inspection.updated_at)}</span>
                  </div>
                  {inspection.error_text ? <div className="banner error">{inspection.error_text}</div> : null}
                  <pre className="json-block">{stringifyJSON(inspection.summary_json)}</pre>
                </div>
              </details>
            ))}
          </div>
          <div className="stack">
            <h3 className="section-subtitle">Password Candidates</h3>
            {(data.release.password_candidates ?? []).map((candidate) => (
              <div className="list-row list-row--start" key={candidate.id}>
                <div>
                  <strong>{candidate.source_kind}</strong>
                  <div className="muted-row">
                    <span>{candidate.source_ref || 'no source ref'}</span>
                    <span>{candidate.verification_status}</span>
                  </div>
                </div>
                <span>{candidate.confidence.toFixed(2)}</span>
              </div>
            ))}
          </div>
        </div>
        <div className="dashboard-grid">
          <div className="stack">
            <h3 className="section-subtitle">Predb Matches</h3>
            {(data.release.predb_matches ?? []).map((match) => (
              <details className="detail-block" key={`predb-${match.entry_id}`}>
                <summary>
                  {match.title} · {match.confidence.toFixed(2)}
                </summary>
                <pre className="json-block">{stringifyJSON(match.payload_json)}</pre>
              </details>
            ))}
          </div>
          <div className="stack">
            <h3 className="section-subtitle">External Matches</h3>
            {[...(data.release.tmdb_matches ?? []), ...(data.release.tvdb_matches ?? [])].map((match) => (
              <details className="detail-block" key={`${match.source}-${match.external_id}`}>
                <summary>
                  {match.source} · {match.title} · {match.confidence.toFixed(2)}
                </summary>
                <pre className="json-block">{stringifyJSON(match.payload_json)}</pre>
              </details>
            ))}
          </div>
        </div>
      </div>

      <div className="page-card stack">
        <div className="page-header">
          <div>
            <h2 className="section-title">Release Files</h2>
            <p className="muted-copy">Catalog view of the release payload. Expand a file to review its article segments.</p>
          </div>
        </div>
        {releaseFileRows.map(({ summary, detail }) => {
          const groups = detail?.newsgroups?.length ? detail.newsgroups : data.release.newsgroups
          return (
            <details
              className="detail-block"
              key={`${summary.file_index}-${summary.file_name}`}
              onToggle={(event) => {
                if (event.currentTarget.open && summary.file_id > 0) {
                  void loadFileDetail(summary.file_id)
                }
              }}
            >
              <summary>
                {summary.file_name} · {formatBytes(summary.size_bytes)} · {fileKindLabel(summary.file_name, summary.is_pars)}
              </summary>
              <div className="stack">
                <div className="muted-row">
                  <span>Index {summary.file_index}</span>
                  <span>
                    Parts {summary.observed_parts} / {summary.total_parts}
                  </span>
                  <span>Articles {summary.article_count}</span>
                  <span>Posted {formatDateTime(summary.posted_at)}</span>
                </div>
                <div className="muted-row">
                  <span>Binary {summary.binary_id || detail?.binary_id || 'not linked'}</span>
                  <span>{summary.match_status || 'unmatched'}</span>
                  <span>{summary.poster || detail?.poster || 'Unknown poster'}</span>
                  <span>{groups.join(', ') || 'No groups recorded'}</span>
                </div>
                {summary.binary_id <= 0 ? (
                  <div className="banner">
                    Catalog-only file. The source binary link is not currently available, so article segments and binary inspection details may be unavailable.
                  </div>
                ) : null}
                {summary.file_name.toLowerCase().endsWith('.nzb') ? (
                  <div className="banner">
                    Posted NZB sidecar. This is an uploaded companion NZB for the release set, not the generated cache NZB and not a required payload volume.
                  </div>
                ) : null}
                {summary.file_id > 0 && loadingFileIDs[summary.file_id] ? <div className="banner">Loading file detail...</div> : null}
                {detail ? (
                  <details className="detail-block detail-block--nested">
                    <summary>Article Segments ({detail.articles.length})</summary>
                    <div className="table-shell">
                      <table className="data-table">
                        <thead>
                          <tr>
                            <th>Part</th>
                            <th>Message ID</th>
                            <th>Bytes</th>
                          </tr>
                        </thead>
                        <tbody>
                          {detail.articles.map((article) => (
                            <tr key={`${detail.file_id}-${article.message_id}-${article.part_number}`}>
                              <td>{article.part_number}</td>
                              <td className="mono-cell">{article.message_id}</td>
                              <td>{formatBytes(article.bytes)}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </details>
                ) : null}
                {detail ? (
                  <details className="detail-block detail-block--nested">
                    <summary>Grouping Evidence</summary>
                    <pre className="json-block">{stringifyJSON(detail.grouping_evidence_json)}</pre>
                  </details>
                ) : null}
                {summary.binary_id > 0 ? (
                  <details
                    className="detail-block detail-block--nested"
                    onToggle={(event) => {
                      if (event.currentTarget.open) {
                        void loadBinaryDetail(summary.binary_id)
                      }
                    }}
                  >
                    <summary>
                      Binary Evidence
                      {binaryDetailsByID[summary.binary_id]
                        ? ` · ${binaryEvidenceSummary(binaryDetailsByID[summary.binary_id])}`
                        : ''}
                    </summary>
                    {loadingBinaryIDs[summary.binary_id] ? <div className="banner">Loading binary evidence...</div> : null}
                    {binaryDetailsByID[summary.binary_id] ? (
                      <pre className="json-block">
                        {stringifyJSON(binaryDetailsByID[summary.binary_id].grouping_evidence_json)}
                      </pre>
                    ) : null}
                  </details>
                ) : null}
              </div>
            </details>
          )
        })}
      </div>

      {hasBinaryDetail ? (
      <div className="page-card stack">
        <div className="page-header">
          <div>
            <h2 className="section-title">Binary Grouping and Evidence</h2>
            <p className="muted-copy">
              Source binary view of how the release was grouped and what downstream inspection stages derived from it.
            </p>
          </div>
        </div>
        {loadedBinaries.map((binary) => (
          <details className="detail-block" key={binary.binary_id}>
            <summary>
              {binary.binary_name} · {binary.match_status || 'unmatched'} · {binary.observed_parts}/{binary.total_parts} ·{' '}
              {binaryEvidenceSummary(binary)}
            </summary>
            <div className="stack">
              <dl className="detail-grid">
                <div>
                  <dt>File</dt>
                  <dd>{binary.file_name || 'Unknown'}</dd>
                </div>
                <div>
                  <dt>Posted</dt>
                  <dd>{formatDateTime(binary.posted_at)}</dd>
                </div>
                <div>
                  <dt>Total Bytes</dt>
                  <dd>{formatBytes(binary.total_bytes)}</dd>
                </div>
                <div>
                  <dt>Poster</dt>
                  <dd>{binary.poster || 'Unknown'}</dd>
                </div>
                <div>
                  <dt>Password State</dt>
                  <dd>{binary.password_state || 'unknown'}</dd>
                </div>
                <div>
                  <dt>Release Key</dt>
                  <dd>{binary.release_key || 'None'}</dd>
                </div>
                <div>
                  <dt>Binary Key</dt>
                  <dd>{binary.binary_key || 'None'}</dd>
                </div>
              </dl>

              <div className="dashboard-grid">
                <div className="stack">
                  <h3 className="section-subtitle">Inspection Runs</h3>
                  {binary.inspections.map((inspection) => (
                    <details className="detail-block detail-block--nested" key={`${binary.binary_id}-${inspection.stage_name}`}>
                      <summary>
                        {inspection.stage_name} · {inspection.status}
                      </summary>
                      <pre className="json-block">{stringifyJSON(inspection.summary_json)}</pre>
                    </details>
                  ))}
                </div>
                <div className="stack">
                  <h3 className="section-subtitle">Derived Artifacts</h3>
                  {binary.artifacts.map((artifact) => (
                    <div className="list-row list-row--start" key={`${binary.binary_id}-${artifact.artifact_path}`}>
                      <div>
                        <strong>{artifact.artifact_name || artifact.artifact_role}</strong>
                        <div className="muted-row">
                          <span>{artifact.stage_name}</span>
                          <span>{artifact.mime_type || 'unknown mime'}</span>
                        </div>
                      </div>
                      <span>{formatBytes(artifact.bytes_total)}</span>
                    </div>
                  ))}
                </div>
              </div>

              <div className="dashboard-grid">
                <div className="stack">
                  <h3 className="section-subtitle">Archive Contents</h3>
                  {binary.archive_entries.map((entry) => (
                    <div className="list-row list-row--start" key={`${binary.binary_id}-${entry.entry_name}`}>
                      <div>
                        <strong>{entry.entry_name}</strong>
                        <div className="muted-row">
                          <span>{entry.media_type || 'unknown'}</span>
                          <span>{entry.encrypted ? 'encrypted' : 'plain'}</span>
                        </div>
                      </div>
                      <span>{formatBytes(entry.uncompressed_bytes)}</span>
                    </div>
                  ))}
                </div>
                <div className="stack">
                  <h3 className="section-subtitle">Media Metadata</h3>
                  {binary.media_streams.map((stream) => (
                    <div className="list-row list-row--start" key={`${binary.binary_id}-${stream.stream_index}-${stream.stream_type}`}>
                      <div>
                        <strong>{stream.stream_type}</strong>
                        <div className="muted-row">
                          <span>{stream.codec_name || 'unknown codec'}</span>
                          <span>{stream.language || 'und'}</span>
                        </div>
                      </div>
                      <span>
                        {stream.width && stream.height ? `${stream.width}x${stream.height}` : stream.channels || '-'}
                      </span>
                    </div>
                  ))}
                </div>
              </div>

              <div className="dashboard-grid">
                <div className="stack">
                  <h3 className="section-subtitle">Text Evidence</h3>
                  {binary.text_evidence.map((entry, index) => (
                    <details className="detail-block detail-block--nested" key={`${binary.binary_id}-text-${index}`}>
                      <summary>
                        {entry.stage_name} · {entry.evidence_kind}
                      </summary>
                      <div className="stack">
                        <div>{entry.text_value}</div>
                        <div className="muted-row">
                          <span>{entry.tokens.join(', ') || 'no tokens'}</span>
                        </div>
                        <pre className="json-block">{stringifyJSON(entry.metadata_json)}</pre>
                      </div>
                    </details>
                  ))}
                </div>
                <div className="stack">
                  <h3 className="section-subtitle">PAR2 and Source Segments</h3>
                  {binary.par2_sets.map((set) => (
                    <div className="list-row list-row--start" key={`${binary.binary_id}-${set.set_name}`}>
                      <div>
                        <strong>{set.set_name}</strong>
                        <div className="muted-row">
                          <span>{set.base_name}</span>
                          <span>{set.signature_ok ? 'signature ok' : 'signature unknown'}</span>
                        </div>
                      </div>
                      <span>{set.recovery_blocks}</span>
                    </div>
                  ))}
                  <details className="detail-block detail-block--nested">
                    <summary>Source Segments ({binary.parts.length})</summary>
                    <div className="table-shell">
                      <table className="data-table">
                        <thead>
                          <tr>
                            <th>Part</th>
                            <th>Message ID</th>
                            <th>Bytes</th>
                          </tr>
                        </thead>
                        <tbody>
                          {binary.parts.map((part) => (
                            <tr key={`${binary.binary_id}-${part.article_header_id}`}>
                              <td>{part.part_number}</td>
                              <td className="mono-cell">{part.message_id}</td>
                              <td>{formatBytes(part.segment_bytes)}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </details>
                </div>
              </div>

              <pre className="json-block">{stringifyJSON(binary.grouping_evidence_json)}</pre>
            </div>
          </details>
        ))}
      </div>
      ) : isArchivedOrPurged ? (
        <div className="page-card">
          <h2 className="section-title">Binary Grouping and Evidence</h2>
          <div className="banner">
            This release has already been archived/purged. Source-level binary and inspection detail is no longer available here.
          </div>
        </div>
      ) : null}
    </div>
  )
}
