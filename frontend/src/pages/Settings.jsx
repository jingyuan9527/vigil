import { useEffect, useState } from 'react'
import { api } from '../api/client'
import BentoCard from '../components/BentoCard'
import Spinner from '../components/Spinner'
import ErrorState from '../components/ErrorState'
import Switch from '../components/Switch'
import ChannelManager from '../components/ChannelManager'
import { useToast } from '../components/Toast'

// 间隔单位与秒数的换算（展示层概念，落库仍是秒）
const INTERVAL_UNITS = [
  { label: '秒', sec: 1 },
  { label: '分钟', sec: 60 },
  { label: '小时', sec: 3600 },
  { label: '天', sec: 86400 },
]

// 从秒数选最大能整除的单位（3600 → 1 小时，5400 → 90 分钟）
function splitInterval(sec) {
  sec = Math.max(0, sec || 0)
  for (const u of [...INTERVAL_UNITS].reverse()) {
    if (sec > 0 && sec % u.sec === 0) return { v: sec / u.sec, unit: u.sec }
  }
  return { v: sec, unit: 1 }
}

function humanInterval(sec) {
  const s = splitInterval(sec)
  const u = INTERVAL_UNITS.find((x) => x.sec === s.unit)
  return `${s.v} ${u.label}`
}

// —— 每天定时时刻选择：自定义「时 : 分」两个下拉，替代原生 <input type="time">。
//    原生时间输入弹层在录屏/截图中不显示且外观不可定制，这里改为完全可录制的
//    原生下拉（小时按时段分组），样式与整体 Bento 风格保持一致。 ——
const HOUR_GROUPS = [
  { label: '凌晨', start: 0, end: 5 },
  { label: '早晨', start: 6, end: 11 },
  { label: '下午', start: 12, end: 17 },
  { label: '晚上', start: 18, end: 23 },
]

const pad2 = (n) => String(n).padStart(2, '0')

// 解析 "HH:MM"（容忍 "H:MM" 单数字小时）；非法返回 null
function parseDailyTime(s) {
  const m = /^(\d{1,2}):(\d{1,2})$/.exec(s || '')
  if (!m) return null
  const h = Number(m[1])
  const min = Number(m[2])
  if (h < 0 || h > 23 || min < 0 || min > 59) return null
  return { hour: h, minute: min }
}

// 时刻选择器：小时（按时段分组）+ 分钟两个下拉，onChange 输出规范化的 HH:MM
function TimeSelect({ value, onChange }) {
  const t = parseDailyTime(value) || { hour: 0, minute: 0 }
  const pick = (part, v) =>
    onChange(`${pad2(part === 'hour' ? v : t.hour)}:${pad2(part === 'minute' ? v : t.minute)}`)
  return (
    <div className="mt-4 flex flex-wrap items-center gap-3">
      <div className="inline-flex items-center gap-0.5 rounded-xl border border-zinc-200 bg-zinc-50 py-1.5 pl-2.5 pr-1.5 dark:border-zinc-700 dark:bg-zinc-800">
        {/* 时钟图标 */}
        <svg
          className="mr-0.5 h-4 w-4 shrink-0 text-zinc-400 dark:text-zinc-500"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <circle cx="12" cy="12" r="9" />
          <path d="M12 7v5l3 2" />
        </svg>
        <select
          aria-label="小时"
          value={t.hour}
          onChange={(e) => pick('hour', Number(e.target.value))}
          className="w-14 cursor-pointer bg-transparent py-1 text-center text-sm font-medium text-zinc-900 outline-none dark:text-zinc-100"
        >
          {HOUR_GROUPS.map((g) => (
            <optgroup key={g.label} label={g.label}>
              {Array.from({ length: g.end - g.start + 1 }, (_, i) => g.start + i).map((h) => (
                <option key={h} value={h}>{pad2(h)}</option>
              ))}
            </optgroup>
          ))}
        </select>
        <span className="select-none pb-0.5 text-sm font-semibold text-zinc-400 dark:text-zinc-500">:</span>
        <select
          aria-label="分钟"
          value={t.minute}
          onChange={(e) => pick('minute', Number(e.target.value))}
          className="w-14 cursor-pointer bg-transparent py-1 text-center text-sm font-medium text-zinc-900 outline-none dark:text-zinc-100"
        >
          {Array.from({ length: 60 }, (_, i) => (
            <option key={i} value={i}>{pad2(i)}</option>
          ))}
        </select>
      </div>
      <span className="text-xs text-zinc-400 dark:text-zinc-500">每天在该时刻扫描一次（服务器本地时区）</span>
    </div>
  )
}

// 字段基础样式不含宽度：Tailwind 生成顺序按刻度而非书写顺序，w-full 会盖掉后续的 w-28，
// 因此宽度由调用处按需拼接（全宽用 INPUT_CLS，定宽用 FIELD_BASE + w-*）。
const FIELD_BASE =
  'rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2.5 text-sm text-zinc-900 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 disabled:cursor-not-allowed disabled:opacity-60 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100'
const INPUT_CLS = `w-full ${FIELD_BASE}`
const INPUT_ERR_CLS = 'border-rose-400 focus:border-rose-500 focus:ring-rose-500/20'

// 分组标识：卡片归属哪一段「改了什么」，保存前一眼可见
const SCAN_KEYS = ['scan_mode', 'scan_daily_time', 'scan_interval']
const DEMO_KEYS = ['disable_default_watch']
const REGISTRY_INSECURE_KEYS = ['registry_insecure']
const REGISTRY_MIRROR_KEYS = ['registry_mirror']

// 注册表镜像主机：host 或 host:port；仅拦明显非法输入，不校验可达性
const MIRROR_RE = /^[a-zA-Z0-9][a-zA-Z0-9.-]*(:\d{1,5})?$/

function SectionHead({ title, desc }) {
  return (
    <div className="shrink-0">
      <h2 className="text-sm font-semibold tracking-tight text-zinc-900 dark:text-zinc-100">{title}</h2>
      {desc && <p className="mt-0.5 text-xs text-zinc-400 dark:text-zinc-500">{desc}</p>}
    </div>
  )
}

function DirtyPill() {
  return (
    <span className="rounded-lg bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-500/15 dark:text-amber-300">
      已修改
    </span>
  )
}

export default function Settings() {
  const toast = useToast()
  const [form, setForm] = useState(null)
  const [saved, setSaved] = useState(null) // 最近一次保存的基线，用于「未保存更改」提示
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [loadError, setLoadError] = useState(false)
  // 间隔编辑草稿：值 + 单位（秒落库，单位仅前端换算）
  const [intervalDraft, setIntervalDraft] = useState({ v: 1, unit: 3600 })
  const [planError, setPlanError] = useState('')
  const [mirrorError, setMirrorError] = useState('')

  const syncDraft = (sec) => setIntervalDraft(splitInterval(sec))

  const loadSettings = async () => {
    setLoading(true)
    setLoadError(false)
    try {
      const s = await api.settings()
      setForm(s)
      setSaved(s)
      syncDraft(s.scan_interval)
      setPlanError('')
      setMirrorError('')
    } catch {
      // 失败必须可感知：form 为 null 时不再静默转圈
      setLoadError(true)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadSettings()
  }, [])

  const update = (patch) => setForm((f) => ({ ...f, ...patch }))
  const dirty = !!form && !!saved && JSON.stringify(form) !== JSON.stringify(saved)
  const changed = (keys) => !!form && !!saved && keys.some((k) => form[k] !== saved[k])

  // 有未保存改动时离开/刷新给出原生确认（浏览器只允许这种提示形式）
  useEffect(() => {
    if (!dirty) return
    const onBeforeUnload = (e) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirty])

  const discard = () => {
    setForm(saved)
    syncDraft(saved.scan_interval)
    setPlanError('')
    setMirrorError('')
    toast('info', '已放弃未保存的更改')
  }

  // 提交前校验：错误就地下沉到对应卡片，而不是统一提示条
  const validate = () => {
    let ok = true
    if ((form.scan_mode || 'interval') === 'interval' && form.scan_interval > 0 && form.scan_interval < 30) {
      setPlanError('自动扫描间隔至少 30 秒（填 0 可关闭自动扫描）')
      ok = false
    } else {
      setPlanError('')
    }
    const mirror = (form.registry_mirror || '').trim()
    if (mirror && !MIRROR_RE.test(mirror)) {
      setMirrorError('格式应为主机名或 host:port，如 mirror.example.com:5000')
      ok = false
    } else {
      setMirrorError('')
    }
    return ok
  }

  const onSave = async (e) => {
    e.preventDefault()
    if (!validate()) {
      toast('error', '请先修正标红的配置项')
      return
    }
    setSaving(true)
    try {
      const s = await api.saveSettings(form)
      setForm(s)
      setSaved(s)
      syncDraft(s.scan_interval)
      toast('success', '设置已保存，扫描计划立即生效')
    } catch {
      toast('error', '保存失败，请稍后重试')
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <Spinner label="加载设置…" />
  if (!form) return <ErrorState message="设置加载失败" onRetry={loadSettings} />

  return (
    <div className="flex h-full flex-col gap-4 overflow-hidden">
      <div className="shrink-0">
        <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">设置</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          调整运行参数。除通知渠道即时生效外，其余改动需点击底部「保存设置」后持久化（重启仍保留）。
        </p>
      </div>

      <form onSubmit={onSave} className="min-h-0 flex-1 space-y-6 overflow-y-auto px-1 pb-1">
        {/* ── 扫描 ── */}
        <section className="space-y-3">
          <SectionHead title="扫描" desc="决定何时检查镜像更新；周期扫描与每天定时二选一。" />
          <div className="bento-grid">
            <BentoCard span="wide" className={changed(SCAN_KEYS) ? 'ring-2 ring-amber-500/30' : ''}>
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2.5">
                    <span className="font-medium text-zinc-900 dark:text-zinc-100">扫描计划</span>
                    {changed(SCAN_KEYS) && <DirtyPill />}
                  </div>
                  <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
                    {form.scan_mode === 'daily'
                      ? `每天 ${form.scan_daily_time || '--:--'}（服务器本地时间）自动扫描一次`
                      : form.scan_interval > 0
                        ? `每 ${humanInterval(form.scan_interval)} 自动扫描一次`
                        : '自动扫描已关闭，仍可随时手动触发「立即扫描」'}
                  </div>
                </div>
                {/* 模式切换：两态分段按钮 */}
                <div className="flex shrink-0 rounded-xl bg-zinc-100 p-1 dark:bg-zinc-800" role="tablist" aria-label="扫描模式">
                  {[
                    { key: 'interval', label: '间隔扫描' },
                    { key: 'daily', label: '每天定时' },
                  ].map((m) => (
                    <button
                      key={m.key}
                      type="button"
                      role="tab"
                      aria-selected={(form.scan_mode || 'interval') === m.key}
                      onClick={() =>
                        update({
                          scan_mode: m.key,
                          // 切入定时模式时确保时刻合法：旧值缺失/非法时落到 00:00，
                          // 避免保存后 daily 扫描因时刻非法被挂起
                          scan_daily_time:
                            m.key === 'daily' && !parseDailyTime(form.scan_daily_time)
                              ? '00:00'
                              : form.scan_daily_time,
                        })
                      }
                      className={`rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                        (form.scan_mode || 'interval') === m.key
                          ? 'bg-white text-zinc-900 shadow-sm dark:bg-zinc-900 dark:text-zinc-100'
                          : 'text-zinc-500 hover:text-zinc-700 dark:text-zinc-400 dark:hover:text-zinc-200'
                      }`}
                    >
                      {m.label}
                    </button>
                  ))}
                </div>
              </div>

              {(form.scan_mode || 'interval') === 'daily' ? (
                <TimeSelect value={form.scan_daily_time} onChange={(v) => update({ scan_daily_time: v })} />
              ) : (
                <div className="mt-4 flex flex-wrap items-center gap-3">
                  <input
                    type="number"
                    min="0"
                    step="1"
                    aria-label="扫描间隔"
                    value={intervalDraft.v}
                    onChange={(e) => {
                      const v = Math.max(0, parseInt(e.target.value, 10) || 0)
                      setIntervalDraft((d) => ({ ...d, v }))
                      update({ scan_interval: v * intervalDraft.unit })
                      if (planError) setPlanError('')
                    }}
                    className={`${FIELD_BASE} w-28 text-right tabular-nums ${planError ? INPUT_ERR_CLS : ''}`}
                  />
                  <select
                    aria-label="间隔单位"
                    value={intervalDraft.unit}
                    onChange={(e) => {
                      const u = Number(e.target.value)
                      // 换单位保持真实秒数不变（1 小时 → 60 分钟），无法整除时就近取整
                      const total = intervalDraft.v * intervalDraft.unit
                      const v = Math.round(total / u)
                      setIntervalDraft({ v, unit: u })
                      update({ scan_interval: v * u })
                      if (planError) setPlanError('')
                    }}
                    className={`${FIELD_BASE} w-32 cursor-pointer`}
                  >
                    {INTERVAL_UNITS.map((u) => (
                      <option key={u.sec} value={u.sec}>{u.label}</option>
                    ))}
                  </select>
                  <span className="text-xs text-zinc-400 dark:text-zinc-500">设为 0 关闭自动扫描；启用时最小 30 秒</span>
                </div>
              )}
              {planError && (
                <p className="mt-2 flex items-center gap-1.5 text-xs text-rose-500">
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true">
                    <circle cx="12" cy="12" r="9" /><path d="M12 8v4" /><path d="M12 16h.01" />
                  </svg>
                  {planError}
                </p>
              )}
            </BentoCard>

            <BentoCard span="wide" className={changed(DEMO_KEYS) ? 'ring-2 ring-amber-500/30' : ''}>
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2.5">
                    <span className="font-medium text-zinc-900 dark:text-zinc-100">演示监控列表</span>
                    {changed(DEMO_KEYS) && <DirtyPill />}
                  </div>
                  <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">关闭内置 nginx / redis / postgres 等演示镜像</div>
                </div>
                <Switch
                  checked={form.disable_default_watch}
                  onChange={(v) => update({ disable_default_watch: v })}
                  label="切换演示监控列表"
                />
              </div>
            </BentoCard>
          </div>
        </section>

        {/* ── 注册表 ── */}
        <section className="space-y-3">
          <SectionHead title="注册表" desc="访问私有仓库或镜像加速时的连接设置。" />
          <div className="bento-grid">
            <BentoCard span="wide" className={changed(REGISTRY_INSECURE_KEYS) ? 'ring-2 ring-amber-500/30' : ''}>
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2.5">
                    <span className="font-medium text-zinc-900 dark:text-zinc-100">HTTP 注册表</span>
                    {changed(REGISTRY_INSECURE_KEYS) && <DirtyPill />}
                  </div>
                  <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">允许向 http 私有注册表发起请求</div>
                </div>
                <Switch
                  checked={form.registry_insecure}
                  onChange={(v) => update({ registry_insecure: v })}
                  label="切换 HTTP 注册表"
                />
              </div>
            </BentoCard>

            <BentoCard span="wide" className={changed(REGISTRY_MIRROR_KEYS) ? 'ring-2 ring-amber-500/30' : ''}>
              <div className="flex items-center gap-2.5">
                <span className="font-medium text-zinc-900 dark:text-zinc-100">注册表镜像主机</span>
                {changed(REGISTRY_MIRROR_KEYS) && <DirtyPill />}
              </div>
              <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
                非空时所有请求改发往该主机（用于私有仓库或加速镜像）；留空不使用镜像。
              </div>
              <input
                type="text"
                placeholder="如 mirror.example.com 或 localhost:5000"
                aria-label="注册表镜像主机"
                aria-invalid={!!mirrorError}
                value={form.registry_mirror ?? ''}
                onChange={(e) => {
                  update({ registry_mirror: e.target.value })
                  if (mirrorError) setMirrorError('')
                }}
                className={`mt-3 ${INPUT_CLS} ${mirrorError ? INPUT_ERR_CLS : ''}`}
              />
              {mirrorError && (
                <p className="mt-1.5 flex items-center gap-1.5 text-xs text-rose-500">
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true">
                    <circle cx="12" cy="12" r="9" /><path d="M12 8v4" /><path d="M12 16h.01" />
                  </svg>
                  {mirrorError}
                </p>
              )}
              <div className="mt-3 space-y-1 text-xs text-zinc-400 dark:text-zinc-500">
                <p>· Docker Hub 加速：填写镜像代理域名</p>
                <p>· 私有仓库：填写 registry 主机名（如 harbor.example.com）</p>
              </div>
            </BentoCard>
          </div>
        </section>

        {/* ── 通知渠道（独立保存，不进底部保存栏） ── */}
        <section className="space-y-3">
          <SectionHead title="通知渠道" desc="镜像更新时并行推送到所有已启用的渠道，改动随保存即时生效。" />
          <ChannelManager />
        </section>

        {/* 粘性保存栏：改动时始终可见，避免改完忘记保存 */}
        <div className="sticky bottom-0 z-10 flex flex-wrap items-center gap-3 rounded-2xl border border-zinc-100 bg-white/95 px-4 py-3 shadow-sm backdrop-blur-sm dark:border-zinc-800 dark:bg-zinc-900/95">
          <button
            type="submit"
            disabled={saving || !dirty}
            className="inline-flex h-11 items-center justify-center rounded-xl bg-zinc-900 px-5 text-sm font-medium text-white transition-colors hover:bg-zinc-700 disabled:cursor-not-allowed disabled:opacity-60 active:scale-95 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200 sm:min-w-36"
          >
            {saving ? '保存中…' : '保存设置'}
          </button>
          {dirty && (
            <button
              type="button"
              onClick={discard}
              disabled={saving}
              className="inline-flex h-11 items-center justify-center rounded-xl border border-zinc-200 px-4 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-50 disabled:opacity-60 active:scale-95 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
            >
              放弃更改
            </button>
          )}
          <span className={`text-sm ${dirty ? 'font-medium text-amber-600 dark:text-amber-400' : 'text-zinc-400 dark:text-zinc-500'} sm:ml-auto`}>
            {dirty ? '有未保存的更改' : '所有更改已保存'}
          </span>
        </div>
      </form>
    </div>
  )
}
