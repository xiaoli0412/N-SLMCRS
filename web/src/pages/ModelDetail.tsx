import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { ArrowLeft, FlaskConical, Activity, FileText, Gauge } from 'lucide-react'
import {
  AreaChart, Area, LineChart, Line, BarChart, Bar,
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell,
} from 'recharts'
import { api, ModelView, TimeSeriesPoint, KeyHealthEntry, ProbeResult } from '../api'
import { PageHeader, Card, Spinner, EmptyState, Badge, StatusBadge, Tabs, Progress, Skeleton, KpiCard } from '../components/ui'

const tipStyle = { background: '#131316', border: '1px solid #262629', borderRadius: 8, fontSize: 12 }
const WINDOWS = [
  { key: '1h', label: '1 小时' },
  { key: '6h', label: '6 小时' },
  { key: '24h', label: '24 小时' },
]

export default function ModelDetail() {
  const { id } = useParams<{ id: string }>()
  const nav = useNavigate()
  const [model, setModel] = useState<ModelView | null>(null)
  const [tab, setTab] = useState('overview')
  const [win, setWin] = useState('1h')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!id) return
    api.getModelDetail(id).then(setModel).catch(() => setModel(null)).finally(() => setLoading(false))
  }, [id])

  if (loading) return <><PageHeader title={id || ''} en="Model Detail" /><Spinner /></>
  if (!model) return <EmptyState text="未找到该模型" />

  const gone = model.status === 'gone'
  const capLabel = model.capability || 'chat'

  return (
    <>
      <div className="flex items-center gap-2 mb-4">
        <button onClick={() => nav('/models')} className="btn-ghost text-[12px] flex items-center gap-1">
          <ArrowLeft className="w-3.5 h-3.5" /> 返回广场
        </button>
      </div>

      <PageHeader title={model.id} en="Model Detail" subtitle={`${model.owned_by || '—'} · ${capLabel}${gone ? ' · 已从上游消失' : ''}`} />

      {/* 模型头部：状态 + 可用度 + 关键规格 */}
      <Card className="mb-4 !p-5 animate-slide-up">
        <div className="flex items-start justify-between gap-4 flex-wrap">
          <div className="flex items-center gap-3">
            <Badge variant={gone ? 'warn' : 'success'}>
              {gone ? '已消失' : model.is_active ? '可用' : '已下线'}
            </Badge>
            <span className="text-[12px] text-surface-muted">{model.description || '—'}</span>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant={model.probe_ok ? 'success' : model.last_probe_ts ? 'danger' : 'default'}>
              {model.probe_ok ? `探活 ${model.probe_latency_ms}ms` : model.last_probe_ts ? '探活失败' : '未探活'}
            </Badge>
            <Badge variant="info">可用度 {model.availability_score.toFixed(0)}</Badge>
          </div>
        </div>
      </Card>

      {/* Tabs */}
      <Tabs
        className="mb-4"
        active={tab}
        onChange={setTab}
        tabs={[
          { id: 'overview', label: '概览' },
          { id: 'health', label: '健康' },
          { id: 'probes', label: '探活' },
          { id: 'spec', label: '参数说明' },
        ]}
      />

      {tab === 'overview' && <OverviewTab model={model} />}
      {tab === 'health' && <HealthTab modelId={model.id} window={win} setWindow={setWin} />}
      {tab === 'probes' && <ProbesTab modelId={model.id} />}
      {tab === 'spec' && <SpecTab model={model} />}
    </>
  )
}

// ─── 概览 tab：四宫格 KPI + 元数据 ───────────────────────────────────────
function OverviewTab({ model }: { model: ModelView }) {
  return (
    <div className="animate-fade-in">
      <div className="grid grid-cols-4 gap-3.5 mb-4">
        <KpiCard label="请求量(1h)" value={model.request_count} accent />
        <KpiCard label="成功率" value={model.success_rate.toFixed(1)} unit="%" />
        <KpiCard label="平均延迟" value={model.avg_latency_ms} unit="ms" />
        <KpiCard label="错误数(1h)" value={model.error_count} />
      </div>
      <div className="grid grid-cols-2 gap-3.5">
        <Card>
          <div className="text-[13px] font-semibold text-gray-200 mb-3">模型元数据</div>
          <dl className="grid grid-cols-2 gap-y-2.5 text-[12px]">
            <SpecRow k="模型 ID" v={model.id} mono />
            <SpecRow k="厂商" v={model.owned_by || '—'} />
            <SpecRow k="能力" v={model.capability || '—'} />
            <SpecRow k="参数量" v={model.param_count || '—'} />
            <SpecRow k="上下文长度" v={model.context_length ? model.context_length.toLocaleString() : '—'} />
            <SpecRow k="根模型" v={model.root || '—'} />
            <SpecRow k="最后同步" v={model.synced_at ? new Date(model.synced_at * 1000).toLocaleString('zh-CN') : '—'} />
            <SpecRow k="最后活跃" v={model.last_seen_active_at ? new Date(model.last_seen_active_at * 1000).toLocaleString('zh-CN') : '—'} />
          </dl>
        </Card>
        <Card>
          <div className="text-[13px] font-semibold text-gray-200 mb-3">说明</div>
          <p className="text-[12.5px] text-surface-muted leading-relaxed">{model.description || '暂无模型说明。可在「参数说明」tab 查看来自开放仓库的规格。'}</p>
        </Card>
      </div>
    </div>
  )
}

// ─── 健康 tab：吸收 Operations 的时序图/错误分类/按 key 健康表（?model= 过滤）──
function HealthTab({ modelId, window, setWindow }: { modelId: string; window: string; setWindow: (w: string) => void }) {
  const [ts, setTs] = useState<TimeSeriesPoint[]>([])
  const [health, setHealth] = useState<KeyHealthEntry[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    const bucket = window === '1h' ? 15 : window === '24h' ? 180 : 60
    Promise.all([
      api.getModelTimeSeries(modelId, window, bucket),
      api.getKeyHealth(window, modelId),
    ]).then(([t, h]) => {
      setTs(t.data || [])
      setHealth(h.data || [])
    }).finally(() => setLoading(false))
  }, [modelId, window])

  const chartData = ts.map((p) => ({
    ts: new Date(p.ts * 1000).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
    req: p.count, ok: p.ok_count, err: p.count - p.ok_count, rate: Number(p.rate.toFixed(1)), tokens: p.tokens,
  }))

  if (loading) return <div className="grid grid-cols-2 gap-3.5"><Skeleton className="h-[210px]" /><Skeleton className="h-[210px]" /></div>

  return (
    <div className="animate-fade-in space-y-4">
      <div className="flex justify-end">
        <div className="flex gap-1.5 p-1 rounded-lg bg-surface-card-hover border border-surface-border">
          {WINDOWS.map((w) => (
            <button key={w.key} onClick={() => setWindow(w.key)}
              className={`px-3 py-1.5 rounded-md text-[12px] font-medium transition-all ${window === w.key ? 'bg-nv-green text-black' : 'text-gray-400 hover:text-gray-200'}`}>
              {w.label}
            </button>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3.5">
        <Card>
          <div className="text-[13px] font-semibold text-gray-200 mb-3">请求量与成功趋势</div>
          <div className="h-[210px]">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData}>
                <defs>
                  <linearGradient id="okG2" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#76b900" stopOpacity={0.35} />
                    <stop offset="100%" stopColor="#76b900" stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="errG2" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#f06060" stopOpacity={0.25} />
                    <stop offset="100%" stopColor="#f06060" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.04)" />
                <XAxis dataKey="ts" tick={{ fill: '#8a8a93', fontSize: 10 }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fill: '#8a8a93', fontSize: 10 }} axisLine={false} tickLine={false} />
                <Tooltip contentStyle={tipStyle} />
                <Area type="monotone" dataKey="ok" name="成功" stroke="#76b900" strokeWidth={2} fill="url(#okG2)" />
                <Area type="monotone" dataKey="err" name="失败" stroke="#f06060" strokeWidth={1.5} fill="url(#errG2)" />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </Card>
        <Card>
          <div className="text-[13px] font-semibold text-gray-200 mb-3">实时吞吐 RPM</div>
          <div className="h-[210px]">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData}>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.04)" />
                <XAxis dataKey="ts" tick={{ fill: '#8a8a93', fontSize: 10 }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fill: '#8a8a93', fontSize: 10 }} axisLine={false} tickLine={false} />
                <Tooltip contentStyle={tipStyle} />
                <Line type="monotone" dataKey="rate" name="RPM" stroke="#76b900" strokeWidth={2.5} dot={false} isAnimationActive />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </Card>
      </div>

      <Card>
        <div className="text-[13px] font-semibold text-gray-200 mb-3">Token 消耗趋势</div>
        <div className="h-[170px]">
          <ResponsiveContainer width="100%" height="100%">
            <BarChart data={chartData}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.04)" />
              <XAxis dataKey="ts" tick={{ fill: '#8a8a93', fontSize: 10 }} axisLine={false} tickLine={false} />
              <YAxis tick={{ fill: '#8a8a93', fontSize: 10 }} axisLine={false} tickLine={false} />
              <Tooltip contentStyle={tipStyle} />
              <Bar dataKey="tokens" name="Tokens" radius={[3, 3, 0, 0]}>
                {chartData.map((_, i) => <Cell key={i} fill="#76b900" fillOpacity={0.5 + (i % 5) * 0.1} />)}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>
      </Card>

      {/* 按 key 健康表（该模型维度，从 Operations 迁移） */}
      <Card>
        <div className="text-[13px] font-semibold text-gray-200 mb-3">上游密钥健康度（本模型）</div>
        {health.length === 0 ? <EmptyState text="暂无该模型的密钥健康数据" /> : (
          <div className="overflow-x-auto">
            <table className="w-full text-[12.5px]">
              <thead>
                <tr className="text-surface-muted text-[10.5px] uppercase tracking-wider border-b border-surface-border">
                  <th className="text-left px-3 py-2 font-semibold">密钥</th>
                  <th className="text-left px-3 py-2 font-semibold">状态</th>
                  <th className="text-right px-3 py-2 font-semibold">请求数</th>
                  <th className="text-right px-3 py-2 font-semibold">成功率</th>
                  <th className="text-right px-3 py-2 font-semibold">平均延迟</th>
                  <th className="text-right px-3 py-2 font-semibold">连续失败</th>
                  <th className="text-left px-3 py-2 font-semibold">健康度</th>
                </tr>
              </thead>
              <tbody>
                {health.map((h, i) => (
                  <tr key={i} className="border-b border-surface-border/60 hover:bg-surface-card-hover">
                    <td className="px-3 py-2.5 font-mono text-[11.5px] text-gray-300">{h.key_mask}</td>
                    <td className="px-3 py-2.5"><StatusBadge status={h.status} /></td>
                    <td className="px-3 py-2.5 text-right text-gray-300">{h.total_requests}</td>
                    <td className="px-3 py-2.5 text-right">
                      <span className={h.success_rate >= 95 ? 'text-nv-green' : h.success_rate >= 80 ? 'text-amber-400' : 'text-red-400'}>
                        {h.success_rate.toFixed(1)}%
                      </span>
                    </td>
                    <td className="px-3 py-2.5 text-right text-gray-400">{h.avg_latency_ms?.toFixed(0) || 0}ms</td>
                    <td className="px-3 py-2.5 text-right">
                      <span className={h.consecutive_fail > 0 ? 'text-red-400' : 'text-surface-muted'}>{h.consecutive_fail}</span>
                    </td>
                    <td className="px-3 py-2.5">
                      <div className="flex items-center gap-2">
                        <Progress value={h.success_rate} className="w-20"
                          indicatorClassName={h.success_rate >= 95 ? 'bg-nv-green' : h.success_rate >= 80 ? 'bg-amber-400' : 'bg-red-400'} />
                        <span className="text-[11px] text-surface-muted">{h.ewma_rate?.toFixed(0) || 0}%</span>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  )
}

// ─── 探活 tab：探活历史趋势 ─────────────────────────────────────────────
function ProbesTab({ modelId }: { modelId: string }) {
  const [data, setData] = useState<{ history: ProbeResult[]; latest: ProbeResult | null } | null>(null)
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    api.getModelProbes(modelId, 100).then(setData).finally(() => setLoading(false))
  }, [modelId])
  if (loading) return <Skeleton className="h-[210px]" />
  const history = data?.history || []
  if (history.length === 0) return <EmptyState text="暂无探活历史" />
  const chart = history.map((p) => ({
    ts: new Date(p.ts * 1000).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
    lat: p.latency_ms, ok: p.ok ? 1 : 0,
  }))
  return (
    <Card className="animate-fade-in">
      <div className="text-[13px] font-semibold text-gray-200 mb-3">探活延迟趋势</div>
      <div className="h-[210px]">
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={chart}>
            <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.04)" />
            <XAxis dataKey="ts" tick={{ fill: '#8a8a93', fontSize: 10 }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fill: '#8a8a93', fontSize: 10 }} axisLine={false} tickLine={false} />
            <Tooltip contentStyle={tipStyle} />
            <Bar dataKey="lat" name="延迟(ms)" radius={[3, 3, 0, 0]}>
              {chart.map((c, i) => <Cell key={i} fill={c.ok ? '#76b900' : '#f06060'} fillOpacity={0.6} />)}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </div>
    </Card>
  )
}

// ─── 参数说明 tab：开放仓库富化规格 ─────────────────────────────────────
function SpecTab({ model }: { model: ModelView }) {
  return (
    <Card className="animate-fade-in">
      <div className="text-[13px] font-semibold text-gray-200 mb-3 flex items-center gap-1.5">
        <FileText className="w-3.5 h-3.5 text-nv-green" /> 外部规格（开放仓库富化）
      </div>
      <dl className="grid grid-cols-2 gap-y-3 text-[12.5px]">
        <SpecRow k="最大输出 Token" v={model.max_tokens ? model.max_tokens.toLocaleString() : '—'} />
        <SpecRow k="许可证" v={model.license || '—'} />
        <SpecRow k="输入定价" v={model.pricing_in || '—'} />
        <SpecRow k="输出定价" v={model.pricing_out || '—'} />
        <SpecRow k="输入模态" v={model.input_modalities?.length ? model.input_modalities.join(' / ') : '—'} />
        <SpecRow k="发布日期" v={model.release_date || '—'} />
      </dl>
      {model.card_url && (
        <a href={model.card_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 mt-4 text-[12px] text-nv-green hover:underline">
          <Gauge className="w-3.5 h-3.5" /> 查看模型卡 →
        </a>
      )}
      <p className="text-[11px] text-surface-muted mt-4 leading-relaxed">
        规格数据来自开放模型注册表（OpenRouter/LiteLLM）周期同步；未命中时显示「—」，可在模型广场「立即同步」后刷新。
      </p>
    </Card>
  )
}

function SpecRow({ k, v, mono }: { k: string; v: string; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between gap-3 border-b border-surface-border/40 pb-2">
      <dt className="text-surface-muted">{k}</dt>
      <dd className={`text-gray-200 text-right truncate ${mono ? 'font-mono text-[11.5px]' : ''}`} title={v}>{v}</dd>
    </div>
  )
}
