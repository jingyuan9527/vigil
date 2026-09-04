// 统一样式的分页条：上一页 / 页码(窗口+省略) / 下一页，附总数提示。
// 页数很多时只展示当前附近页码与首尾页，避免分页条过宽溢出页面。
// 当总页数 <=1 时返回 null（列表短无需分页）。
export default function Pagination({ page, total, pageSize = 12, onChange }) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  if (total <= pageSize) return null

  const cur = Math.min(Math.max(1, page), totalPages)
  const from = (cur - 1) * pageSize + 1
  const to = Math.min(cur * pageSize, total)

  const btn =
    'inline-flex h-9 min-w-9 items-center justify-center rounded-xl px-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40'
  const ghost = 'text-zinc-500 hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800'
  const active = 'bg-zinc-900 text-white dark:bg-white dark:text-zinc-900'
  const ellipsis = 'px-1 text-zinc-300 dark:text-zinc-600'

  // 页码窗口：1 … cur-1 cur cur+1 … totalPages
  const pages = []
  for (let p = 1; p <= totalPages; p++) {
    if (p === 1 || p === totalPages || Math.abs(p - cur) <= 1) pages.push(p)
  }
  const items = []
  let last = 0
  for (const p of pages) {
    if (last && p - last > 1) items.push(<span key={'e' + p} className={ellipsis}>…</span>)
    items.push(
      <button
        key={p}
        onClick={() => onChange(p)}
        aria-current={p === cur ? 'page' : undefined}
        className={`${btn} ${p === cur ? active : ghost}`}
      >
        {p}
      </button>,
    )
    last = p
  }

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 pt-1">
      <span className="text-xs text-zinc-400 dark:text-zinc-500">
        共 {total} 项 · 第 {from}–{to} 项
      </span>
      <nav className="flex items-center gap-1.5" aria-label="分页">
        <button className={`${btn} ${ghost}`} onClick={() => onChange(cur - 1)} disabled={cur <= 1}>
          ← 上一页
        </button>
        {items}
        <button className={`${btn} ${ghost}`} onClick={() => onChange(cur + 1)} disabled={cur >= totalPages}>
          下一页 →
        </button>
      </nav>
    </div>
  )
}
