import { useState, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { motion } from 'framer-motion'
import { Search, RefreshCw, Zap, Boxes } from 'lucide-react'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { api, type ModelView, type ModelCapability } from '@/api'
import { Button, Input, Badge, Card, EmptyState, Skeleton, Sheet, SheetContent } from '@/components/ui'
import ModelDetailPanel from './ModelDetailPanel'
import { cn, fmtNum, fmtCtx, fmtPct, timeAgo } from '@/lib/utils'

const CAPS: ModelCapability[] = [
  'chat', 'reasoning', 'code', 'vision', 'embedding', 'rerank',
  'safety', 'reward', 'translation', 'parsing',
]

export default function Models() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [q, setQ] = useState('')
  const [cap, setCap] = useState('')
  const [activeOnly, setActiveOnly] = useState(false)
  const [selected, setSelected] = useState<string | null>(null)

  const plazaQ = useQuery({
    queryKey: ['models-plaza', cap, activeOnly],
    queryFn: () => api.listModelsPlaza({ capability: cap || undefined, active_only: activeOnly }),
  })

  const syncM = useMutation({ mutationFn: api.syncModels, onSuccess: () => { qc.invalidateQueries({ queryKey: ['models-plaza'] }); toast.success('同步完成') }, onError: () => toast.error('同步失败') })
  const probeM = useMutation({ mutationFn: api.probeAllModels, onSuccess: () => { qc.invalidateQueries({ queryKey: ['models-plaza'] }); toast.success('探活已触发') }, onError: () => toast.error('探活失败') })

  const filtered = useMemo(() => {
    const list = plazaQ.data?.data ?? []
    if (!q.trim()) return list
    const k = q.toLowerCase()
    return list.filter((m) => m.id.toLowerCase().includes(k) || m.owned_by.toLowerCase().includes(k) || (m.description || '').toLowerCase().includes(k))
  }, [plazaQ.data, q])

  const capCounts = useMemo(() => {
    const c: Record<string, number> = {}
    for (const m of plazaQ.data?.data ?? []) c[m.capability] = (c[m.capability] || 0) + 1
    return c
  }, [plazaQ.data])

  return (
    <div className="aurora-bg min-h-full">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t('nav.models')}</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {plazaQ.data ? `上次同步 ${timeAgo(plazaQ.data.last_sync)} · ${plazaQ.data.total} 个模型` : ''}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => probeM.mutate()} disabled={probeM.isPending}>
            <Zap className="h-4 w-4" /> {t('common.probeAll')}
          </Button>
          <Button variant="outline" size="sm" onClick={() => syncM.mutate()} disabled={syncM.isPending}>
            <RefreshCw className={cn('h-4 w-4', syncM.isPending && 'animate-spin')} /> {t('common.sync')}
          </Button>
        </div>
      </div>

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="relative max-w-xs flex-1">
          <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input className="pl-8" placeholder={t('common.search')} value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        <div className="flex flex-wrap gap-1">
          <FilterChip active={cap === ''} onClick={() => setCap('')}>{t('common.all')}</FilterChip>
          <FilterChip active={activeOnly} onClick={() => setActiveOnly((v) => !v)}>{t('common.active')}</FilterChip>
          {CAPS.filter((c) => capCounts[c]).map((c) => (
            <FilterChip key={c} active={cap === c} onClick={() => setCap(cap === c ? '' : c)}>
              {t(`cap.${c}`)} <span className="opacity-50">{capCounts[c]}</span>
            </FilterChip>
          ))}
        </div>
      </div>

      {plazaQ.isLoading ? (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-44 rounded-lg" />)}
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState icon={<Boxes className="h-10 w-10" />} title={t('common.empty')} />
      ) : (
        <motion.div layout className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
          {filtered.map((m, i) => (
            <motion.div
              key={m.id}
              layout
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: Math.min(i * 0.03, 0.3) }}
            >
              <ModelCard model={m} onClick={() => setSelected(m.id)} />
            </motion.div>
          ))}
        </motion.div>
      )}

      <Sheet open={!!selected} onOpenChange={(v) => !v && setSelected(null)}>
        <SheetContent>
          {selected && <ModelDetailPanel id={selected} />}
        </SheetContent>
      </Sheet>
    </div>
  )
}

function FilterChip({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-1 rounded-md border px-2.5 py-1 text-xs transition-colors',
        active ? 'border-primary bg-primary/10 text-primary' : 'border-border text-muted-foreground hover:bg-muted',
      )}
    >
      {children}
    </button>
  )
}

function ModelCard({ model, onClick }: { model: ModelView; onClick: () => void }) {
  const { t } = useTranslation()
  const score = model.availability_score
  const circuit = model.circuit_state
  const isBroken = circuit === 'open' || circuit === 'permanent'

  return (
    <motion.button
      whileHover={{ y: -4 }}
      transition={{ type: 'spring', stiffness: 300, damping: 20 }}
      onClick={onClick}
      className={cn(
        'group relative w-full overflow-hidden rounded-lg border bg-card p-4 text-left shadow-sm transition-colors',
        isBroken ? 'border-destructive/40' : 'hover:border-primary/40',
        !model.is_active && 'opacity-60',
      )}
    >
      <div className="mb-3 flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate font-mono text-sm font-medium">{model.id}</div>
          <div className="mt-0.5 text-xs text-muted-foreground">{model.owned_by}</div>
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1">
          <Badge variant="outline">{t(`cap.${model.capability}`)}</Badge>
          {isBroken && (
            <Badge variant={circuit === 'permanent' ? 'destructive' : 'warning'}>
              {t(`circuit.${circuit}`)}
            </Badge>
          )}
        </div>
      </div>

      <p className="mb-3 line-clamp-2 h-8 text-xs text-muted-foreground">{model.description || '—'}</p>

      <div className="mb-3 grid grid-cols-4 gap-1 text-center">
        <Stat label={t('models.param_count')} value={model.param_count || '—'} />
        <Stat label={t('models.context')} value={fmtCtx(model.context_length)} />
        <Stat label={t('models.requests')} value={fmtNum(model.request_count)} />
        <Stat label={t('models.success')} value={fmtPct(model.success_rate)} />
      </div>

      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground">{model.avg_latency_ms}ms · {fmtNum(model.error_count)} err</span>
        {score > 0 && (
          <span className={cn('font-semibold', score >= 70 ? 'text-success' : score >= 30 ? 'text-warning' : 'text-destructive')}>
            ★ {score.toFixed(0)}
          </span>
        )}
      </div>
    </motion.button>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-sm font-semibold">{value}</div>
      <div className="text-[10px] text-muted-foreground">{label}</div>
    </div>
  )
}
