import { useState, useEffect } from 'react'
import type { AISettingsResponse, AISettingsRequest } from '../types'

interface Props {
  open: boolean
  onClose: () => void
  settings: AISettingsResponse | null
  onSave: (req: AISettingsRequest) => Promise<AISettingsResponse>
}

export function SettingsModal({ open, onClose, settings, onSave }: Props) {
  const [enabled, setEnabled] = useState(false)
  const [provider, setProvider] = useState('noop')
  const [apiKey, setApiKey] = useState('')
  const [model, setModel] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (settings) {
      setEnabled(settings.enabled)
      setProvider(settings.provider)
      setModel(settings.model ?? '')
      setApiKey('')
    }
  }, [settings])

  if (!open) return null

  const handleSave = async () => {
    setSaving(true)
    setError('')
    try {
      await onSave({ enabled, provider, apiKey: apiKey || undefined, model: model || undefined })
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center glass-backdrop animate-fade-in" onClick={onClose}>
      <div
        className="glass-elevated rounded-2xl animate-slide-up w-full max-w-md p-6 space-y-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>AI Settings</h2>
          <button
            onClick={onClose}
            className="transition-all duration-200 hover:opacity-80"
            style={{ color: 'var(--text-tertiary)' }}
            onMouseEnter={(e) => (e.currentTarget.style.color = 'var(--text-primary)')}
            onMouseLeave={(e) => (e.currentTarget.style.color = 'var(--text-tertiary)')}
          >
            <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        {error && (
          <div className="text-sm bg-severity-critical-bg text-severity-critical rounded-lg px-3 py-2">{error}</div>
        )}

        <label className="flex items-center gap-2">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} className="rounded transition-all duration-200" />
          <span className="text-sm" style={{ color: 'var(--text-secondary)' }}>Enable AI suggestions</span>
        </label>

        <div>
          <label className="text-sm" style={{ color: 'var(--text-tertiary)' }}>Provider</label>
          <select
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            className="w-full mt-1 px-3 py-2 glass-inset rounded-xl text-sm transition-all duration-200"
            style={{ color: 'var(--text-primary)' }}
          >
            <option value="noop">NoOp (testing)</option>
            <option value="anthropic">Anthropic (Claude)</option>
            <option value="openai">OpenAI</option>
          </select>
        </div>

        <div>
          <label className="text-sm" style={{ color: 'var(--text-tertiary)' }}>API Key</label>
          <input
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder={settings?.hasApiKey ? '••••••••' : 'Enter API key'}
            className="w-full mt-1 px-3 py-2 glass-inset rounded-xl text-sm transition-all duration-200"
            style={{ color: 'var(--text-primary)' }}
          />
        </div>

        <div>
          <label className="text-sm" style={{ color: 'var(--text-tertiary)' }}>Model (optional)</label>
          <input
            type="text"
            value={model}
            onChange={(e) => setModel(e.target.value)}
            placeholder="Provider default"
            className="w-full mt-1 px-3 py-2 glass-inset rounded-xl text-sm transition-all duration-200"
            style={{ color: 'var(--text-primary)' }}
          />
        </div>

        {/* Provider status */}
        {settings && (
          <div className="text-xs space-y-1">
            <div className="flex items-center gap-1.5">
              <span className={settings.providerReady ? 'severity-dot-healthy' : 'w-2 h-2 rounded-full bg-glass-300'} />
              <span style={{ color: settings.providerReady ? 'var(--text-secondary)' : 'var(--text-tertiary)' }}>
                {settings.providerReady ? 'Provider ready' : 'Provider not configured'}
              </span>
            </div>
            <p style={{ color: 'var(--text-tertiary)' }}>
              Default Anthropic model: claude-haiku-4-5 (cost-efficient). Override above for Sonnet.
            </p>
          </div>
        )}

        <div className="flex justify-end gap-2 pt-2">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm glass-button rounded-lg transition-all duration-200"
            style={{ color: 'var(--text-secondary)' }}
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={saving}
            className="px-4 py-2 text-sm glass-button-primary rounded-lg transition-all duration-200 disabled:opacity-50"
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}
