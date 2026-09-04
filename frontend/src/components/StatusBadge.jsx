// 状态徽章（规范 §2.4 / 规则 A）：状态色板全局唯一，色 + 文案双通道表达。
const MAP = {
  'up-to-date': {
    label: '已是最新',
    cls: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300',
    dot: 'bg-emerald-500',
  },
  'update-available': {
    label: '有更新',
    cls: 'bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
    dot: 'bg-amber-500',
  },
  'new-tag': {
    label: '可选更新',
    cls: 'bg-blue-50 text-blue-700 dark:bg-blue-500/15 dark:text-blue-300',
    dot: 'bg-blue-500',
  },
  unknown: {
    label: '未知',
    cls: 'bg-zinc-100 text-zinc-600 dark:bg-zinc-700/40 dark:text-zinc-300',
    dot: 'bg-zinc-400',
  },
  stale: {
    label: '缺失',
    cls: 'bg-rose-50 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300',
    dot: 'bg-rose-500',
  },
  ignored: {
    label: '已忽略',
    cls: 'bg-zinc-100 text-zinc-500 dark:bg-zinc-700/40 dark:text-zinc-400',
    dot: 'bg-zinc-400',
  },
}

export default function StatusBadge({ status }) {
  const m = MAP[status] || MAP.unknown
  return (
    <span className={`inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-xl px-2.5 py-1 text-xs font-medium ${m.cls}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${m.dot}`} />
      {m.label}
    </span>
  )
}
