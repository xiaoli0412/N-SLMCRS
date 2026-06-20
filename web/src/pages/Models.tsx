import { useEffect, useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ModelView } from '../api'
import { PageHeader, Spinner, EmptyState } from '../components/ui'

// 能力元信息：图标 / 中文标签 / 徽标配色。
// 与后端 internal/modelcatalog 的能力常量一一对应。
const CAP_META: Record<string, { icon: string; label: string; cls: string }> = {
  chat: { icon: '💬', label: '对话', cls: 'bg-nv-green/10 text-nv-green border-nv-green/20' },
  reasoning: { icon: '🧠', label: '推理', cls: 'bg-purple-500/10 text-purple-400 border-purple-500/20' },
  code: { icon: '⌨️', label: '代码', cls: 'bg-cyan-500/10 text-cyan-400 border-cyan-500/20' },
  vision: { icon: '👁️', label: '视觉', cls: 'bg-pink-500/10 text-pink-400 border-pink-500/20' },
  embedding: { icon: '🔗', label: '嵌入', cls: 'bg-amber-500/10 text-amber-400 border-amber-500/20' },
  rerank: { icon: '↕️', label: '重排', cls: 'bg-rose-500/10 text-rose-400 border-rose-500/20' },
  safety: { icon: '🛡️', label: '安全', cls: 'bg-red-500/10 text-red-400 border-red-500/20' },
  reward: { icon: '🏆', label: '奖励', cls: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20' },
  translation: { icon: '🌐', label: '翻译', cls: 'bg-blue-500/10 text-blue-400 border-blue-500/20' },
  parsing: { icon: '📄', label: '解析', cls: 'bg-teal-500/10 text-teal-400 border-teal-500/20' },
}

// 按模型 ID 命名推断厂商图标（用于卡片左上角小标识，能力徽标在右上角）。
function vendorIcon(id: string): string {
  const lid = (id || '').toLowerCase()
  if (lid.includes('deepseek')) return '🧠'
  if (lid.includes('llama')) return '🦙'
  if (lid.includes('qwen')) return '⚡'
  if (lid.includes('mistral') || lid.includes('mixtral')) return '🌬️'
  if (lid.includes('gemma')) return '💎'
  if (lid.includes('phi')) return 'Φ'
  if (lid.includes('nemotron') || lid.includes('nvidia')) return '⬢'
  return '⬢'
}

function capMeta(cap: string) {
  return CAP_META[cap] || { icon: '⬢', label: cap || '通用', cls: 'bg-white/[0.04] text-gray-400 border-white/[0.06]' }
}

// 上下文长度格式化：7B 类数字转 K。
function fmtCtx(len: number): string {
  if (!len) return '—'
  if (len >= 1024) return `${(len / 1024).toFixed(len % 1024 === 0 ? 0 : 1)}K`
  return String(len)
}

// 能力筛选选项（按对话优先排序，与公开端点 chat-only 过滤呼应）。
const CAP_FILTERS: Array<{ value: string; label: string }> = [
  { value: '', label: '全部能力' },
  { value: 'chat', label: '对话' },
  { value: 'reasoning', label: '推理' },
  { value: 'code', label: '代码' },
  { value: 'vision', label: '视觉' },
  { value: 'embedding', label: '嵌入' },
  { value: 'rerank', label: '重排' },
  { value: 'safety', label: '安全' },
  { value: 'reward', label: '奖励' },
  { value: 'translation', label: '翻译' },
  { value: 'parsing', label: '解析' },
]

export default function Models() {
  const { t } = useTranslation()
  const [models, setModels] = useState<ModelView[]>([])
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [q, setQ] = useState('')
  const [cap, setCap] = useState('')
  const [activeOnly, setActiveOnly] = useState(true)
  const [lastSync, setLastSync] = useState(0)

  const load = async () => {
    try {
      const r = await api.listModelsPlaza({ capability: cap || undefined, active_only: activeOnly })
      setModels(r.data || [])
      setLastSync(r.last_sync || 0)
    } catch { /* */ }
    setLoading(false)
  }

  // capability / activeOnly 变化时重新拉取（广场端点支持服务端过滤）
  useEffect(() => { load() }, [cap, activeOnly]) // eslint-disable-line react-hooks/exhaustive-deps

  const sync = async () => {
    setSyncing(true)
    try { await api.syncModels(); await load() } catch { /* */ }
    setSyncing(false)
  }

  // 客户端二次模糊搜索（按 id / 厂商 / 描述）
  const filtered = useMemo(() => {
    if (!q) return models
    const k = q.toLowerCase()
    return models.filter((m) =>
      m.id.toLowerCase().includes(k) ||
      (m.owned_by || '').toLowerCase().includes(k) ||
      (m.description || '').toLowerCase().includes(k),
    )
  }, [models, q])

  // 按能力分组统计（用于顶部能力筛选 chips 旁的数量）
  const capCounts = useMemo(() => {
    const m = new Map<string, number>()
    for (const it of models) m.set(it.capability || 'chat', (m.get(it.capability || 'chat') || 0) + 1)
    return m
  }, [models])

  return (
    <>
      <PageHeader title={t('nav.models')} en="Model Catalog" subtitle="每 24h 自动从 NVIDIA /v1/models 同步 · 失效模型自动推荐最佳替代" />

      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex-1 min-w-[240px] max-w-md relative">
          <input
            className="input w-full"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="🔍 搜索模型 ID / 厂商 / 描述..."
          />
        </div>
        <div className="flex items-center gap-3">
          <select
            className="input max-w-[150px]"
            value={cap}
            onChange={(e) => setCap(e.target.value)}
          >
            {CAP_FILTERS.map((f) => (
              <option key={f.value} value={f.value}>
                {f.label}{f.value && capCounts.has(f.value) ? ` · ${capCounts.get(f.value)}` : ''}
              </option>
            ))}
          </select>
          <label className="flex items-center gap-1.5 text-[11px] text-gray-500 select-none cursor-pointer">
            <input type="checkbox" checked={activeOnly} onChange={(e) => setActiveOnly(e.target.checked)} />
            仅可用
          </label>
          {lastSync > 0 && (
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
            const cm = capMeta(m.capability)
            const stale = !m.is_active
            const rate = m.success_rate || 0
            return (
              <div key={m.id} className={`glass-card p-4 hover:border-nv-green/30 transition-colors ${stale ? 'opacity-50' : ''}`}>
                <div className="flex items-start justify-between mb-2 gap-2">
                  <div className="flex items-center gap-2 min-w-0">
                    <span className="w-7 h-7 rounded-md flex items-center justify-center text-[12px] bg-white/[0.04] shrink-0">
                      {vendorIcon(m.id)}
                    </span>
                    <div className="min-w-0">
                      <div className="text-[12.5px] font-semibold text-gray-200 font-mono leading-tight truncate" title={m.id}>
                        {m.id}
                      </div>
                      <div className="text-[10px] text-gray-600 truncate">{m.owned_by || '—'}</div>
                    </div>
                  </div>
                  <span className={`shrink-0 inline-flex items-center gap-1 px-1.5 py-0.5 rounded border text-[10px] ${cm.cls}`}>
                    <span>{cm.icon}</span>{cm.label}
                  </span>
                </div>

                <div className="text-[10.5px] text-gray-500 leading-snug min-h-[28px]">
                  {m.description || '—'}
                </div>

                <div className="mt-3 grid grid-cols-4 gap-2 text-[10.5px]">
                  <div>
                    <div className="text-gray-600">参数量</div>
                    <div className="text-gray-300 font-semibold">{m.param_count || '—'}</div>
                  </div>
                  <div>
                    <div className="text-gray-600">上下文</div>
                    <div className="text-gray-300 font-semibold">{fmtCtx(m.context_length)}</div>
                  </div>
                  <div>
                    <div className="text-gray-600">请求量</div>
                    <div className="text-gray-300 font-semibold">{m.request_count || 0}</div>
                  </div>
                  <div>
                    <div className="text-gray-600">成功率</div>
                    <div className={`font-semibold ${rate >= 95 ? 'text-nv-green' : rate >= 80 ? 'text-amber-400' : rate > 0 ? 'text-red-400' : 'text-gray-500'}`}>
                      {rate > 0 ? `${rate.toFixed(1)}%` : '—'}
                    </div>
                  </div>
                </div>

                {stale && (
                  <div className="mt-2 pt-2 border-t border-white/[0.04] text-[10.5px] text-amber-400">
                    ⚠ 该模型已下线，对话请求将自动推荐当前成功率最高的可用替代
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </>
  )
}
