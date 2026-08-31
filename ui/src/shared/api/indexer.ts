import { apiRequest } from './http'
import type { PublicReleaseDetail, PublicReleaseListResponse } from '../types'

export function listPublicReleases(params: Record<string, string | number>) {
  const query = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== '' && value !== 0) {
      query.set(key, String(value))
    }
  })
  return apiRequest<PublicReleaseListResponse>(`/api/v1/catalog/releases?${query.toString()}`)
}

export function getPublicRelease(id: string) {
	return apiRequest<PublicReleaseDetail>(`/api/v1/catalog/releases/${id}`)
}

export function sendReleaseToDownloadClient(releaseID: string) {
	return apiRequest(`/api/v1/indexer/releases/${encodeURIComponent(releaseID)}/actions/send-to-download-client`, {
		method: 'POST',
	})
}
