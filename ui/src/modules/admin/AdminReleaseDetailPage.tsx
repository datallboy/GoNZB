import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  getAdminRelease,
  hideAdminRelease,
  identifyAdminRelease,
  patchAdminRelease,
  reenrichAdminRelease,
  reinspectAdminRelease,
  unhideAdminRelease,
} from '../../shared/api/admin'
import { formatBytes, formatDateTime, formatNumber, formatPercent, formatRuntime } from '../../shared/lib/format'
import type { AdminReleaseDetailResponse, ReleaseOverridePatch } from '../../shared/types'

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

function formatDeltaMinutes(value?: number) {
  if (value == null || Number.isNaN(value)) return 'Unknown'
  if (value < 60) return `${value.toFixed(1)} min`
  return `${(value / 60).toFixed(1)} hr`
}

export function AdminReleaseDetailPage() {
  const { id = '' } = useParams()
  const [data, setData] = useState<AdminReleaseDetailResponse | null>(null)
  const [form, setForm] = useState<ReleaseOverridePatch>({})
  const [identityForm, setIdentityForm] = useState({
    title: '',
    external_media_type: '',
    external_year: '',
    season_number: '',
    episode_number: '',
    classification: '',
  })
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      const response = await getAdminRelease(id)
      setData(response)
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
      setIdentityForm((current) => ({
        title: current.title || response.release.release.title || '',
        external_media_type: current.external_media_type || response.release.release.external_media_type || '',
        external_year: current.external_year || String(response.release.release.external_year || ''),
        season_number: current.season_number || String(response.release.release.season_number || ''),
        episode_number: current.episode_number || String(response.release.release.episode_number || ''),
        classification: current.classification || response.release.release.classification || '',
      }))
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

  async function handleManualIdentity(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setMessage(null)
    try {
      await identifyAdminRelease(id, {
        source: 'manual',
        title: identityForm.title,
        external_media_type: identityForm.external_media_type || undefined,
        external_year: Number(identityForm.external_year || 0) || undefined,
        season_number: Number(identityForm.season_number || 0) || undefined,
        episode_number: Number(identityForm.episode_number || 0) || undefined,
        classification: identityForm.classification || undefined,
      })
      await refresh()
      setMessage('Manual identity applied.')
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to apply manual identity')
    }
  }

  async function choosePredbCandidate(entryID: number) {
    setMessage(null)
    try {
      await identifyAdminRelease(id, { source: 'predb', predb_entry_id: entryID })
      await refresh()
      setMessage('PreDB candidate applied.')
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to apply PreDB candidate')
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
  const releaseFileRows = data.release.files ?? []

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
              <dt>Overall Binary Completion</dt>
              <dd>{formatPercent(diagnostics.known_binary_completion_pct)}</dd>
            </div>
            <div>
              <dt>Main Payload Completion</dt>
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

      <form className="page-card stack" onSubmit={handleManualIdentity}>
        <h2 className="section-title">Manual Identity</h2>
        <div className="toolbar-grid">
          <label className="field">
            <span>Title</span>
            <input
              value={identityForm.title}
              onChange={(event) => setIdentityForm((current) => ({ ...current, title: event.target.value }))}
            />
          </label>
          <label className="field">
            <span>Media Type</span>
            <input
              value={identityForm.external_media_type}
              onChange={(event) =>
                setIdentityForm((current) => ({ ...current, external_media_type: event.target.value }))
              }
            />
          </label>
          <label className="field">
            <span>Year</span>
            <input
              type="number"
              value={identityForm.external_year}
              onChange={(event) => setIdentityForm((current) => ({ ...current, external_year: event.target.value }))}
            />
          </label>
          <label className="field">
            <span>Season</span>
            <input
              type="number"
              value={identityForm.season_number}
              onChange={(event) => setIdentityForm((current) => ({ ...current, season_number: event.target.value }))}
            />
          </label>
          <label className="field">
            <span>Episode</span>
            <input
              type="number"
              value={identityForm.episode_number}
              onChange={(event) => setIdentityForm((current) => ({ ...current, episode_number: event.target.value }))}
            />
          </label>
          <label className="field">
            <span>Classification</span>
            <input
              value={identityForm.classification}
              onChange={(event) => setIdentityForm((current) => ({ ...current, classification: event.target.value }))}
            />
          </label>
        </div>
        <div className="button-row">
          <button className="primary-button" type="submit">
            Apply Manual Identity
          </button>
        </div>
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
                  {match.chosen ? 'Chosen · ' : ''}
                  {match.title} · {match.confidence.toFixed(2)}
                </summary>
                <div className="stack">
                  <div className="muted-row">
                    <span>{match.category || 'Uncategorized'}</span>
                    <span>Posted {formatDateTime(match.posted_at)}</span>
                    <span>Delta {formatDeltaMinutes(match.posted_delta_minutes)}</span>
                  </div>
                  <div className="muted-row">
                    <span>Payload {formatBytes(match.payload_size_bytes)} from {match.payload_size_source || 'unknown'}</span>
                    <span>PreDB {formatBytes(match.predb_size_bytes)}</span>
                    <span>Size delta {(match.size_delta_pct * 100).toFixed(1)}%</span>
                  </div>
                  <div className="muted-row">
                    <span>Resolution {match.resolution_match ? 'match' : 'miss'}</span>
                    <span>Video {match.video_codec_match ? 'match' : 'miss'}</span>
                    <span>Audio {match.audio_codec_match ? 'match' : 'miss'}</span>
                  </div>
                  {match.auto_apply_skip_reason ? (
                    <div className="banner">{match.auto_apply_skip_reason}</div>
                  ) : null}
                  <div className="button-row">
                    <button className="secondary-button" type="button" onClick={() => void choosePredbCandidate(match.entry_id)}>
                      Use Candidate
                    </button>
                  </div>
                  <pre className="json-block">{stringifyJSON(match.payload_json)}</pre>
                </div>
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
        {releaseFileRows.map((summary) => {
          const groups = data.release.newsgroups
          return (
            <details className="detail-block" key={`${summary.file_index}-${summary.file_name}`}>
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
                  <span>Binary {summary.binary_id || 'not linked'}</span>
                  <span>{summary.match_status || 'unmatched'}</span>
                  <span>{summary.poster || 'Unknown poster'}</span>
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
                {summary.binary_id > 0 ? (
                  <div className="button-row">
                    <Link className="secondary-button" to={`/admin/indexer/binaries/${summary.binary_id}`}>
                      Open Binary Detail
                    </Link>
                  </div>
                ) : null}
              </div>
            </details>
          )
        })}
      </div>
    </div>
  )
}
