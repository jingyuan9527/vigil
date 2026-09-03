import { useEffect, useState } from 'react'
import { api, fmtTime, shortDigest } from '../api/client'
import BentoCard from '../components/BentoCard'
import StatusBadge from '../components/StatusBadge'
import Spinner from '../components/Spinner'

export default function Notifications() {
  const [items, setItems] = useState([])
  const [loading, setLoading] = useState(true)
  const [unreadOnly, setUnreadOnly] = useState(false)

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

  const markRead = async (id) => {
    await api.markRead(id)
    load()
  }
  const markAll = async () => {
    await api.markAllRead()
    load()
  }

  const unread = items.filter((i) => !i.read).length

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">更新通知</h1>
          <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">检测到镜像远端摘要变化时生成的更新提醒。</p>
        </div>
        <div className="flex items-center gap-2">
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
          <div className="mt-2 text-3xl font-bold text-zinc-900 dark:text-zinc-100">{items.length}</div>
        </BentoCard>
        <BentoCard>
          <div className="text-sm text-zinc-500 dark:text-zinc-400">未读</div>
          <div className="mt-2 text-3xl font-bold text-orange-500">{unread}</div>
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
        <BentoCard className="text-center text-sm text-zinc-400 dark:text-zinc-500">暂无通知</BentoCard>
      ) : (
        <div className="space-y-3">
          {items.map((n) => (
            <BentoCard key={n.id} className={n.read ? 'opacity-70' : ''}>
              <div className="flex flex-wrap items-start justify-between gap-3 overflow-hidden">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-base font-semibold text-zinc-900 dark:text-zinc-100">{n.reference}</span>
                    {!n.read && <span className="h-2 w-2 rounded-full bg-orange-500" />}
                  </div>
                  <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-300">{n.message}</p>
                  <div className="mt-2 flex flex-wrap gap-x-5 gap-y-1 font-mono text-xs text-zinc-400">
                    <span>旧：{shortDigest(n.old_digest)}</span>
                    <span>新：{shortDigest(n.new_digest)}</span>
                  </div>
                  <div className="mt-1 text-xs text-zinc-400">{fmtTime(n.created_at)}</div>
                </div>
                {!n.read && (
                  <button
                    onClick={() => markRead(n.id)}
                    className="shrink-0 rounded-xl border border-zinc-200 px-3 py-1.5 text-sm text-zinc-700 transition-colors hover:bg-zinc-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
                  >
                    标记已读
                  </button>
                )}
              </div>
            </BentoCard>
          ))}
        </div>
      )}
    </div>
  )
}
