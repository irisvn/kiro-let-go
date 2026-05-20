import { useEffect, useRef, useState } from 'react'
import { useProxyStore, type LogEntry } from '@/store/proxy'
import { useToastStore } from '@/store/toast'
import { formatShortTime, formatDuration, formatTokens, formatAccount } from '@/lib/utils'

export function RequestLogPage() {
  const log = useProxyStore((s) => s.log)
  const loadProxyLog = useProxyStore((s) => s.loadProxyLog)
  const clearLog = useProxyStore((s) => s.clearLog)
  const toast = useToastStore((s) => s.addToast)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const [logDetailEntry, setLogDetailEntry] = useState<LogEntry | null>(null)

  useEffect(() => {
    loadProxyLog().catch((e) => toast(e.message, 'error'))
    intervalRef.current = setInterval(() => {
      loadProxyLog().catch(() => {})
    }, 10000)
    return () => { if (intervalRef.current) clearInterval(intervalRef.current) }
  }, [loadProxyLog, toast])

  const handleRefresh = async () => {
    try {
      await loadProxyLog()
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  return (
    <div className="animate-fade-in space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-white">Request Log</h2>
        <div className="flex gap-3">
          <button onClick={handleRefresh} className="bg-slate-700 hover:bg-slate-600 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors">Refresh</button>
          <button onClick={clearLog} className="bg-slate-700 hover:bg-red-600/80 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors">Clear</button>
        </div>
      </div>

      <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden">
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
                  { label: 'Account Label', value: logDetailEntry.account_label || '-' },
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
              {logDetailEntry.kiro_payload && (
                <div>
                  <span className="text-slate-500 text-xs block mb-1">Kiro Payload (truncated)</span>
                  <pre className="bg-slate-950 border border-slate-800 rounded-lg p-3 text-slate-300 text-xs overflow-auto max-h-64">{formatJSON(logDetailEntry.kiro_payload)}</pre>
                </div>
              )}
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

function formatJSON(value: string) {
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}
