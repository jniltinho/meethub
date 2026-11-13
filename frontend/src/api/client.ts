// Typed client for /api/v1: JSON, same-origin cookies, the session's CSRF token on state-changing requests, and
// errors as ApiError with the server's stable code.
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    public readonly retryAfter?: number,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

let csrfToken = ''
let onUnauthorized: ((code: string) => void) | null = null

export function setCsrfToken(token: string): void {
  csrfToken = token
}

// setUnauthorizedHandler is called when an authenticated call answers 401 (session ended).
export function setUnauthorizedHandler(fn: (code: string) => void): void {
  onUnauthorized = fn
}

export async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const unsafe = method !== 'GET' && method !== 'HEAD'
  if (unsafe) {
    headers['Content-Type'] = 'application/json'
    if (csrfToken) headers['X-CSRF-Token'] = csrfToken
  }
  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      headers,
      credentials: 'same-origin',
      body: unsafe ? JSON.stringify(body ?? {}) : undefined,
    })
  } catch {
    throw new ApiError(0, 'network', 'The server could not be reached.')
  }
  if (res.status === 204) return undefined as T
  const data: unknown = await res.json().catch(() => null)
  if (!res.ok) {
    const err = (data as { error?: { code?: string; message?: string } } | null)?.error
    const code = err?.code ?? 'internal'
    const retry = Number(res.headers.get('Retry-After')) || undefined
    if (res.status === 401 && path !== '/auth/login' && onUnauthorized) onUnauthorized(code)
    throw new ApiError(res.status, code, err?.message ?? res.statusText, retry)
  }
  return data as T
}
