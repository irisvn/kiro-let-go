const API_KEY_STORAGE = 'kiro_admin_api_key'

export class ApiError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

export class UnauthorizedError extends ApiError {
  constructor() {
    super('Unauthorized')
    this.name = 'UnauthorizedError'
  }
}

export async function apiCall<T>(method: string, path: string, body?: unknown): Promise<T> {
  const key = localStorage.getItem(API_KEY_STORAGE)
  const opts: RequestInit = {
    method,
    headers: {
      Authorization: `Bearer ${key}`,
      'Content-Type': 'application/json',
    },
  }
  if (body) opts.body = JSON.stringify(body)
  const res = await fetch(path, opts)

  if (res.status === 401) {
    localStorage.removeItem(API_KEY_STORAGE)
    throw new UnauthorizedError()
  }

  if (!res.ok) {
    const errText = await res.text()
    try {
      const errJSON = JSON.parse(errText)
      if (errJSON?.error?.message) {
        throw new ApiError(errJSON.error.message)
      }
    } catch (parseErr) {
      if (parseErr instanceof ApiError) throw parseErr
    }
    throw new ApiError(errText || `Request failed: ${res.status}`)
  }

  if (res.status === 204) return null as T
  return res.json() as Promise<T>
}

export function getApiKey(): string | null {
  return localStorage.getItem(API_KEY_STORAGE)
}

export function setApiKey(key: string): void {
  localStorage.setItem(API_KEY_STORAGE, key)
}

export function removeApiKey(): void {
  localStorage.removeItem(API_KEY_STORAGE)
}
