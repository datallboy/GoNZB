import { apiRequest } from './http'
import type {
  AdminReleaseDetailResponse,
  AdminReleaseListResponse,
  AdminReleaseListParams,
  AdminRunDetailResponse,
  AdminRunListParams,
  AdminRunsResponse,
  AdminStageConfigPatch,
  AdminStagesResponse,
  IndexerOverview,
  ReleaseOverridePatch,
} from '../types'

export function getAdminOverview() {
  return apiRequest<IndexerOverview>('/api/v1/admin/indexer/overview')
}

export async function getAdminStages() {
  const response = await apiRequest<AdminStagesResponse>('/api/v1/admin/indexer/stages')
  return response.items
}

export function patchAdminStage(stageName: string, patch: AdminStageConfigPatch) {
  return apiRequest(`/api/v1/admin/indexer/stages/${stageName}`, { method: 'PATCH', body: patch })
}

export function runStageAction(stageName: string, action: 'run' | 'pause' | 'resume') {
  return apiRequest(`/api/v1/admin/indexer/stages/${stageName}/actions/${action}`, { method: 'POST' })
}

export function getAdminRuns(params: AdminRunListParams) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (!value) {
      continue
    }
    query.set(key, String(value))
  }
  return apiRequest<AdminRunsResponse>(`/api/v1/admin/indexer/runs?${query.toString()}`)
}

export function getAdminRun(id: string) {
  return apiRequest<AdminRunDetailResponse>(`/api/v1/admin/indexer/runs/${id}`)
}

export function getAdminReleases(params: AdminReleaseListParams) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') {
      continue
    }
    query.set(key, String(value))
  }
  return apiRequest<AdminReleaseListResponse>(`/api/v1/admin/indexer/releases?${query.toString()}`)
}

export function getAdminRelease(id: string) {
  return apiRequest<AdminReleaseDetailResponse>(`/api/v1/admin/indexer/releases/${id}`)
}

export function patchAdminRelease(id: string, body: ReleaseOverridePatch) {
  return apiRequest(`/api/v1/admin/indexer/releases/${id}`, { method: 'PATCH', body })
}

export function hideAdminRelease(id: string) {
  return apiRequest(`/api/v1/admin/indexer/releases/${id}/actions/hide`, { method: 'POST' })
}

export function unhideAdminRelease(id: string) {
  return apiRequest(`/api/v1/admin/indexer/releases/${id}/actions/unhide`, { method: 'POST' })
}

export function reinspectAdminRelease(id: string) {
  return apiRequest(`/api/v1/admin/indexer/releases/${id}/actions/reinspect`, { method: 'POST' })
}

export function reenrichAdminRelease(id: string) {
  return apiRequest(`/api/v1/admin/indexer/releases/${id}/actions/reenrich`, { method: 'POST' })
}
