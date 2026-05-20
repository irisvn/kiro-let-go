import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useAccountsStore, type EditAccountForm } from '@/store/accounts'
import { useToastStore } from '@/store/toast'
import { formatTime, isSecretField, formatRate, formatTokenLimits, testStatusLabel, testStatusClass } from '@/lib/utils'

export function AccountDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const detailAccount = useAccountsStore((s) => s.detailAccount)
  const models = useAccountsStore((s) => s.models)
  const modelsLoading = useAccountsStore((s) => s.modelsLoading)
  const testLoading = useAccountsStore((s) => s.testLoading)
  const testResult = useAccountsStore((s) => s.testResult)
  const chatTestModel = useAccountsStore((s) => s.chatTestModel)
  const chatTestMessage = useAccountsStore((s) => s.chatTestMessage)
  const chatTestLoading = useAccountsStore((s) => s.chatTestLoading)
  const chatTestResult = useAccountsStore((s) => s.chatTestResult)
  const actionLoading = useAccountsStore((s) => s.actionLoading)
  const openDetail = useAccountsStore((s) => s.openDetail)
  const loadAccountModels = useAccountsStore((s) => s.loadAccountModels)
  const testAccount = useAccountsStore((s) => s.testAccount)
  const sendChatTest = useAccountsStore((s) => s.sendChatTest)
  const forceRefresh = useAccountsStore((s) => s.forceRefresh)
  const editAccount = useAccountsStore((s) => s.editAccount)
  const deleteAccount = useAccountsStore((s) => s.deleteAccount)
  const setChatTestModel = useAccountsStore((s) => s.setChatTestModel)
  const setChatTestMessage = useAccountsStore((s) => s.setChatTestMessage)
  const loadAccounts = useAccountsStore((s) => s.loadAccounts)
  const toast = useToastStore((s) => s.addToast)

  const [showEditModal, setShowEditModal] = useState(false)
  const [editLoading, setEditLoading] = useState(false)
  const [editForm, setEditForm] = useState<EditAccountForm>({ id: '', label: '', region: '', proxy_url: '' })

  const [showDeleteModal, setShowDeleteModal] = useState(false)
  const [deleteLoading, setDeleteLoading] = useState(false)

  useEffect(() => {
    if (id) {
      openDetail(id).catch((e) => toast(e.message, 'error'))
    }
  }, [id, openDetail, toast])

  if (!detailAccount) return <div className="text-slate-500">Loading...</div>

  const acc = detailAccount.account
  const cb = detailAccount.circuit_breaker

  const handleTest = async () => {
    if (!id) return
    try {
      await testAccount(id)
      const result = useAccountsStore.getState().testResult
      const ok = result?.status === 'valid'
      toast(ok ? 'Account valid' : `Account status: ${testStatusLabel(result?.status || '')}`, ok ? 'success' : 'warning')
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  const handleChatTest = async () => {
    if (!id) return
    try {
      await sendChatTest(id)
      toast('Chat test completed', 'success')
    } catch (e) {
      toast('Chat test failed: ' + (e as Error).message, 'error')
    }
  }

  const handleForceRefresh = async () => {
    if (!id) return
    try {
      await forceRefresh(id)
      toast('Token refresh initiated', 'success')
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  const handleEdit = async () => {
    setEditLoading(true)
    try {
      await editAccount(editForm)
      setShowEditModal(false)
      toast('Account updated', 'success')
      await loadAccounts()
      if (id) await openDetail(id)
    } catch (e) {
      toast((e as Error).message, 'error')
    } finally {
      setEditLoading(false)
    }
  }

  const handleDelete = async () => {
    if (!id) return
    setDeleteLoading(true)
    try {
      await deleteAccount(id)
      setShowDeleteModal(false)
      toast('Account deleted', 'success')
      navigate('/admin/ui/accounts')
      await loadAccounts()
    } catch (e) {
      toast((e as Error).message, 'error')
    } finally {
      setDeleteLoading(false)
    }
  }

  const tokenStatusBadge = () => {
    if (acc.auth_method === 'social') {
      if (!acc.access_token) return <span className="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-medium bg-red-500/15 text-red-400 border border-red-500/30">No Token</span>
      if (acc.expires_at && new Date(acc.expires_at) < new Date()) return <span className="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-medium bg-red-500/15 text-red-400 border border-red-500/30">Token Expired</span>
      if (acc.expires_at) {
        const mins = Math.ceil((new Date(acc.expires_at).getTime() - Date.now()) / 60000)
        return <span className="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-medium bg-emerald-500/15 text-emerald-400 border border-emerald-500/30">Token Valid ({mins} min)</span>
      }
    }
    if (acc.auth_method === 'apikey') {
      return acc.api_key
        ? <span className="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-medium bg-emerald-500/15 text-emerald-400 border border-emerald-500/30">API Key Set</span>
        : <span className="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-medium bg-red-500/15 text-red-400 border border-red-500/30">No API Key</span>
    }
    return null
  }

  const fields = [
    { label: 'ID', key: 'id' as const },
    { label: 'Label', key: 'label' as const },
    { label: 'Auth Method', key: 'auth_method' as const },
    { label: 'Region', key: 'region' as const },
    { label: 'Machine ID', key: 'machine_id' as const },
    { label: 'Enabled', key: 'enabled' as const },
    { label: 'Disabled Reason', key: 'disabled_reason' as const },
    { label: 'Access Token', key: 'access_token' as const },
    { label: 'Refresh Token', key: 'refresh_token' as const },
    { label: 'API Key', key: 'api_key' as const },
    { label: 'Profile ARN', key: 'profile_arn' as const },
    { label: 'Proxy URL', key: 'proxy_url' as const },
    { label: 'Created', key: 'created_at' as const },
    { label: 'Updated', key: 'updated_at' as const },
  ]

  return (
    <div className="animate-fade-in">
      <div className="flex items-center gap-3 mb-4">
        <button onClick={() => navigate('/admin/ui/accounts')} className="text-slate-400 hover:text-white transition-colors text-sm">
          ← Back
        </button>
        <h2 className="text-xl font-semibold text-white">{acc.label}</h2>
      </div>

      <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 mb-4">
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Account Info</h3>
          {tokenStatusBadge()}
        </div>
        <div className="grid grid-cols-2 md:grid-cols-3 gap-x-6 gap-y-3 text-sm">
          {fields.map((field) => (
            <SecretField key={field.key} label={field.label} value={acc[field.key]} isSecret={isSecretField(field.key)} isBoolean={field.key === 'enabled'} />
          ))}
        </div>
      </div>

      <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 mb-4">
        <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider mb-3">Circuit Breaker</h3>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-x-6 gap-y-3 text-sm">
          <div>
            <span className="text-slate-500 text-xs block">State</span>
            <span className={`font-medium ${cb.open ? 'text-red-400' : 'text-emerald-400'}`}>{cb.state}</span>
          </div>
          <div>
            <span className="text-slate-500 text-xs block">Failures</span>
            <span className="text-slate-200">{cb.failures}</span>
          </div>
          <div>
            <span className="text-slate-500 text-xs block">Last Reason</span>
            <span className="text-slate-200 text-xs">{cb.last_reason || '-'}</span>
          </div>
          <div>
            <span className="text-slate-500 text-xs block">Last Failure</span>
            <span className="text-slate-200 text-xs">{formatTime(acc.last_failure_at)}</span>
          </div>
        </div>
      </div>

      <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 mb-4">
        <div className="flex items-center justify-between mb-3">
          <div>
            <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Available Models</h3>
            <p className="text-xs text-slate-500 mt-1">Fetched from Kiro ListAvailableModels</p>
          </div>
          <button
            onClick={() => id && loadAccountModels(id)}
            disabled={modelsLoading}
            className="text-indigo-400 hover:text-indigo-300 disabled:opacity-50 text-xs font-medium transition-colors flex items-center gap-1"
          >
            {modelsLoading && <span className="spinner spinner-sm" />}
            Refresh
          </button>
        </div>
        {modelsLoading && <div className="py-8 text-center text-slate-500 text-sm">Loading models...</div>}
        {!modelsLoading && (!models?.models?.length) && <div className="py-8 text-center text-slate-500 text-sm">No models loaded</div>}
        {!modelsLoading && models?.models?.length ? (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-800 text-slate-500 text-left">
                  <th className="py-2 pr-3 font-medium">Model</th>
                  <th className="py-2 px-3 font-medium">Rate</th>
                  <th className="py-2 px-3 font-medium">Inputs</th>
                  <th className="py-2 px-3 font-medium">Token Limits</th>
                </tr>
              </thead>
              <tbody>
                {models.models.map((model) => (
                  <tr key={model.model_id} className="border-b border-slate-800/50">
                    <td className="py-3 pr-3">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-slate-100 font-medium">{model.model_name || model.model_id}</span>
                        {model.is_default && <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-indigo-500/15 text-indigo-400 border border-indigo-500/30">Default</span>}
                      </div>
                      <div className="font-mono text-xs text-slate-500">{model.model_id}</div>
                    </td>
                    <td className="py-3 px-3 font-mono text-xs text-slate-300">{formatRate(model)}</td>
                    <td className="py-3 px-3 text-xs text-slate-300">{model.supported_input_types?.join(', ') || '-'}</td>
                    <td className="py-3 px-3 font-mono text-xs text-slate-300">{formatTokenLimits(model)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </div>

      <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 mb-4">
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Test API</h3>
          <button
            onClick={handleTest}
            disabled={testLoading || !acc.enabled}
            className="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors flex items-center gap-2"
          >
            {testLoading && <span className="spinner" />}
            {testLoading ? 'Testing...' : 'Test Connection'}
          </button>
        </div>
        <p className="text-xs text-slate-500 mb-3">Uses getUsageLimits only; no chat quota is consumed.</p>
        {testResult && (
          <div className="mt-3 rounded-lg border border-slate-800 bg-slate-950/50 p-3 text-sm">
            <div className="flex flex-wrap items-center gap-2 mb-2">
              <span className={`inline-flex items-center px-2.5 py-0.5 rounded text-xs font-medium border ${testStatusClass(testResult.status)}`}>
                {testStatusLabel(testResult.status)}
              </span>
              <span className="text-xs text-slate-500">{testResult.duration_ms} ms</span>
            </div>
            <div className="grid sm:grid-cols-3 gap-3 text-xs">
              <div><span className="text-slate-500 block">Message</span><span className="text-slate-200">{testResult.message || '-'}</span></div>
              <div><span className="text-slate-500 block">Subscription</span><span className="text-slate-200">{testResult.subscription_title || '-'}</span></div>
              <div><span className="text-slate-500 block">User ID</span><span className="text-slate-200 font-mono break-all">{testResult.user_id || '-'}</span></div>
            </div>
          </div>
        )}
      </div>

      <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 mb-4">
        <div className="flex items-center justify-between mb-3">
          <div>
            <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Chat Test</h3>
            <p className="text-xs text-slate-500 mt-1">Sends a minimal real chat request through this account.</p>
          </div>
          <button
            onClick={handleChatTest}
            disabled={chatTestLoading || !acc.enabled || !chatTestModel}
            className="bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors flex items-center gap-2"
          >
            {chatTestLoading && <span className="spinner" />}
            {chatTestLoading ? 'Sending...' : 'Send Test'}
          </button>
        </div>
        <div className="grid md:grid-cols-3 gap-3">
          <div className="md:col-span-1">
            <label className="block text-xs font-medium text-slate-400 mb-1">Model</label>
            <select
              value={chatTestModel}
              onChange={(e) => setChatTestModel(e.target.value)}
              className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-indigo-500"
            >
              {(models?.models?.length ? models.models : [{ model_id: 'claude-haiku-4.5', model_name: 'Claude Haiku 4.5', is_default: false } as const]).map((m) => (
                <option key={m.model_id} value={m.model_id}>
                  {m.model_name || m.model_id}{m.is_default ? ' (default)' : ''}
                </option>
              ))}
            </select>
          </div>
          <div className="md:col-span-2">
            <label className="block text-xs font-medium text-slate-400 mb-1">Message</label>
            <input
              value={chatTestMessage}
              onChange={(e) => setChatTestMessage(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleChatTest()}
              className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
              placeholder="Enter test message..."
            />
          </div>
        </div>
        {chatTestLoading && (
          <div className="mt-3 flex items-center gap-2 text-sm text-slate-400">
            <span className="spinner" />Waiting for AI response...
          </div>
        )}
        {chatTestResult && (
          <div className={`mt-3 rounded-lg border p-4 text-sm ${chatTestResult.success ? 'border-emerald-500/40 bg-emerald-500/5' : 'border-red-500/40 bg-red-500/5'}`}>
            {chatTestResult.success ? (
              <div>
                <div className="flex flex-wrap items-center gap-2 mb-2">
                  <span className="inline-flex items-center px-2.5 py-0.5 rounded text-xs font-medium bg-emerald-500/15 text-emerald-400 border border-emerald-500/30">Success</span>
                  <span className="text-xs text-slate-500">{chatTestResult.model}</span>
                  <span className="text-xs text-slate-500">{chatTestResult.duration_ms} ms</span>
                </div>
                <div className="whitespace-pre-wrap text-slate-100">{chatTestResult.response || '(empty response)'}</div>
              </div>
            ) : (
              <div>
                <div className="mb-2 inline-flex items-center px-2.5 py-0.5 rounded text-xs font-medium bg-red-500/15 text-red-400 border border-red-500/30">Error</div>
                <div className="text-red-300">{chatTestResult.error || 'Chat test failed'}</div>
              </div>
            )}
          </div>
        )}
      </div>

      <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 mb-4">
        <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider mb-3">Counters</h3>
        <div className="grid grid-cols-3 gap-6 text-sm">
          <div><span className="text-slate-500 text-xs block">Success Count</span><span className="text-emerald-400 font-medium text-lg">{acc.success_count}</span></div>
          <div><span className="text-slate-500 text-xs block">Failure Count</span><span className="text-red-400 font-medium text-lg">{acc.failure_count}</span></div>
          <div><span className="text-slate-500 text-xs block">Last Used</span><span className="text-slate-200 text-xs">{formatTime(acc.last_used_at)}</span></div>
        </div>
      </div>

      <div className="flex flex-wrap gap-3">
        <button onClick={handleForceRefresh} disabled={actionLoading} className="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors flex items-center gap-2">
          {actionLoading && <span className="spinner" />}Force Token Refresh
        </button>
        <button
          onClick={() => { setEditForm({ id: acc.id, label: acc.label, region: acc.region || '', proxy_url: acc.proxy_url || '' }); setShowEditModal(true) }}
          className="bg-slate-700 hover:bg-slate-600 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors"
        >
          Edit
        </button>
        <button
          onClick={() => setShowDeleteModal(true)}
          className="bg-red-600/20 hover:bg-red-600/40 text-red-400 text-sm font-medium rounded-lg px-4 py-2 transition-colors border border-red-500/30"
        >
          Delete
        </button>
      </div>

      {showEditModal && (
        <div className="fixed inset-0 z-40 flex items-center justify-center" onClick={() => setShowEditModal(false)}>
          <div className="fixed inset-0 bg-black/60" />
          <div className="relative bg-slate-900 border border-slate-700 rounded-xl shadow-2xl w-full max-w-lg mx-4 p-6 z-50 max-h-[90vh] overflow-y-auto" onClick={(e) => e.stopPropagation()}>
            <h3 className="text-lg font-semibold text-white mb-4">Edit Account</h3>
            <div className="space-y-4">
              <div><label className="block text-sm font-medium text-slate-300 mb-1">Label</label><input value={editForm.label} onChange={(e) => setEditForm({ ...editForm, label: e.target.value })} className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-indigo-500" /></div>
              <div><label className="block text-sm font-medium text-slate-300 mb-1">Region</label><input value={editForm.region} onChange={(e) => setEditForm({ ...editForm, region: e.target.value })} className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-indigo-500" /></div>
              <div><label className="block text-sm font-medium text-slate-300 mb-1">Proxy URL</label><input value={editForm.proxy_url} onChange={(e) => setEditForm({ ...editForm, proxy_url: e.target.value })} className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-indigo-500" /></div>
            </div>
            <div className="flex justify-end gap-3 mt-6">
              <button onClick={() => setShowEditModal(false)} className="px-4 py-2 text-sm text-slate-400 hover:text-white transition-colors">Cancel</button>
              <button onClick={handleEdit} disabled={editLoading} className="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors flex items-center gap-2">
                {editLoading && <span className="spinner" />}Save
              </button>
            </div>
          </div>
        </div>
      )}

      {showDeleteModal && (
        <div className="fixed inset-0 z-40 flex items-center justify-center" onClick={() => setShowDeleteModal(false)}>
          <div className="fixed inset-0 bg-black/60" />
          <div className="relative bg-slate-900 border border-slate-700 rounded-xl shadow-2xl w-full max-w-md mx-4 p-6 z-50" onClick={(e) => e.stopPropagation()}>
            <h3 className="text-lg font-semibold text-white mb-2">Delete Account</h3>
            <p className="text-sm text-slate-400 mb-4">Are you sure you want to delete <span className="text-white font-medium">{acc.label}</span>? This cannot be undone.</p>
            <div className="flex justify-end gap-3">
              <button onClick={() => setShowDeleteModal(false)} className="px-4 py-2 text-sm text-slate-400 hover:text-white transition-colors">Cancel</button>
              <button onClick={handleDelete} disabled={deleteLoading} className="bg-red-600 hover:bg-red-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors flex items-center gap-2">
                {deleteLoading && <span className="spinner" />}Delete
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function SecretField({ label, value, isSecret, isBoolean }: { label: string; value: string | boolean | null | undefined; isSecret: boolean; isBoolean: boolean }) {
  const [revealed, setRevealed] = useState(false)
  const display = isBoolean
    ? (value ? 'Yes' : 'No')
    : (value || '-')

  return (
    <div>
      <span className="text-slate-500 text-xs block">{label}</span>
      {isSecret ? (
        <div className="flex items-start gap-2">
          <span className="text-slate-200 font-mono text-xs break-all flex-1">
            {revealed ? (value || '-') : (value ? '••••••••••••' : '-')}
          </span>
          {value && (
            <button onClick={() => setRevealed(!revealed)} className="text-xs text-indigo-400 hover:text-indigo-300 whitespace-nowrap shrink-0 mt-0.5">
              {revealed ? 'Hide' : 'Show'}
            </button>
          )}
        </div>
      ) : (
        <span className="text-slate-200 font-mono text-xs break-all">{display}</span>
      )}
    </div>
  )
}
