import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, Model, ModelStats } from '../api'
import { PageHeader, Spinner, EmptyState } from '../components/ui'

export default function Models() {
  const { t } = useTranslation()
  const [models, setModels] = useState<Model[]>([])
  const [stats, setStats] = useState<ModelStats[]>([])
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [q, setQ] = useState('')

  const load = async () => {
    try {
      const [mr, sr] = await Promise.all([
        api.listModels(),
        api.getModelStats('1h').catch(() => ({ data: [] as ModelStats[] })),
      ])
      setModels(mr.data || [])
      setStats(sr.data || [])
    } catch { /* */ }
    setLoading(false)
  }

  useEffect(() => { load() }, [])

  const sync = async () => {
    setSyncing(true)
    try { await api.syncModels(); await load() } catch { /* */ }
    setSyncing(false)
  }

  // 按 model_id 索引 stats
  const statsById = new Map(stats.map((s) => [s.model_id, s]))

  const filtered = models.filter((m) =>
    !q || m.id.toLowerCase().includes(q.toLowerCase()) ||
    m.capability.toLowerCase().includes(q.toLowerCase()) ||
    m.owned_by.toLowerCase().includes(q.toLowerCase()))

  // 最近一次同步时间（最大 synced_at）
  const lastSync = models.reduce<number | null>((acc, m) => {
    if (m.synced_at > (acc ?? 0)) return m.synced_at
    return acc
  }, null)

  return (
    <>
      <PageHeader title={t('nav.models')} en="Model Catalog" subtitle="每 24h 自动从 NVIDIA /v1/models 同步 · 失效模型自动推荐最佳替代" />

      <div className="mb-4 flex items-center justify-between gap-3">
        <div className="flex-1 max-w-md relative">
          <input
            className="input w-full"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="🔍 搜索模型 ID / 类型..."
          />
        </div>
        <div className="flex items-center gap-3">
          {lastSync && (
            <span className="text-[11px] text-gray-600">
              上次同步: {new Date(lastSync * 1000).toLocaleString('zh-CN', { hour12: false })}
            </span>
          )}
          <button onClick={sync} disabled={syncing} className="btn-primary">
            {syncing ? '⏳ 同步中...' : `↻ ${t('common.sync_models')}`}
          </button>
        </div>
      </div>

      {loading ? <Spinner /> : filtered.length === 0 ? (
        <EmptyState text={models.length === 0 ? "尚未同步模型，点击右上角立即同步" : "未匹配到模型"} />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          {filtered.map((m) => {
            const stale = !m.is_active
            const type = inferType(m.id)
            const ms = statsById.get(m.id)
            const total = ms?.total_requests ?? 0
            const rate = ms?.success_rate ?? 0
            return (
              <div key={m.id} className={`glass-card p-4 hover:border-nv-green/30 transition-colors ${stale ? 'opacity-50' : ''}`}>
                <div className="flex items-start justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <span className={`w-7 h-7 rounded-md flex items-center justify-center text-[12px] ${type.bg}`}>
                      {type.icon}
                    </span>
                    <div>
                      <div className="text-[12.5px] font-semibold text-gray-200 font-mono leading-tight">{m.id}</div>
                      <div className="text-[10px] text-gray-600">{type.label}{m.owned_by ? ` · ${m.owned_by}` : ''}</div>
                    </div>
                  </div>
                  {stale ? (
                    <span className="text-[10px] px-1.5 py-0.5 rounded border border-red-500/20 bg-red-500/10 text-red-400">已下线</span>
                  ) : (
                    <span className="text-[10px] px-1.5 py-0.5 rounded border border-nv-green/20 bg-nv-green/10 text-nv-green">可用</span>
                  )}
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2 text-[10.5px]">
                  <div>
                    <div className="text-gray-600">请求量</div>
                    <div className="text-gray-300 font-semibold">{total}</div>
                  </div>
                  <div>
                    <div className="text-gray-600">成功率</div>
                    <div className={`font-semibold ${rate >= 95 ? 'text-nv-green' : rate >= 80 ? 'text-amber-400' : total === 0 ? 'text-gray-400' : 'text-red-400'}`}>
                      {total === 0 ? '—' : `${rate.toFixed(1)}%`}
                    </div>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </>
  )
}

function inferType(id: string): { icon: string; label: string; bg: string } {
  const lid = (id || '').toLowerCase()
  if (lid.includes('deepseek')) return { icon: '🧠', label: 'DeepSeek · 推理', bg: 'bg-purple-500/10 text-purple-400' }
  if (lid.includes('llama')) return { icon: '🦙', label: 'Meta Llama', bg: 'bg-blue-500/10 text-blue-400' }
  if (lid.includes('qwen')) return { icon: '⚡', label: 'Qwen 通义', bg: 'bg-cyan-500/10 text-cyan-400' }
  if (lid.includes('mistral') || lid.includes('mixtral')) return { icon: '🌬️', label: 'Mistral', bg: 'bg-orange-500/10 text-orange-400' }
  if (lid.includes('gemma') || lid.includes('gemma')) return { icon: '💎', label: 'Gemma', bg: 'bg-pink-500/10 text-pink-400' }
  if (lid.includes('phi')) return { icon: 'Φ', label: 'Phi 微软', bg: 'bg-teal-500/10 text-teal-400' }
  if (lid.includes('nemotron')) return { icon: '⬢', label: 'Nemotron NVIDIA', bg: 'bg-nv-green/10 text-nv-green' }
  if (lid.includes('embedding') || lid.includes('arctic-embed')) return { icon: '🔗', label: 'Embedding', bg: 'bg-amber-500/10 text-amber-400' }
  if (lid.includes('rerank') || lid.includes('reranker')) return { icon: '↕', label: 'Rerank', bg: 'bg-rose-500/10 text-rose-400' }
  return { icon: '⬢', label: '通用模型', bg: 'bg-white/[0.04] text-gray-400' }
}