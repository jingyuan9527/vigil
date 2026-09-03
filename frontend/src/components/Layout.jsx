import { useEffect, useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { api, fmtTime } from '../api/client'
import { useTheme } from '../context/ThemeContext'

const nav = [
  { to: '/', label: '仪表盘', icon: GridIcon, end: true },
  { to: '/images', label: '镜像列表', icon: BoxIcon },
  { to: '/compare', label: '版本对比', icon: GitIcon },
  { to: '/notifications', label: '更新通知', icon: BellIcon },
]

export default function Layout() {
  const { theme, toggle } = useTheme()
  const navigate = useNavigate()
  const [unread, setUnread] = useState(0)
  const [scanning, setScanning] = useState(false)
  const [lastScan, setLastScan] = useState(null)

  const refresh = async () => {
    try {
      const n = await api.notifications(true)
      setUnread(n.count || 0)
      const s = await api.scans()
      if (s.scans && s.scans.length) setLastScan(s.scans[0])
    } catch {
      /* ignore */
    }
  }

  useEffect(() => {
    refresh()
  }, [])

  const onScan = async () => {
    setScanning(true)
    try {
      await api.scanNow()
      setTimeout(refresh, 1500)
      setTimeout(refresh, 6000)
    } finally {
      setTimeout(() => setScanning(false), 8000)
    }
  }

  return (
    <div className="flex min-h-screen flex-col lg:flex-row">
      {/* Sidebar */}
      <aside className="flex shrink-0 flex-row gap-1 overflow-x-auto border-b border-zinc-100 bg-white px-4 py-3 dark:border-zinc-800 dark:bg-zinc-900 lg:w-64 lg:flex-col lg:gap-2 lg:border-b-0 lg:border-r lg:px-5 lg:py-7">
        <div className="mb-2 hidden items-center gap-2.5 lg:flex">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-blue-500 to-purple-600 text-white shadow-bento">
            <ShipIcon />
          </div>
          <div>
            <div className="text-base font-bold leading-tight">DockMon</div>
            <div className="text-xs text-zinc-400 dark:text-zinc-500">Docker 镜像监控</div>
          </div>
        </div>

        <nav className="flex flex-1 flex-row gap-1 lg:flex-col">
          {nav.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.end}
              className={({ isActive }) =>
                `group flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium transition-colors ${
                  isActive
                    ? 'bg-zinc-900 text-white dark:bg-white dark:text-zinc-900'
                    : 'text-zinc-600 hover:bg-zinc-100 dark:text-zinc-300 dark:hover:bg-zinc-800'
                }`
              }
            >
              <n.icon />
              <span className="whitespace-nowrap">{n.label}</span>
              {n.to === '/notifications' && unread > 0 && (
                <span className="ml-auto rounded-full bg-orange-500 px-1.5 py-0.5 text-[10px] font-semibold text-white">
                  {unread}
                </span>
              )}
            </NavLink>
          ))}
        </nav>

        <button
          onClick={toggle}
          className="hidden items-center gap-2 rounded-xl px-3 py-2.5 text-sm text-zinc-500 transition-colors hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800 lg:flex"
        >
          {theme === 'dark' ? <SunIcon /> : <MoonIcon />}
          {theme === 'dark' ? '浅色模式' : '深色模式'}
        </button>
      </aside>

      {/* Main */}
      <main className="flex-1 px-4 py-6 md:px-6 lg:px-8 lg:py-8">
        <div className="mx-auto max-w-6xl">
          <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
            <div className="text-xs text-zinc-400 dark:text-zinc-500">
              最近扫描：
              <span className="font-medium text-zinc-600 dark:text-zinc-300">
                {lastScan ? fmtTime(lastScan.started_at) : '暂无'}
              </span>
              {lastScan && (
                <span className="ml-2">
                  {lastScan.status === 'done'
                    ? `已检查 ${lastScan.images_checked} 个，发现 ${lastScan.updates_found} 处更新`
                    : lastScan.status}
                </span>
              )}
            </div>
            <div className="flex items-center gap-2">
              <button
                onClick={toggle}
                className="rounded-xl border border-zinc-200 p-2 text-zinc-500 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-400 dark:hover:bg-zinc-800 lg:hidden"
                aria-label="切换主题"
              >
                {theme === 'dark' ? <SunIcon /> : <MoonIcon />}
              </button>
              <button
                onClick={onScan}
                disabled={scanning}
                className="inline-flex items-center gap-2 rounded-xl bg-zinc-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-700 disabled:opacity-60 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200"
              >
                <RefreshIcon spin={scanning} />
                {scanning ? '扫描中…' : '立即扫描'}
              </button>
            </div>
          </div>

          <Outlet context={{ refreshNotifs: refresh, navigate }} />
        </div>
      </main>
    </div>
  )
}

/* ---------- inline icons ---------- */
function ShipIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M3 17h18l-2 4H5l-2-4Z" /><path d="M12 3v9" /><path d="M7 8h10l3 5H4l3-5Z" />
    </svg>
  )
}
function GridIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <rect x="3" y="3" width="7" height="7" rx="1" /><rect x="14" y="3" width="7" height="7" rx="1" />
      <rect x="3" y="14" width="7" height="7" rx="1" /><rect x="14" y="14" width="7" height="7" rx="1" />
    </svg>
  )
}
function BoxIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M21 8 12 3 3 8v8l9 5 9-5V8Z" /><path d="m3 8 9 5 9-5" /><path d="M12 13v8" />
    </svg>
  )
}
function GitIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="6" cy="6" r="2.5" /><circle cx="6" cy="18" r="2.5" /><circle cx="18" cy="12" r="2.5" />
      <path d="M6 8.5v7" /><path d="M18 9.5a3.5 3.5 0 0 0-3.5-3.5H9" />
    </svg>
  )
}
function BellIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
    </svg>
  )
}
function SunIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
    </svg>
  )
}
function MoonIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" />
    </svg>
  )
}
function RefreshIcon({ spin }) {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={spin ? 'animate-spin' : ''}>
      <path d="M21 12a9 9 0 1 1-2.6-6.4" /><path d="M21 3v6h-6" />
    </svg>
  )
}
