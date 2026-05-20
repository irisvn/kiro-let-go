import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuthStore } from '@/store/auth'

export function LoginPage() {
  const [key, setKey] = useState('')
  const loading = useAuthStore((s) => s.loading)
  const error = useAuthStore((s) => s.error)
  const login = useAuthStore((s) => s.login)
  const navigate = useNavigate()

  const handleLogin = async () => {
    await login(key)
    if (useAuthStore.getState().authenticated) {
      navigate('/admin/ui/accounts')
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <div className="inline-flex items-center justify-center w-14 h-14 rounded-xl bg-indigo-600/20 border border-indigo-500/30 mb-4">
            <svg className="w-7 h-7 text-indigo-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 5.25a3 3 0 013 3m3 0a3 3 0 01-3 3m-3-3a3 3 0 00-3 3m3-3V3m0 5.25v3m0 0a3 3 0 01-3 3m3-3h3m-3 0h-3m3 0v3m0-3V15" />
            </svg>
          </div>
          <h1 className="text-2xl font-bold text-white tracking-tight">kiro-let-go</h1>
          <p className="text-slate-400 mt-1 text-sm">Administration Console</p>
        </div>
        <div className="bg-slate-900 border border-slate-800 rounded-xl p-6 shadow-xl">
          <label className="block text-sm font-medium text-slate-300 mb-2">Admin API Key</label>
          <input
            type="password"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleLogin()}
            disabled={loading}
            className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent disabled:opacity-50"
            placeholder="Enter your admin API key"
          />
          {error && <div className="mt-3 text-sm text-red-400">{error}</div>}
          <button
            onClick={handleLogin}
            disabled={loading || !key.trim()}
            className="mt-4 w-full bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed text-white font-medium rounded-lg px-4 py-2.5 text-sm transition-colors flex items-center justify-center gap-2"
          >
            {loading && <span className="spinner" />}
            {loading ? 'Connecting...' : 'Login'}
          </button>
        </div>
      </div>
    </div>
  )
}
