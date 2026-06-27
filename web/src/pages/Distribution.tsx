import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2, Copy } from 'lucide-react'
import { toast } from 'sonner'
import { api, type Credential } from '@/api'
import { Button, Card, CardHeader, CardTitle, CardContent, Input, Label, Dialog, DialogTrigger, DialogContent, DialogHeader, DialogTitle, Switch, Badge, EmptyState } from '@/components/ui'
import { fmtNum } from '@/lib/utils'

// v0.10 待落地的集成钩子（后端未实现，仅展示槽位）。OCTOPUS/sub2api 已按需求移除。
const FUTURE_HOOKS = [
  { id: 'new-api', name: 'new-api', type: 'Token 中转', desc: '作为上游接入 new-api，同步模型与计费' },
  { id: 'sapi', name: 'sapi', type: 'Token 中转', desc: '类 new-api 的中转/计费集成（v0.10）' },
  { id: 'webhook', name: 'Webhook', type: '事件回调', desc: '成功/失败/限流事件外发通知（v0.10）' },
]

export default function Distribution() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['credentials'], queryFn: api.listCredentials })
  const delM = useMutation({
    mutationFn: (id: number) => api.deleteCredential(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['credentials'] }); toast.success('已删除') },
    onError: (e: any) => toast.error(e.message),
  })
  const creds = q.data?.data ?? []

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">凭证与集成</h1>
        <p className="mt-1 text-sm text-muted-foreground">下游凭证签发 + 集成钩子</p>
      </div>

      <Card>
        <CardHeader className="flex-row items-center justify-between">
          <CardTitle>下游凭证（sk-nv-）</CardTitle>
          <AddCredDialog />
        </CardHeader>
        <CardContent className="p-0">
          {creds.length === 0 ? <EmptyState title="暂无凭证" /> : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead><tr className="text-left text-xs text-muted-foreground">
                  <th className="p-3">凭证</th><th className="p-3">名称</th><th className="p-3">RPM 限</th>
                  <th className="p-3">允许模型</th><th className="p-3">请求数</th><th className="p-3">启用</th><th className="p-3"></th>
                </tr></thead>
                <tbody>
                  {creds.map((c: Credential) => (
                    <tr key={c.id} className="border-t">
                      <td className="p-3 font-mono">{c.credential_mask}</td>
                      <td className="p-3">{c.name || '—'}</td>
                      <td className="p-3">{c.rpm_limit || '不限'}</td>
                      <td className="p-3 max-w-32 truncate">{c.allowed_models || '全部'}</td>
                      <td className="p-3">{fmtNum(c.total_requests)}</td>
                      <td className="p-3"><Badge variant={c.enabled ? 'success' : 'secondary'}>{c.enabled ? '启用' : '停用'}</Badge></td>
                      <td className="p-3"><Button size="icon" variant="ghost" onClick={() => delM.mutate(c.id)}><Trash2 className="h-4 w-4 text-destructive" /></Button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>集成钩子</CardTitle></CardHeader>
        <CardContent className="space-y-2">
          {FUTURE_HOOKS.map((h) => (
            <div key={h.id} className="flex items-center justify-between rounded-md border p-3">
              <div>
                <div className="flex items-center gap-2">
                  <span className="font-medium">{h.name}</span>
                  <Badge variant="outline">{h.type}</Badge>
                </div>
                <p className="mt-0.5 text-xs text-muted-foreground">{h.desc}</p>
              </div>
              <Badge variant="secondary">v0.10</Badge>
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  )
}

function AddCredDialog() {
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const [rpm, setRpm] = useState('')
  const [allowed, setAllowed] = useState('')
  const addM = useMutation({
    mutationFn: () => api.addCredential({ name: name || undefined, rpm_limit: rpm ? Number(rpm) : undefined, allowed_models: allowed || undefined }),
    onSuccess: (r) => { qc.invalidateQueries({ queryKey: ['credentials'] }); toast.success(`已创建：${r.credential}`); navigator.clipboard?.writeText(r.credential); setName(''); setRpm(''); setAllowed('') },
    onError: (e: any) => toast.error(e.message),
  })
  return (
    <Dialog>
      <DialogTrigger asChild><Button size="sm"><Plus className="h-4 w-4" /> 签发凭证</Button></DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle>签发下游凭证</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <div><Label>名称</Label><Input value={name} onChange={(e) => setName(e.target.value)} /></div>
          <div><Label>RPM 限制（0=不限）</Label><Input type="number" value={rpm} onChange={(e) => setRpm(e.target.value)} /></div>
          <div><Label>允许模型（逗号分隔，空=全部）</Label><Input value={allowed} onChange={(e) => setAllowed(e.target.value)} /></div>
          <Button className="w-full" onClick={() => addM.mutate()} disabled={addM.isPending}><Copy className="h-4 w-4" /> 创建并复制</Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
