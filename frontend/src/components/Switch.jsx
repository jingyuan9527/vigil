// 全局唯一的开关控件（设计规范 §2.3）：role=switch + aria-checked，支持 md/sm 两档。
// 抽成组件是为了消除「设置页 Toggle」与「渠道行内开关」两套实现带来的样式漂移。
// 触控目标：视觉高度仅 20/24px，用伪元素向四周扩展 12px 命中区，满足移动端 ≥44px。
export default function Switch({ checked, onChange, disabled = false, size = 'md', label, className = '' }) {
  const s =
    size === 'sm'
      ? { track: 'h-5 w-9', thumb: 'h-4 w-4', on: 'translate-x-[18px]', off: 'translate-x-0.5' }
      : { track: 'h-6 w-11', thumb: 'h-5 w-5', on: 'translate-x-5', off: 'translate-x-0.5' }

  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex shrink-0 items-center rounded-full transition-colors after:absolute after:-inset-3 after:content-[''] disabled:cursor-not-allowed disabled:opacity-60 ${
        s.track
      } ${checked ? 'bg-blue-600' : 'bg-zinc-300 dark:bg-zinc-700'} ${className}`}
    >
      <span
        className={`inline-block transform rounded-full bg-white shadow transition-transform ${s.thumb} ${
          checked ? s.on : s.off
        }`}
      />
    </button>
  )
}
