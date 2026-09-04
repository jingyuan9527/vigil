import { useEffect, useState } from 'react'
import { api, fmtTime, shortDigest } from '../api/client'
import BentoCard from '../components/BentoCard'
import StatusBadge from '../components/StatusBadge'
import Spinner from '../components/Spinner'
import Pagination from '../components/Pagination'
import { useToast } from '../components/Toast'

const PAGE_SIZE = 10

export default function Notifications() {
  const toast = useToast()
  const [items, setItems] = useState([])
  const [loading, setLoading] = useState(true)
  const [unreadOnly, setUnreadOnly] = useState(false)
  const [page, setPage] = useState(1)

  const load = async () => {
    setLoading(true)
    try {
      const r = await api.notifications(unreadOnly)
      setItems(r.notifications || [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line
  }, [unreadOnly])

  // 切换筛选回第 1 页
  useEffect(() => {
    setPage(1)
  }, [unreadOnly])

  const markRead = async (id) => {
    try {
      await api.markRead(id)
      load()
    } catch {
      toast('error', '标记失败，请重试')
    }
  }
  const markAll = async () => {
    try {
      await api.markAllRead()
      load()
      toast('success', '已将全部通知标记为已读')
    } catch {
      toast('error', '操作失败，请重试')
    }
  }

  const unread = items.filter((i) => !i.read).length
  const total = items.length
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const pageItems = items.slice((Math.min(page, totalPages) - 1) * PAGE_SIZE, Math.min(page, totalPages) * PAGE_SIZE)

  const goPage = (p) => {
    setPage(p)
    requestAnimationFrame(() => {
      document.querySelector('.notif-list-top')?.scrollIntoView({ block: 'start', behavior: 'smooth' })
    })
  }

  return (
    <div className="notif-list-top space-y-6 overflow-hidden">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">更新通知</h1>
          <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">检测到镜像远端摘要变化时生成的更新提醒。</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={() => setUnreadOnly((v) => !v)}
            className={`rounded-xl px-3 py-2 text-sm font-medium transition-colors ${
              unreadOnly ? 'bg-zinc-900 text-white dark:bg-white dark:text-zinc-900' : 'bg-zinc-100 text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300'
            }`}
          >
            {unreadOnly ? '仅看未读' : '显示全部'}
          </button>
          <button
            onClick={markAll}
            disabled={unread === 0}
            className="rounded-xl bg-zinc-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-700 disabled:opacity-50 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200"
          >
            全部已读
          </button>
        </div>
      </div>

      {/* 概览卡片 */}
      <div className="bento-grid">
        <BentoCard>
          <div className="text-sm text-zinc-500 dark:text-zinc-400">通知总数</div>
          <div className="mt-2 text-3xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 tabular-nums">{items.length}</div>
        </BentoCard>
        <BentoCard>
          <div className="text-sm text-zinc-500 dark:text-zinc-400">未读</div>
          <div className="mt-2 text-3xl font-bold tracking-tight text-amber-500 tabular-nums">{unread}</div>
        </BentoCard>
        <BentoCard span="wide" className="flex items-center justify-between">
          <div>
            <div className="text-sm text-zinc-500 dark:text-zinc-400">更新可用镜像</div>
            <div className="mt-1 text-sm text-zinc-600 dark:text-zinc-300">在镜像列表可查看需要拉取新版本的项</div>
          </div>
          <StatusBadge status="update-available" />
        </BentoCard>
      </div>

      {loading ? (
        <Spinner label="加载通知…" />
      ) : items.length === 0 ? (
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
        <>
          <div className="space-y-3">
            {pageItems.map((n) => (
            <BentoCard key={n.id} className={n.read ? 'opacity-60' : ''}>
              <div className="flex flex-wrap items-start justify-between gap-3 overflow-hidden">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-3">
                    <span className="truncate text-base font-semibold text-zinc-900 dark:text-zinc-100">{n.reference}</span>
                    {n.type === 'new-tag' ? (
                      <span className="shrink-0 rounded-md bg-blue-100 px-1.5 py-0.5 text-[11px] font-medium text-blue-700 dark:bg-blue-500/15 dark:text-blue-300">
                        可选更新
                      </span>
                    ) : (
                      <span className="shrink-0 rounded-md bg-amber-100 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-500/15 dark:text-amber-300">
                        有新版本
                      </span>
                    )}
                    {!n.read && <span className="h-2 w-2 shrink-0 rounded-full bg-amber-500" />}
                  </div>
                  <p className="mt-1.5 text-sm text-zinc-600 dark:text-zinc-300">{n.message}</p>
                  {n.type === 'new-tag' ? (
                    <div className="mt-2.5 flex flex-wrap items-center gap-x-4 gap-y-1 rounded-xl bg-blue-50/70 px-3 py-2 dark:bg-blue-500/10">
                      <div className="flex items-center gap-1.5 text-xs">
                        <span className="text-zinc-400">当前</span>
                        <span className="font-mono text-zinc-600 dark:text-zinc-300">{n.old_tag || '—'}</span>
                      </div>
                      <span className="text-zinc-300 dark:text-zinc-600">→ 可选</span>
                      <div className="flex items-center gap-1.5 text-xs">
                        <span className="text-blue-600 dark:text-blue-300">升级到</span>
                        <span className="font-mono font-medium text-blue-700 dark:text-blue-300">{n.new_tag}</span>
                      </div>
                    </div>
                  ) : (
                    <div className="mt-2.5 flex flex-wrap items-center gap-x-4 gap-y-1 rounded-xl bg-zinc-50 px-3 py-2 dark:bg-zinc-800/50">
                      <div className="flex items-center gap-1.5 text-xs">
                        <span className="text-zinc-400">旧</span>
                        <span className="font-mono text-zinc-500 dark:text-zinc-400">{shortDigest(n.old_digest)}</span>
                      </div>
                      <span className="text-zinc-300 dark:text-zinc-600">→</span>
                      <div className="flex items-center gap-1.5 text-xs">
                        <span className="text-zinc-400">新</span>
                        <span className="font-mono text-zinc-500 dark:text-zinc-400">{shortDigest(n.new_digest)}</span>
                      </div>
                    </div>
                  )}
                  <div className="mt-2 text-xs text-zinc-400">{fmtTime(n.created_at)}</div>
                </div>
                {!n.read && (
                  <button
                    onClick={() => markRead(n.id)}
                    className="mt-3 w-full shrink-0 rounded-xl border border-zinc-200 px-3 py-1.5 text-sm text-zinc-700 transition-colors hover:bg-zinc-50 sm:mt-0 sm:w-auto dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
                  >
                    标记已读
                  </button>
                )}
              </div>
            </BentoCard>
          ))}
          </div>
          <Pagination page={page} total={total} pageSize={PAGE_SIZE} onChange={goPage} />
        </>
      )}
    </div>
  )
}
