import { createContext, useCallback, useContext, useState } from 'react'
import { createPortal } from 'react-dom'

// 轻提示（规范 §2.11）。成功 emerald / 失败 rose / 信息 blue。
// 桌面右上、移动顶部居中；3s 自动消失，最多叠 3 条。
const ToastCtx = createContext(() => {})

export function useToast() {
  return useContext(ToastCtx)
}

export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([])

  const toast = useCallback((type, message) => {
    const id = Date.now() + Math.random()
    setToasts((prev) => [...prev.slice(-2), { id, type, message }])
    setTimeout(() => setToasts((prev) => prev.filter((t) => t.id !== id)), 3000)
  }, [])

  return (
    <ToastCtx.Provider value={toast}>
      {children}
      {createPortal(
        <div className="pointer-events-none fixed inset-x-0 top-4 z-[60] flex flex-col items-center gap-3 px-4 sm:items-end sm:px-6">
          {toasts.map((t) => (
            <div
              key={t.id}
              role={t.type === 'error' ? 'alert' : 'status'}
              className={`pointer-events-auto w-full max-w-sm rounded-xl px-4 py-2.5 text-sm font-medium shadow-bento ${
                t.type === 'success'
                  ? 'bg-emerald-500 text-white'
                  : t.type === 'error'
                    ? 'bg-rose-500 text-white'
                    : 'bg-blue-500 text-white'
              }`}
            >
              {t.message}
            </div>
          ))}
        </div>,
        document.body,
      )}
    </ToastCtx.Provider>
  )
}
