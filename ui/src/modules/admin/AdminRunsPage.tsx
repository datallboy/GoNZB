import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { getAdminRuns } from '../../shared/api/admin'
import { formatDateTime, formatNumber } from '../../shared/lib/format'
import type { AdminRun } from '../../shared/types'

export function AdminRunsPage() {
  const [stage, setStage] = useState('')
  const [submittedStage, setSubmittedStage] = useState('')
  const [runs, setRuns] = useState<AdminRun[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getAdminRuns(submittedStage)
      .then((response) => {
        setRuns(response.items)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load runs'))
  }, [submittedStage])

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmittedStage(stage)
  }

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <p className="eyebrow">Run History</p>
        <h1 className="page-title">Recent stage runs and lease activity.</h1>
        <form className="toolbar-grid" onSubmit={handleSubmit}>
          <label className="field">
            <span>Stage Filter</span>
            <input
              value={stage}
              onChange={(event) => setStage(event.target.value)}
              placeholder="scrape_latest"
            />
          </label>
          <button className="primary-button align-end" type="submit">
            Filter Runs
          </button>
        </form>
      </div>
      {error ? <div className="banner error">{error}</div> : null}
      <div className="page-card">
        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <th>Stage</th>
                <th>Status</th>
                <th>Owner</th>
                <th>Started</th>
                <th>Finished</th>
                <th>Run ID</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((run) => (
                <tr key={run.id}>
                  <td>{run.stage_name}</td>
                  <td>{run.status}</td>
                  <td>{run.claimed_by || 'n/a'}</td>
                  <td>{formatDateTime(run.started_at)}</td>
                  <td>{formatDateTime(run.finished_at)}</td>
                  <td>{formatNumber(run.id)}</td>
                </tr>
              ))}
              {runs.length === 0 ? (
                <tr>
                  <td colSpan={6}>
                    <div className="empty-state">No runs matched the current filter.</div>
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
