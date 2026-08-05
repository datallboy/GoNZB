import { useCallback, useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { createUploaderSubmission, listUploaderSubmissions } from '../../shared/api/uploader'
import { useAuth } from '../../shared/auth/useAuth'
import { formatBytes, formatDateTime } from '../../shared/lib/format'
import type { UploaderListResponse } from '../../shared/types'

export function UploaderListPage() {
  const { hasPermission } = useAuth()
  const [data, setData] = useState<UploaderListResponse | null>(null)
  const [query, setQuery] = useState('')
  const [state, setState] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [title, setTitle] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [message, setMessage] = useState<string | null>(null)
  const canCreate = hasPermission('uploader.submissions.create')

  const refresh = useCallback(async () => {
    try {
      const response = await listUploaderSubmissions({ q: query, state })
      setData(response)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load uploader submissions')
    }
  }, [query, state])

  useEffect(() => {
    const timer = window.setTimeout(() => void refresh(), 0)
    return () => window.clearTimeout(timer)
  }, [refresh])

  async function upload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!file) return
    setMessage(null)
    try {
      const response = await createUploaderSubmission(file, { title: title.trim() || undefined })
      setMessage(response.created === false ? 'This NZB was already submitted.' : 'NZB added to the review queue.')
      setFile(null)
      setTitle('')
      const input = event.currentTarget.elements.namedItem('nzb')
      if (input instanceof HTMLInputElement) input.value = ''
      await refresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to upload NZB')
    }
  }

  function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void refresh()
  }

  return (
    <div className="page-section stack">
      <section className="page-card">
        <div className="page-header">
          <div>
            <p className="eyebrow">Completed NZB intake</p>
            <h1 className="page-title">Uploader Review Queue</h1>
            <p className="muted-copy">Accept completed NZBs from any producer. GoNZB does not receive torrents, magnet links, or source payload paths.</p>
          </div>
          <span className="status-pill">{data?.count ?? 0} submissions</span>
        </div>
      </section>

      {canCreate ? (
        <form className="page-card stack" onSubmit={upload}>
          <div>
            <p className="eyebrow">Manual handoff</p>
            <h2 className="section-title">Upload a completed NZB</h2>
          </div>
          <div className="toolbar-grid toolbar-grid--compact">
            <label className="field">
              <span>NZB file</span>
              <input name="nzb" type="file" accept=".nzb,application/x-nzb,application/xml,text/xml" required onChange={(event) => setFile(event.target.files?.[0] ?? null)} />
            </label>
            <label className="field">
              <span>Display title override (optional)</span>
              <input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Derived from NZB metadata or filename" />
            </label>
          </div>
          <div className="button-row">
            <span className="muted-copy">Every accepted NZB starts in pending review.</span>
            <button className="primary-button" type="submit" disabled={!file}>Add to queue</button>
          </div>
          {message ? <div className="banner">{message}</div> : null}
        </form>
      ) : null}

      <section className="page-card stack">
        <form className="toolbar-grid toolbar-grid--compact" onSubmit={search}>
          <label className="field">
            <span>Search</span>
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Title, filename, or producer" />
          </label>
          <label className="field">
            <span>State</span>
            <select value={state} onChange={(event) => setState(event.target.value)}>
              <option value="">All states</option>
              <option value="pending_review">Pending review</option>
              <option value="approved">Approved</option>
              <option value="rejected">Rejected</option>
            </select>
          </label>
          <button className="secondary-button" type="submit">Apply filters</button>
        </form>
        {error ? <div className="banner error">{error}</div> : null}
        {!data ? <div className="banner">Loading submissions...</div> : null}
        {data && data.items.length === 0 ? <div className="banner">No submissions match this view.</div> : null}
        {data && data.items.length > 0 ? (
          <div className="table-scroll">
            <table className="data-table">
              <thead><tr><th>Release</th><th>State</th><th>Origin</th><th>Category</th><th>Size</th><th>Posted</th></tr></thead>
              <tbody>
                {data.items.map((item) => (
                  <tr key={item.id}>
                    <td><Link className="table-link" to={`/uploader/${item.id}`}>{item.title}</Link><div className="muted-copy mono-cell">{item.original_filename}</div></td>
                    <td><span className="status-pill status-pill--table">{item.state.replaceAll('_', ' ')}</span></td>
                    <td>{item.provenance_tool || item.intake_kind}</td>
                    <td>{item.category}</td>
                    <td>{formatBytes(item.size_bytes)}</td>
                    <td>{formatDateTime(item.posted_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>
    </div>
  )
}
