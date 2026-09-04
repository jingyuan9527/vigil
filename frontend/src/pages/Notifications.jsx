import { useEffect, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { api, fmtShort, shortDigest } from '../api/client'
import BentoCard from '../components/BentoCard'
import Spinner from '../components/Spinner'
import Pagination from '../components/Pagination'
import ConfirmDialog from '../components/ConfirmDialog'
import { useToast } from '../components/Toast'

const PAGE_SIZE = 8

// 分组定义：高优先级在前，互不混排（需求：按优先级分级呈现）。
const GROUPS = [
  {
    key: 'update',
    title: '有新版本',
    desc: '同一标签的远端摘要已变化，建议尽快拉取更新',
    match: (n) => n.type !== 'new-tag',
    icon: (
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M12 2v8" /><path d="m8 6 4-4 4 4" /><circle cx="12" cy="15" r="6" /><path d="M12 12v6" />
      </svg>
    ),
    iconCls: 'bg-amber-100 text-amber-600 dark:bg-amber-500/15 dark:text-amber-300',
    countCls: 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
    rowAccent: 'bg-amber-500',
  },
  {
    key: 'new-tag',
    title: '可选更新',
    desc: '仓库中出现更高的独立版本标签，可按需升级',
    match: (n) => n.type === 'new-tag',
    icon: (
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <circle cx="12" cy="12" r="9" /><path d="M12 8v4" /><path d="M12 16h.01" />
      </svg>
    ),
    iconCls: 'bg-blue-100 text-blue-600 dark:bg-blue-500/15 dark:text-blue-300',
    countCls: 'bg-blue-100 text-blue-700 dark:bg-blue-500/15 dark:text-blue-300',
    rowAccent: 'bg-blue-500',
  },
]

export default function Notifications() {
  const toast = useToast()
  const { refreshNotifs, dashboardRefresh } = useOutletContext() || {}
  const [items, setItems] = useState([])
  const [loading, setLoading] = useState(true)
  const [unreadOnly, setUnreadOnly] = useState(false)
  const [scanning, setScanning] = useState(false)
  const [pages, setPages] = useState({ update: 1, 'new-tag': 1 }) // 各组独立分页
  const [collapsed, setCollapsed] = useState({ update: false, 'new-tag': false }) // 各组独立折叠
  const [cursor, setCursor] = useState(0) // 当前已加载的最小 ID，用于加载更多
  const [loadingMore, setLoadingMore] = useState(false)
  const [hasMore, setHasMore] = useState(true) // 后端是否还有更早的通知（每页满 100 条才可能有）
  const [confirmClear, setConfirmClear] = useState(false) // 清空已读确认弹窗

  const load = async (nextCursor, silent) => {
    if (!silent) setLoading(true)
    try {
      const r = await api.notifications(unreadOnly, nextCursor || 0)
      const notified = r.notifications || []
      // 后端分页上限 100：返回不足 100 条说明已到底，隐藏「加载更多」
      setHasMore(notified.length >= 100)
      if (nextCursor) {
        setItems((prev) => [...prev, ...notified])
        setCursor(notified.length > 0 ? notified[notified.length - 1].id : nextCursor)
      } else {
        setItems(notified)
        setCursor(notified.length > 0 ? notified[notified.length - 1].id : 0)
      }
    } finally {
      setLoading(false)
    }
  }

  // 加载更多更早的通知（按最小 ID 继续翻页；silent 避免整屏闪 Spinner）
  const loadMore = async () => {
    if (loadingMore || !hasMore || cursor === 0) return
    setLoadingMore(true)
    try {
      await load(cursor, true)
    } finally {
      setLoadingMore(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line
  }, [unreadOnly])

  // 切换筛选后两组都回到第 1 页并重置 cursor
  useEffect(() => {
    setPages({ update: 1, 'new-tag': 1 })
    setCursor(0)
  }, [unreadOnly])

  const markRead = async (id) => {
    try {
      await api.markRead(id)
      // 本地更新已读状态，不重新拉取：保留已加载的更早分页，也避免整屏闪 Spinner。
      // unreadOnly 筛选下直接移除该条（筛选条件即未读）。
      setItems((prev) =>
        prev
          .filter((n) => !(unreadOnly && n.id === id))
          .map((n) => (n.id === id ? { ...n, read: true } : n)),
      )
      refreshNotifs?.()
      dashboardRefresh?.()
    } catch {
      toast('error', '标记失败，请重试')
    }
  }

  const markAll = async () => {
    try {
      await api.markAllRead()
      if (unreadOnly) {
        setItems([])
      } else {
        setItems((prev) => prev.map((n) => ({ ...n, read: true })))
      }
      refreshNotifs?.()
      dashboardRefresh?.()
      toast('success', '已将全部通知标记为已读')
    } catch {
      toast('error', '操作失败，请重试')
    }
  }

  // 清空已读：手动归档历史（未读不受影响；去重基线独立，不影响防刷屏）
  const onClearRead = async () => {
    try {
      await api.clearReadNotifs()
      setItems((prev) => prev.filter((n) => !n.read))
      setConfirmClear(false)
      toast('success', '已清空已读通知')
    } catch {
      setConfirmClear(false)
      toast('error', '清空失败，请重试')
    }
  }

  // 一键对所有镜像重新检测更新（强制扫描：对所有版本差异补发通知）
  const rescanAll = async () => {
    if (scanning) return
    setScanning(true)
    try {
      await api.scanNow(true)
      toast('success', '已触发强制重新扫描，版本差异将逐一补报，结果稍后自动刷新')
      setTimeout(load, 1500)
      setTimeout(() => {
        load()
        refreshNotifs?.()
      }, 6000)
    } catch {
      toast('error', '触发扫描失败，请重试')
      setScanning(false)
      return
    }
    setTimeout(() => setScanning(false), 8000)
  }

  const unread = items.filter((i) => !i.read).length
  const readCount = items.filter((i) => i.read).length // 当前已加载窗口内的已读数（用于禁用清空按钮）
  const grouped = GROUPS.map((g) => ({ ...g, list: items.filter(g.match) }))
  const hasAny = items.length > 0

  const goPage = (key) => (p) => {
    setPages((prev) => ({ ...prev, [key]: p }))
    requestAnimationFrame(() => {
      document.querySelector('.notif-list-top')?.scrollIntoView({ block: 'start', behavior: 'smooth' })
    })
  }

  return (
    <div className="notif-list-top flex h-full flex-col overflow-hidden gap-4">
      <div className="shrink-0 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">更新通知</h1>
          <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">按优先级分组的镜像更新提醒，重要更新置顶展示。</p>
        </div>
        <div className="flex flex-wrap items-center gap-2.5">
          <button
            onClick={() => setUnreadOnly((v) => !v)}
            className={`rounded-xl px-3 py-2 text-sm font-medium transition-colors ${
              unreadOnly ? 'bg-zinc-900 text-white dark:bg-white dark:text-zinc-900' : 'bg-zinc-100 text-zinc-600 hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-300 dark:hover:bg-zinc-700'
            }`}
          >
            {unreadOnly ? '仅看未读' : '显示全部'}
          </button>
          <button
            onClick={markAll}
            disabled={unread === 0}
            className="rounded-xl border border-zinc-200 px-3 py-2 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-50 disabled:opacity-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
          >
            全部已读
          </button>
          <button
            onClick={() => setConfirmClear(true)}
            disabled={readCount === 0}
            className="rounded-xl border border-rose-200 px-3 py-2 text-sm font-medium text-rose-600 transition-colors hover:bg-rose-50 disabled:opacity-50 dark:border-rose-800 dark:text-rose-400 dark:hover:bg-rose-500/10"
          >
            清空已读
          </button>
          <button
            onClick={rescanAll}
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

      {/* 概览：单行紧凑统计条（密度优先） */}
      <BentoCard className="shrink-0 flex flex-wrap items-center gap-x-8 gap-y-3">
        {[
          { label: '有新版本', v: grouped[0].list.length, dot: 'bg-amber-500', num: 'text-amber-600 dark:text-amber-400' },
          { label: '可选更新', v: grouped[1].list.length, dot: 'bg-blue-500', num: 'text-blue-600 dark:text-blue-400' },
          { label: '未读', v: unread, dot: 'bg-zinc-400 dark:bg-zinc-500', num: 'text-zinc-900 dark:text-zinc-100' },
        ].map((s) => (
          <div key={s.label} className="flex items-center gap-2.5">
            <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${s.dot}`} />
            <span className="text-sm text-zinc-500 dark:text-zinc-400">{s.label}</span>
            <span className={`text-xl font-bold tabular-nums ${s.num}`}>{s.v}</span>
          </div>
        ))}
        <span className="ml-auto text-xs text-zinc-400 dark:text-zinc-500">共 {items.length} 条 · 点击组头可折叠</span>
      </BentoCard>

      {/* 分组列表：宽屏左右两列（高优先级在左），各组独立折叠 + 分页 */}
      <div className="min-h-0 flex-1 overflow-y-auto pr-1">
      {loading ? (
        <Spinner label="加载通知…" />
      ) : !hasAny ? (
        <BentoCard className="py-10 text-center">
          <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-zinc-100 text-zinc-400 dark:bg-zinc-800 dark:text-zinc-500">
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
            </svg>
          </div>
          <p className="mt-4 text-sm font-medium text-zinc-600 dark:text-zinc-300">
            {unreadOnly ? '没有未读通知' : '暂无通知'}
          </p>
          <p className="mt-1 text-sm text-zinc-400 dark:text-zinc-500">
            {unreadOnly ? '镜像更新时会产生提醒，可在镜像列表查看状态。' : '镜像远端摘要变化时会在这里生成提醒，一切正常。'}
          </p>
        </BentoCard>
      ) : (
        /* 两栏等高对齐：去掉 items-start 让卡片等高，底部页脚区保持在同一水平线 */
        <div className="grid gap-6 lg:grid-cols-2">
          {grouped.map((g) => (
            <NotifGroup
              key={g.key}
              group={g}
              page={pages[g.key]}
              collapsed={collapsed[g.key]}
              onToggle={() => setCollapsed((prev) => ({ ...prev, [g.key]: !prev[g.key] }))}
              onPage={goPage(g.key)}
              onMarkRead={markRead}
            />
          ))}
        </div>
      )}

      {/* 加载更多：当还有更早通知且加载状态允许时显示 */}
      {!loading && !loadingMore && hasMore && cursor > 0 && items.length > 0 && (
        <div className="flex justify-center shrink-0">
          <button
            onClick={loadMore}
            className="rounded-xl border border-zinc-200 px-5 py-2 text-sm font-medium text-zinc-600 transition-colors hover:bg-zinc-50 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800"
          >
            加载更多通知
          </button>
        </div>
      )}
      {loadingMore && (
        <div className="flex justify-center shrink-0">
          <span className="text-xs text-zinc-400 animate-pulse">加载中…</span>
        </div>
      )}
      </div>

      <ConfirmDialog
        open={confirmClear}
        title="清空已读通知"
        description="将删除所有已读通知，未读通知不受影响；去重不受影响，常规扫描不会因此重复提醒。此操作不可撤销。"
        confirmText="确认清空"
        danger
        onConfirm={onClearRead}
        onCancel={() => setConfirmClear(false)}
      />
    </div>
  )
}

/* 单个分组：容器 + divide-y 行式列表（不嵌套卡片，规范规则 G） */
function NotifGroup({ group, page, collapsed, onToggle, onPage, onMarkRead }) {
  const total = group.list.length
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const cur = Math.min(page, totalPages)
  const pageItems = group.list.slice((cur - 1) * PAGE_SIZE, cur * PAGE_SIZE)

  return (
    /* 展开时随行等高（两栏底部对齐）；折叠后 self-start 缩回自身内容高度，不拉伸成空白大卡 */
    <section className={`flex flex-col overflow-hidden rounded-2xl border border-zinc-100 bg-white dark:border-zinc-800 dark:bg-zinc-900 ${collapsed ? 'self-start' : ''}`}>
      {/* 组头：图标 + 标题 + 计数 + 折叠 */}
      <button
        onClick={onToggle}
        aria-expanded={!collapsed}
        className="flex w-full shrink-0 items-center gap-3 px-4 py-3.5 text-left transition-colors hover:bg-zinc-50 md:px-6 dark:hover:bg-zinc-800/50"
      >
        <span className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-xl ${group.iconCls}`}>{group.icon}</span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2.5">
            <span className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">{group.title}</span>
            <span className={`rounded-md px-1.5 py-0.5 text-[11px] font-semibold tabular-nums ${group.countCls}`}>{total}</span>
          </div>
          <p className="mt-0.5 truncate text-xs text-zinc-400 dark:text-zinc-500">{group.desc}</p>
        </div>
        <svg
          width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"
          className={`shrink-0 text-zinc-400 transition-transform duration-200 ${collapsed ? '' : 'rotate-180'}`}
        >
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>

      {!collapsed && (
        total === 0 ? (
          <div className="flex flex-1 items-center justify-center border-t border-zinc-100 px-4 py-6 text-center text-sm text-zinc-400 md:px-5 dark:border-zinc-800 dark:text-zinc-500">
            本组暂无通知
          </div>
        ) : (
          <>
            <ul className="flex-1 divide-y divide-zinc-100 dark:divide-zinc-800">
              {pageItems.map((n) => (
                <li key={n.id} className={`flex items-start gap-3 px-4 py-3 transition-colors hover:bg-zinc-50 md:px-5 dark:hover:bg-zinc-800/50 ${n.read ? 'opacity-60' : ''}`}>
                  <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${n.read ? 'bg-zinc-300 dark:bg-zinc-600' : group.rowAccent}`} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate text-sm font-semibold text-zinc-900 dark:text-zinc-100" title={n.reference}>{n.reference}</span>
                      <span className="shrink-0 text-[11px] tabular-nums text-zinc-400">{fmtShort(n.created_at)}</span>
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-xs text-zinc-500 dark:text-zinc-400">
                      {n.type === 'new-tag' ? (
                        <>
                          <span>{n.old_tag || '—'}</span>
                          <span className="text-zinc-300 dark:text-zinc-600">→</span>
                          <span className="font-medium text-blue-600 dark:text-blue-300">{n.new_tag}</span>
                        </>
                      ) : (
                        <>
                          <span title={n.old_digest}>{shortDigest(n.old_digest)}</span>
                          <span className="text-zinc-300 dark:text-zinc-600">→</span>
                          <span title={n.new_digest}>{shortDigest(n.new_digest)}</span>
                        </>
                      )}
                    </div>
                  </div>
                  {!n.read && (
                    <button
                      onClick={() => onMarkRead(n.id)}
                      title="标记为已读"
                      className="shrink-0 rounded-lg border border-zinc-200 px-2 py-0.5 text-[11px] text-zinc-600 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800"
                    >
                      已读
                    </button>
                  )}
                </li>
              ))}
            </ul>
            {/* 底部页脚：两条目数超一页才出现分页条；单页时展示等高的提示条，保证两栏页脚对齐 */}
            <div className="mt-auto border-t border-zinc-100 px-4 py-2.5 dark:border-zinc-800">
              {total > PAGE_SIZE ? (
                <Pagination page={cur} total={total} pageSize={PAGE_SIZE} onChange={onPage} />
              ) : (
                <p className="flex h-10 items-center justify-center text-xs text-zinc-400 dark:text-zinc-500">
                  共 {total} 条 · 单页展示
                </p>
              )}
            </div>
          </>
        )
      )}
    </section>
  )
}
