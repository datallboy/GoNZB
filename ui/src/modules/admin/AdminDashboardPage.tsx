import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getAdminBacklogStats, getAdminOverview, refreshAdminBacklogStats } from '../../shared/api/admin'
import type { IndexerBacklogStats, IndexerOverview } from '../../shared/types'

export function AdminDashboardPage() {
  const [overview, setOverview] = useState<IndexerOverview | null>(null)
  const [backlog, setBacklog] = useState<IndexerBacklogStats | null>(null)
  const [backlogLoading, setBacklogLoading] = useState(false)
  const [backlogError, setBacklogError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getAdminOverview()
      .then(setOverview)
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load overview'))
    void getAdminBacklogStats()
      .then(setBacklog)
      .catch((err) => setBacklogError(err instanceof Error ? err.message : 'Failed to load backlog'))
  }, [])

  function refreshBacklog() {
    setBacklogLoading(true)
    setBacklogError(null)
    void refreshAdminBacklogStats()
      .then(setBacklog)
      .catch((err) => setBacklogError(err instanceof Error ? err.message : 'Failed to load backlog'))
      .finally(() => setBacklogLoading(false))
  }

  if (error) {
    return <div className="banner error">{error}</div>
  }
  if (!overview) {
    return <div className="banner">Loading indexer overview...</div>
  }

  const cards = [
    ['Releases', overview.release_count],
    ['Files', overview.file_count],
    ['Ready Releases', overview.ready_release_count],
    ['Cached NZBs', overview.ready_nzb_count],
    ['Running Stages', overview.running_stage_count],
    ['Paused Stages', overview.paused_stage_count],
    ['Failed Runs', overview.failed_run_count],
  ]

  return (
    <div className="page-section stack">
      <div className="page-hero">
        <div>
          <p className="eyebrow">Admin Dashboard</p>
          <h1 className="page-title">Operational health for the indexer runtime.</h1>
        </div>
        <div className="button-row">
          <Link className="secondary-button" to="/admin/indexer/stages">
            Runtime Stages
          </Link>
          <Link className="primary-button" to="/admin/indexer/releases">
            Moderation Queue
          </Link>
        </div>
      </div>
      <div className="hero-stat-grid">
        {cards.map(([label, value]) => (
          <div className="stat-card" key={label}>
            <span>{label}</span>
            <strong>{value}</strong>
          </div>
        ))}
        <div className="stat-card">
          <span>Unassembled Headers</span>
          <strong>{backlog?.available ? backlog.unassembled_headers.toLocaleString() : 'Not Cached'}</strong>
          <div className="button-row">
            <button className="secondary-button" type="button" onClick={refreshBacklog} disabled={backlogLoading}>
              {backlogLoading ? 'Refreshing...' : 'Refresh Count'}
            </button>
          </div>
          {backlog?.available && backlog.queried_at ? <small>As of {new Date(backlog.queried_at).toLocaleString()}</small> : <small>Uses the last persisted snapshot.</small>}
          {backlogError ? <small>{backlogError}</small> : null}
        </div>
      </div>
    </div>
  )
}
