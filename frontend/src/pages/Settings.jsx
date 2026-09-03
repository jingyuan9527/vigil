import { useEffect, useState } from 'react'
import { api } from '../api/client'
import Spinner from '../components/Spinner'

function Toggle({ checked, onChange }) {
  return (
    <button
      type="button"
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors ${
        checked ? 'bg-blue-600' : 'bg-zinc-300 dark:bg-zinc-700'
      }`}
      role="switch"
      aria-checked={checked}
    >
      <span
        className={`inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform ${
          checked ? 'translate-x-5' : 'translate-x-0.5'
        }`}
      />
    </button>
  )
}

export default function Settings() {
  const [form, setForm] = useState(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState(null)

  useEffect(() => {
    ;(async () => {
      try {
        const s = await api.settings()
        setForm(s)
      } catch {
        setMsg({ type: 'error', text: '加载设置失败' })
      } finally {
        setLoading(false)
      }
    })()
  }, [])

  const update = (patch) => setForm((f) => ({ ...f, ...patch }))

  const onSave = async (e) => {
    e.preventDefault()
    setSaving(true)
    setMsg(null)
    try {
      const saved = await api.saveSettings(form)
      setForm(saved)
      setMsg({ type: 'ok', text: '设置已保存，扫描间隔等将立即生效' })
    } catch {
      setMsg({ type: 'error', text: '保存失败，请重试' })
    } finally {
      setSaving(false)
    }
  }

  if (loading || !form) return <Spinner label="加载设置…" />

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">设置</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">在页面上调整运行参数，保存后立即生效并持久化（重启后仍保留）。</p>
      </div>

      <form onSubmit={onSave} className="space-y-5">
        {/* 扫描间隔 */}
        <section className="rounded-2xl border border-zinc-100 bg-white p-5 dark:border-zinc-800 dark:bg-zinc-900">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <div className="font-medium text-zinc-900 dark:text-zinc-100">扫描间隔（秒）</div>
              <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
                周期扫描的间隔；设为 0 可关闭自动扫描（仍可用「立即扫描」手动触发）。最小 {30} 秒。
              </div>
            </div>
            <input
              type="number"
              min="0"
              step="1"
              value={form.scan_interval}
              onChange={(e) => update({ scan_interval: parseInt(e.target.value, 10) || 0 })}
              className="w-32 rounded-xl border border-zinc-200 bg-white px-3 py-2 text-right text-sm text-zinc-900 outline-none focus:border-blue-500 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
            />
          </div>
        </section>

        {/* 关闭内置演示监控列表 */}
        <section className="rounded-2xl border border-zinc-100 bg-white p-5 dark:border-zinc-800 dark:bg-zinc-900">
          <div className="flex items-center justify-between gap-3">
            <div>
              <div className="font-medium text-zinc-900 dark:text-zinc-100">关闭内置演示监控列表</div>
              <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
                开启后将不再自动监控 nginx / redis / postgres 等内置演示镜像（手动添加的监控不受影响）。
              </div>
            </div>
            <Toggle checked={form.disable_default_watch} onChange={(v) => update({ disable_default_watch: v })} />
          </div>
        </section>

        {/* 允许 http 注册表 */}
        <section className="rounded-2xl border border-zinc-100 bg-white p-5 dark:border-zinc-800 dark:bg-zinc-900">
          <div className="flex items-center justify-between gap-3">
            <div>
              <div className="font-medium text-zinc-900 dark:text-zinc-100">允许 http 注册表</div>
              <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
                开启后允许向使用 http（非 https）的私有注册表发起请求。
              </div>
            </div>
            <Toggle checked={form.registry_insecure} onChange={(v) => update({ registry_insecure: v })} />
          </div>
        </section>

        {/* 注册表镜像 */}
        <section className="rounded-2xl border border-zinc-100 bg-white p-5 dark:border-zinc-800 dark:bg-zinc-900">
          <div className="font-medium text-zinc-900 dark:text-zinc-100">注册表镜像主机</div>
          <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
            非空时所有 manifest/tag 请求改发往该主机（用于私有仓库或加速镜像）。留空表示不使用镜像。
          </div>
          <input
            type="text"
            placeholder="如 mirror.example.com 或 localhost:5000"
            value={form.registry_mirror}
            onChange={(e) => update({ registry_mirror: e.target.value })}
            className="mt-3 w-full rounded-xl border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-900 outline-none focus:border-blue-500 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
          />
        </section>

        {/* 钉钉通知 Webhook */}
        <section className="rounded-2xl border border-zinc-100 bg-white p-5 dark:border-zinc-800 dark:bg-zinc-900">
          <div className="font-medium text-zinc-900 dark:text-zinc-100">钉钉通知 Webhook</div>
          <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
            镜像更新时自动发送钉钉通知。留空表示不启用。可在钉钉群机器人设置中获取 Webhook 地址。
          </div>
          <input
            type="text"
            placeholder="https://oapi.dingtalk.com/robot/send?access_token=xxx"
            value={form.dingtalk_webhook}
            onChange={(e) => update({ dingtalk_webhook: e.target.value })}
            className="mt-3 w-full rounded-xl border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-900 outline-none focus:border-blue-500 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
          />
        </section>

        {msg && (
          <div
            className={`rounded-xl px-4 py-2 text-sm ${
              msg.type === 'ok'
                ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                : 'bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-300'
            }`}
          >
            {msg.text}
          </div>
        )}

        <div className="flex items-center gap-3">
          <button
            type="submit"
            disabled={saving}
            className="inline-flex items-center gap-2 rounded-xl bg-zinc-900 px-5 py-2.5 text-sm font-medium text-white transition-colors hover:bg-zinc-700 disabled:opacity-60 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200"
          >
            {saving ? '保存中…' : '保存设置'}
          </button>
          <button
            type="button"
            onClick={() => api.scanNow()}
            className="rounded-xl border border-zinc-200 px-5 py-2.5 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
          >
            立即扫描一次
          </button>
        </div>
      </form>
    </div>
  )
}
