import { useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import {
  createGoNZBNetCoverageAssignment,
  createGoNZBNetCoverageClaim,
  createGoNZBNetCoverageComplete,
  createGoNZBNetCoverageFailed,
  getGoNZBNetCoverageDashboard,
  getGoNZBNetCoverageGroups,
  getGoNZBNetCoveragePlan,
  getGoNZBNetCoverageSuggestions,
  getGoNZBNetEventDiagnostics,
  getGoNZBNetNodeCapabilities,
  getGoNZBNetPeerDeliveryDiagnostics,
  getGoNZBNetPeerDiagnostics,
  getGoNZBNetRejectedEventDiagnostics,
  getGoNZBNetValidationTaskDiagnostics,
  getGoNZBNetValidationGaps,
  materializeGoNZBNetStalePenalties,
} from '../../shared/api/admin'
import { formatDateTime, formatNumber } from '../../shared/lib/format'
import type {
  GoNZBNetCoverageAssignment,
  GoNZBNetCoverageClaim,
  GoNZBNetCoverageDashboard,
  GoNZBNetCoverageOutcome,
  GoNZBNetCoveragePlan,
  GoNZBNetCoverageSuggestion,
  GoNZBNetEventDiagnostic,
  GoNZBNetGroupCatalogItem,
  GoNZBNetNodeCapability,
  GoNZBNetPeerDeliveryDiagnostic,
  GoNZBNetPeerDiagnostic,
  GoNZBNetRejectedEventDiagnostic,
  GoNZBNetValidationTaskDiagnostic,
  GoNZBNetValidationGap,
} from '../../shared/types'

type ActionMode = 'scanner' | 'validator'

type AssignmentForm = {
  assignment_id: string
  group: string
  assigned_node_id: string
  range_start: string
  range_end: string
  priority: string
  due_at: string
}

type ClaimForm = {
  assignment_id: string
  group: string
  range_start: string
  range_end: string
  expires_at: string
}

type OutcomeForm = {
  claim_id: string
  assignment_id: string
  group: string
  range_start: string
  range_end: string
  release_count: string
  reason: string
}

const defaultPoolID = 'pool.local'

const defaultAssignmentForm: AssignmentForm = {
  assignment_id: '',
  group: '',
  assigned_node_id: '',
  range_start: '',
  range_end: '',
  priority: '0',
  due_at: '',
}

const defaultClaimForm: ClaimForm = {
  assignment_id: '',
  group: '',
  range_start: '',
  range_end: '',
  expires_at: '',
}

const defaultOutcomeForm: OutcomeForm = {
  claim_id: '',
  assignment_id: '',
  group: '',
  range_start: '',
  range_end: '',
  release_count: '0',
  reason: '',
}

function optionalNumber(value: string) {
  const trimmed = value.trim()
  if (!trimmed) {
    return undefined
  }
  const parsed = Number(trimmed)
  return Number.isFinite(parsed) ? parsed : undefined
}

function requiredNumber(value: string) {
  return optionalNumber(value) ?? 0
}

function label(value?: string | number | null, fallback = 'n/a') {
  if (value === undefined || value === null || value === '') {
    return fallback
  }
  return String(value)
}

function shortID(value?: string) {
  if (!value) {
    return 'n/a'
  }
  if (value.length <= 18) {
    return value
  }
  return `${value.slice(0, 10)}...${value.slice(-6)}`
}

function rangeLabel(item: { range_start?: number; range_end?: number }) {
  if (!item.range_start && !item.range_end) {
    return 'n/a'
  }
  return `${formatNumber(item.range_start ?? 0)} - ${formatNumber(item.range_end ?? 0)}`
}

function capabilityKeys(value?: Record<string, unknown> | null) {
  if (!value) {
    return []
  }
  return Object.entries(value)
    .filter(([, enabled]) => enabled === true)
    .map(([key]) => key)
    .sort()
}

function firstNodeID(nodes: GoNZBNetNodeCapability[]) {
  return nodes.find((node) => node.status === 'local')?.node_id ?? nodes[0]?.node_id ?? ''
}

function StatCard({
  label: statLabel,
  value,
  detail,
}: {
  label: string
  value: string
  detail: string
}) {
  return (
    <div className="stat-card">
      <span>{statLabel}</span>
      <strong>{value}</strong>
      <small>{detail}</small>
    </div>
  )
}

function SectionTable({
  title,
  count,
  children,
}: {
  title: string
  count?: number
  children: ReactNode
}) {
  return (
    <div className="page-card stack">
      <div className="release-table-toolbar">
        <h2 className="section-title">{title}</h2>
        {typeof count === 'number' ? (
          <span className="muted-copy">{formatNumber(count)}</span>
        ) : null}
      </div>
      <div className="table-shell">{children}</div>
    </div>
  )
}

function AssignmentRows({ rows }: { rows: GoNZBNetCoverageAssignment[] }) {
  return (
    <table className="data-table data-table--compact">
      <thead>
        <tr>
          <th>Assignment</th>
          <th>Group</th>
          <th>Node</th>
          <th>Range</th>
          <th>Priority</th>
          <th>Status</th>
          <th>Created</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((item) => (
          <tr key={item.assignment_id}>
            <td className="mono-cell breakable-value" title={item.assignment_id}>{shortID(item.assignment_id)}</td>
            <td className="breakable-value">{item.group}</td>
            <td className="mono-cell breakable-value" title={item.assigned_node_id}>{shortID(item.assigned_node_id)}</td>
            <td>{rangeLabel(item)}</td>
            <td>{formatNumber(item.priority)}</td>
            <td><span className="status-pill status-pill--table">{label(item.status)}</span></td>
            <td>{formatDateTime(item.created_at)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function ClaimRows({ rows }: { rows: GoNZBNetCoverageClaim[] }) {
  return (
    <table className="data-table data-table--compact">
      <thead>
        <tr>
          <th>Claim</th>
          <th>Group</th>
          <th>Node</th>
          <th>Range</th>
          <th>Type</th>
          <th>Status</th>
          <th>Expires</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((item) => (
          <tr key={item.claim_id}>
            <td className="mono-cell breakable-value" title={item.claim_id}>{shortID(item.claim_id)}</td>
            <td className="breakable-value">{item.group}</td>
            <td className="mono-cell breakable-value" title={item.node_id}>{shortID(item.node_id)}</td>
            <td>{rangeLabel(item)}</td>
            <td><span className="status-pill status-pill--table">{item.claim_type}</span></td>
            <td><span className="status-pill status-pill--table">{item.status}</span></td>
            <td>{formatDateTime(item.expires_at)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function OutcomeRows({ rows }: { rows: GoNZBNetCoverageOutcome[] }) {
  return (
    <table className="data-table data-table--compact">
      <thead>
        <tr>
          <th>Outcome</th>
          <th>Group</th>
          <th>Range</th>
          <th>Type</th>
          <th>Releases</th>
          <th>Reason</th>
          <th>At</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((item) => (
          <tr key={item.outcome_id}>
            <td className="mono-cell breakable-value" title={item.outcome_id}>{shortID(item.outcome_id)}</td>
            <td className="breakable-value">{item.group}</td>
            <td>{rangeLabel(item)}</td>
            <td><span className="status-pill status-pill--table">{item.outcome_type}</span></td>
            <td>{formatNumber(item.release_count ?? 0)}</td>
            <td>{label(item.reason)}</td>
            <td>{formatDateTime(item.occurred_at)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export function AdminGoNZBNetPage() {
  const [poolID, setPoolID] = useState(defaultPoolID)
  const [mode, setMode] = useState<ActionMode>('scanner')
  const [nodes, setNodes] = useState<GoNZBNetNodeCapability[]>([])
  const [dashboard, setDashboard] = useState<GoNZBNetCoverageDashboard | null>(null)
  const [groups, setGroups] = useState<GoNZBNetGroupCatalogItem[]>([])
  const [validationGaps, setValidationGaps] = useState<GoNZBNetValidationGap[]>([])
  const [suggestions, setSuggestions] = useState<GoNZBNetCoverageSuggestion[]>([])
  const [plan, setPlan] = useState<GoNZBNetCoveragePlan | null>(null)
  const [peerDiagnostics, setPeerDiagnostics] = useState<GoNZBNetPeerDiagnostic[]>([])
  const [eventDiagnostics, setEventDiagnostics] = useState<GoNZBNetEventDiagnostic[]>([])
  const [rejectedDiagnostics, setRejectedDiagnostics] = useState<GoNZBNetRejectedEventDiagnostic[]>([])
  const [deliveryDiagnostics, setDeliveryDiagnostics] = useState<GoNZBNetPeerDeliveryDiagnostic[]>([])
  const [validationTaskDiagnostics, setValidationTaskDiagnostics] = useState<GoNZBNetValidationTaskDiagnostic[]>([])
  const [assignmentForm, setAssignmentForm] = useState<AssignmentForm>(defaultAssignmentForm)
  const [claimForm, setClaimForm] = useState<ClaimForm>(defaultClaimForm)
  const [completeForm, setCompleteForm] = useState<OutcomeForm>(defaultOutcomeForm)
  const [failedForm, setFailedForm] = useState<OutcomeForm>(defaultOutcomeForm)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [actionStatus, setActionStatus] = useState<string | null>(null)

  const effectivePoolID = poolID.trim() || defaultPoolID
  const suggestedNodeID = useMemo(() => firstNodeID(nodes), [nodes])

  async function refresh() {
    setLoading(true)
    try {
      const [
        nextNodes,
        nextDashboard,
        nextGroups,
        nextValidationGaps,
        nextSuggestions,
        nextPlan,
        nextPeers,
        nextEvents,
        nextRejected,
        nextDeliveries,
        nextValidationTasks,
      ] =
        await Promise.all([
          getGoNZBNetNodeCapabilities(),
          getGoNZBNetCoverageDashboard(effectivePoolID),
          getGoNZBNetCoverageGroups(effectivePoolID),
          getGoNZBNetValidationGaps(effectivePoolID, 100),
          getGoNZBNetCoverageSuggestions({ pool_id: effectivePoolID, mode, limit: 25 }),
          getGoNZBNetCoveragePlan({ pool_id: effectivePoolID, mode, limit: 25 }),
          getGoNZBNetPeerDiagnostics(100),
          getGoNZBNetEventDiagnostics(100),
          getGoNZBNetRejectedEventDiagnostics(100),
          getGoNZBNetPeerDeliveryDiagnostics(100),
          getGoNZBNetValidationTaskDiagnostics(100),
        ])
      setNodes(nextNodes.items ?? [])
      setDashboard(nextDashboard)
      setGroups(nextGroups.items ?? [])
      setValidationGaps(nextValidationGaps.items ?? [])
      setSuggestions(nextSuggestions.items ?? [])
      setPlan(nextPlan)
      setPeerDiagnostics(nextPeers.items ?? [])
      setEventDiagnostics(nextEvents.items ?? [])
      setRejectedDiagnostics(nextRejected.items ?? [])
      setDeliveryDiagnostics(nextDeliveries.items ?? [])
      setValidationTaskDiagnostics(nextValidationTasks.items ?? [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load GoNZBNet admin state')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void refresh()
  }, [effectivePoolID, mode])

  async function handleAssignment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await createGoNZBNetCoverageAssignment({
        assignment_id: assignmentForm.assignment_id.trim() || undefined,
        pool_id: effectivePoolID,
        group: assignmentForm.group.trim(),
        assigned_node_id: assignmentForm.assigned_node_id.trim() || suggestedNodeID,
        range_start: optionalNumber(assignmentForm.range_start),
        range_end: optionalNumber(assignmentForm.range_end),
        priority: optionalNumber(assignmentForm.priority),
        due_at: assignmentForm.due_at.trim() || undefined,
      })
      setActionStatus(`Assignment signed ${shortID(response.event_id)}`)
      setAssignmentForm(defaultAssignmentForm)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create coverage assignment')
    }
  }

  async function handleClaim(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await createGoNZBNetCoverageClaim({
        assignment_id: claimForm.assignment_id.trim() || undefined,
        pool_id: effectivePoolID,
        group: claimForm.group.trim(),
        range_start: optionalNumber(claimForm.range_start),
        range_end: optionalNumber(claimForm.range_end),
        expires_at: claimForm.expires_at.trim() || undefined,
      })
      setActionStatus(`Claim signed ${shortID(response.event_id)}`)
      setClaimForm(defaultClaimForm)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create coverage claim')
    }
  }

  async function handleComplete(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await createGoNZBNetCoverageComplete({
        claim_id: completeForm.claim_id.trim() || undefined,
        assignment_id: completeForm.assignment_id.trim() || undefined,
        pool_id: effectivePoolID,
        group: completeForm.group.trim(),
        range_start: requiredNumber(completeForm.range_start),
        range_end: requiredNumber(completeForm.range_end),
        release_count: optionalNumber(completeForm.release_count),
      })
      setActionStatus(`Complete signed ${shortID(response.event_id)}`)
      setCompleteForm(defaultOutcomeForm)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create completion outcome')
    }
  }

  async function handleFailed(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await createGoNZBNetCoverageFailed({
        claim_id: failedForm.claim_id.trim() || undefined,
        assignment_id: failedForm.assignment_id.trim() || undefined,
        pool_id: effectivePoolID,
        group: failedForm.group.trim(),
        range_start: requiredNumber(failedForm.range_start),
        range_end: requiredNumber(failedForm.range_end),
        reason: failedForm.reason.trim(),
      })
      setActionStatus(`Failure signed ${shortID(response.event_id)}`)
      setFailedForm(defaultOutcomeForm)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create failure outcome')
    }
  }

  async function handleStalePenalties() {
    try {
      const response = await materializeGoNZBNetStalePenalties(effectivePoolID)
      setActionStatus(`${formatNumber(response.created ?? 0)} stale penalties materialized`)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to materialize stale penalties')
    }
  }

  const assignments = dashboard?.assignments ?? []
  const claims = dashboard?.claims ?? []
  const staleClaims = dashboard?.stale_claims ?? []
  const outcomes = dashboard?.outcomes ?? []
  const gaps = dashboard?.gaps ?? []
  const duplicates = dashboard?.duplicates ?? []
  const coveragePercent = Math.round((dashboard?.coverage_score ?? 0) * 100)

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <div className="page-header">
          <div>
            <p className="eyebrow">GoNZBNet</p>
            <h1 className="page-title">Federation coverage</h1>
          </div>
          <button className="secondary-button" type="button" onClick={() => void refresh()} disabled={loading}>
            {loading ? 'Loading...' : 'Refresh'}
          </button>
        </div>
        <div className="toolbar-grid">
          <label className="field">
            <span>Pool</span>
            <input className="table-input" value={poolID} onChange={(event) => setPoolID(event.target.value)} />
          </label>
          <label className="field">
            <span>Mode</span>
            <select className="table-input" value={mode} onChange={(event) => setMode(event.target.value as ActionMode)}>
              <option value="scanner">scanner</option>
              <option value="validator">validator</option>
            </select>
          </label>
        </div>
        {error ? <div className="banner error">{error}</div> : null}
        {actionStatus ? <div className="banner">{actionStatus}</div> : null}
        <div className="stat-grid">
          <StatCard label="Coverage" value={`${coveragePercent}%`} detail={`${formatNumber(outcomes.length)} outcomes`} />
          <StatCard label="Assignments" value={formatNumber(assignments.length)} detail={`${formatNumber(gaps.length)} open gaps`} />
          <StatCard label="Claims" value={formatNumber(claims.length)} detail={`${formatNumber(staleClaims.length)} stale`} />
          <StatCard label="Validation gaps" value={formatNumber(validationGaps.length)} detail={`${formatNumber(duplicates.length)} duplicate ranges`} />
          <StatCard label="Peers" value={formatNumber(peerDiagnostics.length)} detail={`${formatNumber(deliveryDiagnostics.length)} delivery records`} />
          <StatCard label="Event log" value={formatNumber(eventDiagnostics.length)} detail={`${formatNumber(rejectedDiagnostics.length)} rejected events`} />
        </div>
      </div>

      <div className="two-column-grid">
        <form className="page-card stack" onSubmit={handleAssignment}>
          <h2 className="section-title">Assignment</h2>
          <div className="toolbar-grid">
            <label className="field">
              <span>Group</span>
              <input className="table-input" required value={assignmentForm.group} onChange={(event) => setAssignmentForm({ ...assignmentForm, group: event.target.value })} />
            </label>
            <label className="field">
              <span>Node</span>
              <input className="table-input" value={assignmentForm.assigned_node_id || suggestedNodeID} onChange={(event) => setAssignmentForm({ ...assignmentForm, assigned_node_id: event.target.value })} />
            </label>
            <label className="field">
              <span>Range start</span>
              <input className="table-input" inputMode="numeric" value={assignmentForm.range_start} onChange={(event) => setAssignmentForm({ ...assignmentForm, range_start: event.target.value })} />
            </label>
            <label className="field">
              <span>Range end</span>
              <input className="table-input" inputMode="numeric" value={assignmentForm.range_end} onChange={(event) => setAssignmentForm({ ...assignmentForm, range_end: event.target.value })} />
            </label>
            <label className="field">
              <span>Priority</span>
              <input className="table-input" inputMode="numeric" value={assignmentForm.priority} onChange={(event) => setAssignmentForm({ ...assignmentForm, priority: event.target.value })} />
            </label>
            <label className="field">
              <span>Assignment ID</span>
              <input className="table-input" value={assignmentForm.assignment_id} onChange={(event) => setAssignmentForm({ ...assignmentForm, assignment_id: event.target.value })} />
            </label>
          </div>
          <button className="primary-button align-end" type="submit">Sign assignment</button>
        </form>

        <form className="page-card stack" onSubmit={handleClaim}>
          <h2 className="section-title">Claim</h2>
          <div className="toolbar-grid">
            <label className="field">
              <span>Group</span>
              <input className="table-input" required value={claimForm.group} onChange={(event) => setClaimForm({ ...claimForm, group: event.target.value })} />
            </label>
            <label className="field">
              <span>Assignment ID</span>
              <input className="table-input" value={claimForm.assignment_id} onChange={(event) => setClaimForm({ ...claimForm, assignment_id: event.target.value })} />
            </label>
            <label className="field">
              <span>Range start</span>
              <input className="table-input" inputMode="numeric" value={claimForm.range_start} onChange={(event) => setClaimForm({ ...claimForm, range_start: event.target.value })} />
            </label>
            <label className="field">
              <span>Range end</span>
              <input className="table-input" inputMode="numeric" value={claimForm.range_end} onChange={(event) => setClaimForm({ ...claimForm, range_end: event.target.value })} />
            </label>
            <label className="field">
              <span>Expires at</span>
              <input className="table-input" value={claimForm.expires_at} onChange={(event) => setClaimForm({ ...claimForm, expires_at: event.target.value })} />
            </label>
          </div>
          <button className="primary-button align-end" type="submit">Sign claim</button>
        </form>
      </div>

      <div className="two-column-grid">
        <form className="page-card stack" onSubmit={handleComplete}>
          <h2 className="section-title">Complete</h2>
          <div className="toolbar-grid">
            <label className="field">
              <span>Group</span>
              <input className="table-input" required value={completeForm.group} onChange={(event) => setCompleteForm({ ...completeForm, group: event.target.value })} />
            </label>
            <label className="field">
              <span>Claim ID</span>
              <input className="table-input" value={completeForm.claim_id} onChange={(event) => setCompleteForm({ ...completeForm, claim_id: event.target.value })} />
            </label>
            <label className="field">
              <span>Range start</span>
              <input className="table-input" required inputMode="numeric" value={completeForm.range_start} onChange={(event) => setCompleteForm({ ...completeForm, range_start: event.target.value })} />
            </label>
            <label className="field">
              <span>Range end</span>
              <input className="table-input" required inputMode="numeric" value={completeForm.range_end} onChange={(event) => setCompleteForm({ ...completeForm, range_end: event.target.value })} />
            </label>
            <label className="field">
              <span>Releases</span>
              <input className="table-input" inputMode="numeric" value={completeForm.release_count} onChange={(event) => setCompleteForm({ ...completeForm, release_count: event.target.value })} />
            </label>
          </div>
          <button className="primary-button align-end" type="submit">Sign complete</button>
        </form>

        <form className="page-card stack" onSubmit={handleFailed}>
          <h2 className="section-title">Failed</h2>
          <div className="toolbar-grid">
            <label className="field">
              <span>Group</span>
              <input className="table-input" required value={failedForm.group} onChange={(event) => setFailedForm({ ...failedForm, group: event.target.value })} />
            </label>
            <label className="field">
              <span>Claim ID</span>
              <input className="table-input" value={failedForm.claim_id} onChange={(event) => setFailedForm({ ...failedForm, claim_id: event.target.value })} />
            </label>
            <label className="field">
              <span>Range start</span>
              <input className="table-input" required inputMode="numeric" value={failedForm.range_start} onChange={(event) => setFailedForm({ ...failedForm, range_start: event.target.value })} />
            </label>
            <label className="field">
              <span>Range end</span>
              <input className="table-input" required inputMode="numeric" value={failedForm.range_end} onChange={(event) => setFailedForm({ ...failedForm, range_end: event.target.value })} />
            </label>
            <label className="field">
              <span>Reason</span>
              <input className="table-input" value={failedForm.reason} onChange={(event) => setFailedForm({ ...failedForm, reason: event.target.value })} />
            </label>
          </div>
          <button className="primary-button align-end" type="submit">Sign failed</button>
        </form>
      </div>

      <SectionTable title="Node capabilities" count={nodes.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Node</th>
              <th>Status</th>
              <th>Capabilities</th>
              <th>Modules</th>
              <th>Updated</th>
            </tr>
          </thead>
          <tbody>
            {nodes.map((node) => (
              <tr key={node.node_id}>
                <td className="mono-cell breakable-value" title={node.node_id}>
                  {label(node.alias, shortID(node.node_id))}
                  <div className="muted-copy">{shortID(node.node_id)}</div>
                </td>
                <td><span className="status-pill status-pill--table">{node.status}</span></td>
                <td>{capabilityKeys(node.capabilities).map((key) => <span className="status-pill status-pill--table" key={key}>{key}</span>)}</td>
                <td>{Object.entries(node.module_status ?? {}).map(([key, value]) => <div key={key}>{key}: {String(value)}</div>)}</td>
                <td>{formatDateTime(node.updated_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Peer diagnostics" count={peerDiagnostics.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Peer</th>
              <th>Node</th>
              <th>Status</th>
              <th>Cursor</th>
              <th>Failures</th>
              <th>Last sync</th>
              <th>Updated</th>
            </tr>
          </thead>
          <tbody>
            {peerDiagnostics.map((item) => (
              <tr key={item.id}>
                <td className="breakable-value">{item.peer_url}</td>
                <td className="mono-cell breakable-value" title={item.node_id}>{shortID(item.node_id)}</td>
                <td><span className="status-pill status-pill--table">{item.enabled ? item.status : 'disabled'}</span></td>
                <td className="mono-cell breakable-value" title={item.last_event_id}>{shortID(item.last_event_id || item.cursor)}</td>
                <td>
                  {formatNumber(item.failure_count)}
                  {item.last_error ? <div className="muted-copy breakable-value">{item.last_error}</div> : null}
                </td>
                <td>{formatDateTime(item.last_sync_at)}</td>
                <td>{formatDateTime(item.updated_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Event diagnostics" count={eventDiagnostics.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Event</th>
              <th>Type</th>
              <th>Author</th>
              <th>Pools</th>
              <th>Status</th>
              <th>Projected</th>
              <th>Received</th>
            </tr>
          </thead>
          <tbody>
            {eventDiagnostics.map((item) => (
              <tr key={item.event_id}>
                <td className="mono-cell breakable-value" title={item.event_id}>{shortID(item.event_id)}</td>
                <td><span className="status-pill status-pill--table">{item.event_type}</span></td>
                <td className="mono-cell breakable-value" title={item.author_node_id}>{shortID(item.author_node_id)}</td>
                <td className="breakable-value">{(item.pool_ids ?? []).join(', ') || 'local'}</td>
                <td>
                  <span className="status-pill status-pill--table">{item.validation_status}</span>
                  {item.rejection_reason ? <div className="muted-copy breakable-value">{item.rejection_reason}</div> : null}
                </td>
                <td>{item.projected ? formatDateTime(item.projected_at) : 'no'}</td>
                <td>{formatDateTime(item.received_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Rejected event diagnostics" count={rejectedDiagnostics.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Event</th>
              <th>Type</th>
              <th>Author</th>
              <th>Reason</th>
              <th>Received</th>
            </tr>
          </thead>
          <tbody>
            {rejectedDiagnostics.map((item) => (
              <tr key={item.id}>
                <td className="mono-cell breakable-value" title={item.event_id}>{shortID(item.event_id)}</td>
                <td>{label(item.event_type)}</td>
                <td className="mono-cell breakable-value" title={item.author_node_id}>{shortID(item.author_node_id)}</td>
                <td className="breakable-value">{item.rejection_reason}</td>
                <td>{formatDateTime(item.received_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Peer delivery diagnostics" count={deliveryDiagnostics.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Peer</th>
              <th>Event</th>
              <th>Type</th>
              <th>Status</th>
              <th>Attempts</th>
              <th>Delivered</th>
              <th>Updated</th>
            </tr>
          </thead>
          <tbody>
            {deliveryDiagnostics.map((item) => (
              <tr key={`${item.peer_id}-${item.event_id}`}>
                <td className="breakable-value">{item.peer_url}</td>
                <td className="mono-cell breakable-value" title={item.event_id}>{shortID(item.event_id)}</td>
                <td>{label(item.event_type)}</td>
                <td>
                  <span className="status-pill status-pill--table">{item.status}</span>
                  {item.last_error ? <div className="muted-copy breakable-value">{item.last_error}</div> : null}
                </td>
                <td>{formatNumber(item.attempts)}</td>
                <td>{formatDateTime(item.delivered_at)}</td>
                <td>{formatDateTime(item.updated_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Validation task diagnostics" count={validationTaskDiagnostics.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Task</th>
              <th>Manifest</th>
              <th>Release</th>
              <th>Pool</th>
              <th>Status</th>
              <th>Attempts</th>
              <th>Due</th>
              <th>Updated</th>
            </tr>
          </thead>
          <tbody>
            {validationTaskDiagnostics.map((item) => (
              <tr key={item.task_id}>
                <td>{formatNumber(item.task_id)}</td>
                <td className="mono-cell breakable-value" title={item.manifest_id}>{shortID(item.manifest_id)}</td>
                <td className="mono-cell breakable-value" title={item.release_id}>{shortID(item.release_id)}</td>
                <td>{item.pool_id}</td>
                <td>
                  <span className="status-pill status-pill--table">{item.status}</span>
                  {item.last_error ? <div className="muted-copy breakable-value">{item.last_error}</div> : null}
                </td>
                <td>{formatNumber(item.attempts)}</td>
                <td>{formatDateTime(item.due_at)}</td>
                <td>{formatDateTime(item.updated_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Suggestions" count={suggestions.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Assignment</th>
              <th>Group</th>
              <th>Node</th>
              <th>Range</th>
              <th>Reason</th>
            </tr>
          </thead>
          <tbody>
            {suggestions.map((item) => (
              <tr key={`${item.assignment.assignment_id}-${item.reason}`}>
                <td className="mono-cell breakable-value" title={item.assignment.assignment_id}>{shortID(item.assignment.assignment_id)}</td>
                <td className="breakable-value">{item.assignment.group}</td>
                <td className="mono-cell breakable-value" title={item.assignment.assigned_node_id}>{shortID(item.assignment.assigned_node_id)}</td>
                <td>{rangeLabel(item.assignment)}</td>
                <td>{item.reason}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title={`Plan: ${label(plan?.mode, mode)}`} count={(plan?.suggestions ?? []).length}>
        <AssignmentRows rows={(plan?.suggestions ?? []).map((item) => item.assignment)} />
      </SectionTable>

      <SectionTable title="Assignments" count={assignments.length}>
        <AssignmentRows rows={assignments} />
      </SectionTable>

      <SectionTable title="Claims" count={claims.length}>
        <ClaimRows rows={claims} />
      </SectionTable>

      <SectionTable title="Stale claims" count={staleClaims.length}>
        <div className="button-row">
          <button className="secondary-button" type="button" onClick={() => void handleStalePenalties()}>
            Materialize stale penalties
          </button>
        </div>
        <ClaimRows rows={staleClaims} />
      </SectionTable>

      <SectionTable title="Outcomes" count={outcomes.length}>
        <OutcomeRows rows={outcomes} />
      </SectionTable>

      <SectionTable title="Group catalog" count={groups.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Group</th>
              <th>Range</th>
              <th>Retention</th>
              <th>Confidence</th>
              <th>Author</th>
              <th>Observed</th>
            </tr>
          </thead>
          <tbody>
            {groups.map((item) => (
              <tr key={`${item.pool_id}-${item.group}`}>
                <td className="breakable-value">{item.group}</td>
                <td>{formatNumber(item.low_watermark)} - {formatNumber(item.high_watermark)}</td>
                <td>{formatNumber(item.retention_days)}d</td>
                <td>{Math.round(item.confidence * 100)}%</td>
                <td className="mono-cell breakable-value" title={item.author_node_id}>{shortID(item.author_node_id)}</td>
                <td>{formatDateTime(item.observed_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Validation gaps" count={validationGaps.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Release</th>
              <th>Manifest</th>
              <th>Source</th>
              <th>Attestations</th>
              <th>Last task</th>
            </tr>
          </thead>
          <tbody>
            {validationGaps.map((item) => (
              <tr key={`${item.release_id}-${item.manifest_id}`}>
                <td className="mono-cell breakable-value" title={item.release_id}>{shortID(item.release_id)}</td>
                <td className="mono-cell breakable-value" title={item.manifest_id}>{shortID(item.manifest_id)}</td>
                <td className="mono-cell breakable-value" title={item.source_node_id}>{shortID(item.source_node_id)}</td>
                <td>{formatNumber(item.validation_attestation_count)}</td>
                <td>{formatDateTime(item.last_validation_task_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Duplicate ranges" count={duplicates.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Group</th>
              <th>Range</th>
              <th>Claims</th>
              <th>Nodes</th>
            </tr>
          </thead>
          <tbody>
            {duplicates.map((item) => (
              <tr key={`${item.pool_id}-${item.group}-${item.range_start}-${item.range_end}`}>
                <td className="breakable-value">{item.group}</td>
                <td>{rangeLabel(item)}</td>
                <td>{formatNumber(item.claim_count)}</td>
                <td className="mono-cell breakable-value">{item.node_ids.map(shortID).join(', ')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>
    </div>
  )
}
