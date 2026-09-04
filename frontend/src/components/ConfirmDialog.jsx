import { useEffect } from 'react'
import { createPortal } from 'react-dom'

// 统一移除/危险操作的二次确认对话框（规范 §2.10 / 规则 J）。
// 桌面：居中模态；移动端（<sm）：底部抽屉（bottom sheet），带安全区 padding。
// 键盘：Esc 取消，Enter 主操作。
export default function ConfirmDialog({
  open,
  title,
  description,
  confirmText = '确认',
  cancelText = '取消',
  danger = false,
  onConfirm,
  onCancel,
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (e) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onCancel?.()
      } else if (e.key === 'Enter') {
        // 阻止聚焦按钮的原生 click，避免与 onConfirm 双触发
        e.preventDefault()
        onConfirm?.()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onCancel, onConfirm])

  if (!open) return null

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-end justify-center sm:items-center">
      <div className="absolute inset-0 bg-black/40" onClick={onCancel} aria-hidden="true" />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="relative z-10 w-full rounded-t-2xl border border-zinc-100 bg-white p-6 pb-[calc(1.5rem+env(safe-area-inset-bottom))] shadow-bento dark:border-zinc-800 dark:bg-zinc-900 sm:max-w-sm sm:rounded-2xl"
      >
        <h3 className="text-lg font-semibold tracking-tight text-zinc-900 dark:text-zinc-100">{title}</h3>
        {description && (
          <p className="mt-2 text-sm leading-relaxed text-zinc-500 dark:text-zinc-400">{description}</p>
        )}
        <div className="mt-6 flex gap-3">
          <button
            type="button"
            onClick={onCancel}
            className="flex-1 rounded-xl border border-zinc-200 py-2.5 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-50 active:scale-95 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
          >
            {cancelText}
          </button>
          <button
            type="button"
            onClick={onConfirm}
            className={`flex-1 rounded-xl py-2.5 text-sm font-medium text-white transition-colors active:scale-95 ${
              danger
                ? 'bg-rose-500 hover:bg-rose-600'
                : 'bg-zinc-900 hover:bg-zinc-700 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200'
            }`}
          >
            {confirmText}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
