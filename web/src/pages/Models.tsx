import { useEffect, useState, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { RefreshCw, FlaskConical, Search, Zap } from 'lucide-react'
import { toast } from 'sonner'
import { api, ModelView } from '../api'
import { PageHeader, Spinner, EmptyState, Button, Badge } from '../components/ui'

// 能力元信息：图标 / 中文标签 / 徽标配色。
const CAP_META: Record<string, { icon: string; label: string; variant: 'success' | 'info' | 'warn' | 'danger' | 'default' }> = {
  chat: { icon: '💬', label: '对话', variant: 'success' },
  reasoning: { icon: '🧠', label: '推理', variant: 'info' },
  code: { icon: '⌨️', label: '代码', variant: 'info' },
  vision: { icon: '👁️', label: '视觉', variant: 'warn' },
  embedding: { icon: '🔗', label: '嵌入', variant: 'warn' },
  rerank: { icon: '↕️', label: '重排', variant: 'danger' },
  safety: { icon: '🛡️', label: '安全', variant: 'danger' },
  reward: { icon: '🏆', label: '奖励', variant: 'warn' },
  translation: { icon: '🌐', label: '翻译', variant: 'info' },
  parsing: { icon: '📄', label: '解析', variant: 'default' },
}

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
  return CAP_META[cap] || { icon: '⬢', label: cap || '通用', variant: 'default' as const }
}

function fmtCtx(len: number): string {
  if (!len) return '—'
  if (len >= 1024) return `${(len / 1024).toFixed(len % 1024 === 0 ? 0 : 1)}K`
  return String(len)
}

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

// 可用度评分配色与文案
function availMeta(score: number): { variant: 'success' | 'warn' | 'danger' | 'default'; label: string; text: string } {
  if (score <= 0) return { variant: 'default', label: '—', text: 'text-surface-muted' }
  if (score >= 80) return { variant: 'success', label: `${score.toFixed(0)}`, text: 'text-nv-green' }
  if (score >= 50) return { variant: 'warn', label: `${score.toFixed(0)}`, text: 'text-amber-400' }
  return { variant: 'danger', label: `${score.toFixed(0)}`, text: 'text-red-400' }
}

export default function Models() {
  const { t } = useTranslation()
  const nav = useNavigate()
  const [models, setModels] = useState<ModelView[]>([])
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [probingAll, setProbingAll] = useState(false)
  const [testingId, setTestingId] = useState<string | null>(null)
  const [q, setQ] = useState('')
  const [cap, setCap] = useState('')
  const [activeOnly, setActiveOnly] = useState(false)
  const [lastSync, setLastSync] = useState(0)

  const load = async () => {
    try {
      const r = await api.listModelsPlaza({ capability: cap || undefined, active_only: activeOnly })
      setModels(r.data || [])
      setLastSync(r.last_sync || 0)
    } catch (e: any) {
      toast.error('加载模型失败', { description: e?.message })
    }
    setLoading(false)
  }

  useEffect(() => { load() }, [cap, activeOnly]) // eslint-disable-line react-hooks/exhaustive-deps

  const sync = async () => {
    setSyncing(true)
    try {
      await api.syncModels()
      toast.success('模型同步完成')
      await load()
    } catch (e: any) {
      toast.error('同步失败', { description: e?.message || '无可用上游密钥？先在 /keys 配置 nvapi- 密钥' })
    }
    setSyncing(false)
  }

  const probeAll = async () => {
    setProbingAll(true)
    try {
      await api.probeAllModels()
      toast.success('探活完成')
      await load()
    } catch (e: any) {
      toast.error('探活失败', { description: e?.message })
    }
    setProbingAll(false)
  }

  const testOne = async (id: string) => {
    setTestingId(id)
    try {
      const r = await api.testModel(id)
      if (r.ok) {
        toast.success(`${id} 可用`, { description: `延迟 ${r.latency_ms}ms · HTTP ${r.http_status}` })
      } else {
        toast.error(`${id} 不可用`, { description: r.error || r.status })
      }
      await load()
    } catch (e: any) {
      toast.error('探活失败', { description: e?.message })
    }
    setTestingId(null)
  }

  const filtered = useMemo(() => {
    if (!q) return models
    const k = q.toLowerCase()
    return models.filter((m) =>
      m.id.toLowerCase().includes(k) ||
      (m.owned_by || '').toLowerCase().includes(k) ||
      (m.description || '').toLowerCase().includes(k),
    )
  }, [models, q])

  const capCounts = useMemo(() => {
    const m = new Map<string, number>()
    for (const it of models) m.set(it.capability || 'chat', (m.get(it.capability || 'chat') || 0) + 1)
    return m
  }, [models])

  return (
    <>
      <PageHeader title={t('nav.models')} en="Model Plaza" subtitle="每 24h 自动从 NVIDIA /v1/models 同步 · 主动探活 + 被动统计双路可用度（仿 new-api）" />

      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex-1 min-w-[240px] max-w-md relative">
          <Search className="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-surface-muted" />
          <input
            className="input pl-9 w-full"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="搜索模型 ID / 厂商 / 描述..."
          />
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <select className="input max-w-[150px]" value={cap} onChange={(e) => setCap(e.target.value)}>
            {CAP_FILTERS.map((f) => (
              <option key={f.value} value={f.value}>
                {f.label}{f.value && capCounts.has(f.value) ? ` · ${capCounts.get(f.value)}` : ''}
              </option>
            ))}
          </select>
          <label className="flex items-center gap-1.5 text-[11px] text-surface-muted select-none cursor-pointer">
            <input type="checkbox" checked={activeOnly} onChange={(e) => setActiveOnly(e.target.checked)} />
            仅可用
          </label>
          {lastSync > 0 && (
            <span className="text-[11px] text-surface-muted">
              上次同步: {new Date(lastSync * 1000).toLocaleString('zh-CN', { hour12: false })}
            </span>
          )}
          <Button variant="outline" size="sm" onClick={probeAll} disabled={probingAll}>
            <Zap className="w-3.5 h-3.5" />
            {probingAll ? '探活中...' : '探活全部'}
          </Button>
          <Button size="sm" onClick={sync} disabled={syncing}>
            <RefreshCw className={`w-3.5 h-3.5 ${syncing ? 'animate-spin' : ''}`} />
            {syncing ? '同步中...' : t('common.sync_models')}
          </Button>
        </div>
      </div>

      {loading ? <Spinner /> : filtered.length === 0 ? (
        <EmptyState text={models.length === 0 ? '尚未同步模型，点击右上角立即同步' : '未匹配到模型'} />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          {filtered.map((m, i) => {
            const cm = capMeta(m.capability)
            const stale = !m.is_active
            const rate = m.success_rate || 0
            const av = availMeta(m.availability_score || 0)
            const probing = m.last_probe_ts > 0
            return (
              <div key={m.id} onClick={() => nav(`/models/${m.id}`)}
                className={`card p-4 flex flex-col transition-all duration-200 hover:border-surface-border-hover hover:shadow-card-hover hover:-translate-y-0.5 cursor-pointer animate-slide-up ${stale ? 'opacity-60' : ''}`}
                style={{ animationDelay: `${Math.min(i * 30, 300)}ms` }}>
                <div className="flex items-start justify-between mb-2 gap-2">
                  <div className="flex items-center gap-2 min-w-0">
                    <span className="w-7 h-7 rounded-md flex items-center justify-center text-[12px] bg-surface-card-hover shrink-0">
                      {vendorIcon(m.id)}
                    </span>
                    <div className="min-w-0">
                      <div className="text-[12.5px] font-semibold text-gray-200 font-mono leading-tight truncate" title={m.id}>
                        {m.id}
                      </div>
                      <div className="text-[10px] text-surface-muted truncate">
                        {m.owned_by || '—'}
                        {m.status === 'gone' && <span className="text-amber-400/80"> · 已消失</span>}
                      </div>
                    </div>
                  </div>
                  <Badge variant={m.status === 'gone' ? 'warn' : cm.variant} className="shrink-0">
                    <span>{cm.icon}</span>{m.status === 'gone' ? '已消失' : cm.label}
                  </Badge>
                </div>

                <div className="text-[10.5px] text-surface-muted leading-snug min-h-[28px] flex-1">
                  {m.description || '—'}
                </div>

                {/* 可用度评分条 */}
                <div className="mt-3 flex items-center justify-between">
                  <div className="text-[10px] text-surface-muted">可用度</div>
                  <div className="flex items-center gap-1.5">
                    {probing ? (
                      <Badge variant={m.probe_ok ? 'success' : 'danger'}>
                        {m.probe_ok ? '● 可用' : '○ 不可用'}
                        {m.probe_latency_ms > 0 && ` ${m.probe_latency_ms}ms`}
                      </Badge>
                    ) : (
                      <Badge variant="default">未探活</Badge>
                    )}
                    <Badge variant={av.variant}>★ {av.label}</Badge>
                  </div>
                </div>

                <div className="mt-2 grid grid-cols-4 gap-2 text-[10.5px]">
                  <div>
                    <div className="text-surface-muted">参数量</div>
                    <div className="text-gray-300 font-semibold">{m.param_count || '—'}</div>
                  </div>
                  <div>
                    <div className="text-surface-muted">上下文</div>
                    <div className="text-gray-300 font-semibold">{fmtCtx(m.context_length)}</div>
                  </div>
                  <div>
                    <div className="text-surface-muted">请求/h</div>
                    <div className="text-gray-300 font-semibold">{m.request_count || 0}</div>
                  </div>
                  <div>
                    <div className="text-surface-muted">成功率</div>
                    <div className={`font-semibold ${rate >= 95 ? 'text-nv-green' : rate >= 80 ? 'text-amber-400' : rate > 0 ? 'text-red-400' : 'text-surface-muted'}`}>
                      {rate > 0 ? `${rate.toFixed(1)}%` : '—'}
                    </div>
                  </div>
                </div>

                {m.avg_latency_ms > 0 && (
                  <div className="mt-1.5 text-[10px] text-surface-muted">
                    平均延迟 {m.avg_latency_ms}ms{m.error_count > 0 && ` · 错误 ${m.error_count}`}
                  </div>
                )}

                <div className="mt-3 pt-2 border-t border-surface-border flex items-center justify-between">
                  {stale ? (
                    <span className="text-[10.5px] text-amber-400">⚠ 已下线，请求自动推荐替代</span>
                  ) : (
                    <span className="text-[10.5px] text-surface-muted">active</span>
                  )}
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={(e) => { e.stopPropagation(); testOne(m.id) }}
                    disabled={testingId === m.id || stale}
                  >
                    <FlaskConical className="w-3.5 h-3.5" />
                    {testingId === m.id ? '测试中...' : 'Test'}
                  </Button>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </>
  )
}
