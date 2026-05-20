import { Outlet, NavLink, useNavigate } from 'react-router-dom'
import { useAuthStore } from '@/store/auth'

const navItems = [
  { to: '/admin/ui/accounts', label: 'Accounts' },
  { to: '/admin/ui/quota', label: 'Quota' },
  { to: '/admin/ui/proxy', label: 'Proxy' },
  { to: '/admin/ui/logs', label: 'Logs' },
  { to: '/admin/ui/health', label: 'Health' },
]

export function Layout() {
  const logout = useAuthStore((s) => s.logout)
  const navigate = useNavigate()

  const handleLogout = () => {
    logout()
    navigate('/admin/ui/')
  }

  return (
    <div className="min-h-screen flex flex-col">
      <nav className="bg-slate-900 border-b border-slate-800 px-6 py-3 flex items-center justify-between shrink-0">
        <div className="flex items-center gap-6">
          <span className="text-lg font-bold text-white tracking-tight">kiro-let-go</span>
          <div className="flex gap-1">
            {navItems.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                className={({ isActive }) =>
                  `px-3 py-1.5 rounded-md text-sm font-medium border transition-colors ${
                    isActive
                      ? 'bg-indigo-600/20 text-indigo-400 border-indigo-500/30'
                      : 'text-slate-400 hover:text-slate-200 border-transparent'
                  }`
                }
              >
                {item.label}
              </NavLink>
            ))}
          </div>
        </div>
        <button
          onClick={handleLogout}
          className="text-slate-400 hover:text-red-400 text-sm font-medium transition-colors"
        >
          Logout
        </button>
      </nav>
      <main className="flex-1 overflow-auto p-6">
        <Outlet />
      </main>
    </div>
  )
}
