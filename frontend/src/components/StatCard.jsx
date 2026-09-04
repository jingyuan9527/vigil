import BentoCard from './BentoCard'

// 统计卡片：icon 容器使用渐变强调色（规范 §1.1.2 三套渐变之一），数字突出。
export default function StatCard({ label, value, icon, gradient = 'from-blue-500 to-violet-600', hint }) {
  return (
    <BentoCard>
      <div className="flex items-start justify-between">
        <div>
          <div className="text-sm text-zinc-500 dark:text-zinc-400">{label}</div>
          <div className="mt-2 text-3xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 tabular-nums">{value}</div>
          {hint && <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">{hint}</div>}
        </div>
        <div className={`flex h-11 w-11 items-center justify-center rounded-2xl bg-gradient-to-br ${gradient} text-white shadow-bento transition-transform duration-200 group-hover:scale-110`}>
          {icon}
        </div>
      </div>
    </BentoCard>
  )
}
