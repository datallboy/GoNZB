import { apiFormRequest, apiRequest } from './http'
import type {
  UploaderDetailResponse,
  UploaderListResponse,
  UploaderSubmissionResponse,
  UploaderFederationPoolsResponse,
  UploaderFederationPublicationsResponse,
  UploaderUpdate,
} from '../types'

export function listUploaderSubmissions(params: { q?: string; state?: string } = {}) {
  const query = new URLSearchParams()
  if (params.q) query.set('q', params.q)
  if (params.state) query.set('state', params.state)
  return apiRequest<UploaderListResponse>(`/api/v1/uploader/submissions?${query.toString()}`)
}

export function getUploaderSubmission(id: string) {
  return apiRequest<UploaderDetailResponse>(`/api/v1/uploader/submissions/${encodeURIComponent(id)}`)
}

export function createUploaderSubmission(file: File, metadata?: { title?: string; category_id?: number }) {
  const form = new FormData()
  form.append('nzb', file, file.name)
  if (metadata && (metadata.title || metadata.category_id)) {
    form.append('metadata', JSON.stringify(metadata))
  }
  return apiFormRequest<UploaderSubmissionResponse>('/api/v1/uploader/submissions', form)
}

export function updateUploaderSubmission(id: string, update: UploaderUpdate) {
  return apiRequest<UploaderSubmissionResponse>(`/api/v1/uploader/submissions/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: update,
  })
}

export function approveUploaderSubmission(id: string, note = '') {
  return uploaderAction(id, 'approve', note)
}

export function rejectUploaderSubmission(id: string, note = '') {
  return uploaderAction(id, 'reject', note)
}

export function returnUploaderSubmissionToPending(id: string, note = '') {
  return uploaderAction(id, 'return-to-pending', note)
}

export function getEligibleUploaderFederationPools() {
  return apiRequest<UploaderFederationPoolsResponse>('/api/v1/uploader/federation-pools')
}

export function publishUploaderSubmission(id: string, poolIds: string[]) {
  return apiRequest<UploaderFederationPublicationsResponse>(
    `/api/v1/uploader/submissions/${encodeURIComponent(id)}/federation-publications`,
    { method: 'POST', body: { pool_ids: poolIds } },
  )
}

export function withdrawUploaderPublication(id: string, poolId: string, reason = '') {
  return apiRequest<{ publication: import('../types').UploaderFederationPublication }>(
    `/api/v1/uploader/submissions/${encodeURIComponent(id)}/federation-publications/${encodeURIComponent(poolId)}`,
    { method: 'DELETE', body: { reason } },
  )
}

function uploaderAction(id: string, action: string, note: string) {
  return apiRequest<UploaderSubmissionResponse>(
    `/api/v1/uploader/submissions/${encodeURIComponent(id)}/actions/${action}`,
    { method: 'POST', body: { note } },
  )
}
