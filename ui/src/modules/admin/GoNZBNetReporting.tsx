import { useState } from 'react'
import type {
  GoNZBNetActivityReport,
  GoNZBNetActivityRollup,
  GoNZBNetArticleAvailabilityDiagnostic,
  GoNZBNetCoverageAssignment,
  GoNZBNetCoverageClaim,
  GoNZBNetCoverageOutcome,
  GoNZBNetEventDiagnostic,
  GoNZBNetHealthAttestationDiagnostic,
  GoNZBNetManifestSourceDiagnostic,
  GoNZBNetOverviewReport,
  GoNZBNetPeerDeliveryDiagnostic,
  GoNZBNetPeerDiagnostic,
  GoNZBNetPoolHealthReport,
  GoNZBNetReleaseSourceDiagnostic,
  GoNZBNetRoleJob,
  GoNZBNetRolesReport,
  GoNZBNetValidationTaskDiagnostic,
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
  evidence: RoleEvidence
}

export type RoleEvidence = {
  poolID: string
  events: GoNZBNetEventDiagnostic[]
  peers: GoNZBNetPeerDiagnostic[]
  deliveries: GoNZBNetPeerDeliveryDiagnostic[]
  validationTasks: GoNZBNetValidationTaskDiagnostic[]
  releaseSources: GoNZBNetReleaseSourceDiagnostic[]
  manifestSources: GoNZBNetManifestSourceDiagnostic[]
  health: GoNZBNetHealthAttestationDiagnostic[]
  articleAvailability: GoNZBNetArticleAvailabilityDiagnostic[]
  assignments: GoNZBNetCoverageAssignment[]
  claims: GoNZBNetCoverageClaim[]
  outcomes: GoNZBNetCoverageOutcome[]
}

const jobGuide: Record<string, { reads: string; produces: string; idle: string }> = {
  consume: {
    reads: 'Signed release cards and manifest advertisements received from eligible pools.',
    produces: 'Local searchable release/source rows and verified cached manifests for grabs.',
    idle: 'It waits when peers have sent no new releases and no local grab needs a manifest.',
  },
  contribute: {
    reads: 'Public-ready releases and generated manifests from this node’s local indexer.',
    produces: 'Signed ReleaseCard, ResolutionManifest, and manifest-availability events for pools.',
    idle: 'It publishes nothing when no new local release passes the public federation policy.',
  },
  verify: {
    reads: 'Pending manifest-validation tasks plus articles reachable through configured NNTP providers.',
    produces: 'Signed article-availability, checksum, and release-health attestations.',
    idle: 'It completes an empty pass when no unclaimed validation task or health candidate exists.',
  },
  coordinate: {
    reads: 'Pool coverage gaps, scanner capacity, active claims, checkpoints, and expired work.',
    produces: 'Assignments, claims, scanner observations, completion outcomes, and reassignment decisions.',
    idle: 'It waits when no coverage gap is assigned to this node and no stale claim needs reassignment.',
  },
  connection: {
    reads: 'Peer outboxes, local undelivered events, admission state, and authenticated gossip traffic.',
    produces: 'Accepted local event projections, peer deliveries, cursors, and connection health state.',
    idle: 'A successful zero-item pass means peers are reachable but already synchronized.',
  },
}

const statusText: Record<string, string> = {
  off: 'Off',
  local: 'This node',
  starting: 'Starting',
  ready: 'Ready',
  working: 'Working',
  degraded: 'Needs attention',
  blocked: 'Blocked',
}

const roleTabLabel: Record<string, string> = {
  consume: 'Find & use',
  contribute: 'Contribute',
  verify: 'Verify health',
  coordinate: 'Coordinate',
  connection: 'Connection',
}

const capabilityLabel: Record<string, string> = {
  admin: 'Pool administration',
  consumer: 'Release consumer',
  scanner: 'Scanner',
  indexer: 'Indexer',
  manifest_builder: 'Manifest builder',
  manifest_cache: 'Manifest cache',
  validator: 'Validator',
  health_checker: 'Health checker',
  relay: 'Relay',
  coverage: 'Coverage worker',
  scheduler: 'Scheduler',
  coverage_coordinator: 'Coverage coordinator',
}

function Status({ value }: { value: string }) {
  return <span className={`status-pill status-pill--table gonzbnet-status gonzbnet-status--${value}`}>{statusText[value] ?? value}</span>
}

function shortID(value?: string) {
  if (!value) return '—'
  return value.length > 18 ? `${value.slice(0, 10)}…${value.slice(-6)}` : value
}

function Metric({ label, value, detail }: { label: string; value: string | number; detail?: string }) {
  return <div className="stat-card"><span>{label}</span><strong>{typeof value === 'number' ? formatNumber(value) : value}</strong>{detail ? <small>{detail}</small> : null}</div>
}

function TaskResult({ component }: { component: GoNZBNetRoleJob['components'][number] }) {
  const passes = component.successes + component.failures
  const work = component.items_in + component.items_out
  let result = `${formatNumber(component.items_in)} read · ${formatNumber(component.items_out)} produced`
  if (passes > 0 && work === 0) result = `${formatNumber(passes)} passes completed; no eligible work found`
  if (passes === 0) result = component.execution_mode === 'on_demand' ? 'Has not been requested yet' : 'Has not run yet'
  return (
    <div className="gonzbnet-task-result">
      <strong>{result}</strong>
      <span>{formatNumber(component.successes)} successful passes · {formatNumber(component.failures)} failed</span>
      <span>{component.last_useful_at ? `Last result ${formatDateTime(component.last_useful_at)}` : 'No useful result recorded yet'}</span>
      <span>{component.last_success_at ? `Last check ${formatDateTime(component.last_success_at)}` : 'No completed check yet'}</span>
      {component.next_run_at ? <span>Next check {formatDateTime(component.next_run_at)}</span> : null}
    </div>
  )
}

function EmptyEvidence({ children }: { children: string }) {
  return <div className="gonzbnet-evidence-empty">{children}</div>
}

function displayCapability(value: string) {
  return capabilityLabel[value] ?? value.replaceAll('_', ' ')
}

function displayNodeAddress(value: string) {
  if (!value) return 'No advertised address'
  try {
    return new URL(value).host || value
  } catch {
    return value
  }
}

function PoolMemberRoster({ pools }: { pools: GoNZBNetOverviewReport['pools'] }) {
  return (
    <section className="page-card stack">
      <div><p className="eyebrow">Who is in each pool</p><h2 className="section-title">Pool members</h2><p className="muted-copy">A node can hold several pool roles. The node count is unique; roles are listed under that node.</p></div>
      <div className="gonzbnet-pool-rosters">
        {pools.map((pool) => <section className="gonzbnet-pool-roster stack" key={pool.pool_id}>
          <div className="gonzbnet-pool-roster__header"><div><h3>{pool.display_name || pool.pool_id}</h3><span className="muted-copy mono-cell">{pool.pool_id}</span></div><strong>{formatNumber(pool.members)} node{pool.members === 1 ? '' : 's'}</strong></div>
          {(pool.member_nodes ?? []).length ? <div className="gonzbnet-member-grid">{(pool.member_nodes ?? []).map((member) => <article className="gonzbnet-member-card" key={member.node_id}>
            <div className="gonzbnet-member-card__header"><div><strong>{member.alias || (member.local ? 'This node' : shortID(member.node_id))}</strong><div className="muted-copy mono-cell" title={member.node_id}>{shortID(member.node_id)}</div></div><Status value={member.local ? 'local' : member.status} /></div>
            <dl className="gonzbnet-member-details">
              <div><dt>Address</dt><dd>{displayNodeAddress(member.base_url)}{member.local ? ' (this dashboard)' : ''}</dd></div>
              <div><dt>Pool roles</dt><dd>{member.roles.length ? member.roles.join(' · ') : 'Member'}</dd></div>
              <div><dt>Capabilities</dt><dd>{member.capabilities.length ? member.capabilities.map(displayCapability).join(' · ') : 'No operational capabilities granted'}</dd></div>
            </dl>
          </article>)}</div> : <EmptyEvidence>No active nodes are recorded for this pool.</EmptyEvidence>}
        </section>)}
      </div>
      <p className="muted-copy">Set a recognizable node alias and advertised URL under <a href="/admin/settings?tab=gonzbnet">Settings → GoNZBNet</a> so other pool administrators can identify this node.</p>
    </section>
  )
}

function RecentSignedEvents({ events, nodeID }: { events: GoNZBNetEventDiagnostic[]; nodeID: string }) {
  const local = events.filter((event) => event.author_node_id === nodeID).slice(0, 8)
  if (!local.length) return <EmptyEvidence>This node has not produced a matching signed result in the loaded diagnostic window.</EmptyEvidence>
  return (
    <div className="table-scroll"><table className="data-table data-table--compact"><thead><tr><th>Result</th><th>Pool</th><th>Status</th><th>Created</th></tr></thead><tbody>{local.map((event) => (
      <tr key={event.event_id}><td>{event.event_type}<div className="muted-copy mono-cell">{shortID(event.event_id)}</div></td><td>{event.pool_ids.join(', ') || 'network'}</td><td><Status value={event.validation_status} /></td><td>{formatDateTime(event.created_at)}</td></tr>
    ))}</tbody></table></div>
  )
}

function RoleEvidencePanel({ job, nodeID, evidence }: { job: GoNZBNetRoleJob; nodeID: string; evidence: RoleEvidence }) {
  const poolEvents = evidence.events.filter((event) => event.pool_ids.includes(evidence.poolID))
  if (job.key === 'consume') {
    return <section className="gonzbnet-evidence stack"><h4>Data this role has produced locally</h4><div className="stat-grid"><Metric label="Pool releases" value={evidence.releaseSources.length} detail="projected into local search" /><Metric label="Resolvable" value={evidence.releaseSources.filter((item) => item.resolvable).length} detail="have an authorized manifest source" /><Metric label="Manifest sources" value={evidence.manifestSources.length} detail="known verified sources" /></div>{evidence.releaseSources.length ? <div className="table-scroll"><table className="data-table data-table--compact"><thead><tr><th>Release</th><th>Pool</th><th>Availability</th><th>Resolvable</th><th>Last seen</th></tr></thead><tbody>{evidence.releaseSources.slice(0, 8).map((item) => <tr key={`${item.release_id}-${item.source_node_id}`}><td>{item.title || shortID(item.release_id)}<div className="muted-copy mono-cell">{shortID(item.release_id)}</div></td><td>{item.pool_id}</td><td>{Math.round(item.availability_score * 100)}%</td><td>{item.resolvable ? 'Yes' : 'No'}</td><td>{formatDateTime(item.last_seen_at)}</td></tr>)}</tbody></table></div> : <EmptyEvidence>No federated releases have been projected into the local searchable catalog yet.</EmptyEvidence>}</section>
  }
  if (job.key === 'contribute') {
    const types = new Set(['ReleaseCard', 'ResolutionManifest', 'ManifestAvailability'])
    const events = poolEvents.filter((event) => types.has(event.event_type))
    const local = events.filter((event) => event.author_node_id === nodeID)
    return <section className="gonzbnet-evidence stack"><h4>Signed results published by this node</h4><div className="stat-grid"><Metric label="Release cards" value={local.filter((item) => item.event_type === 'ReleaseCard').length} /><Metric label="Manifests" value={local.filter((item) => item.event_type === 'ResolutionManifest').length} /><Metric label="Availability notices" value={local.filter((item) => item.event_type === 'ManifestAvailability').length} /></div><RecentSignedEvents events={events} nodeID={nodeID} /></section>
  }
  if (job.key === 'verify') {
    const types = new Set(['ValidatorCapacity', 'ArticleAvailabilityAttestation', 'ChecksumAttestation', 'HealthAttestation'])
    const signed = poolEvents.filter((event) => types.has(event.event_type))
    const tasks = evidence.validationTasks.filter((item) => item.pool_id === evidence.poolID)
    return (
      <section className="gonzbnet-evidence stack">
        <h4>Validation inputs and results</h4>
        <div className="stat-grid">
          <Metric label="Pending tasks" value={tasks.filter((item) => item.status === 'pending').length} detail={`${tasks.filter((item) => item.status === 'completed').length} completed in loaded rows`} />
          <Metric label="Article results" value={evidence.articleAvailability.length} detail="signed reachability attestations" />
          <Metric label="Health results" value={evidence.health.length} detail="signed release-health attestations" />
        </div>
        <h4>Recent article availability and health evidence</h4>
        {evidence.articleAvailability.length || evidence.health.length ? (
          <div className="table-scroll"><table className="data-table data-table--compact"><thead><tr><th>Evidence</th><th>Release</th><th>Status</th><th>Articles</th><th>Method</th><th>Checked</th></tr></thead><tbody>
            {evidence.articleAvailability.slice(0, 6).map((item) => <tr key={item.attestation_id}><td>Article availability</td><td className="mono-cell">{shortID(item.release_id)}</td><td><Status value={item.status} /></td><td>{formatNumber(item.articles_available)} / {formatNumber(item.articles_total)}</td><td>{item.method}</td><td>{formatDateTime(item.checked_at)}</td></tr>)}
            {evidence.health.slice(0, 6).map((item) => <tr key={item.attestation_id}><td>Release health</td><td className="mono-cell">{shortID(item.release_id)}</td><td><Status value={item.status} /></td><td>{formatNumber(item.articles_available)} / {formatNumber(item.articles_total)}</td><td>{item.method}</td><td>{formatDateTime(item.checked_at)}</td></tr>)}
          </tbody></table></div>
        ) : <EmptyEvidence>The validator is polling successfully, but it has produced no validation or health evidence yet. It needs pending manifest tasks sourced from pool releases.</EmptyEvidence>}
        <h4>Validation queue</h4>
        {tasks.length ? (
          <div className="table-scroll"><table className="data-table data-table--compact"><thead><tr><th>Release</th><th>Manifest</th><th>Pool</th><th>Status</th><th>Attempts</th><th>Updated</th></tr></thead><tbody>
            {tasks.slice(0, 10).map((item) => <tr key={item.task_id}><td className="mono-cell">{shortID(item.release_id)}</td><td className="mono-cell">{shortID(item.manifest_id)}</td><td>{item.pool_id}</td><td><Status value={item.status} /></td><td>{formatNumber(item.attempts)}</td><td>{formatDateTime(item.completed_at ?? item.claimed_at ?? item.updated_at)}</td></tr>)}
          </tbody></table></div>
        ) : <EmptyEvidence>No validation tasks have been created from received manifests.</EmptyEvidence>}
        <h4>Recent signed validator output</h4>
        <RecentSignedEvents events={signed} nodeID={nodeID} />
      </section>
    )
  }
  if (job.key === 'coordinate') {
    return <section className="gonzbnet-evidence stack"><h4>Coverage work coordinated by this node</h4><div className="stat-grid"><Metric label="Assignments" value={evidence.assignments.length} /><Metric label="Active claims" value={evidence.claims.filter((item) => item.status === 'active').length} /><Metric label="Outcomes" value={evidence.outcomes.length} detail={`${evidence.outcomes.reduce((sum, item) => sum + (item.release_count ?? 0), 0)} releases reported`} /></div>{evidence.outcomes.length ? <div className="table-scroll"><table className="data-table data-table--compact"><thead><tr><th>Group</th><th>Range</th><th>Outcome</th><th>Releases</th><th>At</th></tr></thead><tbody>{evidence.outcomes.slice(0, 8).map((item) => <tr key={item.outcome_id}><td>{item.group}</td><td>{item.range_start}–{item.range_end}</td><td><Status value={item.outcome_type} /></td><td>{formatNumber(item.release_count ?? 0)}</td><td>{formatDateTime(item.occurred_at)}</td></tr>)}</tbody></table></div> : <EmptyEvidence>No coverage completion or failure results exist in the selected pool.</EmptyEvidence>}</section>
  }
  return <section className="gonzbnet-evidence stack"><h4>Peer exchange results</h4><div className="stat-grid"><Metric label="Peers" value={evidence.peers.length} detail={`${evidence.peers.filter((item) => item.status === 'connected').length} connected`} /><Metric label="Deliveries" value={evidence.deliveries.length} detail={`${evidence.deliveries.filter((item) => item.status === 'accepted').length} accepted`} /><Metric label="Accepted events" value={poolEvents.filter((item) => item.validation_status === 'accepted').length} detail="in loaded diagnostic rows" /></div>{evidence.peers.length ? <div className="table-scroll"><table className="data-table data-table--compact"><thead><tr><th>Peer</th><th>Status</th><th>Failures</th><th>Last sync</th></tr></thead><tbody>{evidence.peers.slice(0, 8).map((item) => <tr key={item.id}><td>{item.peer_url}</td><td><Status value={item.enabled ? item.status : 'off'} /></td><td>{formatNumber(item.failure_count)}</td><td>{formatDateTime(item.last_sync_at)}</td></tr>)}</tbody></table></div> : <EmptyEvidence>No peers are configured, so pull and push passes can only complete with zero exchanged events.</EmptyEvidence>}</section>
}

function RoleSummary({ job, detail = false, nodeID = '', evidence }: { job: GoNZBNetRoleJob; detail?: boolean; nodeID?: string; evidence?: RoleEvidence }) {
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
        <>
        <div className="gonzbnet-role-flow">
          <div><span>Reads</span><p>{jobGuide[job.key]?.reads}</p></div>
          <div><span>Produces</span><p>{jobGuide[job.key]?.produces}</p></div>
          <div><span>Idle means</span><p>{jobGuide[job.key]?.idle}</p></div>
        </div>
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
              </div>
              <TaskResult component={component} />
              {component.reason ? <div className="muted-copy">{component.reason}</div> : null}
              {component.last_error ? <div className="banner error">{component.last_error}</div> : null}
            </div>
          ))}
        </div>
        {evidence ? <RoleEvidencePanel job={job} nodeID={nodeID} evidence={evidence} /> : null}
        </>
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

function RolesView({ report, evidence }: { report: GoNZBNetRolesReport | null; evidence: RoleEvidence }) {
  const [selectedKey, setSelectedKey] = useState('')
  if (!report) return <div className="page-card muted-copy">Role reporting is unavailable.</div>
  const enabled = report.jobs.filter((job) => job.configured)
  const disabled = report.jobs.filter((job) => !job.configured)
  const selected = enabled.find((job) => job.key === selectedKey) ?? enabled[0]
  return (
    <div className="stack">
      <section className="page-card stack">
        <div><p className="eyebrow">What this node does</p><h2 className="section-title">Enabled jobs</h2></div>
        {selected ? <>
          <div className="settings-tabs gonzbnet-role-tabs" role="tablist" aria-label="Enabled GoNZBNet roles">
            {enabled.map((job) => {
              const active = job.key === selected.key
              return <button className={`settings-tab${active ? ' is-active' : ''}`} id={`gonzbnet-role-tab-${job.key}`} type="button" role="tab" aria-selected={active} aria-controls={`gonzbnet-role-panel-${job.key}`} onClick={() => setSelectedKey(job.key)} key={job.key}><span>{roleTabLabel[job.key] ?? job.label}</span><Status value={job.status} /></button>
            })}
          </div>
          <div id={`gonzbnet-role-panel-${selected.key}`} role="tabpanel" aria-labelledby={`gonzbnet-role-tab-${selected.key}`}>
            <RoleSummary detail job={selected} nodeID={report.node_id} evidence={evidence} />
          </div>
        </> : <EmptyEvidence>No GoNZBNet jobs are enabled on this node.</EmptyEvidence>}
      </section>
      <details className="page-card">
        <summary>Off jobs ({disabled.length})</summary>
        <div className="gonzbnet-job-grid gonzbnet-details-body">{disabled.map((job) => <RoleSummary detail job={job} nodeID={report.node_id} evidence={evidence} key={job.key} />)}</div>
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

function PoolsView({ report, pool }: { report: GoNZBNetPoolHealthReport | null; pool?: GoNZBNetOverviewReport['pools'][number] }) {
  if (!report) return <div className="page-card muted-copy">Select or create a pool to see shared health and contribution reporting.</div>
  return (
    <div className="stack">
      <div className="page-card"><p className="eyebrow">Selected pool</p><h2 className="section-title">{report.pool_id}</h2><p className="muted-copy">Fresh evidence is under 2 hours old; evidence becomes stale after 24 hours.</p></div>
      {pool ? <PoolMemberRoster pools={[pool]} /> : null}
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
  if (props.view === 'roles') return <RolesView report={props.roles} evidence={props.evidence} />
  if (props.view === 'pools') return <PoolsView report={props.poolHealth} pool={props.overview?.pools.find((pool) => pool.pool_id === props.poolHealth?.pool_id)} />
  return <ActivityView report={props.activity} window={props.activityWindow} onWindowChange={props.onActivityWindowChange} />
}
