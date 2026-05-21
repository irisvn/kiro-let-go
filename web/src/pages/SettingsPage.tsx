import { useEffect, useRef, useState } from 'react'
import type { AvailableModel, DynamicSettings, ModelMappingRule } from '@/store/settings'
import { useSettingsStore } from '@/store/settings'
import { useToastStore } from '@/store/toast'

const strategies = ['round_robin', 'balanced', 'most_quota']
const ruleTypes = ['replace', 'alias', 'loadbalance']

const emptyRule = (): ModelMappingRule => ({
  id: `rule-${Date.now()}`,
  name: '',
  enabled: true,
  rule_type: 'replace',
  source_model: '',
  target_models: ['claude-sonnet-4.6'],
  weights: [],
})

export function SettingsPage() {
  const settings = useSettingsStore((s) => s.settings)
  const loading = useSettingsStore((s) => s.loading)
  const saving = useSettingsStore((s) => s.saving)
  const availableModels = useSettingsStore((s) => s.availableModels)
  const loadSettings = useSettingsStore((s) => s.loadSettings)
  const saveSettings = useSettingsStore((s) => s.saveSettings)
  const loadAvailableModels = useSettingsStore((s) => s.loadAvailableModels)
  const toast = useToastStore((s) => s.addToast)
  const [draft, setDraft] = useState<DynamicSettings | null>(null)

  useEffect(() => {
    loadSettings().catch((e) => toast(e.message, 'error'))
  }, [loadSettings, toast])

  useEffect(() => {
    loadAvailableModels().catch(() => {})
  }, [loadAvailableModels])

  useEffect(() => {
    if (settings) setDraft({ ...settings, model_mappings: settings.model_mappings || [] })
  }, [settings])

  const patch = (part: Partial<DynamicSettings>) => {
    if (draft) setDraft({ ...draft, ...part })
  }

  const patchRule = (index: number, part: Partial<ModelMappingRule>) => {
    if (!draft) return
    const rules = [...draft.model_mappings]
    rules[index] = { ...rules[index], ...part }
    patch({ model_mappings: rules })
  }

  const handleSave = async () => {
    if (!draft) return
    try {
      await saveSettings({
        ...draft,
        base_cooldown_sec: Number(draft.base_cooldown_sec),
        max_backoff_multiplier: Number(draft.max_backoff_multiplier),
        probabilistic_retry_chance: Number(draft.probabilistic_retry_chance),
        max_attempts: Number(draft.max_attempts),
        cache_ttl_seconds: Number(draft.cache_ttl_seconds),
        first_token_timeout_sec: Number(draft.first_token_timeout_sec),
        first_token_max_retries: Number(draft.first_token_max_retries),
        streaming_read_timeout_sec: Number(draft.streaming_read_timeout_sec),
        fake_reasoning_max_tokens: Number(draft.fake_reasoning_max_tokens),
        fake_reasoning_budget_cap: Number(draft.fake_reasoning_budget_cap),
      })
      toast('Settings saved and applied (no restart needed)', 'success')
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  if (loading && !draft) return <div className="text-slate-400">Loading settings…</div>
  if (!draft) return <div className="text-red-400">Settings unavailable</div>

  return (
    <div className="animate-fade-in space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-xl font-semibold text-white">Settings</h2>
          <p className="text-sm text-slate-500">Dynamic configuration applies immediately without restart.</p>
        </div>
        <button onClick={handleSave} disabled={saving} className="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg px-4 py-2 transition-colors">
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>

      <section className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
        <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Load Balancer</h3>
        <div className="grid gap-4 md:grid-cols-2">
          <Field label="Strategy">
            <select value={draft.strategy} onChange={(e) => patch({ strategy: e.target.value })} className="input">
              {strategies.map((s) => <option key={s} value={s}>{s}</option>)}
            </select>
          </Field>
          <Toggle label="Sticky session" checked={draft.sticky_session} onChange={(v) => patch({ sticky_session: v })} />
        </div>
      </section>

      <section className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
        <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Failover & Timeouts</h3>
        <div className="grid gap-4 md:grid-cols-4">
          <NumberField label="Base cooldown (sec)" value={draft.base_cooldown_sec} onChange={(v) => patch({ base_cooldown_sec: v })} />
          <NumberField label="Max backoff multiplier" value={draft.max_backoff_multiplier} onChange={(v) => patch({ max_backoff_multiplier: v })} />
          <NumberField label="Retry chance" value={draft.probabilistic_retry_chance} step="0.01" min="0" max="1" onChange={(v) => patch({ probabilistic_retry_chance: v })} />
          <NumberField label="Max attempts" value={draft.max_attempts} onChange={(v) => patch({ max_attempts: v })} />
        </div>
        <div className="grid gap-4 md:grid-cols-3 pt-2 border-t border-slate-800">
          <NumberField label="First token timeout (sec)" value={draft.first_token_timeout_sec} onChange={(v) => patch({ first_token_timeout_sec: v })} />
          <NumberField label="First token max retries" value={draft.first_token_max_retries} onChange={(v) => patch({ first_token_max_retries: v })} />
          <NumberField label="Total streaming timeout (sec)" value={draft.streaming_read_timeout_sec} onChange={(v) => patch({ streaming_read_timeout_sec: v })} />
        </div>
      </section>

      <section className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
        <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">MCP Web Search</h3>
        <div className="flex items-center gap-4">
          <Toggle label="Enable Web Search via MCP API" checked={draft.web_search_enabled} onChange={(v) => patch({ web_search_enabled: v })} />
        </div>
      </section>

      <section className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
        <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Extended Thinking (Fake Reasoning)</h3>
        <div className="grid gap-4 md:grid-cols-3">
          <div className="flex items-center">
            <Toggle label="Enable Extended Thinking Mode" checked={draft.fake_reasoning_enabled} onChange={(v) => patch({ fake_reasoning_enabled: v })} />
          </div>
          <NumberField label="Max thinking tokens" value={draft.fake_reasoning_max_tokens} onChange={(v) => patch({ fake_reasoning_max_tokens: v })} />
          <NumberField label="Thinking budget cap" value={draft.fake_reasoning_budget_cap} onChange={(v) => patch({ fake_reasoning_budget_cap: v })} />
        </div>
      </section>

      <section className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
        <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Truncation Recovery</h3>
        <div className="flex items-center gap-4">
          <Toggle label="Enable Truncation Recovery & Notices" checked={draft.truncation_recovery_enabled} onChange={(v) => patch({ truncation_recovery_enabled: v })} />
        </div>
      </section>

      <section className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
        <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Quota</h3>
        <NumberField label="Cache TTL (seconds)" value={draft.cache_ttl_seconds} onChange={(v) => patch({ cache_ttl_seconds: v })} />
      </section>

      <section className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Model Mappings</h3>
          <button onClick={() => patch({ model_mappings: [...draft.model_mappings, emptyRule()] })} className="bg-slate-700 hover:bg-slate-600 text-white text-sm rounded-lg px-3 py-1.5">Add rule</button>
        </div>
        <div className="space-y-3">
          {draft.model_mappings.map((rule, i) => (
            <ModelMappingCard key={rule.id || i} rule={rule} index={i} models={availableModels} patchRule={patchRule} removeRule={() => patch({ model_mappings: draft.model_mappings.filter((_, idx) => idx !== i) })} />
          ))}
        </div>
      </section>
    </div>
  )
}

function ModelMappingCard({ rule, index, models, patchRule, removeRule }: {
  rule: ModelMappingRule
  index: number
  models: AvailableModel[]
  patchRule: (index: number, part: Partial<ModelMappingRule>) => void
  removeRule: () => void
}) {
  const isLoadBalance = rule.rule_type === 'loadbalance'
  const isSingleTarget = rule.rule_type === 'replace' || rule.rule_type === 'alias'

  const addTarget = (modelId: string) => {
    if (!modelId) return
    const targets = [...(rule.target_models || [])]
    if (targets.includes(modelId)) return
    if (isSingleTarget) {
      patchRule(index, { target_models: [modelId], weights: [] })
    } else {
      patchRule(index, { target_models: [...targets, modelId], weights: [...(rule.weights || []), 0] })
    }
  }

  const removeTarget = (modelId: string) => {
    const idx = (rule.target_models || []).indexOf(modelId)
    const newTargets = (rule.target_models || []).filter((_, i) => i !== idx)
    const newWeights = (rule.weights || []).filter((_, i) => i !== idx)
    patchRule(index, { target_models: newTargets, weights: newWeights })
  }

  const updateWeight = (modelId: string, weight: number) => {
    const idx = (rule.target_models || []).indexOf(modelId)
    if (idx === -1) return
    const newWeights = [...(rule.weights || [])]
    newWeights[idx] = weight
    patchRule(index, { weights: newWeights })
  }

  return (
    <div className="rounded-lg border border-slate-800 bg-slate-950/40 p-4 space-y-3">
      <div className="grid gap-3 md:grid-cols-4">
        <Field label="Name"><input value={rule.name || ''} onChange={(e) => patchRule(index, { name: e.target.value })} className="input" /></Field>
        <Field label="Type">
          <select value={rule.rule_type} onChange={(e) => {
            const newType = e.target.value
            const updates: Partial<ModelMappingRule> = { rule_type: newType }
            if (newType !== 'loadbalance') {
              updates.weights = []
            } else if (!rule.weights || rule.weights.length === 0) {
              updates.weights = (rule.target_models || []).map(() => 0)
            }
            patchRule(index, updates)
          }} className="input">
            {ruleTypes.map((t) => <option key={t} value={t}>{t}</option>)}
          </select>
        </Field>
        <ModelCombobox label="Source" value={rule.source_model || ''} models={models} onChange={(v) => patchRule(index, { source_model: v })} />
        <div className="flex items-end gap-3">
          <Toggle label="Enabled" checked={rule.enabled} onChange={(v) => patchRule(index, { enabled: v })} />
          <button onClick={removeRule} className="text-red-400 hover:text-red-300 text-sm pb-2">Delete</button>
        </div>
      </div>

      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <span className="text-xs text-slate-500">Target Models</span>
          {isSingleTarget && (rule.target_models || []).length > 0 && (
            <span className="text-xs text-slate-600">(replace/alias uses single target)</span>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          {(rule.target_models || []).map((modelId) => (
            <TargetChip
              key={modelId}
              modelId={modelId}
              models={models}
              weight={isLoadBalance ? (rule.weights || [])[(rule.target_models || []).indexOf(modelId)] : undefined}
              onWeightChange={isLoadBalance ? (w) => updateWeight(modelId, w) : undefined}
              onRemove={() => removeTarget(modelId)}
            />
          ))}
          <ModelDropdown models={models} excluded={rule.target_models || []} onSelect={addTarget} />
        </div>
      </div>
    </div>
  )
}

function TargetChip({ modelId, models, weight, onWeightChange, onRemove }: {
  modelId: string
  models: AvailableModel[]
  weight?: number
  onWeightChange?: (weight: number) => void
  onRemove: () => void
}) {
  const displayName = models.find((m) => m.model_id === modelId)?.model_name || modelId

  return (
    <div className="flex items-center gap-1.5 bg-slate-800 border border-slate-700 rounded-md px-2.5 py-1.5 text-sm">
      <span className="text-slate-200">{displayName}</span>
      <code className="text-xs text-slate-500 font-mono">{modelId}</code>
      {onWeightChange !== undefined && weight !== undefined && (
        <input
          type="number"
          value={weight || 0}
          onChange={(e) => onWeightChange(Number(e.target.value))}
          className="w-14 text-xs bg-slate-900 border border-slate-600 rounded px-1.5 py-0.5 text-slate-300 text-center"
          placeholder="wt"
          min={0}
        />
      )}
      <button onClick={onRemove} className="text-slate-500 hover:text-red-400 transition-colors ml-0.5" aria-label={`Remove ${modelId}`}>
        ×
      </button>
    </div>
  )
}

function ModelDropdown({ models, excluded, onSelect }: { models: AvailableModel[]; excluded: string[]; onSelect: (id: string) => void }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const available = models.filter((m) => !excluded.includes(m.model_id))

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1 bg-slate-800 hover:bg-slate-700 border border-slate-700 rounded-md px-2.5 py-1.5 text-sm text-slate-400 hover:text-slate-200 transition-colors"
      >
        <span>+</span>
        <span>Add</span>
        <svg className={`w-3 h-3 transition-transform ${open ? 'rotate-180' : ''}`} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
        </svg>
      </button>
      {open && (
        <div className="absolute z-50 top-full mt-1 left-0 min-w-56 max-h-64 overflow-y-auto bg-slate-800 border border-slate-700 rounded-lg shadow-xl">
          {available.length === 0 && (
            <div className="px-3 py-2 text-xs text-slate-500">No more models available</div>
          )}
          {available.map((m) => (
            <button
              key={m.model_id}
              onClick={() => { onSelect(m.model_id); setOpen(false) }}
              className="w-full text-left px-3 py-2 hover:bg-slate-700 transition-colors flex items-center justify-between gap-2"
            >
              <span className="text-sm text-slate-200">{m.model_name}</span>
              <code className="text-xs text-slate-500 font-mono">{m.model_id}</code>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

function ModelCombobox({ label, value, models, onChange }: {
  label: string
  value: string
  models: AvailableModel[]
  onChange: (value: string) => void
}) {
  const [inputValue, setInputValue] = useState(value)
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => { setInputValue(value) }, [value])

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const filtered = models.filter((m) =>
    m.model_id.toLowerCase().includes(inputValue.toLowerCase()) ||
    m.model_name.toLowerCase().includes(inputValue.toLowerCase())
  )

  const exactMatch = models.find((m) => m.model_id === inputValue)

  return (
    <Field label={label}>
      <div className="relative" ref={ref}>
        <input
          value={inputValue}
          onChange={(e) => { setInputValue(e.target.value); setOpen(true); onChange(e.target.value) }}
          onFocus={() => setOpen(true)}
          className="input"
          placeholder="Type or select a model…"
        />
        {open && !exactMatch && filtered.length > 0 && (
          <div className="absolute z-50 top-full mt-1 left-0 right-0 max-h-48 overflow-y-auto bg-slate-800 border border-slate-700 rounded-lg shadow-xl">
            {filtered.map((m) => (
              <button
                key={m.model_id}
                onClick={() => { setInputValue(m.model_id); onChange(m.model_id); setOpen(false) }}
                className="w-full text-left px-3 py-2 hover:bg-slate-700 transition-colors flex items-center justify-between gap-2"
              >
                <span className="text-sm text-slate-200">{m.model_name}</span>
                <code className="text-xs text-slate-500 font-mono">{m.model_id}</code>
              </button>
            ))}
          </div>
        )}
        {open && exactMatch && (
          <div className="absolute z-50 top-full mt-1 left-0 right-0 max-h-48 overflow-y-auto bg-slate-800 border border-slate-700 rounded-lg shadow-xl">
            <div className="px-3 py-2 text-xs text-slate-500">
              {exactMatch.model_name} — <code className="font-mono">{exactMatch.model_id}</code>
            </div>
          </div>
        )}
      </div>
    </Field>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className="block text-xs text-slate-500 space-y-1.5"><span>{label}</span>{children}</label>
}

function NumberField({ label, value, onChange, ...props }: { label: string; value: number; onChange: (value: number) => void; step?: string; min?: string; max?: string }) {
  return <Field label={label}><input type="number" value={value} onChange={(e) => onChange(Number(e.target.value))} className="input" {...props} /></Field>
}

function Toggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (checked: boolean) => void }) {
  return <label className="flex items-center gap-2 text-sm text-slate-300"><input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} className="h-4 w-4 accent-indigo-600" />{label}</label>
}

