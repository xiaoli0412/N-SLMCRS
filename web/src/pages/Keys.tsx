import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2, Upload } from 'lucide-react'
import { toast } from 'sonner'
import { api, type UpstreamKey } from '@/api'
import { Button, Card, CardHeader, CardTitle, CardContent, Input, Label, Switch, Dialog, DialogTrigger, DialogContent, DialogHeader, DialogTitle, Textarea, Badge, EmptyState } from '@/components/ui'
import { fmtNum } from '@/lib/utils'

export default function Keys() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['keys'], queryFn: api.listKeys })
  const toggleM = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => api.toggleKey(id, enabled),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['keys'] }),
  })
  const delM = useMutation({
    mutationFn: (id: number) => api.deleteKey(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['keys'] }); toast.success('已删除') },
    onError: (e: any) => toast.error(e.message),
  })
  const keys = q.data?.data ?? []
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">上游密钥</h1>
          <p className="mt-1 text-sm text-muted-foreground">NVIDIA nvapi- 密钥池 · {keys.length} 个</p>
        </div>
        <AddKeyDialog />
      </div>
      <Card>
        <CardContent className="p-0">
          {keys.length === 0 ? <EmptyState title="暂无密钥" desc="添加 NVIDIA nvapi- 密钥以启用转发" /> : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead><tr className="text-left text-xs text-muted-foreground">
                  <th className="p-3">密钥</th><th className="p-3">标签</th><th className="p-3">状态</th>
                  <th className="p-3">连续失败</th><th className="p-3">RPM</th><th className="p-3">启用</th><th className="p-3"></th>
                </tr></thead>
                <tbody>
                  {keys.map((k: UpstreamKey) => (
                    <tr key={k.id} className="border-t">
                      <td className="p-3 font-mono">{k.key_mask}</td>
                      <td className="p-3">{k.label || '—'}</td>
                      <td className="p-3"><StatusBadge status={k.status} /></td>
                      <td className="p-3">{k.consecutive_fail}</td>
                      <td className="p-3">{k.rpm_override || '默认'}</td>
                      <td className="p-3"><Switch checked={k.enabled} onCheckedChange={(v) => toggleM.mutate({ id: k.id, enabled: v })} /></td>
                      <td className="p-3"><Button size="icon" variant="ghost" onClick={() => delM.mutate(k.id)}><Trash2 className="h-4 w-4 text-destructive" /></Button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  if (status === 'active') return <Badge variant="success">active</Badge>
  if (status === 'circuit_open') return <Badge variant="destructive">熔断</Badge>
  if (status === 'half_open') return <Badge variant="warning">半开</Badge>
  return <Badge variant="secondary">{status}</Badge>
}

function AddKeyDialog() {
  const qc = useQueryClient()
  const [raw, setRaw] = useState('')
  const [label, setLabel] = useState('')
  const bulkM = useMutation({
    mutationFn: () => api.bulkAddKeys({ raw, label }),
    onSuccess: (r) => { qc.invalidateQueries({ queryKey: ['keys'] }); toast.success(`导入 ${r.added} 个，跳过 ${r.skipped} 个`); setRaw(''); setLabel('') },
    onError: (e: any) => toast.error(e.message),
  })
  return (
    <Dialog>
      <DialogTrigger asChild><Button size="sm"><Plus className="h-4 w-4" /> 添加密钥</Button></DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle>批量导入密钥</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <div>
            <Label>密钥（每行一个 nvapi-xxx）</Label>
            <Textarea className="min-h-32 font-mono text-xs" value={raw} onChange={(e) => setRaw(e.target.value)} placeholder="nvapi-xxxxx&#10;nvapi-yyyyy" />
          </div>
          <div><Label>标签</Label><Input value={label} onChange={(e) => setLabel(e.target.value)} /></div>
          <Button className="w-full" disabled={!raw.trim()} onClick={() => bulkM.mutate()}><Upload className="h-4 w-4" /> 导入</Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
