import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatTime(val: string | null | undefined): string {
  if (!val) return '-'
  try {
    return new Date(val).toLocaleString()
  } catch {
    return val
  }
}

export function formatShortTime(val: string | null | undefined): string {
  if (!val) return '-'
  try {
    return new Date(val).toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  } catch {
    return val
  }
}

export function formatDuration(ms: number | null | undefined): string {
  if (ms == null) return '-'
  if (ms >= 1000) return (ms / 1000).toFixed(1) + 's'
  return ms + 'ms'
}

export function formatTokens(entry: { input_tokens?: number; output_tokens?: number } | null): string {
  if (!entry || (!entry.input_tokens && !entry.output_tokens)) return '-'
  return `${entry.input_tokens || 0}/${entry.output_tokens || 0}`
}

export function formatAccount(entry: { account_label?: string; account_id?: string } | null): string {
  if (!entry) return '-'
  return entry.account_label || entry.account_id || '-'
}

export function isSecretField(key: string): boolean {
  return key === 'access_token' || key === 'refresh_token' || key === 'api_key'
}

export function formatRate(model: { rate_multiplier?: number | null; rate_unit?: string } | null): string {
  if (!model) return '-'
  const mult = model.rate_multiplier != null ? model.rate_multiplier : 0
  return mult + ' ' + (model.rate_unit || '').trim()
}

export function formatTokenLimits(model: { token_limits?: { max_input_tokens?: number; max_output_tokens?: number } } | null): string {
  if (!model || !model.token_limits) return '-'
  const input = model.token_limits.max_input_tokens || 0
  const output = model.token_limits.max_output_tokens || 0
  return input.toLocaleString() + ' in / ' + output.toLocaleString() + ' out'
}

export function testStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    valid: 'Valid',
    banned: 'Banned',
    suspended: 'Suspended',
    token_expired: 'Token Expired',
    error: 'Error',
  }
  return labels[status] || 'Unknown'
}

export function testStatusClass(status: string): string {
  if (status === 'valid') return 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30'
  if (status === 'suspended') return 'bg-amber-500/15 text-amber-400 border-amber-500/30'
  if (status === 'banned' || status === 'token_expired') return 'bg-red-500/15 text-red-400 border-red-500/30'
  return 'bg-slate-700/60 text-slate-300 border-slate-600'
}
