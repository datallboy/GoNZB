import { apiRequest } from './http'

export function getSettings() {
  return apiRequest<Record<string, unknown>>('/api/v1/admin/settings')
}

export function getCapabilities() {
  return apiRequest<Record<string, unknown>>('/api/v1/admin/capabilities')
}

export function updateSettings(body: Record<string, unknown>) {
  return apiRequest<Record<string, unknown>>('/api/v1/admin/settings', { method: 'PUT', body })
}

export function testSettingsConnection(body: { kind: 'postgres' | 'nntp' | 'newznab'; id?: string }) {
  return apiRequest<{ ok: boolean; kind: string; id?: string; latency_ms: number; message: string }>(
    '/api/v1/admin/connections/test',
    { method: 'POST', body },
  )
}
