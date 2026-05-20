import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAccountsStore, type AddAccountForm, type Account } from '@/store/accounts'
import { useQuotaStore } from '@/store/quota'
import { useToastStore } from '@/store/toast'
import { formatTime } from '@/lib/utils'

export function AccountsPage() {
  const accounts = useAccountsStore((s) => s.accounts)
  const loadAccounts = useAccountsStore((s) => s.loadAccounts)
  const toggleEnabled = useAccountsStore((s) => s.toggleEnabled)
  const deleteAccount = useAccountsStore((s) => s.deleteAccount)
  const addAccount = useAccountsStore((s) => s.addAccount)
  const toast = useToastStore((s) => s.addToast)
  const navigate = useNavigate()

  const [showAddModal, setShowAddModal] = useState(false)
  const [addLoading, setAddLoading] = useState(false)
  const [addForm, setAddForm] = useState<AddAccountForm>({
    label: '', auth_method: 'social', refresh_token: '', api_key: '',
    profile_arn: '', region: 'us-east-1', proxy_url: '',
  })

  const [showDisableModal, setShowDisableModal] = useState(false)
  const [disableAccountId, setDisableAccountId] = useState('')
  const [disableReason, setDisableReason] = useState('')
  const [disableLoading, setDisableLoading] = useState(false)

  const [showDeleteModal, setShowDeleteModal] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<Account | null>(null)
  const [deleteLoading, setDeleteLoading] = useState(false)

  useEffect(() => {
    loadAccounts().catch((e) => toast(e.message, 'error'))
  }, [loadAccounts, toast])

  const handleToggleEnabled = async (acc: Account) => {
    try {
      const result = await toggleEnabled(acc)
      if (result && 'needDisable' in result) {
        setDisableAccountId(result.accountId)
        setDisableReason('')
        setShowDisableModal(true)
      } else {
        toast('Account enabled', 'success')
        await loadAccounts()
      }
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  const handleSubmitDisable = async () => {
    setDisableLoading(true)
    try {
      await useAccountsStore.getState().submitDisable(disableAccountId, disableReason)
      setShowDisableModal(false)
      toast('Account disabled', 'success')
      await loadAccounts()
    } catch (e) {
      toast((e as Error).message, 'error')
    } finally {
      setDisableLoading(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setDeleteLoading(true)
    try {
      await deleteAccount(deleteTarget.id)
      setShowDeleteModal(false)
      toast('Account deleted', 'success')
      await loadAccounts()
    } catch (e) {
      toast((e as Error).message, 'error')
    } finally {
      setDeleteLoading(false)
    }
  }

  const handleAdd = async () => {
    setAddLoading(true)
    try {
      const result = await addAccount(addForm)
      setShowAddModal(false)
      if (result?.verified) {
        toast('Account created and verified successfully', 'success')
      } else if (result && !result.verified) {
        toast('Account created but verification failed: ' + (result.verification_error || 'unknown error') + '. Account has been disabled.', 'warning')
      } else {
        toast('Account created', 'success')
      }
      await loadAccounts()
      await useQuotaStore.getState().loadQuota()
    } catch (e) {
      toast((e as Error).message, 'error')
    } finally {
      setAddLoading(false)
    }
  }

  return (
    <div className="animate-fade-in">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-xl font-semibold text-white">Accounts</h2>
        <button
          onClick={() => {
            setAddForm({ label: '', auth_method: 'social', refresh_token: '', api_key: '', profile_arn: '', region: 'us-east-1', proxy_url: '' })
            setShowAddModal(true)
          }}
          className="bg-indigo-600 hover:bg-indigo-500 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors"
        >
          + Add Account
        </button>
      </div>

      <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-slate-800 text-slate-400 text-left">
              <th className="px-4 py-3 font-medium">Label</th>
              <th className="px-4 py-3 font-medium">Auth</th>
              <th className="px-4 py-3 font-medium">Region</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Enabled</th>
              <th className="px-4 py-3 font-medium text-right">Failures</th>
              <th className="px-4 py-3 font-medium text-right">Successes</th>
              <th className="px-4 py-3 font-medium">Last Used</th>
              <th className="px-4 py-3 font-medium text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {accounts.map((acc) => (
              <tr key={acc.id} className="border-b border-slate-800/50 hover:bg-slate-800/30 transition-colors">
                <td className="px-4 py-3">
                  <button
                    onClick={() => navigate(`/admin/ui/accounts/${acc.id}`)}
                    className="text-indigo-400 hover:text-indigo-300 font-medium"
                  >
                    {acc.label}
                  </button>
                </td>
                <td className="px-4 py-3">
                  <span
                    className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${
                      acc.auth_method === 'apikey'
                        ? 'bg-amber-500/15 text-amber-400'
                        : 'bg-sky-500/15 text-sky-400'
                    }`}
                  >
                    {acc.auth_method}
                  </span>
                </td>
                <td className="px-4 py-3 text-slate-300 font-mono text-xs">{acc.region || '-'}</td>
                <td className="px-4 py-3"><AccountStatusBadge account={acc} /></td>
                <td className="px-4 py-3">
                  <button
                    onClick={() => handleToggleEnabled(acc)}
                    className={`inline-flex items-center px-2.5 py-0.5 rounded text-xs font-medium border transition-colors ${
                      acc.enabled
                        ? 'bg-emerald-500/20 text-emerald-400 border-emerald-500/30'
                        : 'bg-red-500/20 text-red-400 border-red-500/30'
                    }`}
                  >
                    {acc.enabled ? 'Enabled' : 'Disabled'}
                  </button>
                </td>
                <td className={`px-4 py-3 text-right ${acc.failure_count > 0 ? 'text-red-400' : 'text-slate-500'}`}>
                  {acc.failure_count}
                </td>
                <td className="px-4 py-3 text-right text-slate-300">{acc.success_count}</td>
                <td className="px-4 py-3 text-slate-400 text-xs">{formatTime(acc.last_used_at)}</td>
                <td className="px-4 py-3 text-right">
                  <div className="flex items-center justify-end gap-2">
                    <button
                      onClick={() => navigate(`/admin/ui/accounts/${acc.id}`)}
                      className="text-slate-400 hover:text-white transition-colors text-xs"
                    >
                      Detail
                    </button>
                    <button
                      onClick={() => { setDeleteTarget(acc); setShowDeleteModal(true) }}
                      className="text-red-400/70 hover:text-red-400 transition-colors text-xs"
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {accounts.length === 0 && (
              <tr>
                <td colSpan={9} className="px-4 py-8 text-center text-slate-500">No accounts found</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>


      {showAddModal && (
        <ModalOverlay onClose={() => setShowAddModal(false)}>
          <h3 className="text-lg font-semibold text-white mb-4">Add Account</h3>
          <div className="space-y-4">
            <Field label="Label *" value={addForm.label} onChange={(v) => setAddForm({ ...addForm, label: v })} placeholder="my-account" />
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-1">Auth Method *</label>
              <select
                value={addForm.auth_method}
                onChange={(e) => setAddForm({ ...addForm, auth_method: e.target.value })}
                className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-indigo-500"
              >
                <option value="social">social</option>
                <option value="apikey">apikey</option>
              </select>
            </div>
            {addForm.auth_method === 'social' && (
              <Field label="Refresh Token *" value={addForm.refresh_token} onChange={(v) => setAddForm({ ...addForm, refresh_token: v })} type="password" mono />
            )}
            {addForm.auth_method === 'apikey' && (
              <Field label="API Key *" value={addForm.api_key} onChange={(v) => setAddForm({ ...addForm, api_key: v })} type="password" mono placeholder="ksk_..." />
            )}
            <Field label="Profile ARN" value={addForm.profile_arn} onChange={(v) => setAddForm({ ...addForm, profile_arn: v })} />
            <Field label="Region" value={addForm.region} onChange={(v) => setAddForm({ ...addForm, region: v })} placeholder="us-east-1" />
            <Field label="Proxy URL" value={addForm.proxy_url} onChange={(v) => setAddForm({ ...addForm, proxy_url: v })} placeholder="http://proxy:8080" />
          </div>
          <div className="flex justify-end gap-3 mt-6">
            <button onClick={() => setShowAddModal(false)} className="px-4 py-2 text-sm text-slate-400 hover:text-white transition-colors">Cancel</button>
            <button onClick={handleAdd} disabled={addLoading} className="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors flex items-center gap-2">
              {addLoading && <span className="spinner" />}Create
            </button>
          </div>
        </ModalOverlay>
      )}


      {showDisableModal && (
        <ModalOverlay onClose={() => setShowDisableModal(false)}>
          <h3 className="text-lg font-semibold text-white mb-2">Disable Account</h3>
          <p className="text-sm text-slate-400 mb-4">Provide a reason for disabling this account.</p>
          <input
            value={disableReason}
            onChange={(e) => setDisableReason(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSubmitDisable()}
            className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-indigo-500"
            placeholder="Reason for disabling"
          />
          <div className="flex justify-end gap-3 mt-4">
            <button onClick={() => setShowDisableModal(false)} className="px-4 py-2 text-sm text-slate-400 hover:text-white transition-colors">Cancel</button>
            <button onClick={handleSubmitDisable} disabled={disableLoading} className="bg-red-600 hover:bg-red-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors flex items-center gap-2">
              {disableLoading && <span className="spinner" />}Disable
            </button>
          </div>
        </ModalOverlay>
      )}


      {showDeleteModal && deleteTarget && (
        <ModalOverlay onClose={() => setShowDeleteModal(false)}>
          <h3 className="text-lg font-semibold text-white mb-2">Delete Account</h3>
          <p className="text-sm text-slate-400 mb-4">
            Are you sure you want to delete <span className="text-white font-medium">{deleteTarget.label}</span>? This cannot be undone.
          </p>
          <div className="flex justify-end gap-3">
            <button onClick={() => setShowDeleteModal(false)} className="px-4 py-2 text-sm text-slate-400 hover:text-white transition-colors">Cancel</button>
            <button onClick={handleDelete} disabled={deleteLoading} className="bg-red-600 hover:bg-red-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors flex items-center gap-2">
              {deleteLoading && <span className="spinner" />}Delete
            </button>
          </div>
        </ModalOverlay>
      )}
    </div>
  )
}

function AccountStatusBadge({ account }: { account: Account }) {
  if (!account.enabled) {
    return <span className="inline-flex items-center px-2.5 py-0.5 rounded text-xs font-medium bg-slate-500/15 text-slate-400 border border-slate-500/30">Disabled</span>
  }
  if (account.circuit_state === 'open') {
    return (
      <span className="inline-flex items-center px-2.5 py-0.5 rounded text-xs font-medium bg-red-500/20 text-red-400 border border-red-500/30">
        Circuit Open{account.failure_count > 0 ? ` (${account.failure_count})` : ''}
      </span>
    )
  }
  if (account.circuit_state === 'cooldown') {
    return (
      <span className="inline-flex items-center px-2.5 py-0.5 rounded text-xs font-medium bg-amber-500/15 text-amber-400 border border-amber-500/30">
        Cooldown{account.failure_count > 0 ? ` (${account.failure_count})` : ''}
      </span>
    )
  }
  return (
    <span className="inline-flex items-center px-2.5 py-0.5 rounded text-xs font-medium bg-emerald-500/15 text-emerald-400 border border-emerald-500/30">
      Active{account.failure_count > 0 ? ` (${account.failure_count} failures)` : ''}
    </span>
  )
}

function Field({ label, value, onChange, type = 'text', mono = false, placeholder }: {
  label: string; value: string; onChange: (v: string) => void; type?: string; mono?: boolean; placeholder?: string
}) {
  return (
    <div>
      <label className="block text-sm font-medium text-slate-300 mb-1">{label}</label>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={`w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-indigo-500 ${mono ? 'font-mono' : ''}`}
        placeholder={placeholder}
      />
    </div>
  )
}

function ModalOverlay({ children, onClose }: { children: React.ReactNode; onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center" onClick={onClose}>
      <div className="fixed inset-0 bg-black/60" />
      <div
        className="relative bg-slate-900 border border-slate-700 rounded-xl shadow-2xl w-full max-w-lg mx-4 p-6 z-50 max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        {children}
      </div>
    </div>
  )
}
