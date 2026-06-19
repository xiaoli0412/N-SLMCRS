import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, UpstreamKey } from '../api'
import { PageHeader, StatusBadge, Spinner, EmptyState, Card } from '../components/ui'

export default function Keys() {
  const { t } = useTranslation()
  const [keys, setKeys] = useState<UpstreamKey[]>([])
  const [loading, setLoading] = useState(true)
  const [showAdd, setShowAdd] = useState(false)
  const [form, setForm] = useState({ key_value: '', label: '', email: '', rpm_override: 0 })

  const load = async () => {
    try {
      const r = await api.listKeys()
      setKeys(r.data || [])
    } catch { /* */ }
    setLoading(false)
  }

  useEffect(() => { load() }, [])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    await api.addKey(form)
    setForm({ key_value: '', label: '', email: '', rpm_override: 0 })
    setShowAdd(false)
    load()
  }

  return (
    <>
      <PageHeader title={t('nav.keys')} en="Upstream Keys" subtitle="精细化配置 NVIDIA Studio 上游 nvapi- 密钥（每 Key 独立 RPM / 标签 / 状态）" />
      <div className="mb-4 flex justify-end">
        <button onClick={() => setShowAdd(!showAdd)} className="btn-primary">+ {t('common.add')}</button>
      </div>

      {showAdd && (
        <Card className="mb-4 animate-slide-up">
          <form onSubmit={submit} className="grid grid-cols-4 gap-3">
            <div>
              <label className="text-[11px] text-gray-500">NVIDIA Key (nvapi-xxx)</label>
              <input className="input mt-1" value={form.key_value} onChange={(e) => setForm({ ...form, key_value: e.target.value })} placeholder="nvapi-xxxxxxxxxxxx" required />
            </div>
            <div>
              <label className="text-[11px] text-gray-500">{t('common.status')} / 标签</label>
              <input className="input mt-1" value={form.label} onChange={(e) => setForm({ ...form, label: e.target.value })} placeholder="账号备注" />
            </div>
            <div>
              <label className="text-[11px] text-gray-500">邮箱</label>
              <input className="input mt-1" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} placeholder="注册邮箱" />
            </div>
            <div>
              <label className="text-[11px] text-gray-500">RPM 覆盖（0=默认40）</label>
              <input type="number" className="input mt-1" value={form.rpm_override} onChange={(e) => setForm({ ...form, rpm_override: +e.target.value })} />
            </div>
            <div className="col-span-4 flex gap-2 justify-end">
              <button type="button" onClick={() => setShowAdd(false)} className="btn-ghost">取消</button>
              <button type="submit" className="btn-primary">{t('common.save')}</button>
            </div>
          </form>
        </Card>
      )}

      {loading ? <Spinner /> : keys.length === 0 ? <EmptyState text="尚未配置上游密钥，点击右上角添加" /> : (
        <div className="glass-card overflow-hidden">
          <table className="w-full text-[13px]">
            <thead>
              <tr className="bg-white/[0.02] text-gray-500 text-[11px] uppercase tracking-wider">
                <th className="text-left px-4 py-2.5 font-semibold">密钥</th>
                <th className="text-left px-4 py-2.5 font-semibold">标签</th>
                <th className="text-left px-4 py-2.5 font-semibold">邮箱</th>
                <th className="text-left px-4 py-2.5 font-semibold">RPM</th>
                <th className="text-left px-4 py-2.5 font-semibold">{t('common.status')}</th>
                <th className="text-left px-4 py-2.5 font-semibold">{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id} className="border-t border-white/[0.04] hover:bg-white/[0.015]">
                  <td className="px-4 py-2.5 font-mono text-[12px] text-gray-300">{k.key_mask}</td>
                  <td className="px-4 py-2.5 text-gray-400">{k.label || '—'}</td>
                  <td className="px-4 py-2.5 text-gray-500">{k.email || '—'}</td>
                  <td className="px-4 py-2.5 text-gray-400">{k.rpm_override || 40}</td>
                  <td className="px-4 py-2.5"><StatusBadge status={k.status} /></td>
                  <td className="px-4 py-2.5">
                    <div className="flex gap-1.5">
                      <button onClick={async () => { await api.toggleKey(k.id, !k.enabled); load() }}
                        className={`btn-ghost text-[11px] ${k.enabled ? 'text-nv-green' : 'text-gray-600'}`}>
                        {k.enabled ? '● 启用' : '○ 停用'}
                      </button>
                      <button onClick={async () => { if (confirm('确认删除？')) { await api.deleteKey(k.id); load() } }}
                        className="btn-ghost text-[11px] text-red-400 hover:text-red-300">
                        删除
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
