import { apiRequest, apiURL } from "./http"
import type {
  IndexerBackfillProgress,
  IndexerDashboardStats,
  IndexerDailyBucketResponse,
  IndexerDeferredArticleRangeResponse,
  IndexerGroupProfileResponse,
  IndexerNNTPStats,
  IndexerOverviewStreamSnapshot,
  IndexerRecoveryCapacity,
  IndexerStorageStatus,
  IndexerStageThroughput,
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

export function getAdminDailyBuckets(limit = 50) {
  return apiRequest<IndexerDailyBucketResponse>(`/api/v1/admin/indexer/work/daily-buckets?limit=${limit}`)
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
