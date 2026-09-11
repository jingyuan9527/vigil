import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import Spinner from './Spinner'
import ErrorState from './ErrorState'
import ConfirmDialog from './ConfirmDialog'
import Drawer from './Drawer'
import Switch from './Switch'
import { useToast } from './Toast'

// 各渠道类型的展示名与配置字段（保存时组装成 config 对象，与后端 provider 对齐）。
// format 仅用于前端校验（url / 其余不校验）；hint 是字段级说明，errors 提示优先于 hint。
const CHANNEL_KINDS = {
  dingtalk: {
    label: '钉钉',
    fields: [
      { key: 'webhook', label: 'Webhook 地址', type: 'text', required: true, format: 'url', placeholder: 'https://oapi.dingtalk.com/robot/send?access_token=xxx' },
      { key: 'secret', label: '加签密钥', type: 'password', placeholder: '机器人开启「加签」时填写', hint: '可选；仅在本机请求时参与签名，不会展示在列表中' },
    ],
  },
  wecom: {
    label: '企业微信',
    fields: [
      { key: 'webhook', label: 'Webhook 地址', type: 'text', required: true, format: 'url', placeholder: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx' },
    ],
  },
  feishu: {
    label: '飞书',
    fields: [
      { key: 'webhook', label: 'Webhook 地址', type: 'text', required: true, format: 'url', placeholder: 'https://open.feishu.cn/open-apis/bot/v2/hook/xxx' },
    ],
  },
  telegram: {
    label: 'Telegram',
    fields: [
      { key: 'bot_token', label: 'Bot Token', type: 'password', required: true, placeholder: '123456:ABC-DEF…' },
      { key: 'chat_id', label: 'Chat ID', type: 'text', required: true, placeholder: '如 1234567890 或 @频道' },
      { key: 'api_base', label: 'API 地址', type: 'text', format: 'url', placeholder: '留空使用官方 api.telegram.org', hint: '可选；自建 Bot API 服务时填写' },
    ],
  },
  webhook: {
    label: '通用 Webhook',
    fields: [
      { key: 'url', label: 'URL', type: 'text', required: true, format: 'url', placeholder: 'https://your-endpoint.example/hook' },
      {
        key: 'format',
        label: '载荷格式',
        type: 'select',
        options: [
          { value: 'json', label: 'JSON（title/message/markdown/time）' },
          { value: 'text', label: '纯文本 POST' },
        ],
      },
      { key: 'headers', label: '请求头', type: 'text', placeholder: '{"X-Auth-Token":"xxx"}', hint: '可选；JSON 对象，保存前会校验格式' },
    ],
  },
}

const KIND_ORDER = ['dingtalk', 'wecom', 'feishu', 'telegram', 'webhook']

const INPUT_CLS =
  'w-full rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2.5 text-sm text-zinc-900 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 disabled:cursor-not-allowed disabled:opacity-60 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100'
const INPUT_ERR_CLS = 'border-rose-400 focus:border-rose-500 focus:ring-rose-500/20'

const BTN_BASE =
  'inline-flex items-center justify-center gap-1.5 rounded-xl text-sm font-medium transition-colors active:scale-95 disabled:cursor-not-allowed disabled:opacity-60'
// 移动端 44px 触控高度，桌面收紧到 36px（设计规范 §4.0）
const BTN_SECONDARY = `${BTN_BASE} h-11 px-4 sm:h-9 sm:px-3.5 border border-zinc-200 text-zinc-700 hover:bg-zinc-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800`
const BTN_PRIMARY = `${BTN_BASE} min-h-[44px] px-5 py-2.5 bg-zinc-900 text-white hover:bg-zinc-700 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200`
const BTN_DANGER = `${BTN_BASE} h-11 px-4 sm:h-9 sm:px-3.5 border border-rose-200 text-rose-600 hover:bg-rose-50 dark:border-rose-900/50 dark:text-rose-400 dark:hover:bg-rose-950/30`

// 配置对象 → 表单字段字符串值（headers 等对象字段序化回文本）
function configToFields(config, meta) {
  const out = {}
  for (const f of meta.fields || []) {
    let v = config?.[f.key]
    if (v == null) v = ''
    if (typeof v === 'object') v = JSON.stringify(v)
    out[f.key] = String(v)
  }
  return out
}

// 表单字段字符串值 → 配置对象（非法项已被 validateField 拦截，这里只做组装）
function fieldsToConfig(meta, fieldValues) {
  const cfg = {}
  for (const f of meta.fields || []) {
    if (f.type === 'select') {
      cfg[f.key] = fieldValues[f.key] || f.options?.[0]?.value || ''
      continue
    }
    const raw = String(fieldValues[f.key] ?? '').trim()
    if (raw === '') continue
    if (f.key === 'headers') {
      try {
        cfg.headers = JSON.parse(raw)
      } catch {
        /* 前面已校验，理论不可达 */
      }
    } else {
      cfg[f.key] = raw
    }
  }
  return cfg
}

// 单字段校验：返回错误文案（空串表示通过）。required 之外的格式错误也在此拦截，
// 使错误能就地展示在字段下方，而不是提交后统一丢到顶部。
function validateField(f, value) {
  const v = String(value ?? '').trim()
  if (f.required && !v) return `请填写${f.label}`
  if (!v) return ''
  if (f.format === 'url') {
    try {
      const u = new URL(v)
      if (u.protocol !== 'http:' && u.protocol !== 'https:') return '仅支持 http / https 地址'
    } catch {
      return '请输入完整地址（含 http(s)://）'
    }
  }
  if (f.key === 'headers') {
    try {
      const o = JSON.parse(v)
      if (o === null || typeof o !== 'object' || Array.isArray(o)) return '请求头需为 JSON 对象'
    } catch {
      return '请求头不是合法 JSON'
    }
  }
  return ''
}

// 列表行的配置摘要：只显示主机名与关键标识，绝不回显 token/secret，
// 同时让同类渠道可区分（而非只靠名称）。
function channelPreview(ch) {
  const c = ch.config || {}
  const host = (u) => {
    try {
      return new URL(u).host
    } catch {
      return u ? String(u) : ''
    }
  }
  switch (ch.kind) {
    case 'telegram':
      return [c.api_base ? host(c.api_base) : 'api.telegram.org', c.chat_id && `chat ${c.chat_id}`].filter(Boolean).join(' · ')
    case 'webhook':
      return [host(c.url), c.format === 'text' ? '纯文本' : 'JSON'].filter(Boolean).join(' · ')
    default:
      return host(c.webhook) || host(c.url)
  }
}

function Field({ label, required, hint, error, children }) {
  return (
    <div>
      <div className="text-xs font-medium text-zinc-500 dark:text-zinc-400">
        {label}
        {required && <span className="ml-0.5 text-rose-500">*</span>}
      </div>
      <div className="mt-1.5">{children}</div>
      {error ? (
        <p className="mt-1.5 flex items-center gap-1.5 text-xs text-rose-500">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true">
            <circle cx="12" cy="12" r="9" />
            <path d="M12 8v4" />
            <path d="M12 16h.01" />
          </svg>
          {error}
        </p>
      ) : (
        hint && <p className="mt-1.5 text-xs text-zinc-400 dark:text-zinc-500">{hint}</p>
      )}
    </div>
  )
}

export default function ChannelManager() {
  const toast = useToast()
  const [channels, setChannels] = useState(null) // null=加载中
  const [kinds, setKinds] = useState(KIND_ORDER)
  const [loadError, setLoadError] = useState(false)
  const [draft, setDraft] = useState(null) // null=列表 | 表单草稿
  const [errors, setErrors] = useState({}) // 字段级错误
  const [saving, setSaving] = useState(false)
  const [rowBusy, setRowBusy] = useState(null) // {id, action:'toggle'|'test'}
  const [delTarget, setDelTarget] = useState(null)
  const fieldRefs = useRef({}) // 校验失败时聚焦首个出错字段

  const load = async () => {
    try {
      const res = await api.channels()
      setChannels(res.channels || [])
      setKinds(res.kinds || KIND_ORDER)
      setLoadError(false)
    } catch {
      setLoadError(true)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const openCreate = () => {
    const kind = kinds[0] || 'webhook'
    const meta = CHANNEL_KINDS[kind] || { label: kind, fields: [] }
    setErrors({})
    setDraft({ id: null, kind, name: meta.label || kind, enabled: true, fieldValues: configToFields({}, meta) })
  }

  const openEdit = (ch) => {
    const meta = CHANNEL_KINDS[ch.kind] || { label: ch.kind, fields: [] }
    setErrors({})
    setDraft({
      id: ch.id,
      kind: ch.kind,
      name: ch.name || meta.label,
      enabled: !!ch.enabled,
      fieldValues: configToFields(ch.config || {}, meta),
    })
  }

  // 切换类型时重置字段：不同渠道的 config 结构互不通用，保留旧值会产生误导
  const setKind = (kind) => {
    const meta = CHANNEL_KINDS[kind] || { label: kind, fields: [] }
    setErrors({})
    setDraft((d) => ({
      ...d,
      kind,
      name: d.id ? d.name : meta.label || kind,
      fieldValues: configToFields({}, meta),
    }))
  }

  const updateField = (key, value) => {
    setDraft((d) => ({ ...d, fieldValues: { ...d.fieldValues, [key]: value } }))
    // 已出错的字段在用户修正时即时消除错误（未出错的字段不提前打扰）
    setErrors((prev) => {
      if (!prev[key]) return prev
      const f = (CHANNEL_KINDS[draft.kind]?.fields || []).find((x) => x.key === key)
      const msg = f ? validateField(f, value) : ''
      const next = { ...prev }
      if (msg) next[key] = msg
      else delete next[key]
      return next
    })
  }

  const validateAll = () => {
    const meta = CHANNEL_KINDS[draft.kind] || { fields: [] }
    const next = {}
    for (const f of meta.fields) {
      const msg = validateField(f, draft.fieldValues[f.key])
      if (msg) next[f.key] = msg
    }
    setErrors(next)
    const firstKey = meta.fields.find((f) => next[f.key])?.key
    if (firstKey) {
      fieldRefs.current[firstKey]?.focus()
      return false
    }
    return true
  }

  const save = async () => {
    if (!draft) return
    if (!validateAll()) return
    const meta = CHANNEL_KINDS[draft.kind] || { fields: [] }
    setSaving(true)
    try {
      const body = {
        kind: draft.kind,
        name: (draft.name || '').trim() || meta.label,
        enabled: draft.enabled,
        config: fieldsToConfig(meta, draft.fieldValues),
      }
      if (draft.id) await api.updateChannel(draft.id, body)
      else await api.createChannel(body)
      toast('success', draft.id ? '渠道已更新' : '渠道已添加')
      setDraft(null)
      await load()
    } catch (e) {
      toast('error', '保存失败：' + (e?.message || e))
    } finally {
      setSaving(false)
    }
  }

  const toggleEnabled = async (ch) => {
    setRowBusy({ id: ch.id, action: 'toggle' })
    try {
      await api.updateChannel(ch.id, { name: ch.name, enabled: !ch.enabled })
      await load()
      toast('success', ch.enabled ? '已停用该渠道' : '已启用该渠道')
    } catch {
      toast('error', '状态切换失败，请重试')
    } finally {
      setRowBusy(null)
    }
  }

  const testChannel = async (ch) => {
    setRowBusy({ id: ch.id, action: 'test' })
    try {
      const res = await api.testChannel(ch.id)
      if (res.ok) toast('success', `已向「${ch.name || CHANNEL_KINDS[ch.kind]?.label || ch.kind}」发送测试通知`)
      else toast('error', `测试失败：${res.error || '未知错误'}`)
    } catch (e) {
      toast('error', '测试请求失败：' + (e?.message || e))
    } finally {
      setRowBusy(null)
    }
  }

  const confirmDelete = async () => {
    const target = delTarget
    setDelTarget(null)
    if (!target) return
    setRowBusy({ id: target.id, action: 'delete' })
    try {
      await api.deleteChannel(target.id)
      toast('success', '渠道已删除')
      await load()
    } catch {
      toast('error', '删除失败，请重试')
    } finally {
      setRowBusy(null)
    }
  }

  const meta = draft ? CHANNEL_KINDS[draft.kind] || { fields: [] } : null

  return (
    <div className="bento-card group p-4 md:p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="font-medium text-zinc-900 dark:text-zinc-100">通知渠道</div>
          <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
            镜像更新时向所有启用的渠道并行推送；此处改动保存后立即生效，无需底部「保存设置」。
          </div>
        </div>
        <button type="button" onClick={openCreate} className={BTN_PRIMARY}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" className="h-4 w-4" aria-hidden="true">
            <path d="M12 5v14M5 12h14" />
          </svg>
          添加渠道
        </button>
      </div>

      {channels === null && !loadError ? (
        <Spinner label="加载渠道…" />
      ) : loadError ? (
        <div className="mt-4">
          <ErrorState message="通知渠道加载失败" onRetry={load} />
        </div>
      ) : channels.length === 0 ? (
        <div className="mt-5 rounded-2xl border border-dashed border-zinc-200 px-5 py-8 text-center dark:border-zinc-700">
          <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-zinc-100 text-zinc-400 dark:bg-zinc-800 dark:text-zinc-500">
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
              <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
            </svg>
          </div>
          <p className="mt-3 text-sm font-medium text-zinc-600 dark:text-zinc-300">尚未配置通知渠道</p>
          <p className="mx-auto mt-1 max-w-xs text-xs text-zinc-400 dark:text-zinc-500">
            添加渠道后，镜像发现更新会并行推送到所有已启用的渠道。
          </p>
          <button type="button" onClick={openCreate} className={`${BTN_PRIMARY} mt-4`}>
            添加第一个渠道
          </button>
        </div>
      ) : (
        <ul className="mt-4 divide-y divide-zinc-100 dark:divide-zinc-800">
          {channels.map((ch) => {
            const kindMeta = CHANNEL_KINDS[ch.kind] || { label: ch.kind }
            const label = ch.name || kindMeta.label
            const busy = rowBusy?.id === ch.id ? rowBusy.action : null
            const preview = channelPreview(ch)
            return (
              <li key={ch.id} className="py-4 first:pt-0 last:pb-0">
                {/* 第一行：标识 + 状态开关；开关带文案，避免「裸开关」猜含义 */}
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2.5">
                      <span className="rounded-lg bg-blue-50 px-2 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">
                        {kindMeta.label}
                      </span>
                      <span className="truncate text-sm font-medium text-zinc-900 dark:text-zinc-100">{label}</span>
                    </div>
                    <div className="mt-1 truncate font-mono text-xs text-zinc-400 dark:text-zinc-500" title={preview}>
                      {preview || '—'}
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2.5 pt-0.5">
                    <span className={`text-xs font-medium ${ch.enabled ? 'text-emerald-600 dark:text-emerald-400' : 'text-zinc-400 dark:text-zinc-500'}`}>
                      {ch.enabled ? '已启用' : '已停用'}
                    </span>
                    <Switch size="sm" checked={!!ch.enabled} disabled={busy === 'toggle'} onChange={() => toggleEnabled(ch)} label={`${ch.enabled ? '停用' : '启用'}${label}`} />
                  </div>
                </div>

                {/* 第二行：操作区。编辑为主行动，删除危险操作右移独立，降低误触 */}
                <div className="mt-3 flex flex-wrap items-center gap-2.5">
                  <button type="button" onClick={() => openEdit(ch)} className={BTN_SECONDARY}>
                    编辑
                  </button>
                  <button type="button" onClick={() => testChannel(ch)} disabled={busy === 'test'} className={BTN_SECONDARY}>
                    {busy === 'test' ? '测试中…' : '测试连接'}
                  </button>
                  <button type="button" onClick={() => setDelTarget(ch)} disabled={busy === 'delete'} className={`${BTN_DANGER} ml-auto`}>
                    删除
                  </button>
                </div>
              </li>
            )
          })}
        </ul>
      )}

      <Drawer
        open={!!draft}
        title={draft?.id ? '编辑通知渠道' : '添加通知渠道'}
        description={
          draft?.id
            ? '修改后保存即生效；可点击列表中的「测试连接」校验配置是否正确。'
            : '选择渠道类型并填写配置，保存后会出现在列表中，可发送测试通知验证。'
        }
        onClose={() => !saving && setDraft(null)}
        footer={
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => setDraft(null)}
              disabled={saving}
              className={`${BTN_BASE} h-11 flex-1 border border-zinc-200 text-zinc-700 hover:bg-zinc-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800`}
            >
              取消
            </button>
            <button
              type="button"
              onClick={save}
              disabled={saving}
              className={`${BTN_BASE} h-11 flex-[2] bg-zinc-900 text-white hover:bg-zinc-700 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200`}
            >
              {saving ? '保存中…' : draft?.id ? '保存修改' : '保存渠道'}
            </button>
          </div>
        }
      >
        {draft && meta && (
          <div className="space-y-4">
            <Field label="渠道类型" hint={draft.id ? '渠道创建后不可更改类型' : undefined}>
              <select
                value={draft.kind}
                disabled={!!draft.id}
                onChange={(e) => setKind(e.target.value)}
                aria-label="渠道类型"
                className={`${INPUT_CLS} cursor-pointer`}
              >
                {kinds.map((k) => (
                  <option key={k} value={k}>
                    {CHANNEL_KINDS[k]?.label || k}
                  </option>
                ))}
              </select>
            </Field>

            <Field label="名称" hint="用于区分同类型的多个渠道，留空则用类型名">
              <input
                type="text"
                value={draft.name}
                onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
                placeholder="如 运维群"
                aria-label="名称"
                className={INPUT_CLS}
              />
            </Field>

            {meta.fields.map((f) => (
              <Field key={f.key} label={f.label} required={f.required} hint={f.hint} error={errors[f.key]}>
                {f.type === 'select' ? (
                  <select
                    value={draft.fieldValues[f.key] || f.options?.[0]?.value || ''}
                    onChange={(e) => updateField(f.key, e.target.value)}
                    aria-label={f.label}
                    className={`${INPUT_CLS} cursor-pointer`}
                  >
                    {(f.options || []).map((o) => (
                      <option key={o.value} value={o.value}>
                        {o.label}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    ref={(el) => {
                      fieldRefs.current[f.key] = el
                    }}
                    type={f.type}
                    value={draft.fieldValues[f.key] ?? ''}
                    onChange={(e) => updateField(f.key, e.target.value)}
                    placeholder={f.placeholder}
                    aria-label={f.label}
                    aria-invalid={!!errors[f.key]}
                    className={`${INPUT_CLS} ${errors[f.key] ? INPUT_ERR_CLS : ''}`}
                  />
                )}
              </Field>
            ))}

            <div className="flex items-center justify-between gap-3 rounded-xl bg-zinc-50 px-3.5 py-3 dark:bg-zinc-800/60">
              <div className="min-w-0">
                <div className="text-sm font-medium text-zinc-800 dark:text-zinc-100">启用该渠道</div>
                <div className="mt-0.5 text-xs text-zinc-400 dark:text-zinc-500">停用后镜像更新不再向该渠道推送</div>
              </div>
              <Switch checked={draft.enabled} onChange={(v) => setDraft((d) => ({ ...d, enabled: v }))} label="启用该渠道" />
            </div>
          </div>
        )}
      </Drawer>

      <ConfirmDialog
        open={!!delTarget}
        title="删除通知渠道"
        description={`确认删除「${delTarget?.name || delTarget?.kind || ''}」渠道吗？删除后不再向该渠道推送通知，此操作不可撤销。`}
        confirmText="删除"
        cancelText="取消"
        danger
        onConfirm={confirmDelete}
        onCancel={() => setDelTarget(null)}
      />
    </div>
  )
}
