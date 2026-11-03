import { useEffect, useState } from 'react'
import { api, fmtTime } from '../api/client'
import Spinner from './Spinner'
import ErrorState from './ErrorState'
import ConfirmDialog from './ConfirmDialog'
import Drawer from './Drawer'
import { useToast } from './Toast'

const INPUT_CLS =
  'w-full rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2.5 text-sm text-zinc-900 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 disabled:cursor-not-allowed disabled:opacity-60 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100'

const BTN_BASE =
  'inline-flex items-center justify-center gap-1.5 rounded-xl text-sm font-medium transition-colors active:scale-95 disabled:cursor-not-allowed disabled:opacity-60'
const BTN_PRIMARY = `${BTN_BASE} min-h-[44px] px-5 py-2.5 bg-zinc-900 text-white hover:bg-zinc-700 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200`
const BTN_SECONDARY = `${BTN_BASE} h-11 px-4 sm:h-9 sm:px-3.5 border border-zinc-200 text-zinc-700 hover:bg-zinc-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800`
const BTN_DANGER = `${BTN_BASE} h-11 px-4 sm:h-9 sm:px-3.5 border border-rose-200 text-rose-600 hover:bg-rose-50 dark:border-rose-900/50 dark:text-rose-400 dark:hover:bg-rose-950/30`

// 供 AI 助手填入的 MCP 端点（同源部署即当前站点 /mcp）
const mcpEndpoint = () => `${window.location.origin}/mcp`

// 创建成功页：明文仅此一次展示，复制按钮 + 醒目提醒
function CreatedKeyView({ name, apiKey, onDone }) {
  const toast = useToast()
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(apiKey)
      setCopied(true)
      toast('success', '密钥已复制到剪贴板')
    } catch {
      toast('error', '复制失败，请手动选择复制')
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start gap-3 rounded-xl bg-amber-50 px-3.5 py-3 dark:bg-amber-500/10">
        <svg className="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
          <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
        </svg>
        <div className="text-xs leading-relaxed text-amber-800 dark:text-amber-200">
          这是密钥<strong>唯一一次</strong>完整展示的机会，关闭后将无法再查看。请立即复制并保存到 AI 助手的配置中。
        </div>
      </div>

      <div>
        <div className="text-xs font-medium text-zinc-500 dark:text-zinc-400">密钥名称</div>
        <div className="mt-1.5 text-sm text-zinc-900 dark:text-zinc-100">{name}</div>
      </div>

      <div>
        <div className="text-xs font-medium text-zinc-500 dark:text-zinc-400">API Key</div>
        <div className="mt-1.5 flex items-stretch gap-2">
          <code className="min-w-0 flex-1 truncate rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2.5 font-mono text-xs text-zinc-800 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-200" title={apiKey}>
            {apiKey}
          </code>
          <button type="button" onClick={copy} className={`${BTN_SECONDARY} shrink-0`}>
            {copied ? '已复制' : '复制'}
          </button>
        </div>
      </div>

      <div className="rounded-xl bg-zinc-50 px-3.5 py-3 text-xs leading-relaxed text-zinc-500 dark:bg-zinc-800/60 dark:text-zinc-400">
        <div className="font-medium text-zinc-700 dark:text-zinc-200">AI 助手配置示例（MCP 端点）</div>
        <code className="mt-1.5 block break-all font-mono text-[11px]">{mcpEndpoint()}</code>
        <div className="mt-2">请求头携带 <code className="font-mono">Authorization: Bearer &lt;API Key&gt;</code>（或 <code className="font-mono">X-API-Key</code>）即可访问。</div>
      </div>

      <button type="button" onClick={onDone} className={`${BTN_PRIMARY} w-full`}>
        我已保存，完成
      </button>
    </div>
  )
}

export default function MCPKeyManager() {
  const toast = useToast()
  const [keys, setKeys] = useState(null) // null=加载中
  const [loadError, setLoadError] = useState(false)
  const [drawer, setDrawer] = useState(null) // null | {mode:'create'} | {mode:'created', name, apiKey}
  const [name, setName] = useState('')
  const [creating, setCreating] = useState(false)
  const [rowBusy, setRowBusy] = useState(null)
  const [delTarget, setDelTarget] = useState(null)

  const load = async () => {
    try {
      const res = await api.mcpKeys()
      setKeys(res.keys || [])
      setLoadError(false)
    } catch {
      setLoadError(true)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const create = async () => {
    if (!name.trim()) {
      toast('error', '请先填写密钥名称')
      return
    }
    setCreating(true)
    try {
      const res = await api.createMCPKey(name.trim())
      setDrawer({ mode: 'created', name: name.trim(), apiKey: res.key })
      setName('')
      await load()
    } catch (e) {
      toast('error', '生成失败：' + (e?.message || e))
    } finally {
      setCreating(false)
    }
  }

  const confirmDelete = async () => {
    const target = delTarget
    setDelTarget(null)
    if (!target) return
    setRowBusy(target.id)
    try {
      await api.deleteMCPKey(target.id)
      toast('success', '密钥已吊销，使用它的客户端将立即失效')
      await load()
    } catch {
      toast('error', '吊销失败，请重试')
    } finally {
      setRowBusy(null)
    }
  }

  return (
    <div className="bento-card group p-4 md:p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="font-medium text-zinc-900 dark:text-zinc-100">API Key</div>
          <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
            密钥只在生成时完整显示一次；列表仅保留前缀用于辨认。吊销后立即生效。
          </div>
        </div>
        <button type="button" onClick={() => setDrawer({ mode: 'create' })} className={BTN_PRIMARY}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" className="h-4 w-4" aria-hidden="true">
            <path d="M12 5v14M5 12h14" />
          </svg>
          生成密钥
        </button>
      </div>

      {keys === null && !loadError ? (
        <Spinner label="加载密钥…" />
      ) : loadError ? (
        <div className="mt-4">
          <ErrorState message="密钥列表加载失败" onRetry={load} />
        </div>
      ) : keys.length === 0 ? (
        <div className="mt-5 rounded-2xl border border-dashed border-zinc-200 px-5 py-8 text-center dark:border-zinc-700">
          <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-zinc-100 text-zinc-400 dark:bg-zinc-800 dark:text-zinc-500">
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <circle cx="7.5" cy="15.5" r="4.5" />
              <path d="m21 2-9.6 9.6" />
              <path d="m15.5 7.5 3 3L22 7l-3-3" />
            </svg>
          </div>
          <p className="mt-3 text-sm font-medium text-zinc-600 dark:text-zinc-300">尚未生成 MCP API Key</p>
          <p className="mx-auto mt-1 max-w-sm text-xs text-zinc-400 dark:text-zinc-500">
            生成密钥后，Claude Desktop / CodeBuddy 等 AI 助手即可通过 MCP 查询镜像更新并执行忽略、已读等标记操作。
          </p>
          <button type="button" onClick={() => setDrawer({ mode: 'create' })} className={`${BTN_PRIMARY} mt-4`}>
            生成第一个密钥
          </button>
        </div>
      ) : (
        <ul className="mt-4 divide-y divide-zinc-100 dark:divide-zinc-800">
          {keys.map((k) => (
            <li key={k.id} className="py-4 first:pt-0 last:pb-0">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2.5">
                    <span className="truncate text-sm font-medium text-zinc-900 dark:text-zinc-100">{k.name}</span>
                    <code className="rounded-lg bg-zinc-100 px-2 py-0.5 font-mono text-xs text-zinc-500 dark:bg-zinc-800 dark:text-zinc-400" title="密钥前缀（用于辨认，非完整密钥）">
                      {k.key_prefix}
                    </code>
                  </div>
                  <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
                    创建于 {fmtTime(k.created_at)}
                    {k.last_used_at ? ` · 最近使用 ${fmtTime(k.last_used_at)}` : ' · 从未使用'}
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => setDelTarget(k)}
                  disabled={rowBusy === k.id}
                  className={`${BTN_DANGER} ml-auto shrink-0`}
                >
                  {rowBusy === k.id ? '吊销中…' : '吊销'}
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}

      <Drawer
        open={!!drawer}
        title={drawer?.mode === 'created' ? '密钥已生成' : '生成 MCP API Key'}
        description={
          drawer?.mode === 'created'
            ? '请立即保存密钥，关闭本窗口后不再显示。'
            : '为密钥起一个便于辨认的名字（如「我的笔记本」「CI 机器人」）。'
        }
        onClose={() => !creating && setDrawer(null)}
        footer={
          drawer?.mode === 'create' ? (
            <div className="flex items-center gap-3">
              <button
                type="button"
                onClick={() => setDrawer(null)}
                disabled={creating}
                className={`${BTN_BASE} h-11 flex-1 border border-zinc-200 text-zinc-700 hover:bg-zinc-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800`}
              >
                取消
              </button>
              <button
                type="button"
                onClick={create}
                disabled={creating}
                className={`${BTN_BASE} h-11 flex-[2] bg-zinc-900 text-white hover:bg-zinc-700 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200`}
              >
                {creating ? '生成中…' : '生成密钥'}
              </button>
            </div>
          ) : null
        }
      >
        {drawer?.mode === 'create' ? (
          <div>
            <label className="text-xs font-medium text-zinc-500 dark:text-zinc-400" htmlFor="mcp-key-name">
              密钥名称 <span className="ml-0.5 text-rose-500">*</span>
            </label>
            <input
              id="mcp-key-name"
              type="text"
              autoFocus
              value={name}
              maxLength={50}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && create()}
              placeholder="如 我的笔记本"
              aria-label="密钥名称"
              className={`mt-1.5 ${INPUT_CLS}`}
            />
          </div>
        ) : drawer?.mode === 'created' ? (
          <CreatedKeyView name={drawer.name} apiKey={drawer.apiKey} onDone={() => setDrawer(null)} />
        ) : null}
      </Drawer>

      <ConfirmDialog
        open={!!delTarget}
        title="吊销 API Key"
        description={`确认吊销「${delTarget?.name || ''}」吗？使用该密钥的 AI 助手将立即失去访问权限，此操作不可撤销。`}
        confirmText="吊销"
        cancelText="取消"
        danger
        onConfirm={confirmDelete}
        onCancel={() => setDelTarget(null)}
      />
    </div>
  )
}
