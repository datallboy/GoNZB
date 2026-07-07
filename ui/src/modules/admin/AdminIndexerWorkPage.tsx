import { useEffect, useState } from 'react'
import {
  getAdminGroupProfiles,
  getAdminRecoveryCapacity,
} from '../../shared/api/admin'
import type {
  IndexerGroupProfileResponse,
  IndexerRecoveryCapacity,
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
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      const [nextCapacity, nextProfiles] = await Promise.all([
        getAdminRecoveryCapacity(),
        getAdminGroupProfiles(25),
      ])
      setCapacity(nextCapacity)
      setProfiles(nextProfiles)
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
            <h1 className="page-title">Recovery capacity and group tiers.</h1>
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
