import { z } from 'zod'

export const QueueStatusSchema = z.enum([
  'pending',
  'downloading',
  'processing',
  'completed',
  'failed',
])

export const QueueReleaseSchema = z.object({
  id: z.string(),
  title: z.string(),
  size: z.number(),
  category: z.string(),
  source: z.string(),
  publish_date: z.string().optional(),
})

export const QueueProgressSchema = z.object({
  bytes_written: z.number(),
})

export const QueueItemSchema = z.object({
  id: z.string(),
  release_id: z.string(),
  status: QueueStatusSchema,
  out_dir: z.string(),
  error: z.string().optional(),
  created_at: z.string().optional(),
  updated_at: z.string().optional(),
  started_at: z.string().optional(),
  completed_at: z.string().optional(),
  release: QueueReleaseSchema.optional(),
  progress: QueueProgressSchema,
  metrics: z.object({
    downloaded_bytes: z.number(),
    avg_bps: z.number(),
    download_seconds: z.number(),
    postprocess_seconds: z.number(),
  }),
})

export const QueueListResponseSchema = z.object({
  items: z.array(QueueItemSchema),
  count: z.number(),
})

export const QueueHistoryResponseSchema = z.object({
  items: z.array(QueueItemSchema),
  total: z.number(),
  limit: z.number(),
  offset: z.number(),
  status: z.string(),
  has_more: z.boolean(),
})

export const QueueCancelResponseSchema = z.object({
  ok: z.boolean(),
  id: z.string(),
})

export const QueueBulkResultSchema = z.object({
  ok: z.boolean(),
  requested: z.number().optional(),
  cancelled: z.number().optional(),
  deleted: z.number().optional(),
})

export const QueueFileSchema = z.object({
  id: z.number(),
  filename: z.string(),
  size: z.number(),
  index: z.number(),
  is_pars: z.boolean(),
  subject: z.string(),
  date: z.number(),
  groups: z.array(z.string()),
})

export const QueueEventSchema = z.object({
  id: z.number(),
  stage: z.string(),
  status: z.string(),
  message: z.string(),
  meta_json: z.string(),
  created_at: z.string(),
})

export const QueueFilesResponseSchema = z.object({
  items: z.array(QueueFileSchema),
  count: z.number(),
})

export const QueueEventsResponseSchema = z.object({
  items: z.array(QueueEventSchema),
  count: z.number(),
})

export const ReleaseSearchItemSchema = z.object({
  id: z.string(),
  title: z.string(),
  size: z.number(),
  category: z.string(),
  source: z.string(),
  cache_present: z.boolean(),
  cache_blob_size: z.number(),
})

export const ReleaseSearchResponseSchema = z.object({
  items: z.array(ReleaseSearchItemSchema),
  count: z.number(),
})

export type QueueStatus = z.infer<typeof QueueStatusSchema>
export type QueueItem = z.infer<typeof QueueItemSchema>
export type QueueListResponse = z.infer<typeof QueueListResponseSchema>
export type QueueHistoryResponse = z.infer<typeof QueueHistoryResponseSchema>
export type ReleaseSearchItem = z.infer<typeof ReleaseSearchItemSchema>
export type QueueFile = z.infer<typeof QueueFileSchema>
export type QueueEvent = z.infer<typeof QueueEventSchema>
