import { useState } from 'react'
import type { Issue } from '../types'

interface Props {
  issue: Issue
}

const severityConfig = {
  Critical: { bg: 'bg-red-50 dark:bg-red-900/20', border: 'border-red-400', icon: '!', iconBg: 'bg-red-500' },
  Warning: { bg: 'bg-yellow-50 dark:bg-yellow-900/20', border: 'border-yellow-400', icon: '!', iconBg: 'bg-yellow-500' },
  Info: { bg: 'bg-blue-50 dark:bg-blue-900/20', border: 'border-blue-400', icon: 'i', iconBg: 'bg-blue-500' },
}

export function IssueRow({ issue }: Props) {
  const [copied, setCopied] = useState(false)
  const cfg = severityConfig[issue.severity] ?? severityConfig.Info

  const copyCommand = () => {
    const cmd = extractCommand(issue.suggestion)
    if (cmd) {
      navigator.clipboard.writeText(cmd).then(() => {
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      })
    }
  }

  const cmd = extractCommand(issue.suggestion)

  return (
    <div className={`px-4 py-3 border-l-4 ${cfg.border} ${cfg.bg}`}>
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-start gap-2 min-w-0">
          <span className={`${cfg.iconBg} text-white text-xs w-5 h-5 rounded-full flex items-center justify-center flex-shrink-0 mt-0.5 font-bold`}>
            {cfg.icon}
          </span>
          <div className="min-w-0">
            <div className="font-medium text-sm flex items-center gap-1.5">
              <span>
                <span className="text-gray-500 dark:text-gray-400">{issue.namespace}/</span>
                {issue.resource}
              </span>
              {issue.aiEnhanced && (
                <span className="inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-semibold bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300">
                  AI
                </span>
              )}
            </div>
            <div className="text-sm text-gray-600 dark:text-gray-300 mt-0.5">{issue.message}</div>
            {issue.rootCause && (
              <div className="text-xs text-gray-600 dark:text-gray-400 mt-1 italic">
                Root cause: {issue.rootCause}
              </div>
            )}
            {issue.suggestion && (
              <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">{issue.suggestion}</div>
            )}
            {cmd && (
              <div className="mt-2 flex items-center gap-2">
                <code className="text-xs bg-gray-100 dark:bg-gray-900 px-2 py-1 rounded font-mono break-all">
                  {cmd}
                </code>
                <button
                  onClick={copyCommand}
                  className="text-xs text-indigo-600 dark:text-indigo-400 hover:underline flex-shrink-0"
                >
                  {copied ? 'Copied!' : 'Copy'}
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

function extractCommand(suggestion: string): string | null {
  const match = suggestion.match(/`(kubectl[^`]+)`/)
  return match ? match[1] : null
}
