import { useState, useEffect } from 'react'
import type { AISettingsResponse, AISettingsRequest, ModelEntry } from '../types'
import { useCatalog } from '../hooks/useCatalog'

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
  const [explainModel, setExplainModel] = useState('')
  const [customModel, setCustomModel] = useState('')
  const [customExplainModel, setCustomExplainModel] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const { catalog, loading: catalogLoading } = useCatalog(provider)

  // Get models for current provider
  const providerModels = catalog?.[provider] ?? []
  const primaryModels = providerModels.filter(
    (m) => m.tier === 'primary' || m.tier === 'both'
  )
  const explainModels = providerModels.filter(
    (m) => m.tier === 'explain' || m.tier === 'both'
  )

  useEffect(() => {
    if (settings) {
      setEnabled(settings.enabled)
      setProvider(settings.provider)
      setModel(settings.model ?? '')
      setExplainModel(settings.explainModel ?? '')
      setApiKey('')
      setCustomModel('')
      setCustomExplainModel('')
    }
  }, [settings])

  if (!open) return null

  const isCustomModel = model !== '' && !providerModels.some((m) => m.id === model)
  const isCustomExplainModel = explainModel !== '' && !providerModels.some((m) => m.id === explainModel)

  const handleSave = async () => {
    setSaving(true)
    setError('')
    try {
      const finalModel = isCustomModel ? customModel || model : model
      const finalExplainModel = isCustomExplainModel ? customExplainModel || explainModel : explainModel
      await onSave({
        enabled,
        provider,
        apiKey: apiKey || undefined,
        model: finalModel || undefined,
        explainModel: finalExplainModel || undefined,
      })
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center glass-backdrop animate-fade-in"
      onClick={onClose}
      onKeyDown={(e) => { if (e.key === 'Escape') onClose() }}
      role="dialog"
      aria-modal="true"
      aria-labelledby="settings-title"
    >
      <div
        className="glass-elevated rounded-2xl animate-slide-up w-full max-w-lg p-6 space-y-4 max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 id="settings-title" className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>AI Settings</h2>
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

        {/* Provider selector */}
        <div>
          <label className="text-sm" style={{ color: 'var(--text-tertiary)' }}>Provider</label>
          <select
            value={provider}
            onChange={(e) => {
              setProvider(e.target.value)
              setModel('')
              setExplainModel('')
              setCustomModel('')
              setCustomExplainModel('')
            }}
            className="w-full mt-1 px-3 py-2 glass-inset rounded-xl text-sm transition-all duration-200"
            style={{ color: 'var(--text-primary)' }}
          >
            <option value="noop">NoOp (testing)</option>
            <option value="anthropic">Anthropic (Claude)</option>
            <option value="openai">OpenAI</option>
          </select>
        </div>

        {/* API Key */}
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

        {/* Primary Model Selection */}
        {provider !== 'noop' && (
          <div>
            <label className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>Analysis Model</label>
            {catalogLoading ? (
              <div className="mt-2 text-xs" style={{ color: 'var(--text-tertiary)' }}>Loading models...</div>
            ) : (
              <div className="mt-2 space-y-1.5">
                {primaryModels.map((m) => (
                  <ModelRadio
                    key={m.id}
                    entry={m}
                    selected={model === m.id}
                    onSelect={() => { setModel(m.id); setCustomModel('') }}
                  />
                ))}
                {/* Custom model option */}
                <label className="flex items-center gap-2 p-2 glass-inset rounded-lg cursor-pointer transition-all duration-200">
                  <input
                    type="radio"
                    name="primary-model"
                    checked={isCustomModel || model === ''}
                    onChange={() => setModel(customModel || '')}
                    className="shrink-0"
                  />
                  <div className="flex-1">
                    <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>Custom model ID</div>
                    {(isCustomModel || model === '') && (
                      <input
                        type="text"
                        value={isCustomModel ? model : customModel}
                        onChange={(e) => {
                          setCustomModel(e.target.value)
                          setModel(e.target.value)
                        }}
                        placeholder="e.g., claude-sonnet-4-5-20250929"
                        className="w-full mt-1 px-2 py-1 glass-inset rounded-lg text-xs transition-all duration-200"
                        style={{ color: 'var(--text-primary)' }}
                      />
                    )}
                  </div>
                </label>
              </div>
            )}
          </div>
        )}

        {/* Explain Model Selection */}
        {provider !== 'noop' && explainModels.length > 0 && (
          <div>
            <label className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>
              Explain Model <span className="font-normal" style={{ color: 'var(--text-tertiary)' }}>(optional, for narrative summaries)</span>
            </label>
            <div className="mt-2 space-y-1.5">
              <label className="flex items-center gap-2 p-2 glass-inset rounded-lg cursor-pointer transition-all duration-200">
                <input
                  type="radio"
                  name="explain-model"
                  checked={explainModel === ''}
                  onChange={() => { setExplainModel(''); setCustomExplainModel('') }}
                  className="shrink-0"
                />
                <span className="text-sm" style={{ color: 'var(--text-secondary)' }}>Same as analysis model</span>
              </label>
              {explainModels.map((m) => (
                <ModelRadio
                  key={m.id}
                  entry={m}
                  selected={explainModel === m.id}
                  onSelect={() => { setExplainModel(m.id); setCustomExplainModel('') }}
                  radioName="explain-model"
                />
              ))}
              {/* Custom explain model */}
              <label className="flex items-center gap-2 p-2 glass-inset rounded-lg cursor-pointer transition-all duration-200">
                <input
                  type="radio"
                  name="explain-model"
                  checked={isCustomExplainModel}
                  onChange={() => setExplainModel(customExplainModel || 'custom')}
                  className="shrink-0"
                />
                <div className="flex-1">
                  <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>Custom model ID</div>
                  {isCustomExplainModel && (
                    <input
                      type="text"
                      value={explainModel === 'custom' ? customExplainModel : explainModel}
                      onChange={(e) => {
                        setCustomExplainModel(e.target.value)
                        setExplainModel(e.target.value)
                      }}
                      placeholder="e.g., claude-haiku-4-5-20251001"
                      className="w-full mt-1 px-2 py-1 glass-inset rounded-lg text-xs transition-all duration-200"
                      style={{ color: 'var(--text-primary)' }}
                    />
                  )}
                </div>
              </label>
            </div>
          </div>
        )}

        {/* Provider status */}
        {settings && (
          <div className="text-xs space-y-1">
            <div className="flex items-center gap-1.5">
              <span className={settings.providerReady ? 'severity-dot-healthy' : 'w-2 h-2 rounded-full bg-glass-300'} />
              <span style={{ color: settings.providerReady ? 'var(--text-secondary)' : 'var(--text-tertiary)' }}>
                {settings.providerReady ? 'Provider ready' : 'Provider not configured'}
              </span>
            </div>
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

/** Reusable model radio button with status badge and pricing hint */
function ModelRadio({
  entry,
  selected,
  onSelect,
  radioName = 'primary-model',
}: {
  entry: ModelEntry
  selected: boolean
  onSelect: () => void
  radioName?: string
}) {
  return (
    <label className={`flex items-center gap-2 p-2 rounded-lg cursor-pointer transition-all duration-200 ${selected ? 'glass-panel ring-1 ring-accent/30' : 'glass-inset'}`}>
      <input
        type="radio"
        name={radioName}
        checked={selected}
        onChange={onSelect}
        className="shrink-0"
      />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>{entry.label}</span>
          {entry.status === 'deprecated' && (
            <span className="severity-pill-warning text-[10px]">Deprecated</span>
          )}
        </div>
        <div className="flex items-center gap-2 mt-0.5">
          <span className="text-[11px]" style={{ color: 'var(--text-tertiary)' }}>{entry.id}</span>
          {entry.pricingHint && (
            <span className="text-[10px] px-1.5 py-0.5 glass-inset rounded" style={{ color: 'var(--text-tertiary)' }}>{entry.pricingHint}</span>
          )}
        </div>
      </div>
    </label>
  )
}
