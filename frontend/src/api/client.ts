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
  const form = body instanceof FormData // the profile picture upload: the browser sets the multipart type
  if (unsafe) {
    if (!form) headers['Content-Type'] = 'application/json'
    if (csrfToken) headers['X-CSRF-Token'] = csrfToken
  }
  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      headers,
      credentials: 'same-origin',
      body: !unsafe ? undefined : form ? body : JSON.stringify(body ?? {}),
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

/** upload posts a form with XMLHttpRequest (fetch reports no upload progress): the same cookies, CSRF token and
 *  ApiError as api(); abort() cancels it (the promise then rejects with code "aborted"). */
export function upload<T>(
  path: string,
  form: FormData,
  onProgress: (fraction: number) => void,
): { done: Promise<T>; abort: () => void } {
  const xhr = new XMLHttpRequest()
  const done = new Promise<T>((resolve, reject) => {
    xhr.open('POST', `/api/v1${path}`)
    xhr.responseType = 'json'
    xhr.setRequestHeader('Accept', 'application/json')
    if (csrfToken) xhr.setRequestHeader('X-CSRF-Token', csrfToken)
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(e.loaded / e.total)
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) return resolve(xhr.response as T)
      const err = (xhr.response as { error?: { code?: string; message?: string } } | null)?.error
      const code = err?.code ?? 'internal'
      if (xhr.status === 401 && onUnauthorized) onUnauthorized(code)
      const retry = Number(xhr.getResponseHeader('Retry-After')) || undefined
      reject(new ApiError(xhr.status, code, err?.message ?? xhr.statusText, retry))
    }
    xhr.onerror = () => reject(new ApiError(0, 'network', 'The server could not be reached.'))
    xhr.onabort = () => reject(new ApiError(0, 'aborted', 'Cancelled.'))
    xhr.send(form)
  })
  return { done, abort: () => xhr.abort() }
}
