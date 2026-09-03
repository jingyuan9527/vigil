import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, fmtTime, shortDigest } from '../api/client'
import BentoCard from '../components/BentoCard'
import StatusBadge from '../components/StatusBadge'
import Spinner from '../components/Spinner'

const FILTERS = [
  { key: '', label: '全部' },
  { key: 'update-available', label: '有更新' },
  { key: 'up-to-date', label: '已是最新' },
  { key: 'unknown', label: '未知' },
]

export default function Images() {
  const navigate = useNavigate()
  const [images, setImages] = useState([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState('')
  const [query, setQuery] = useState('')
  const [newRef, setNewRef] = useState('')
  const [adding, setAdding] = useState(false)
  const [err, setErr] = useState('')

  const load = async () => {
    setLoading(true)
    try {
      const r = await api.images(filter)
      setImages(r.images || [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line
  }, [filter])

  const filtered = useMemo(() => {
    if (!query) return images
    return images.filter((i) => i.reference.toLowerCase().includes(query.toLowerCase()))
  }, [images, query])

  const onAdd = async (e) => {
    e.preventDefault()
    setErr('')
    if (!newRef.trim()) return
    setAdding(true)
    try {
      await api.addImage(newRef.trim())
      setNewRef('')
      await load()
    } catch {
      setErr('添加失败，请确认引用格式（如 nginx:latest）')
    } finally {
      setAdding(false)
    }
  }

  const onRemove = async (id) => {
    if (!confirm('确定移除该监控项？')) return
    await api.removeImage(id)
    await load()
  }

  return (
    <div className="space-y-6 overflow-hidden">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">镜像列表</h1>
          <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">所有被监控的镜像引用及其版本状态。</p>
        </div>
        <form onSubmit={onAdd} className="flex min-w-0 items-center gap-2">
          <input
            value={newRef}
            onChange={(e) => setNewRef(e.target.value)}
            placeholder="新增监控，如 redis:7"
            className="min-w-0 flex-1 rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm outline-none transition-all focus:border-bento-accent focus:ring-2 focus:ring-blue-500/20 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
          />
          <button disabled={adding} className="rounded-xl bg-zinc-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-700 disabled:opacity-60 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200">
            {adding ? '添加中…' : '添加'}
          </button>
        </form>
      </div>

      {err && <div className="rounded-xl bg-rose-50 px-4 py-2 text-sm text-rose-600 dark:bg-rose-500/10 dark:text-rose-300">{err}</div>}

      <div className="flex flex-wrap items-center gap-2 overflow-hidden">
        {FILTERS.map((f) => (
          <button
            key={f.key}
            onClick={() => setFilter(f.key)}
            className={`rounded-xl px-3 py-1.5 text-sm font-medium transition-colors ${
              filter === f.key
                ? 'bg-zinc-900 text-white dark:bg-white dark:text-zinc-900'
                : 'bg-zinc-100 text-zinc-600 hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-300'
            }`}
          >
            {f.label}
          </button>
        ))}
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="搜索引用…"
          className="ml-auto w-full max-w-[11rem] rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-1.5 text-sm outline-none transition-all focus:border-bento-accent focus:ring-2 focus:ring-blue-500/20 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
        />
      </div>

      {loading ? (
        <Spinner label="加载镜像…" />
      ) : filtered.length === 0 ? (
        <BentoCard className="text-center text-sm text-zinc-400 dark:text-zinc-500">没有匹配的镜像</BentoCard>
      ) : (
        <div className="bento-grid">
          {filtered.map((img) => (
            <BentoCard key={img.id} className="group flex flex-col">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <div className="truncate text-base font-semibold text-zinc-900 dark:text-zinc-100" title={img.reference}>
                    {img.reference}
                  </div>
                  <div className="mt-0.5 flex items-center gap-1.5 text-xs text-zinc-400">
                    <span className={`h-1.5 w-1.5 rounded-full ${img.source === 'docker' ? 'bg-blue-400' : 'bg-zinc-300 dark:bg-zinc-600'}`} />
                    {img.source === 'docker' ? 'Docker' : '手动'}
                  </div>
                </div>
                <StatusBadge status={img.status} />
              </div>

              <div className="mt-4 space-y-1.5 text-xs">
                <DigestRow label="本地" value={shortDigest(img.local_digest)} />
                <DigestRow label="远端" value={shortDigest(img.remote_digest)} />
              </div>

              <div className="mt-auto pt-3">
                <div className="mb-3 text-xs text-zinc-400">
                  {img.last_check ? fmtTime(img.last_check) : '未扫描'}
                </div>
                <div className="flex gap-2">
                  <button
                    onClick={() => navigate('/compare?id=' + img.id)}
                    className="flex-1 rounded-xl border border-zinc-200 py-1.5 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-50 hover:border-zinc-300 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800 dark:hover:border-zinc-600"
                  >
                    版本对比
                  </button>
                  <button
                    onClick={() => onRemove(img.id)}
                    className="rounded-xl border border-zinc-200 px-3 py-1.5 text-sm text-zinc-400 transition-colors hover:bg-rose-50 hover:text-rose-500 hover:border-rose-200 dark:border-zinc-700 dark:hover:bg-rose-500/10 dark:hover:text-rose-400 dark:hover:border-rose-800"
                  >
                    移除
                  </button>
                </div>
              </div>
            </BentoCard>
          ))}
        </div>
      )}
    </div>
  )
}

function DigestRow({ label, value }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <span className="shrink-0 text-zinc-400">{label}</span>
      <span className="truncate font-mono text-zinc-600 dark:text-zinc-300">{value}</span>
    </div>
  )
}
