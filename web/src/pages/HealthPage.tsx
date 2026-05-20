import { useEffect, useRef } from 'react'
import { useHealthStore } from '@/store/health'

export function HealthPage() {
  const status = useHealthStore((s) => s.status)
  const version = useHealthStore((s) => s.version)
  const loadHealth = useHealthStore((s) => s.loadHealth)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    loadHealth()
    intervalRef.current = setInterval(loadHealth, 30000)
    return () => { if (intervalRef.current) clearInterval(intervalRef.current) }
  }, [loadHealth])

  return (
    <div className="animate-fade-in">
      <h2 className="text-xl font-semibold text-white mb-4">Server Health</h2>
      <div className="bg-slate-900 border border-slate-800 rounded-xl p-5">
        <div className="grid grid-cols-2 gap-6 text-sm">
          <div>
            <span className="text-slate-500 text-xs block">Status</span>
            <span className="font-medium text-emerald-400">{status || '-'}</span>
          </div>
          <div>
            <span className="text-slate-500 text-xs block">Version</span>
            <span className="text-slate-200 font-mono">{version || '-'}</span>
          </div>
        </div>
      </div>
      <p className="text-slate-500 text-xs mt-3">Auto-refreshes every 30 seconds</p>
    </div>
  )
}
