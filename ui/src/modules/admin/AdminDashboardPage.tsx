import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getAdminBackfillProgress, getAdminDashboardStats, getAdminOverview, getAdminStageThroughput, refreshAdminDashboardStats } from '../../shared/api/admin'
import type { IndexerBackfillProgress, IndexerDashboardStat, IndexerDashboardStats, IndexerOverview, IndexerStageThroughput } from '../../shared/types'

function formatTimestamp(value?: string) {
  if (!value) {
    return null
  }
  return new Date(value).toLocaleString()
}

function formatDate(value?: string) {
  if (!value) {
    return 'Not observed'
  }
  return new Date(value).toLocaleDateString()
}

function formatRate(value: number) {
  if (!Number.isFinite(value) || value <= 0) {
    return '0'
  }
  if (value >= 100) {
    return value.toLocaleString(undefined, { maximumFractionDigits: 0 })
  }
  if (value >= 10) {
    return value.toLocaleString(undefined, { maximumFractionDigits: 1 })
  }
  return value.toLocaleString(undefined, { maximumFractionDigits: 2 })
}

function formatDuration(ms: number) {
  if (!Number.isFinite(ms) || ms <= 0) {
    return '0s'
  }
  const seconds = ms / 1000
  if (seconds < 60) {
    return `${formatRate(seconds)}s`
  }
  const minutes = seconds / 60
  return `${formatRate(minutes)}m`
}

function statFootnote(stat: IndexerDashboardStat) {
  if (stat.last_error) {
    const attemptedAt = formatTimestamp(stat.refresh_attempted_at)
    const snapshotAt = formatTimestamp(stat.updated_at)
    if (snapshotAt) {
      return `Refresh failed${attemptedAt ? ` at ${attemptedAt}` : ''}. Showing last good snapshot from ${snapshotAt}.`
    }
    return `Refresh failed${attemptedAt ? ` at ${attemptedAt}` : ''}. No cached snapshot yet.`
  }
  const updatedAt = formatTimestamp(stat.updated_at)
  if (updatedAt) {
    return `As of ${updatedAt} · ${stat.exact ? 'exact count' : 'bounded estimate'}`
  }
  return 'Uses the last persisted snapshot.'
}

function formatStatValue(stat: IndexerDashboardStat) {
  if (!stat.available) {
    return 'Not Cached'
  }
  if (stat.key.endsWith('_bytes')) {
    const value = stat.value
    if (value >= 1024 ** 3) {
      return `${(value / 1024 ** 3).toFixed(1)} GB`
    }
    if (value >= 1024 ** 2) {
      return `${(value / 1024 ** 2).toFixed(1)} MB`
    }
    if (value >= 1024) {
      return `${(value / 1024).toFixed(1)} KB`
    }
  }
  return stat.value.toLocaleString()
}

function isInspectBacklogStat(stat: IndexerDashboardStat) {
  return stat.key.startsWith('pending_inspect_')
}

function backlogCard(stat: IndexerDashboardStat) {
  return (
    <div className="stat-card" key={stat.key}>
      <span>{stat.label}</span>
      <strong>{formatStatValue(stat)}</strong>
      <small>{stat.description}</small>
      <small>{statFootnote(stat)}</small>
      {stat.last_error ? <small>{stat.last_error}</small> : null}
    </div>
  )
}

export function AdminDashboardPage() {
  const [overview, setOverview] = useState<IndexerOverview | null>(null)
  const [stats, setStats] = useState<IndexerDashboardStats | null>(null)
  const [backfill, setBackfill] = useState<IndexerBackfillProgress | null>(null)
  const [throughput, setThroughput] = useState<IndexerStageThroughput | null>(null)
  const [statsLoading, setStatsLoading] = useState(false)
  const [statsError, setStatsError] = useState<string | null>(null)
  const [backfillError, setBackfillError] = useState<string | null>(null)
  const [throughputError, setThroughputError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getAdminOverview()
      .then(setOverview)
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load overview'))
    void getAdminDashboardStats()
      .then(setStats)
      .catch((err) => setStatsError(err instanceof Error ? err.message : 'Failed to load dashboard stats'))
    void getAdminBackfillProgress()
      .then(setBackfill)
      .catch((err) => setBackfillError(err instanceof Error ? err.message : 'Failed to load backfill progress'))
    void getAdminStageThroughput()
      .then(setThroughput)
      .catch((err) => setThroughputError(err instanceof Error ? err.message : 'Failed to load stage throughput'))
  }, [])

  function refreshStats() {
    setStatsLoading(true)
    setStatsError(null)
    void refreshAdminDashboardStats()
      .then(setStats)
      .catch((err) => setStatsError(err instanceof Error ? err.message : 'Failed to refresh dashboard stats'))
      .finally(() => setStatsLoading(false))
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
  const backlogStats = stats?.items ?? []
  const commandBacklogStats = backlogStats.filter((stat) => !isInspectBacklogStat(stat))
  const inspectBacklogStats = backlogStats.filter(isInspectBacklogStat)

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

      <div className="page-card stack">
        <div className="toolbar-row">
          <div>
            <h2 className="section-title">Operational Backlog</h2>
            <p className="muted-copy">
              Queue-focused snapshots for the stages operators tune most often. Refresh recomputes backlog counts without storage diagnostics.
            </p>
          </div>
          <button className="secondary-button" type="button" onClick={refreshStats} disabled={statsLoading}>
            {statsLoading ? 'Refreshing...' : 'Refresh Backlog'}
          </button>
        </div>
        {statsError ? <div className="banner error">{statsError}</div> : null}
        <div className="hero-stat-grid">
          {commandBacklogStats.map(backlogCard)}
        </div>
        <div>
          <h3 className="section-title">Inspection Backlog</h3>
          <div className="hero-stat-grid">{inspectBacklogStats.map(backlogCard)}</div>
        </div>
      </div>

      <div className="page-card stack">
        <div>
          <h2 className="section-title">Recent Stage Throughput</h2>
          <p className="muted-copy">
            Recent persisted run speed across the last 1, 6, and 24 hours. This helps show where throughput is strong and where a stage is slowing down.
          </p>
        </div>
        {throughputError ? <div className="banner error">{throughputError}</div> : null}
        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <th>Stage</th>
                <th>Last 1h</th>
                <th>Last 6h</th>
                <th>Last 24h</th>
              </tr>
            </thead>
            <tbody>
              {(throughput?.items ?? []).map((item) => (
                <tr key={item.stage_name}>
                  <td>
                    <strong>{item.label}</strong>
                    <div className="muted-copy">Tracks {item.item_label} processed.</div>
                  </td>
                  {item.windows.map((window) => (
                    <td key={`${item.stage_name}-${window.window_hours}`}>
                      <div>
                        <strong>{window.items_processed.toLocaleString()}</strong> {item.item_label}
                      </div>
                      <div className="muted-copy">{formatRate(window.items_per_second)}/sec</div>
                      <div className="muted-copy">{window.completed_runs} completed · {window.failed_runs} failed</div>
                      <div className="muted-copy">Avg run {formatDuration(window.avg_run_duration_ms)}</div>
                    </td>
                  ))}
                </tr>
              ))}
              {!throughputError && (throughput?.items.length ?? 0) === 0 ? (
                <tr>
                  <td colSpan={4} className="muted-copy">
                    No recent stage throughput data yet.
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      <div className="page-card stack">
        <div>
          <h2 className="section-title">Backfill Progress</h2>
          <p className="muted-copy">
            Per-newsgroup backfill state, including the configured cutoff, furthest scraped article date, and the latest article date seen so far.
          </p>
        </div>
        {backfillError ? <div className="banner error">{backfillError}</div> : null}
        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <th>Newsgroup</th>
                <th>Cutoff</th>
                <th>Status</th>
                <th>Oldest Scraped</th>
                <th>Latest Scraped</th>
                <th>Backfill Cursor</th>
                <th>Latest Article</th>
              </tr>
            </thead>
            <tbody>
              {(backfill?.items ?? []).map((item) => (
                <tr key={item.group_name}>
                  <td>
                    <strong>{item.group_name}</strong>
                    <div className="muted-copy">Providers tracked: {item.provider_count}</div>
                  </td>
                  <td>{formatDate(item.configured_cutoff_date)}</td>
                  <td>
                    <span className={`pill ${item.cutoff_reached ? 'tone-excellent' : 'tone-fair'}`}>
                      {item.cutoff_reached ? 'Reached' : 'Running'}
                    </span>
                    <div className="muted-copy">
                      {item.last_checkpoint_updated_at ? `Updated ${formatTimestamp(item.last_checkpoint_updated_at)}` : 'No checkpoint update yet'}
                    </div>
                  </td>
                  <td>{formatDate(item.oldest_scraped_article_date)}</td>
                  <td>{formatDate(item.latest_scraped_article_date)}</td>
                  <td>{item.backfill_cursor_article_number > 0 ? item.backfill_cursor_article_number.toLocaleString() : 'Not started'}</td>
                  <td>{item.latest_article_number > 0 ? item.latest_article_number.toLocaleString() : 'Not observed'}</td>
                </tr>
              ))}
              {!backfillError && (backfill?.items.length ?? 0) === 0 ? (
                <tr>
                  <td colSpan={7} className="muted-copy">
                    No backfill checkpoint data yet.
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
