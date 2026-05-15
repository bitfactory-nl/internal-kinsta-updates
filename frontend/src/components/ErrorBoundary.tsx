import { Component } from 'react'
import type { ReactNode } from 'react'

interface Props { children: ReactNode; label?: string }
interface State { error: string | null }

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(err: unknown): State {
    return { error: err instanceof Error ? err.message : String(err) }
  }

  render() {
    if (this.state.error) {
      return (
        <div className="flex-1 flex flex-col items-center justify-center gap-3 p-6 text-center">
          <p className="text-red-600 text-sm font-semibold">
            {this.props.label ?? 'Render error'}
          </p>
          <pre className="text-xs text-red-700/70 bg-red-100 rounded-lg p-3 max-w-lg text-left whitespace-pre-wrap break-all">
            {this.state.error}
          </pre>
          <button
            onClick={() => this.setState({ error: null })}
            className="text-xs text-gray-600 hover:text-gray-800 transition-colors"
          >
            Retry
          </button>
        </div>
      )
    }
    return this.props.children
  }
}
