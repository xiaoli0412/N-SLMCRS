import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Button } from './ui'

// v0.13 生产稳定性：全局 React 错误边界，捕获子树渲染错误，
// 避免整页白屏；展示友好回退 + 可复制错误信息，供运维定位。
interface Props {
  children: ReactNode
}
interface State {
  hasError: boolean
  error?: Error
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // 控制台留痕，便于从浏览器开发者工具定位（不外发，单节点无收集端）。
    // eslint-disable-next-line no-console
    console.error('[ErrorBoundary] render error:', error, info.componentStack)
  }

  handleReset = () => {
    this.setState({ hasError: false, error: undefined })
  }

  render() {
    if (!this.state.hasError) return this.props.children
    const msg = this.state.error?.message || '未知错误'
    return (
      <div className="flex min-h-[60vh] flex-col items-center justify-center gap-4 p-8 text-center">
        <div className="rounded-lg border bg-card p-8 shadow-sm">
          <h2 className="text-lg font-semibold">页面渲染出错</h2>
          <p className="mt-2 max-w-md text-sm text-muted-foreground">
            该视图遇到意外错误。可重试或刷新整页；若反复出现，请把下方信息反馈运维。
          </p>
          <pre className="mt-4 max-w-md overflow-auto rounded bg-muted p-3 text-left text-xs text-muted-foreground">
            {msg}
          </pre>
          <div className="mt-4 flex justify-center gap-2">
            <Button size="sm" variant="outline" onClick={this.handleReset}>
              重试
            </Button>
            <Button size="sm" onClick={() => location.reload()}>
              刷新整页
            </Button>
          </div>
        </div>
      </div>
    )
  }
}
