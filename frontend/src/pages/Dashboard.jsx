import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, fmtTime, shortDigest } from '../api/client'
import BentoCard from '../components/BentoCard'
import StatCard from '../components/StatCard'
import StatusBadge from '../components/StatusBadge'
import Spinner from '../components/Spinner'

function IconBox({ path }) {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      {path}
    </svg>
  )
}

export default function Dashboard() {
  const navigate = useNavigate()
  const [stats, setStats] = useState(null)
  const [images, setImages] = useState([])
  const [notifs, setNotifs] = useState([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    ;(async () => {
      try {
        const [s, imgs, n] = await Promise.all([api.stats(), api.images(), api.notifications(false)])
        setStats(s)
        setImages(imgs.images || [])
        setNotifs((n.notifications || []).slice(0, 6))
      } catch (e) {
        // ignore
      } finally {
        setLoading(false)
      }
    })()
  }, [])

  if (loading || !stats) return <Spinner label="加载仪表盘…" />

  const updates = images.filter((i) => i.status === 'update-available')
  const dist = [
    { label: '已是最新', v: stats.up_to_date, color: 'bg-emerald-500' },
    { label: '有更新', v: stats.update_available, color: 'bg-amber-500' },
    { label: '未知', v: stats.unknown, color: 'bg-zinc-300 dark:bg-zinc-600' },
  ]
  const totalDist = dist.reduce((a, b) => a + b.v, 0) || 1

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">仪表盘</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">实时掌握所有被监控镜像的版本状态与更新动态。</p>
      </div>

      <div className="bento-grid">
        {/* Hero 大卡片 */}
        <BentoCard span="lg" className="bg-gradient-to-br from-blue-500 to-purple-600 text-white">
          <div className="flex h-full flex-col justify-between">
            <div>
              <div className="inline-flex rounded-xl bg-white/15 px-2.5 py-1 text-xs font-medium">系统状态</div>
              <h2 className="mt-4 text-2xl font-bold md:text-3xl">Docker 镜像监控总览</h2>
              <p className="mt-2 max-w-md text-sm text-white/80">
                {stats.total === 0
                  ? '尚未添加任何监控镜像。前往镜像列表添加引用（如 nginx:latest），即可开始比对本地与注册表摘要并推送更新提醒。'
                  : '自动采集本地与注册表镜像，比对摘要并推送更新提醒。当前共监控'}
                {stats.total > 0 && <span className="mx-1 font-semibold">{stats.total}</span>}
                {stats.total > 0 && '个镜像引用。'}
              </p>
            </div>
            <div className="mt-6">
              {stats.total === 0 ? (
                <button
                  onClick={() => navigate('/images')}
                  className="inline-flex items-center rounded-xl bg-white px-4 py-2 text-sm font-medium text-blue-700 transition-colors hover:bg-white/90 active:scale-95"
                >
                  去添加第一个镜像
                </button>
              ) : (
                <div className="grid grid-cols-3 gap-3">
                  <Mini label="最新" value={stats.up_to_date} />
                  <Mini label="待更新" value={stats.update_available} />
                  <Mini label="未读" value={stats.unread_notifications} />
                </div>
              )}
            </div>
          </div>
        </BentoCard>

        <StatCard label="监控镜像总数" value={stats.total} gradient="from-blue-500 to-violet-600"
          icon={<IconBox path={<><rect x="3" y="3" width="7" height="7" rx="1" /><rect x="14" y="3" width="7" height="7" rx="1" /><rect x="3" y="14" width="7" height="7" rx="1" /><rect x="14" y="14" width="7" height="7" rx="1" /></>} />} />
        <StatCard label="有更新可用" value={stats.update_available} gradient="from-orange-400 to-pink-500"
          icon={<IconBox path={<><path d="M12 2v8" /><path d="m8 6 4-4 4 4" /><circle cx="12" cy="15" r="6" /><path d="M12 12v6" /></>} />} />
        <StatCard label="已是最新" value={stats.up_to_date} gradient="from-green-400 to-cyan-500"
          icon={<IconBox path={<><path d="M20 6 9 17l-5-5" /></>} />} />
        <StatCard label="未读通知" value={stats.unread_notifications} gradient="from-blue-500 to-violet-600"
          icon={<IconBox path={<><path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" /></>} />} />

        {/* 状态分布 */}
        <BentoCard span="wide">
          <div className="mb-4 flex items-center justify-between">
            <h3 className="font-semibold text-zinc-900 dark:text-zinc-100">状态分布</h3>
            <span className="text-xs text-zinc-400 dark:text-zinc-500">共 {totalDist} 个镜像</span>
          </div>
          <div className="flex h-3 w-full gap-0 overflow-hidden rounded-full bg-zinc-100 dark:bg-zinc-800">
            {dist.map((d, i) => (
              <div
                key={d.label}
                className={`${d.color} transition-all duration-500`}
                style={{
                  width: `${(d.v / totalDist) * 100}%`,
                  borderRadius: i === 0 ? '9999px 0 0 9999px' : i === dist.length - 1 ? '0 9999px 9999px 0' : '0',
                }}
              />
            ))}
          </div>
          <div className="mt-4 grid grid-cols-3 gap-3">
            {dist.map((d) => (
              <div key={d.label} className="flex items-center gap-2.5 text-sm">
                <span className={`h-2.5 w-2.5 rounded-full ${d.color}`} />
                <span className="text-zinc-600 dark:text-zinc-300">{d.label}</span>
                <span className="ml-auto font-semibold text-zinc-900 dark:text-zinc-100">{d.v}</span>
              </div>
            ))}
          </div>
        </BentoCard>

        {/* 最近更新动态 */}
        <BentoCard span="tall">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="font-semibold text-zinc-900 dark:text-zinc-100">最近更新动态</h3>
            <button onClick={() => navigate('/notifications')} className="text-xs font-medium text-bento-accent hover:underline">查看全部</button>
          </div>
          <div className="space-y-2.5">
            {updates.length === 0 && notifs.length === 0 && (
              <p className="py-8 text-center text-sm text-zinc-400 dark:text-zinc-500">暂无更新动态</p>
            )}
            {updates.slice(0, 5).map((i) => (
              <div key={i.id} className="rounded-xl p-3 transition-colors hover:bg-zinc-50 dark:hover:bg-zinc-800/50">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate text-sm font-medium text-zinc-800 dark:text-zinc-100">{i.reference}</div>
                    <div className="mt-1 font-mono text-[11px] text-zinc-400">
                      {shortDigest(i.local_digest)} → {shortDigest(i.remote_digest)}
                    </div>
                  </div>
                  <StatusBadge status="update-available" />
                </div>
              </div>
            ))}
            {notifs.slice(0, 3).map((n) => (
              <div key={n.id} className="rounded-xl p-3 transition-colors hover:bg-zinc-50 dark:hover:bg-zinc-800/50">
                <div className="truncate text-sm font-medium text-zinc-800 dark:text-zinc-100">{n.reference}</div>
                <div className="mt-1 text-xs text-zinc-400">{fmtTime(n.created_at)}</div>
              </div>
            ))}
          </div>
        </BentoCard>

        {/* 扫描信息 */}
        <BentoCard>
          <h3 className="font-semibold text-zinc-900 dark:text-zinc-100">扫描信息</h3>
          <div className="mt-3 space-y-2 text-sm">
            <Row k="最近扫描" v={stats.last_scan_at ? fmtTime(stats.last_scan_at) : '暂无'} />
            <Row k="扫描状态" v={stats.last_scan_status || '—'} />
            <Row k="监控引用" v={`${stats.total} 个`} />
          </div>
          <button onClick={() => navigate('/images')} className="mt-4 w-full rounded-xl border border-zinc-200 py-2 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800">
            浏览镜像列表
          </button>
        </BentoCard>
      </div>
    </div>
  )
}

function Mini({ label, value }) {
  return (
    <div className="rounded-xl bg-white/10 p-3 text-center">
      <div className="text-2xl font-bold tabular-nums">{value}</div>
      <div className="text-xs text-white/70">{label}</div>
    </div>
  )
}
function Row({ k, v }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-zinc-500 dark:text-zinc-400">{k}</span>
      <span className="font-medium text-zinc-800 dark:text-zinc-100">{v}</span>
    </div>
  )
}
