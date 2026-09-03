const MAP = {
  'up-to-date': {
    label: '已是最新',
    cls: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300',
    dot: 'bg-emerald-500',
  },
  'update-available': {
    label: '有更新',
    cls: 'bg-orange-100 text-orange-700 dark:bg-orange-500/15 dark:text-orange-300',
    dot: 'bg-orange-500',
  },
  unknown: {
    label: '未知',
    cls: 'bg-zinc-100 text-zinc-600 dark:bg-zinc-700/40 dark:text-zinc-300',
    dot: 'bg-zinc-400',
  },
  stale: {
    label: '缺失',
    cls: 'bg-rose-100 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300',
    dot: 'bg-rose-500',
  },
}

export default function StatusBadge({ status }) {
  const m = MAP[status] || MAP.unknown
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-xl px-2.5 py-1 text-xs font-medium ${m.cls}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${m.dot}`} />
      {m.label}
    </span>
  )
}
