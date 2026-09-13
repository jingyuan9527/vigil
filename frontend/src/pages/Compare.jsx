import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, fmtTime, modeLabel, shortDigest, SCAN_DONE_EVENT } from '../api/client'
import BentoCard from '../components/BentoCard'
import StatusBadge from '../components/StatusBadge'
import Spinner from '../components/Spinner'
import ErrorState from '../components/ErrorState'

const TL_PREVIEW = 10 // 版本时间线默认展示条数，超出折叠
const LATEST_VERSIONS = 2 // 版本号区域仅展示最近 N 个数字版本（最新者高亮）

// 状态过滤（作用于镜像选择器）
const FILTERS = [
  { key: '', label: '全部' },
  { key: 'update-available', label: '有更新' },
  { key: 'up-to-date', label: '已是最新' },
]

// 列表排序权重：有更新的镜像排最前，方便优先比对
const STATUS_ORDER = { 'update-available': 0, unknown: 1, 'up-to-date': 2, stale: 3 }

// 紧凑状态点（选择器用，替代整枚徽章省宽度）
const STATUS_DOT = {
  'up-to-date': 'bg-emerald-500',
  'update-available': 'bg-amber-500',
  'new-tag': 'bg-blue-500',
  unknown: 'bg-zinc-400',
  stale: 'bg-rose-500',
  ignored: 'bg-zinc-400',
}

// 版本号感知的 tag 排序：与后端 version.Compare 一致（按段比较、缺段补 0），
// 可解析的降序在前（新版本一眼可见），其余（浮动/描述性 tag）按字典序垫底。
function parseVer(tag) {
  const m = String(tag).replace(/^v/i, '').match(/^(\d+(?:\.\d+)*)(?:$|[-+._])/)
  if (!m) return null
  return m[1].split('.').map(Number)
}
function cmpVer(a, b) {
  const n = Math.max(a.length, b.length)
  for (let i = 0; i < n; i++) {
    const x = a[i] || 0
    const y = b[i] || 0
    if (x !== y) return x - y
  }
  return 0
}
function sortTagsVersionFirst(tags) {
  const numbered = []
  const others = []
  for (const t of tags) {
    const v = parseVer(t)
    if (v) numbered.push([t, v])
    else others.push(t)
  }
  numbered.sort((a, b) => cmpVer(b[1], a[1]) || b[0].localeCompare(a[0]))
  others.sort((a, b) => a.localeCompare(b))
  return [...numbered.map(([t]) => t), ...others]
}

export default function Compare() {
  const [params, setParams] = useSearchParams()
  const [images, setImages] = useState([])
  const [detail, setDetail] = useState(null)
  const [loading, setLoading] = useState(true)
  const [loadingDetail, setLoadingDetail] = useState(false)
  const [listError, setListError] = useState(false)
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState('')
  const [selectorOpen, setSelectorOpen] = useState(false) // 移动端 chip 列表展开
  const [selectorOverflow, setSelectorOverflow] = useState(false) // 折叠态是否有被截断的 chip
  const [tlOpen, setTlOpen] = useState(false) // 版本时间线展开
  const selectorRef = useRef(null)

  const id = params.get('id')

  const fetchList = async () => {
    setLoading(true)
    setListError(false)
    try {
      const r = await api.images()
      setImages(r.images || [])
      if (!id && r.images && r.images.length) {
        setParams({ id: String(r.images[0].id) }, { replace: true })
      }
    } catch {
      // 列表失败给出错误态与重试，而不是静默呈现「暂无镜像」误导用户
      setListError(true)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchList()
    // eslint-disable-next-line
  }, [])

  const fetchDetail = async (iid) => {
    if (!iid) return
    setLoadingDetail(true)
    setTlOpen(false)
    api
      .image(iid)
      .then(setDetail)
      .catch(() => setDetail(null))
      .finally(() => setLoadingDetail(false))
  }

  useEffect(() => {
    if (!id) return
    fetchDetail(id)
  }, [id])

  // 顶栏「立即扫描」/ 通知页「全部重新扫描」结束后自动刷新列表与当前详情
  useEffect(() => {
    const onScanDone = () => {
      fetchList()
      fetchDetail(id)
    }
    window.addEventListener(SCAN_DONE_EVENT, onScanDone)
    return () => window.removeEventListener(SCAN_DONE_EVENT, onScanDone)
  }, [id])

  // 选择器列表：搜索 + 状态过滤 + 有更新优先排序
  const filteredImages = useMemo(() => {
    let out = images
    if (filter) out = out.filter((i) => i.status === filter)
    if (query) out = out.filter((i) => i.reference.toLowerCase().includes(query.toLowerCase()))
    return [...out].sort(
      (a, b) => (STATUS_ORDER[a.status] ?? 9) - (STATUS_ORDER[b.status] ?? 9) || a.reference.localeCompare(b.reference),
    )
  }, [images, query, filter])

  // 移动端折叠态被 max-h 截断即视为溢出：仅此时才提供「展开全部」，
  // 避免 chip 被静默裁掉、用户既看不见也没有入口展开
  useLayoutEffect(() => {
    const el = selectorRef.current
    if (!el) return
    setSelectorOverflow(el.scrollHeight - el.clientHeight > 4)
  }, [filteredImages, selectorOpen])

  const pick = (i) => {
    setParams({ id: String(i.id) })
    setSelectorOpen(false) // 移动端选中后自动收起，避免遮挡详情
  }

  const tags = (detail && detail.tags) || []
  const versions = (detail && detail.versions) || []
  // 仅保留可解析为数字版本号的 tag：latest 等浮动 tag 不代表版本高低，
  // 若纳入排序会把「最新」误标到浮动 tag 上。排序后取前 N 个，第 1 个即当前最新版本。
  const numberedTags = useMemo(() => sortTagsVersionFirst(tags).filter((t) => parseVer(t)), [tags])
  const latestTags = useMemo(() => numberedTags.slice(0, LATEST_VERSIONS), [numberedTags])
  const shownVersions = tlOpen ? versions : versions.slice(0, TL_PREVIEW)
  const digestDiff = !!detail && !!detail.image && detail.image.local_digest !== detail.image.remote_digest

  // 列表占位态（加载/错误/空）：桌面左栏与移动端 chip 区共用，避免两处文案漂移
  const listPlaceholder = loading ? (
    <Spinner />
  ) : listError ? (
    <ErrorState message="镜像列表加载失败" onRetry={fetchList} />
  ) : images.length === 0 ? (
    <BentoCard className="py-6 text-center text-sm text-zinc-400 dark:text-zinc-500">
      尚未添加任何镜像，请先在「镜像列表」页添加监控。
    </BentoCard>
  ) : null

  return (
    <div className="flex h-full flex-col gap-6 overflow-hidden">
      <div className="shrink-0">
        <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">版本对比</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">对比本地与远端镜像摘要，查看最新版本号与版本时间线。</p>
      </div>

      {/* 工具栏：搜索 + 状态过滤，桌面左栏与移动端 chip 共用同一套筛选状态 */}
      {!loading && !listError && images.length > 0 && (
        <div className="shrink-0 flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <span className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">镜像列表</span>
            <span className="text-xs tabular-nums text-zinc-400 dark:text-zinc-500">
              {filteredImages.length}/{images.length}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex gap-3">
              {FILTERS.map((f) => (
                <button
                  key={f.key}
                  onClick={() => setFilter(f.key)}
                  className={`rounded-lg px-2.5 py-1 text-xs font-medium transition-colors ${
                    filter === f.key
                      ? 'bg-zinc-900 text-white dark:bg-white dark:text-zinc-900'
                      : 'bg-zinc-100 text-zinc-600 hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-300 dark:hover:bg-zinc-700'
                  }`}
                >
                  {f.label}
                </button>
              ))}
            </div>
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索镜像引用…"
              className="w-full rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-1.5 text-sm outline-none transition-all focus:border-bento-accent focus:ring-2 focus:ring-blue-500/20 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100 sm:w-52"
            />
          </div>
        </div>
      )}

      {/* 移动端镜像选择器（<lg）：横向 wrap chip；桌面改由左栏竖列承载 */}
      <div className="shrink-0 lg:hidden">
        {listPlaceholder || (
          <>
            <div
              ref={selectorRef}
              className={`flex flex-wrap gap-2.5 transition-all ${
                selectorOpen ? 'max-h-[45vh] overflow-y-auto' : 'max-h-[76px] overflow-hidden'
              }`}
              role="tablist"
              aria-label="选择镜像"
            >
              {filteredImages.map((i) => {
                const active = String(i.id) === id
                return (
                  <button
                    key={i.id}
                    onClick={() => pick(i)}
                    role="tab"
                    aria-selected={active}
                    title={i.reference}
                    className={`flex shrink-0 items-center gap-2.5 rounded-xl border px-3 py-1.5 text-sm font-medium transition-colors ${
                      active
                        ? 'border-blue-500 bg-blue-50/50 text-blue-700 ring-1 ring-blue-500/20 dark:bg-blue-900/20 dark:text-blue-300'
                        : 'border-zinc-200 bg-white text-zinc-600 hover:bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800'
                    }`}
                  >
                    <span className={`h-2 w-2 shrink-0 rounded-full ${STATUS_DOT[i.status] || STATUS_DOT.unknown}`} />
                    <span className="max-w-[12rem] truncate">{i.reference}</span>
                  </button>
                )
              })}
              {filteredImages.length === 0 && (
                <span className="py-2 text-sm text-zinc-400 dark:text-zinc-500">没有匹配的镜像</span>
              )}
            </div>
            {(selectorOverflow || selectorOpen) && (
              <button
                onClick={() => setSelectorOpen((v) => !v)}
                className="mt-2 text-xs font-medium text-bento-accent transition-colors hover:underline"
              >
                {selectorOpen ? '收起' : `展开全部 ${filteredImages.length} 个`}
              </button>
            )}
          </>
        )}
      </div>

      {/* 主体：桌面左栏列镜像（自身内滚，几十上百个也不挤占详情），移动端仅剩详情 */}
      <div className="min-h-0 flex-1 grid grid-rows-[minmax(0,1fr)] gap-6 overflow-hidden lg:grid-cols-[280px_1fr]">
        <div className="hidden min-w-0 flex-col lg:flex lg:min-h-0 lg:max-h-full">
          <div className="min-h-0 flex-1 space-y-0.5 overflow-y-auto pr-1">
            {listPlaceholder ||
              (filteredImages.length === 0 ? (
                <BentoCard className="text-center text-sm text-zinc-400 dark:text-zinc-500">没有匹配的镜像</BentoCard>
              ) : (
                filteredImages.map((i) => {
                  const active = String(i.id) === id
                  return (
                    <button
                      key={i.id}
                      onClick={() => pick(i)}
                      className={`flex w-full items-center gap-2.5 rounded-xl px-2.5 py-2 text-left transition-colors ${
                        active
                          ? 'bg-blue-50/70 ring-1 ring-blue-500/30 dark:bg-blue-900/25'
                          : 'hover:bg-zinc-100 dark:hover:bg-zinc-800/70'
                      }`}
                    >
                      <span className={`h-2 w-2 shrink-0 rounded-full ${STATUS_DOT[i.status] || STATUS_DOT.unknown}`} />
                      <span className="min-w-0 flex-1 truncate text-sm font-medium text-zinc-700 dark:text-zinc-200" title={i.reference}>
                        {i.reference}
                      </span>
                      <StatusBadge status={i.status} />
                    </button>
                  )
                })
              ))}
          </div>
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
              {/* 概要信息 */}
              <BentoCard>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <h3 className="break-all font-semibold text-zinc-900 dark:text-zinc-100">{detail.image.reference}</h3>
                  <StatusBadge status={detail.image.status} />
                </div>
                {/* 元数据 2×2 网格：避免四组信息挤成一行，窄屏也不易错位 */}
                <div className="mt-2 grid grid-cols-1 gap-x-5 gap-y-1 text-xs text-zinc-400 sm:grid-cols-2">
                  <span>最近检查：{fmtTime(detail.image.last_check)}</span>
                  <span>远端变更：{fmtTime(detail.image.last_update)}</span>
                  <span>来源：{detail.image.source === 'docker' ? 'Docker 守护进程' : detail.image.source === 'default' ? '演示监控' : '手动监控'}</span>
                  <span>检测模式：{modeLabel(detail.image.effective_mode)}{detail.image.mode && detail.image.mode !== 'auto' ? `（手动覆写）` : '（自动）'}</span>
                </div>
                {/* 分隔列（divide-x），不包子卡片（规则 G：禁止嵌套卡片） */}
                <div className="mt-4 flex flex-col gap-4 border-t border-zinc-100 pt-4 sm:flex-row sm:gap-0 sm:divide-x sm:divide-zinc-100 dark:border-zinc-800 dark:sm:divide-zinc-800">
                  <CompareCol title="本地版本" tag={detail.image.tag} digest={shortDigest(detail.image.local_digest)} accent="from-green-400 to-cyan-500" side="left" changed={digestDiff} />
                  <CompareCol title="远端最新" tag={detail.image.tag} digest={shortDigest(detail.image.remote_digest)} accent="from-orange-400 to-pink-500" side="right" changed={digestDiff} />
                </div>
              </BentoCard>

              <div className="bento-grid">
                {/* 版本号：仅保留最近 2 个数字版本，最高者高亮为「最新」 */}
                <BentoCard span="wide">
                  <div className="mb-1 flex flex-wrap items-center justify-between gap-3">
                    <h3 className="font-semibold text-zinc-900 dark:text-zinc-100">版本号</h3>
                    {numberedTags.length > 0 && (
                      <span className="text-xs tabular-nums text-zinc-400 dark:text-zinc-500">共 {numberedTags.length} 个数字版本</span>
                    )}
                  </div>
                  <p className="text-xs text-zinc-400 dark:text-zinc-500">
                    仅展示最近 {LATEST_VERSIONS} 个数字版本号，按版本新→旧排序（latest 等浮动 tag 不计入）
                  </p>
                  <div className="mt-4 flex flex-wrap items-center gap-3">
                    {latestTags.length ? (
                      latestTags.map((t, idx) =>
                        idx === 0 ? (
                          <span
                            key={t}
                            title={t}
                            className="flex items-center gap-3 rounded-xl bg-blue-600 px-3.5 py-2 font-mono text-sm font-semibold text-white shadow-sm"
                          >
                            <span className="rounded-md bg-white/25 px-1.5 py-0.5 font-sans text-[10px] font-semibold leading-none">
                              最新
                            </span>
                            <span className="max-w-[18rem] truncate">{t}</span>
                          </span>
                        ) : (
                          <span
                            key={t}
                            title={t}
                            className="flex items-center gap-3 rounded-xl bg-zinc-100 px-3 py-1.5 font-mono text-xs text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300"
                          >
                            <span className="max-w-[16rem] truncate">{t}</span>
                          </span>
                        ),
                      )
                    ) : (
                      <span className="text-xs text-zinc-400">
                        {tags.length ? '注册表中暂无可比较的数字版本号' : '注册表未公开标签列表'}
                      </span>
                    )}
                  </div>
                </BentoCard>

                {/* 版本快照统计 */}
                <BentoCard>
                  <h3 className="font-semibold text-zinc-900 dark:text-zinc-100">版本快照</h3>
                  <p className="mt-1 text-xs text-zinc-400">记录到的远端摘要变化总数</p>
                  <div className="mt-3 text-3xl font-bold tracking-tight text-zinc-900 tabular-nums dark:text-zinc-100">{versions.length}</div>
                  <p className="mt-1 text-xs text-zinc-400">
                    {versions.length > TL_PREVIEW ? `默认展示最近 ${TL_PREVIEW} 条` : '全部展示在下方时间线'}
                  </p>
                </BentoCard>
              </div>

              {/* 版本时间线：超长折叠 */}
              <BentoCard>
                <div className="mb-4 flex items-center justify-between">
                  <div>
                    <h3 className="font-semibold text-zinc-900 dark:text-zinc-100">版本时间线</h3>
                    <p className="mt-0.5 text-xs text-zinc-400">每次扫描记录到的远端摘要快照（共 {versions.length} 条）</p>
                  </div>
                  {versions.length > TL_PREVIEW && (
                    <button onClick={() => setTlOpen((v) => !v)} className="shrink-0 text-xs font-medium text-bento-accent hover:underline">
                      {tlOpen ? '收起' : `展开全部 ${versions.length} 条`}
                    </button>
                  )}
                </div>
                <div className="space-y-0">
                  {versions.length ? (
                    shownVersions.map((v, idx) => (
                      <div key={v.id} className="flex items-start gap-3">
                        <div className="flex flex-col items-center pt-2">
                          <span className={`h-3 w-3 shrink-0 rounded-full ring-2 ring-white dark:ring-zinc-900 ${idx === 0 ? 'bg-amber-500' : 'bg-zinc-300 dark:bg-zinc-600'}`} />
                          {idx < shownVersions.length - 1 && <span className="h-full w-px flex-1 bg-zinc-200 dark:bg-zinc-700" />}
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
function CompareCol({ title, tag, digest, accent, side, changed }) {
  return (
    <div className={`min-w-0 flex-1 ${side === 'right' ? 'sm:pl-6' : 'sm:pr-6'}`}>
      <div className="flex items-center gap-3">
        <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-gradient-to-br ${accent} text-white shadow-sm transition-transform duration-200 group-hover:scale-110`}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round"><path d="M20 6 9 17l-5-5" /></svg>
        </span>
        <span className="text-sm font-medium text-zinc-700 dark:text-zinc-200">{title}</span>
        <span className="ml-auto rounded-lg bg-zinc-100 px-2 py-0.5 font-mono text-xs font-medium text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300">{tag}</span>
      </div>
      {/* 摘要与本地不同（即「有更新」的来源）时高亮，避免两侧 tag 相同看不出差异 */}
      <div
        className={`mt-3 break-all font-mono text-sm leading-relaxed ${changed ? 'font-medium text-amber-600 dark:text-amber-400' : 'text-zinc-500 dark:text-zinc-400'}`}
        title={digest}
      >
        {digest}
        {changed && <span className="ml-2 font-sans text-xs font-normal text-zinc-400 dark:text-zinc-500">已变化</span>}
      </div>
    </div>
  )
}
