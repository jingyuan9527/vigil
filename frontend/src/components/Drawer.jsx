import { useEffect } from 'react'
import { createPortal } from 'react-dom'

// 表单抽屉（设计规范 §2.10 的模态在「表单」场景的延伸）：
// 桌面（≥lg）从右侧滑出固定宽度面板；移动端为底部抽屉，内部滚动 + 安全区底 padding。
// 与 ConfirmDialog 同构的语义（role=dialog / aria-modal / Esc 关闭 / 遮罩点击关闭），
// 目的是让「新增/编辑」不再整块替换列表，用户始终保留列表上下文。
export default function Drawer({ open, title, description, onClose, footer, children }) {
  useEffect(() => {
    if (!open) return
    const onKey = (e) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose?.()
      }
    }
    window.addEventListener('keydown', onKey)
    // 抽屉打开时锁定页面滚动，避免背景跟随滚动造成「双层滚动」错觉
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = prevOverflow
    }
  }, [open, onClose])

  if (!open) return null

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-end justify-center lg:items-stretch lg:justify-end">
      <div className="drawer-overlay-in absolute inset-0 bg-black/40" onClick={onClose} aria-hidden="true" />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="drawer-in relative z-10 flex max-h-[90dvh] w-full flex-col rounded-t-2xl border border-zinc-100 bg-white shadow-bento dark:border-zinc-800 dark:bg-zinc-900 lg:h-full lg:max-h-none lg:w-[30rem] lg:rounded-l-2xl lg:rounded-tr-none lg:border-y-0 lg:border-r-0"
      >
        <header className="flex shrink-0 items-start justify-between gap-3 border-b border-zinc-100 px-5 py-4 dark:border-zinc-800">
          <div className="min-w-0">
            <h3 className="text-base font-semibold tracking-tight text-zinc-900 dark:text-zinc-100">{title}</h3>
            {description && <p className="mt-1 text-xs leading-relaxed text-zinc-400 dark:text-zinc-500">{description}</p>}
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭"
            className="-mr-1.5 flex h-11 w-11 shrink-0 items-center justify-center rounded-xl text-zinc-400 transition-colors hover:bg-zinc-100 hover:text-zinc-600 dark:text-zinc-500 dark:hover:bg-zinc-800 dark:hover:text-zinc-300"
          >
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
              <path d="M18 6 6 18M6 6l12 12" />
            </svg>
          </button>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5">{children}</div>

        {footer && (
          <footer className="shrink-0 border-t border-zinc-100 px-5 py-4 pb-[calc(1rem+env(safe-area-inset-bottom))] dark:border-zinc-800 lg:pb-4">
            {footer}
          </footer>
        )}
      </div>
    </div>,
    document.body,
  )
}
