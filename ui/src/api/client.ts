import { z } from 'zod'

export type ApiConfig = {
  baseUrl: string
  apiKey?: string
}

export async function apiGet<T>(
  path: string,
  schema: z.ZodSchema<T>,
  config: ApiConfig,
): Promise<T> {
  const url = new URL(path, config.baseUrl)
  const res = await fetch(url.toString(), {
    headers: buildHeaders(config.apiKey),
  })
  return parseResponse(res, schema)
}

export async function apiPostJson<T>(
  path: string,
  body: unknown,
  schema: z.ZodSchema<T>,
  config: ApiConfig,
): Promise<T> {
  const url = new URL(path, config.baseUrl)
  const res = await fetch(url.toString(), {
    method: 'POST',
    headers: {
      ...buildHeaders(config.apiKey),
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })
  return parseResponse(res, schema)
}

export async function apiPostForm<T>(
  path: string,
  form: FormData,
  schema: z.ZodSchema<T>,
  config: ApiConfig,
): Promise<T> {
  const url = new URL(path, config.baseUrl)
  const res = await fetch(url.toString(), {
    method: 'POST',
    headers: buildHeaders(config.apiKey),
    body: form,
  })
  return parseResponse(res, schema)
}

function buildHeaders(apiKey?: string): HeadersInit {
  const headers: HeadersInit = {}
  if (apiKey) {
    headers['X-API-Key'] = apiKey
  }
  return headers
}

async function parseResponse<T>(res: Response, schema: z.ZodSchema<T>): Promise<T> {
  const text = await res.text()
  const raw = text ? JSON.parse(text) : {}

  if (!res.ok) {
    const message =
      typeof raw?.error === 'string'
        ? raw.error
        : `HTTP ${res.status}: ${res.statusText}`
    throw new Error(message)
  }

  return schema.parse(raw)
}
