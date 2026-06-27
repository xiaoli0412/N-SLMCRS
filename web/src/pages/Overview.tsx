import { useQuery } from '@tanstack/react-query'
import { motion } from 'framer-motion'
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { api } from '@/api'
import { Card, CardHeader, CardTitle, CardContent, Skeleton } from '@/components/ui'
import { fmtNum, fmtPct } from '@/lib/utils'

export default function Overview() {
  const mQ = useQuery({ queryKey: ['metrics', '1h'], queryFn: () => api.getMetrics('1h') })
  const tsQ = useQuery({ queryKey: ['timeseries', '1h'], queryFn: () => api.getTimeSeries('1h', 60) })
  const m = mQ.data
  const data = (tsQ.data?.data ?? []).map((p) => ({ ...p, ts: p.ts * 1000 }))

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">概览</h1>
        <p className="mt-1 text-sm text-muted-foreground">近 1 小时网关实时指标</p>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Kpi label="总请求" value={m ? fmtNum(m.TotalRequests) : '—'} loading={mQ.isLoading} />
        <Kpi label="成功率" value={m ? fmtPct(m.SuccessRate) : '—'} loading={mQ.isLoading} accent="text-success" />
        <Kpi label="平均延迟" value={m ? m.AvgLatencyMS + 'ms' : '—'} loading={mQ.isLoading} />
        <Kpi label="当前 RPM" value={m ? fmtNum(m.CurrentRPM) : '—'} loading={mQ.isLoading} accent="text-primary" />
      </div>

      <Card>
        <CardHeader><CardTitle>请求量趋势（1h）</CardTitle></CardHeader>
        <CardContent>
          {tsQ.isLoading ? <Skeleton className="h-48" /> : (
            <div className="h-48">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={data}>
                  <defs><linearGradient id="og" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="hsl(var(--primary))" stopOpacity={0.4} /><stop offset="100%" stopColor="hsl(var(--primary))" stopOpacity={0} /></linearGradient></defs>
                  <XAxis dataKey="ts" tickFormatter={(v) => new Date(v).toLocaleTimeString()} fontSize={10} stroke="hsl(var(--muted-foreground))" />
                  <YAxis fontSize={10} stroke="hsl(var(--muted-foreground))" />
                  <Tooltip labelFormatter={(v) => new Date(Number(v)).toLocaleString()} contentStyle={{ background: 'hsl(var(--popover))', border: '1px solid hsl(var(--border))', borderRadius: 6, fontSize: 12 }} />
                  <Area dataKey="count" stroke="hsl(var(--primary))" fill="url(#og)" />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          )}
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <MiniStat label="错误请求" value={m ? fmtNum(m.ErrorRequests) : '—'} />
        <MiniStat label="限流" value={m ? fmtNum(m.RateLimited) : '—'} />
        <MiniStat label="总 Token" value={m ? fmtNum(m.TotalTokens) : '—'} />
      </div>
    </div>
  )
}

function Kpi({ label, value, loading, accent }: { label: string; value: string; loading?: boolean; accent?: string }) {
  return (
    <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }}>
      <Card className="relative overflow-hidden">
        <CardContent className="p-4">
          {loading ? <Skeleton className="h-7 w-16" /> : <div className={`text-2xl font-bold ${accent || ''}`}>{value}</div>}
          <div className="mt-1 text-xs text-muted-foreground">{label}</div>
        </CardContent>
      </Card>
    </motion.div>
  )
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <Card><CardContent className="flex items-center justify-between p-4">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="text-lg font-semibold">{value}</span>
    </CardContent></Card>
  )
}
