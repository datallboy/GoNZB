import { useCallback, useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  approveUploaderSubmission,
  getUploaderSubmission,
  getEligibleUploaderFederationPools,
  publishUploaderSubmission,
  rejectUploaderSubmission,
  returnUploaderSubmissionToPending,
  updateUploaderSubmission,
  withdrawUploaderPublication,
} from '../../shared/api/uploader'
import { apiURL } from '../../shared/api/http'
import { useAuth } from '../../shared/auth/useAuth'
import { formatBytes, formatDateTime } from '../../shared/lib/format'
import type { UploaderDetailResponse, UploaderUpdate } from '../../shared/types'

const categoryOptions = [
  [8010, 'Other > Misc'], [2030, 'Movies > SD'], [2040, 'Movies > HD'], [2045, 'Movies > UHD'],
  [5030, 'TV > SD'], [5040, 'TV > HD'], [5045, 'TV > UHD'], [5070, 'TV > Anime'],
  [3010, 'Audio > MP3'], [3040, 'Audio > Lossless'], [4020, 'PC > ISO'], [4050, 'PC > Games'],
  [7020, 'Books > Ebook'], [7030, 'Books > Comics'],
] as const

export function UploaderDetailPage() {
  const { id = '' } = useParams()
  const { hasPermission } = useAuth()
  const [data, setData] = useState<UploaderDetailResponse | null>(null)
  const [form, setForm] = useState<UploaderUpdate>({})
  const [note, setNote] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [message, setMessage] = useState<string | null>(null)
  const [eligiblePools, setEligiblePools] = useState<string[]>([])
  const canReview = hasPermission('uploader.submissions.review')
  const canManagePublications = hasPermission('uploader.publications.manage')

  const refresh = useCallback(async () => {
    try {
      const response = await getUploaderSubmission(id)
      setData(response)
      setForm({
        title: response.submission.title,
        category_id: response.submission.category_id,
        posted_at: response.submission.posted_at,
        password: response.submission.password ?? '',
        imdb_id: response.submission.imdb_id ?? '',
        tmdb_id: response.submission.tmdb_id ?? 0,
        tvdb_id: response.submission.tvdb_id ?? 0,
        year: response.submission.year ?? 0,
        resolution: response.submission.resolution ?? '',
        media_source: response.submission.media_source ?? '',
        video_codec: response.submission.video_codec ?? '',
        audio_codec: response.submission.audio_codec ?? '',
      })
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load submission')
    }
  }, [id])

  useEffect(() => {
    const timer = window.setTimeout(() => void refresh(), 0)
    return () => window.clearTimeout(timer)
  }, [refresh])

  useEffect(() => {
    if (!canManagePublications) return
    void getEligibleUploaderFederationPools()
      .then((response) => setEligiblePools(response.items))
      .catch(() => setEligiblePools([]))
  }, [canManagePublications])

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setMessage(null)
    try {
      await updateUploaderSubmission(id, { ...form, note })
      await refresh()
      setMessage('Review metadata saved.')
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to save metadata')
    }
  }

  async function transition(action: 'approve' | 'reject' | 'pending') {
    setMessage(null)
    try {
      if (action === 'approve') await approveUploaderSubmission(id, note)
      if (action === 'reject') await rejectUploaderSubmission(id, note)
      if (action === 'pending') await returnUploaderSubmissionToPending(id, note)
      await refresh()
      setMessage(`Submission moved to ${action === 'pending' ? 'pending review' : action + 'd'}.`)
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to change review state')
    }
  }

  async function publish(poolId: string) {
    setMessage(null)
    try {
      await publishUploaderSubmission(id, [poolId])
      await refresh()
      setMessage(`Published to ${poolId}.`)
    } catch (err) {
      await refresh()
      setMessage(err instanceof Error ? err.message : 'Federation publication failed')
    }
  }

  async function withdraw(poolId: string) {
    setMessage(null)
    try {
      await withdrawUploaderPublication(id, poolId, note)
      await refresh()
      setMessage(`Withdrawal published to ${poolId}.`)
    } catch (err) {
      await refresh()
      setMessage(err instanceof Error ? err.message : 'Federation withdrawal failed')
    }
  }

  if (error) return <div className="banner error">{error}</div>
  if (!data) return <div className="banner">Loading submission...</div>
  const item = data.submission

  return (
    <div className="page-section stack">
      <section className="page-card">
        <div className="page-header">
          <div>
            <p className="eyebrow">Uploader submission</p>
            <h1 className="page-title">{item.title}</h1>
            <p className="muted-copy mono-cell">{item.id} · {item.nzb_sha256}</p>
          </div>
          <div className="button-row">
            <span className="status-pill">{item.state.replaceAll('_', ' ')}</span>
            {canReview ? <a className="secondary-button" href={apiURL(`/api/v1/uploader/submissions/${item.id}/nzb`)}>Download NZB</a> : null}
            <Link className="secondary-button" to="/uploader">Back to queue</Link>
          </div>
        </div>
      </section>

      {message ? <div className="banner">{message}</div> : null}

      <section className="page-card">
        <h2 className="section-title">Derived NZB facts</h2>
        <dl className="detail-grid detail-grid--wide-values">
          <div><dt>Size</dt><dd>{formatBytes(item.size_bytes)}</dd></div>
          <div><dt>Files / segments</dt><dd>{item.file_count} / {item.segment_count}</dd></div>
          <div><dt>Posted</dt><dd>{formatDateTime(item.posted_at)}</dd></div>
          <div><dt>Poster</dt><dd className="breakable-value">{item.poster || 'Unknown'}</dd></div>
          <div><dt>Groups</dt><dd className="breakable-value">{item.groups.join(', ') || 'None'}</dd></div>
          <div><dt>Password</dt><dd>{item.has_password ? (item.password || 'Present, hidden') : 'None supplied'}</dd></div>
          <div><dt>PAR2 / NFO</dt><dd>{item.has_par2 ? 'PAR2' : 'No PAR2 detected'} · {item.has_nfo ? 'NFO' : 'No NFO detected'}</dd></div>
          <div><dt>Origin</dt><dd>{item.provenance_tool || item.intake_kind}</dd></div>
        </dl>
      </section>

      {canReview ? (
        <form className="page-card stack" onSubmit={save}>
          <div><p className="eyebrow">Reviewer controls</p><h2 className="section-title">Catalog metadata</h2></div>
          <div className="toolbar-grid toolbar-grid--compact">
            <label className="field"><span>Title</span><input required disabled={item.state !== 'pending_review'} value={form.title ?? ''} onChange={(event) => setForm({ ...form, title: event.target.value })} /></label>
            <label className="field"><span>Category</span><select disabled={item.state !== 'pending_review'} value={form.category_id ?? 8010} onChange={(event) => setForm({ ...form, category_id: Number(event.target.value) })}>{categoryOptions.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
            <label className="field"><span>Posted at</span><input disabled={item.state !== 'pending_review'} value={form.posted_at ?? ''} onChange={(event) => setForm({ ...form, posted_at: event.target.value })} /></label>
            <label className="field"><span>Archive password</span><input disabled={item.state !== 'pending_review'} value={form.password ?? ''} onChange={(event) => setForm({ ...form, password: event.target.value })} /></label>
            <label className="field"><span>IMDb ID</span><input disabled={item.state !== 'pending_review'} value={form.imdb_id ?? ''} onChange={(event) => setForm({ ...form, imdb_id: event.target.value })} /></label>
            <label className="field"><span>TMDB ID</span><input type="number" disabled={item.state !== 'pending_review'} value={form.tmdb_id ?? 0} onChange={(event) => setForm({ ...form, tmdb_id: Number(event.target.value) })} /></label>
            <label className="field"><span>TVDB ID</span><input type="number" disabled={item.state !== 'pending_review'} value={form.tvdb_id ?? 0} onChange={(event) => setForm({ ...form, tvdb_id: Number(event.target.value) })} /></label>
            <label className="field"><span>Year</span><input type="number" disabled={item.state !== 'pending_review'} value={form.year ?? 0} onChange={(event) => setForm({ ...form, year: Number(event.target.value) })} /></label>
            <label className="field"><span>Resolution</span><input disabled={item.state !== 'pending_review'} value={form.resolution ?? ''} onChange={(event) => setForm({ ...form, resolution: event.target.value })} /></label>
          </div>
          <label className="field"><span>Review note</span><textarea value={note} onChange={(event) => setNote(event.target.value)} /></label>
          <div className="button-row">
            {item.state === 'pending_review' ? <button className="secondary-button" type="submit">Save metadata</button> : <span className="muted-copy">Return to pending before editing.</span>}
            <div className="button-row">
              {item.state === 'pending_review' ? <button className="primary-button" type="button" onClick={() => void transition('approve')}>Approve locally</button> : null}
              {item.state === 'pending_review' ? <button className="danger-button" type="button" onClick={() => void transition('reject')}>Reject</button> : null}
              {item.state !== 'pending_review' ? <button className="secondary-button" type="button" onClick={() => void transition('pending')}>Return to pending</button> : null}
            </div>
          </div>
        </form>
      ) : null}

      {canManagePublications ? (
        <section className="page-card stack">
          <div>
            <p className="eyebrow">Explicit publication</p>
            <h2 className="section-title">GoNZBNet pools</h2>
            <p className="muted-copy">Local approval does not publish automatically. Each pool is selected independently.</p>
          </div>
          {eligiblePools.length === 0 ? <div className="banner">No eligible pools are currently available.</div> : (
            <div className="button-row">
              {eligiblePools.map((poolId) => {
                const publication = data.federation_publications?.find((candidate) => candidate.pool_id === poolId)
                const canPublish = item.state === 'approved' && (!publication || publication.state === 'withdrawn' || publication.state === 'failed')
                return (
                  <div className="button-row" key={poolId}>
                    <span className="status-pill">{poolId}: {publication?.state.replaceAll('_', ' ') ?? 'not published'}</span>
                    {canPublish ? <button className="primary-button" type="button" onClick={() => void publish(poolId)}>{publication?.state === 'withdrawn' ? 'Restore' : 'Publish'}</button> : null}
                    {publication?.state === 'published' ? <button className="danger-button" type="button" onClick={() => void withdraw(poolId)}>Withdraw</button> : null}
                  </div>
                )
              })}
            </div>
          )}
          {(data.federation_publications ?? []).some((publication) => publication.last_error) ? (
            <div className="banner error">
              {(data.federation_publications ?? []).filter((publication) => publication.last_error).map((publication) => `${publication.pool_id}: ${publication.last_error}`).join(' · ')}
            </div>
          ) : null}
        </section>
      ) : null}

      <section className="page-card stack">
        <h2 className="section-title">NZB files</h2>
        <div className="table-scroll"><table className="data-table data-table--compact"><thead><tr><th>Name / subject</th><th>Groups</th><th>Size</th><th>Segments</th></tr></thead><tbody>
          {(item.files ?? []).map((file, index) => <tr key={`${file.subject}-${index}`}><td>{file.name || 'Obfuscated'}<div className="muted-copy breakable-value">{file.subject}</div></td><td>{file.groups.join(', ')}</td><td>{formatBytes(file.size_bytes)}</td><td>{file.segment_count}</td></tr>)}
        </tbody></table></div>
      </section>

      {(item.artifacts ?? []).length > 0 ? (
        <section className="page-card stack">
          <h2 className="section-title">Review artifacts</h2>
          <div className="table-scroll"><table className="data-table data-table--compact"><thead><tr><th>Artifact</th><th>Kind</th><th>Media type</th><th>Size</th><th /></tr></thead><tbody>
            {(item.artifacts ?? []).map((artifact) => <tr key={artifact.id}><td>{artifact.label || artifact.original_filename}<div className="muted-copy mono-cell">{artifact.sha256}</div></td><td>{artifact.kind}</td><td>{artifact.detected_media_type}</td><td>{formatBytes(artifact.size_bytes)}</td><td><a className="secondary-button" href={apiURL(`/api/v1/uploader/submissions/${item.id}/artifacts/${artifact.id}`)}>Download</a></td></tr>)}
          </tbody></table></div>
        </section>
      ) : null}

      <section className="page-card stack">
        <h2 className="section-title">Audit history</h2>
        <div className="table-scroll"><table className="data-table data-table--compact"><thead><tr><th>Event</th><th>Actor</th><th>State</th><th>Note</th><th>At</th></tr></thead><tbody>
          {data.events.map((event) => <tr key={event.id}><td>{event.event_type.replaceAll('_', ' ')}</td><td>{event.actor}</td><td>{event.prior_state || '—'} → {event.next_state || '—'}</td><td>{event.note || '—'}</td><td>{formatDateTime(event.created_at)}</td></tr>)}
        </tbody></table></div>
      </section>
    </div>
  )
}
