import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { api, type KeyHealthEntry } from '@/api'
import { Card, CardHeader, CardTitle, CardContent, Button, Skeleton, EmptyState, ErrorState, DataTable, type DataTableColumn } from '@/components/ui'
import { fmtNum, fmtPct } from '@/lib/utils'

const WINDOWS = ['1h', '6h', '24h', '7d'] as const

export default function Operations() {
  const [win, setWin] = useState<(typeof WINDOWS)[number]>('1h')
  const mQ = useQuery({ queryKey: ['metrics', win], queryFn: () => api.getMetrics(win) })
  const tsQ = useQuery({ queryKey: ['timeseries', win], queryFn: () => api.getTimeSeries(win, 60) })
  const khQ = useQuery({ queryKey: ['key-health', win], queryFn: () => api.getKeyHealth(win) })
  const m = mQ.data
  const data = (tsQ.data?.data ?? []).map((p) => ({ ...p, ts: p.ts * 1000 }))

  // v0.13：列定义集中，支持点击表头排序（成功率/延迟/请求数/连续失败）。
  const columns: DataTableColumn<KeyHealthEntry>[] = [
    { key: 'mask', header: '密钥', cell: (k) => <span className="font-mono">{k.key_mask}</span> },
    { key: 'status', header: '状态', cell: (k) => <span>{k.status}</span> },
    {
      key: 'req', header: '请求数', sortable: true, sortValue: (k) => k.total_requests,
      cell: (k) => fmtNum(k.total_requests),
    },
    {
      key: 'rate', header: '成功率', sortable: true, sortValue: (k) => k.success_rate,
      cell: (k) => <span className={k.success_rate >= 95 ? 'text-success' : k.success_rate < 80 ? 'text-destructive' : ''}>{fmtPct(k.success_rate)}</span>,
    },
    {
      key: 'lat', header: '延迟', sortable: true, sortValue: (k) => k.avg_latency_ms,
      cell: (k) => `${k.avg_latency_ms}ms`,
    },
    {
      key: 'fail', header: '连续失败', sortable: true, sortValue: (k) => k.consecutive_fail,
      cell: (k) => <span className={k.consecutive_fail > 0 ? 'text-warning' : ''}>{k.consecutive_fail}</span>,
    },
  ]

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
        <Stat label="总请求" loading={mQ.isLoading} value={m ? fmtNum(m.TotalRequests) : undefined} />
        <Stat label="成功率" loading={mQ.isLoading} value={m ? fmtPct(m.SuccessRate) : undefined} accent="text-success" />
        <Stat label="平均延迟" loading={mQ.isLoading} value={m ? `${m.AvgLatencyMS}ms` : undefined} />
        <Stat label="峰值 RPM" loading={mQ.isLoading} value={m ? fmtNum(m.PeakRPM) : undefined} accent="text-primary" />
      </div>

      <Card>
        <CardHeader><CardTitle>请求时序</CardTitle></CardHeader>
        <CardContent>
          {tsQ.isLoading ? (
            <Skeleton className="h-56 w-full" />
          ) : tsQ.isError ? (
            <ErrorState title="时序加载失败" onRetry={() => tsQ.refetch()} />
          ) : (
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
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>上游密钥健康</CardTitle></CardHeader>
        <CardContent>
          <DataTable<KeyHealthEntry>
            columns={columns}
            data={khQ.data?.data ?? []}
            rowKey={(k) => k.key_mask}
            loading={khQ.isLoading}
            error={khQ.isError ? '健康数据加载失败' : undefined}
            onRetry={() => khQ.refetch()}
            empty={<EmptyState title="暂无密钥健康数据" desc="添加上游密钥并产生请求后，此处显示每 Key 成功率与延迟。" />}
          />
        </CardContent>
      </Card>
    </div>
  )
}

function Stat({ label, value, accent, loading }: { label: string; value?: string; accent?: string; loading?: boolean }) {
  return (
    <Card><CardContent className="p-4">
      {loading || value === undefined ? (
        <Skeleton className="h-7 w-20" />
      ) : (
        <div className={`text-2xl font-bold ${accent || ''}`}>{value}</div>
      )}
      <div className="mt-1 text-xs text-muted-foreground">{label}</div>
    </CardContent></Card>
  )
}
