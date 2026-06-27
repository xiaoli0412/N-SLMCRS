import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { api } from '@/api'
import { Card, CardContent, Input, Badge, EmptyState } from '@/components/ui'
import { timeAgo } from '@/lib/utils'

export default function Logs() {
  const [q, setQ] = useState('')
  const [level, setLevel] = useState('')
  const [source, setSource] = useState('')
  const params = new URLSearchParams()
  if (q) params.set('trace_id', q)
  if (level) params.set('level', level)
  if (source) params.set('source', source)
  const logsQ = useQuery({ queryKey: ['logs', q, level, source], queryFn: () => api.getLogs('?' + params.toString()), refetchInterval: 10_000 })
  const logs = logsQ.data?.data ?? []

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">日志中心</h1>
        <p className="mt-1 text-sm text-muted-foreground">结构化应用日志（app_logs，slog 扇出）</p>
      </div>

      <Card>
        <CardContent className="p-3">
          <div className="flex flex-wrap gap-2">
            <div className="relative min-w-48 flex-1">
              <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input className="pl-8" placeholder="trace_id" value={q} onChange={(e) => setQ(e.target.value)} />
            </div>
            <select className="h-9 rounded-md border bg-transparent px-2 text-sm" value={level} onChange={(e) => setLevel(e.target.value)}>
              <option value="">all levels</option>
              <option value="debug">debug</option><option value="info">info</option>
              <option value="warn">warn</option><option value="error">error</option>
            </select>
            <select className="h-9 rounded-md border bg-transparent px-2 text-sm" value={source} onChange={(e) => setSource(e.target.value)}>
              <option value="">all sources</option>
              <option value="entry">entry</option><option value="scheduler">scheduler</option>
              <option value="upstream">upstream</option><option value="server">server</option>
              <option value="modelhealth">modelhealth</option>
            </select>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-0">
          {logs.length === 0 ? <EmptyState title="暂无日志" /> : (
            <div className="max-h-[60vh] overflow-auto font-mono text-xs">
              {logs.map((l, i) => (
                <div key={i} className="flex items-start gap-2 border-b px-3 py-1.5 hover:bg-muted/50">
                  <span className="shrink-0 text-muted-foreground">{timeAgo(l.ts)}</span>
                  <LevelBadge level={l.level} />
                  <span className="shrink-0 text-muted-foreground">{l.source}</span>
                  {l.trace_id && <span className="shrink-0 text-primary/70">{l.trace_id.slice(0, 8)}</span>}
                  <span className="min-w-0 flex-1 break-all">{l.message}</span>
                  {l.context && <span className="shrink-0 text-muted-foreground/70">{l.context}</span>}
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function LevelBadge({ level }: { level: string }) {
  const v = level === 'error' ? 'destructive' : level === 'warn' ? 'warning' : level === 'debug' ? 'secondary' : 'default'
  return <Badge variant={v as any} className="shrink-0 uppercase">{level}</Badge>
}
