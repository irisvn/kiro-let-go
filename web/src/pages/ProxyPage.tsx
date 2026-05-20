import { useEffect, useRef, useState } from 'react'
import { useProxyStore, type LogEntry } from '@/store/proxy'
import { useToastStore } from '@/store/toast'
import { formatShortTime, formatDuration, formatTokens, formatAccount } from '@/lib/utils'

export function ProxyPage() {
  const config = useProxyStore((s) => s.config)
  const log = useProxyStore((s) => s.log)
  const roundRobinResult = useProxyStore((s) => s.roundRobinResult)
  const roundRobinLoading = useProxyStore((s) => s.roundRobinLoading)
  const roundRobinCount = useProxyStore((s) => s.roundRobinCount)
  const loadProxyConfig = useProxyStore((s) => s.loadProxyConfig)
  const loadProxyLog = useProxyStore((s) => s.loadProxyLog)
  const testRoundRobin = useProxyStore((s) => s.testRoundRobin)
  const setRoundRobinCount = useProxyStore((s) => s.setRoundRobinCount)
  const clearLog = useProxyStore((s) => s.clearLog)
  const toast = useToastStore((s) => s.addToast)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const [logDetailEntry, setLogDetailEntry] = useState<LogEntry | null>(null)

  useEffect(() => {
    loadProxyConfig().catch((e) => toast(e.message, 'error'))
    loadProxyLog().catch((e) => toast(e.message, 'error'))
    intervalRef.current = setInterval(() => {
      loadProxyLog().catch(() => {})
    }, 10000)
    return () => { if (intervalRef.current) clearInterval(intervalRef.current) }
  }, [loadProxyConfig, loadProxyLog, toast])

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
      await Promise.all([loadProxyConfig(), loadProxyLog()])
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

      <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden">
        <div className="flex items-center justify-between p-5 border-b border-slate-800">
          <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Recent Requests</h3>
          <div className="flex gap-3">
            <button onClick={() => loadProxyLog()} className="text-indigo-400 hover:text-indigo-300 text-xs font-medium">Refresh</button>
            <button onClick={clearLog} className="text-slate-400 hover:text-white text-xs font-medium">Clear</button>
          </div>
        </div>
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-slate-800 text-slate-400 text-left">
              <th className="px-4 py-3 font-medium">Time</th>
              <th className="px-4 py-3 font-medium">Method</th>
              <th className="px-4 py-3 font-medium">Path</th>
              <th className="px-4 py-3 font-medium">Model</th>
              <th className="px-4 py-3 font-medium">Tokens</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Duration</th>
              <th className="px-4 py-3 font-medium">Account</th>
              <th className="px-4 py-3 font-medium text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {log.map((entry) => (
              <tr key={entry.id} className="border-b border-slate-800/50 hover:bg-slate-800/30">
                <td className="px-4 py-3 text-slate-400 font-mono text-xs">{formatShortTime(entry.timestamp)}</td>
                <td className="px-4 py-3 text-emerald-400 font-mono">{entry.method}</td>
                <td className="px-4 py-3 text-slate-200 font-mono text-xs">{entry.path}</td>
                <td className="px-4 py-3 text-slate-200 font-mono text-xs max-w-[14rem] truncate">{entry.model || '-'}</td>
                <td className="px-4 py-3 text-slate-300 font-mono">{formatTokens(entry)}</td>
                <td className={`px-4 py-3 font-mono ${entry.status >= 400 ? 'text-red-400' : 'text-emerald-400'}`}>{entry.status}</td>
                <td className="px-4 py-3 text-slate-300 font-mono">{formatDuration(entry.duration_ms)}</td>
                <td className="px-4 py-3 text-slate-400 max-w-[10rem] truncate">{formatAccount(entry)}</td>
                <td className="px-4 py-3 text-right">
                  <button onClick={() => setLogDetailEntry(entry)} className="text-indigo-400 hover:text-indigo-300 text-xs font-medium">Detail</button>
                </td>
              </tr>
            ))}
            {log.length === 0 && (
              <tr><td colSpan={9} className="px-4 py-8 text-center text-slate-500">No proxy requests logged</td></tr>
            )}
          </tbody>
        </table>
      </div>

      {logDetailEntry && (
        <div className="fixed inset-0 z-40 flex items-center justify-center" onClick={() => setLogDetailEntry(null)}>
          <div className="fixed inset-0 bg-black/60" />
          <div className="relative bg-slate-900 border border-slate-700 rounded-xl shadow-2xl w-full max-w-3xl mx-4 p-6 z-50 max-h-[90vh] overflow-y-auto" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-white">Request Detail</h3>
              <button onClick={() => setLogDetailEntry(null)} className="text-slate-400 hover:text-white text-sm">Close</button>
            </div>
            <div className="space-y-4">
              <div className="grid grid-cols-2 md:grid-cols-3 gap-x-6 gap-y-3 text-sm">
                {[
                  { label: 'ID', value: logDetailEntry.id },
                  { label: 'Timestamp', value: new Date(logDetailEntry.timestamp).toLocaleString() },
                  { label: 'Method', value: logDetailEntry.method },
                  { label: 'Path', value: logDetailEntry.path },
                  { label: 'Model', value: logDetailEntry.model || '-' },
                  { label: 'Format', value: logDetailEntry.format || '-' },
                  { label: 'Stream', value: logDetailEntry.stream ? 'true' : 'false' },
                  { label: 'Status', value: String(logDetailEntry.status) },
                  { label: 'Duration', value: formatDuration(logDetailEntry.duration_ms) },
                  { label: 'Client IP', value: logDetailEntry.client_ip || '-' },
                  { label: 'Account', value: formatAccount(logDetailEntry) },
                  { label: 'Account ID', value: logDetailEntry.account_id || '-' },
                  { label: 'Input Tokens', value: String(logDetailEntry.input_tokens || 0) },
                  { label: 'Output Tokens', value: String(logDetailEntry.output_tokens || 0) },
                  { label: 'User Agent', value: logDetailEntry.user_agent || '-' },
                ].map((f) => (
                  <div key={f.label}>
                    <span className="text-slate-500 text-xs block">{f.label}</span>
                    <span className="text-slate-200 font-mono text-xs break-all">{f.value}</span>
                  </div>
                ))}
              </div>
              {logDetailEntry.error && (
                <div>
                  <span className="text-slate-500 text-xs block mb-1">Error</span>
                  <pre className="bg-red-950/30 border border-red-900/50 rounded-lg p-3 text-red-300 text-xs overflow-auto">{logDetailEntry.error}</pre>
                </div>
              )}
              <div>
                <span className="text-slate-500 text-xs block mb-1">Request Body (truncated)</span>
                <pre className="bg-slate-950 border border-slate-800 rounded-lg p-3 text-slate-300 text-xs overflow-auto max-h-48">{logDetailEntry.request_body || '-'}</pre>
              </div>
              <div>
                <span className="text-slate-500 text-xs block mb-1">Response Snippet</span>
                <pre className="bg-slate-950 border border-slate-800 rounded-lg p-3 text-slate-300 text-xs overflow-auto max-h-48">{logDetailEntry.response_snippet || '-'}</pre>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
