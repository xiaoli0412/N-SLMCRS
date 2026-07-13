import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Check, Sparkles, Cpu, Zap, Server, AlertTriangle } from 'lucide-react'
import { api, type StrategyPreset } from '@/api'
import { Card, CardContent, Badge, Button, Skeleton, ErrorState } from '@/components/ui'
import { cn } from '@/lib/utils'

export default function Strategy() {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const zh = i18n.language === 'zh'

  const q = useQuery({ queryKey: ['strategy'], queryFn: api.getStrategy, refetchInterval: 15_000 })
  const applyM = useMutation({
    mutationFn: (id: string) => api.setStrategy(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['strategy'] }); toast.success(t('strategy.applied')) },
    onError: (e: Error) => toast.error(e.message),
  })

  if (q.isLoading) return <Skeleton className="h-64 w-full" />
  if (q.isError) return <ErrorState title={t('common.empty', 'Error')} onRetry={() => q.refetch()} />
  const s = q.data
  if (!s) return null

  const name = (p: StrategyPreset) => (zh ? p.name_zh : p.name_en)
  const character = (p: StrategyPreset) => (zh ? p.character_zh : p.character_en)
  const scenario = (p: StrategyPreset) => (zh ? p.scenario_zh : p.scenario_en)

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t('strategy.title')}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{t('strategy.subtitle')}</p>
        </div>
        <div className="flex items-center gap-2 text-xs">
          <Badge variant={s.kernel_online ? 'success' : 'warning'}>
            {s.kernel_online ? (
              <span className="flex items-center gap-1"><Cpu className="h-3 w-3" />{t('strategy.kernelOnline')}</span>
            ) : (
              <span className="flex items-center gap-1"><AlertTriangle className="h-3 w-3" />{t('strategy.kernelOffline')}</span>
            )}
          </Badge>
          <Badge variant="secondary"><Server className="mr-1 h-3 w-3" />{t('strategy.keyCount')}: {s.key_count}</Badge>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {s.presets.map((p) => {
          const isActive = p.id === s.active.id
          const isRec = p.id === s.recommended
          return (
            <Card
              key={p.id}
              className={cn(
                'relative flex flex-col transition-all',
                isActive && 'border-primary ring-1 ring-primary/40 shadow-md',
              )}
            >
              <CardContent className="flex flex-1 flex-col gap-3 p-5">
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-2">
                    <span className="text-2xl">{p.icon}</span>
                    <div>
                      <div className="font-semibold leading-tight">{name(p)}</div>
                      <div className="text-xs text-muted-foreground">{p.id}</div>
                    </div>
                  </div>
                  <div className="flex flex-col items-end gap-1">
                    {isActive && (
                      <Badge variant="default"><Check className="mr-1 h-3 w-3" />{t('strategy.active')}</Badge>
                    )}
                    {isRec && (
                      <Badge variant="success"><Sparkles className="mr-1 h-3 w-3" />{t('strategy.recommended')}</Badge>
                    )}
                  </div>
                </div>

                <p className="text-sm font-medium text-primary/90">{character(p)}</p>

                <div className="grid grid-cols-2 gap-x-3 gap-y-1.5 text-xs">
                  <Row label={t('strategy.selection')} value={t(`strategy.algo.${p.selection}`, p.selection)} icon />
                  <Row label={t('strategy.fanout')} value={p.fanout > 0 ? `${t('strategy.fanoutFixed')} ${p.fanout}` : t('strategy.fanoutAuto')} />
                  <Row label={t('strategy.breakerThreshold')} value={`${p.breaker_threshold}`} />
                  <Row label={t('strategy.breakerCooldown')} value={`${p.breaker_cooldown_sec}s`} />
                  <Row label={t('strategy.rpmHeadroom')} value={`${Math.round(p.rpm_headroom * 100)}%`} />
                  <Row label={t('strategy.keyCount')} value={p.max_keys > 0 ? `${p.min_keys}–${p.max_keys}` : `≥${p.min_keys}`} />
                </div>

                <p className="mt-auto text-xs leading-relaxed text-muted-foreground">{scenario(p)}</p>

                {!isActive && (
                  <Button
                    size="sm"
                    className="w-full"
                    disabled={applyM.isPending}
                    onClick={() => applyM.mutate(p.id)}
                  >
                    {t('strategy.apply')}
                  </Button>
                )}
                {isActive && (
                  <div className="rounded-md bg-muted py-1.5 text-center text-xs text-muted-foreground">
                    {t('strategy.active')}
                  </div>
                )}
              </CardContent>
            </Card>
          )
        })}
      </div>

      {/* 切换差异预览：当前 vs 推荐不一致时提示 */}
      {s.recommended !== s.active.id && (
        <Card>
          <CardContent className="p-4 text-xs">
            <div className="mb-2 font-medium text-muted-foreground">{t('strategy.recommendHint')}</div>
            <DiffTable
              rows={[
                [t('strategy.selection'),
                  t(`strategy.algo.${s.active.selection}`, s.active.selection),
                  t(`strategy.algo.${s.presets.find((p) => p.id === s.recommended)!.selection}`)],
                [t('strategy.fanout'),
                  s.active.fanout > 0 ? String(s.active.fanout) : t('strategy.fanoutAuto'),
                  s.presets.find((p) => p.id === s.recommended)!.fanout > 0
                    ? String(s.presets.find((p) => p.id === s.recommended)!.fanout)
                    : t('strategy.fanoutAuto')],
                [t('strategy.breakerThreshold'), String(s.active.breaker_threshold),
                  String(s.presets.find((p) => p.id === s.recommended)!.breaker_threshold)],
                [t('strategy.breakerCooldown'), `${s.active.breaker_cooldown_sec}s`,
                  `${s.presets.find((p) => p.id === s.recommended)!.breaker_cooldown_sec}s`],
                [t('strategy.rpmHeadroom'), `${Math.round(s.active.rpm_headroom * 100)}%`,
                  `${Math.round(s.presets.find((p) => p.id === s.recommended)!.rpm_headroom * 100)}%`],
              ]}
            />
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function Row({ label, value, icon }: { label: string; value: string; icon?: boolean }) {
  return (
    <div className="flex flex-col">
      <span className="text-muted-foreground">{label}</span>
      <span className={cn('font-medium', icon && 'flex items-center gap-1')}>
        {icon && <Zap className="h-3 w-3 text-primary/70" />}
        {value}
      </span>
    </div>
  )
}

function DiffTable({ rows }: { rows: [string, string, string][] }) {
  return (
    <div className="overflow-hidden rounded-md border">
      <table className="w-full text-xs">
        <thead className="bg-muted/50">
          <tr>
            <th className="px-3 py-1.5 text-left font-medium text-muted-foreground">{'维度'}</th>
            <th className="px-3 py-1.5 text-left font-medium text-muted-foreground">{'当前'}</th>
            <th className="px-3 py-1.5 text-left font-medium text-muted-foreground">{'推荐'}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(([dim, a, b], i) => (
            <tr key={i} className="border-t">
              <td className="px-3 py-1.5 text-muted-foreground">{dim}</td>
              <td className="px-3 py-1.5">{a}</td>
              <td className={cn('px-3 py-1.5 font-medium', a !== b && 'text-primary')}>{b}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
