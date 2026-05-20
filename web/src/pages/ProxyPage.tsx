import { useEffect } from 'react'
import { useProxyStore } from '@/store/proxy'
import { useToastStore } from '@/store/toast'

export function ProxyPage() {
  const config = useProxyStore((s) => s.config)
  const roundRobinResult = useProxyStore((s) => s.roundRobinResult)
  const roundRobinLoading = useProxyStore((s) => s.roundRobinLoading)
  const roundRobinCount = useProxyStore((s) => s.roundRobinCount)
  const loadProxyConfig = useProxyStore((s) => s.loadProxyConfig)
  const testRoundRobin = useProxyStore((s) => s.testRoundRobin)
  const setRoundRobinCount = useProxyStore((s) => s.setRoundRobinCount)
  const toast = useToastStore((s) => s.addToast)

  useEffect(() => {
    loadProxyConfig().catch((e) => toast(e.message, 'error'))
  }, [loadProxyConfig, toast])

  const handleRoundRobin = async () => {
    try {
      await testRoundRobin()
      toast('Round-robin test completed', 'success')
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
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Round-Robin Test</h3>
          <div className="flex items-center gap-2">
            <input
              type="number" min={1} max={20}
              value={roundRobinCount}
              onChange={(e) => setRoundRobinCount(Number(e.target.value))}
              className="w-16 bg-slate-800 border border-slate-700 rounded px-2 py-1 text-sm text-white"
            />
            <button onClick={handleRoundRobin} disabled={roundRobinLoading} className="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors flex items-center gap-2">
              {roundRobinLoading && <span className="spinner" />}Run Test
            </button>
          </div>
        </div>
        {roundRobinResult ? (
          <div className="space-y-2 text-sm">
            {roundRobinResult.results.map((r) => (
              <div key={r.attempt} className="flex items-center gap-2 font-mono">
                <span className="text-slate-500">#{r.attempt}</span>
                <span className="text-slate-600">→</span>
                <span className="text-slate-200">{r.account_label || r.account_id || '-'}</span>
                <span className={r.success ? 'text-emerald-400' : 'text-red-400'}>{r.success ? '✓' : '✕'}</span>
                {r.error && <span className="text-red-400 text-xs">{r.error}</span>}
              </div>
            ))}
            <div className="pt-2 border-t border-slate-800 text-slate-400">
              Summary: <span className="font-mono text-slate-200">{Object.entries(roundRobinResult.summary || {}).map(([k, v]) => `${k}: ${v}`).join(', ') || '-'}</span>
            </div>
          </div>
        ) : (
          <p className="text-slate-500 text-sm">Acquire + release only; no upstream chat requests are sent.</p>
        )}
      </div>
    </div>
  )
}