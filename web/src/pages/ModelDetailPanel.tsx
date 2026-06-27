import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { motion } from 'framer-motion'
import { X, Zap, RotateCcw, ExternalLink } from 'lucide-react'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { Area, AreaChart, Bar, BarChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { api } from '@/api'
import { Badge, Button, Separator, Skeleton, Tabs, TabsList, TabsTrigger, TabsContent, EmptyState } from '@/components/ui'
import { cn, fmtNum, fmtCtx, fmtPct, timeAgo } from '@/lib/utils'

export default function ModelDetailPanel({ id }: { id: string }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const detailQ = useQuery({ queryKey: ['model-detail', id], queryFn: () => api.getModelDetail(id) })
  const m = detailQ.data

  const resetM = useMutation({
    mutationFn: () => api.resetModelCircuit(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['model-detail', id] }); toast.success('熔断已复位') },
    onError: () => toast.error('复位失败'),
  })
  const testM = useMutation({
    mutationFn: () => api.testModel(id),
    onSuccess: (r) => r.ok ? toast.success(`探活成功 · ${r.latency_ms}ms`) : toast.error(`探活失败: ${r.error || r.status}`),
    onError: () => toast.error('探活失败'),
  })

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b px-5 py-4">
        <div className="min-w-0">
          <h2 className="truncate font-mono text-sm font-semibold">{id}</h2>
          {m && <p className="text-xs text-muted-foreground">{m.owned_by} · {t(`cap.${m.capability}`)}</p>}
        </div>
        <div className="flex items-center gap-2">
          {m && <CircuitBadge state={m.circuit_state} />}
          <Button variant="ghost" size="icon" onClick={() => qc.invalidateQueries({ queryKey: ['model-detail', id] })}>
            <X className="h-4 w-4" />
          </Button>
        </div>
      </div>

      <div className="flex-1 overflow-auto px-5 py-4">
        {detailQ.isLoading ? <Skeleton className="h-64" /> : !m ? <EmptyState title={t('common.empty')} /> : (
          <Tabs defaultValue="overview">
            <TabsList>
              <TabsTrigger value="overview">{t('common.active')}</TabsTrigger>
              <TabsTrigger value="health">{t('models.health')}</TabsTrigger>
              <TabsTrigger value="probes">{t('models.probes')}</TabsTrigger>
              <TabsTrigger value="params">{t('models.params')}</TabsTrigger>
            </TabsList>

            <TabsContent value="overview">
              <OverviewTab model={m} onTest={() => testM.mutate()} testing={testM.isPending} />
            </TabsContent>
            <TabsContent value="health">
              <HealthTab id={id} />
            </TabsContent>
            <TabsContent value="probes">
              <ProbesTab id={id} />
            </TabsContent>
            <TabsContent value="params">
              <ParamsTab model={m} onReset={() => resetM.mutate()} resetting={resetM.isPending} />
            </TabsContent>
          </Tabs>
        )}
      </div>
    </div>
  )
}

function CircuitBadge({ state }: { state: string }) {
  const { t } = useTranslation()
  if (state === 'closed' || !state) return <Badge variant="success">{t('circuit.closed')}</Badge>
  if (state === 'permanent') return <Badge variant="destructive">{t('circuit.permanent')}</Badge>
  if (state === 'open') return <Badge variant="warning">{t('circuit.open')}</Badge>
  return <Badge variant="secondary">{t('circuit.half_open')}</Badge>
}

function KPI({ label, value, accent }: { label: string; value: string; accent?: string }) {
  return (
    <div className="rounded-md border bg-card p-3">
      <div className={cn('text-lg font-bold', accent)}>{value}</div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  )
}

function OverviewTab({ model, onTest, testing }: { model: any; onTest: () => void; testing: boolean }) {
  const { t } = useTranslation()
  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-4">
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <KPI label={t('models.requests')} value={fmtNum(model.request_count)} />
        <KPI label={t('models.success')} value={fmtPct(model.success_rate)} accent={model.success_rate >= 70 ? 'text-success' : 'text-warning'} />
        <KPI label={t('models.latency')} value={model.avg_latency_ms + 'ms'} />
        <KPI label={t('models.avail')} value={fmtPct(model.availability_score)} accent="text-primary" />
      </div>
      <div className="flex gap-2">
        <Button size="sm" variant="outline" onClick={onTest} disabled={testing}>
          <Zap className="h-4 w-4" /> {t('common.test')}
        </Button>
        {model.card_url && (
          <a href={model.card_url} target="_blank" rel="noreferrer">
            <Button size="sm" variant="ghost"><ExternalLink className="h-4 w-4" /> {t('models.card')}</Button>
          </a>
        )}
      </div>
      <Separator />
      <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
        <Row k="ID" v={model.id} />
        <Row k="Owner" v={model.owned_by} />
        <Row k={t('models.param_count')} v={model.param_count || '—'} />
        <Row k={t('models.context')} v={fmtCtx(model.context_length)} />
        <Row k={t('models.architecture')} v={model.architecture || '—'} />
        <Row k={t('models.interfaces')} v={(model.supported_interfaces || []).join(', ') || '—'} />
        <Row k="Status" v={model.status} />
        <Row k="Synced" v={timeAgo(model.synced_at)} />
      </dl>
      {model.description && <p className="text-sm text-muted-foreground">{model.description}</p>}
    </motion.div>
  )
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex justify-between gap-2">
      <dt className="text-muted-foreground">{k}</dt>
      <dd className="truncate font-medium">{v}</dd>
    </div>
  )
}

function HealthTab({ id }: { id: string }) {
  const { t } = useTranslation()
  const tsQ = useQuery({ queryKey: ['model-ts', id], queryFn: () => api.getModelTimeSeries(id, '24h', 300) })
  const data = (tsQ.data?.data ?? []).map((p) => ({ ...p, ts: p.ts * 1000 }))
  if (!data.length) return <EmptyState title={t('common.empty')} />
  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-4">
      <div className="h-40">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={data}>
            <defs><linearGradient id="g1" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="hsl(var(--primary))" stopOpacity={0.4} /><stop offset="100%" stopColor="hsl(var(--primary))" stopOpacity={0} /></linearGradient></defs>
            <XAxis dataKey="ts" tickFormatter={(v) => new Date(v).toLocaleTimeString()} fontSize={10} stroke="hsl(var(--muted-foreground))" />
            <YAxis fontSize={10} stroke="hsl(var(--muted-foreground))" />
            <Tooltip labelFormatter={(v) => new Date(Number(v)).toLocaleString()} contentStyle={{ background: 'hsl(var(--popover))', border: '1px solid hsl(var(--border))', borderRadius: 6, fontSize: 12 }} />
            <Area dataKey="count" stroke="hsl(var(--primary))" fill="url(#g1)" />
            <Area dataKey="ok_count" stroke="hsl(var(--success))" fill="transparent" />
          </AreaChart>
        </ResponsiveContainer>
      </div>
      <p className="text-xs text-muted-foreground">24h 请求量（蓝=总量，绿=成功）</p>
    </motion.div>
  )
}

function ProbesTab({ id }: { id: string }) {
  const { t } = useTranslation()
  const prQ = useQuery({ queryKey: ['model-probes', id], queryFn: () => api.getModelProbes(id, 100) })
  const data = (prQ.data?.history ?? []).map((p) => ({ ...p, ts: p.ts * 1000 }))
  if (!data.length) return <EmptyState title={t('common.empty')} />
  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-3">
      <div className="h-40">
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={data}>
            <XAxis dataKey="ts" tickFormatter={(v) => new Date(v).toLocaleTimeString()} fontSize={10} stroke="hsl(var(--muted-foreground))" />
            <YAxis fontSize={10} stroke="hsl(var(--muted-foreground))" />
            <Tooltip labelFormatter={(v) => new Date(Number(v)).toLocaleString()} contentStyle={{ background: 'hsl(var(--popover))', border: '1px solid hsl(var(--border))', borderRadius: 6, fontSize: 12 }} />
            <Bar dataKey="latency_ms" fill="hsl(var(--primary))" radius={[3, 3, 0, 0]} />
          </BarChart>
        </ResponsiveContainer>
      </div>
      <div className="space-y-1 text-xs">
        {data.slice(-8).reverse().map((p) => (
          <div key={p.ts} className="flex justify-between rounded-md border px-2 py-1">
            <span>{new Date(p.ts).toLocaleString()}</span>
            <span className={p.ok ? 'text-success' : 'text-destructive'}>{p.ok ? '✓ ' + p.latency_ms + 'ms' : '✗ ' + (p.error || p.status)}</span>
          </div>
        ))}
      </div>
    </motion.div>
  )
}

function ParamsTab({ model, onReset, resetting }: { model: any; onReset: () => void; resetting: boolean }) {
  const { t } = useTranslation()
  const broken = model.circuit_state === 'open' || model.circuit_state === 'permanent'
  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-4">
      {broken && (
        <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-sm font-medium text-destructive">
              {t('circuit.' + (model.circuit_state === 'permanent' ? 'permanent' : 'open'))}
            </span>
            <Button size="sm" variant="outline" onClick={onReset} disabled={resetting}>
              <RotateCcw className="h-4 w-4" /> {t('circuit.reset')}
            </Button>
          </div>
          <div className="grid grid-cols-2 gap-2 text-xs">
            <div>{t('circuit.success_rate')}: <b>{model.circuit_success_rate}%</b></div>
            <div>{t('circuit.bad_sweep')}: <b>{model.bad_sweep_count ?? '—'}</b></div>
          </div>
        </div>
      )}
      <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
        <Row k={t('models.architecture')} v={model.architecture || '—'} />
        <Row k={t('models.context')} v={fmtCtx(model.context_length)} />
        <Row k="Max Tokens" v={model.max_tokens ? fmtNum(model.max_tokens) : '—'} />
        <Row k={t('models.interfaces')} v={(model.supported_interfaces || []).join(', ') || '—'} />
        <Row k={t('models.pricing')} v={`${model.pricing_in || '—'} / ${model.pricing_out || '—'}`} />
        <Row k={t('models.license')} v={model.license || '—'} />
        <Row k={t('models.released')} v={model.release_date || '—'} />
        <Row k="Modalities" v={(model.input_modalities || []).join(', ') || '—'} />
      </dl>
      {model.card_url && (
        <a href={model.card_url} target="_blank" rel="noreferrer" className="text-sm text-primary hover:underline">
          {t('models.card')} ↗
        </a>
      )}
    </motion.div>
  )
}
