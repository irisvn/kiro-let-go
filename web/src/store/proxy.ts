import { create } from 'zustand'
import { apiCall } from '@/lib/api'
import { handleApiError } from '@/store/auth'

export interface ProxyEndpoint {
  method: string
  path: string
  description: string
  format: string
}

export interface ProxyConfig {
  endpoints: ProxyEndpoint[]
  load_balancer_strategy: string
  sticky_session: boolean
  max_attempts: number
  base_cooldown_sec: number
  enabled_accounts: number
  total_accounts: number
  quota_cache_ttl_seconds: number
  host: string
  port: number
  probabilistic_retry_chance: number
}

export interface ApiTestResult {
  success: boolean
  format: string
  model: string
  response: string
  duration_ms: number
  input_tokens: number
  output_tokens: number
  account_id?: string
  account_label?: string
  error?: string
}

export interface LogEntry {
  id: string
  timestamp: string
  method: string
  path: string
  model: string
  format: string
  stream: boolean
  status: number
  duration_ms: number
  client_ip: string
  account_id: string
  account_label: string
  input_tokens: number
  output_tokens: number
  user_agent: string
  error: string
  request_body: string
  response_snippet: string
}

interface ProxyState {
  config: ProxyConfig | null
  log: LogEntry[]
  apiTestFormat: 'anthropic' | 'openai'
  apiTestModel: string
  apiTestMessage: string
  apiTestLoading: boolean
  apiTestResult: ApiTestResult | null
  loadProxyConfig: () => Promise<void>
  loadProxyLog: () => Promise<void>
  testProxyAPI: () => Promise<void>
  setApiTestFormat: (format: 'anthropic' | 'openai') => void
  setApiTestModel: (model: string) => void
  setApiTestMessage: (message: string) => void
  clearLog: () => void
}

export const useProxyStore = create<ProxyState>((set, get) => ({
  config: null,
  log: [],
  apiTestFormat: 'anthropic',
  apiTestModel: 'claude-haiku-4.5',
  apiTestMessage: 'Hi',
  apiTestLoading: false,
  apiTestResult: null,

  loadProxyConfig: async () => {
    try {
      const config = await apiCall<ProxyConfig>('GET', '/admin/proxy/config')
      set({ config })
    } catch (e) {
      throw new Error('Failed to load proxy config: ' + handleApiError(e))
    }
  },

  loadProxyLog: async () => {
    try {
      const log = await apiCall<LogEntry[]>('GET', '/admin/proxy/log?limit=50')
      set({ log: log || [] })
    } catch (e) {
      throw new Error('Failed to load proxy log: ' + handleApiError(e))
    }
  },

  testProxyAPI: async () => {
    set({ apiTestLoading: true, apiTestResult: null })
    try {
      const result = await apiCall<ApiTestResult>('POST', '/admin/proxy/test-api', {
        format: get().apiTestFormat,
        model: get().apiTestModel,
        message: get().apiTestMessage,
      })
      set({ apiTestResult: result })
    } catch (e) {
      set({ apiTestResult: { success: false, format: get().apiTestFormat, model: get().apiTestModel, response: '', duration_ms: 0, input_tokens: 0, output_tokens: 0, error: handleApiError(e) } })
    } finally {
      set({ apiTestLoading: false })
    }
  },

  setApiTestFormat: (format) => set({ apiTestFormat: format }),
  setApiTestModel: (model) => set({ apiTestModel: model }),
  setApiTestMessage: (message) => set({ apiTestMessage: message }),
  clearLog: () => set({ log: [] }),
}))