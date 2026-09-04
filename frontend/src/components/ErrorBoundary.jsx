import { Component } from 'react'

// 全局错误边界：捕获子树渲染异常，避免整个 SPA 白屏。
export default class ErrorBoundary extends Component {
  constructor(props) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error) {
    return { hasError: true, error }
  }

  componentDidCatch(error, info) {
    // eslint-disable-next-line no-console
    console.error('[ErrorBoundary]', error, info.componentStack)
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex h-full items-center justify-center p-8">
          <div className="text-center">
            <p className="text-lg font-semibold text-zinc-800 dark:text-zinc-100">页面加载出错</p>
            <p className="mt-2 text-sm text-zinc-500 dark:text-zinc-400">
              {this.state.error?.message || '未知错误'}
            </p>
            <button
              onClick={() => window.location.reload()}
              className="mt-4 rounded-xl border border-zinc-200 px-4 py-2 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
            >
              刷新页面
            </button>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}
