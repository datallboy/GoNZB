import { z } from 'zod'
import { apiGet, apiPostForm, apiPostJson, type ApiConfig } from './client'
import {
  QueueBulkResultSchema,
  QueueCancelResponseSchema,
  QueueEventsResponseSchema,
  QueueFilesResponseSchema,
  QueueHistoryResponseSchema,
  QueueItemSchema,
  QueueListResponseSchema,
  ReleaseSearchResponseSchema,
  type QueueEvent,
  type QueueFile,
  type QueueHistoryResponse,
  type QueueItem,
  type QueueListResponse,
  type ReleaseSearchItem,
} from '../types/queue'

export async function fetchQueue(config: ApiConfig): Promise<QueueListResponse> {
  return apiGet('/api/v1/queue', QueueListResponseSchema, config)
}

export async function fetchHistory(
  config: ApiConfig,
  params: { limit?: number; offset?: number; status?: string } = {},
): Promise<QueueHistoryResponse> {
  const q = new URLSearchParams()
  if (params.limit !== undefined) q.set('limit', String(params.limit))
  if (params.offset !== undefined) q.set('offset', String(params.offset))
  if (params.status) q.set('status', params.status)

  const suffix = q.toString() ? `?${q}` : ''
  return apiGet(`/api/v1/queue/history${suffix}`, QueueHistoryResponseSchema, config)
}

export async function enqueueByReleaseId(
  config: ApiConfig,
  payload: { release_id: string },
): Promise<QueueItem> {
  return apiPostJson('/api/v1/queue', payload, QueueItemSchema, config)
}

export async function enqueueByUpload(config: ApiConfig, file: File): Promise<QueueItem> {
  const form = new FormData()
  form.append('nzb', file)
  return apiPostForm('/api/v1/queue', form, QueueItemSchema, config)
}

export async function cancelQueueItem(config: ApiConfig, id: string): Promise<void> {
  await apiPostJson(`/api/v1/queue/${id}/cancel`, {}, QueueCancelResponseSchema, config)
}

export async function cancelQueueItems(config: ApiConfig, ids: string[]): Promise<void> {
  await apiPostJson('/api/v1/queue/bulk/cancel', { ids }, QueueBulkResultSchema, config)
}

export async function deleteQueueItems(config: ApiConfig, ids: string[]): Promise<void> {
  await apiPostJson('/api/v1/queue/bulk/delete', { ids }, QueueBulkResultSchema, config)
}

export async function clearQueueHistory(config: ApiConfig): Promise<void> {
  await apiPostJson('/api/v1/queue/history/clear', {}, QueueBulkResultSchema, config)
}

export async function fetchQueueItemFiles(config: ApiConfig, id: string): Promise<QueueFile[]> {
  const res = await apiGet(`/api/v1/queue/${id}/files`, QueueFilesResponseSchema, config)
  return res.items
}

export async function fetchQueueItemEvents(config: ApiConfig, id: string): Promise<QueueEvent[]> {
  const res = await apiGet(`/api/v1/queue/${id}/events`, QueueEventsResponseSchema, config)
  return res.items
}

export async function searchReleases(config: ApiConfig, query: string): Promise<ReleaseSearchItem[]> {
  const q = new URLSearchParams({ q: query })
  const res = await apiGet(`/api/v1/releases/search?${q.toString()}`, ReleaseSearchResponseSchema, config)
  return res.items
}

const EventStatsSchema = z.object({
  bps: z.number(),
  progress: z.number(),
  active_jobs: z.number(),
  active_item: z
    .object({
      id: z.string(),
      release_id: z.string(),
      title: z.string(),
      status: z.string(),
      size: z.number(),
      bytes: z.number(),
    })
    .optional(),
})

export type QueueEventStats = z.infer<typeof EventStatsSchema>

export function connectQueueEvents(
  config: ApiConfig,
  handlers: {
    onStats: (stats: QueueEventStats) => void
    onError?: (err: Event) => void
  },
): EventSource {
  const url = new URL('/api/v1/events/queue', config.baseUrl)
  if (config.apiKey) {
    url.searchParams.set('apikey', config.apiKey)
  }

  const source = new EventSource(url.toString())
  source.onmessage = (msg) => {
    try {
      const parsed = EventStatsSchema.parse(JSON.parse(msg.data))
      handlers.onStats(parsed)
    } catch {
      // ignore malformed event payloads
    }
  }
  source.onerror = (err) => {
    if (handlers.onError) handlers.onError(err)
  }

  return source
}
