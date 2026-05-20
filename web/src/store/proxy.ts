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

export interface RoundRobinResult {
  results: { attempt: number; account_id: string; account_label: string; success: boolean; error?: string }[]
  summary: Record<string, number>
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
  roundRobinResult: RoundRobinResult | null
  roundRobinLoading: boolean
  roundRobinCount: number
  loadProxyConfig: () => Promise<void>
  loadProxyLog: () => Promise<void>
  testRoundRobin: () => Promise<void>
  setRoundRobinCount: (count: number) => void
  clearLog: () => void
}

export const useProxyStore = create<ProxyState>((set, get) => ({
  config: null,
  log: [],
  roundRobinResult: null,
  roundRobinLoading: false,
  roundRobinCount: 5,

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

  testRoundRobin: async () => {
    set({ roundRobinLoading: true, roundRobinResult: null })
    try {
      const result = await apiCall<RoundRobinResult>('POST', '/admin/proxy/test-roundrobin', {
        count: get().roundRobinCount,
      })
      set({ roundRobinResult: result })
      await get().loadProxyConfig()
    } catch (e) {
      throw new Error('Round-robin test failed: ' + handleApiError(e))
    } finally {
      set({ roundRobinLoading: false })
    }
  },

  setRoundRobinCount: (count) => set({ roundRobinCount: count }),
  clearLog: () => set({ log: [] }),
}))
