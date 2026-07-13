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
          <p className="text-red text-[13px] font-semibold">
            {this.props.label ?? 'Render error'}
          </p>
          <pre className="text-[12px] font-mono text-red bg-red-soft border border-border rounded-[11px] p-3.5 max-w-lg text-left whitespace-pre-wrap break-all">
            {this.state.error}
          </pre>
          <button
            onClick={() => this.setState({ error: null })}
            className="bg-panel-2 border border-border text-fg-muted text-[12.5px] font-semibold px-[15px] py-[9px]
                       rounded-[9px] hover:bg-hover transition-colors"
          >
            Retry
          </button>
        </div>
      )
    }
    return this.props.children
  }
}
