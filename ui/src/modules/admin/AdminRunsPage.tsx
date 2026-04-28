import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getAdminRuns } from '../../shared/api/admin'
import { formatDateTime, formatNumber } from '../../shared/lib/format'
import type { AdminRun, AdminRunListParams } from '../../shared/types'
import { runStatusOptions, runTriggerOptions, stageOptions } from './adminData'

export function AdminRunsPage() {
  const [filters, setFilters] = useState<AdminRunListParams>({
    stage: '',
    status: '',
    trigger_kind: '',
  })
  const [runs, setRuns] = useState<AdminRun[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getAdminRuns(filters)
      .then((response) => {
        setRuns(response.items)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load runs'))
  }, [filters])

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <p className="eyebrow">Run History</p>
        <h1 className="page-title">Recent stage runs and lease activity.</h1>
        <div className="toolbar-grid toolbar-grid--compact">
          <label className="field">
            <span>Stage</span>
            <select
              value={filters.stage}
              onChange={(event) => setFilters((current) => ({ ...current, stage: event.target.value }))}
            >
              {stageOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Status</span>
            <select
              value={filters.status}
              onChange={(event) => setFilters((current) => ({ ...current, status: event.target.value }))}
            >
              {runStatusOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Trigger</span>
            <select
              value={filters.trigger_kind}
              onChange={(event) => setFilters((current) => ({ ...current, trigger_kind: event.target.value }))}
            >
              {runTriggerOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
          <button
            className="secondary-button align-end"
            type="button"
            onClick={() => setFilters({ stage: '', status: '', trigger_kind: '' })}
          >
            Reset Filters
          </button>
        </div>
      </div>
      {error ? <div className="banner error">{error}</div> : null}
      <div className="page-card">
        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <th>Stage</th>
                <th>Status</th>
                <th>Trigger</th>
                <th>Owner</th>
                <th>Started</th>
                <th>Finished</th>
                <th>Run ID</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((run) => (
                <tr key={run.id}>
                  <td>
                    <Link className="table-link" to={`/admin/indexer/runs/${run.id}`}>
                      {run.stage_name}
                    </Link>
                  </td>
                  <td>{run.status}</td>
                  <td>{run.trigger_kind}</td>
                  <td>{run.claimed_by || 'n/a'}</td>
                  <td>{formatDateTime(run.started_at)}</td>
                  <td>{formatDateTime(run.finished_at)}</td>
                  <td>{formatNumber(run.id)}</td>
                </tr>
              ))}
              {runs.length === 0 ? (
                <tr>
                  <td colSpan={7}>
                    <div className="empty-state">No runs matched the current filters.</div>
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
