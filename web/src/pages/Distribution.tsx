import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, Credential } from '../api'
import { PageHeader, Spinner, EmptyState, Card, StatusBadge } from '../components/ui'

interface Hook {
  id: string
  name: string
  type: string
  status: 'connected' | 'idle' | 'error'
  desc: string
}

// 未来对接的下游集成钩子（占位，实际启用时再实现协议）
const HOOKS: Hook[] = [
  { id: 'newapi', name: 'new-api', type: 'Token 中转', status: 'idle', desc: '将网关作为上游接入 new-api 渠道，自动同步模型列表与计费' },
  { id: 'octopus', name: 'OCTOPUS', type: '聚合分发', status: 'idle', desc: '对接 OCTOPUS 多渠道负载均衡，凭证下发与流量回采' },
  { id: 'webhook', name: 'Webhook', type: '事件回调', status: 'idle', desc: '请求成功 / 失败 / 限流触发外部 Webhook 通知' },
  { id: 'openai_sdk', name: 'OpenAI SDK', type: '协议兼容', status: 'connected', desc: 'OpenAI Python/Node SDK 直连，base_url 指向本网关即可' },
]

export default function Distribution() {
  const { t } = useTranslation()
  const [creds, setCreds] = useState<Credential[]>([])
  const [loading, setLoading] = useState(true)
  const [showAdd, setShowAdd] = useState(false)
  const [form, setForm] = useState({ name: '', rpm_limit: 0, allowed_models: '' })
  const [newCred, setNewCred] = useState<{ id: number; credential: string } | null>(null)

  const load = async () => {
    try {
      const r = await api.listCredentials()
      setCreds(r.data || [])
    } catch { /* */ }
    setLoading(false)
  }

  useEffect(() => { load() }, [])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    const r = await api.addCredential(form)
    setNewCred({ id: r.id, credential: r.credential })
    setForm({ name: '', rpm_limit: 0, allowed_models: '' })
    setShowAdd(false)
    load()
  }

  const copy = (s: string) => navigator.clipboard?.writeText(s)

  return (
    <>
      <PageHeader title={t('nav.distribution')} en="Distribution" subtitle="向下游客户端签发凭证 · 配置集成钩子接入第三方平台" />

      <div className="mb-4 flex justify-end">
        <button onClick={() => setShowAdd(!showAdd)} className="btn-primary">+ {t('common.create_credential')}</button>
      </div>

      {/* 新凭证一次性展示 */}
      {newCred && (
        <Card className="mb-4 !border-nv-green/30 !bg-nv-green/[0.04] animate-slide-up">
          <div className="flex items-center justify-between">
            <div>
              <div className="text-[12px] text-nv-green font-semibold mb-1">✓ 凭证已签发（仅显示一次，请妥善保存）</div>
              <code className="text-[13px] text-gray-200 font-mono break-all">{newCred.credential}</code>
            </div>
            <div className="flex gap-2">
              <button onClick={() => copy(newCred.credential)} className="btn-ghost text-[11px]">📋 复制</button>
              <button onClick={() => setNewCred(null)} className="btn-ghost text-[11px]">关闭</button>
            </div>
          </div>
        </Card>
      )}

      {showAdd && (
        <Card className="mb-4 animate-slide-up">
          <form onSubmit={submit} className="grid grid-cols-3 gap-3">
            <div>
              <label className="text-[11px] text-gray-500">名称 / 备注</label>
              <input className="input mt-1" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="如：内网测试客户端" />
            </div>
            <div>
              <label className="text-[11px] text-gray-500">RPM 限额（0=不限）</label>
              <input type="number" className="input mt-1" value={form.rpm_limit} onChange={(e) => setForm({ ...form, rpm_limit: +e.target.value })} />
            </div>
            <div>
              <label className="text-[11px] text-gray-500">允许模型（逗号分隔，空=全部）</label>
              <input className="input mt-1" value={form.allowed_models} onChange={(e) => setForm({ ...form, allowed_models: e.target.value })} placeholder="deepseek-ai/deepseek-r1,meta/llama-3.1-405b" />
            </div>
            <div className="col-span-3 flex gap-2 justify-end">
              <button type="button" onClick={() => setShowAdd(false)} className="btn-ghost">取消</button>
              <button type="submit" className="btn-primary">{t('common.save')}</button>
            </div>
          </form>
        </Card>
      )}

      {/* 凭证表 */}
      {loading ? <Spinner /> : creds.length === 0 ? <EmptyState text="尚未签发下游凭证，点击右上角创建" /> : (
        <div className="glass-card overflow-hidden mb-5">
          <table className="w-full text-[13px]">
            <thead>
              <tr className="bg-white/[0.02] text-gray-500 text-[11px] uppercase tracking-wider">
                <th className="text-left px-4 py-2.5 font-semibold">凭证</th>
                <th className="text-left px-4 py-2.5 font-semibold">名称</th>
                <th className="text-left px-4 py-2.5 font-semibold">RPM 限额</th>
                <th className="text-left px-4 py-2.5 font-semibold">允许模型</th>
                <th className="text-right px-4 py-2.5 font-semibold">已用请求</th>
                <th className="text-left px-4 py-2.5 font-semibold">状态</th>
                <th className="text-left px-4 py-2.5 font-semibold">{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {creds.map((c) => (
                <tr key={c.id} className="border-t border-white/[0.04] hover:bg-white/[0.015]">
                  <td className="px-4 py-2.5">
                    <div className="flex items-center gap-2">
                      <code className="font-mono text-[12px] text-gray-300">{c.credential_mask}</code>
                    </div>
                  </td>
                  <td className="px-4 py-2.5 text-gray-400">{c.name || '—'}</td>
                  <td className="px-4 py-2.5 text-gray-400">{c.rpm_limit || '不限'}</td>
                  <td className="px-4 py-2.5 text-gray-500 text-[11px] max-w-[200px] truncate">{c.allowed_models || '全部'}</td>
                  <td className="px-4 py-2.5 text-right text-gray-300">{c.total_requests}</td>
                  <td className="px-4 py-2.5">
                    <StatusBadge status={c.enabled ? 'active' : 'disabled'} />
                  </td>
                  <td className="px-4 py-2.5">
                    <button onClick={async () => { if (confirm('确认删除此凭证？下游将无法访问')) { await api.deleteCredential(c.id); load() } }}
                      className="btn-ghost text-[11px] text-red-400 hover:text-red-300">
                      {t('common.delete')}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* 集成钩子 */}
      <div className="glass-card p-5">
        <div className="flex items-center justify-between mb-3">
          <div className="text-[13px] font-semibold text-gray-200">
            集成钩子 <span className="text-gray-600 text-[11px] font-normal">/ Integration Hooks</span>
          </div>
          <span className="text-[10px] text-gray-600">未来扩展 · 部分已内置</span>
        </div>
        <div className="grid grid-cols-2 gap-3">
          {HOOKS.map((h) => (
            <div key={h.id} className="p-3.5 rounded-xl border border-white/[0.05] bg-white/[0.015] hover:border-nv-green/20 transition-colors">
              <div className="flex items-start justify-between mb-2">
                <div>
                  <div className="text-[13px] font-semibold text-gray-200">{h.name}</div>
                  <div className="text-[10px] text-gray-600 mt-0.5">{h.type}</div>
                </div>
                <span className={`text-[10px] px-1.5 py-0.5 rounded border ${
                  h.status === 'connected' ? 'text-nv-green bg-nv-green/10 border-nv-green/20' : 'text-gray-500 bg-white/[0.03] border-white/[0.06]'
                }`}>
                  {h.status === 'connected' ? '● 已接入' : '○ 待启用'}
                </span>
              </div>
              <div className="text-[11.5px] text-gray-500 leading-relaxed">{h.desc}</div>
            </div>
          ))}
        </div>
      </div>
    </>
  )
}
