import { useEffect, useState } from 'react'
import {
  getAdminDailyBuckets,
  getAdminDeferredRanges,
  getAdminGroupProfiles,
  getAdminRecoveryCapacity,
} from '../../shared/api/admin'
import type {
  IndexerDailyBucket,
  IndexerDailyBucketResponse,
  IndexerDeferredArticleRangeResponse,
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

function progressValue(bucket: IndexerDailyBucket) {
  if (!bucket.scrape_progress_known || typeof bucket.scrape_progress_pct !== 'number') {
    return null
  }
  return Math.max(0, Math.min(100, bucket.scrape_progress_pct))
}

function ProgressCell({ bucket }: { bucket: IndexerDailyBucket }) {
  const value = progressValue(bucket)
  if (value === null) {
    return (
      <div className="bucket-progress-cell">
        <span className="muted-copy">unknown</span>
        <span className="bucket-progress-meta">
          low {bucket.lower_boundary_crossed ? 'seen' : 'open'} · high {bucket.upper_boundary_crossed ? 'seen' : 'open'}
        </span>
      </div>
    )
  }
  return (
    <div className="bucket-progress-cell">
      <div className="bucket-progress" aria-label={`Scrape progress ${value.toFixed(1)} percent`}>
        <span style={{ width: `${value}%` }} />
      </div>
      <span className="bucket-progress-meta">{value.toFixed(1)}%</span>
    </div>
  )
}

export function AdminIndexerWorkPage() {
  const [capacity, setCapacity] = useState<IndexerRecoveryCapacity | null>(null)
  const [buckets, setBuckets] = useState<IndexerDailyBucketResponse | null>(null)
  const [profiles, setProfiles] = useState<IndexerGroupProfileResponse | null>(null)
  const [deferred, setDeferred] = useState<IndexerDeferredArticleRangeResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      const [nextCapacity, nextBuckets, nextProfiles, nextDeferred] = await Promise.all([
        getAdminRecoveryCapacity(),
        getAdminDailyBuckets(50),
        getAdminGroupProfiles(25),
        getAdminDeferredRanges(25, 'queued'),
      ])
      setCapacity(nextCapacity)
      setBuckets(nextBuckets)
      setProfiles(nextProfiles)
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
            <h1 className="page-title">Daily buckets and recovery capacity.</h1>
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
        <h2 className="section-title">Daily buckets</h2>
        <div className="table-shell">
          <table className="data-table data-table--compact">
            <thead>
              <tr>
                <th>Day</th>
                <th>Group</th>
                <th>Tier</th>
                <th>Scrape</th>
                <th>Headers</th>
                <th>Unassembled</th>
                <th>yEnc</th>
                <th>Binaries</th>
                <th>Releases</th>
              </tr>
            </thead>
            <tbody>
              {(buckets?.items || []).map((bucket) => (
                <tr key={`${bucket.provider_id}-${bucket.newsgroup_id}-${bucket.bucket_day}-${bucket.bucket_article_low}`}>
                  <td>{bucket.bucket_day}</td>
                  <td>
                    <strong>{bucket.group_name}</strong>
                    <br />
                    <span className="muted-copy">{bucket.provider_key}</span>
                  </td>
                  <td><span className="status-pill status-pill--table">{bucket.tier}</span></td>
                  <td><ProgressCell bucket={bucket} /></td>
                  <td>{formatNumber(bucket.headers_staged)}</td>
                  <td>{formatNumber(bucket.unassembled_headers)}</td>
                  <td>
                    {formatNumber(bucket.yenc_ready)} ready
                    <br />
                    <span className="muted-copy">{formatNumber(bucket.yenc_running)} running · {formatNumber(bucket.yenc_done)} done</span>
                  </td>
                  <td>
                    {formatNumber(bucket.binaries_complete)} / {formatNumber(bucket.binaries_total)}
                    <br />
                    <span className="muted-copy">{formatNumber(bucket.binaries_weak)} weak</span>
                  </td>
                  <td>{formatNumber(bucket.releases_created)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className="two-column-grid">
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

        <div className="page-card stack">
          <h2 className="section-title">Deferred scrape ranges</h2>
          <div className="table-shell">
            <table className="data-table data-table--compact">
              <thead>
                <tr>
                  <th>Group</th>
                  <th>Kind</th>
                  <th>Articles</th>
                  <th>Reason</th>
                </tr>
              </thead>
              <tbody>
                {(deferred?.items || []).map((range) => (
                  <tr key={range.id}>
                    <td>{range.group_name}</td>
                    <td>{range.range_kind}</td>
                    <td>{formatNumber(range.estimated_count)}</td>
                    <td>{range.reason}</td>
                  </tr>
                ))}
                {deferred?.items.length === 0 ? (
                  <tr>
                    <td colSpan={4}>No queued deferred ranges.</td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  )
}
