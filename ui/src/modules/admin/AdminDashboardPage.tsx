import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getAdminBackfillProgress, getAdminDashboardStats, getAdminNNTPStats, getAdminOverview, getAdminStageThroughput, refreshAdminDashboardStats } from '../../shared/api/admin'
import type { IndexerBackfillProgress, IndexerDashboardStat, IndexerDashboardStats, IndexerNNTPStats, IndexerOverview, IndexerStageThroughput } from '../../shared/types'

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

function statFreshness(stat: IndexerDashboardStat) {
  if (stat.last_error) {
    const attemptedAt = formatTimestamp(stat.refresh_attempted_at)
    return `Refresh failed${attemptedAt ? ` at ${attemptedAt}` : ''}`
  }
  const updatedAt = formatTimestamp(stat.updated_at)
  return updatedAt ? `Updated ${updatedAt}` : 'Waiting for first snapshot'
}

function formatStatValue(stat: IndexerDashboardStat) {
  if (!stat.available) {
    return 'Not Cached'
  }
  const suffix = stat.capped ? '+' : ''
  if (stat.key.endsWith('_bytes')) {
    const value = stat.value
    if (value >= 1024 ** 3) {
      return `${(value / 1024 ** 3).toFixed(1)} GB${suffix}`
    }
    if (value >= 1024 ** 2) {
      return `${(value / 1024 ** 2).toFixed(1)} MB${suffix}`
    }
    if (value >= 1024) {
      return `${(value / 1024).toFixed(1)} KB${suffix}`
    }
  }
  return `${stat.value.toLocaleString()}${suffix}`
}

function isInspectBacklogStat(stat: IndexerDashboardStat) {
  return stat.key.startsWith('pending_inspect_')
}

function backlogCommand(stat: IndexerDashboardStat) {
  switch (stat.key) {
    case 'unassembled_headers':
      return 'indexer assemble'
    case 'pending_release_summary_refresh_summaries':
      return 'indexer release refresh-summaries'
    case 'pending_release_candidate_families':
      return 'indexer release'
    case 'generate_nzb_pending_releases':
      return 'indexer release generate-nzb'
    case 'archive_pending_releases':
      return 'indexer release archive-nzb'
    case 'archived_waiting_for_purge_releases':
      return 'indexer release purge-archived-sources'
    case 'pending_yenc_recovery_binaries':
      return 'indexer recover-yenc'
    case 'pending_inspect_discovery_binaries':
      return 'indexer inspect discovery'
    case 'pending_inspect_par2_binaries':
      return 'indexer inspect par2'
    case 'pending_inspect_nfo_binaries':
      return 'indexer inspect nfo'
    case 'pending_inspect_archive_binaries':
      return 'indexer inspect archive'
    case 'pending_inspect_password_binaries':
      return 'indexer inspect password'
    case 'pending_inspect_media_binaries':
      return 'indexer inspect media'
    default:
      return null
  }
}

function backlogCard(stat: IndexerDashboardStat) {
  const command = backlogCommand(stat)
  return (
    <div className="stat-card backlog-stat-card" key={stat.key}>
      <div className="backlog-stat-card__header">
        <span>{stat.label}</span>
        <small>{stat.exact ? 'exact' : stat.capped ? 'capped estimate' : 'estimate'}</small>
      </div>
      <strong>{formatStatValue(stat)}</strong>
      {command ? <code>{command}</code> : null}
      <small>{statFreshness(stat)}</small>
      {stat.last_error ? <small className="backlog-stat-card__error">{stat.last_error}</small> : null}
    </div>
  )
}

function LoadingBlock({ label }: { label: string }) {
  return (
    <div className="loading-panel" role="status" aria-live="polite">
      <span className="loading-spinner" aria-hidden="true" />
      <span>{label}</span>
    </div>
  )
}

export function AdminDashboardPage() {
  const [overview, setOverview] = useState<IndexerOverview | null>(null)
  const [stats, setStats] = useState<IndexerDashboardStats | null>(null)
  const [backfill, setBackfill] = useState<IndexerBackfillProgress | null>(null)
  const [throughput, setThroughput] = useState<IndexerStageThroughput | null>(null)
  const [nntpStats, setNNTPStats] = useState<IndexerNNTPStats | null>(null)
  const [overviewLoading, setOverviewLoading] = useState(true)
  const [statsLoading, setStatsLoading] = useState(false)
  const [backfillLoading, setBackfillLoading] = useState(false)
  const [throughputLoading, setThroughputLoading] = useState(false)
  const [nntpLoading, setNNTPLoading] = useState(false)
  const [statsError, setStatsError] = useState<string | null>(null)
  const [backfillError, setBackfillError] = useState<string | null>(null)
  const [throughputError, setThroughputError] = useState<string | null>(null)
  const [nntpError, setNNTPError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
  let cancelled = false

  function loadNNTPStats(showLoading = false) {
    if (showLoading) {
      setNNTPLoading(true)
    }

    void getAdminNNTPStats()
      .then((value) => {
        if (!cancelled) {
          setNNTPStats(value)
          setNNTPError(null)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setNNTPError(
            err instanceof Error
              ? err.message
              : 'Failed to load NNTP stats'
          )
        }
      })
      .finally(() => {
        if (showLoading && !cancelled) {
          setNNTPLoading(false)
        }
      })
  }

  void getAdminOverview()
    .then((value) => {
      if (!cancelled) {
        setOverview(value)
      }
    })
    .catch((err) => {
      if (!cancelled) {
        setError(
          err instanceof Error
            ? err.message
            : 'Failed to load overview'
        )
      }
    })
    .finally(() => {
      if (!cancelled) {
        setOverviewLoading(false)
      }
    })

  const timer = window.setTimeout(() => {
    if (cancelled) {
      return
    }

    setStatsLoading(true)
    void getAdminDashboardStats()
      .then((value) => {
        if (!cancelled) {
          setStats(value)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setStatsError(
            err instanceof Error
              ? err.message
              : 'Failed to load dashboard stats'
          )
        }
      })
      .finally(() => {
        if (!cancelled) {
          setStatsLoading(false)
        }
      })

    setThroughputLoading(true)
    void getAdminStageThroughput()
      .then((value) => {
        if (!cancelled) {
          setThroughput(value)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setThroughputError(
            err instanceof Error
              ? err.message
              : 'Failed to load stage throughput'
          )
        }
      })
      .finally(() => {
        if (!cancelled) {
          setThroughputLoading(false)
        }
      })

    // Initial NNTP load with spinner
    loadNNTPStats(true)

    setBackfillLoading(true)
    void getAdminBackfillProgress()
      .then((value) => {
        if (!cancelled) {
          setBackfill(value)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setBackfillError(
            err instanceof Error
              ? err.message
              : 'Failed to load backfill progress'
          )
        }
      })
      .finally(() => {
        if (!cancelled) {
          setBackfillLoading(false)
        }
      })
  }, 0)

  // Silent NNTP auto-refresh every second
  const nntpInterval = window.setInterval(() => {
    if (!cancelled) {
      loadNNTPStats(false)
    }
  }, 1000)

  return () => {
    cancelled = true
    window.clearTimeout(timer)
    window.clearInterval(nntpInterval)
  }
}, [])

  function refreshStats() {
    setStatsLoading(true)
    setStatsError(null)
    void refreshAdminDashboardStats()
      .then(setStats)
      .catch((err) => setStatsError(err instanceof Error ? err.message : 'Failed to refresh dashboard stats'))
      .finally(() => setStatsLoading(false))
  }

  const cards = [
    ['Releases', overview?.release_count],
    ['Files', overview?.file_count],
    ['Ready Releases', overview?.ready_release_count],
    ['Archived NZBs', overview?.archived_nzb_count],
    ['Running Stages', overview?.running_stage_count],
    ['Paused Stages', overview?.paused_stage_count],
    ['Failed Runs', overview?.failed_run_count],
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
      {error ? <div className="banner error">{error}</div> : null}
      {overviewLoading && !overview ? (
        <LoadingBlock label="Loading overview cards..." />
      ) : (
        <div className="hero-stat-grid">
          {cards.map(([label, value]) => (
            <div className="stat-card" key={label}>
              <span>{label}</span>
              <strong>{typeof value === 'number' ? value.toLocaleString() : 'Unavailable'}</strong>
            </div>
          ))}
        </div>
      )}

      <div className="page-card stack">
        <div className="toolbar-row">
          <div>
            <h2 className="section-title">Operational Backlog</h2>
            <p className="muted-copy">
              Queue-focused snapshots for the stages operators tune most often. Refresh recomputes backlog counts without storage diagnostics.
            </p>
          </div>
          <button className="secondary-button" type="button" onClick={refreshStats} disabled={statsLoading}>
            {statsLoading ? (stats ? 'Refreshing...' : 'Loading...') : 'Refresh Backlog'}
          </button>
        </div>
        {statsError ? <div className="banner error">{statsError}</div> : null}
        {statsLoading && !stats ? (
          <LoadingBlock label="Loading backlog snapshot..." />
        ) : (
          <>
            <div className="hero-stat-grid">{commandBacklogStats.map(backlogCard)}</div>
            <div>
              <h3 className="section-title">Inspection Backlog</h3>
              <div className="hero-stat-grid">{inspectBacklogStats.map(backlogCard)}</div>
            </div>
          </>
        )}
      </div>

      <div className="page-card stack">
        <div>
          <h2 className="section-title">NNTP Capacity</h2>
          <p className="muted-copy">
            Live indexer transport pressure. Waiting requests mean indexer work is queued behind the configured NNTP connection limit.
          </p>
        </div>
        {nntpError ? <div className="banner error">{nntpError}</div> : null}
        {nntpLoading && !nntpStats ? (
          <LoadingBlock label="Loading NNTP capacity..." />
        ) : (
          <>
            <div className="hero-stat-grid">
              <div className="stat-card">
                <span>Policy</span>
                <strong>{nntpStats?.policy ?? 'Unavailable'}</strong>
              </div>
              <div className="stat-card">
                <span>Active Connections</span>
                <strong>
                  {nntpStats ? `${nntpStats.active.toLocaleString()} / ${nntpStats.capacity.toLocaleString()}` : 'Unavailable'}
                </strong>
              </div>
              <div className="stat-card">
                <span>Waiting Requests</span>
                <strong>{nntpStats ? nntpStats.waiting.toLocaleString() : 'Unavailable'}</strong>
              </div>
              <div className="stat-card">
                <span>Max Wait</span>
                <strong>{nntpStats ? formatDuration(nntpStats.wait_max_ms) : 'Unavailable'}</strong>
              </div>
              <div className="stat-card">
                <span>Missing Articles</span>
                <strong>{nntpStats ? nntpStats.article_not_found.toLocaleString() : 'Unavailable'}</strong>
              </div>
              <div className="stat-card">
                <span>NNTP Errors</span>
                <strong>{nntpStats ? nntpStats.operation_errors.toLocaleString() : 'Unavailable'}</strong>
              </div>
            </div>
            <div className="table-shell">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>Stage Demand</th>
                    <th>Active</th>
                    <th>Waiting</th>
                    <th>Requests</th>
                    <th>Max Wait</th>
                  </tr>
                </thead>
                <tbody>
                  {(nntpStats?.scopes ?? []).map((scope) => (
                    <tr key={scope.scope}>
                      <td>
                        <strong>{scope.scope}</strong>
                      </td>
                      <td>{scope.active.toLocaleString()}</td>
                      <td>{scope.waiting.toLocaleString()}</td>
                      <td>
                        <strong>{(scope.fetches + scope.fetch_body_prefix + scope.group_stats + scope.xover).toLocaleString()}</strong>
                        <div className="muted-copy">
                          fetch {scope.fetches.toLocaleString()} · prefix {scope.fetch_body_prefix.toLocaleString()} · xover {scope.xover.toLocaleString()}
                        </div>
                      </td>
                      <td>{formatDuration(scope.wait_max_ms)}</td>
                    </tr>
                  ))}
                  {!nntpLoading && !nntpError && (nntpStats?.scopes.length ?? 0) === 0 ? (
                    <tr>
                      <td colSpan={5} className="muted-copy">
                        No stage-level NNTP demand has been recorded yet.
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </div>
            <div className="table-shell">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>Provider</th>
                    <th>Connections</th>
                    <th>Pool</th>
                    <th>Retries</th>
                    <th>Errors</th>
                  </tr>
                </thead>
                <tbody>
                  {(nntpStats?.providers ?? []).map((provider) => (
                    <tr key={provider.id}>
                      <td>
                        <strong>{provider.label || provider.id}</strong>
                        <div className="muted-copy">Priority {provider.priority}</div>
                      </td>
                      <td>
                        <strong>{provider.active.toLocaleString()} / {provider.capacity.toLocaleString()}</strong>
                        <div className="muted-copy">active / capacity</div>
                      </td>
                      <td>
                        <strong>{provider.idle.toLocaleString()}</strong>
                        <div className="muted-copy">
                          {provider.dials.toLocaleString()} dials · {provider.pool_reuses.toLocaleString()} reuses
                        </div>
                      </td>
                      <td>
                        <strong>{(provider.fetch_retries + provider.group_stats_retries + provider.xover_retries).toLocaleString()}</strong>
                        <div className="muted-copy">
                          fetch {provider.fetch_retries.toLocaleString()} · xover {provider.xover_retries.toLocaleString()}
                        </div>
                      </td>
                      <td>
                        <strong>{provider.recoverable_errors.toLocaleString()}</strong>
                        <div className="muted-copy">
                          {provider.dial_failures.toLocaleString()} dial failures · {provider.pool_discard_error.toLocaleString()} discarded
                        </div>
                      </td>
                    </tr>
                  ))}
                  {!nntpLoading && !nntpError && (nntpStats?.providers.length ?? 0) === 0 ? (
                    <tr>
                      <td colSpan={5} className="muted-copy">
                        No indexer NNTP manager is currently configured.
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </div>
          </>
        )}
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
              {throughputLoading && !throughput ? (
                <tr>
                  <td colSpan={4}>
                    <LoadingBlock label="Loading stage throughput..." />
                  </td>
                </tr>
              ) : null}
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
              {!throughputLoading && !throughputError && (throughput?.items.length ?? 0) === 0 ? (
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
              {backfillLoading && !backfill ? (
                <tr>
                  <td colSpan={7}>
                    <LoadingBlock label="Loading backfill progress..." />
                  </td>
                </tr>
              ) : null}
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
              {!backfillLoading && !backfillError && (backfill?.items.length ?? 0) === 0 ? (
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
