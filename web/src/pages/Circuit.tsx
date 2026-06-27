import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { motion } from 'framer-motion'
import { RefreshCw, RotateCcw, Zap } from 'lucide-react'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { api, type ModelCircuit } from '@/api'
import { Button, Card, Badge, EmptyState, Skeleton, Sheet, SheetContent } from '@/components/ui'
import ModelDetailPanel from './ModelDetailPanel'
import { timeAgo } from '@/lib/utils'

export default function Circuit() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [selected, setSelected] = useState<string | null>(null)
  const q = useQuery({ queryKey: ['model-circuit'], queryFn: () => api.listModelCircuit() })
  const sweepM = useMutation({ mutationFn: api.healthSweep, onSuccess: () => { qc.invalidateQueries({ queryKey: ['model-circuit'] }); toast.success('健康扫描已触发') }, onError: (e: any) => toast.error(e.message) })
  const resetM = useMutation({
    mutationFn: (model: string) => api.resetModelCircuit(model),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['model-circuit'] }); toast.success('熔断已复位') },
    onError: (e: any) => toast.error(e.message),
  })

  const list = q.data?.data ?? []
  const broken = list.filter((c) => c.State === 'open' || c.State === 'permanent' || c.State === 'half_open')

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t('nav.circuit')}</h1>
          <p className="mt-1 text-sm text-muted-foreground">模型级熔断状态 · {list.length} 个有记录 · {broken.length} 个异常</p>
        </div>
        <Button size="sm" onClick={() => sweepM.mutate()} disabled={sweepM.isPending}>
          <Zap className="h-4 w-4" /> {t('common.sweep')}
        </Button>
      </div>

      {q.isLoading ? <Skeleton className="h-40" /> : broken.length === 0 ? (
        <EmptyState icon={<RefreshCw className="h-10 w-10" />} title="所有模型熔断正常" desc="无 open / permanent 模型" />
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
          {broken.map((c, i) => (
            <motion.div key={c.Model} initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: i * 0.03 }}>
              <Card className="p-4">
                <div className="mb-3 flex items-center justify-between">
                  <button className="truncate text-left font-mono text-sm font-medium hover:text-primary" onClick={() => setSelected(c.Model)}>{c.Model}</button>
                  <CircuitBadge state={c.State} />
                </div>
                <div className="mb-3 grid grid-cols-3 gap-2 text-center text-xs">
                  <div><div className="font-semibold">{c.SuccessRatePct}%</div><div className="text-muted-foreground">{t('circuit.success_rate')}</div></div>
                  <div><div className="font-semibold">{c.BadSweepCount}</div><div className="text-muted-foreground">{t('circuit.bad_sweep')}</div></div>
                  <div><div className="font-semibold">{c.ConsecutiveFail}</div><div className="text-muted-foreground">连续失败</div></div>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-xs text-muted-foreground">扫描 {timeAgo(c.LastSweepAt)}</span>
                  <Button size="sm" variant="outline" onClick={() => resetM.mutate(c.Model)} disabled={resetM.isPending}>
                    <RotateCcw className="h-3.5 w-3.5" /> {t('circuit.reset')}
                  </Button>
                </div>
              </Card>
            </motion.div>
          ))}
        </div>
      )}

      <Sheet open={!!selected} onOpenChange={(v) => !v && setSelected(null)}>
        <SheetContent>{selected && <ModelDetailPanel id={selected} />}</SheetContent>
      </Sheet>
    </div>
  )
}

function CircuitBadge({ state }: { state: string }) {
  const { t } = useTranslation()
  if (state === 'permanent') return <Badge variant="destructive">{t('circuit.permanent')}</Badge>
  if (state === 'open') return <Badge variant="warning">{t('circuit.open')}</Badge>
  return <Badge variant="secondary">{t('circuit.half_open')}</Badge>
}
