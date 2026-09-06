import BentoCard from './BentoCard'

// 统一错误态：与空态（BentoCard 居中文案）同构，可选重试。
// 页面数据加载失败时必须呈现此态，禁止 catch 后静默让用户面对永久转圈或空页面。
export default function ErrorState({ message = '加载失败，请稍后重试', onRetry }) {
  return (
    <BentoCard className="py-10 text-center">
      <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-rose-50 text-rose-400 dark:bg-rose-500/10 dark:text-rose-400/80">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <circle cx="12" cy="12" r="9" /><path d="M12 8v4" /><path d="M12 16h.01" />
        </svg>
      </div>
      <p className="mt-4 text-sm font-medium text-zinc-600 dark:text-zinc-300">{message}</p>
      {onRetry && (
        <button
          onClick={onRetry}
          className="mt-4 rounded-xl border border-zinc-200 px-4 py-2 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-50 active:scale-95 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
        >
          重试
        </button>
      )}
    </BentoCard>
  )
}
