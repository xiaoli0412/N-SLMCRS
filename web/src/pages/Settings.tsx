import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api, type SchedulerSettings } from '@/api'
import { Card, CardHeader, CardTitle, CardContent, Button, Input, Label, Separator } from '@/components/ui'

export default function Settings() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['settings'], queryFn: api.getSettings })
  const [s, setS] = useState<Partial<SchedulerSettings>>({})
  useEffect(() => { if (q.data) setS(q.data) }, [q.data])

  const saveM = useMutation({
    mutationFn: () => api.putSettings(s),
    onSuccess: (r) => { setS(r.settings); toast.success('已保存并热生效') },
    onError: (e: any) => toast.error(e.message),
  })

  if (!q.data) return null
  const set = (k: keyof SchedulerSettings, v: number) => setS((p) => ({ ...p, [k]: v }))

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">设置</h1>
        <p className="mt-1 text-sm text-muted-foreground">熔断 / 调度 / 模型健康扫描运行时配置</p>
      </div>

      <Card>
        <CardHeader><CardTitle>调度与按 Key 熔断</CardTitle></CardHeader>
        <CardContent className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label="默认并发" value={s.default_concurrency} onChange={(v) => set('default_concurrency', v)} />
          <Field label="最大并发" value={s.max_concurrency} onChange={(v) => set('max_concurrency', v)} />
          <Field label="熔断阈值（连续失败）" value={s.circuit_threshold} onChange={(v) => set('circuit_threshold', v)} />
          <Field label="熔断冷却（秒）" value={s.circuit_cooldown_sec} onChange={(v) => set('circuit_cooldown_sec', v)} />
          <Field label="请求超时（秒）" value={s.request_timeout_sec} onChange={(v) => set('request_timeout_sec', v)} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>模型级健康扫描与熔断（v0.9）</CardTitle></CardHeader>
        <CardContent className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label="每接口探测次数" value={s.mh_probe_count} onChange={(v) => set('mh_probe_count', v)} />
          <Field label="探测间隔（秒）" value={s.mh_probe_interval_sec} onChange={(v) => set('mh_probe_interval_sec', v)} />
          <Field label="扫描周期（秒）" value={s.mh_sweep_interval_sec} onChange={(v) => set('mh_sweep_interval_sec', v)} />
          <Field label="永久熔断地板（%）" value={s.mh_success_rate_floor} onChange={(v) => set('mh_success_rate_floor', v)} />
          <Field label="临时熔断阈值（%）" value={s.mh_success_rate_threshold} onChange={(v) => set('mh_success_rate_threshold', v)} />
          <Field label="连续坏扫描→永久" value={s.mh_bad_sweep_to_permanent} onChange={(v) => set('mh_bad_sweep_to_permanent', v)} />
          <Field label="熔断冷却基准（秒）" value={s.mh_cooldown_base_sec} onChange={(v) => set('mh_cooldown_base_sec', v)} />
        </CardContent>
      </Card>

      <div className="flex justify-end">
        <Button onClick={() => saveM.mutate()} disabled={saveM.isPending}>{saveM.isPending ? '保存中…' : '保存并热生效'}</Button>
      </div>
    </div>
  )
}

function Field({ label, value, onChange }: { label: string; value: number | undefined; onChange: (v: number) => void }) {
  return (
    <div>
      <Label className="mb-1.5 block">{label}</Label>
      <Input type="number" value={value ?? 0} onChange={(e) => onChange(Number(e.target.value))} />
    </div>
  )
}
