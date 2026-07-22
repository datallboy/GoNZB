import { useEffect, useState } from 'react'
import {
  getAdminDeferredRanges,
  getAdminGroupProfiles,
  getAdminRecoveryCapacity,
  getAdminSourceBucketOutcomes,
} from '../../shared/api/admin'
import type {
  IndexerDeferredArticleRangeResponse,
  IndexerGroupProfileResponse,
  IndexerRecoveryCapacity,
  IndexerSourceBucketOutcomeReport,
} from '../../shared/types'

function formatNumber(value?: number) {
  if (typeof value !== 'number') {
    return 'n/a'
  }
  return value.toLocaleString()
}

function formatRate(value?: number) {
  if (typeof value !== 'number') {
    return 'n/a'
  }
  return `${Math.round(value).toLocaleString()}/hour`
}

function formatTime(value?: string) {
  if (!value) {
    return 'n/a'
  }
  return new Date(value).toLocaleString()
}

export function AdminIndexerWorkPage() {
  const [capacity, setCapacity] = useState<IndexerRecoveryCapacity | null>(null)
  const [profiles, setProfiles] = useState<IndexerGroupProfileResponse | null>(null)
  const [outcomes, setOutcomes] = useState<IndexerSourceBucketOutcomeReport | null>(null)
  const [deferred, setDeferred] = useState<IndexerDeferredArticleRangeResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      const [nextCapacity, nextProfiles, nextOutcomes, nextDeferred] = await Promise.all([
        getAdminRecoveryCapacity(),
        getAdminGroupProfiles(25),
        getAdminSourceBucketOutcomes(100),
        getAdminDeferredRanges(50, ''),
      ])
      setCapacity(nextCapacity)
      setProfiles(nextProfiles)
      setOutcomes(nextOutcomes)
      setDeferred(nextDeferred)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load indexer work state')
    }
  }

  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => void refresh(), 15000)
    return () => window.clearInterval(timer)
  }, [])

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <div className="page-header">
          <div>
            <p className="eyebrow">Indexer Work</p>
            <h1 className="page-title">Queue capacity and source outcomes.</h1>
          </div>
          <button className="secondary-button" type="button" onClick={() => void refresh()}>
            Refresh
          </button>
        </div>
        {error ? <div className="banner error">{error}</div> : null}
        <div className="stat-grid">
          <div className="stat-card">
            <span>yEnc open</span>
            <strong>{formatNumber(capacity?.open_total)}</strong>
            <small>{formatNumber(capacity?.remaining_to_hard)} until hard cap</small>
          </div>
          <div className="stat-card">
            <span>Recovery rate</span>
            <strong>{formatRate(capacity?.probes_per_hour_ewma)}</strong>
            <small>EWMA probe throughput</small>
          </div>
          <div className="stat-card">
            <span>Queue caps</span>
            <strong>{formatNumber(capacity?.hard_cap)}</strong>
            <small>soft {formatNumber(capacity?.soft_cap)}</small>
          </div>
          <div className="stat-card">
            <span>Ready age</span>
            <strong>{formatTime(capacity?.oldest_ready_at)}</strong>
            <small>oldest ready work item</small>
          </div>
        </div>
      </div>

      <div className="page-card stack">
        <div>
          <h2 className="section-title">Source-day outcomes</h2>
          <p className="muted-copy">Every provider, group, and posted-date bucket stays active until its work is finished, or becomes terminal through an archived release or bounded no-yield decision.</p>
        </div>
        <div className="stat-grid">
          <div className="stat-card">
            <span>Active buckets</span>
            <strong>{formatNumber(outcomes?.active_buckets)}</strong>
            <small>{formatNumber(outcomes?.open_work_count)} open work items</small>
          </div>
          <div className="stat-card">
            <span>Successful buckets</span>
            <strong>{formatNumber(outcomes?.success_buckets)}</strong>
            <small>{formatNumber(outcomes?.terminal_release_count)} durable releases</small>
          </div>
          <div className="stat-card">
            <span>No-yield buckets</span>
            <strong>{formatNumber(outcomes?.no_yield_buckets)}</strong>
            <small>settled after bounded processing</small>
          </div>
          <div className="stat-card">
            <span>Purge eligible</span>
            <strong>{formatNumber(outcomes?.purge_eligible_buckets)}</strong>
            <small>{formatNumber(outcomes?.purged_buckets)} already purged</small>
          </div>
        </div>
        <div className="table-shell">
          <table className="data-table data-table--compact">
            <thead>
              <tr>
                <th>Provider / group</th>
                <th>Source day</th>
                <th>Outcome</th>
                <th>Headers</th>
                <th>Open work</th>
                <th>Releases</th>
                <th>Reason / progress</th>
              </tr>
            </thead>
            <tbody>
              {(outcomes?.items || []).map((item) => (
                <tr key={`${item.provider_id}-${item.newsgroup_id}-${item.source_day}`}>
                  <td>{item.provider_key} / {item.group_name}</td>
                  <td>{item.source_day}</td>
                  <td><span className="status-pill status-pill--table">{item.state}</span></td>
                  <td>{formatNumber(item.headers_ingested)}</td>
                  <td>{formatNumber(item.open_work_count)}</td>
                  <td>{formatNumber(item.terminal_release_count)}</td>
                  <td>{item.terminal_reason || `last progress ${formatTime(item.last_progress_at)}`}</td>
                </tr>
              ))}
              {(outcomes?.items.length ?? 0) === 0 ? <tr><td colSpan={7}>No source buckets have been ingested yet.</td></tr> : null}
            </tbody>
          </table>
        </div>
      </div>

      <div className="page-card stack">
        <div>
          <h2 className="section-title">Deferred scrape ranges</h2>
          <p className="muted-copy">Ranges postponed by partition-day or recovery-cap limits. Ready and running work is drained by <code>scrape_deferred</code>; abandoned work needs operator review.</p>
        </div>
        <div className="table-shell">
          <table className="data-table data-table--compact">
            <thead>
              <tr>
                <th>Provider / group</th>
                <th>Article range</th>
                <th>State</th>
                <th>Reason</th>
                <th>Attempts</th>
                <th>Last error</th>
              </tr>
            </thead>
            <tbody>
              {(deferred?.items || []).map((item) => (
                <tr key={item.id}>
                  <td>{item.provider_key} / {item.group_name}</td>
                  <td>{formatNumber(item.article_low)}–{formatNumber(item.article_high)}</td>
                  <td><span className="status-pill status-pill--table">{item.state}</span></td>
                  <td>{item.reason || 'n/a'}</td>
                  <td>{formatNumber(item.attempts)}</td>
                  <td>{item.last_error || 'none'}</td>
                </tr>
              ))}
              {(deferred?.items.length ?? 0) === 0 ? <tr><td colSpan={6}>No deferred scrape ranges.</td></tr> : null}
            </tbody>
          </table>
        </div>
      </div>

      <div className="page-card stack">
        <h2 className="section-title">Group tiers</h2>
        <div className="table-shell">
          <table className="data-table data-table--compact">
            <thead>
              <tr>
                <th>Group</th>
                <th>Tier</th>
                <th>Queued 1d</th>
                <th>Releases 1d</th>
              </tr>
            </thead>
            <tbody>
              {(profiles?.items || []).map((profile) => (
                <tr key={`${profile.provider_id}-${profile.newsgroup_id}`}>
                  <td>{profile.group_name}</td>
                  <td><span className="status-pill status-pill--table">{profile.tier_override || profile.tier}</span></td>
                  <td>{formatNumber(profile.recovery_queued_1d)}</td>
                  <td>{formatNumber(profile.releases_created_1d)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
