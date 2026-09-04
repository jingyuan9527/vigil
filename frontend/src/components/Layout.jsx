import { useEffect, useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { api, fmtTime } from '../api/client'
import { useTheme } from '../context/ThemeContext'
import { useAuth } from '../context/AuthContext'

const nav = [
  { to: '/', label: '仪表盘', icon: GridIcon, end: true },
  { to: '/images', label: '镜像', icon: BoxIcon },
  { to: '/compare', label: '对比', icon: GitIcon },
  { to: '/notifications', label: '通知', icon: BellIcon },
  { to: '/settings', label: '设置', icon: GearIcon },
]

export default function Layout() {
  const { theme, toggle } = useTheme()
  const { logout } = useAuth()
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
      {/* 桌面侧边栏（仅 lg 显示） */}
      <aside className="hidden shrink-0 flex-col gap-3 border-r border-zinc-100 bg-white px-5 py-7 dark:border-zinc-800 dark:bg-zinc-900 lg:flex lg:w-64">
        <div className="mb-2 flex items-center gap-2.5">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-blue-500 to-purple-600 text-white shadow-bento">
            <ShipIcon />
          </div>
          <div>
            <div className="text-base font-bold leading-tight">DockMon</div>
            <div className="text-xs text-zinc-400 dark:text-zinc-500">Docker 镜像监控</div>
          </div>
        </div>

        <nav className="mt-2 flex flex-1 flex-col gap-1.5">
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
              <span className="whitespace-nowrap">{n.label === '镜像' ? '镜像列表' : n.label === '对比' ? '版本对比' : n.label === '通知' ? '更新通知' : n.label}</span>
              {n.to === '/notifications' && unread > 0 && (
                <span className="ml-auto rounded-full bg-amber-500 px-1.5 py-0.5 text-[10px] font-semibold text-white">
                  {unread}
                </span>
              )}
            </NavLink>
          ))}
        </nav>

        <button
          onClick={toggle}
          className="flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm text-zinc-500 transition-colors hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800"
        >
          {theme === 'dark' ? <SunIcon /> : <MoonIcon />}
          {theme === 'dark' ? '浅色模式' : '深色模式'}
        </button>
        <button
          onClick={logout}
          className="flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm text-zinc-500 transition-colors hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800"
        >
          <LogoutIcon />
          退出登录
        </button>
      </aside>

      {/* 移动端顶部精简条（仅 <lg） */}
      <header className="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-zinc-100 bg-white px-4 dark:border-zinc-800 dark:bg-zinc-900 lg:hidden">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-xl bg-gradient-to-br from-blue-500 to-purple-600 text-white shadow-bento">
            <ShipIcon />
          </div>
          <div className="leading-tight">
            <div className="text-sm font-bold">DockMon</div>
            <div className="text-[10px] text-zinc-400 dark:text-zinc-500">
              最近扫描：{lastScan ? fmtTime(lastScan.started_at) : '暂无'}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2.5">
          <button
            onClick={onScan}
            disabled={scanning}
            className="flex h-11 w-11 items-center justify-center rounded-xl text-zinc-500 transition-colors hover:bg-zinc-100 disabled:opacity-60 dark:text-zinc-400 dark:hover:bg-zinc-800"
            aria-label="立即扫描"
          >
            <RefreshIcon spin={scanning} />
          </button>
          <button
            onClick={toggle}
            className="flex h-11 w-11 items-center justify-center rounded-xl text-zinc-500 transition-colors hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800"
            aria-label="切换主题"
          >
            {theme === 'dark' ? <SunIcon /> : <MoonIcon />}
          </button>
          <button
            onClick={logout}
            className="flex h-11 w-11 items-center justify-center rounded-xl text-zinc-500 transition-colors hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800"
            aria-label="退出登录"
          >
            <LogoutIcon />
          </button>
        </div>
      </header>

      {/* 主内容 */}
      <main className="flex-1 overflow-hidden px-4 py-6 pb-24 md:px-6 lg:px-8 lg:py-8 lg:pb-8">
        <div className="mx-auto max-w-6xl">
          {/* 桌面：最近扫描 + 扫描/主题（移动端已在顶栏） */}
          <div className="mb-6 hidden items-center justify-between gap-3 rounded-2xl border border-zinc-100 bg-white px-4 py-3 dark:border-zinc-800 dark:bg-zinc-900 lg:flex">
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
            <div className="flex items-center gap-3">
              <button
                onClick={toggle}
                className="flex h-10 w-10 items-center justify-center rounded-xl border border-zinc-200 text-zinc-500 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-400 dark:hover:bg-zinc-800"
                aria-label="切换主题"
              >
                {theme === 'dark' ? <SunIcon /> : <MoonIcon />}
              </button>
              <button
                onClick={onScan}
                disabled={scanning}
                className="inline-flex h-10 items-center gap-3 rounded-xl bg-zinc-900 px-4 text-sm font-medium text-white transition-colors hover:bg-zinc-700 disabled:opacity-60 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200"
              >
                <RefreshIcon spin={scanning} />
                {scanning ? '扫描中…' : '立即扫描'}
              </button>
            </div>
          </div>

          <Outlet context={{ refreshNotifs: refresh, navigate }} />
        </div>
      </main>

      {/* 移动端底部 5 项 Tab 栏（仅 <lg） */}
      <nav className="fixed inset-x-0 bottom-0 z-30 flex border-t border-zinc-100 bg-white pb-[env(safe-area-inset-bottom)] dark:border-zinc-800 dark:bg-zinc-900 lg:hidden">
        {nav.map((n) => (
          <NavLink
            key={n.to}
            to={n.to}
            end={n.end}
            className={({ isActive }) =>
              `group relative flex flex-1 flex-col items-center justify-center gap-1.5 py-2 min-h-[56px] text-xs font-medium transition-colors ${
                isActive
                  ? 'text-blue-600 dark:text-blue-400'
                  : 'text-zinc-400 dark:text-zinc-500'
              }`
            }
          >
            <span className="relative">
              <n.icon />
              {n.to === '/notifications' && unread > 0 && (
                <span className="absolute -right-2 -top-1.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-amber-500 px-1 text-[10px] font-semibold text-white">
                  {unread}
                </span>
              )}
            </span>
            {n.label}
          </NavLink>
        ))}
      </nav>
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
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <rect x="3" y="3" width="7" height="7" rx="1" /><rect x="14" y="3" width="7" height="7" rx="1" />
      <rect x="3" y="14" width="7" height="7" rx="1" /><rect x="14" y="14" width="7" height="7" rx="1" />
    </svg>
  )
}
function BoxIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M21 8 12 3 3 8v8l9 5 9-5V8Z" /><path d="m3 8 9 5 9-5" /><path d="M12 13v8" />
    </svg>
  )
}
function GitIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="6" cy="6" r="2.5" /><circle cx="6" cy="18" r="2.5" /><circle cx="18" cy="12" r="2.5" />
      <path d="M6 8.5v7" /><path d="M18 9.5a3.5 3.5 0 0 0-3.5-3.5H9" />
    </svg>
  )
}
function BellIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
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
function GearIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z" />
    </svg>
  )
}
function LogoutIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
      <polyline points="16 17 21 12 16 7" />
      <line x1="21" y1="12" x2="9" y2="12" />
    </svg>
  )
}
