import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { api } from '@/api'
import { Card, CardHeader, CardTitle, CardContent, Button } from '@/components/ui'
import { fmtNum, fmtPct } from '@/lib/utils'

const WINDOWS = ['1h', '6h', '24h', '7d'] as const

export default function Operations() {
  const [win, setWin] = useState<typeof WINDOWS[number]>('1h')
  const mQ = useQuery({ queryKey: ['metrics', win], queryFn: () => api.getMetrics(win) })
  const tsQ = useQuery({ queryKey: ['timeseries', win], queryFn: () => api.getTimeSeries(win, 60) })
  const khQ = useQuery({ queryKey: ['key-health', win], queryFn: () => api.getKeyHealth(win) })
  const m = mQ.data
  const data = (tsQ.data?.data ?? []).map((p) => ({ ...p, ts: p.ts * 1000 }))

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">调度运营</h1>
          <p className="mt-1 text-sm text-muted-foreground">流量、密钥健康与限流</p>
        </div>
        <div className="flex gap-1">
          {WINDOWS.map((w) => (
            <Button key={w} size="sm" variant={win === w ? 'default' : 'outline'} onClick={() => setWin(w)}>{w}</Button>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Stat label="总请求" value={m ? fmtNum(m.TotalRequests) : '—'} />
        <Stat label="成功率" value={m ? fmtPct(m.SuccessRate) : '—'} accent="text-success" />
        <Stat label="平均延迟" value={m ? m.AvgLatencyMS + 'ms' : '—'} />
        <Stat label="峰值 RPM" value={m ? fmtNum(m.PeakRPM) : '—'} accent="text-primary" />
      </div>

      <Card>
        <CardHeader><CardTitle>请求时序</CardTitle></CardHeader>
        <CardContent>
          <div className="h-56">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={data}>
                <defs><linearGradient id="opg" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="hsl(var(--primary))" stopOpacity={0.4} /><stop offset="100%" stopColor="hsl(var(--primary))" stopOpacity={0} /></linearGradient></defs>
                <XAxis dataKey="ts" tickFormatter={(v) => new Date(v).toLocaleString()} fontSize={10} stroke="hsl(var(--muted-foreground))" />
                <YAxis fontSize={10} stroke="hsl(var(--muted-foreground))" />
                <Tooltip labelFormatter={(v) => new Date(Number(v)).toLocaleString()} contentStyle={{ background: 'hsl(var(--popover))', border: '1px solid hsl(var(--border))', borderRadius: 6, fontSize: 12 }} />
                <Area dataKey="count" stroke="hsl(var(--primary))" fill="url(#opg)" />
                <Area dataKey="ok_count" stroke="hsl(var(--success))" fill="transparent" />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>上游密钥健康</CardTitle></CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead><tr className="text-left text-xs text-muted-foreground">
                <th className="pb-2">密钥</th><th className="pb-2">状态</th><th className="pb-2">请求数</th>
                <th className="pb-2">成功率</th><th className="pb-2">延迟</th><th className="pb-2">连续失败</th>
              </tr></thead>
              <tbody>
                {(khQ.data?.data ?? []).map((k) => (
                  <tr key={k.key_mask} className="border-t">
                    <td className="py-2 font-mono">{k.key_mask}</td>
                    <td className="py-2">{k.status}</td>
                    <td className="py-2">{fmtNum(k.total_requests)}</td>
                    <td className="py-2">{fmtPct(k.success_rate)}</td>
                    <td className="py-2">{k.avg_latency_ms}ms</td>
                    <td className="py-2">{k.consecutive_fail}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function Stat({ label, value, accent }: { label: string; value: string; accent?: string }) {
  return (
    <Card><CardContent className="p-4">
      <div className={`text-2xl font-bold ${accent || ''}`}>{value}</div>
      <div className="mt-1 text-xs text-muted-foreground">{label}</div>
    </CardContent></Card>
  )
}
