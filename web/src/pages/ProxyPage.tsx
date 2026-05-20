import { useEffect } from 'react'
import { useProxyStore } from '@/store/proxy'
import { useToastStore } from '@/store/toast'

const MODELS = [
  'claude-haiku-4.5',
  'claude-sonnet-4.5',
  'claude-sonnet-4.6',
  'claude-opus-4.5',
  'claude-opus-4.6',
  'claude-opus-4.7',
]

export function ProxyPage() {
  const config = useProxyStore((s) => s.config)
  const apiTestFormat = useProxyStore((s) => s.apiTestFormat)
  const apiTestModel = useProxyStore((s) => s.apiTestModel)
  const apiTestMessage = useProxyStore((s) => s.apiTestMessage)
  const apiTestLoading = useProxyStore((s) => s.apiTestLoading)
  const apiTestResult = useProxyStore((s) => s.apiTestResult)
  const loadProxyConfig = useProxyStore((s) => s.loadProxyConfig)
  const testProxyAPI = useProxyStore((s) => s.testProxyAPI)
  const setApiTestFormat = useProxyStore((s) => s.setApiTestFormat)
  const setApiTestModel = useProxyStore((s) => s.setApiTestModel)
  const setApiTestMessage = useProxyStore((s) => s.setApiTestMessage)
  const toast = useToastStore((s) => s.addToast)

  useEffect(() => {
    loadProxyConfig().catch((e) => toast(e.message, 'error'))
  }, [loadProxyConfig, toast])

  const handleTest = async () => {
    try {
      await testProxyAPI()
      toast('API test completed', 'success')
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  const handleRefresh = async () => {
    try {
      await loadProxyConfig()
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  const anthropicEndpoints = config?.endpoints?.filter(e => e.format === 'anthropic') || []
  const openaiEndpoints = config?.endpoints?.filter(e => e.format === 'openai') || []

  return (
    <div className="animate-fade-in space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-white">Proxy</h2>
        <button onClick={handleRefresh} className="bg-slate-700 hover:bg-slate-600 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors">Refresh</button>
      </div>

      <div className="bg-slate-900 border border-slate-800 rounded-xl p-5">
        <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider mb-3">API Endpoints</h3>
        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <h4 className="text-indigo-400 font-medium mb-2">Anthropic API</h4>
            {anthropicEndpoints.map((ep) => (
              <div key={ep.path} className="flex gap-3 text-sm py-1">
                <span className="font-mono text-emerald-400 w-12">{ep.method}</span>
                <span className="font-mono text-slate-200">{ep.path}</span>
                <span className="text-slate-500">{ep.description}</span>
              </div>
            ))}
          </div>
          <div>
            <h4 className="text-indigo-400 font-medium mb-2">OpenAI API</h4>
            {openaiEndpoints.map((ep) => (
              <div key={ep.path} className="flex gap-3 text-sm py-1">
                <span className="font-mono text-emerald-400 w-12">{ep.method}</span>
                <span className="font-mono text-slate-200">{ep.path}</span>
                <span className="text-slate-500">{ep.description}</span>
              </div>
            ))}
          </div>
        </div>
        <div className="mt-4 pt-3 border-t border-slate-800 text-sm text-slate-400">
          Auth: <span className="font-mono text-slate-200">Authorization: Bearer &lt;proxy_api_key&gt;</span>
          <span className="mx-2 text-slate-600">or</span>
          <span className="font-mono text-slate-200">x-api-key: &lt;proxy_api_key&gt;</span>
        </div>
      </div>

      {config && (
        <div className="bg-slate-900 border border-slate-800 rounded-xl p-5">
          <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider mb-3">Configuration</h3>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
            <div><span className="text-slate-500 text-xs block">Strategy</span><span className="text-slate-200 font-mono">{config.load_balancer_strategy}</span></div>
            <div><span className="text-slate-500 text-xs block">Sticky</span><span className="text-slate-200 font-mono">{config.sticky_session ? 'true' : 'false'}</span></div>
            <div><span className="text-slate-500 text-xs block">Max Attempts</span><span className="text-slate-200 font-mono">{config.max_attempts}</span></div>
            <div><span className="text-slate-500 text-xs block">Cooldown</span><span className="text-slate-200 font-mono">{config.base_cooldown_sec}s</span></div>
            <div><span className="text-slate-500 text-xs block">Accounts</span><span className="text-slate-200 font-mono">{config.enabled_accounts} enabled / {config.total_accounts} total</span></div>
            <div><span className="text-slate-500 text-xs block">Quota TTL</span><span className="text-slate-200 font-mono">{config.quota_cache_ttl_seconds}s</span></div>
            <div><span className="text-slate-500 text-xs block">Bind</span><span className="text-slate-200 font-mono">{config.host}:{config.port}</span></div>
            <div><span className="text-slate-500 text-xs block">Retry Chance</span><span className="text-slate-200 font-mono">{config.probabilistic_retry_chance}</span></div>
          </div>
        </div>
      )}

      <div className="bg-slate-900 border border-slate-800 rounded-xl p-5">
        <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider mb-4">API Test</h3>

        <div className="space-y-4">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-end">
            <div className="flex-1 min-w-0">
              <label className="block text-xs text-slate-500 mb-1.5">Format</label>
              <div className="flex rounded-lg overflow-hidden border border-slate-700">
                <button
                  onClick={() => setApiTestFormat('anthropic')}
                  className={`flex-1 px-4 py-2 text-sm font-medium transition-colors ${
                    apiTestFormat === 'anthropic'
                      ? 'bg-indigo-600 text-white'
                      : 'bg-slate-800 text-slate-400 hover:bg-slate-700 hover:text-slate-200'
                  }`}
                >
                  Anthropic
                </button>
                <button
                  onClick={() => setApiTestFormat('openai')}
                  className={`flex-1 px-4 py-2 text-sm font-medium transition-colors ${
                    apiTestFormat === 'openai'
                      ? 'bg-indigo-600 text-white'
                      : 'bg-slate-800 text-slate-400 hover:bg-slate-700 hover:text-slate-200'
                  }`}
                >
                  OpenAI
                </button>
              </div>
            </div>

            <div className="flex-1 min-w-0">
              <label className="block text-xs text-slate-500 mb-1.5">Model</label>
              <select
                value={apiTestModel}
                onChange={(e) => setApiTestModel(e.target.value)}
                className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-indigo-500"
              >
                {MODELS.map((m) => (
                  <option key={m} value={m}>{m}</option>
                ))}
              </select>
            </div>

            <div className="flex-[2] min-w-0">
              <label className="block text-xs text-slate-500 mb-1.5">Message</label>
              <input
                type="text"
                value={apiTestMessage}
                onChange={(e) => setApiTestMessage(e.target.value)}
                placeholder="Hi"
                className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
              />
            </div>
          </div>

          <button
            onClick={handleTest}
            disabled={apiTestLoading}
            className="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors flex items-center gap-2"
          >
            {apiTestLoading && <span className="spinner" />}
            Send Test
          </button>

          {apiTestResult && (
            <div className={`rounded-lg border p-4 text-sm ${
              apiTestResult.success
                ? 'bg-slate-800/50 border-emerald-500/30'
                : 'bg-slate-800/50 border-red-500/30'
            }`}>
              <div className="flex items-center gap-3 mb-2">
                <span className={apiTestResult.success ? 'text-emerald-400' : 'text-red-400'}>
                  {apiTestResult.success ? '✓' : '✕'} {apiTestResult.success ? 'Success' : 'Failed'}
                </span>
                {apiTestResult.success && (
                  <>
                    <span className="text-slate-500">|</span>
                    <span className="text-slate-400">Model: <span className="text-slate-200 font-mono">{apiTestResult.model}</span></span>
                  </>
                )}
              </div>
              {apiTestResult.success && (
                <div className="flex items-center gap-3 mb-3 text-xs text-slate-400">
                  <span>Duration: <span className="text-slate-200 font-mono">{(apiTestResult.duration_ms / 1000).toFixed(1)}s</span></span>
                  <span className="text-slate-600">|</span>
                  <span>Tokens: <span className="text-slate-200 font-mono">{apiTestResult.input_tokens} in / {apiTestResult.output_tokens} out</span></span>
                  {apiTestResult.account_label && (
                    <>
                      <span className="text-slate-600">|</span>
                      <span>Account: <span className="text-slate-200 font-mono">{apiTestResult.account_label}</span></span>
                    </>
                  )}
                </div>
              )}
              {apiTestResult.success && apiTestResult.response && (
                <div className="bg-slate-900/50 rounded-md p-3 text-slate-200 font-mono text-xs whitespace-pre-wrap break-words">
                  {apiTestResult.response}
                </div>
              )}
              {apiTestResult.error && (
                <div className="text-red-400 text-xs mt-1">{apiTestResult.error}</div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}