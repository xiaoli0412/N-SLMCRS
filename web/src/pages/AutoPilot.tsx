import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Bot, Check, X } from 'lucide-react'
import { toast } from 'sonner'
import { api, type AutoPilotMode, type AutoPilotEngine } from '@/api'
import { Card, CardHeader, CardTitle, CardContent, Button, Badge, EmptyState } from '@/components/ui'
import { fmtNum, timeAgo } from '@/lib/utils'

const MODES: AutoPilotMode[] = ['manual', 'assisted', 'fullauto']
const ENGINES: AutoPilotEngine[] = ['adaptive', 'predict', 'llm']

export default function AutoPilot() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['ap-state'], queryFn: api.getAutopilotState, refetchInterval: 10_000 })
  const modeM = useMutation({ mutationFn: (m: AutoPilotMode) => api.setAutopilotMode(m), onSuccess: () => qc.invalidateQueries({ queryKey: ['ap-state'] }) })
  const engM = useMutation({ mutationFn: (e: AutoPilotEngine) => api.setAutopilotEngine(e), onSuccess: () => qc.invalidateQueries({ queryKey: ['ap-state'] }) })
  const s = q.data
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Auto-Pilot</h1>
        <p className="mt-1 text-sm text-muted-foreground">智能调度自动调参</p>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Stat label="当前并发" value={s ? String(s.RuntimeConcurrency) : '—'} accent="text-primary" />
        <Stat label="可用密钥" value={s ? String(s.AvailableKeyCount) : '—'} />
        <Stat label="在途请求" value={s ? String(s.InflightRequests) : '—'} />
        <Stat label="干预次数" value={s ? fmtNum(s.Interventions) : '—'} />
      </div>

      <Card>
        <CardHeader><CardTitle>模式与引擎</CardTitle></CardHeader>
        <CardContent className="space-y-4">
          <div>
            <div className="mb-2 text-xs text-muted-foreground">模式</div>
            <div className="flex gap-2">
              {MODES.map((m) => (
                <Button key={m} size="sm" variant={s?.Mode === m ? 'default' : 'outline'} onClick={() => modeM.mutate(m)}>{m}</Button>
              ))}
            </div>
          </div>
          <div>
            <div className="mb-2 text-xs text-muted-foreground">引擎</div>
            <div className="flex gap-2">
              {ENGINES.map((e) => (
                <Button key={e} size="sm" variant={s?.Engine === e ? 'default' : 'outline'} onClick={() => engM.mutate(e)}>{e}</Button>
              ))}
            </div>
            <p className="mt-2 text-xs text-muted-foreground">LLM 后端：{s?.LLMBackendMode || '—'} · 档位 {s?.ClientConcurrencyTier || '—'}</p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>最近事件</CardTitle></CardHeader>
        <CardContent>
          {(s?.RecentEvents ?? []).length === 0 ? <EmptyState title="暂无事件" /> : (
            <div className="space-y-1">
              {s!.RecentEvents.map((e, i) => (
                <div key={i} className="flex items-start gap-2 rounded-md border p-2 text-xs">
                  {e.Applied ? <Check className="mt-0.5 h-3.5 w-3.5 text-success" /> : <X className="mt-0.5 h-3.5 w-3.5 text-destructive" />}
                  <div className="flex-1">
                    <div className="flex items-center gap-2">
                      <Badge variant="outline">{e.Kind}</Badge>
                      <span className="text-muted-foreground">{timeAgo(e.TS)}</span>
                    </div>
                    <p className="mt-0.5">{e.Detail}</p>
                    <p className="text-muted-foreground">{e.Reason}</p>
                  </div>
                </div>
              ))}
            </div>
          )}
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
