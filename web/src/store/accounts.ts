import { create } from 'zustand'
import { apiCall } from '@/lib/api'
import { handleApiError } from '@/store/auth'

export interface Account {
  id: string
  label: string
  auth_method: string
  region: string
  enabled: boolean
  disabled_reason: string
  failure_count: number
  success_count: number
  last_used_at: string | null
  last_failure_at: string | null
  access_token?: string
  refresh_token?: string
  api_key?: string
  machine_id?: string
  profile_arn?: string
  proxy_url?: string
  expires_at?: string
  created_at?: string
  updated_at?: string
}

export interface CircuitBreaker {
  state: string
  open: boolean
  failures: number
  last_reason: string
}

export interface AccountDetail {
  account: Account
  circuit_breaker: CircuitBreaker
}

export interface ModelInfo {
  model_id: string
  model_name: string
  is_default: boolean
  rate_multiplier: number | null
  rate_unit: string
  supported_input_types: string[]
  token_limits: {
    max_input_tokens: number
    max_output_tokens: number
  }
}

export interface ModelsResult {
  models: ModelInfo[]
  default_model?: { model_id: string }
}

export interface TestResult {
  status: string
  message: string
  duration_ms: number
  subscription_title?: string
  user_id?: string
}

export interface ChatTestResult {
  success: boolean
  response?: string
  error?: string
  model?: string
  duration_ms?: number
}

interface AccountsState {
  accounts: Account[]
  detailAccount: AccountDetail | null
  models: ModelsResult | null
  modelsLoading: boolean
  testLoading: boolean
  testResult: TestResult | null
  chatTestModel: string
  chatTestMessage: string
  chatTestLoading: boolean
  chatTestResult: ChatTestResult | null
  actionLoading: boolean
  loadAccounts: () => Promise<void>
  openDetail: (id: string) => Promise<void>
  loadAccountModels: (accountId: string) => Promise<void>
  testAccount: (accountId: string) => Promise<void>
  sendChatTest: (accountId: string) => Promise<void>
  toggleEnabled: (acc: Account) => Promise<{ needDisable: boolean; accountId: string } | void>
  submitDisable: (accountId: string, reason: string) => Promise<void>
  deleteAccount: (id: string) => Promise<void>
  addAccount: (form: AddAccountForm) => Promise<{ verified?: boolean; verification_error?: string } | null>
  editAccount: (form: EditAccountForm) => Promise<void>
  forceRefresh: (id: string) => Promise<void>
  setChatTestModel: (model: string) => void
  setChatTestMessage: (message: string) => void
  clearDetail: () => void
}

export interface AddAccountForm {
  label: string
  auth_method: string
  refresh_token: string
  api_key: string
  profile_arn: string
  region: string
  proxy_url: string
}

export interface EditAccountForm {
  id: string
  label: string
  region: string
  proxy_url: string
}

export const useAccountsStore = create<AccountsState>((set, get) => ({
  accounts: [],
  detailAccount: null,
  models: null,
  modelsLoading: false,
  testLoading: false,
  testResult: null,
  chatTestModel: 'claude-haiku-4.5',
  chatTestMessage: 'Hi',
  chatTestLoading: false,
  chatTestResult: null,
  actionLoading: false,

  loadAccounts: async () => {
    try {
      const accounts = await apiCall<Account[]>('GET', '/admin/accounts')
      set({ accounts: accounts || [] })
    } catch (e) {
      throw new Error('Failed to load accounts: ' + handleApiError(e))
    }
  },

  openDetail: async (id) => {
    try {
      const detail = await apiCall<AccountDetail>('GET', `/admin/accounts/${id}`)
      set({
        detailAccount: detail,
        models: null,
        modelsLoading: false,
        testLoading: false,
        testResult: null,
        chatTestModel: 'claude-haiku-4.5',
        chatTestMessage: 'Hi',
        chatTestLoading: false,
        chatTestResult: null,
      })
      await get().loadAccountModels(id)
    } catch (e) {
      throw new Error('Failed to load account: ' + handleApiError(e))
    }
  },

  loadAccountModels: async (accountId) => {
    set({ modelsLoading: true })
    try {
      const result = await apiCall<ModelsResult>('GET', `/admin/accounts/${accountId}/models`)
      const modelsResult = result || { models: [] }
      set({ models: modelsResult })
      const defaultModel = modelsResult.default_model?.model_id
        || modelsResult.models.find(m => m.is_default)?.model_id
        || modelsResult.models[0]?.model_id
        || 'claude-haiku-4.5'
      set({ chatTestModel: defaultModel })
    } catch (e) {
      throw new Error('Failed to load models: ' + handleApiError(e))
    } finally {
      set({ modelsLoading: false })
    }
  },

  testAccount: async (accountId) => {
    set({ testLoading: true, testResult: null })
    try {
      const result = await apiCall<TestResult>('POST', `/admin/accounts/${accountId}/test`)
      set({ testResult: result })
    } catch (e) {
      throw new Error('Test failed: ' + handleApiError(e))
    } finally {
      set({ testLoading: false })
    }
  },

  sendChatTest: async (accountId) => {
    const { chatTestModel, chatTestMessage } = get()
    set({ chatTestLoading: true, chatTestResult: null })
    try {
      const result = await apiCall<ChatTestResult>('POST', `/admin/accounts/${accountId}/chat-test`, {
        model: chatTestModel,
        message: chatTestMessage,
      })
      set({ chatTestResult: result })
    } catch (e) {
      set({ chatTestResult: { success: false, error: handleApiError(e) } })
      throw e
    } finally {
      set({ chatTestLoading: false })
    }
  },

  toggleEnabled: async (acc) => {
    if (acc.enabled) {
      return { needDisable: true, accountId: acc.id }
    }
    try {
      await apiCall('PATCH', `/admin/accounts/${acc.id}`, { enabled: true })
    } catch (e) {
      throw new Error('Failed to enable: ' + handleApiError(e))
    }
  },

  submitDisable: async (accountId, reason) => {
    try {
      await apiCall('PATCH', `/admin/accounts/${accountId}`, {
        enabled: false,
        disabled_reason: reason || 'Disabled via admin UI',
      })
    } catch (e) {
      throw new Error('Failed to disable: ' + handleApiError(e))
    }
  },

  deleteAccount: async (id) => {
    try {
      await apiCall('DELETE', `/admin/accounts/${id}`)
    } catch (e) {
      throw new Error('Failed to delete: ' + handleApiError(e))
    }
  },

  addAccount: async (form) => {
    const payload: Record<string, unknown> = {
      label: form.label,
      auth_method: form.auth_method,
      region: form.region,
      enabled: true,
    }
    if (form.auth_method === 'social') {
      payload.refresh_token = form.refresh_token
    } else {
      payload.api_key = form.api_key
    }
    if (form.profile_arn) payload.profile_arn = form.profile_arn
    if (form.proxy_url) payload.proxy_url = form.proxy_url
    try {
      return await apiCall<{ verified?: boolean; verification_error?: string }>('POST', '/admin/accounts', payload)
    } catch (e) {
      throw new Error('Failed to create: ' + handleApiError(e))
    }
  },

  editAccount: async (form) => {
    const payload: Record<string, unknown> = {}
    if (form.label) payload.label = form.label
    if (form.region) payload.region = form.region
    if (form.proxy_url) payload.proxy_url = form.proxy_url
    try {
      await apiCall('PATCH', `/admin/accounts/${form.id}`, payload)
    } catch (e) {
      throw new Error('Failed to update: ' + handleApiError(e))
    }
  },

  forceRefresh: async (id) => {
    set({ actionLoading: true })
    try {
      await apiCall('POST', `/admin/accounts/${id}/refresh`)
      const detail = await apiCall<AccountDetail>('GET', `/admin/accounts/${id}`)
      set({ detailAccount: detail })
    } catch (e) {
      throw new Error('Refresh failed: ' + handleApiError(e))
    } finally {
      set({ actionLoading: false })
    }
  },

  setChatTestModel: (model) => set({ chatTestModel: model }),
  setChatTestMessage: (message) => set({ chatTestMessage: message }),
  clearDetail: () => set({ detailAccount: null, models: null, testResult: null, chatTestResult: null }),
}))
