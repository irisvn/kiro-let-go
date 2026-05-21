import { create } from 'zustand'
import { apiCall } from '@/lib/api'
import { handleApiError } from '@/store/auth'

export interface ModelMappingRule {
  id?: string
  name: string
  enabled: boolean
  rule_type: 'replace' | 'alias' | 'loadbalance' | string
  source_model: string
  target_models: string[]
  weights?: number[]
}

export interface DynamicSettings {
  strategy: 'round_robin' | 'balanced' | 'most_quota' | string
  sticky_session: boolean
  base_cooldown_sec: number
  max_backoff_multiplier: number
  probabilistic_retry_chance: number
  max_attempts: number
  cache_ttl_seconds: number
  model_mappings: ModelMappingRule[]
}

interface SettingsState {
  settings: DynamicSettings | null
  loading: boolean
  saving: boolean
  loadSettings: () => Promise<void>
  saveSettings: (settings: DynamicSettings) => Promise<void>
}

export const useSettingsStore = create<SettingsState>((set) => ({
  settings: null,
  loading: false,
  saving: false,

  loadSettings: async () => {
    set({ loading: true })
    try {
      const settings = await apiCall<DynamicSettings>('GET', '/admin/settings')
      set({ settings })
    } catch (e) {
      throw new Error('Failed to load settings: ' + handleApiError(e))
    } finally {
      set({ loading: false })
    }
  },

  saveSettings: async (settings) => {
    set({ saving: true })
    try {
      const saved = await apiCall<DynamicSettings>('PUT', '/admin/settings', settings)
      set({ settings: saved })
    } catch (e) {
      throw new Error('Failed to save settings: ' + handleApiError(e))
    } finally {
      set({ saving: false })
    }
  },
}))
