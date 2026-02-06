import type { HealthUpdate } from '../types'

interface Props {
  health: HealthUpdate
}

export function ExportButton({ health }: Props) {
  const exportJSON = () => {
    const blob = new Blob([JSON.stringify(health, null, 2)], { type: 'application/json' })
    download(blob, 'kube-assist-health.json')
  }

  const exportCSV = () => {
    const rows = [['Checker', 'Severity', 'Namespace', 'Resource', 'Message', 'Suggestion']]
    for (const [name, result] of Object.entries(health.results)) {
      for (const issue of result.issues) {
        rows.push([name, issue.severity, issue.namespace, issue.resource, issue.message, issue.suggestion])
      }
    }
    const csv = rows.map((r) => r.map((c) => `"${c.replace(/"/g, '""')}"`).join(',')).join('\n')
    const blob = new Blob([csv], { type: 'text/csv' })
    download(blob, 'kube-assist-health.csv')
  }

  return (
    <div className="flex gap-1">
      <button
        onClick={exportJSON}
        className="px-2 py-1 text-xs rounded border border-gray-200 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-700"
      >
        JSON
      </button>
      <button
        onClick={exportCSV}
        className="px-2 py-1 text-xs rounded border border-gray-200 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-700"
      >
        CSV
      </button>
    </div>
  )
}

function download(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}
