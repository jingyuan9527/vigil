const API = '/api'

function getToken() {
  return localStorage.getItem('dockmon_token')
}

function authHeaders(extra) {
  const h = { ...extra }
  const t = getToken()
  if (t) h['Authorization'] = 'Bearer ' + t
  return h
}

async function getJSON(path) {
  const res = await fetch(API + path, { headers: authHeaders() })
  if (res.status === 401) {
    localStorage.removeItem('dockmon_token')
    window.location.reload()
    throw new Error('未授权')
  }
  if (!res.ok) throw new Error('请求失败: ' + res.status)
  return res.json()
}

async function postJSON(path, body) {
  const res = await fetch(API + path, {
    method: 'POST',
    headers: authHeaders({ 'Content-Type': 'application/json' }),
    body: body ? JSON.stringify(body) : undefined,
  })
  if (res.status === 401) {
    localStorage.removeItem('dockmon_token')
    window.location.reload()
    throw new Error('未授权')
  }
  if (!res.ok) throw new Error('请求失败: ' + res.status)
  return res.json()
}

async function putJSON(path, body) {
  const res = await fetch(API + path, {
    method: 'PUT',
    headers: authHeaders({ 'Content-Type': 'application/json' }),
    body: body ? JSON.stringify(body) : undefined,
  })
  if (res.status === 401) {
    localStorage.removeItem('dockmon_token')
    window.location.reload()
    throw new Error('未授权')
  }
  if (!res.ok) throw new Error('请求失败: ' + res.status)
  return res.json()
}

async function del(path) {
  const res = await fetch(API + path, { method: 'DELETE', headers: authHeaders() })
  if (res.status === 401) {
    localStorage.removeItem('dockmon_token')
    window.location.reload()
    throw new Error('未授权')
  }
  if (!res.ok) throw new Error('请求失败: ' + res.status)
  return res.json()
}

export const api = {
  // Auth（不需要 token）
  authCheck: () => getJSON('/auth/check'),
  authSetup: (username, password) => postJSON('/auth/setup', { username, password }),
  authLogin: (username, password) => postJSON('/auth/login', { username, password }),

  // Protected APIs
  health: () => getJSON('/health'),
  stats: () => getJSON('/stats'),
  images: (status) => getJSON('/images' + (status ? `?status=${status}` : '')),
  image: (id) => getJSON('/images/' + id),
  scans: () => getJSON('/scans'),
  notifications: (unread) => getJSON('/notifications' + (unread ? '?unread=1' : '')),
  scanNow: () => postJSON('/scan'),
  settings: () => getJSON('/settings'),
  saveSettings: (s) => putJSON('/settings', s),
  testDingTalk: (webhook, secret) => postJSON('/dingtalk/test', { webhook, secret }),
  addImage: (reference) => postJSON('/images', { reference }),
  removeImage: (id) => del('/images/' + id),
  setIgnored: (id, ignored) => putJSON(`/images/${id}/ignored`, { ignored }),
  markRead: (id) => postJSON('/notifications/' + id + '/read'),
  markAllRead: () => postJSON('/notifications/read-all'),
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
