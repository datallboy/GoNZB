import { apiRequest, apiURL } from "./http"
import type {
  IndexerBackfillProgress,
  IndexerDashboardStats,
  IndexerDeferredArticleRangeResponse,
  IndexerGroupProfileResponse,
  IndexerNNTPStats,
  IndexerOverviewStreamSnapshot,
  IndexerRecoveryCapacity,
  IndexerStorageStatus,
  IndexerStageThroughput,
  AdminAttentionListParams,
  AdminAttentionListResponse,
  AdminArticleCohortListResponse,
  AdminReleaseDetailResponse,
  AdminBinaryListParams,
  AdminBinaryListResponse,
  AdminBinaryDetail,
  AdminFileDetail,
  AdminReleaseListResponse,
  AdminReleaseListParams,
  AdminMaintenanceTaskPatch,
  AdminMaintenanceTaskRun,
  AdminMaintenanceTasksResponse,
  AdminStorageAuditReport,
  AdminRunDetailResponse,
  AdminRunListParams,
  AdminRunsResponse,
  AdminScrapeConfigResponse,
  AdminStageConfigPatch,
  AdminStagesResponse,
  GoNZBNetActionResponse,
  GoNZBNetAssignmentRequest,
  GoNZBNetClaimRequest,
  GoNZBNetCoverageDashboard,
  GoNZBNetCoveragePlan,
  GoNZBNetCoverageSuggestion,
  GoNZBNetCoverageSuggestionParams,
  GoNZBNetConfigValidation,
  GoNZBNetEventDiagnostic,
  GoNZBNetGroupCatalogItem,
  GoNZBNetHealthAttestationDiagnostic,
  GoNZBNetKeyExportRequest,
  GoNZBNetKeyExportResponse,
  GoNZBNetKeyRotateRequest,
  GoNZBNetKeyRotateResponse,
  GoNZBNetListResponse,
  GoNZBNetManifestSourceDiagnostic,
  GoNZBNetManifestResolveRequest,
  GoNZBNetManifestResolveResponse,
  GoNZBNetNodeCapability,
  GoNZBNetNodeProfileResponse,
  GoNZBNetOutcomeRequest,
  GoNZBNetPeerActionResponse,
  GoNZBNetPeerDeliveryDiagnostic,
  GoNZBNetPeerDiagnostic,
  GoNZBNetPeerRequest,
  GoNZBNetPoolMember,
  GoNZBNetPoolMemberRequest,
  GoNZBNetRejectedEventDiagnostic,
  GoNZBNetReleaseSourceDiagnostic,
  GoNZBNetReputationDiagnostic,
  GoNZBNetSyncActionResponse,
  GoNZBNetTombstone,
  GoNZBNetTombstoneRequest,
  GoNZBNetTrustPool,
  GoNZBNetTrustPoolRequest,
  GoNZBNetValidationTaskDiagnostic,
  GoNZBNetValidationGap,
  IndexerOverview,
  ReleaseOverridePatch,
} from "../types"

export function getAdminOverview() {
  return apiRequest<IndexerOverview>("/api/v1/admin/indexer/overview")
}

export function getAdminDashboardStats() {
  return apiRequest<IndexerDashboardStats>(
    "/api/v1/admin/indexer/overview/stats",
  )
}

export function refreshAdminDashboardStats() {
  return apiRequest<IndexerDashboardStats>(
    "/api/v1/admin/indexer/overview/stats/actions/refresh",
    { method: "POST" },
  )
}

export function getAdminBackfillProgress() {
  return apiRequest<IndexerBackfillProgress>(
    "/api/v1/admin/indexer/overview/backfill-progress",
  )
}

export function getAdminRecoveryCapacity() {
  return apiRequest<IndexerRecoveryCapacity>("/api/v1/admin/indexer/work/recovery-capacity")
}

export function getAdminGroupProfiles(limit = 50) {
  return apiRequest<IndexerGroupProfileResponse>(`/api/v1/admin/indexer/work/group-profiles?limit=${limit}`)
}

export function getAdminDeferredRanges(limit = 50, state = "queued") {
  const query = new URLSearchParams({ limit: String(limit) })
  if (state) {
    query.set("state", state)
  }
  return apiRequest<IndexerDeferredArticleRangeResponse>(`/api/v1/admin/indexer/work/deferred-ranges?${query.toString()}`)
}

export function getAdminArticleCohorts(params: { kind?: string; status?: string; limit?: number; offset?: number } = {}) {
  const query = new URLSearchParams()
  query.set("limit", String(params.limit ?? 100))
  query.set("offset", String(params.offset ?? 0))
  if (params.kind) {
    query.set("kind", params.kind)
  }
  if (params.status) {
    query.set("status", params.status)
  }
  return apiRequest<AdminArticleCohortListResponse>(`/api/v1/admin/indexer/work/cohorts?${query.toString()}`)
}

export function getAdminStageThroughput() {
  return apiRequest<IndexerStageThroughput>(
    "/api/v1/admin/indexer/overview/throughput",
  )
}

export function getAdminNNTPStats() {
  return apiRequest<IndexerNNTPStats>("/api/v1/admin/indexer/overview/nntp")
}

export function getAdminStorageStatus() {
  return apiRequest<IndexerStorageStatus>("/api/v1/admin/indexer/storage")
}

export function openAdminOverviewStream(
  onMessage: (snapshot: IndexerOverviewStreamSnapshot) => void,
) {
  const source = new EventSource(
    apiURL("/api/v1/admin/indexer/overview/stream"),
    { withCredentials: true },
  )
  source.addEventListener("overview", (event) => {
    const message = event as MessageEvent<string>
    onMessage(JSON.parse(message.data) as IndexerOverviewStreamSnapshot)
  })
  return source
}

export function getAdminScrapeConfig() {
  return apiRequest<AdminScrapeConfigResponse>("/api/v1/admin/indexer/scrape")
}

export function updateAdminScrapeConfig(body: Record<string, unknown>) {
  return apiRequest<AdminScrapeConfigResponse>("/api/v1/admin/indexer/scrape", {
    method: "PUT",
    body,
  })
}

export function scanAdminScrapeProviders() {
  return apiRequest<AdminScrapeConfigResponse>(
    "/api/v1/admin/indexer/scrape/actions/scan",
    { method: "POST" },
  )
}

export function getAdminScrapeProviderInventory(params?: {
  q?: string
  limit?: number
  offset?: number
  sort?: string
  direction?: string
}) {
  const query = new URLSearchParams()
  if (params?.q) {
    query.set("q", params.q)
  }
  if (params?.limit) {
    query.set("limit", String(params.limit))
  }
  if (params?.offset) {
    query.set("offset", String(params.offset))
  }
  if (params?.sort) {
    query.set("sort", params.sort)
  }
  if (params?.direction) {
    query.set("direction", params.direction)
  }
  const suffix = query.toString()
  return apiRequest<{
    items: AdminScrapeConfigResponse["provider_group_inventory"]
    count: number
    limit: number
    offset: number
  }>(
    `/api/v1/admin/indexer/scrape/provider-inventory${suffix ? `?${suffix}` : ""}`,
  )
}

export function previewAdminScrapeWildcards(params?: {
  q?: string
  limit?: number
  offset?: number
}) {
  const query = new URLSearchParams()
  if (params?.q) {
    query.set("q", params.q)
  }
  if (params?.limit) {
    query.set("limit", String(params.limit))
  }
  if (params?.offset) {
    query.set("offset", String(params.offset))
  }
  const suffix = query.toString()
  return apiRequest<{
    items: AdminScrapeConfigResponse["preview_groups"]
    count: number
    limit: number
    offset: number
  }>(`/api/v1/admin/indexer/scrape/preview${suffix ? `?${suffix}` : ""}`)
}

export function getAdminScrapeCrosspostPopularity(params?: { limit?: number }) {
  const query = new URLSearchParams()
  if (params?.limit) {
    query.set("limit", String(params.limit))
  }
  const suffix = query.toString()
  return apiRequest<{
    items: AdminScrapeConfigResponse["crosspost_popularity"]
    limit: number
  }>(
    `/api/v1/admin/indexer/scrape/crosspost-popularity${suffix ? `?${suffix}` : ""}`,
  )
}

export function applyAdminScrapeWildcards() {
  return apiRequest<AdminScrapeConfigResponse>(
    "/api/v1/admin/indexer/scrape/actions/apply",
    { method: "POST" },
  )
}

export async function getAdminStages() {
  const response = await apiRequest<AdminStagesResponse>(
    "/api/v1/admin/indexer/stages",
  )
  return response.items
}

export function patchAdminStage(
  stageName: string,
  patch: AdminStageConfigPatch,
) {
  return apiRequest(`/api/v1/admin/indexer/stages/${stageName}`, {
    method: "PATCH",
    body: patch,
  })
}

export function runStageAction(
  stageName: string,
  action: "run" | "pause" | "resume",
) {
  return apiRequest(
    `/api/v1/admin/indexer/stages/${stageName}/actions/${action}`,
    { method: "POST" },
  )
}

export async function getAdminMaintenanceTasks() {
  const response = await apiRequest<AdminMaintenanceTasksResponse>(
    "/api/v1/admin/indexer/maintenance/tasks",
  )
  return response.items
}

export function getAdminMaintenanceStorageAudit() {
  return apiRequest<AdminStorageAuditReport>(
    "/api/v1/admin/indexer/maintenance/storage-audit",
  )
}

export function dryRunAdminMaintenanceTask(taskKey: string) {
  return apiRequest<AdminMaintenanceTaskRun>(
    `/api/v1/admin/indexer/maintenance/tasks/${taskKey}/dry-run`,
    { method: "POST" },
  )
}

export function runAdminMaintenanceTask(taskKey: string) {
  return apiRequest<AdminMaintenanceTaskRun>(
    `/api/v1/admin/indexer/maintenance/tasks/${taskKey}/run`,
    { method: "POST" },
  )
}

export function patchAdminMaintenanceTask(
  taskKey: string,
  patch: AdminMaintenanceTaskPatch,
) {
  return apiRequest(`/api/v1/admin/indexer/maintenance/tasks/${taskKey}`, {
    method: "PATCH",
    body: patch,
  })
}

export function getAdminRuns(params: AdminRunListParams) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (!value) {
      continue
    }
    query.set(key, String(value))
  }
  return apiRequest<AdminRunsResponse>(
    `/api/v1/admin/indexer/runs?${query.toString()}`,
  )
}

export function getAdminRun(id: string) {
  return apiRequest<AdminRunDetailResponse>(`/api/v1/admin/indexer/runs/${id}`)
}

export function getAdminReleases(params: AdminReleaseListParams) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === "") {
      continue
    }
    query.set(key, String(value))
  }
  return apiRequest<AdminReleaseListResponse>(
    `/api/v1/admin/indexer/releases?${query.toString()}`,
  )
}

export function getAdminAttention(params: AdminAttentionListParams) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === "") {
      continue
    }
    query.set(key, String(value))
  }
  return apiRequest<AdminAttentionListResponse>(
    `/api/v1/admin/indexer/attention?${query.toString()}`,
  )
}

export function getAdminRelease(id: string) {
  return apiRequest<AdminReleaseDetailResponse>(
    `/api/v1/admin/indexer/releases/${id}`,
  )
}

export function getAdminBinaries(params: AdminBinaryListParams) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === "") {
      continue
    }
    query.set(key, String(value))
  }
  return apiRequest<AdminBinaryListResponse>(
    `/api/v1/admin/indexer/binaries?${query.toString()}`,
  )
}

export function getIndexerFile(id: number) {
  return apiRequest<AdminFileDetail>(`/api/v1/indexer/files/${id}`)
}

export function getIndexerBinary(id: number) {
  return apiRequest<AdminBinaryDetail>(`/api/v1/indexer/binaries/${id}`)
}

export function patchAdminRelease(id: string, body: ReleaseOverridePatch) {
  return apiRequest(`/api/v1/admin/indexer/releases/${id}`, {
    method: "PATCH",
    body,
  })
}

export function identifyAdminRelease(
  id: string,
  body:
    | { source: "predb"; predb_entry_id: number }
    | {
        source: "manual"
        title: string
        external_media_type?: string
        external_year?: number
        season_number?: number
        episode_number?: number
        classification?: string
        notes?: string
      },
) {
  return apiRequest(`/api/v1/admin/indexer/releases/${id}/actions/identify`, {
    method: "POST",
    body,
  })
}

export function hideAdminRelease(id: string) {
  return apiRequest(`/api/v1/admin/indexer/releases/${id}/actions/hide`, {
    method: "POST",
  })
}

export function unhideAdminRelease(id: string) {
  return apiRequest(`/api/v1/admin/indexer/releases/${id}/actions/unhide`, {
    method: "POST",
  })
}

export function reinspectAdminRelease(id: string) {
  return apiRequest(`/api/v1/admin/indexer/releases/${id}/actions/reinspect`, {
    method: "POST",
  })
}

export function reenrichAdminRelease(id: string) {
  return apiRequest(`/api/v1/admin/indexer/releases/${id}/actions/reenrich`, {
    method: "POST",
  })
}

function goNZBNetQuery(params: Record<string, string | number | undefined> = {}) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === "") {
      continue
    }
    query.set(key, String(value))
  }
  const suffix = query.toString()
  return suffix ? `?${suffix}` : ""
}

export function getGoNZBNetNodeCapabilities() {
  return apiRequest<GoNZBNetListResponse<GoNZBNetNodeCapability>>(
    "/api/v1/admin/gonzbnet/nodes/capabilities",
  )
}

export function getGoNZBNetNodeProfile() {
  return apiRequest<GoNZBNetNodeProfileResponse>(
    "/api/v1/admin/gonzbnet/node/profile",
  )
}

export function getGoNZBNetConfigValidation() {
  return apiRequest<GoNZBNetConfigValidation>(
    "/api/v1/admin/gonzbnet/config/validation",
  )
}

export function getGoNZBNetCoverageDashboard(poolID?: string) {
  return apiRequest<GoNZBNetCoverageDashboard>(
    `/api/v1/admin/gonzbnet/coverage${goNZBNetQuery({ pool_id: poolID })}`,
  )
}

export function getGoNZBNetCoverageGroups(poolID?: string) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetGroupCatalogItem>>(
    `/api/v1/admin/gonzbnet/coverage/groups${goNZBNetQuery({ pool_id: poolID })}`,
  )
}

export function getGoNZBNetValidationGaps(poolID?: string, limit = 100) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetValidationGap>>(
    `/api/v1/admin/gonzbnet/coverage/validation-gaps${goNZBNetQuery({ pool_id: poolID, limit })}`,
  )
}

export function getGoNZBNetCoverageSuggestions(
  params: GoNZBNetCoverageSuggestionParams = {},
) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetCoverageSuggestion>>(
    `/api/v1/admin/gonzbnet/coverage/suggestions${goNZBNetQuery(params)}`,
  )
}

export function getGoNZBNetCoveragePlan(
  params: GoNZBNetCoverageSuggestionParams = {},
) {
  return apiRequest<GoNZBNetCoveragePlan>(
    `/api/v1/admin/gonzbnet/coverage/plan${goNZBNetQuery(params)}`,
  )
}

export function createGoNZBNetCoverageAssignment(
  body: GoNZBNetAssignmentRequest,
) {
  return apiRequest<GoNZBNetActionResponse>(
    "/api/v1/admin/gonzbnet/coverage/assignments",
    { method: "POST", body },
  )
}

export function createGoNZBNetCoverageClaim(body: GoNZBNetClaimRequest) {
  return apiRequest<GoNZBNetActionResponse>(
    "/api/v1/admin/gonzbnet/coverage/claims",
    { method: "POST", body },
  )
}

export function createGoNZBNetCoverageComplete(
  body: GoNZBNetOutcomeRequest,
) {
  return apiRequest<GoNZBNetActionResponse>(
    "/api/v1/admin/gonzbnet/coverage/complete",
    { method: "POST", body },
  )
}

export function createGoNZBNetCoverageFailed(body: GoNZBNetOutcomeRequest) {
  return apiRequest<GoNZBNetActionResponse>(
    "/api/v1/admin/gonzbnet/coverage/failed",
    { method: "POST", body },
  )
}

export function materializeGoNZBNetStalePenalties(poolID?: string) {
  return apiRequest<GoNZBNetActionResponse>(
    `/api/v1/admin/gonzbnet/coverage/stale-penalties${goNZBNetQuery({ pool_id: poolID })}`,
    { method: "POST" },
  )
}

export function getGoNZBNetPeerDiagnostics(limit = 100) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetPeerDiagnostic>>(
    `/api/v1/admin/gonzbnet/diagnostics/peers${goNZBNetQuery({ limit })}`,
  )
}

export function getGoNZBNetEventDiagnostics(limit = 100) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetEventDiagnostic>>(
    `/api/v1/admin/gonzbnet/diagnostics/events${goNZBNetQuery({ limit })}`,
  )
}

export function getGoNZBNetRejectedEventDiagnostics(limit = 100) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetRejectedEventDiagnostic>>(
    `/api/v1/admin/gonzbnet/diagnostics/rejected-events${goNZBNetQuery({ limit })}`,
  )
}

export function getGoNZBNetPeerDeliveryDiagnostics(limit = 100) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetPeerDeliveryDiagnostic>>(
    `/api/v1/admin/gonzbnet/diagnostics/deliveries${goNZBNetQuery({ limit })}`,
  )
}

export function getGoNZBNetValidationTaskDiagnostics(limit = 100) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetValidationTaskDiagnostic>>(
    `/api/v1/admin/gonzbnet/diagnostics/validation-tasks${goNZBNetQuery({ limit })}`,
  )
}

export function getGoNZBNetReleaseSourceDiagnostics(poolID?: string, limit = 100) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetReleaseSourceDiagnostic>>(
    `/api/v1/admin/gonzbnet/diagnostics/release-sources${goNZBNetQuery({ pool_id: poolID, limit })}`,
  )
}

export function getGoNZBNetManifestSourceDiagnostics(poolID?: string, limit = 100) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetManifestSourceDiagnostic>>(
    `/api/v1/admin/gonzbnet/diagnostics/manifest-sources${goNZBNetQuery({ pool_id: poolID, limit })}`,
  )
}

export function getGoNZBNetHealthDiagnostics(poolID?: string, limit = 100) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetHealthAttestationDiagnostic>>(
    `/api/v1/admin/gonzbnet/diagnostics/health${goNZBNetQuery({ pool_id: poolID, limit })}`,
  )
}

export function getGoNZBNetReputationDiagnostics(limit = 100) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetReputationDiagnostic>>(
    `/api/v1/admin/gonzbnet/diagnostics/reputation${goNZBNetQuery({ limit })}`,
  )
}

export function resolveGoNZBNetManifest(body: GoNZBNetManifestResolveRequest) {
  return apiRequest<GoNZBNetManifestResolveResponse>(
    "/api/v1/admin/gonzbnet/manifests/resolve",
    { method: "POST", body },
  )
}

export function getGoNZBNetTrustPools() {
  return apiRequest<GoNZBNetListResponse<GoNZBNetTrustPool>>(
    "/api/v1/admin/gonzbnet/pools",
  )
}

export function upsertGoNZBNetTrustPool(body: GoNZBNetTrustPoolRequest) {
  return apiRequest<GoNZBNetActionResponse>("/api/v1/admin/gonzbnet/pools", {
    method: "POST",
    body,
  })
}

export function getGoNZBNetPoolMembers(poolID: string) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetPoolMember>>(
    `/api/v1/admin/gonzbnet/pools/${encodeURIComponent(poolID)}/members`,
  )
}

export function upsertGoNZBNetPoolMember(
  poolID: string,
  body: GoNZBNetPoolMemberRequest,
) {
  return apiRequest<GoNZBNetActionResponse>(
    `/api/v1/admin/gonzbnet/pools/${encodeURIComponent(poolID)}/members`,
    { method: "POST", body },
  )
}

export function revokeGoNZBNetPoolMember(poolID: string, nodeID: string) {
  return apiRequest<GoNZBNetActionResponse>(
    `/api/v1/admin/gonzbnet/pools/${encodeURIComponent(poolID)}/members/${encodeURIComponent(nodeID)}/revoke`,
    { method: "POST" },
  )
}

export function getGoNZBNetTombstones(activeOnly = false) {
  return apiRequest<GoNZBNetListResponse<GoNZBNetTombstone>>(
    `/api/v1/admin/gonzbnet/moderation/tombstones${goNZBNetQuery({ active: activeOnly ? "true" : undefined })}`,
  )
}

export function createGoNZBNetTombstone(body: GoNZBNetTombstoneRequest) {
  return apiRequest<GoNZBNetActionResponse>(
    "/api/v1/admin/gonzbnet/moderation/tombstones",
    { method: "POST", body },
  )
}

export function upsertGoNZBNetPeer(body: GoNZBNetPeerRequest) {
  return apiRequest<GoNZBNetPeerActionResponse>(
    "/api/v1/admin/gonzbnet/peers",
    { method: "POST", body },
  )
}

export function setGoNZBNetPeerEnabled(peerID: number, enabled: boolean) {
  return apiRequest<GoNZBNetPeerActionResponse>(
    `/api/v1/admin/gonzbnet/peers/${peerID}/${enabled ? "enable" : "disable"}`,
    { method: "POST" },
  )
}

export function deleteGoNZBNetPeer(peerID: number) {
  return apiRequest<GoNZBNetPeerActionResponse>(
    `/api/v1/admin/gonzbnet/peers/${peerID}`,
    { method: "DELETE" },
  )
}

export function setGoNZBNetNodeBlocked(nodeID: string, blocked: boolean) {
  return apiRequest<GoNZBNetActionResponse>(
    `/api/v1/admin/gonzbnet/nodes/${encodeURIComponent(nodeID)}/${blocked ? "block" : "unblock"}`,
    { method: "POST" },
  )
}

export function exportGoNZBNetKey(body: GoNZBNetKeyExportRequest) {
  return apiRequest<GoNZBNetKeyExportResponse>(
    "/api/v1/admin/gonzbnet/keys/export",
    { method: "POST", body },
  )
}

export function rotateGoNZBNetKey(body: GoNZBNetKeyRotateRequest) {
  return apiRequest<GoNZBNetKeyRotateResponse>(
    "/api/v1/admin/gonzbnet/keys/rotate",
    { method: "POST", body },
  )
}

export function runGoNZBNetPullSync() {
  return apiRequest<GoNZBNetSyncActionResponse>(
    "/api/v1/admin/gonzbnet/sync/pull",
    { method: "POST" },
  )
}

export function runGoNZBNetPushSync(limit?: number) {
  return apiRequest<GoNZBNetSyncActionResponse>(
    `/api/v1/admin/gonzbnet/sync/push${goNZBNetQuery({ limit })}`,
    { method: "POST" },
  )
}

export function runGoNZBNetGossipSync() {
  return apiRequest<GoNZBNetSyncActionResponse>(
    "/api/v1/admin/gonzbnet/sync/gossip",
    { method: "POST" },
  )
}
