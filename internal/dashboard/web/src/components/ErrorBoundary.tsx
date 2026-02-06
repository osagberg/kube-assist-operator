import { Component, type ErrorInfo, type ReactNode } from 'react'

interface Props {
  children: ReactNode
}

interface State {
  hasError: boolean
  error: Error | null
}

class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('ErrorBoundary caught an error:', error, errorInfo)
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex flex-col items-center justify-center min-h-[400px] p-8">
          <div className="glass-panel rounded-xl p-6 max-w-md text-center" style={{ background: 'rgba(239, 68, 68, 0.08)' }}>
            <h2 className="text-lg font-semibold text-severity-critical mb-2">
              Something went wrong
            </h2>
            <p className="text-sm text-severity-critical mb-4" style={{ opacity: 0.8 }}>
              {this.state.error?.message || 'An unexpected error occurred while rendering the dashboard.'}
            </p>
            <button
              onClick={() => this.setState({ hasError: false, error: null })}
              className="px-4 py-2 glass-button-primary rounded-lg text-sm transition-all duration-200"
              aria-label="Retry loading the dashboard"
            >
              Try Again
            </button>
          </div>
        </div>
      )
    }

    return this.props.children
  }
}

export { ErrorBoundary }
