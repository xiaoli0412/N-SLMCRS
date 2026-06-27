import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts'
import { api, Metrics } from '../api'
import { PageHeader, KpiCard, Spinner } from '../components/ui'

export default function Overview() {
  const { t } = useTranslation()
  const [m, setM] = useState<Metrics | null>(null)
  const [ts, setTs] = useState<any[]>([])
  const [loading, setLoading] = useState(true)

  const load = async () => {
    try {
      const [metrics, series] = await Promise.all([api.getMetrics('1h'), api.getTimeSeries('1h', 60)])
      setM(metrics)
      setTs(series.data.map((p: any) => ({ ts: new Date(p.TS * 1000).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }), count: p.Count, rate: p.Rate })))
    } catch { /* 忽略 */ }
    setLoading(false)
  }

  useEffect(() => {
    load()
    const id = setInterval(load, 5000)
    return () => clearInterval(id)
  }, [])

  if (loading || !m) return <><PageHeader title={t('nav.overview')} en="Overview" /><Spinner /></>

  return (
    <>
      <PageHeader title={t('nav.overview')} en="Overview" subtitle={`${t('common.running')} · ${t('common.success_rate')} ${m.SuccessRate.toFixed(1)}%`} />

      <div className="grid grid-cols-4 gap-3.5 mb-5">
        <KpiCard label={t('common.success_rate')} value={m.SuccessRate.toFixed(1)} unit="%" accent trend={`${m.SuccessRequests}/${m.TotalRequests}`} trendUp />
        <KpiCard label={t('common.throughput')} value={m.CurrentRPM} unit="/ RPM" trend={`峰值 ${m.PeakRPM || m.CurrentRPM}`} trendUp />
        <KpiCard label={t('common.upstream_keys')} value="—" />
        <KpiCard label={t('common.tokens_today')} value={(m.TotalTokens / 1000000).toFixed(2)} unit="M" />
      </div>

      <div className="grid grid-cols-3 gap-3.5">
        <div className="col-span-2 card p-5">
          <div className="flex items-center justify-between mb-3">
            <div className="text-[13px] font-semibold text-gray-200">请求量与成功率趋势 <span className="text-surface-muted text-[11px] font-normal">/ Traffic</span></div>
            <div className="flex gap-1">
              <span className="text-[11px] px-2 py-0.5 rounded bg-nv-green/10 text-nv-green border border-nv-green/20">1h</span>
            </div>
          </div>
          <div className="h-[200px]">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={ts}>
                <defs>
                  <linearGradient id="g" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#76b900" stopOpacity={0.3} />
                    <stop offset="100%" stopColor="#76b900" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.04)" />
                <XAxis dataKey="ts" tick={{ fill: '#8a8a93', fontSize: 10 }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fill: '#8a8a93', fontSize: 10 }} axisLine={false} tickLine={false} />
                <Tooltip contentStyle={{ background: '#131316', border: '1px solid #262629', borderRadius: 8, fontSize: 12 }} />
                <Area type="monotone" dataKey="count" stroke="#76b900" strokeWidth={2} fill="url(#g)" />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>

        <div className="card p-5">
          <div className="text-[13px] font-semibold text-gray-200 mb-3">错误分布 <span className="text-surface-muted text-[11px] font-normal">/ Errors</span></div>
          <div className="space-y-2.5">
            <ErrorBar label="成功" value={m.SuccessRequests} total={m.TotalRequests} color="#76b900" />
            <ErrorBar label="错误" value={m.ErrorRequests} total={m.TotalRequests} color="#f06060" />
            <ErrorBar label="限流(429)" value={m.RateLimited} total={m.TotalRequests} color="#f5c542" />
            <ErrorBar label="超时" value={m.Timeouts} total={m.TotalRequests} color="#a855f7" />
          </div>
          <div className="mt-4 pt-3 border-t border-surface-border text-[11px] text-surface-muted">
            平均延迟 <span className="text-gray-300 font-semibold">{m.AvgLatencyMS.toFixed(0)}ms</span>
          </div>
        </div>
      </div>
    </>
  )
}

function ErrorBar({ label, value, total, color }: { label: string; value: number; total: number; color: string }) {
  const pct = total > 0 ? (value / total) * 100 : 0
  return (
    <div>
      <div className="flex justify-between text-[11px] mb-1">
        <span className="text-surface-muted">{label}</span>
        <span className="text-gray-300">{value}</span>
      </div>
      <div className="h-1.5 bg-surface-card-hover rounded-full overflow-hidden">
        <div className="h-full rounded-full transition-all duration-500" style={{ width: `${pct}%`, background: color }} />
      </div>
    </div>
  )
}
