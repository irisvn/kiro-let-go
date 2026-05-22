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
  web_search_enabled: boolean
  first_token_timeout_sec: number
  first_token_max_retries: number
  streaming_read_timeout_sec: number
  truncation_recovery_enabled: boolean
  fake_reasoning_enabled: boolean
  fake_reasoning_max_tokens: number
  fake_reasoning_budget_cap: number
}

export interface AvailableModel {
  model_id: string
  model_name: string
  supported_input_types?: string[]
  token_limits?: {
    max_input_tokens: number
    max_output_tokens: number
  }
}

interface SettingsState {
  settings: DynamicSettings | null
  loading: boolean
  saving: boolean
  availableModels: AvailableModel[]
  loadSettings: () => Promise<void>
  saveSettings: (settings: DynamicSettings) => Promise<void>
  loadAvailableModels: () => Promise<void>
}

export const useSettingsStore = create<SettingsState>((set) => ({
  settings: null,
  loading: false,
  saving: false,
  availableModels: [],

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

  loadAvailableModels: async () => {
    try {
      const result = await apiCall<{ models: AvailableModel[] }>('GET', '/admin/models')
      set({ availableModels: result.models || [] })
    } catch {
      set({
        availableModels: [
          { model_id: 'auto', model_name: 'Auto' },
          { model_id: 'claude-opus-4.7', model_name: 'Claude Opus 4.7' },
          { model_id: 'claude-opus-4.6', model_name: 'Claude Opus 4.6' },
          { model_id: 'claude-opus-4.5', model_name: 'Claude Opus 4.5' },
          { model_id: 'claude-sonnet-4.6', model_name: 'Claude Sonnet 4.6' },
          { model_id: 'claude-sonnet-4.5', model_name: 'Claude Sonnet 4.5' },
          { model_id: 'claude-sonnet-4', model_name: 'Claude Sonnet 4' },
          { model_id: 'claude-haiku-4.5', model_name: 'Claude Haiku 4.5' },
          { model_id: 'deepseek-3.2', model_name: 'Deepseek v3.2' },
          { model_id: 'minimax-m2.5', model_name: 'MiniMax M2.5' },
          { model_id: 'minimax-m2.1', model_name: 'MiniMax M2.1' },
          { model_id: 'glm-5', model_name: 'GLM-5' },
          { model_id: 'qwen3-coder-next', model_name: 'Qwen3 Coder Next' },
        ],
      })
    }
  },
}))
