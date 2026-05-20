import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { useAuthStore } from '@/store/auth'
import { Layout } from '@/components/Layout'
import { LoginPage } from '@/components/LoginPage'
import { AccountsPage } from '@/pages/AccountsPage'
import { AccountDetailPage } from '@/pages/AccountDetailPage'
import { QuotaPage } from '@/pages/QuotaPage'
import { ProxyPage } from '@/pages/ProxyPage'
import { HealthPage } from '@/pages/HealthPage'
import { ToastContainer } from '@/components/Toast'

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const authenticated = useAuthStore((s) => s.authenticated)
  if (!authenticated) return <Navigate to="/admin/ui/" replace />
  return <>{children}</>
}

export default function App() {
  const authenticated = useAuthStore((s) => s.authenticated)

  return (
    <BrowserRouter>
      <ToastContainer />
      <Routes>
        <Route
          path="/admin/ui/"
          element={authenticated ? <Navigate to="/admin/ui/accounts" replace /> : <LoginPage />}
        />
        <Route
          path="/admin/ui"
          element={authenticated ? <Navigate to="/admin/ui/accounts" replace /> : <LoginPage />}
        />
        <Route
          path="/admin/ui"
          element={
            <ProtectedRoute>
              <Layout />
            </ProtectedRoute>
          }
        >
          <Route path="accounts" element={<AccountsPage />} />
          <Route path="accounts/:id" element={<AccountDetailPage />} />
          <Route path="quota" element={<QuotaPage />} />
          <Route path="proxy" element={<ProxyPage />} />
          <Route path="health" element={<HealthPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
