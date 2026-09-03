export default function Spinner({ label = '加载中…' }) {
  return (
    <div className="flex items-center justify-center gap-3 py-16 text-zinc-500 dark:text-zinc-400">
      <span className="h-5 w-5 animate-spin rounded-full border-2 border-zinc-300 border-t-bento-accent dark:border-zinc-600 dark:border-t-bento-accent" />
      <span className="text-sm">{label}</span>
    </div>
  )
}
