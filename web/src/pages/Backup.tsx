import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Database, Download, Trash2, Plus } from 'lucide-react'
import { toast } from 'sonner'
import { api } from '@/api'
import { Card, CardHeader, CardTitle, CardContent, Button, EmptyState } from '@/components/ui'
import { fmtBytes, timeAgo } from '@/lib/utils'

export default function Backup() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['backups'], queryFn: api.listBackups })
  const createM = useMutation({
    mutationFn: api.createBackup,
    onSuccess: (r) => { qc.invalidateQueries({ queryKey: ['backups'] }); toast.success(`已创建：${r.name}`) },
    onError: (e: any) => toast.error(e.message),
  })
  const delM = useMutation({
    mutationFn: (f: string) => api.deleteBackup(f),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['backups'] }); toast.success('已删除') },
    onError: (e: any) => toast.error(e.message),
  })
  const dlM = useMutation({
    mutationFn: (f: string) => api.downloadBackup(f),
    onSuccess: (url) => { const a = document.createElement('a'); a.href = url; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000) },
    onError: (e: any) => toast.error(e.message),
  })
  const list = q.data?.data ?? []
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">数据库备份</h1>
          <p className="mt-1 text-sm text-muted-foreground">VACUUM INTO 事务一致快照</p>
        </div>
        <Button size="sm" onClick={() => createM.mutate()} disabled={createM.isPending}><Plus className="h-4 w-4" /> 立即备份</Button>
      </div>
      <Card>
        <CardContent className="p-0">
          {list.length === 0 ? <EmptyState icon={<Database className="h-10 w-10" />} title="暂无备份" /> : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead><tr className="text-left text-xs text-muted-foreground">
                  <th className="p-3">文件名</th><th className="p-3">大小</th><th className="p-3">时间</th><th className="p-3"></th>
                </tr></thead>
                <tbody>
                  {list.map((b) => (
                    <tr key={b.name} className="border-t">
                      <td className="p-3 font-mono text-xs">{b.name}</td>
                      <td className="p-3">{fmtBytes(b.size)}</td>
                      <td className="p-3">{timeAgo(b.mod_time)}</td>
                      <td className="p-3">
                        <div className="flex gap-1">
                          <Button size="icon" variant="ghost" onClick={() => dlM.mutate(b.name)}><Download className="h-4 w-4" /></Button>
                          <Button size="icon" variant="ghost" onClick={() => delM.mutate(b.name)}><Trash2 className="h-4 w-4 text-destructive" /></Button>
                        </div>
                      </td>
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
