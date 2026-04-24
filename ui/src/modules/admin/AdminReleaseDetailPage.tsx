import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { useParams } from 'react-router-dom'
import {
  getAdminRelease,
  hideAdminRelease,
  patchAdminRelease,
  reenrichAdminRelease,
  reinspectAdminRelease,
  unhideAdminRelease,
} from '../../shared/api/admin'
import { formatDateTime } from '../../shared/lib/format'
import type { AdminReleaseDetailResponse, ReleaseOverridePatch } from '../../shared/types'

export function AdminReleaseDetailPage() {
  const { id = '' } = useParams()
  const [data, setData] = useState<AdminReleaseDetailResponse | null>(null)
  const [form, setForm] = useState<ReleaseOverridePatch>({})
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

  if (error) {
    return <div className="banner error">{error}</div>
  }
  if (!data) {
    return <div className="banner">Loading release...</div>
  }

  return (
    <div className="page-section stack">
      <div className="page-card">
        <p className="eyebrow">Admin Release Detail</p>
        <h1 className="page-title">{data.release.release.title}</h1>
        <p className="muted-copy">
          Posted {formatDateTime(data.release.release.posted_at)} · Override updated{' '}
          {formatDateTime(data.override?.updated_at)}
        </p>
      </div>

      <div className="dashboard-grid">
        <div className="page-card">
          <h2 className="section-title">Current Override</h2>
          <dl className="detail-grid">
            <div>
              <dt>Display Title</dt>
              <dd>{data.override?.display_title || 'None'}</dd>
            </div>
            <div>
              <dt>Classification</dt>
              <dd>{data.override?.classification_override || 'None'}</dd>
            </div>
            <div>
              <dt>Hidden</dt>
              <dd>{data.override?.hidden ? 'Yes' : 'No'}</dd>
            </div>
            <div>
              <dt>Tags</dt>
              <dd>{(data.override?.tags ?? []).join(', ') || 'None'}</dd>
            </div>
          </dl>
        </div>

        <form className="page-card stack" onSubmit={handleSubmit}>
          <h2 className="section-title">Edit Override</h2>
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
          <div className="toolbar-grid">
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
            <label className="field">
              <span>IMDb</span>
              <input
                value={String(form.imdb_id_override ?? '')}
                onChange={(event) =>
                  setForm((current) => ({ ...current, imdb_id_override: event.target.value }))
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
              rows={6}
              value={String(form.notes ?? '')}
              onChange={(event) => setForm((current) => ({ ...current, notes: event.target.value }))}
            />
          </label>
          <div className="button-row">
            <button
              className="secondary-button"
              type="button"
              onClick={() => void handleAction('reinspect')}
            >
              Reinspect Release
            </button>
            <button
              className="secondary-button"
              type="button"
              onClick={() => void handleAction('reenrich')}
            >
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
      </div>
    </div>
  )
}
