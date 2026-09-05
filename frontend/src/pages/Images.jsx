import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, fmtTime, fmtShort, modeLabel, shortDigest } from '../api/client'
import BentoCard from '../components/BentoCard'
import StatusBadge from '../components/StatusBadge'
import Spinner from '../components/Spinner'
import Pagination from '../components/Pagination'
import ConfirmDialog from '../components/ConfirmDialog'
import { useToast } from '../components/Toast'

// 概览卡统计项：即状态过滤入口（点击切换、再点取消），不再单设一排筛选 chips
const OVERVIEW = [
  { label: '有更新', key: 'update-available', cls: 'bg-amber-500', activeCls: 'ring-2 ring-amber-500/40 bg-amber-50/60 dark:bg-amber-500/10' },
  { label: '已是最新', key: 'up-to-date', cls: 'bg-emerald-500', activeCls: 'ring-2 ring-emerald-500/40 bg-emerald-50/60 dark:bg-emerald-500/10' },
  { label: '未知', key: 'unknown', cls: 'bg-zinc-400', activeCls: 'ring-2 ring-zinc-400/40 bg-zinc-100 dark:bg-zinc-800' },
  { label: '缺失', key: 'stale', cls: 'bg-rose-500', activeCls: 'ring-2 ring-rose-500/40 bg-rose-50/60 dark:bg-rose-500/10' },
  {
    label: '已忽略', key: 'ignored', cls: 'bg-zinc-300 dark:bg-zinc-600', activeCls: 'ring-2 ring-zinc-400/40 bg-zinc-100 dark:bg-zinc-800',
    count: (list) => list.reduce((n, i) => n + (i.ignored ? 1 : 0), 0), // 忽略是独立标记，不走 status 字段
  },
]

const PAGE_SIZE = 12

// 列表排序权重：有更新优先，便于一眼定位需要处理的镜像
const STATUS_ORDER = { 'update-available': 0, unknown: 1, 'up-to-date': 2, stale: 3 }

export default function Images() {
  const navigate = useNavigate()
  const toast = useToast()
  const [images, setImages] = useState([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState('')
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(1)
  const [newRef, setNewRef] = useState('')
  const [adding, setAdding] = useState(false)
  const [confirmId, setConfirmId] = useState(null) // 待确认移除的镜像 id

  const load = async () => {
    setLoading(true)
    try {
      // 始终拉取全集：过滤（含已忽略）在客户端完成，保证概览计数不随筛选抖动
      const r = await api.images()
      setImages(r.images || [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line
  }, [filter])

  // 筛选或搜索条件变化时回到第 1 页
  useEffect(() => {
    setPage(1)
  }, [filter, query])

  const filtered = useMemo(() => {
    let out = images
    if (filter === 'ignored') out = out.filter((i) => i.ignored)
    else if (filter) out = out.filter((i) => !i.ignored && i.status === filter)
    if (query) out = out.filter((i) => i.reference.toLowerCase().includes(query.toLowerCase()))
    return [...out].sort(
      (a, b) => (STATUS_ORDER[a.status] ?? 9) - (STATUS_ORDER[b.status] ?? 9) || a.reference.localeCompare(b.reference),
    )
  }, [images, query, filter])

  // 分页切片（客户端分页，基于过滤后的全集）
  const total = filtered.length
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const pageItems = useMemo(() => {
    const start = (Math.min(page, totalPages) - 1) * PAGE_SIZE
    return filtered.slice(start, start + PAGE_SIZE)
  }, [filtered, page, totalPages])

  const goPage = (p) => {
    setPage(p)
    // 切换分页后让列表回到可视区顶部
    requestAnimationFrame(() => {
      document.querySelector('.img-list-top')?.scrollIntoView({ block: 'start', behavior: 'smooth' })
    })
  }

  const onAdd = async (e) => {
    e.preventDefault()
    if (!newRef.trim()) return
    setAdding(true)
    try {
      await api.addImage(newRef.trim())
      setNewRef('')
      await load()
      toast('success', `已添加监控：${newRef.trim()}`)
    } catch {
      toast('error', '添加失败，请确认引用格式（如 nginx:latest）')
    } finally {
      setAdding(false)
    }
  }

  const onRemove = async (id) => {
    try {
      await api.removeImage(id)
      setConfirmId(null)
      await load()
      toast('success', '已移除该镜像')
    } catch {
      setConfirmId(null)
      toast('error', '移除失败，请重试')
    }
  }

  // 忽略 = 跳过全部检测（不校验摘要、不巡检标签），行数据冻结，也不产生通知。
  const onToggleIgnore = async (img) => {
    try {
      await api.setIgnored(img.id, !img.ignored)
      await load()
      toast('success', img.ignored ? '已恢复该镜像的更新检测' : '已忽略该镜像（跳过全部检测）')
    } catch (e) {
      toast('error', e?.message || '操作失败，请重试')
    }
  }

  // 设置检测模式覆写（auto/digest-only/pin-watch）
  const onSetMode = async (img, mode) => {
    if (mode === (img.mode || 'auto')) return
    try {
      await api.setMode(img.id, mode)
      await load()
      toast('success', '检测模式已更新，下次扫描生效')
    } catch {
      toast('error', '更新失败，请重试')
    }
  }

  // 概览块点击：切换对应状态过滤（再点一次取消）
  const pickOverview = (key) => setFilter((f) => (f === key ? '' : key))

  const confirmImg = images.find((i) => i.id === confirmId)

  return (
    <div className="img-list-top flex h-full flex-col overflow-hidden gap-4">
      <div className="shrink-0">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">镜像列表</h1>
          <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">所有被监控的镜像引用及其版本状态，有更新的排在前。</p>
        </div>
        <form onSubmit={onAdd} className="flex min-w-0 items-center gap-2.5">
          <input
            value={newRef}
            onChange={(e) => setNewRef(e.target.value)}
            placeholder="新增监控，如 redis:7"
            className="min-w-0 flex-1 rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm outline-none transition-all focus:border-bento-accent focus:ring-2 focus:ring-blue-500/20 sm:w-56 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
          />
          <button disabled={adding} className="shrink-0 rounded-xl bg-zinc-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-700 disabled:opacity-60 active:scale-95 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200">
            {adding ? '添加中…' : '添加'}
          </button>
        </form>
      </div>
      </div>

      {/* 搜索（状态筛选统一收口在下方「监控概览」统计卡） */}
      <div className="shrink-0 flex justify-end">
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="搜索引用…"
          className="w-full rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-1.5 text-sm outline-none transition-all focus:border-bento-accent focus:ring-2 focus:ring-blue-500/20 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100 sm:w-64"
        />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto pr-1">
      {loading ? (
        <Spinner label="加载镜像…" />
      ) : images.length === 0 ? (
        <BentoCard className="py-10 text-center">
          <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-zinc-100 text-zinc-400 dark:bg-zinc-800 dark:text-zinc-500">
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 8 12 3 3 8v8l9 5 9-5V8Z" /><path d="m3 8 9 5 9-5" /><path d="M12 13v8" />
            </svg>
          </div>
          <p className="mt-4 text-sm font-medium text-zinc-600 dark:text-zinc-300">尚未添加任何镜像</p>
          <p className="mx-auto mt-1 max-w-xs text-sm text-zinc-400 dark:text-zinc-500">
            使用右上角输入框添加引用（如 nginx:latest、redis:7），保存后即可开始监控版本变化。
          </p>
        </BentoCard>
      ) : filtered.length === 0 ? (
        <BentoCard className="py-10 text-center">
          <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-zinc-100 text-zinc-400 dark:bg-zinc-800 dark:text-zinc-500">
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="11" cy="11" r="7" /><path d="m21 21-4.3-4.3" />
            </svg>
          </div>
          <p className="mt-4 text-sm font-medium text-zinc-600 dark:text-zinc-300">没有匹配的镜像</p>
          <p className="mt-1 text-sm text-zinc-400 dark:text-zinc-500">调整筛选条件或搜索词后再试。</p>
        </BentoCard>
      ) : (
        <>
          {/* 监控概览：唯一的状态过滤入口（点击切换，再点取消；计数不随筛选变化） */}
          <BentoCard>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h3 className="font-semibold text-zinc-900 dark:text-zinc-100">监控概览</h3>
              <div className="flex items-center gap-3">
                <span className="text-xs text-zinc-400 dark:text-zinc-500">共 {images.length} 个 · 当前匹配 {filtered.length} 个</span>
                {filter && (
                  <button
                    onClick={() => setFilter('')}
                    className="rounded-lg bg-zinc-100 px-2 py-1 text-xs font-medium text-zinc-600 transition-colors hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-300 dark:hover:bg-zinc-700"
                  >
                    取消筛选 ✕
                  </button>
                )}
              </div>
            </div>
            <div className="mt-4 grid grid-cols-2 gap-2.5 sm:grid-cols-3 lg:grid-cols-5">
              {OVERVIEW.map((s) => {
                const v = s.count ? s.count(images) : countStatus(images, s.key)
                const active = filter === s.key
                // 计数为 0 且未激活时隐藏，避免「缺失 0」这类恒零项常驻占位
                if (v === 0 && !active) return null
                return (
                  <button
                    key={s.key}
                    onClick={() => pickOverview(s.key)}
                    aria-pressed={active}
                    className={`flex items-center gap-2.5 rounded-xl border border-transparent px-3 py-2.5 text-left transition-colors hover:bg-zinc-50 dark:hover:bg-zinc-800/60 ${active ? s.activeCls : ''}`}
                  >
                    <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${s.cls}`} />
                    <span className="text-sm text-zinc-500 dark:text-zinc-400">{s.label}</span>
                    <span className="ml-auto text-lg font-bold tabular-nums text-zinc-900 dark:text-zinc-100">{v}</span>
                  </button>
                )
              })}
            </div>
          </BentoCard>
          <div className="bento-grid">
            {pageItems.map((img) => (
            <BentoCard key={img.id} className={`group flex flex-col ${img.ignored ? 'ring-1 ring-zinc-300/70 dark:ring-zinc-600/60' : ''}`}>
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2.5">
                    <span className="truncate text-base font-semibold text-zinc-900 dark:text-zinc-100" title={img.reference}>
                      {img.reference}
                    </span>
                    {img.ignored && (
                      <span className="shrink-0 rounded-md bg-zinc-200 px-1.5 py-0.5 text-[11px] font-medium text-zinc-500 dark:bg-zinc-700 dark:text-zinc-300">
                        已忽略
                      </span>
                    )}
                  </div>
                  <div className="mt-0.5 flex items-center gap-1.5 whitespace-nowrap text-xs text-zinc-400">
                    <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${img.source === 'docker' ? 'bg-blue-400' : 'bg-zinc-300 dark:bg-zinc-600'}`} />
                    <span className="shrink-0">{img.source === 'docker' ? 'Docker' : '手动'}</span>
                    <span className="shrink-0 text-zinc-300 dark:text-zinc-600">·</span>
                    {/* 相对/短时间防换行，完整时间见悬浮提示 */}
                    <span title={img.last_check ? fmtTime(img.last_check) : undefined}>
                      {img.last_check ? fmtShort(img.last_check) : '未扫描'}
                    </span>
                  </div>
                </div>
                <StatusBadge status={img.status} />
              </div>

              {img.ignored && (
                <div className="mt-2 rounded-lg bg-zinc-50 px-2.5 py-1.5 text-[11px] leading-relaxed text-zinc-500 dark:bg-zinc-800/60 dark:text-zinc-400">
                  已忽略：跳过全部检测（不校验摘要、不巡检标签），也不产生任何通知。
                </div>
              )}

              {/* 检测模式覆写 */}
              <div className="mt-3 flex items-center gap-2">
                <span className="shrink-0 text-[11px] text-zinc-400">检测模式</span>
                <select
                  value={img.mode || 'auto'}
                  disabled={img.ignored}
                  onChange={(e) => onSetMode(img, e.target.value)}
                  title={img.ignored ? '已忽略的镜像不执行检测' : `选择检测模式（当前生效：${modeLabel(img.effective_mode)}）`}
                  className="min-w-0 flex-1 truncate rounded-lg border border-zinc-200 bg-zinc-50 px-2 py-1 text-[11px] font-medium text-zinc-600 outline-none transition-colors hover:border-zinc-300 focus:border-bento-accent focus:ring-2 focus:ring-blue-500/20 disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-300"
                >
                  {/* 选项用短文案，避免四列卡片内文字被截断；生效模式见悬浮提示 */}
                  <option value="auto">自动</option>
                  <option value="digest-only">仅摘要检测</option>
                  <option value="pin-watch">锁定+新标签</option>
                </select>
              </div>

              <div className="mt-4 space-y-1.5 text-xs">
                <DigestRow label="本地" value={shortDigest(img.local_digest)} />
                <DigestRow label="远端" value={shortDigest(img.remote_digest)} />
              </div>

              {/* 操作区：一行三钮，紧凑不堆叠 */}
              <div className="mt-auto pt-4">
                <div className="flex gap-2">
                  <button
                    onClick={() => navigate('/compare?id=' + img.id)}
                    className="flex-1 rounded-xl border border-zinc-200 py-1.5 text-sm font-medium text-zinc-700 transition-colors hover:border-zinc-300 hover:bg-zinc-50 active:scale-95 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800 dark:hover:border-zinc-600"
                  >
                    版本对比
                  </button>
                  <button
                    onClick={() => onToggleIgnore(img)}
                    title={img.ignored ? '恢复该镜像的更新提醒' : '忽略该镜像的更新提醒（仍扫描不提醒）'}
                    className={`shrink-0 rounded-xl border px-3 py-1.5 text-sm font-medium transition-colors active:scale-95 ${
                      img.ignored
                        ? 'border-emerald-300 text-emerald-600 hover:bg-emerald-50 dark:border-emerald-700 dark:text-emerald-400 dark:hover:bg-emerald-500/10'
                        : 'border-zinc-200 text-zinc-500 hover:bg-zinc-50 hover:text-zinc-700 dark:border-zinc-700 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-200'
                    }`}
                  >
                    {img.ignored ? '恢复' : '忽略'}
                  </button>
                  <button
                    onClick={() => setConfirmId(img.id)}
                    title="从监控中移除该镜像"
                    className="shrink-0 rounded-xl border border-zinc-200 px-3 py-1.5 text-sm text-zinc-400 transition-colors hover:border-rose-200 hover:bg-rose-50 hover:text-rose-500 active:scale-95 dark:border-zinc-700 dark:hover:border-rose-800 dark:hover:bg-rose-500/10 dark:hover:text-rose-400"
                  >
                    移除
                  </button>
                </div>
              </div>
            </BentoCard>
          ))}
          </div>
          <Pagination page={page} total={total} pageSize={PAGE_SIZE} onChange={goPage} />
        </>
      )}
      </div>

      <ConfirmDialog
        open={confirmId !== null}
        title="移除镜像"
        description={confirmImg ? `确定要将「${confirmImg.reference}」从监控中移除吗？此操作不可撤销，本地与远端摘要记录将被删除。` : '确定要移除该镜像吗？'}
        confirmText="确认移除"
        danger
        onConfirm={() => confirmId !== null && onRemove(confirmId)}
        onCancel={() => setConfirmId(null)}
      />
    </div>
  )
}

function countStatus(list, status) {
  return list.reduce((n, i) => n + (i.status === status ? 1 : 0), 0)
}

function DigestRow({ label, value }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="shrink-0 text-zinc-400">{label}</span>
      <span className="truncate font-mono text-zinc-600 dark:text-zinc-300" title={value}>{value}</span>
    </div>
  )
}
