import { create } from 'zustand'
import { setApiKey, removeApiKey, getApiKey, UnauthorizedError } from '@/lib/api'

interface AuthState {
  authenticated: boolean
  loading: boolean
  error: string
  loginKey: string
  setLoginKey: (key: string) => void
  login: (key: string) => Promise<void>
  logout: () => void
  checkAuth: () => void
}

export const useAuthStore = create<AuthState>((set) => ({
  authenticated: !!getApiKey(),
  loading: false,
  error: '',
  loginKey: '',

  setLoginKey: (key) => set({ loginKey: key }),

  login: async (key) => {
    if (!key.trim()) return
    set({ loading: true, error: '' })
    try {
      const res = await fetch('/admin/accounts', {
        headers: { Authorization: `Bearer ${key.trim()}` },
      })
      if (res.status === 401) {
        set({ error: 'Invalid API key', loading: false })
        return
      }
      if (!res.ok) {
        set({ error: `Connection failed: ${res.status}`, loading: false })
        return
      }
      setApiKey(key.trim())
      set({ authenticated: true, loading: false, loginKey: '' })
    } catch (e) {
      set({ error: `Network error: ${(e as Error).message}`, loading: false })
    }
  },

  logout: () => {
    removeApiKey()
    set({ authenticated: false, loginKey: '', error: '' })
  },

  checkAuth: () => {
    set({ authenticated: !!getApiKey() })
  },
}))

export function handleApiError(err: unknown): string {
  if (err instanceof UnauthorizedError) {
    useAuthStore.getState().logout()
    return 'Session expired. Please log in again.'
  }
  if (err instanceof Error) return err.message
  return String(err)
}
