import { create } from 'zustand'
import { apiCall } from '@/lib/api'
import { handleApiError } from '@/store/auth'

export interface QuotaInfo {
  account_id: string
  label: string
  subscription_title: string
  limit_total: number | null
  limit_remaining: number | null
  current_usage: number | null
  overage_cap: number | null
  overage_rate: number | null
  currency: string
  reset_time: string | null
  fetched_at: string | null
  stale: boolean
}

interface QuotaState {
  quotas: QuotaInfo[]
  loading: boolean
  refreshing: Record<string, boolean>
  loadQuota: () => Promise<void>
  refreshQuota: (accountId: string) => Promise<void>
  refreshAllQuota: () => Promise<void>
}

export const useQuotaStore = create<QuotaState>((set, get) => ({
  quotas: [],
  loading: false,
  refreshing: {},

  loadQuota: async () => {
    set({ loading: true })
    try {
      const quotas = await apiCall<QuotaInfo[]>('GET', '/admin/quota')
      const refreshing: Record<string, boolean> = {}
      for (const q of quotas || []) {
        refreshing[q.account_id] = false
      }
      set({ quotas: quotas || [], refreshing })
    } catch (e) {
      throw new Error('Failed to load quota: ' + handleApiError(e))
    } finally {
      set({ loading: false })
    }
  },

  refreshQuota: async (accountId) => {
    set((s) => ({ refreshing: { ...s.refreshing, [accountId]: true } }))
    try {
      const result = await apiCall<QuotaInfo>('GET', `/admin/accounts/${accountId}/quota?force=true`)
      if (result) {
        set((s) => ({
          quotas: s.quotas.map(q =>
            q.account_id === accountId
              ? {
                  ...q,
                  subscription_title: result.subscription_title,
                  limit_total: result.limit_total,
                  limit_remaining: result.limit_remaining,
                  current_usage: result.current_usage,
                  overage_cap: result.overage_cap,
                  fetched_at: result.fetched_at,
                  stale: false,
                }
              : q
          ),
        }))
      }
    } catch (e) {
      throw new Error('Failed to refresh quota: ' + handleApiError(e))
    } finally {
      set((s) => ({ refreshing: { ...s.refreshing, [accountId]: false } }))
    }
  },

  refreshAllQuota: async () => {
    set({ loading: true })
    try {
      const { quotas, refreshQuota } = get()
      for (const q of quotas) {
        await refreshQuota(q.account_id)
      }
    } finally {
      set({ loading: false })
    }
  },
}))
