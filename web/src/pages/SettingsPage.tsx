import { useEffect, useState } from 'react'
import type { DynamicSettings, ModelMappingRule } from '@/store/settings'
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
  const loadSettings = useSettingsStore((s) => s.loadSettings)
  const saveSettings = useSettingsStore((s) => s.saveSettings)
  const toast = useToastStore((s) => s.addToast)
  const [draft, setDraft] = useState<DynamicSettings | null>(null)

  useEffect(() => {
    loadSettings().catch((e) => toast(e.message, 'error'))
  }, [loadSettings, toast])

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
        <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Failover</h3>
        <div className="grid gap-4 md:grid-cols-4">
          <NumberField label="Base cooldown (sec)" value={draft.base_cooldown_sec} onChange={(v) => patch({ base_cooldown_sec: v })} />
          <NumberField label="Max backoff multiplier" value={draft.max_backoff_multiplier} onChange={(v) => patch({ max_backoff_multiplier: v })} />
          <NumberField label="Retry chance" value={draft.probabilistic_retry_chance} step="0.01" min="0" max="1" onChange={(v) => patch({ probabilistic_retry_chance: v })} />
          <NumberField label="Max attempts" value={draft.max_attempts} onChange={(v) => patch({ max_attempts: v })} />
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
            <div key={rule.id || i} className="grid gap-3 rounded-lg border border-slate-800 bg-slate-950/40 p-4 md:grid-cols-6">
              <Field label="Name"><input value={rule.name || ''} onChange={(e) => patchRule(i, { name: e.target.value })} className="input" /></Field>
              <Field label="Type"><select value={rule.rule_type} onChange={(e) => patchRule(i, { rule_type: e.target.value })} className="input">{ruleTypes.map((t) => <option key={t} value={t}>{t}</option>)}</select></Field>
              <Field label="Source"><input value={rule.source_model || ''} onChange={(e) => patchRule(i, { source_model: e.target.value })} className="input" /></Field>
              <Field label="Targets (comma-separated)"><input value={(rule.target_models || []).join(', ')} onChange={(e) => patchRule(i, { target_models: csv(e.target.value) })} className="input" /></Field>
              <Field label="Weights"><input value={(rule.weights || []).join(', ')} onChange={(e) => patchRule(i, { weights: csv(e.target.value).map(Number).filter((n) => !Number.isNaN(n)) })} className="input" /></Field>
              <div className="flex items-end gap-3">
                <Toggle label="Enabled" checked={rule.enabled} onChange={(v) => patchRule(i, { enabled: v })} />
                <button onClick={() => patch({ model_mappings: draft.model_mappings.filter((_, idx) => idx !== i) })} className="text-red-400 hover:text-red-300 text-sm pb-2">Delete</button>
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
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

function csv(value: string): string[] {
  return value.split(',').map((v) => v.trim()).filter(Boolean)
}
