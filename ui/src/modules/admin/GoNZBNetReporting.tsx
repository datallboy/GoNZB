import type {
  GoNZBNetActivityReport,
  GoNZBNetActivityRollup,
  GoNZBNetArticleAvailabilityDiagnostic,
  GoNZBNetOverviewReport,
  GoNZBNetPoolHealthReport,
  GoNZBNetRoleJob,
  GoNZBNetRolesReport,
} from '../../shared/types'
import { formatDateTime, formatNumber } from '../../shared/lib/format'

type View = 'overview' | 'roles' | 'pools' | 'activity'

type Props = {
  view: View
  overview: GoNZBNetOverviewReport | null
  roles: GoNZBNetRolesReport | null
  activity: GoNZBNetActivityReport | null
  poolHealth: GoNZBNetPoolHealthReport | null
  articleAvailability: GoNZBNetArticleAvailabilityDiagnostic[]
  activityWindow: string
  onActivityWindowChange: (window: string) => void
}

const statusText: Record<string, string> = {
  off: 'Off',
  starting: 'Starting',
  ready: 'Ready',
  working: 'Working',
  degraded: 'Needs attention',
  blocked: 'Blocked',
}

function Status({ value }: { value: string }) {
  return <span className={`status-pill status-pill--table gonzbnet-status gonzbnet-status--${value}`}>{statusText[value] ?? value}</span>
}

function RoleSummary({ job, detail = false }: { job: GoNZBNetRoleJob; detail?: boolean }) {
  return (
    <article className="gonzbnet-job-card stack">
      <div className="gonzbnet-job-card__header">
        <div>
          <h3 className="section-title">{job.label}</h3>
          <p className="muted-copy">{job.description}</p>
        </div>
        <Status value={job.status} />
      </div>
      <div className="gonzbnet-job-meta">
        <span>{job.pools.length ? `${job.pools.length} eligible pool${job.pools.length === 1 ? '' : 's'}` : 'No eligible pools'}</span>
        <span>{job.last_useful_at ? `Last useful work ${formatDateTime(job.last_useful_at)}` : 'No useful work recorded yet'}</span>
      </div>
      {job.warnings.map((warning) => <div className="banner" key={warning}>{warning}</div>)}
      {detail ? (
        <div className="gonzbnet-component-list">
          {job.components.map((component) => (
            <div className="gonzbnet-component-row" key={component.key}>
              <div>
                <strong>{component.label}</strong>
                <div className="muted-copy">{component.description}</div>
              </div>
              <div className="gonzbnet-component-row__metrics">
                <Status value={component.status} />
                <span>{component.execution_mode.replace('_', ' ')}</span>
                <span>{formatNumber(component.successes)} successful</span>
                <span>{formatNumber(component.failures)} failed</span>
                <span>{component.last_success_at ? formatDateTime(component.last_success_at) : 'Never completed'}</span>
              </div>
              {component.reason ? <div className="muted-copy">{component.reason}</div> : null}
              {component.last_error ? <div className="banner error">{component.last_error}</div> : null}
            </div>
          ))}
        </div>
      ) : null}
    </article>
  )
}

function OverviewView({ report }: { report: GoNZBNetOverviewReport | null }) {
  if (!report) return <div className="page-card muted-copy">Overview reporting is unavailable.</div>
  return (
    <div className="stack">
      {report.warnings.map((warning) => <div className="banner" key={warning}>{warning}</div>)}
      {report.pending_admissions ? <div className="banner"><a href="/admin/gonzbnet?view=pools">{report.pending_admissions} pool admission request{report.pending_admissions === 1 ? '' : 's'} need attention.</a></div> : null}
      {!report.pools.length ? <div className="banner"><a href="/admin/gonzbnet?view=pools">Create or join a pool to begin exchanging signed release data.</a></div> : null}
      <div className="stat-grid">
        <div className="stat-card"><span>Node</span><strong>{report.module_enabled ? 'Online' : 'Off'}</strong><small>{report.node_alias || report.node_id}</small></div>
        <div className="stat-card"><span>Roles healthy</span><strong>{report.jobs_healthy} / {report.jobs_configured}</strong><small>Configured grouped jobs</small></div>
        <div className="stat-card"><span>Peers</span><strong>{report.peers_connected} / {report.peers_total}</strong><small>Connected</small></div>
        <div className="stat-card"><span>Pools</span><strong>{report.pools.filter((pool) => pool.enabled).length}</strong><small>{report.pending_admissions} pending admissions</small></div>
        <div className="stat-card"><span>Release health</span><strong>{formatNumber(report.release_evidence.fresh)} fresh</strong><small>of {formatNumber(report.release_evidence.total)} shared reports</small></div>
        <div className="stat-card"><span>Article availability</span><strong>{formatNumber(report.article_evidence.fresh)} fresh</strong><small>from {formatNumber(report.article_evidence.reporters)} reporters</small></div>
      </div>
      <div className="gonzbnet-job-grid">
        {report.jobs.filter((job) => job.configured).map((job) => <RoleSummary job={job} key={job.key} />)}
      </div>
      <p className="muted-copy">Configuration is read-only here. Change GoNZBNet jobs in <a href="/admin/settings?tab=gonzbnet">Settings</a>.</p>
    </div>
  )
}

function RolesView({ report }: { report: GoNZBNetRolesReport | null }) {
  if (!report) return <div className="page-card muted-copy">Role reporting is unavailable.</div>
  const enabled = report.jobs.filter((job) => job.configured)
  const disabled = report.jobs.filter((job) => !job.configured)
  return (
    <div className="stack">
      <section className="page-card stack">
        <div><p className="eyebrow">What this node does</p><h2 className="section-title">Enabled jobs</h2></div>
        <div className="gonzbnet-job-grid">{enabled.map((job) => <RoleSummary detail job={job} key={job.key} />)}</div>
      </section>
      <details className="page-card">
        <summary>Off jobs ({disabled.length})</summary>
        <div className="gonzbnet-job-grid gonzbnet-details-body">{disabled.map((job) => <RoleSummary detail job={job} key={job.key} />)}</div>
      </details>
      <p className="muted-copy">Jobs are configured under <a href="/admin/settings?tab=gonzbnet">Settings → GoNZBNet</a>.</p>
    </div>
  )
}

function EvidenceCard({ title, report }: { title: string; report: GoNZBNetPoolHealthReport['release_health'] }) {
  return (
    <article className="page-card stack">
      <h3 className="section-title">{title}</h3>
      <div className="stat-grid">
        <div className="stat-card"><span>Fresh</span><strong>{formatNumber(report.fresh)}</strong><small>of {formatNumber(report.total)}</small></div>
        <div className="stat-card"><span>Aging</span><strong>{formatNumber(report.aging)}</strong><small>2–24 hours old</small></div>
        <div className="stat-card"><span>Stale</span><strong>{formatNumber(report.stale)}</strong><small>over 24 hours old</small></div>
        <div className="stat-card"><span>Reporters</span><strong>{formatNumber(report.reporters)}</strong><small>{report.last_checked_at ? `Last ${formatDateTime(report.last_checked_at)}` : 'No reports yet'}</small></div>
      </div>
      <div className="gonzbnet-status-counts">{Object.entries(report.statuses).map(([status, count]) => <span key={status}><Status value={status} /> {formatNumber(count)}</span>)}</div>
    </article>
  )
}

function PoolsView({ report, availability }: { report: GoNZBNetPoolHealthReport | null; availability: GoNZBNetArticleAvailabilityDiagnostic[] }) {
  if (!report) return <div className="page-card muted-copy">Select or create a pool to see shared health and contribution reporting.</div>
  return (
    <div className="stack">
      <div className="page-card"><p className="eyebrow">Selected pool</p><h2 className="section-title">{report.pool_id}</h2><p className="muted-copy">Fresh evidence is under 2 hours old; evidence becomes stale after 24 hours.</p></div>
      <div className="two-column-grid">
        <EvidenceCard title="Release health shared by the pool" report={report.release_health} />
        <EvidenceCard title="Article availability shared by the pool" report={report.article_availability} />
      </div>
      <section className="page-card table-card stack">
        <h2 className="section-title">Member contributions</h2>
        <div className="table-scroll"><table className="data-table data-table--compact"><thead><tr><th>Node</th><th>Releases</th><th>Manifests</th><th>Health</th><th>Availability</th><th>Coverage</th><th>Last contribution</th></tr></thead><tbody>
          {report.contributors.map((item) => <tr key={item.node_id}><td>{item.alias || item.node_id}</td><td>{formatNumber(item.release_cards)}</td><td>{formatNumber(item.manifests)}</td><td>{formatNumber(item.health_attestations)}</td><td>{formatNumber(item.article_availability)}</td><td>{formatNumber(item.coverage_events)}</td><td>{formatDateTime(item.last_contribution_at)}</td></tr>)}
        </tbody></table></div>
      </section>
      <section className="page-card table-card stack">
        <h2 className="section-title">Recent article availability evidence</h2>
        <div className="table-scroll"><table className="data-table data-table--compact"><thead><tr><th>Release</th><th>Reporter</th><th>Status</th><th>Available</th><th>Checked</th></tr></thead><tbody>
          {availability.map((item) => <tr key={item.attestation_id}><td className="mono-cell">{item.release_id}</td><td className="mono-cell">{item.author_node_id}</td><td><Status value={item.status} /></td><td>{formatNumber(item.articles_available)} / {formatNumber(item.articles_total)}</td><td>{formatDateTime(item.checked_at)}</td></tr>)}
        </tbody></table></div>
      </section>
    </div>
  )
}

type Point = { at: string; success: number; failure: number; items: number }

function points(items: GoNZBNetActivityRollup[]): Point[] {
  const grouped = new Map<string, Point>()
  for (const item of items) {
    const current = grouped.get(item.bucket_start) ?? { at: item.bucket_start, success: 0, failure: 0, items: 0 }
    current.success += item.successes
    current.failure += item.failures
    current.items += item.items_in + item.items_out
    grouped.set(item.bucket_start, current)
  }
  return [...grouped.values()].sort((a, b) => a.at.localeCompare(b.at))
}

function ActivityChart({ data, value, label }: { data: Point[]; value: keyof Pick<Point, 'success' | 'failure' | 'items'>; label: string }) {
  const width = 720
  const height = 180
  const max = Math.max(1, ...data.map((point) => point[value]))
  const path = data.map((point, index) => `${index ? 'L' : 'M'} ${(index / Math.max(1, data.length - 1)) * width} ${height - (point[value] / max) * (height - 12)}`).join(' ')
  return (
    <article className="page-card stack">
      <div><h3 className="section-title">{label}</h3><p className="muted-copy">Peak {formatNumber(max)} per reporting bucket</p></div>
      {data.length ? <svg className={`gonzbnet-chart gonzbnet-chart--${value}`} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`${label} over time`}><path d={path} fill="none" vectorEffect="non-scaling-stroke" />{data.map((point, index) => <circle key={point.at} cx={(index / Math.max(1, data.length - 1)) * width} cy={height - (point[value] / max) * (height - 12)} r="4" tabIndex={0}><title>{formatDateTime(point.at)}: {formatNumber(point[value])}</title></circle>)}</svg> : <div className="gonzbnet-empty-chart">No activity recorded in this window.</div>}
    </article>
  )
}

function ActivityView({ report, window, onWindowChange }: { report: GoNZBNetActivityReport | null; window: string; onWindowChange: (window: string) => void }) {
  const data = points(report?.items ?? [])
  const failures = (report?.items ?? []).reduce((sum, item) => sum + item.failures, 0)
  return (
    <div className="stack">
      <div className="page-card gonzbnet-activity-toolbar">
        <div><p className="eyebrow">Runtime work</p><h2 className="section-title">Activity over time</h2></div>
        <label className="field"><span>Window</span><select className="table-input" value={window} onChange={(event) => onWindowChange(event.target.value)}><option value="1h">Last hour</option><option value="24h">Last 24 hours</option><option value="7d">Last 7 days</option><option value="30d">Last 30 days</option></select></label>
      </div>
      {failures ? <div className="banner error">{formatNumber(failures)} failed executions occurred in this window.</div> : null}
      {report?.partial ? <div className="banner">Activity history begins when this version starts recording rollups.</div> : null}
      <div className="two-column-grid"><ActivityChart data={data} value="success" label="Successful work" /><ActivityChart data={data} value="items" label="Items processed" /></div>
      <ActivityChart data={data} value="failure" label="Failed work" />
      <details className="page-card">
        <summary>Activity data table ({data.length} buckets)</summary>
        <div className="table-scroll gonzbnet-details-body"><table className="data-table data-table--compact"><thead><tr><th>Bucket</th><th>Successful</th><th>Failed</th><th>Items processed</th></tr></thead><tbody>{data.map((point) => <tr key={point.at}><td>{formatDateTime(point.at)}</td><td>{formatNumber(point.success)}</td><td>{formatNumber(point.failure)}</td><td>{formatNumber(point.items)}</td></tr>)}</tbody></table></div>
      </details>
    </div>
  )
}

export function GoNZBNetReporting(props: Props) {
  if (props.view === 'overview') return <OverviewView report={props.overview} />
  if (props.view === 'roles') return <RolesView report={props.roles} />
  if (props.view === 'pools') return <PoolsView report={props.poolHealth} availability={props.articleAvailability} />
  return <ActivityView report={props.activity} window={props.activityWindow} onWindowChange={props.onActivityWindowChange} />
}
