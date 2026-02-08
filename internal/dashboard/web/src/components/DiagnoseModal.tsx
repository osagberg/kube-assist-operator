import { useState, useEffect } from 'react'
import type { TargetKind, TroubleshootAction, CreateTroubleshootRequest } from '../types'
import { createTroubleshootRequest } from '../api/client'
import { showToast } from './Toast'

const TARGET_KINDS: TargetKind[] = ['Deployment', 'StatefulSet', 'DaemonSet', 'Pod', 'ReplicaSet']
const ACTIONS: TroubleshootAction[] = ['diagnose', 'logs', 'events', 'describe', 'all']

interface Props {
  open: boolean
  onClose: () => void
  prefill?: {
    namespace?: string
    targetKind?: TargetKind
    targetName?: string
  }
}

export function DiagnoseModal({ open, onClose, prefill }: Props) {
  const [namespace, setNamespace] = useState('default')
  const [targetKind, setTargetKind] = useState<TargetKind>('Deployment')
  const [targetName, setTargetName] = useState('')
  const [actions, setActions] = useState<TroubleshootAction[]>(['diagnose'])
  const [tailLines, setTailLines] = useState(100)
  const [ttl, setTtl] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (open) {
      // Always reset to defaults first
      setNamespace('default')
      setTargetKind('Deployment')
      setTargetName('')
      setActions(['diagnose'])
      setTailLines(100)
      setTtl('')
      setError('')
      // Then overlay prefill fields if provided
      if (prefill) {
        if (prefill.namespace) setNamespace(prefill.namespace)
        if (prefill.targetKind) setTargetKind(prefill.targetKind)
        if (prefill.targetName) setTargetName(prefill.targetName)
      }
    }
  }, [open, prefill])

  if (!open) return null

  const toggleAction = (action: TroubleshootAction) => {
    if (action === 'all') {
      setActions((prev) => (prev.includes('all') ? ['diagnose'] : ['all']))
      return
    }
    setActions((prev) => {
      const filtered = prev.filter((a) => a !== 'all')
      if (filtered.includes(action)) {
        const next = filtered.filter((a) => a !== action)
        return next.length === 0 ? ['diagnose'] : next
      }
      return [...filtered, action]
    })
  }

  const handleSubmit = async () => {
    if (!targetName.trim()) {
      setError('Target name is required')
      return
    }
    setSubmitting(true)
    setError('')
    try {
      const body: CreateTroubleshootRequest = {
        namespace,
        target: { kind: targetKind, name: targetName.trim() },
        actions,
        tailLines,
      }
      if (ttl && parseInt(ttl, 10) > 0) {
        body.ttlSecondsAfterFinished = parseInt(ttl, 10)
      }
      const resp = await createTroubleshootRequest(body)
      showToast(`TroubleshootRequest "${resp.name}" created in ${resp.namespace}`, 'success')
      onClose()
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to create request'
      setError(msg)
      showToast(msg, 'error')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center glass-backdrop animate-fade-in"
      onClick={onClose}
      onKeyDown={(e) => { if (e.key === 'Escape') onClose() }}
      role="dialog"
      aria-modal="true"
      aria-labelledby="diagnose-title"
    >
      <div
        className="glass-elevated rounded-2xl animate-slide-up w-full max-w-lg p-6 space-y-4 max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 id="diagnose-title" className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
            Create TroubleshootRequest
          </h2>
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

        {/* Namespace */}
        <div>
          <label className="text-sm" style={{ color: 'var(--text-tertiary)' }}>Namespace</label>
          <input
            type="text"
            value={namespace}
            onChange={(e) => setNamespace(e.target.value)}
            placeholder="default"
            className="w-full mt-1 px-3 py-2 glass-inset rounded-xl text-sm transition-all duration-200"
            style={{ color: 'var(--text-primary)' }}
          />
        </div>

        {/* Target Kind */}
        <div>
          <label className="text-sm" style={{ color: 'var(--text-tertiary)' }}>Target Kind</label>
          <select
            value={targetKind}
            onChange={(e) => setTargetKind(e.target.value as TargetKind)}
            className="w-full mt-1 px-3 py-2 glass-inset rounded-xl text-sm transition-all duration-200"
            style={{ color: 'var(--text-primary)' }}
          >
            {TARGET_KINDS.map((k) => (
              <option key={k} value={k}>{k}</option>
            ))}
          </select>
        </div>

        {/* Target Name */}
        <div>
          <label className="text-sm" style={{ color: 'var(--text-tertiary)' }}>Target Name <span className="text-severity-critical">*</span></label>
          <input
            type="text"
            value={targetName}
            onChange={(e) => setTargetName(e.target.value)}
            placeholder="my-deployment"
            className="w-full mt-1 px-3 py-2 glass-inset rounded-xl text-sm transition-all duration-200"
            style={{ color: 'var(--text-primary)' }}
            required
          />
        </div>

        {/* Actions */}
        <div>
          <label className="text-sm" style={{ color: 'var(--text-tertiary)' }}>Actions</label>
          <div className="mt-2 flex flex-wrap gap-2">
            {ACTIONS.map((action) => (
              <label key={action} className="flex items-center gap-1.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={actions.includes(action) || (action !== 'all' && actions.includes('all'))}
                  disabled={action !== 'all' && actions.includes('all')}
                  onChange={() => toggleAction(action)}
                  className="rounded transition-all duration-200"
                />
                <span className="text-sm" style={{ color: 'var(--text-secondary)' }}>{action}</span>
              </label>
            ))}
          </div>
        </div>

        {/* Tail Lines */}
        <div>
          <label className="text-sm" style={{ color: 'var(--text-tertiary)' }}>Tail Lines</label>
          <input
            type="number"
            value={tailLines}
            onChange={(e) => {
              const v = parseInt(e.target.value, 10)
              if (!isNaN(v)) setTailLines(Math.max(1, Math.min(10000, v)))
            }}
            min={1}
            max={10000}
            className="w-full mt-1 px-3 py-2 glass-inset rounded-xl text-sm transition-all duration-200"
            style={{ color: 'var(--text-primary)' }}
          />
        </div>

        {/* TTL */}
        <div>
          <label className="text-sm" style={{ color: 'var(--text-tertiary)' }}>
            TTL (seconds after finished) <span className="font-normal" style={{ color: 'var(--text-tertiary)' }}>(optional)</span>
          </label>
          <input
            type="number"
            value={ttl}
            onChange={(e) => setTtl(e.target.value)}
            placeholder="Leave empty for no auto-delete"
            min={0}
            className="w-full mt-1 px-3 py-2 glass-inset rounded-xl text-sm transition-all duration-200"
            style={{ color: 'var(--text-primary)' }}
          />
        </div>

        <div className="flex justify-end gap-2 pt-2">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm glass-button rounded-lg transition-all duration-200"
            style={{ color: 'var(--text-secondary)' }}
          >
            Cancel
          </button>
          <button
            onClick={handleSubmit}
            disabled={submitting || !targetName.trim()}
            className="px-4 py-2 text-sm glass-button-primary rounded-lg transition-all duration-200 disabled:opacity-50"
          >
            {submitting ? 'Creating...' : 'Create'}
          </button>
        </div>
      </div>
    </div>
  )
}
