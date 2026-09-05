// 品牌图标：蓝色渐变圆角底 + 白鲸，与 public/favicon.svg、docs/logo.svg 同一图形源。
// 渐变 id 固定：页面同时渲染多个 Logo 时引用的是相同渐变定义，视觉无差异。
export default function Logo({ className = 'h-9 w-9' }) {
  return (
    <svg viewBox="0 0 64 64" className={className} aria-hidden="true">
      <defs>
        <linearGradient id="vigil-logo-gradient" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#38bdf8" />
          <stop offset="1" stopColor="#1d4ed8" />
        </linearGradient>
      </defs>
      <rect width="64" height="64" rx="14" fill="url(#vigil-logo-gradient)" />
      <path d="M46.5 18.5 C46.5 16.5 46.5 15 46.5 13.5" stroke="#fff" strokeWidth="2.6" strokeLinecap="round" fill="none" />
      <circle cx="42.5" cy="11.5" r="1.7" fill="#fff" />
      <circle cx="50.5" cy="11.5" r="1.7" fill="#fff" />
      <circle cx="46.5" cy="8" r="1.9" fill="#fff" />
      <path d="M21.5 34 C17 28.5 12 22.5 8.5 17.5 C7 24.5 9.5 32 14.5 36.8 C9.5 38.5 7.5 45.5 9.5 52 C15 48.5 19.5 43 21 38.5 Z" fill="#fff" />
      <path d="M20.5 33 C26 22.5 39 17.5 47.5 21.5 C55 25 58.5 30.5 58.5 36.5 C58.5 44.5 50 49.5 39.5 49.5 C31.5 49.5 24.5 46.5 20.5 41 Z" fill="#fff" />
      <path d="M29.5 46 C28.5 50 30.5 52.8 34.5 53 C36.8 51.8 37 48.2 35.5 45.8 Z" fill="#bae6fd" />
      <circle cx="48" cy="31.5" r="2.1" fill="#0c4a6e" />
      <path d="M51.5 38.5 C49.5 40 47 40.5 44 40.3" stroke="#0c4a6e" strokeWidth="1.4" strokeLinecap="round" fill="none" opacity=".8" />
    </svg>
  )
}
