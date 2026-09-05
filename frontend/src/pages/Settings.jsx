import { useEffect, useState } from 'react'
import { api } from '../api/client'
import BentoCard from '../components/BentoCard'
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
  const [saved, setSaved] = useState(null) // 最近一次保存的基线，用于「未保存更改」提示
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState(null)
  const [testing, setTesting] = useState(false)
  const [testMsg, setTestMsg] = useState(null)

  useEffect(() => {
    ;(async () => {
      try {
        const s = await api.settings()
        setForm(s)
        setSaved(s)
      } catch {
        setMsg({ type: 'error', text: '加载设置失败' })
      } finally {
        setLoading(false)
      }
    })()
  }, [])

  const update = (patch) => setForm((f) => ({ ...f, ...patch }))
  const dirty = !!form && !!saved && JSON.stringify(form) !== JSON.stringify(saved)

  const onSave = async (e) => {
    e.preventDefault()
    setSaving(true)
    setMsg(null)
    try {
      const s = await api.saveSettings(form)
      setForm(s)
      setSaved(s)
      setMsg({ type: 'ok', text: '设置已保存，扫描间隔等将立即生效' })
    } catch {
      setMsg({ type: 'error', text: '保存失败，请重试' })
    } finally {
      setSaving(false)
    }
  }

  const onTestDingTalk = async () => {
    setTesting(true)
    setTestMsg(null)
    try {
      const res = await api.testDingTalk(form.dingtalk_webhook, form.dingtalk_secret)
      if (res.ok) {
        setTestMsg({ type: 'ok', text: '测试通知已发送，请检查钉钉群是否收到' })
      } else {
        setTestMsg({ type: 'error', text: '连通性测试失败：' + (res.error || '未知错误') })
      }
    } catch (e) {
      setTestMsg({ type: 'error', text: '测试请求失败：' + (e?.message || e) })
    } finally {
      setTesting(false)
    }
  }

  if (loading || !form) return <Spinner label="加载设置…" />

  return (
    <div className="flex h-full flex-col gap-4 overflow-hidden">
      <div className="shrink-0">
        <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 md:text-3xl">设置</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">在页面上调整运行参数，保存后立即生效并持久化（重启后仍保留）。</p>
      </div>

      <form onSubmit={onSave} className="min-h-0 flex-1 space-y-4 overflow-y-auto pr-1">
        <div className="bento-grid">
          {/* 扫描间隔 */}
          <BentoCard span="wide">
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
                className="w-32 rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2 text-right text-sm text-zinc-900 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
              />
            </div>
          </BentoCard>

          {/* 关闭内置演示监控列表 */}
          <BentoCard>
            <div className="flex items-center justify-between gap-3">
              <div>
                <div className="font-medium text-zinc-900 dark:text-zinc-100">演示监控列表</div>
                <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
                  关闭内置 nginx/redis/postgres 等演示镜像
                </div>
              </div>
              <Toggle checked={form.disable_default_watch} onChange={(v) => update({ disable_default_watch: v })} />
            </div>
          </BentoCard>

          {/* 允许 http 注册表 */}
          <BentoCard>
            <div className="flex items-center justify-between gap-3">
              <div>
                <div className="font-medium text-zinc-900 dark:text-zinc-100">HTTP 注册表</div>
                <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
                  允许向 http 私有注册表发起请求
                </div>
              </div>
              <Toggle checked={form.registry_insecure} onChange={(v) => update({ registry_insecure: v })} />
            </div>
          </BentoCard>

          {/* 注册表镜像 */}
          <BentoCard span="wide">
            <div className="font-medium text-zinc-900 dark:text-zinc-100">注册表镜像主机</div>
            <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
              非空时所有请求改发往该主机（用于私有仓库或加速镜像）。留空不使用镜像。
            </div>
            <input
              type="text"
              placeholder="如 mirror.example.com 或 localhost:5000"
              value={form.registry_mirror}
              onChange={(e) => update({ registry_mirror: e.target.value })}
              className="mt-3 w-full rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-900 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
            />
            <div className="mt-3 space-y-1 text-xs text-zinc-400 dark:text-zinc-500">
              <p>· Docker Hub 加速：填写镜像代理域名</p>
              <p>· 私有仓库：填写 registry 主机名（如 harbor.example.com）</p>
            </div>
          </BentoCard>

          {/* 钉钉通知 Webhook */}
          <BentoCard span="wide">
            <div className="font-medium text-zinc-900 dark:text-zinc-100">钉钉通知 Webhook</div>
            <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
              镜像更新时自动发送钉钉通知。留空不启用。
            </div>
            <input
              type="text"
              placeholder="https://oapi.dingtalk.com/robot/send?access_token=xxx"
              value={form.dingtalk_webhook}
              onChange={(e) => update({ dingtalk_webhook: e.target.value })}
              className="mt-3 w-full rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-900 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
            />
            <div className="mt-3 font-medium text-zinc-900 dark:text-zinc-100">加签密钥（可选）</div>
            <div className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">
              若机器人启用了「加签」安全设置，请填写密钥；留空表示不加签。
            </div>
            <input
              type="password"
              placeholder="钉钉机器人安全设置中的加签密钥"
              value={form.dingtalk_secret}
              onChange={(e) => update({ dingtalk_secret: e.target.value })}
              className="mt-3 w-full rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-900 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-100"
            />
            <div className="mt-3">
              <button
                type="button"
                onClick={onTestDingTalk}
                disabled={testing || !form.dingtalk_webhook.trim()}
                className="inline-flex items-center gap-3 rounded-xl border border-zinc-200 px-4 py-2 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-50 disabled:opacity-60 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
              >
                {testing ? '测试中…' : '测试连接'}
              </button>
              {testMsg && (
                <span
                  className={`ml-3 text-sm ${
                    testMsg.type === 'ok'
                      ? 'text-emerald-600 dark:text-emerald-400'
                      : 'text-rose-600 dark:text-rose-400'
                  }`}
                >
                  {testMsg.text}
                </span>
              )}
            </div>
          </BentoCard>
        </div>

        {msg && (
          <div
            className={`rounded-xl px-4 py-2 text-sm ${
              msg.type === 'ok'
                ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                : 'bg-rose-50 text-rose-700 dark:bg-rose-500/10 dark:text-rose-300'
            }`}
          >
            {msg.text}
          </div>
        )}

        <div className="flex flex-wrap items-center gap-3">
          <button
            type="submit"
            disabled={saving || !dirty}
            className="inline-flex items-center gap-3 rounded-xl bg-zinc-900 px-5 py-2.5 text-sm font-medium text-white transition-colors hover:bg-zinc-700 disabled:opacity-60 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200"
          >
            {saving ? '保存中…' : '保存设置'}
          </button>
          {/* 未保存提示：表单与最近一次保存的基线不一致时高亮，避免改完忘记保存 */}
          <span className={`text-sm ${dirty ? 'font-medium text-amber-600 dark:text-amber-400' : 'text-zinc-400 dark:text-zinc-500'}`}>
            {dirty ? '有未保存的更改' : '所有更改已保存'}
          </span>
        </div>
      </form>
    </div>
  )
}
