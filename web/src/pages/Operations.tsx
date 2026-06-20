import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  LineChart, Line, AreaChart, Area, BarChart, Bar,
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell,
} from 'recharts'
import { api, Metrics, KeyHealth } from '../api'
import { PageHeader, KpiCard, Spinner, EmptyState, StatusBadge } from '../components/ui'

const WINDOWS = [
  { key: '5m', label: '5 分钟', en: '5m' },
  { key: '1h', label: '1 小时', en: '1h' },
  { key: '6h', label: '6 小时', en: '6h' },
  { key: '24h', label: '24 小时', en: '24h' },
]

export default function Operations() {
  const { t } = useTranslation()
  const [win, setWin] = useState('1h')
  const [m, setM] = useState<Metrics | null>(null)
  const [ts, setTs] = useState<any[]>([])
  const [health, setHealth] = useState<KeyHealth[]>([])
  const [loading, setLoading] = useState(true)

  const load = async () => {
    try {
      const [metrics, series, kh] = await Promise.all([
        api.getMetrics(win),
        api.getTimeSeries(win, win === '5m' ? 15 : win === '24h' ? 180 : 60),
        api.getKeyHealth(win),
      ])
      setM(metrics)
      setTs(series.data.map((p: any) => ({
        ts: new Date(p.TS * 1000).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
        req: p.Count,
        ok: p.OkCount,
        err: p.Count - p.OkCount,
        rate: Number(p.Rate.toFixed(1)),
        tokens: p.Tokens,
      })))
      setHealth(kh.data || [])
    } catch { /* 忽略 */ }
    setLoading(false)
  }

  useEffect(() => {
    setLoading(true)
    load()
    const id = setInterval(load, 5000)
    return () => clearInterval(id)
  }, [win])

  if (loading || !m) return <><PageHeader title={t('nav.operations')} en="Operations" /><Spinner /></>

  return (
    <>
      <PageHeader title={t('nav.operations')} en="Operations" subtitle="实时成功率 / 请求量 / Token / 密钥健康度全维度监控" />

      {/* 窗口切换 */}
      <div className="mb-4 flex justify-between">
        <div className="flex gap-1.5 p-1 rounded-lg bg-white/[0.03] border border-white/[0.06]">
          {WINDOWS.map((w) => (
            <button
              key={w.key}
              onClick={() => setWin(w.key)}
              className={`px-3 py-1.5 rounded-md text-[12px] font-medium transition-all ${win === w.key
                ? 'bg-nv-green text-black shadow-nv-glow'
                : 'text-gray-400 hover:text-gray-200'}`}
            >
              {w.label}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-nv-green/5 border border-nv-green/15 text-[11px] text-nv-green">
          <span className="status-dot ok animate-pulse-slow" /> 实时刷新 · 5s
        </div>
      </div>

      {/* KPI */}
      <div className="grid grid-cols-6 gap-3 mb-4">
        <KpiCard label="成功率" value={m.SuccessRate.toFixed(1)} unit="%" accent trend={`${m.SuccessRequests}/${m.TotalRequests}`} trendUp />
        <KpiCard label="总请求" value={m.TotalRequests} />
        <KpiCard label="实时 RPM" value={m.CurrentRPM} trend={`峰值 ${m.PeakRPM || m.CurrentRPM}`} trendUp />
        <KpiCard label="平均延迟" value={m.AvgLatencyMS.toFixed(0)} unit="ms" />
        <KpiCard label="Token 用量" value={(m.TotalTokens / 1000000).toFixed(2)} unit="M" />
        <KpiCard label="限流 429" value={m.RateLimited} />
      </div>

      {/* 请求量 + 成功率 双图 */}
      <div className="grid grid-cols-2 gap-3.5 mb-4">
        <div className="glass-card p-5">
          <div className="text-[13px] font-semibold text-gray-200 mb-3">
            请求量与成功趋势 <span className="text-gray-600 text-[11px] font-normal">/ Requests</span>
          </div>
          <div className="h-[210px]">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={ts}>
                <defs>
                  <linearGradient id="okG" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#76b900" stopOpacity={0.35} />
                    <stop offset="100%" stopColor="#76b900" stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="errG" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#f06060" stopOpacity={0.25} />
                    <stop offset="100%" stopColor="#f06060" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.04)" />
                <XAxis dataKey="ts" tick={{ fill: '#666', fontSize: 10 }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fill: '#666', fontSize: 10 }} axisLine={false} tickLine={false} />
                <Tooltip contentStyle={tipStyle} />
                <Area type="monotone" dataKey="ok" name="成功" stroke="#76b900" strokeWidth={2} fill="url(#okG)" />
                <Area type="monotone" dataKey="err" name="失败" stroke="#f06060" strokeWidth={1.5} fill="url(#errG)" />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>

        <div className="glass-card p-5">
          <div className="text-[13px] font-semibold text-gray-200 mb-3">
            实时吞吐 RPM <span className="text-gray-600 text-[11px] font-normal">/ Throughput</span>
          </div>
          <div className="h-[210px]">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={ts}>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.04)" />
                <XAxis dataKey="ts" tick={{ fill: '#666', fontSize: 10 }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fill: '#666', fontSize: 10 }} axisLine={false} tickLine={false} />
                <Tooltip contentStyle={tipStyle} />
                <Line type="monotone" dataKey="rate" name="RPM" stroke="#76b900" strokeWidth={2.5} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </div>
      </div>

      {/* Token + 错误分类 */}
      <div className="grid grid-cols-3 gap-3.5 mb-4">
        <div className="col-span-2 glass-card p-5">
          <div className="text-[13px] font-semibold text-gray-200 mb-3">
            Token 消耗趋势 <span className="text-gray-600 text-[11px] font-normal">/ Token Usage</span>
          </div>
          <div className="h-[170px]">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={ts}>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.04)" />
                <XAxis dataKey="ts" tick={{ fill: '#666', fontSize: 10 }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fill: '#666', fontSize: 10 }} axisLine={false} tickLine={false} />
                <Tooltip contentStyle={tipStyle} />
                <Bar dataKey="tokens" name="Tokens" radius={[3, 3, 0, 0]}>
                  {ts.map((_, i) => <Cell key={i} fill="#76b900" fillOpacity={0.5 + (i % 5) * 0.1} />)}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>

        <div className="glass-card p-5">
          <div className="text-[13px] font-semibold text-gray-200 mb-3">错误分类 <span className="text-gray-600 text-[11px] font-normal">/ Breakdown</span></div>
          <div className="space-y-3 pt-1">
            <MiniStat label="成功 Success" value={m.SuccessRequests} color="#76b900" />
            <MiniStat label="业务错误 Error" value={m.ErrorRequests} color="#f06060" />
            <MiniStat label="限流 Rate-Limited" value={m.RateLimited} color="#f5c542" />
            <MiniStat label="超时 Timeout" value={m.Timeouts} color="#a855f7" />
          </div>
        </div>
      </div>

      {/* 密钥健康表 */}
      <div className="glass-card p-5">
        <div className="text-[13px] font-semibold text-gray-200 mb-3">
          上游密钥健康度 <span className="text-gray-600 text-[11px] font-normal">/ Key Health</span>
        </div>
        {health.length === 0 ? <EmptyState text="暂无密钥健康数据，发起请求后将自动采集" /> : (
          <div className="overflow-x-auto">
            <table className="w-full text-[12.5px]">
              <thead>
                <tr className="text-gray-500 text-[10.5px] uppercase tracking-wider border-b border-white/[0.04]">
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
                {health.map((h, i) => {
                  // 防御性 fallback：后端可能返回 0 数据（无请求）
                  const rate = h.success_rate ?? 0
                  const ewma = h.ewma_rate ?? rate
                  const total = h.total_requests ?? 0
                  const consFail = h.consecutive_fail ?? 0
                  return (
                  <tr key={i} className="border-b border-white/[0.03] hover:bg-white/[0.015]">
                    <td className="px-3 py-2.5 font-mono text-[11.5px] text-gray-300">{h.key_mask}</td>
                    <td className="px-3 py-2.5"><StatusBadge status={h.status} /></td>
                    <td className="px-3 py-2.5 text-right text-gray-300">{total}</td>
                    <td className="px-3 py-2.5 text-right">
                      <span className={rate >= 95 ? 'text-nv-green' : rate >= 80 ? 'text-amber-400' : total === 0 ? 'text-gray-600' : 'text-red-400'}>
                        {rate.toFixed(1)}%
                      </span>
                    </td>
                    <td className="px-3 py-2.5 text-right text-gray-400">{h.avg_latency_ms?.toFixed(0) || 0}ms</td>
                    <td className="px-3 py-2.5 text-right">
                      <span className={consFail > 0 ? 'text-red-400' : 'text-gray-600'}>{consFail}</span>
                    </td>
                    <td className="px-3 py-2.5">
                      <div className="flex items-center gap-2">
                        <div className="w-20 h-1.5 bg-white/[0.04] rounded-full overflow-hidden">
                          <div className="h-full rounded-full" style={{
                            width: `${rate}%`,
                            background: rate >= 95 ? '#76b900' : rate >= 80 ? '#f5c542' : '#f06060',
                          }} />
                        </div>
                        <span className="text-[11px] text-gray-600">{ewma.toFixed(0)}%</span>
                      </div>
                    </td>
                  </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </>
  )
}

const tipStyle = { background: '#141414', border: '1px solid #2a2a2a', borderRadius: 8, fontSize: 12 }

function MiniStat({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-2">
        <span className="w-2 h-2 rounded-full" style={{ background: color }} />
        <span className="text-[12px] text-gray-400">{label}</span>
      </div>
      <span className="text-[15px] font-bold text-gray-200">{value}</span>
    </div>
  )
}
