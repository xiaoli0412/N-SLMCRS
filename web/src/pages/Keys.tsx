import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, UpstreamKey, BulkImportItem } from '../api'
import { PageHeader, StatusBadge, Spinner, EmptyState, Card } from '../components/ui'

export default function Keys() {
  const { t } = useTranslation()
  const [keys, setKeys] = useState<UpstreamKey[]>([])
  const [loading, setLoading] = useState(true)
  const [showAdd, setShowAdd] = useState(false)
  const [showBulk, setShowBulk] = useState(false)
  const [toast, setToast] = useState<{ msg: string; kind: 'ok' | 'err' } | null>(null)
  const [form, setForm] = useState({ key_value: '', label: '', email: '', rpm_override: 0 })

  // 批量导入表单
  const [bulk, setBulk] = useState({ raw: '', label: '', email: '', rpm_override: 0 })
  const [bulkResult, setBulkResult] = useState<{ added: number; skipped: number; items: BulkImportItem[] } | null>(null)
  const [bulkBusy, setBulkBusy] = useState(false)

  const flash = (msg: string, kind: 'ok' | 'err' = 'ok') => {
    setToast({ msg, kind })
    setTimeout(() => setToast(null), 2600)
  }

  const load = async () => {
    try {
      const r = await api.listKeys()
      setKeys(r.data || [])
    } catch (e: any) {
      flash(`加载失败: ${e.message || e}`, 'err')
    }
    setLoading(false)
  }

  useEffect(() => { load() }, [])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await api.addKey(form)
      setForm({ key_value: '', label: '', email: '', rpm_override: 0 })
      setShowAdd(false)
      flash('已添加上游密钥')
      load()
    } catch (e: any) {
      flash(`添加失败: ${e.message || e}`, 'err')
    }
  }

  const submitBulk = async (e: React.FormEvent) => {
    e.preventDefault()
    setBulkBusy(true)
    try {
      const r = await api.bulkAddKeys({
        raw: bulk.raw,
        label: bulk.label,
        email: bulk.email,
        rpm_override: bulk.rpm_override,
      })
      setBulkResult({ added: r.added, skipped: r.skipped, items: r.items || [] })
      flash(`导入完成：新增 ${r.added} · 跳过 ${r.skipped}`)
      load()
    } catch (e: any) {
      flash(`批量导入失败: ${e.message || e}`, 'err')
    } finally {
      setBulkBusy(false)
    }
  }

  // 实时统计粘贴框中合法 key 数量
  const parsedPreview = (() => {
    const parts = bulk.raw.split(/[\n\r,;\s]+/).map((s) => s.trim()).filter(Boolean)
    const valid = parts.filter((p) => p.startsWith('nvapi-'))
    return { total: parts.length, valid: valid.length }
  })()

  return (
    <>
      <PageHeader title={t('nav.keys')} en="Upstream Keys" subtitle="精细化配置 NVIDIA Studio 上游 nvapi- 密钥（每 Key 独立 RPM / 标签 / 状态）" />

      {toast && (
        <div className={`fixed top-4 right-4 z-50 px-4 py-2.5 rounded-lg text-[13px] font-medium shadow-lg animate-slide-up border ${
          toast.kind === 'ok'
            ? 'bg-nv-green/15 text-nv-green border-nv-green/30'
            : 'bg-red-500/15 text-red-300 border-red-500/30'
        }`}>
          {toast.kind === 'ok' ? '✓ ' : '✗ '}{toast.msg}
        </div>
      )}

      <div className="mb-4 flex flex-wrap items-center gap-2 justify-end">
        <span className="mr-auto text-[11px] text-surface-muted">
          共 <span className="text-gray-300 font-semibold">{keys.length}</span> 个密钥 ·
          活跃 <span className="text-nv-green font-semibold">{keys.filter((k) => k.enabled && k.status === 'active').length}</span>
        </span>
        <button onClick={() => { setShowBulk(!showBulk); setShowAdd(false); setBulkResult(null) }} className="btn-ghost border border-surface-border">
          ⬆ {t('common.bulk_import')}
        </button>
        <button onClick={() => { setShowAdd(!showAdd); setShowBulk(false) }} className="btn-primary">
          + {t('common.add')}
        </button>
      </div>

      {/* 批量导入面板 */}
      {showBulk && (
        <Card className="mb-4 animate-slide-up">
          <form onSubmit={submitBulk} className="space-y-3">
            <div className="flex items-center justify-between">
              <div>
                <div className="text-[13px] font-semibold text-gray-200">批量导入上游密钥</div>
                <div className="text-[11px] text-surface-muted mt-0.5">
                  每行一个 <code className="text-nv-green">nvapi-xxx</code>，或用逗号 / 分号 / 空白分隔；自动去重并跳过已存在。
                </div>
              </div>
              {parsedPreview.total > 0 && (
                <div className="text-[11px] px-2.5 py-1 rounded-md border border-surface-border bg-surface-card-hover">
                  解析 <span className="text-gray-200 font-semibold">{parsedPreview.total}</span> ·
                  合法 <span className="text-nv-green font-semibold">{parsedPreview.valid}</span>
                </div>
              )}
            </div>
            <textarea
              className="input font-mono text-[12px] min-h-[140px] resize-y"
              placeholder={'nvapi-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\nnvapi-yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy'}
              value={bulk.raw}
              onChange={(e) => { setBulk({ ...bulk, raw: e.target.value }); setBulkResult(null) }}
              required
            />
            <div className="grid grid-cols-3 gap-3">
              <div>
                <label className="text-[11px] text-surface-muted">统一标签（可选）</label>
                <input className="input mt-1" value={bulk.label} onChange={(e) => setBulk({ ...bulk, label: e.target.value })} placeholder="如：批量导入 2026-06" />
              </div>
              <div>
                <label className="text-[11px] text-surface-muted">统一邮箱（可选）</label>
                <input className="input mt-1" value={bulk.email} onChange={(e) => setBulk({ ...bulk, email: e.target.value })} placeholder="注册邮箱" />
              </div>
              <div>
                <label className="text-[11px] text-surface-muted">RPM 覆盖（0=默认40）</label>
                <input type="number" className="input mt-1" value={bulk.rpm_override} onChange={(e) => setBulk({ ...bulk, rpm_override: +e.target.value })} />
              </div>
            </div>

            {bulkResult && (
              <div className="rounded-lg border border-surface-border bg-surface-card-hover p-3 animate-slide-up">
                <div className="flex items-center gap-4 text-[12px] mb-2">
                  <span className="text-nv-green font-semibold">✓ 新增 {bulkResult.added}</span>
                  <span className="text-amber-400 font-semibold">⊘ 跳过 {bulkResult.skipped}</span>
                  <span className="text-surface-muted">共 {bulkResult.added + bulkResult.skipped} 条解析</span>
                </div>
                {bulkResult.items.length > 0 && (
                  <div className="max-h-[180px] overflow-y-auto text-[11px] font-mono space-y-0.5">
                    {bulkResult.items.map((it, i) => (
                      <div key={i} className="flex items-center gap-2">
                        <span className={
                          it.status === 'added' ? 'text-nv-green' :
                          it.status === 'duplicate' ? 'text-amber-400' : 'text-red-400'
                        }>
                          {it.status === 'added' ? '✓' : it.status === 'duplicate' ? '⊘' : '✗'}
                        </span>
                        <span className="text-gray-400 w-28 truncate">{it.key_mask || '—'}</span>
                        <span className="text-surface-muted">{it.status}{it.reason ? ` · ${it.reason}` : ''}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}

            <div className="flex gap-2 justify-end">
              <button type="button" onClick={() => { setShowBulk(false); setBulkResult(null) }} className="btn-ghost">取消</button>
              <button type="submit" disabled={bulkBusy} className="btn-primary disabled:opacity-50">
                {bulkBusy ? '导入中…' : `${t('common.import')} (${parsedPreview.valid})`}
              </button>
            </div>
          </form>
        </Card>
      )}

      {showAdd && (
        <Card className="mb-4 animate-slide-up">
          <form onSubmit={submit} className="grid grid-cols-4 gap-3">
            <div>
              <label className="text-[11px] text-surface-muted">NVIDIA Key (nvapi-xxx)</label>
              <input className="input mt-1" value={form.key_value} onChange={(e) => setForm({ ...form, key_value: e.target.value })} placeholder="nvapi-xxxxxxxxxxxx" required />
            </div>
            <div>
              <label className="text-[11px] text-surface-muted">{t('common.status')} / 标签</label>
              <input className="input mt-1" value={form.label} onChange={(e) => setForm({ ...form, label: e.target.value })} placeholder="账号备注" />
            </div>
            <div>
              <label className="text-[11px] text-surface-muted">邮箱</label>
              <input className="input mt-1" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} placeholder="注册邮箱" />
            </div>
            <div>
              <label className="text-[11px] text-surface-muted">RPM 覆盖（0=默认40）</label>
              <input type="number" className="input mt-1" value={form.rpm_override} onChange={(e) => setForm({ ...form, rpm_override: +e.target.value })} />
            </div>
            <div className="col-span-4 flex gap-2 justify-end">
              <button type="button" onClick={() => setShowAdd(false)} className="btn-ghost">取消</button>
              <button type="submit" className="btn-primary">{t('common.save')}</button>
            </div>
          </form>
        </Card>
      )}

      {loading ? <Spinner /> : keys.length === 0 ? <EmptyState text="尚未配置上游密钥，点击右上角添加或批量导入" /> : (
        <div className="card overflow-hidden">
          <table className="w-full text-[13px]">
            <thead>
              <tr className="bg-surface-card-hover text-surface-muted text-[11px] uppercase tracking-wider">
                <th className="text-left px-4 py-2.5 font-semibold">密钥</th>
                <th className="text-left px-4 py-2.5 font-semibold">标签</th>
                <th className="text-left px-4 py-2.5 font-semibold">邮箱</th>
                <th className="text-left px-4 py-2.5 font-semibold">RPM</th>
                <th className="text-left px-4 py-2.5 font-semibold">连续失败</th>
                <th className="text-left px-4 py-2.5 font-semibold">{t('common.status')}</th>
                <th className="text-left px-4 py-2.5 font-semibold">{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id} className="border-t border-surface-border/60 hover:bg-surface-card-hover">
                  <td className="px-4 py-2.5 font-mono text-[12px] text-gray-300">{k.key_mask}</td>
                  <td className="px-4 py-2.5 text-gray-400">{k.label || '—'}</td>
                  <td className="px-4 py-2.5 text-surface-muted">{k.email || '—'}</td>
                  <td className="px-4 py-2.5 text-gray-400">{k.rpm_override || 40}</td>
                  <td className="px-4 py-2.5">
                    {k.consecutive_fail > 0
                      ? <span className="text-amber-400">{k.consecutive_fail}</span>
                      : <span className="text-surface-muted">0</span>}
                  </td>
                  <td className="px-4 py-2.5"><StatusBadge status={k.status} /></td>
                  <td className="px-4 py-2.5">
                    <div className="flex gap-1.5">
                      <button onClick={async () => { await api.toggleKey(k.id, !k.enabled); load() }}
                        className={`btn-ghost text-[11px] ${k.enabled ? 'text-nv-green' : 'text-surface-muted'}`}>
                        {k.enabled ? '● 启用' : '○ 停用'}
                      </button>
                      <button onClick={async () => { if (confirm('确认删除？')) { await api.deleteKey(k.id); flash('已删除'); load() } }}
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
