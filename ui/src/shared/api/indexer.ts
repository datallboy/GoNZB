import { apiRequest } from './http'
import type { PublicReleaseDetail, PublicReleaseListResponse } from '../types'

export function listPublicReleases(params: Record<string, string | number>) {
  const query = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== '' && value !== 0) {
      query.set(key, String(value))
    }
  })
  return apiRequest<PublicReleaseListResponse>(`/api/v1/indexer/releases?${query.toString()}`)
}

export function getPublicRelease(id: string) {
  return apiRequest<PublicReleaseDetail>(`/api/v1/indexer/releases/${id}`)
}

export function enqueueReleaseToDownloader(releaseID: string, title: string) {
  return apiRequest('/api/v1/queue', {
    method: 'POST',
    body: {
      source_kind: 'usenet_index',
      release_id: releaseID,
      title,
    },
  })
}
