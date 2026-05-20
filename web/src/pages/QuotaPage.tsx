import { useEffect, useRef } from 'react'
import { useQuotaStore } from '@/store/quota'
import { useToastStore } from '@/store/toast'
import { formatTime } from '@/lib/utils'

export function QuotaPage() {
  const quotas = useQuotaStore((s) => s.quotas)
  const loading = useQuotaStore((s) => s.loading)
  const refreshing = useQuotaStore((s) => s.refreshing)
  const loadQuota = useQuotaStore((s) => s.loadQuota)
  const refreshQuota = useQuotaStore((s) => s.refreshQuota)
  const refreshAllQuota = useQuotaStore((s) => s.refreshAllQuota)
  const toast = useToastStore((s) => s.addToast)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    loadQuota().catch((e) => toast(e.message, 'error'))
    intervalRef.current = setInterval(() => {
      const { quotas: qs, refreshQuota: rq } = useQuotaStore.getState()
      for (const q of qs) rq(q.account_id)
    }, 30 * 60 * 1000)
    return () => { if (intervalRef.current) clearInterval(intervalRef.current) }
  }, [loadQuota, toast])

  const handleRefreshAll = async () => {
    try {
      await refreshAllQuota()
      toast('All quotas refreshed', 'success')
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  const handleRefreshOne = async (accountId: string) => {
    try {
      await refreshQuota(accountId)
      toast('Quota refreshed', 'success')
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  return (
    <div className="animate-fade-in">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-xl font-semibold text-white">Quota Dashboard</h2>
        <button
          onClick={handleRefreshAll}
          disabled={loading}
          className="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors flex items-center gap-2"
        >
          {loading && <span className="spinner" />}Refresh All
        </button>
      </div>

      <div className={`grid gap-4 ${quotas.length > 1 ? 'md:grid-cols-2' : ''}`}>
        {quotas.map((q) => {
          const pct = q.limit_total && q.limit_total > 0 ? ((q.current_usage || 0) / q.limit_total) * 100 : 0
          const barColor = pct >= 90 ? 'bg-red-500' : pct >= 70 ? 'bg-amber-500' : 'bg-emerald-500'
          const daysLeft = q.reset_time ? Math.max(0, Math.ceil((new Date(q.reset_time).getTime() - Date.now()) / 86400000)) : null

          return (
            <div key={q.account_id} className="bg-slate-900 border border-slate-800 rounded-xl p-5 hover:border-slate-700 transition-colors">
              <div className="flex items-center justify-between mb-3">
                <div>
                  <h3 className="text-white font-medium text-sm">{q.label}</h3>
                  <span className={`text-xs px-2 py-0.5 rounded-full mt-1 inline-block ${q.subscription_title ? 'bg-indigo-500/15 text-indigo-400 border border-indigo-500/30' : 'bg-slate-700 text-slate-400'}`}>
                    {q.subscription_title || 'Unknown Plan'}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  {q.stale && <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-amber-500/15 text-amber-400 border border-amber-500/30">Stale</span>}
                  <button
                    onClick={() => handleRefreshOne(q.account_id)}
                    disabled={refreshing[q.account_id]}
                    className="text-indigo-400 hover:text-indigo-300 disabled:opacity-50 text-xs font-medium transition-colors flex items-center gap-1"
                  >
                    {refreshing[q.account_id] && <span className="spinner spinner-sm" />}
                    {refreshing[q.account_id] ? '' : 'Refresh'}
                  </button>
                </div>
              </div>

              {q.limit_total != null && q.limit_total > 0 ? (
                <div className="mt-3">
                  <div className="flex items-baseline justify-between mb-1.5">
                    <span className="text-xs text-slate-400">Credits Used</span>
                    <span className="text-sm font-mono">
                      <span className="text-white">{(q.current_usage || 0).toLocaleString()}</span>
                      <span className="text-slate-500"> / </span>
                      <span className="text-slate-300">{q.limit_total.toLocaleString()}</span>
                    </span>
                  </div>
                  <div className="w-full h-2.5 bg-slate-800 rounded-full overflow-hidden">
                    <div className={`h-full rounded-full transition-all duration-500 ${barColor}`} style={{ width: `${Math.min(pct, 100)}%` }} />
                  </div>
                  <div className="flex items-center justify-between mt-1.5">
                    <span className="text-xs text-slate-500">{Math.round(pct)}% used</span>
                    <span className={`text-xs font-mono ${q.limit_remaining != null && q.limit_remaining < 100 ? 'text-amber-400' : 'text-emerald-400'}`}>
                      {(q.limit_remaining ?? 0).toLocaleString()} remaining
                    </span>
                  </div>
                </div>
              ) : (
                <div className="mt-3 text-center py-4">
                  <p className="text-slate-500 text-xs">No quota data cached</p>
                  <button onClick={() => handleRefreshOne(q.account_id)} disabled={refreshing[q.account_id]} className="mt-2 text-indigo-400 hover:text-indigo-300 text-xs font-medium">
                    Click Refresh to fetch
                  </button>
                </div>
              )}

              {q.overage_cap != null && q.overage_cap > 0 && (
                <div className="mt-3 pt-3 border-t border-slate-800">
                  <div className="grid grid-cols-2 gap-2 text-xs">
                    <div><span className="text-slate-500">Overage Cap</span><span className="text-slate-300 font-mono block">{q.overage_cap.toLocaleString()} credits</span></div>
                    <div><span className="text-slate-500">Overage Rate</span><span className="text-slate-300 font-mono block">{q.overage_rate != null ? `$${q.overage_rate.toFixed(2)}/${q.currency || 'credit'}` : '-'}</span></div>
                  </div>
                </div>
              )}

              {q.reset_time && (
                <div className="mt-3 pt-3 border-t border-slate-800">
                  <div className="grid grid-cols-2 gap-2 text-xs">
                    <div><span className="text-slate-500">Resets On</span><span className="text-slate-300 block">{new Date(q.reset_time).toLocaleDateString()}</span></div>
                    <div><span className="text-slate-500">Days Remaining</span><span className={`font-mono block ${daysLeft != null && daysLeft <= 3 ? 'text-amber-400' : 'text-slate-300'}`}>{daysLeft} days</span></div>
                  </div>
                </div>
              )}

              {q.fetched_at && (
                <div className="mt-3 pt-2 border-t border-slate-800/50">
                  <span className="text-xs text-slate-600">Fetched: {formatTime(q.fetched_at)}</span>
                </div>
              )}
            </div>
          )
        })}
      </div>

      {quotas.length === 0 && (
        <div className="bg-slate-900 border border-slate-800 rounded-xl p-8 text-center">
          <p className="text-slate-500">No accounts found. Add an account first.</p>
        </div>
      )}
    </div>
  )
}
