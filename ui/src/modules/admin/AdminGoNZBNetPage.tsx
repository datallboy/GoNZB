import { useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import {
  approveGoNZBNetPoolMember,
  createGoNZBNetPoolMemberRevocation,
  createGoNZBNetCoverageAssignment,
  createGoNZBNetCoverageClaim,
  createGoNZBNetCoverageComplete,
  createGoNZBNetCoverageFailed,
  createGoNZBNetTombstone,
  deleteGoNZBNetPeer,
  deleteGoNZBNetRolePoolAccess,
  exportGoNZBNetKey,
  getGoNZBNetConfigValidation,
  getGoNZBNetCoverageDashboard,
  getGoNZBNetCoverageGroups,
  getGoNZBNetCoveragePlan,
  getGoNZBNetCoverageSuggestions,
  getGoNZBNetEventDiagnostics,
  getGoNZBNetHealthDiagnostics,
  getGoNZBNetManifestSourceDiagnostics,
  getGoNZBNetNodeCapabilities,
  getGoNZBNetNodeProfile,
  getGoNZBNetPoolControlEvents,
  getGoNZBNetPoolMembers,
  getGoNZBNetPeerDeliveryDiagnostics,
  getGoNZBNetPeerDiagnostics,
  getGoNZBNetRejectedEventDiagnostics,
  getGoNZBNetReleaseSourceDiagnostics,
  getGoNZBNetReputationDiagnostics,
  getGoNZBNetRolePoolAccess,
  getGoNZBNetTombstones,
  getGoNZBNetTrustPools,
  getGoNZBNetValidationTaskDiagnostics,
  getGoNZBNetValidationGaps,
  materializeGoNZBNetStalePenalties,
  recomputeGoNZBNetScores,
  requestGoNZBNetPoolJoin,
  revokeGoNZBNetPoolMember,
  resolveGoNZBNetManifest,
  rotateGoNZBNetKey,
  runGoNZBNetGossipSync,
  runGoNZBNetPullSync,
  runGoNZBNetPushSync,
  setGoNZBNetNodeBlocked,
  setGoNZBNetPeerEnabled,
  upsertGoNZBNetPoolMember,
  upsertGoNZBNetPeer,
  upsertGoNZBNetRolePoolAccess,
  upsertGoNZBNetTrustPool,
} from '../../shared/api/admin'
import { formatDateTime, formatNumber } from '../../shared/lib/format'
import type {
  GoNZBNetCoverageAssignment,
  GoNZBNetCoverageClaim,
  GoNZBNetConfigValidation,
  GoNZBNetCoverageDashboard,
  GoNZBNetCoverageOutcome,
  GoNZBNetCoveragePlan,
  GoNZBNetCoverageSuggestion,
  GoNZBNetEventDiagnostic,
  GoNZBNetGroupCatalogItem,
  GoNZBNetHealthAttestationDiagnostic,
  GoNZBNetManifestSourceDiagnostic,
  GoNZBNetNodeCapability,
  GoNZBNetNodeProfileResponse,
  GoNZBNetPeerDeliveryDiagnostic,
  GoNZBNetPeerDiagnostic,
  GoNZBNetPoolControlEvent,
  GoNZBNetPoolMember,
  GoNZBNetRejectedEventDiagnostic,
  GoNZBNetReleaseSourceDiagnostic,
  GoNZBNetReputationDiagnostic,
  GoNZBNetRolePoolAccess,
  GoNZBNetTombstone,
  GoNZBNetTrustPool,
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

type PoolForm = {
  pool_id: string
  display_name: string
  description: string
  membership_threshold: string
  moderation_threshold: string
  checkpoint_witness_threshold: string
  accept_mode: string
  min_node_trust_score: string
  accepted_event_types: string
  enabled: boolean
}

type MemberForm = {
  node_id: string
  role: string
  status: string
  allowed_capabilities: string
}

type RolePoolAccessForm = {
  role_id: string
  can_search: boolean
  can_get: boolean
  can_resolve_manifest: boolean
}

type PoolJoinForm = {
  requested_roles: string
  message: string
}

type MemberApprovalForm = {
  node_id: string
  role: string
  proposal_event_id: string
  approvals_required: string
}

type MemberRevocationForm = {
  node_id: string
  reason: string
  effective_at: string
  approvals_required: string
}

type TombstoneForm = {
  target_type: string
  target_id: string
  pool_id: string
  severity: string
  reason: string
  evidence_event_ids: string
  effective_at: string
  expires_at: string
}

type ManifestResolveForm = {
  release_id: string
}

type KeyExportForm = {
  backup_password: string
  confirmation: string
}

type KeyRotateForm = {
  confirmation: string
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

const defaultPoolForm: PoolForm = {
  pool_id: defaultPoolID,
  display_name: 'Local Pool',
  description: '',
  membership_threshold: '1',
  moderation_threshold: '1',
  checkpoint_witness_threshold: '1',
  accept_mode: 'pool_member',
  min_node_trust_score: '0',
  accepted_event_types: '',
  enabled: true,
}

const defaultMemberForm: MemberForm = {
  node_id: '',
  role: 'member',
  status: 'active',
  allowed_capabilities: 'consumer',
}

const defaultRolePoolAccessForm: RolePoolAccessForm = {
  role_id: '',
  can_search: true,
  can_get: true,
  can_resolve_manifest: false,
}

const defaultPoolJoinForm: PoolJoinForm = {
  requested_roles: 'member',
  message: '',
}

const defaultMemberApprovalForm: MemberApprovalForm = {
  node_id: '',
  role: 'member',
  proposal_event_id: '',
  approvals_required: '',
}

const defaultMemberRevocationForm: MemberRevocationForm = {
  node_id: '',
  reason: '',
  effective_at: '',
  approvals_required: '',
}

const defaultTombstoneForm: TombstoneForm = {
  target_type: 'release',
  target_id: '',
  pool_id: '',
  severity: 'local_only',
  reason: '',
  evidence_event_ids: '',
  effective_at: '',
  expires_at: '',
}

const defaultManifestResolveForm: ManifestResolveForm = {
  release_id: '',
}

const defaultKeyExportForm: KeyExportForm = {
  backup_password: '',
  confirmation: '',
}

const defaultKeyRotateForm: KeyRotateForm = {
  confirmation: '',
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

function csvList(value: string) {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
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

function poolControlSubject(item: GoNZBNetPoolControlEvent) {
  const body = item.body_json ?? {}
  const subject = typeof body.subject_node_id === 'string'
    ? body.subject_node_id
    : typeof body.candidate_node_id === 'string'
      ? body.candidate_node_id
      : ''
  return shortID(subject)
}

function poolControlDetail(item: GoNZBNetPoolControlEvent) {
  const body = item.body_json ?? {}
  const parts = [
    typeof body.role === 'string' ? body.role : '',
    typeof body.proposal_event_id === 'string' ? shortID(body.proposal_event_id) : '',
    typeof body.reason === 'string' ? body.reason : '',
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(' / ') : 'n/a'
}

function rangeLabel(item: { range_start?: number; range_end?: number }) {
  if (!item.range_start && !item.range_end) {
    return 'n/a'
  }
  return `${formatNumber(item.range_start ?? 0)} - ${formatNumber(item.range_end ?? 0)}`
}

function scorePercent(value?: number | null) {
  if (value === undefined || value === null || !Number.isFinite(value)) {
    return 'n/a'
  }
  return `${Math.round(value * 100)}%`
}

function mapEntries<T>(value?: Record<string, T> | null) {
  return Object.entries(value ?? {}).sort(([left], [right]) => left.localeCompare(right))
}

function displayConfigValue(value: unknown) {
  if (typeof value === 'boolean') {
    return value ? 'enabled' : 'disabled'
  }
  if (typeof value === 'number') {
    return formatNumber(value)
  }
  if (value === undefined || value === null || value === '') {
    return 'n/a'
  }
  return String(value)
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
  const [nodeProfile, setNodeProfile] = useState<GoNZBNetNodeProfileResponse | null>(null)
  const [configValidation, setConfigValidation] = useState<GoNZBNetConfigValidation | null>(null)
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
  const [releaseSourceDiagnostics, setReleaseSourceDiagnostics] = useState<GoNZBNetReleaseSourceDiagnostic[]>([])
  const [manifestSourceDiagnostics, setManifestSourceDiagnostics] = useState<GoNZBNetManifestSourceDiagnostic[]>([])
  const [healthDiagnostics, setHealthDiagnostics] = useState<GoNZBNetHealthAttestationDiagnostic[]>([])
  const [reputationDiagnostics, setReputationDiagnostics] = useState<GoNZBNetReputationDiagnostic[]>([])
  const [trustPools, setTrustPools] = useState<GoNZBNetTrustPool[]>([])
  const [poolMembers, setPoolMembers] = useState<GoNZBNetPoolMember[]>([])
  const [poolControlEvents, setPoolControlEvents] = useState<GoNZBNetPoolControlEvent[]>([])
  const [rolePoolAccess, setRolePoolAccess] = useState<GoNZBNetRolePoolAccess[]>([])
  const [tombstones, setTombstones] = useState<GoNZBNetTombstone[]>([])
  const [assignmentForm, setAssignmentForm] = useState<AssignmentForm>(defaultAssignmentForm)
  const [claimForm, setClaimForm] = useState<ClaimForm>(defaultClaimForm)
  const [completeForm, setCompleteForm] = useState<OutcomeForm>(defaultOutcomeForm)
  const [failedForm, setFailedForm] = useState<OutcomeForm>(defaultOutcomeForm)
  const [poolForm, setPoolForm] = useState<PoolForm>(defaultPoolForm)
  const [memberForm, setMemberForm] = useState<MemberForm>(defaultMemberForm)
  const [rolePoolAccessForm, setRolePoolAccessForm] = useState<RolePoolAccessForm>(defaultRolePoolAccessForm)
  const [poolJoinForm, setPoolJoinForm] = useState<PoolJoinForm>(defaultPoolJoinForm)
  const [memberApprovalForm, setMemberApprovalForm] = useState<MemberApprovalForm>(defaultMemberApprovalForm)
  const [memberRevocationForm, setMemberRevocationForm] = useState<MemberRevocationForm>(defaultMemberRevocationForm)
  const [tombstoneForm, setTombstoneForm] = useState<TombstoneForm>(defaultTombstoneForm)
  const [manifestResolveForm, setManifestResolveForm] = useState<ManifestResolveForm>(defaultManifestResolveForm)
  const [keyExportForm, setKeyExportForm] = useState<KeyExportForm>(defaultKeyExportForm)
  const [keyRotateForm, setKeyRotateForm] = useState<KeyRotateForm>(defaultKeyRotateForm)
  const [exportedKey, setExportedKey] = useState('')
  const [peerURL, setPeerURL] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [actionStatus, setActionStatus] = useState<string | null>(null)

  const effectivePoolID = poolID.trim() || defaultPoolID
  const suggestedNodeID = useMemo(() => firstNodeID(nodes), [nodes])
  const suggestedReleaseID = useMemo(() => {
    return validationGaps.find((item) => item.release_id)?.release_id ?? releaseSourceDiagnostics.find((item) => item.release_id)?.release_id ?? ''
  }, [validationGaps, releaseSourceDiagnostics])

  async function refresh() {
    setLoading(true)
    try {
      const [
        nextNodes,
        nextNodeProfile,
        nextConfigValidation,
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
        nextTrustPools,
        nextPoolMembers,
        nextPoolControlEvents,
        nextRolePoolAccess,
        nextTombstones,
        nextReleaseSources,
        nextManifestSources,
        nextHealth,
        nextReputation,
      ] =
        await Promise.all([
          getGoNZBNetNodeCapabilities(),
          getGoNZBNetNodeProfile(),
          getGoNZBNetConfigValidation(),
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
          getGoNZBNetTrustPools(),
          getGoNZBNetPoolMembers(effectivePoolID),
          getGoNZBNetPoolControlEvents(effectivePoolID, 100),
          getGoNZBNetRolePoolAccess(effectivePoolID),
          getGoNZBNetTombstones(false).catch(() => ({ items: [], count: 0 })),
          getGoNZBNetReleaseSourceDiagnostics(effectivePoolID, 100),
          getGoNZBNetManifestSourceDiagnostics(effectivePoolID, 100),
          getGoNZBNetHealthDiagnostics(effectivePoolID, 100),
          getGoNZBNetReputationDiagnostics(100),
        ])
      setNodes(nextNodes.items ?? [])
      setNodeProfile(nextNodeProfile)
      setConfigValidation(nextConfigValidation)
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
      setTrustPools(nextTrustPools.items ?? [])
      setPoolMembers(nextPoolMembers.items ?? [])
      setPoolControlEvents(nextPoolControlEvents.items ?? [])
      setRolePoolAccess(nextRolePoolAccess.items ?? [])
      setTombstones(nextTombstones.items ?? [])
      setReleaseSourceDiagnostics(nextReleaseSources.items ?? [])
      setManifestSourceDiagnostics(nextManifestSources.items ?? [])
      setHealthDiagnostics(nextHealth.items ?? [])
      setReputationDiagnostics(nextReputation.items ?? [])
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

  async function handlePool(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await upsertGoNZBNetTrustPool({
        pool_id: poolForm.pool_id.trim() || effectivePoolID,
        display_name: poolForm.display_name.trim(),
        description: poolForm.description.trim() || undefined,
        membership_threshold: optionalNumber(poolForm.membership_threshold),
        moderation_threshold: optionalNumber(poolForm.moderation_threshold),
        checkpoint_witness_threshold: optionalNumber(poolForm.checkpoint_witness_threshold),
        accept_mode: poolForm.accept_mode.trim() || undefined,
        min_node_trust_score: optionalNumber(poolForm.min_node_trust_score),
        accepted_event_types: csvList(poolForm.accepted_event_types),
        enabled: poolForm.enabled,
      })
      setActionStatus(`Pool saved ${response.status}`)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save trust pool')
    }
  }

  async function handlePeer(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await upsertGoNZBNetPeer({ peer_url: peerURL.trim() })
      setActionStatus(`Peer saved ${response.peer_id ?? ''}`)
      setPeerURL('')
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save peer')
    }
  }

  async function handlePeerEnabled(peerID: number, enabled: boolean) {
    try {
      const response = await setGoNZBNetPeerEnabled(peerID, enabled)
      setActionStatus(`Peer ${enabled ? 'enabled' : 'disabled'} ${response.status}`)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update peer')
    }
  }

  async function handlePeerDelete(peerID: number) {
    try {
      const response = await deleteGoNZBNetPeer(peerID)
      setActionStatus(`Peer removed ${response.status}`)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove peer')
    }
  }

  async function handleNodeBlocked(nodeID: string, blocked: boolean) {
    try {
      const response = await setGoNZBNetNodeBlocked(nodeID, blocked)
      setActionStatus(`Node ${blocked ? 'blocked' : 'unblocked'} ${response.status}`)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update node')
    }
  }

  async function handleSync(action: 'pull' | 'push' | 'gossip') {
    try {
      const response = action === 'pull'
        ? await runGoNZBNetPullSync()
        : action === 'push'
          ? await runGoNZBNetPushSync()
          : await runGoNZBNetGossipSync()
      setActionStatus(`${action} sync: ${formatNumber(response.result.accepted)} accepted, ${formatNumber(response.result.rejected)} rejected`)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to run ${action} sync`)
    }
  }

  async function handleRecomputeScores() {
    try {
      const response = await recomputeGoNZBNetScores({ pool_id: effectivePoolID })
      setActionStatus(`Scores recomputed: ${formatNumber(response.result.source_updates)} sources, ${formatNumber(response.result.card_updates)} releases`)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to recompute scores')
    }
  }

  async function handleMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await upsertGoNZBNetPoolMember(effectivePoolID, {
        node_id: memberForm.node_id.trim(),
        role: memberForm.role.trim() || undefined,
        status: memberForm.status.trim() || undefined,
        allowed_capabilities: csvList(memberForm.allowed_capabilities),
      })
      setActionStatus(`Pool member saved ${response.status}`)
      setMemberForm(defaultMemberForm)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save pool member')
    }
  }

  async function handleRolePoolAccess(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await upsertGoNZBNetRolePoolAccess(effectivePoolID, {
        role_id: rolePoolAccessForm.role_id.trim(),
        can_search: rolePoolAccessForm.can_search,
        can_get: rolePoolAccessForm.can_get,
        can_resolve_manifest: rolePoolAccessForm.can_resolve_manifest,
      })
      setActionStatus(`Role pool access saved ${response.status}`)
      setRolePoolAccessForm(defaultRolePoolAccessForm)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save role pool access')
    }
  }

  async function handleRolePoolAccessDelete(roleID: string) {
    try {
      const response = await deleteGoNZBNetRolePoolAccess(effectivePoolID, roleID)
      setActionStatus(`Role pool access removed ${response.status}`)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove role pool access')
    }
  }

  async function handleRevokeMember(nodeID: string) {
    try {
      const response = await revokeGoNZBNetPoolMember(effectivePoolID, nodeID)
      setActionStatus(`Pool member revoked ${response.status}`)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to revoke pool member')
    }
  }

  async function handlePoolJoin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await requestGoNZBNetPoolJoin(effectivePoolID, {
        requested_roles: csvList(poolJoinForm.requested_roles),
        message: poolJoinForm.message.trim() || undefined,
      })
      setActionStatus(`Pool join requested ${shortID(response.event_id)}`)
      setPoolJoinForm(defaultPoolJoinForm)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to request pool join')
    }
  }

  async function handleMemberApproval(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const nodeID = memberApprovalForm.node_id.trim()
      const response = await approveGoNZBNetPoolMember(effectivePoolID, nodeID, {
        role: memberApprovalForm.role.trim() || undefined,
        proposal_event_id: memberApprovalForm.proposal_event_id.trim(),
        approvals_required: optionalNumber(memberApprovalForm.approvals_required),
      })
      setActionStatus(`Pool member approved ${shortID(response.event_id)}`)
      setMemberApprovalForm(defaultMemberApprovalForm)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to approve pool member')
    }
  }

  async function handleMemberRevocation(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const nodeID = memberRevocationForm.node_id.trim()
      const response = await createGoNZBNetPoolMemberRevocation(effectivePoolID, nodeID, {
        reason: memberRevocationForm.reason.trim(),
        effective_at: memberRevocationForm.effective_at.trim() || undefined,
        approvals_required: optionalNumber(memberRevocationForm.approvals_required),
      })
      setActionStatus(`Pool member revoked ${shortID(response.event_id)}`)
      setMemberRevocationForm(defaultMemberRevocationForm)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to sign pool member revocation')
    }
  }

  async function handleTombstone(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await createGoNZBNetTombstone({
        target_type: tombstoneForm.target_type.trim(),
        target_id: tombstoneForm.target_id.trim(),
        pool_id: tombstoneForm.pool_id.trim() || undefined,
        severity: tombstoneForm.severity.trim() || undefined,
        reason: tombstoneForm.reason.trim(),
        evidence_event_ids: csvList(tombstoneForm.evidence_event_ids),
        effective_at: tombstoneForm.effective_at.trim() || undefined,
        expires_at: tombstoneForm.expires_at.trim() || undefined,
      })
      setActionStatus(`Tombstone signed ${shortID(response.event_id)}`)
      setTombstoneForm(defaultTombstoneForm)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create tombstone')
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

  async function handleManifestResolve(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const releaseID = manifestResolveForm.release_id.trim() || suggestedReleaseID
      const response = await resolveGoNZBNetManifest({ release_id: releaseID })
      setActionStatus(`Resolved ${shortID(response.release_id)} (${formatNumber(response.nzb_bytes)} bytes)`)
      setManifestResolveForm(defaultManifestResolveForm)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to resolve manifest')
    }
  }

  async function handleKeyExport(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await exportGoNZBNetKey({
        backup_password: keyExportForm.backup_password,
        confirmation: keyExportForm.confirmation.trim(),
      })
      setExportedKey(JSON.stringify(response, null, 2))
      setKeyExportForm(defaultKeyExportForm)
      setActionStatus(`Node key backup exported for ${shortID(response.node_id)}`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to export node key backup')
    }
  }

  async function handleKeyRotate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await rotateGoNZBNetKey({
        confirmation: keyRotateForm.confirmation.trim(),
      })
      setKeyRotateForm(defaultKeyRotateForm)
      setActionStatus(`Rotated node key ${shortID(response.old_node_id)} -> ${shortID(response.new_node_id)}`)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to rotate node key')
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
          <button className="secondary-button" type="button" onClick={() => void handleRecomputeScores()}>
            Recompute scores
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
          <StatCard label="Trust pools" value={formatNumber(trustPools.length)} detail={`${formatNumber(poolMembers.length)} selected-pool members`} />
          <StatCard label="Tombstones" value={formatNumber(tombstones.length)} detail={`${formatNumber(tombstones.filter((item) => item.active).length)} active`} />
          <StatCard label="Release sources" value={formatNumber(releaseSourceDiagnostics.length)} detail={`${formatNumber(manifestSourceDiagnostics.length)} manifest sources`} />
          <StatCard label="Health" value={formatNumber(healthDiagnostics.length)} detail={`${formatNumber(reputationDiagnostics.length)} reputation events`} />
          <StatCard label="Config" value={configValidation?.valid ? 'Valid' : 'Review'} detail={`${formatNumber(configValidation?.issues.length ?? 0)} issues`} />
        </div>
      </div>

      <div className="two-column-grid">
        <SectionTable title="Local node profile">
          <table className="data-table data-table--compact">
            <tbody>
              <tr>
                <th>Node</th>
                <td className="mono-cell breakable-value" title={nodeProfile?.node_id}>{shortID(nodeProfile?.node_id)}</td>
              </tr>
              <tr>
                <th>Public key</th>
                <td className="mono-cell breakable-value" title={nodeProfile?.public_key}>{shortID(nodeProfile?.public_key)}</td>
              </tr>
              <tr>
                <th>Alias</th>
                <td>{label(nodeProfile?.profile.alias)}</td>
              </tr>
              <tr>
                <th>Software</th>
                <td>{label(nodeProfile?.profile.software)} {label(nodeProfile?.profile.software_version, '')}</td>
              </tr>
              <tr>
                <th>Base endpoint</th>
                <td className="breakable-value">{label(nodeProfile?.profile.endpoints?.base)}</td>
              </tr>
              <tr>
                <th>Protocols</th>
                <td className="breakable-value">{(nodeProfile?.profile.protocols ?? []).join(', ') || 'n/a'}</td>
              </tr>
            </tbody>
          </table>
        </SectionTable>

        <SectionTable title="Configuration validation" count={configValidation?.issues.length ?? 0}>
          <table className="data-table data-table--compact">
            <thead>
              <tr>
                <th>Severity</th>
                <th>Field</th>
                <th>Message</th>
              </tr>
            </thead>
            <tbody>
              {(configValidation?.issues ?? []).map((item, index) => (
                <tr key={`${item.field}-${index}`}>
                  <td><span className="status-pill status-pill--table">{item.severity}</span></td>
                  <td className="mono-cell breakable-value">{item.field}</td>
                  <td className="breakable-value">{item.message}</td>
                </tr>
              ))}
              {configValidation && configValidation.issues.length === 0 ? (
                <tr>
                  <td><span className="status-pill status-pill--table">ok</span></td>
                  <td className="mono-cell">gonzbnet</td>
                  <td>No configuration issues detected.</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </SectionTable>
      </div>

      <SectionTable title="Module configuration">
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Module</th>
              <th>Status</th>
              <th>Limits</th>
              <th>Privacy</th>
              <th>Network</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td className="breakable-value">
                {mapEntries(configValidation?.summary.module_enabled).map(([key, enabled]) => (
                  <span className="status-pill status-pill--table" key={key}>{key}: {enabled ? 'on' : 'off'}</span>
                ))}
              </td>
              <td>
                <span className="status-pill status-pill--table">{configValidation?.summary.http_enabled ? 'http' : 'http off'}</span>
                <div className="muted-copy">mode {label(configValidation?.summary.mode)}</div>
              </td>
              <td>
                {mapEntries(configValidation?.summary.limits).map(([key, value]) => (
                  <div key={key}>{key}: {formatNumber(value)}</div>
                ))}
              </td>
              <td>
                {mapEntries(configValidation?.summary.privacy).map(([key, value]) => (
                  <div key={key}>{key}: {value ? 'true' : 'false'}</div>
                ))}
              </td>
              <td className="breakable-value">
                pool {label(configValidation?.summary.local_pool_id)}
                <div className="muted-copy">network {label(configValidation?.summary.network_id)}</div>
                <div className="muted-copy">peers {formatNumber(configValidation?.summary.manual_peers ?? 0)}</div>
                <div className="muted-copy">path {label(configValidation?.summary.http_base_path)}</div>
              </td>
            </tr>
          </tbody>
        </table>
      </SectionTable>

      <div className="two-column-grid">
        <SectionTable title="Publisher and validation">
          <table className="data-table data-table--compact">
            <tbody>
              {mapEntries(configValidation?.summary.publisher).map(([key, value]) => (
                <tr key={key}>
                  <th className="mono-cell breakable-value">{key}</th>
                  <td>{displayConfigValue(value)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </SectionTable>

        <SectionTable title="Sync and gossip">
          <table className="data-table data-table--compact">
            <tbody>
              {mapEntries({ ...(configValidation?.summary.sync ?? {}), ...(configValidation?.summary.gossip ?? {}) }).map(([key, value]) => (
                <tr key={key}>
                  <th className="mono-cell breakable-value">{key}</th>
                  <td>{displayConfigValue(value)}</td>
                </tr>
              ))}
              <tr>
                <th className="mono-cell breakable-value">redacted</th>
                <td className="breakable-value">{(configValidation?.summary.redacted_sensitive_config_names ?? []).join(', ') || 'n/a'}</td>
              </tr>
            </tbody>
          </table>
        </SectionTable>
      </div>

      <div className="two-column-grid">
        <form className="page-card stack" onSubmit={handleKeyExport}>
          <h2 className="section-title">Key backup</h2>
          <div className="toolbar-grid">
            <label className="field">
              <span>Backup password</span>
              <input className="table-input" required type="password" value={keyExportForm.backup_password} onChange={(event) => setKeyExportForm({ ...keyExportForm, backup_password: event.target.value })} />
            </label>
            <label className="field">
              <span>Confirmation</span>
              <input className="table-input" required value={keyExportForm.confirmation} onChange={(event) => setKeyExportForm({ ...keyExportForm, confirmation: event.target.value })} />
            </label>
          </div>
          <button className="primary-button align-end" type="submit">Export encrypted backup</button>
          {exportedKey ? (
            <textarea className="table-input mono-cell" readOnly rows={8} value={exportedKey} />
          ) : null}
        </form>

        <form className="page-card stack" onSubmit={handleKeyRotate}>
          <h2 className="section-title">Key rotation</h2>
          <label className="field">
            <span>Confirmation</span>
            <input className="table-input" required value={keyRotateForm.confirmation} onChange={(event) => setKeyRotateForm({ confirmation: event.target.value })} />
          </label>
          <button className="secondary-button align-end" type="submit">Rotate node key</button>
        </form>
      </div>

      <div className="two-column-grid">
        <form className="page-card stack" onSubmit={handlePool}>
          <h2 className="section-title">Trust pool</h2>
          <div className="toolbar-grid">
            <label className="field">
              <span>Pool ID</span>
              <input className="table-input" required value={poolForm.pool_id} onChange={(event) => setPoolForm({ ...poolForm, pool_id: event.target.value })} />
            </label>
            <label className="field">
              <span>Name</span>
              <input className="table-input" required value={poolForm.display_name} onChange={(event) => setPoolForm({ ...poolForm, display_name: event.target.value })} />
            </label>
            <label className="field">
              <span>Description</span>
              <input className="table-input" value={poolForm.description} onChange={(event) => setPoolForm({ ...poolForm, description: event.target.value })} />
            </label>
            <label className="field">
              <span>Accept mode</span>
              <select className="table-input" value={poolForm.accept_mode} onChange={(event) => setPoolForm({ ...poolForm, accept_mode: event.target.value })}>
                <option value="pool_member">pool_member</option>
                <option value="known_node">known_node</option>
              </select>
            </label>
            <label className="field">
              <span>Membership</span>
              <input className="table-input" inputMode="numeric" value={poolForm.membership_threshold} onChange={(event) => setPoolForm({ ...poolForm, membership_threshold: event.target.value })} />
            </label>
            <label className="field">
              <span>Moderation</span>
              <input className="table-input" inputMode="numeric" value={poolForm.moderation_threshold} onChange={(event) => setPoolForm({ ...poolForm, moderation_threshold: event.target.value })} />
            </label>
            <label className="field">
              <span>Witnesses</span>
              <input className="table-input" inputMode="numeric" value={poolForm.checkpoint_witness_threshold} onChange={(event) => setPoolForm({ ...poolForm, checkpoint_witness_threshold: event.target.value })} />
            </label>
            <label className="field">
              <span>Min trust</span>
              <input className="table-input" inputMode="decimal" value={poolForm.min_node_trust_score} onChange={(event) => setPoolForm({ ...poolForm, min_node_trust_score: event.target.value })} />
            </label>
            <label className="field">
              <span>Event types</span>
              <input className="table-input" value={poolForm.accepted_event_types} onChange={(event) => setPoolForm({ ...poolForm, accepted_event_types: event.target.value })} />
            </label>
            <label className="checkbox-inline align-end">
              <input type="checkbox" checked={poolForm.enabled} onChange={(event) => setPoolForm({ ...poolForm, enabled: event.target.checked })} />
              <span>Enabled</span>
            </label>
          </div>
          <button className="primary-button align-end" type="submit">Save pool</button>
        </form>

        <form className="page-card stack" onSubmit={handleMember}>
          <h2 className="section-title">Pool member</h2>
          <div className="toolbar-grid">
            <label className="field">
              <span>Node ID</span>
              <input className="table-input" required value={memberForm.node_id} onChange={(event) => setMemberForm({ ...memberForm, node_id: event.target.value })} />
            </label>
            <label className="field">
              <span>Role</span>
              <select className="table-input" value={memberForm.role} onChange={(event) => setMemberForm({ ...memberForm, role: event.target.value })}>
                <option value="member">member</option>
                <option value="admin">admin</option>
                <option value="witness">witness</option>
              </select>
            </label>
            <label className="field">
              <span>Status</span>
              <select className="table-input" value={memberForm.status} onChange={(event) => setMemberForm({ ...memberForm, status: event.target.value })}>
                <option value="active">active</option>
                <option value="pending">pending</option>
                <option value="revoked">revoked</option>
              </select>
            </label>
            <label className="field">
              <span>Capabilities</span>
              <input className="table-input" value={memberForm.allowed_capabilities} onChange={(event) => setMemberForm({ ...memberForm, allowed_capabilities: event.target.value })} />
            </label>
          </div>
          <button className="primary-button align-end" type="submit">Save member</button>
        </form>

        <form className="page-card stack" onSubmit={handlePoolJoin}>
          <h2 className="section-title">Request pool join</h2>
          <div className="toolbar-grid">
            <label className="field">
              <span>Pool ID</span>
              <input className="table-input" readOnly value={effectivePoolID} />
            </label>
            <label className="field">
              <span>Roles</span>
              <input className="table-input" value={poolJoinForm.requested_roles} onChange={(event) => setPoolJoinForm({ ...poolJoinForm, requested_roles: event.target.value })} />
            </label>
            <label className="field">
              <span>Message</span>
              <input className="table-input" value={poolJoinForm.message} onChange={(event) => setPoolJoinForm({ ...poolJoinForm, message: event.target.value })} />
            </label>
          </div>
          <button className="primary-button align-end" type="submit">Request join</button>
        </form>

        <form className="page-card stack" onSubmit={handleMemberApproval}>
          <h2 className="section-title">Approve member</h2>
          <div className="toolbar-grid">
            <label className="field">
              <span>Node ID</span>
              <input className="table-input" required value={memberApprovalForm.node_id} onChange={(event) => setMemberApprovalForm({ ...memberApprovalForm, node_id: event.target.value })} />
            </label>
            <label className="field">
              <span>Role</span>
              <select className="table-input" value={memberApprovalForm.role} onChange={(event) => setMemberApprovalForm({ ...memberApprovalForm, role: event.target.value })}>
                <option value="member">member</option>
                <option value="admin">admin</option>
                <option value="witness">witness</option>
              </select>
            </label>
            <label className="field">
              <span>Join event</span>
              <input className="table-input" required value={memberApprovalForm.proposal_event_id} onChange={(event) => setMemberApprovalForm({ ...memberApprovalForm, proposal_event_id: event.target.value })} />
            </label>
            <label className="field">
              <span>Required</span>
              <input className="table-input" inputMode="numeric" value={memberApprovalForm.approvals_required} onChange={(event) => setMemberApprovalForm({ ...memberApprovalForm, approvals_required: event.target.value })} />
            </label>
          </div>
          <button className="primary-button align-end" type="submit">Sign approval</button>
        </form>

        <form className="page-card stack" onSubmit={handleMemberRevocation}>
          <h2 className="section-title">Revoke member</h2>
          <div className="toolbar-grid">
            <label className="field">
              <span>Node ID</span>
              <input className="table-input" required value={memberRevocationForm.node_id} onChange={(event) => setMemberRevocationForm({ ...memberRevocationForm, node_id: event.target.value })} />
            </label>
            <label className="field">
              <span>Reason</span>
              <input className="table-input" required value={memberRevocationForm.reason} onChange={(event) => setMemberRevocationForm({ ...memberRevocationForm, reason: event.target.value })} />
            </label>
            <label className="field">
              <span>Effective at</span>
              <input className="table-input" value={memberRevocationForm.effective_at} onChange={(event) => setMemberRevocationForm({ ...memberRevocationForm, effective_at: event.target.value })} />
            </label>
            <label className="field">
              <span>Required</span>
              <input className="table-input" inputMode="numeric" value={memberRevocationForm.approvals_required} onChange={(event) => setMemberRevocationForm({ ...memberRevocationForm, approvals_required: event.target.value })} />
            </label>
          </div>
          <button className="secondary-button align-end" type="submit">Sign revocation</button>
        </form>

        <form className="page-card stack" onSubmit={handleRolePoolAccess}>
          <h2 className="section-title">Role pool access</h2>
          <div className="toolbar-grid">
            <label className="field">
              <span>Role ID</span>
              <input className="table-input" required value={rolePoolAccessForm.role_id} onChange={(event) => setRolePoolAccessForm({ ...rolePoolAccessForm, role_id: event.target.value })} />
            </label>
            <label className="checkbox-inline align-end">
              <input type="checkbox" checked={rolePoolAccessForm.can_search} onChange={(event) => setRolePoolAccessForm({ ...rolePoolAccessForm, can_search: event.target.checked })} />
              <span>Search</span>
            </label>
            <label className="checkbox-inline align-end">
              <input type="checkbox" checked={rolePoolAccessForm.can_get} onChange={(event) => setRolePoolAccessForm({ ...rolePoolAccessForm, can_get: event.target.checked })} />
              <span>Get</span>
            </label>
            <label className="checkbox-inline align-end">
              <input type="checkbox" checked={rolePoolAccessForm.can_resolve_manifest} onChange={(event) => setRolePoolAccessForm({ ...rolePoolAccessForm, can_resolve_manifest: event.target.checked })} />
              <span>Resolve</span>
            </label>
          </div>
          <button className="primary-button align-end" type="submit">Save role access</button>
        </form>
      </div>

      <form className="page-card stack" onSubmit={handleTombstone}>
        <h2 className="section-title">Tombstone</h2>
        <div className="toolbar-grid">
          <label className="field">
            <span>Target type</span>
            <select className="table-input" value={tombstoneForm.target_type} onChange={(event) => setTombstoneForm({ ...tombstoneForm, target_type: event.target.value })}>
              <option value="release">release</option>
              <option value="manifest">manifest</option>
              <option value="node">node</option>
              <option value="event">event</option>
            </select>
          </label>
          <label className="field">
            <span>Target ID</span>
            <input className="table-input" required value={tombstoneForm.target_id} onChange={(event) => setTombstoneForm({ ...tombstoneForm, target_id: event.target.value })} />
          </label>
          <label className="field">
            <span>Pool</span>
            <input className="table-input" value={tombstoneForm.pool_id} onChange={(event) => setTombstoneForm({ ...tombstoneForm, pool_id: event.target.value })} />
          </label>
          <label className="field">
            <span>Severity</span>
            <select className="table-input" value={tombstoneForm.severity} onChange={(event) => setTombstoneForm({ ...tombstoneForm, severity: event.target.value })}>
              <option value="local_only">local_only</option>
              <option value="reject">reject</option>
              <option value="hide">hide</option>
            </select>
          </label>
          <label className="field">
            <span>Reason</span>
            <input className="table-input" required value={tombstoneForm.reason} onChange={(event) => setTombstoneForm({ ...tombstoneForm, reason: event.target.value })} />
          </label>
          <label className="field">
            <span>Evidence</span>
            <input className="table-input" value={tombstoneForm.evidence_event_ids} onChange={(event) => setTombstoneForm({ ...tombstoneForm, evidence_event_ids: event.target.value })} />
          </label>
          <label className="field">
            <span>Effective at</span>
            <input className="table-input" value={tombstoneForm.effective_at} onChange={(event) => setTombstoneForm({ ...tombstoneForm, effective_at: event.target.value })} />
          </label>
          <label className="field">
            <span>Expires at</span>
            <input className="table-input" value={tombstoneForm.expires_at} onChange={(event) => setTombstoneForm({ ...tombstoneForm, expires_at: event.target.value })} />
          </label>
        </div>
        <button className="primary-button align-end" type="submit">Sign tombstone</button>
      </form>

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
              <th>Action</th>
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
                <td>
                  {node.status === 'local' ? (
                    <span className="muted-copy">local</span>
                  ) : (
                    <button className="secondary-button secondary-button--small" type="button" onClick={() => void handleNodeBlocked(node.node_id, node.status !== 'blocked')}>
                      {node.status === 'blocked' ? 'Unblock' : 'Block'}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Trust pools" count={trustPools.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Pool</th>
              <th>Policy</th>
              <th>Events</th>
              <th>Status</th>
              <th>Updated</th>
            </tr>
          </thead>
          <tbody>
            {trustPools.map((item) => (
              <tr key={item.pool_id}>
                <td className="breakable-value">
                  {item.display_name}
                  <div className="muted-copy mono-cell">{item.pool_id}</div>
                </td>
                <td>
                  m{formatNumber(item.membership_threshold)} / mod{formatNumber(item.moderation_threshold)}
                  <div className="muted-copy">trust {item.min_node_trust_score}</div>
                </td>
                <td className="breakable-value">{(item.accepted_event_types ?? []).join(', ')}</td>
                <td><span className="status-pill status-pill--table">{item.enabled ? item.accept_mode : 'disabled'}</span></td>
                <td>{formatDateTime(item.updated_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title={`Members: ${effectivePoolID}`} count={poolMembers.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Node</th>
              <th>Role</th>
              <th>Status</th>
              <th>Capabilities</th>
              <th>Joined</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {poolMembers.map((item) => (
              <tr key={`${item.pool_id}-${item.node_id}`}>
                <td className="mono-cell breakable-value" title={item.node_id}>{shortID(item.node_id)}</td>
                <td><span className="status-pill status-pill--table">{item.role}</span></td>
                <td><span className="status-pill status-pill--table">{item.status}</span></td>
                <td className="breakable-value">{(item.allowed_capabilities ?? []).join(', ')}</td>
                <td>{formatDateTime(item.joined_at)}</td>
                <td>
                  <button className="secondary-button secondary-button--small" type="button" onClick={() => void handleRevokeMember(item.node_id)}>
                    Revoke
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title={`Role access: ${effectivePoolID}`} count={rolePoolAccess.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Role</th>
              <th>Search</th>
              <th>Get</th>
              <th>Resolve</th>
              <th>Updated</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {rolePoolAccess.map((item) => (
              <tr key={`${item.pool_id}-${item.role_id}`}>
                <td className="mono-cell breakable-value">{item.role_id}</td>
                <td>{item.can_search ? 'yes' : 'no'}</td>
                <td>{item.can_get ? 'yes' : 'no'}</td>
                <td>{item.can_resolve_manifest ? 'yes' : 'no'}</td>
                <td>{formatDateTime(item.updated_at)}</td>
                <td>
                  <button className="secondary-button secondary-button--small" type="button" onClick={() => void handleRolePoolAccessDelete(item.role_id)}>
                    Remove
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title={`Pool control events: ${effectivePoolID}`} count={poolControlEvents.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Event</th>
              <th>Type</th>
              <th>Author</th>
              <th>Subject</th>
              <th>Detail</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {poolControlEvents.map((item) => (
              <tr key={item.event_id}>
                <td className="mono-cell breakable-value" title={item.event_id}>{shortID(item.event_id)}</td>
                <td>{item.event_type}</td>
                <td className="mono-cell breakable-value" title={item.author_node_id}>{shortID(item.author_node_id)}</td>
                <td className="mono-cell breakable-value">{poolControlSubject(item)}</td>
                <td className="breakable-value">{poolControlDetail(item)}</td>
                <td>{formatDateTime(item.created_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Tombstones" count={tombstones.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Target</th>
              <th>Pool</th>
              <th>Severity</th>
              <th>Reason</th>
              <th>Approval</th>
              <th>Status</th>
              <th>Updated</th>
            </tr>
          </thead>
          <tbody>
            {tombstones.map((item) => (
              <tr key={item.id}>
                <td className="mono-cell breakable-value" title={item.target_id}>
                  {item.target_type}
                  <div className="muted-copy">{shortID(item.target_id)}</div>
                </td>
                <td>{label(item.pool_id, 'local')}</td>
                <td><span className="status-pill status-pill--table">{item.severity}</span></td>
                <td className="breakable-value">{item.reason}</td>
                <td>{formatNumber(item.approval_count)} / {formatNumber(item.approvals_required)}</td>
                <td><span className="status-pill status-pill--table">{item.active ? 'active' : 'inactive'}</span></td>
                <td>{formatDateTime(item.updated_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <div className="page-card stack">
        <h2 className="section-title">Peer controls</h2>
        <form className="release-table-search" onSubmit={handlePeer}>
          <input className="table-input" required value={peerURL} onChange={(event) => setPeerURL(event.target.value)} />
          <button className="primary-button" type="submit">Add peer</button>
        </form>
        <div className="button-row">
          <button className="secondary-button" type="button" onClick={() => void handleSync('pull')}>Pull sync</button>
          <button className="secondary-button" type="button" onClick={() => void handleSync('push')}>Push sync</button>
          <button className="secondary-button" type="button" onClick={() => void handleSync('gossip')}>Gossip</button>
        </div>
      </div>

      <form className="page-card stack" onSubmit={handleManifestResolve}>
        <h2 className="section-title">Manifest resolve</h2>
        <div className="release-table-search">
          <input
            className="table-input"
            placeholder={suggestedReleaseID || 'release_id'}
            value={manifestResolveForm.release_id}
            onChange={(event) => setManifestResolveForm({ release_id: event.target.value })}
          />
          <button className="primary-button" type="submit" disabled={!manifestResolveForm.release_id.trim() && !suggestedReleaseID}>
            Resolve
          </button>
        </div>
      </form>

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
              <th>Action</th>
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
                <td>
                  <button className="secondary-button secondary-button--small" type="button" onClick={() => void handlePeerEnabled(item.id, !item.enabled)}>
                    {item.enabled ? 'Disable' : 'Enable'}
                  </button>
                  <button className="secondary-button secondary-button--small" type="button" onClick={() => void handlePeerDelete(item.id)}>
                    Remove
                  </button>
                </td>
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

      <SectionTable title="Release source diagnostics" count={releaseSourceDiagnostics.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Release</th>
              <th>Manifest</th>
              <th>Source</th>
              <th>Pool</th>
              <th>Scores</th>
              <th>Status</th>
              <th>Last seen</th>
            </tr>
          </thead>
          <tbody>
            {releaseSourceDiagnostics.map((item) => (
              <tr key={`${item.release_id}-${item.source_node_id}-${item.pool_id}`}>
                <td className="breakable-value" title={item.release_id}>
                  {label(item.title, shortID(item.release_id))}
                  <div className="muted-copy mono-cell">{shortID(item.release_id)}</div>
                </td>
                <td className="mono-cell breakable-value" title={item.manifest_id}>{shortID(item.manifest_id)}</td>
                <td className="mono-cell breakable-value" title={item.source_node_id}>
                  {shortID(item.source_node_id)}
                  <div className="muted-copy" title={item.source_event_id}>{shortID(item.source_event_id)}</div>
                </td>
                <td>{label(item.pool_id, 'local')}</td>
                <td>
                  trust {scorePercent(item.trust_score)}
                  <div className="muted-copy">avail {scorePercent(item.availability_score)}</div>
                  <div className="muted-copy">manifest {scorePercent(item.manifest_confidence_score)}</div>
                </td>
                <td><span className="status-pill status-pill--table">{item.resolvable ? 'resolvable' : 'pending'}</span></td>
                <td>
                  {formatDateTime(item.last_seen_at)}
                  <div className="muted-copy">posted {formatDateTime(item.posted_at)}</div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Manifest source diagnostics" count={manifestSourceDiagnostics.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Manifest</th>
              <th>Release</th>
              <th>Source</th>
              <th>Pool</th>
              <th>Status</th>
              <th>Failures</th>
              <th>Updated</th>
            </tr>
          </thead>
          <tbody>
            {manifestSourceDiagnostics.map((item) => (
              <tr key={`${item.manifest_id}-${item.source_node_id}-${item.pool_id}`}>
                <td className="mono-cell breakable-value" title={item.manifest_id}>{shortID(item.manifest_id)}</td>
                <td className="mono-cell breakable-value" title={item.release_id}>{shortID(item.release_id)}</td>
                <td className="mono-cell breakable-value" title={item.source_node_id}>{shortID(item.source_node_id)}</td>
                <td>{label(item.pool_id, 'local')}</td>
                <td>
                  <span className="status-pill status-pill--table">{item.advertised ? 'advertised' : 'hidden'}</span>
                  <div className="muted-copy">trust {scorePercent(item.trust_score)}</div>
                </td>
                <td>
                  {formatNumber(item.failure_count)}
                  <div className="muted-copy">{formatNumber(item.avg_latency_ms)} ms avg</div>
                  <div className="muted-copy">last failure {formatDateTime(item.last_failure_at)}</div>
                </td>
                <td>
                  {formatDateTime(item.updated_at)}
                  <div className="muted-copy">last success {formatDateTime(item.last_success_at)}</div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Health attestations" count={healthDiagnostics.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Attestation</th>
              <th>Release</th>
              <th>Author</th>
              <th>Pool</th>
              <th>Status</th>
              <th>Articles</th>
              <th>Scores</th>
              <th>Checked</th>
            </tr>
          </thead>
          <tbody>
            {healthDiagnostics.map((item) => (
              <tr key={item.attestation_id}>
                <td className="mono-cell breakable-value" title={item.attestation_id}>
                  {shortID(item.attestation_id)}
                  <div className="muted-copy" title={item.source_event_id}>{shortID(item.source_event_id)}</div>
                </td>
                <td className="mono-cell breakable-value" title={item.release_id}>
                  {shortID(item.release_id)}
                  <div className="muted-copy" title={item.manifest_id}>{shortID(item.manifest_id)}</div>
                </td>
                <td className="mono-cell breakable-value" title={item.author_node_id}>{shortID(item.author_node_id)}</td>
                <td>{label(item.pool_id, 'local')}</td>
                <td>
                  <span className="status-pill status-pill--table">{item.status}</span>
                  <div className="muted-copy">{label(item.method)}</div>
                </td>
                <td>
                  {formatNumber(item.articles_available)} / {formatNumber(item.articles_total)}
                  <div className="muted-copy">{formatNumber(item.missing_articles)} missing</div>
                  <div className="muted-copy">{formatNumber(item.retention_days_observed)}d retention</div>
                </td>
                <td>
                  avail {scorePercent(item.availability_score)}
                  <div className="muted-copy">confidence {scorePercent(item.confidence)}</div>
                  <div className="muted-copy">repair {item.repair_available ? scorePercent(item.repair_confidence) : 'no'}</div>
                </td>
                <td>
                  {formatDateTime(item.checked_at)}
                  <div className="muted-copy">updated {formatDateTime(item.updated_at)}</div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </SectionTable>

      <SectionTable title="Reputation diagnostics" count={reputationDiagnostics.length}>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Node</th>
              <th>Pool</th>
              <th>Delta</th>
              <th>Trust</th>
              <th>Reason</th>
              <th>Event</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {reputationDiagnostics.map((item) => (
              <tr key={item.id}>
                <td className="mono-cell breakable-value" title={item.node_id}>{shortID(item.node_id)}</td>
                <td>{label(item.pool_id, 'local')}</td>
                <td>{item.delta > 0 ? '+' : ''}{item.delta.toFixed(3)}</td>
                <td>{scorePercent(item.local_trust_score)}</td>
                <td className="breakable-value">{item.reason}</td>
                <td className="mono-cell breakable-value" title={item.event_id}>{shortID(item.event_id)}</td>
                <td>{formatDateTime(item.created_at)}</td>
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
