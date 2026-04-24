import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getAdminOverview } from '../../shared/api/admin'
import type { IndexerOverview } from '../../shared/types'

export function AdminDashboardPage() {
  const [overview, setOverview] = useState<IndexerOverview | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getAdminOverview()
      .then(setOverview)
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load overview'))
  }, [])

  if (error) {
    return <div className="banner error">{error}</div>
  }
  if (!overview) {
    return <div className="banner">Loading indexer overview...</div>
  }

  const cards = [
    ['Releases', overview.release_count],
    ['Files', overview.file_count],
    ['Ready NZBs', overview.ready_nzb_count],
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
      </div>
    </div>
  )
}
