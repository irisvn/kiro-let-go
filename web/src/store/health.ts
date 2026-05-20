import { create } from 'zustand'

interface HealthState {
  status: string
  version: string
  loadHealth: () => Promise<void>
}

export const useHealthStore = create<HealthState>((set) => ({
  status: '',
  version: '',

  loadHealth: async () => {
    try {
      const res = await fetch('/health')
      if (res.ok) {
        const data = await res.json()
        set({ status: data.status || '', version: data.version || '' })
      }
    } catch {
      // intentionally swallowed: health endpoint is best-effort
    }
  },
}))
