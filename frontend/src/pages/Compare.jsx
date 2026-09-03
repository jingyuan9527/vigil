import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, fmtTime, shortDigest } from '../api/client'
import BentoCard from '../components/BentoCard'
import StatusBadge from '../components/StatusBadge'
import Spinner from '../components/Spinner'

export default function Compare() {
  const [params, setParams] = useSearchParams()
  const [images, setImages] = useState([])
  const [detail, setDetail] = useState(null)
  const [loading, setLoading] = useState(true)
  const [loadingDetail, setLoadingDetail] = useState(false)

  const id = params.get('id')

  useEffect(() => {
    ;(async () => {
      try {
        const r = await api.images()
        setImages(r.images || [])
        if (!id && r.images && r.images.length) {
          setParams({ id: String(r.images[0].id) }, { replace: true })
        }
      } finally {
        setLoading(false)
      }
    })()
  }, [])

  useEffect(() => {
    if (!id) return
    setLoadingDetail(true)
    api
      .image(id)
      .then(setDetail)
      .catch(() => setDetail(null))
      .finally(() => setLoadingDetail(false))
  }, [id])

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">版本对比</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">对比本地与远端镜像摘要，查看版本时间线与可用标签。</p>
      </div>

      <div className="grid gap-6 overflow-hidden lg:grid-cols-[260px_1fr]">
        {/* 选择器 */}
        <div className="min-w-0 space-y-2">
          {loading ? (
            <Spinner />
          ) : (
            images.map((i) => (
              <button
                key={i.id}
                onClick={() => setParams({ id: String(i.id) })}
                className={`bento-card flex w-full items-center justify-between p-3 text-left transition-all ${
                  String(i.id) === id
                    ? 'border-blue-500 bg-blue-50/50 ring-1 ring-blue-500/20 dark:bg-blue-900/10'
                    : 'hover:border-zinc-200 dark:hover:border-zinc-700'
                }`}
              >
                <span className="truncate text-sm font-medium text-zinc-800 dark:text-zinc-100">{i.reference}</span>
                <StatusBadge status={i.status} />
              </button>
            ))
          )}
        </div>

        {/* 详情 */}
        {loadingDetail || !detail ? (
          <Spinner label="加载版本详情…" />
        ) : (
          <div className="space-y-6">
            {/* 当前 vs 最新 */}
            <div className="bento-grid">
              <BentoCard span="wide">
                <div className="mb-3 flex items-center justify-between">
                  <h3 className="font-semibold text-zinc-900 dark:text-zinc-100">{detail.image.reference}</h3>
                  <StatusBadge status={detail.image.status} />
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  <CompareCol title="本地版本" tag={detail.image.tag} digest={shortDigest(detail.image.local_digest)} accent="from-emerald-400 to-cyan-500" />
                  <CompareCol title="远端最新" tag={detail.image.tag} digest={shortDigest(detail.image.remote_digest)} accent="from-orange-400 to-pink-500" />
                </div>
                <div className="mt-4 flex flex-wrap gap-x-6 gap-y-1 text-xs text-zinc-400">
                  <span>最近检查：{fmtTime(detail.image.last_check)}</span>
                  <span>远端变更：{fmtTime(detail.image.last_update)}</span>
                  <span>来源：{detail.image.source === 'docker' ? 'Docker 守护进程' : '手动监控'}</span>
                </div>
              </BentoCard>

              {/* 可用标签 */}
              <BentoCard span="tall">
                <h3 className="font-semibold text-zinc-900 dark:text-zinc-100">可用标签</h3>
                <p className="mt-1 text-xs text-zinc-400">注册表中可拉取的其它版本</p>
                <div className="mt-3 flex flex-wrap gap-2">
                  {detail.tags && detail.tags.length ? (
                    detail.tags.slice(0, 24).map((t) => (
                      <span key={t} className="rounded-xl bg-zinc-100 px-2.5 py-1 font-mono text-xs text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300">
                        {t}
                      </span>
                    ))
                  ) : (
                    <span className="text-xs text-zinc-400">注册表未公开标签列表</span>
                  )}
                </div>
              </BentoCard>
            </div>

            {/* 版本时间线 */}
            <BentoCard>
              <h3 className="font-semibold text-zinc-900 dark:text-zinc-100">版本时间线</h3>
              <p className="mb-4 text-xs text-zinc-400">每次扫描记录到的远端摘要快照</p>
              <div className="space-y-0">
                {detail.versions && detail.versions.length ? (
                  detail.versions.map((v, idx) => (
                    <div key={v.id} className="flex items-start gap-3">
                      <div className="flex flex-col items-center pt-2">
                        <span className={`h-3 w-3 shrink-0 rounded-full ring-2 ring-white dark:ring-zinc-900 ${idx === 0 ? 'bg-orange-500' : 'bg-zinc-300 dark:bg-zinc-600'}`} />
                        {idx < detail.versions.length - 1 && <span className="h-full w-px flex-1 bg-zinc-200 dark:bg-zinc-700" />}
                      </div>
                      <div className="flex w-full items-center justify-between rounded-xl border border-zinc-100 px-3 py-2.5 transition-colors hover:border-zinc-200 hover:bg-zinc-50 dark:border-zinc-800 dark:hover:border-zinc-700 dark:hover:bg-zinc-800/50">
                        <span className="font-mono text-sm text-zinc-700 dark:text-zinc-200">{shortDigest(v.digest)}</span>
                        <span className="text-xs text-zinc-400">{fmtTime(v.scanned_at)}</span>
                      </div>
                    </div>
                  ))
                ) : (
                  <p className="text-sm text-zinc-400">暂无版本快照</p>
                )}
              </div>
            </BentoCard>
          </div>
        )}
      </div>
    </div>
  )
}

function CompareCol({ title, tag, digest, accent }) {
  return (
    <div className="rounded-2xl border border-zinc-100 p-4 transition-colors hover:border-zinc-200 dark:border-zinc-800 dark:hover:border-zinc-700">
      <div className="flex items-center gap-2">
        <span className={`flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-br ${accent} text-white shadow-sm`}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round"><path d="M20 6 9 17l-5-5" /></svg>
        </span>
        <span className="text-sm font-medium text-zinc-700 dark:text-zinc-200">{title}</span>
        <span className="ml-auto rounded-lg bg-zinc-100 px-2 py-0.5 text-xs font-medium text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300">{tag}</span>
      </div>
      <div className="mt-3 font-mono text-sm text-zinc-500 dark:text-zinc-400">{digest}</div>
    </div>
  )
}
