import { useEffect, useState } from 'react'
import { api } from '../api/client'
import Spinner from './Spinner'
import ConfirmDialog from './ConfirmDialog'

// 各渠道类型的展示名与配置字段（保存时组装成 config 对象，与后端 provider 对齐）。
const CHANNEL_KINDS = {
  dingtalk: {
    label: '钉钉',
    fields: [
      { key: 'webhook', label: 'Webhook', type: 'text', required: true, placeholder: 'https://oapi.dingtalk.com/robot/send?access_token=xxx' },
      { key: 'secret', label: '加签密钥（可选）', type: 'password', placeholder: '机器人安全设置中的加签密钥' },
    ],
  },
  wecom: {
    label: '企业微信',
    fields: [
      { key: 'webhook', label: 'Webhook', type: 'text', required: true, placeholder: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx' },
    ],
  },
  feishu: {
    label: '飞书',
    fields: [
      { key: 'webhook', label: 'Webhook', type: 'text', required: true, placeholder: 'https://open.feishu.cn/open-apis/bot/v2/hook/xxx' },
    ],
  },
  telegram: {
    label: 'Telegram',
    fields: [
      { key: 'bot_token', label: 'Bot Token', type: 'password', required: true, placeholder: '123456:ABC-DEF...' },
      { key: 'chat_id', label: 'Chat ID', type: 'text', required: true, placeholder: '如 1234567890 或 @频道' },
      { key: 'api_base', label: 'API 地址（可选）', type: 'text', placeholder: '留空使用官方 https://api.telegram.org，自建 Bot API 服务时填写' },
    ],
  },
  webhook: {
    label: '通用 Webhook',
    fields: [
      { key: 'url', label: 'URL', type: 'text', required: true, placeholder: 'https://your-endpoint.example/hook' },
      { key: 'headers', label: '请求头（可选，JSON）', type: 'text', placeholder: '{"X-Auth-Token":"xxx"}' },
      { key: 'format', label: '载荷格式', type: 'select', options: [
        { value: 'json', label: 'JSON（title/message/markdown/time）' },
        { value: 'text', label: '纯文本 POST' },
      ] },
    ],
  },
}

const KIND_ORDER = ['dingtalk', 'wecom', 'feishu', 'telegram', 'webhook']

// 配置对象 → 表单字段字符串值（headers 等对象字段序化回文本）
function configToFields(config, fields) {
  const out = {}
  for (const f of fields) {
    let v = config?.[f.key]
    if (v == null) v = ''
    if (typeof v === 'object') v = JSON.stringify(v)
    out[f.key] = String(v)
  }
  return out
}

// 表单字段字符串值 → 配置对象（headers 文本解析回对象，非法则抛错）
function fieldsToConfig(fields, fieldValues) {
  const cfg = {}
  for (const f of fields) {
    if (f.type === 'select') {
      cfg[f.key] = fieldValues[f.key] || ''
      continue
    }
    const raw = String(fieldValues[f.key] ?? '').trim()
    if (f.key === 'headers' && raw) {
      try {
        cfg.headers = JSON.parse(raw)
      } catch {
        throw new Error('请求头不是合法的 JSON 对象')
      }
    } else if (raw !== '') {
      cfg[f.key] = raw
    }
  }
  return cfg
}

export default function ChannelManager() {
  const [channels, setChannels] = useState(null) // null=加载中
  const [kinds, setKinds] = useState(KIND_ORDER)
  const [editing, setEditing] = useState(null) // null=列表 | {id?, ...} 表单草稿
  const [saving, setSaving] = useState(false)
  const [busy, setBusy] = useState(null) // 行操作中的 {type,id}
  const [delTarget, setDelTarget] = useState(null)
  const [msg, setMsg] = useState(null) // {type,text}
  const [testingId, setTestingId] = useState(null)

  const load = async () => {
    try {
      const res = await api.channels()
      setChannels(res.channels || [])
      setKinds(res.kinds || KIND_ORDER)
    } catch {
      setMsg({ type: 'error', text: '通知渠道加载失败' })
    }
  }

  useEffect(() => {
    load()
  }, [])

  const openCreate = () => {
    const kind = kinds[0] || 'webhook'
    setEditing({
      id: null,
      kind,
      name: (CHANNEL_KINDS[kind] || {}).label || kind,
      enabled: true,
      fieldValues: {},
    })
  }

  const openEdit = (ch) => {
    const kind = CHANNEL_KINDS[ch.kind]
    setEditing({
      id: ch.id,
      kind: ch.kind,
      name: ch.name || (kind ? kind.label : ch.kind),
      enabled: ch.enabled,
      fieldValues: configToFields(ch.config || {}, kind ? kind.fields : []),
      config: ch.config || {},
    })
  }

  const setKind = (kind) => {
    const meta = CHANNEL_KINDS[kind] || {}
    setEditing((e) => ({
      ...e,
      kind,
      name: (e.id ? e.name : (meta.label || kind)) || '',
      fieldValues: configToFields(e.config || {}, meta.fields || []),
    }))
  }

  const save = async () => {
    if (!editing) return
    const meta = CHANNEL_KINDS[editing.kind]
    // 必填项校验（前端早于后端报错，体验更好）
    for (const f of meta.fields) {
      if (f.required && !String(editing.fieldValues[f.key] ?? '').trim()) {
        setMsg({ type: 'error', text: `请填写${f.label}` })
        return
      }
    }
    let config
    try {
      config = fieldsToConfig(meta.fields, editing.fieldValues)
    } catch (e) {
      setMsg({ type: 'error', text: e.message })
      return
    }
    setSaving(true)
    setMsg(null)
    try {
      const body = { kind: editing.kind, name: editing.name.trim() || meta.label, enabled: editing.enabled, config }
      if (editing.id) {
        await api.updateChannel(editing.id, body)
      } else {
        await api.createChannel(body)
      }
      setEditing(null)
      setMsg({ type: 'ok', text: editing.id ? '渠道已更新' : '渠道已添加' })
      await load()
    } catch (e) {
      setMsg({ type: 'error', text: '保存失败：' + (e?.message || e) })
    } finally {
      setSaving(false)
    }
  }

  const toggleEnabled = async (ch) => {
    setBusy({ type: 'toggle', id: ch.id })
    setMsg(null)
    try {
      await api.updateChannel(ch.id, { name: ch.name, enabled: !ch.enabled })
      await load()
    } catch {
      setMsg({ type: 'error', text: '状态切换失败' })
    } finally {
      setBusy(null)
    }
  }

  const testChannel = async (ch) => {
    setTestingId(ch.id)
    setMsg(null)
    try {
      const res = await api.testChannel(ch.id)
      setMsg(res.ok
        ? { type: 'ok', text: `已向「${ch.name || ch.kind}」发送测试通知，请到对应端确认` }
        : { type: 'error', text: `测试失败：${res.error || '未知错误'}` })
    } catch (e) {
      setMsg({ type: 'error', text: '测试请求失败：' + (e?.message || e) })
    } finally {
      setTestingId(null)
    }
  }

  const confirmDelete = async () => {
    const target = delTarget
    setDelTarget(null)
    if (!target) return
    setBusy({ type: 'delete', id: target.id })
    setMsg(null)
    try {
      await api.deleteChannel(target.id)
      setMsg({ type: 'ok', text: '渠道已删除' })
      await load()
    } catch {
      setMsg({ type: 'error', text: '删除失败' })
    } finally {
      setBusy(null)
    }
  }

  const fieldValue = (key) => editing.fieldValues[key] ?? ''
  const updateField = (key, v) =>
    setEditing((e) => ({ ...e, fieldValues: { ...e.fieldValues, [key]: v } }))

  return (
    <div className="bento-card group p-4 md:p-6 bento-wide">
      {/* 卡片头：标题 + 添加按钮 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="font-medium text-zinc-900 dark:text-zinc-100">通知渠道</div>
          <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
            镜像更新时向所有启用的渠道推送（钉钉 / 企业微信 / 飞书 / Telegram / 通用 Webhook）
          </div>
        </div>
        {!editing && (
          <button
            type="button"
            onClick={openCreate}
            className="inline-flex items-center gap-1.5 rounded-xl bg-zinc-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-700 disabled:opacity-60 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200"
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" className="h-4 w-4" aria-hidden="true">
              <path d="M12 5v14M5 12h14" />
            </svg>
            添加渠道
          </button>
        )}
      </div>

      {msg && (
        <div
          className={`mt-4 rounded-xl px-4 py-2 text-sm ${
            msg.type === 'ok'
              ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
              : 'bg-rose-50 text-rose-700 dark:bg-rose-500/10 dark:text-rose-300'
          }`}
        >
          {msg.text}
        </div>
      )}

      {/* 表单态：新增 / 编辑 */}
      {editing ? (
        <div className="mt-5 space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <label className="text-xs font-medium text-zinc-500 dark:text-zinc-400">渠道类型</label>
              <select
                value={editing.kind}
                disabled={!!editing.id}
                onChange={(e) => setKind(e.target.value)}
                className="mt-1.5 w-full cursor-pointer rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-900 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 disabled:opacity-60 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
              >
                {kinds.map((k) => (
                  <option key={k} value={k}>{CHANNEL_KINDS[k]?.label || k}</option>
                ))}
              </select>
            </div>
            <div>
              <label className="text-xs font-medium text-zinc-500 dark:text-zinc-400">名称（可选）</label>
              <input
                type="text"
                value={editing.name}
                onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                placeholder="如 运维群"
                className="mt-1.5 w-full rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-900 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
              />
            </div>
          </div>

          {(CHANNEL_KINDS[editing.kind]?.fields || []).map((f) => (
            <div key={f.key}>
              <label className="text-xs font-medium text-zinc-500 dark:text-zinc-400">
                {f.label}{f.required && <span className="text-rose-500"> *</span>}
              </label>
              {f.type === 'select' ? (
                <select
                  value={fieldValue(f.key) || f.options[0].value}
                  onChange={(e) => updateField(f.key, e.target.value)}
                  className="mt-1.5 w-full cursor-pointer rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-900 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
                >
                  {f.options.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </select>
              ) : (
                <input
                  type={f.type}
                  value={fieldValue(f.key)}
                  onChange={(e) => updateField(f.key, e.target.value)}
                  placeholder={f.placeholder}
                  className="mt-1.5 w-full rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-900 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
                />
              )}
            </div>
          ))}

          <div className="flex flex-wrap items-center gap-3 pt-1">
            <label className="flex cursor-pointer items-center gap-2 text-sm text-zinc-700 dark:text-zinc-200">
              <input
                type="checkbox"
                checked={editing.enabled}
                onChange={(e) => setEditing({ ...editing, enabled: e.target.checked })}
                className="h-4 w-4 rounded border-zinc-300 accent-blue-600 dark:border-zinc-600"
              />
              启用该渠道
            </label>
            <div className="flex-1" />
            <button
              type="button"
              onClick={() => setEditing(null)}
              disabled={saving}
              className="rounded-xl border border-zinc-200 px-4 py-2 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-50 disabled:opacity-60 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
            >
              取消
            </button>
            <button
              type="button"
              onClick={save}
              disabled={saving}
              className="rounded-xl bg-zinc-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-700 disabled:opacity-60 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200"
            >
              {saving ? '保存中…' : '保存渠道'}
            </button>
          </div>
        </div>
      ) : channels === null ? (
        <div className="mt-6"><Spinner label="加载渠道…" /></div>
      ) : channels.length === 0 ? (
        <p className="mt-6 rounded-xl border border-dashed border-zinc-200 px-4 py-6 text-center text-sm text-zinc-400 dark:border-zinc-700">
          尚未配置通知渠道。添加一个渠道后，镜像更新会推送到这里。
        </p>
      ) : (
        <ul className="mt-5 space-y-3">
          {channels.map((ch) => {
            const meta = CHANNEL_KINDS[ch.kind] || { label: ch.kind }
            const inFlight = busy?.type === 'toggle' && busy.id === ch.id
            return (
              <li
                key={ch.id}
                className="flex flex-wrap items-center gap-3 rounded-xl border border-zinc-200 px-4 py-3 transition-colors dark:border-zinc-700"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="rounded-md bg-blue-50 px-2 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">
                      {meta.label}
                    </span>
                    <span className="truncate text-sm font-medium text-zinc-900 dark:text-zinc-100">
                      {ch.name || meta.label}
                    </span>
                  </div>
                  <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
                    {ch.enabled ? '启用中' : '已停用'}
                  </div>
                </div>

                <label className="flex cursor-pointer items-center gap-2 text-xs text-zinc-500 dark:text-zinc-400">
                  <button
                    type="button"
                    onClick={() => toggleEnabled(ch)}
                    disabled={inFlight}
                    aria-pressed={ch.enabled}
                    className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:opacity-60 ${
                      ch.enabled ? 'bg-blue-600' : 'bg-zinc-300 dark:bg-zinc-700'
                    }`}
                  >
                    <span
                      className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${
                        ch.enabled ? 'translate-x-[18px]' : 'translate-x-0.5'
                      }`}
                    />
                  </button>
                </label>

                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => testChannel(ch)}
                    disabled={testingId === ch.id}
                    className="rounded-lg border border-zinc-200 px-3 py-1.5 text-xs font-medium text-zinc-700 transition-colors hover:bg-zinc-50 disabled:opacity-60 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
                  >
                    {testingId === ch.id ? '测试中…' : '测试'}
                  </button>
                  <button
                    type="button"
                    onClick={() => openEdit(ch)}
                    className="rounded-lg border border-zinc-200 px-3 py-1.5 text-xs font-medium text-zinc-700 transition-colors hover:bg-zinc-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
                  >
                    编辑
                  </button>
                  <button
                    type="button"
                    onClick={() => setDelTarget(ch)}
                    className="rounded-lg border border-rose-200 px-3 py-1.5 text-xs font-medium text-rose-600 transition-colors hover:bg-rose-50 dark:border-rose-900/50 dark:text-rose-400 dark:hover:bg-rose-950/30"
                  >
                    删除
                  </button>
                </div>
              </li>
            )
          })}
        </ul>
      )}

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