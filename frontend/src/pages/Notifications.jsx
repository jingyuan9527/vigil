import { useEffect, useRef, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { api, fmtShort, shortDigest, SCAN_DONE_EVENT } from '../api/client'
import BentoCard from '../components/BentoCard'
import Spinner from '../components/Spinner'
import Pagination from '../components/Pagination'
import ConfirmDialog from '../components/ConfirmDialog'
import ErrorState from '../components/ErrorState'
import { useToast } from '../components/Toast'

const PAGE_SIZE = 6 // 每组单页条数：调小让两栏更容易都走分页，避免等高布局下大片留白

// 状态筛选：全部展示已读/未读/已忽略，未读与已忽略可单独查看。
const FILTERS = [
  { key: 'all', label: '全部' },
  { key: 'unread', label: '未读' },
  { key: 'ignored', label: '已忽略' },
]

// 两栏分组：同一镜像只进一栏。最近一次提醒是「新 tag」→ 可选更新；
// 摘要变化 / 仅有可升级版本 → 有新版本。
const GROUPS = [
  {
    key: 'update',
    title: '有新版本',
    desc: '同一标签的摘要已变化，或存在更高版本可升级',
    icon: (
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M12 2v8" /><path d="m8 6 4-4 4 4" /><circle cx="12" cy="15" r="6" /><path d="M12 12v6" />
      </svg>
    ),
    iconCls: 'bg-amber-100 text-amber-600 dark:bg-amber-500/15 dark:text-amber-300',
    countCls: 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
  },
  {
    key: 'new-tag',
    title: '可选更新',
    desc: '仓库最近提醒了新的版本标签（可能来自其它分支）',
    icon: (
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <circle cx="12" cy="12" r="9" /><path d="M12 8v4" /><path d="M12 16h.01" />
      </svg>
    ),
    iconCls: 'bg-blue-100 text-blue-600 dark:bg-blue-500/15 dark:text-blue-300',
    countCls: 'bg-blue-100 text-blue-700 dark:bg-blue-500/15 dark:text-blue-300',
  },
]

const groupKeyOf = (n) => (n.new_tags && n.new_tags.length > 0 ? 'new-tag' : 'update')

const REASON_LABEL = {
  digest: '摘要变更',
  version: '版本更新',
  both: '摘要 + 版本',
}

export default function Notifications() {
  const toast = useToast()
  const { refreshNotifs, dashboardRefresh } = useOutletContext() || {}
  const [items, setItems] = useState([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [filter, setFilter] = useState('all')
  const [pages, setPages] = useState({ update: 1, 'new-tag': 1 }) // 两栏独立分页
  const [collapsed, setCollapsed] = useState({ update: false, 'new-tag': false }) // 两栏独立折叠
  const [scanning, setScanning] = useState(false)
  const [confirmRescan, setConfirmRescan] = useState(false)
  const pollRef = useRef(null) // 强制扫描轮询器：登记到 ref 供卸载清理

  // 卸载时清掉仍在跑的扫描轮询，防止悬空定时器继续 setState
  useEffect(() => () => clearInterval(pollRef.current), [])

  const load = async (silent) => {
    if (!silent) setLoading(true)
    try {
      const r = await api.updates()
      setItems(r.updates || [])
      setLoadError(false)
    } catch {
      setLoadError(true)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 顶栏「立即扫描」结束时广播事件：通知页跟随刷新，避免展示过期状态
  useEffect(() => {
    const onDone = () => load(true)
    window.addEventListener(SCAN_DONE_EVENT, onDone)
    return () => window.removeEventListener(SCAN_DONE_EVENT, onDone)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 切换筛选后两栏都回到第 1 页
  useEffect(() => {
    setPages({ update: 1, 'new-tag': 1 })
  }, [filter])

  // 状态变更先落库再改本地：失败时提示并保留原状态（下轮扫描会纠正）。
  const patchStatus = (imageID, status) =>
    setItems((prev) => prev.map((n) => (n.image_id === imageID ? { ...n, status } : n)))

  const markRead = async (imageID) => {
    try {
      await api.markUpdateRead(imageID)
      patchStatus(imageID, 'read')
      refreshNotifs?.()
      dashboardRefresh?.()
    } catch {
      toast('error', '标记失败，请重试')
    }
  }

  const ignore = async (imageID) => {
    try {
      await api.ignoreUpdate(imageID)
      patchStatus(imageID, 'ignored')
      refreshNotifs?.()
      dashboardRefresh?.()
      toast('success', '已忽略该版本，出现更高版本时会重新提醒')
    } catch {
      toast('error', '忽略失败，请重试')
    }
  }

  const unignore = async (imageID) => {
    try {
      await api.unignoreUpdate(imageID)
      patchStatus(imageID, 'read')
      refreshNotifs?.()
      dashboardRefresh?.()
      toast('success', '已取消忽略')
    } catch {
      toast('error', '操作失败，请重试')
    }
  }

  const markAll = async () => {
    try {
      await api.markAllUpdatesRead()
      setItems((prev) => prev.map((n) => (n.status === 'unread' ? { ...n, status: 'read' } : n)))
      refreshNotifs?.()
      dashboardRefresh?.()
      toast('success', '已将全部更新标记为已读')
    } catch {
      toast('error', '操作失败，请重试')
    }
  }

  // 全部重新扫描：只重新检测并刷新聚合记录，不重复推送相同版本
  const rescanAll = async () => {
    if (scanning) return
    setScanning(true)
    try {
      await api.scanNow(true)
      toast('success', '已触发重新扫描，仅新版本会再次提醒')
      refreshNotifs?.() // 立刻让顶栏进入 running 态
    } catch {
      toast('error', '触发扫描失败，请重试')
      setScanning(false)
      return
    }
    // 轮询扫描状态直到结束：固定延时刷新会停在过期的 running 状态（扫描可能远超 6s）
    const deadline = Date.now() + 180_000
    clearInterval(pollRef.current) // 组件生命周期内重复触发时先清旧轮询
    pollRef.current = setInterval(async () => {
      if (Date.now() > deadline) {
        clearInterval(pollRef.current)
        setScanning(false)
        return
      }
      try {
        const s = await api.scans()
        const cur = s.scans && s.scans[0]
        if (!cur || cur.status === 'running') return
        clearInterval(pollRef.current)
        setScanning(false)
        await load()
        refreshNotifs?.()
        window.dispatchEvent(new Event(SCAN_DONE_EVENT))
      } catch {
        /* 单次轮询失败忽略，下一轮重试 */
      }
    }, 3000)
  }

  const unread = items.filter((i) => i.status === 'unread').length
  const ignored = items.filter((i) => i.status === 'ignored').length
  const filtered = items.filter((i) => filter === 'all' || i.status === filter)
  // 同一镜像只进一栏：最近一次提醒带 new_tags → 可选更新，否则归入有新版本
  const groups = GROUPS.map((g) => ({ ...g, list: filtered.filter((n) => groupKeyOf(n) === g.key) }))

  const goPage = (key) => (p) => {
    setPages((prev) => ({ ...prev, [key]: p }))
    requestAnimationFrame(() => {
      document.querySelector('.notif-list-top')?.scrollIntoView({ block: 'start', behavior: 'smooth' })
    })
  }
  const toggleGroup = (key) => setCollapsed((prev) => ({ ...prev, [key]: !prev[key] }))

  return (
    <div className="notif-list-top flex h-full flex-col overflow-hidden gap-4">
      <div className="shrink-0 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">更新通知</h1>
          <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
            按优先级分两栏：有新版本（摘要变化/可升级）与可选更新（新 tag），同一镜像只出现一次。
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2.5">
          <div className="flex rounded-xl bg-zinc-100 p-1 dark:bg-zinc-800">
            {FILTERS.map((f) => (
              <button
                key={f.key}
                onClick={() => setFilter(f.key)}
                className={`rounded-lg px-3 py-1.5 text-sm font-medium transition-colors ${
                  filter === f.key
                    ? 'bg-white text-zinc-900 shadow-sm dark:bg-zinc-900 dark:text-zinc-100'
                    : 'text-zinc-500 hover:text-zinc-700 dark:text-zinc-400 dark:hover:text-zinc-200'
                }`}
              >
                {f.label}
              </button>
            ))}
          </div>
          <button
            onClick={markAll}
            disabled={unread === 0}
            className="rounded-xl border border-zinc-200 px-3 py-2 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-50 disabled:opacity-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
          >
            全部已读
          </button>
          <button
            onClick={() => setConfirmRescan(true)}
            disabled={scanning}
            className="inline-flex items-center gap-2 rounded-xl bg-zinc-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-700 disabled:opacity-60 active:scale-95 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200"
          >
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={scanning ? 'animate-spin' : ''}>
              <path d="M21 12a9 9 0 1 1-2.6-6.4" /><path d="M21 3v6h-6" />
            </svg>
            {scanning ? '扫描中…' : '全部重新扫描'}
          </button>
        </div>
      </div>

      {/* 概览：聚合记录的待处理/未读/已忽略计数（每镜像只计一次） */}
      <BentoCard className="shrink-0 flex flex-wrap items-center gap-x-8 gap-y-3">
        {[
          { label: '存在更新', v: items.length, dot: 'bg-zinc-400 dark:bg-zinc-500', num: 'text-zinc-900 dark:text-zinc-100' },
          { label: '未读', v: unread, dot: 'bg-amber-500', num: 'text-amber-600 dark:text-amber-400' },
          { label: '已忽略', v: ignored, dot: 'bg-rose-400', num: 'text-rose-600 dark:text-rose-400' },
        ].map((s) => (
          <div key={s.label} className="flex items-center gap-2.5">
            <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${s.dot}`} />
            <span className="text-sm text-zinc-500 dark:text-zinc-400">{s.label}</span>
            <span className={`text-xl font-bold tabular-nums ${s.num}`}>{s.v}</span>
          </div>
        ))}
        <span className="ml-auto text-xs text-zinc-400 dark:text-zinc-500">同一镜像同一版本不会重复提醒</span>
      </BentoCard>

      <div className="min-h-0 flex-1 overflow-y-auto pr-1">
        {loading ? (
          <Spinner label="加载更新记录…" />
        ) : loadError ? (
          <ErrorState message="更新通知加载失败" onRetry={() => load()} />
        ) : filtered.length === 0 ? (
          <BentoCard className="py-10 text-center">
            <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-zinc-100 text-zinc-400 dark:bg-zinc-800 dark:text-zinc-500">
              <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
              </svg>
            </div>
            <p className="mt-4 text-sm font-medium text-zinc-600 dark:text-zinc-300">
              {filter === 'unread' ? '没有未读更新' : filter === 'ignored' ? '没有已忽略的版本' : '暂无更新记录'}
            </p>
            <p className="mt-1 text-sm text-zinc-400 dark:text-zinc-500">
              {filter === 'unread'
                ? '版本变化时会产生提醒，可在镜像列表查看当前状态。'
                : filter === 'ignored'
                  ? '在更新条目上点击「忽略此版本」可减少重复提醒。'
                  : '镜像版本或摘要变化时会在这里聚合展示，一切正常。'}
            </p>
          </BentoCard>
        ) : (
          /* 两栏等高：同一镜像只进一栏，各组独立折叠 + 分页 */
          <div className="grid gap-6 lg:grid-cols-2">
            {groups.map((g) => (
              <NotifGroup
                key={g.key}
                group={g}
                page={pages[g.key]}
                collapsed={collapsed[g.key]}
                onToggle={() => toggleGroup(g.key)}
                onPage={goPage(g.key)}
                onRead={markRead}
                onIgnore={ignore}
                onUnignore={unignore}
              />
            ))}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={confirmRescan}
        title="全部重新扫描"
        description="重新检测所有镜像并刷新聚合记录，用于补齐失败轮次或同步版本信息。相同版本不会重复推送，只有出现新版本或摘要变化才会再次提醒。"
        confirmText="开始重新扫描"
        onConfirm={() => {
          setConfirmRescan(false)
          rescanAll()
        }}
        onCancel={() => setConfirmRescan(false)}
      />
    </div>
  )
}

/* 单个分栏：组头（图标/标题/计数/折叠）+ 行式列表 + 独立分页。
   两栏在宽屏等高，折叠后缩回自身内容高度，避免拉伸成空白大卡。 */
function NotifGroup({ group, page, collapsed, onToggle, onPage, onRead, onIgnore, onUnignore }) {
  const total = group.list.length
  const unread = group.list.filter((n) => n.status === 'unread').length
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const cur = Math.min(page, totalPages)
  const pageItems = group.list.slice((cur - 1) * PAGE_SIZE, cur * PAGE_SIZE)

  return (
    <section
      className={`flex flex-col overflow-hidden rounded-2xl border border-zinc-100 bg-white dark:border-zinc-800 dark:bg-zinc-900 ${
        collapsed ? 'self-start' : ''
      }`}
    >
      <button
        onClick={onToggle}
        aria-expanded={!collapsed}
        className="flex w-full shrink-0 items-center gap-3 px-4 py-3.5 text-left transition-colors hover:bg-zinc-50 md:px-5 dark:hover:bg-zinc-800/50"
      >
        <span className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-xl ${group.iconCls}`}>{group.icon}</span>
        <span className="min-w-0 flex-1">
          <span className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">{group.title}</span>
            <span className={`rounded-md px-1.5 py-0.5 text-[10px] font-semibold ${group.countCls}`}>{total}</span>
            {unread > 0 && (
              <span className="rounded-md bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-semibold text-amber-600 dark:text-amber-400">
                未读 {unread}
              </span>
            )}
          </span>
          <span className="mt-0.5 block truncate text-[11px] text-zinc-400 dark:text-zinc-500">{group.desc}</span>
        </span>
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className={`shrink-0 text-zinc-400 transition-transform ${collapsed ? '-rotate-90' : ''}`}
        >
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>

      {!collapsed &&
        (total === 0 ? (
          <div className="flex flex-1 items-center justify-center border-t border-zinc-100 px-4 py-8 text-sm text-zinc-400 dark:border-zinc-800 dark:text-zinc-500">
            本栏暂无更新
          </div>
        ) : (
          <>
            <ul className="flex-1 divide-y divide-zinc-100 dark:divide-zinc-800">
              {pageItems.map((n) => (
                <UpdateRow key={n.image_id} item={n} onRead={onRead} onIgnore={onIgnore} onUnignore={onUnignore} />
              ))}
            </ul>
            <div className="border-t border-zinc-100 px-4 py-2.5 dark:border-zinc-800">
              {total > PAGE_SIZE ? (
                <Pagination page={cur} total={total} pageSize={PAGE_SIZE} onChange={onPage} />
              ) : (
                <p className="flex h-10 items-center justify-center text-xs text-zinc-400 dark:text-zinc-500">
                  共 {total} 条 · 单页展示
                </p>
              )}
            </div>
          </>
        ))}
    </section>
  )
}

/* 单条聚合更新：一行展示版本对照、摘要变化与候选版本，右侧为状态操作。 */
function UpdateRow({ item: n, onRead, onIgnore, onUnignore }) {
  const digestChanged = n.base_digest && n.current_digest && n.base_digest !== n.current_digest
  const latestKnown = !!n.latest_version
  const hasNewerVersion = latestKnown && n.latest_version !== n.current_version
  const candidates = n.candidates || []
  // new_tags 是最近一次「可选新版本」提醒中的新 tag（可能低于当前版本/来自其它分支），
  // 已出现在候选版本里的不重复展示。
  const newTags = (n.new_tags || []).filter((t) => !candidates.includes(t))

  return (
    <li className={`flex items-start gap-3 px-4 py-3.5 transition-colors hover:bg-zinc-50 md:px-5 dark:hover:bg-zinc-800/50 ${n.status === 'read' ? 'opacity-70' : ''}`}>
      <span
        className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${
          n.status === 'unread' ? 'bg-amber-500' : n.status === 'ignored' ? 'bg-rose-400' : 'bg-zinc-300 dark:bg-zinc-600'
        }`}
      />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span className="truncate text-sm font-semibold text-zinc-900 dark:text-zinc-100" title={n.reference}>{n.reference}</span>
          {n.status === 'unread' && (
            <span className="rounded-md bg-amber-100 px-1.5 py-0.5 text-[10px] font-semibold text-amber-700 dark:bg-amber-500/15 dark:text-amber-300">未读</span>
          )}
          {n.status === 'ignored' && (
            <span className="rounded-md bg-rose-50 px-1.5 py-0.5 text-[10px] font-semibold text-rose-600 dark:bg-rose-500/10 dark:text-rose-400">已忽略</span>
          )}
          {n.status === 'read' && (
            <span className="rounded-md bg-zinc-100 px-1.5 py-0.5 text-[10px] font-medium text-zinc-500 dark:bg-zinc-800 dark:text-zinc-400">已读</span>
          )}
          {REASON_LABEL[n.reason] && (
            <span className="rounded-md bg-zinc-100 px-1.5 py-0.5 text-[10px] text-zinc-500 dark:bg-zinc-800 dark:text-zinc-400">{REASON_LABEL[n.reason]}</span>
          )}
          <span className="ml-auto shrink-0 text-[11px] tabular-nums text-zinc-400">{fmtShort(n.last_seen_at)}</span>
        </div>

        <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
          <span className="text-zinc-500 dark:text-zinc-400">当前</span>
          <span className="font-mono font-medium text-zinc-800 dark:text-zinc-200">{n.current_version || '—'}</span>
          {hasNewerVersion ? (
            <>
              <span className="text-zinc-300 dark:text-zinc-600">→</span>
              <span className="text-zinc-500 dark:text-zinc-400">最新</span>
              <span className="font-mono font-medium text-blue-600 dark:text-blue-300">{n.latest_version}</span>
            </>
          ) : latestKnown ? (
            <span className="text-zinc-400 dark:text-zinc-500">已是仓库最高版本</span>
          ) : (
            <span className="text-zinc-400 dark:text-zinc-500">仓库未提供版本号</span>
          )}
        </div>

        {digestChanged && (
          <div className="mt-1 flex flex-wrap items-center gap-x-2 font-mono text-[11px] text-zinc-400 dark:text-zinc-500">
            <span title={n.base_digest}>{shortDigest(n.base_digest)}</span>
            <span className="text-zinc-300 dark:text-zinc-600">→</span>
            <span title={n.current_digest}>{shortDigest(n.current_digest)}</span>
          </div>
        )}

        {candidates.length > 0 && (
          <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
            <span className="text-[11px] text-zinc-400 dark:text-zinc-500">候选版本</span>
            {candidates.slice(0, 8).map((t) => (
              <span key={t} className="rounded-md bg-blue-50 px-1.5 py-0.5 font-mono text-[11px] text-blue-700 dark:bg-blue-500/10 dark:text-blue-300">{t}</span>
            ))}
            {candidates.length > 8 && (
              <span className="text-[11px] text-zinc-400 dark:text-zinc-500">…等 {candidates.length} 个</span>
            )}
          </div>
        )}

        {newTags.length > 0 && (
          <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
            <span className="text-[11px] text-zinc-400 dark:text-zinc-500">可选新版本</span>
            {newTags.slice(0, 8).map((t) => (
              <span key={t} className="rounded-md bg-amber-50 px-1.5 py-0.5 font-mono text-[11px] text-amber-700 dark:bg-amber-500/10 dark:text-amber-300">{t}</span>
            ))}
            {newTags.length > 8 && (
              <span className="text-[11px] text-zinc-400 dark:text-zinc-500">…等 {newTags.length} 个</span>
            )}
          </div>
        )}
      </div>

      <div className="flex shrink-0 flex-col items-end gap-1.5">
        {n.status === 'unread' && (
          <button
            onClick={() => onRead(n.image_id)}
            title="标记为已读"
            className="rounded-lg border border-zinc-200 px-2 py-0.5 text-[11px] text-zinc-600 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800"
          >
            标为已读
          </button>
        )}
        {n.status === 'ignored' ? (
          <button
            onClick={() => onUnignore(n.image_id)}
            title="取消忽略"
            className="rounded-lg border border-zinc-200 px-2 py-0.5 text-[11px] text-zinc-600 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800"
          >
            取消忽略
          </button>
        ) : (
          <button
            onClick={() => onIgnore(n.image_id)}
            title="忽略当前版本，出现更高版本时重新提醒"
            className="rounded-lg border border-zinc-200 px-2 py-0.5 text-[11px] text-zinc-500 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-400 dark:hover:bg-zinc-800"
          >
            忽略此版本
          </button>
        )}
      </div>
    </li>
  )
}
