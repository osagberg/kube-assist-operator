import { useEffect } from 'react'

interface Props {
  onToggleTheme: () => void
  onRefresh: () => void
  onFocusSearch: () => void
  onFocusNamespace: () => void
  onTogglePause: () => void
  onSeverity: (index: number) => void
  onShowHelp: () => void
}

export function useKeyboardShortcuts({
  onToggleTheme,
  onRefresh,
  onFocusSearch,
  onFocusNamespace,
  onTogglePause,
  onSeverity,
  onShowHelp,
}: Props) {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Skip when typing in inputs
      const tag = (e.target as HTMLElement).tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') {
        if (e.key === 'Escape') {
          ;(e.target as HTMLElement).blur()
        }
        return
      }

      switch (e.key) {
        case '/':
          e.preventDefault()
          onFocusSearch()
          break
        case 'f':
          e.preventDefault()
          onFocusNamespace()
          break
        case 't':
          onToggleTheme()
          break
        case 'p':
          onTogglePause()
          break
        case 'r':
          onRefresh()
          break
        case '1':
          onSeverity(0)
          break
        case '2':
          onSeverity(1)
          break
        case '3':
          onSeverity(2)
          break
        case '4':
          onSeverity(3)
          break
        case '?':
          onShowHelp()
          break
        case 'Escape':
          // handled by modals
          break
      }
    }

    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [onToggleTheme, onRefresh, onFocusSearch, onFocusNamespace, onTogglePause, onSeverity, onShowHelp])
}

interface HelpProps {
  open: boolean
  onClose: () => void
}

const shortcuts = [
  ['/', 'Focus search'],
  ['f', 'Focus namespace filter'],
  ['t', 'Toggle theme'],
  ['p', 'Pause/resume updates'],
  ['r', 'Refresh data'],
  ['1-4', 'Severity filter (All/Critical/Warning/Info)'],
  ['?', 'Show this help'],
  ['Esc', 'Close modal / blur input'],
]

export function KeyboardShortcutsHelp({ open, onClose }: HelpProps) {
  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        className="bg-white dark:bg-gray-800 rounded-xl shadow-2xl w-full max-w-sm p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-semibold mb-4">Keyboard Shortcuts</h2>
        <div className="space-y-2">
          {shortcuts.map(([key, desc]) => (
            <div key={key} className="flex items-center justify-between text-sm">
              <kbd className="bg-gray-100 dark:bg-gray-700 px-2 py-0.5 rounded font-mono text-xs">{key}</kbd>
              <span className="text-gray-600 dark:text-gray-300">{desc}</span>
            </div>
          ))}
        </div>
        <button
          onClick={onClose}
          className="mt-4 w-full py-2 text-sm rounded-lg bg-indigo-600 text-white hover:bg-indigo-700"
        >
          Close
        </button>
      </div>
    </div>
  )
}
