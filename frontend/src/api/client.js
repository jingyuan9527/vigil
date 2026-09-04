const API = '/api'

// 认证基于 httpOnly cookie（SameSite=Lax）：同源 fetch 自动携带，
// JS 不再持有/读写令牌，降低 XSS 窃取面。令牌无效时刷新页面，
// 由 AuthProvider 重新校验并回到登录页。

async function handle(res) {
  if (res.status === 401) {
    window.location.reload()
    throw new Error('未授权')
  }
  if (!res.ok) throw new Error('请求失败: ' + res.status)
  return res.json()
}

async function getJSON(path) {
  const res = await fetch(API + path)
  return handle(res)
}

async function postJSON(path, body) {
  const res = await fetch(API + path, {
    method: 'POST',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  return handle(res)
}

async function putJSON(path, body) {
  const res = await fetch(API + path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  })
  return handle(res)
}

async function del(path) {
  const res = await fetch(API + path, { method: 'DELETE' })
  return handle(res)
}

export const api = {
  // Auth（不需要 token）
  authCheck: () => getJSON('/auth/check'),
  authSetup: (username, password) => postJSON('/auth/setup', { username, password }),
  authLogin: (username, password) => postJSON('/auth/login', { username, password }),
  authLogout: () => postJSON('/auth/logout'),

  // Protected APIs
  health: () => getJSON('/health'),
  stats: () => getJSON('/stats'),
  images: (status) => getJSON('/images' + (status ? `?status=${status}` : '')),
  image: (id) => getJSON('/images/' + id),
  scans: () => getJSON('/scans'),
  notifications: (unread, cursor) => getJSON('/notifications' + (unread ? '?unread=1' : '') + (cursor ? '&cursor=' + cursor : '')),
  scanNow: (force) => postJSON('/scan' + (force ? '?force=1' : '')),
  settings: () => getJSON('/settings'),
  saveSettings: (s) => putJSON('/settings', s),
  testDingTalk: (webhook, secret) => postJSON('/dingtalk/test', { webhook, secret }),
  addImage: (reference) => postJSON('/images', { reference }),
  removeImage: (id) => del('/images/' + id),
  setIgnored: (id, ignored) => putJSON(`/images/${id}/ignored`, { ignored }),
  setMode: (id, mode) => putJSON(`/images/${id}/mode`, { mode }),
  markRead: (id) => postJSON('/notifications/' + id + '/read'),
  markAllRead: () => postJSON('/notifications/read-all'),
}

// 检测模式标签（用于镜像卡/对比详情展示）
export function modeLabel(mode) {
  switch (mode) {
    case 'digest-only':
      return '仅摘要检测'
    case 'pin-watch':
      return '锁定+新标签监视'
    default:
      return '自动'
  }
}

// 短摘要：sha256:abcd...1234 -> abcd…1234
export function shortDigest(d) {
  if (!d) return '—'
  const v = d.startsWith('sha256:') ? d.slice(7) : d
  if (v.length <= 16) return v
  return v.slice(0, 8) + '…' + v.slice(-6)
}

export function fmtTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (isNaN(d)) return '—'
  return d.toLocaleString('zh-CN', { hour12: false })
}

// 紧凑时间：今天只显示 HH:mm，更早的显示 M/D（用于高密度列表行）
export function fmtShort(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (isNaN(d)) return '—'
  const now = new Date()
  if (d.toDateString() === now.toDateString()) {
    return d.toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit' })
  }
  return `${d.getMonth() + 1}/${d.getDate()}`
}