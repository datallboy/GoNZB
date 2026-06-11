const API_BASE = (import.meta.env.VITE_API_BASE_URL as string | undefined)?.replace(/\/$/, '') ?? ''

type RequestOptions = {
  method?: string
  body?: unknown
}

export async function apiRequest<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers = new Headers()
  if (options.body !== undefined) {
    headers.set('Content-Type', 'application/json')
  }
  if (!isSafeMethod(options.method ?? 'GET')) {
    const csrfToken = readCookie('gonzb_csrf')
    if (csrfToken) {
      headers.set('X-CSRF-Token', csrfToken)
    }
  }

  const response = await fetch(`${API_BASE}${path}`, {
    method: options.method ?? 'GET',
    headers,
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    credentials: 'include',
  })

  if (!response.ok) {
    const message = await parseError(response)
    throw new Error(message)
  }
  if (response.status === 204) {
    return undefined as T
  }
  return (await response.json()) as T
}

export function apiURL(path: string) {
  return `${API_BASE}${path}`
}

function isSafeMethod(method: string) {
  return method === 'GET' || method === 'HEAD' || method === 'OPTIONS'
}

function readCookie(name: string) {
  if (typeof document === 'undefined') {
    return ''
  }
  const prefix = `${name}=`
  for (const part of document.cookie.split(';')) {
    const trimmed = part.trim()
    if (trimmed.startsWith(prefix)) {
      return decodeURIComponent(trimmed.slice(prefix.length))
    }
  }
  return ''
}

async function parseError(response: Response): Promise<string> {
  const contentType = response.headers.get('content-type') ?? ''
  if (!contentType.includes('application/json')) {
    return response.statusText || `Request failed with status ${response.status}`
  }
  const payload = (await response.json()) as { error?: string | { message?: string } }
  if (typeof payload.error === 'string') {
    return payload.error
  }
  return payload.error?.message ?? response.statusText ?? `Request failed with status ${response.status}`
}
