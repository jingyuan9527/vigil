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

  const pick = (i) => setParams({ id: String(i.id) })

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">版本对比</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">对比本地与远端镜像摘要，查看版本时间线与可用标签。</p>
      </div>

      {/* 移动端选择器：顶部横向滚动 chip 行（<lg） */}
      {!loading && images.length > 0 && (
        <div className="flex gap-3 overflow-x-auto pb-1 lg:hidden" role="tablist" aria-label="选择镜像">
          {images.map((i) => {
            const active = String(i.id) === id
            return (
              <button
                key={i.id}
                onClick={() => pick(i)}
                aria-current={active ? 'true' : undefined}
                className={`shrink-0 rounded-xl px-3 py-2 text-sm font-medium transition-colors ${
                  active
                    ? 'border border-blue-500 bg-blue-50/50 text-blue-700 ring-1 ring-blue-500/20 dark:bg-blue-900/20 dark:text-blue-300'
                    : 'border border-zinc-200 bg-white text-zinc-600 hover:bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800'
                }`}
              >
                {i.reference}
              </button>
            )
          })}
        </div>
      )}

      <div className="grid gap-6 overflow-hidden lg:h-[calc(100vh-230px)] lg:grid-cols-[260px_1fr]">
        {/* 桌面选择器（仅 lg） */}
        <div className="hidden min-w-0 space-y-2 overflow-y-auto pr-1 lg:block lg:max-h-full">
          {loading ? (
            <Spinner />
          ) : images.length === 0 ? (
            <BentoCard className="text-center text-sm text-zinc-400 dark:text-zinc-500">暂无镜像</BentoCard>
          ) : (
            images.map((i) => (
              <button
                key={i.id}
                onClick={() => pick(i)}
                className={`bento-card flex w-full items-center justify-between gap-3 p-3 text-left transition-all ${
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
        <div className="min-w-0 overflow-y-auto">
        {!id ? (
          <BentoCard className="flex min-h-40 items-center justify-center text-center text-sm text-zinc-400 dark:text-zinc-500">
            请选择一个镜像查看版本详情
          </BentoCard>
        ) : loadingDetail ? (
          <Spinner label="加载版本详情…" />
        ) : !detail ? (
          <BentoCard className="flex min-h-40 items-center justify-center text-center text-sm text-zinc-400 dark:text-zinc-500">
            镜像不存在或已被移除
          </BentoCard>
        ) : (
          <div className="space-y-6">
            {/* 当前 vs 最新 */}
            <div className="bento-grid">
              <BentoCard span="wide">
                <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                  <h3 className="font-semibold text-zinc-900 dark:text-zinc-100">{detail.image.reference}</h3>
                  <StatusBadge status={detail.image.status} />
                </div>
                {/* 分隔列（divide-x），不包子卡片（规则 G：禁止嵌套卡片） */}
                <div className="flex flex-col gap-4 sm:flex-row sm:gap-0 sm:divide-x sm:divide-zinc-100 dark:sm:divide-zinc-800">
                  <CompareCol title="本地版本" tag={detail.image.tag} digest={shortDigest(detail.image.local_digest)} accent="from-green-400 to-cyan-500" side="left" />
                  <CompareCol title="远端最新" tag={detail.image.tag} digest={shortDigest(detail.image.remote_digest)} accent="from-orange-400 to-pink-500" side="right" />
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
                <div className="mt-3 flex flex-wrap gap-3">
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
                        <span className={`h-3 w-3 shrink-0 rounded-full ring-2 ring-white dark:ring-zinc-900 ${idx === 0 ? 'bg-amber-500' : 'bg-zinc-300 dark:bg-zinc-600'}`} />
                        {idx < detail.versions.length - 1 && <span className="h-full w-px flex-1 bg-zinc-200 dark:bg-zinc-700" />}
                      </div>
                      <div className="flex w-full items-center justify-between gap-3 rounded-xl px-3 py-2.5 transition-colors hover:bg-zinc-50 dark:hover:bg-zinc-800/50">
                        <span className="truncate font-mono text-sm text-zinc-700 dark:text-zinc-200" title={v.digest}>{shortDigest(v.digest)}</span>
                        <span className="shrink-0 text-xs text-zinc-400">{fmtTime(v.scanned_at)}</span>
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
    </div>
  )
}

// 内部列：无自身边框/阴影/圆角，仅由父级 divide-x 分隔（规范 §5.4 / 规则 G）
function CompareCol({ title, tag, digest, accent, side }) {
  return (
    <div className={`min-w-0 flex-1 ${side === 'right' ? 'sm:pl-6' : 'sm:pr-6'}`}>
      <div className="flex items-center gap-3">
        <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-gradient-to-br ${accent} text-white shadow-sm transition-transform duration-200 group-hover:scale-110`}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round"><path d="M20 6 9 17l-5-5" /></svg>
        </span>
        <span className="text-sm font-medium text-zinc-700 dark:text-zinc-200">{title}</span>
        <span className="ml-auto rounded-lg bg-zinc-100 px-2 py-0.5 text-xs font-medium text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300">{tag}</span>
      </div>
      <div className="mt-3 break-all font-mono text-sm leading-relaxed text-zinc-500 dark:text-zinc-400" title={digest}>{digest}</div>
    </div>
  )
}
