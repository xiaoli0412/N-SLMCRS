import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2, Copy, Webhook, Cable, Send } from 'lucide-react'
import { toast } from 'sonner'
import { api, type Credential, type Channel, type ChannelConfig } from '@/api'
import { Button, Card, CardHeader, CardTitle, CardContent, Input, Label, Textarea, Dialog, DialogTrigger, DialogContent, DialogHeader, DialogTitle, Switch, Badge, EmptyState, Separator } from '@/components/ui'
import { fmtNum, timeAgo } from '@/lib/utils'

const EVENT_OPTIONS = ['success', 'error', 'rate_limited', 'circuit']

export default function Distribution() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">凭证与集成</h1>
        <p className="mt-1 text-sm text-muted-foreground">下游凭证签发 + 集成渠道 + Webhook 事件回调</p>
      </div>
      <CredentialsCard />
      <ChannelsCard />
      <WebhookCard />
    </div>
  )
}

// ─── 下游凭证（sk-nv-）──────────────────────────────────────────────────
function CredentialsCard() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['credentials'], queryFn: api.listCredentials })
  const delM = useMutation({
    mutationFn: (id: number) => api.deleteCredential(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['credentials'] }); toast.success('已删除') },
    onError: (e: any) => toast.error(e.message),
  })
  const creds = q.data?.data ?? []

  return (
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

// ─── 集成渠道（new-api / sapi）───────────────────────────────────────────
function ChannelsCard() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['channels'], queryFn: api.listChannels })
  const delM = useMutation({
    mutationFn: (id: number) => api.deleteChannel(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['channels'] }); toast.success('渠道已删除') },
    onError: (e: any) => toast.error(e.message),
  })
  const toggleM = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => api.toggleChannel(id, enabled),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['channels'] }),
    onError: (e: any) => { toast.error(e.message); qc.invalidateQueries({ queryKey: ['channels'] }) },
  })
  const chs = q.data?.data ?? []

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <div>
          <CardTitle className="flex items-center gap-2"><Cable className="h-5 w-5" /> 集成渠道</CardTitle>
          <p className="mt-1 text-xs text-muted-foreground">new-api / sapi 通过 OpenAI 兼容协议接入本网关，模型同步 + 计费回采</p>
        </div>
        <AddChannelDialog />
      </CardHeader>
      <CardContent className="p-0">
        {chs.length === 0 ? <EmptyState title="暂无集成渠道" desc="新增渠道后将生成可粘贴的接入配置" /> : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead><tr className="text-left text-xs text-muted-foreground">
                <th className="p-3">名称</th><th className="p-3">类型</th><th className="p-3">接入地址</th>
                <th className="p-3">密钥</th><th className="p-3">请求数</th><th className="p-3">最近同步</th>
                <th className="p-3">启用</th><th className="p-3"></th>
              </tr></thead>
              <tbody>
                {chs.map((ch: Channel) => (
                  <tr key={ch.ID} className="border-t">
                    <td className="p-3 font-medium">{ch.Name}</td>
                    <td className="p-3"><Badge variant="outline">{ch.Type}</Badge></td>
                    <td className="p-3 max-w-48 truncate font-mono text-xs">{ch.BaseURL || '—'}</td>
                    <td className="p-3 font-mono text-xs">{ch.APIKeyMask || '—'}</td>
                    <td className="p-3">{fmtNum(ch.TotalRequests)}</td>
                    <td className="p-3 text-xs text-muted-foreground">{ch.LastSyncAt ? timeAgo(ch.LastSyncAt) : '—'}</td>
                    <td className="p-3">
                      <Switch
                        checked={ch.Enabled}
                        onCheckedChange={(v) => toggleM.mutate({ id: ch.ID, enabled: v })}
                        disabled={toggleM.isPending}
                      />
                    </td>
                    <td className="p-3">
                      <div className="flex gap-1">
                        <ChannelConfigBtn id={ch.ID} name={ch.Name} />
                        <Button size="icon" variant="ghost" onClick={() => delM.mutate(ch.ID)}><Trash2 className="h-4 w-4 text-destructive" /></Button>
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
  )
}

function AddChannelDialog() {
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const [type, setType] = useState('newapi')
  const [baseURL, setBaseURL] = useState('')
  const [apiKey, setApiKey] = useState('')
  const addM = useMutation({
    mutationFn: () => api.addChannel({ name, type, base_url: baseURL || undefined, api_key: apiKey }),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ['channels'] })
      toast.success(`渠道 ${r.config.name} 已创建，接入配置已复制`)
      navigator.clipboard?.writeText(JSON.stringify(r.config, null, 2))
      setName(''); setBaseURL(''); setApiKey('')
    },
    onError: (e: any) => toast.error(e.message),
  })
  return (
    <Dialog>
      <DialogTrigger asChild><Button size="sm"><Plus className="h-4 w-4" /> 新增渠道</Button></DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle>新增集成渠道</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <div><Label>名称 *</Label><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="如 newapi-prod" /></div>
          <div>
            <Label>类型</Label>
            <select className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm" value={type} onChange={(e) => setType(e.target.value)}>
              <option value="newapi">newapi</option>
              <option value="sapi">sapi</option>
            </select>
          </div>
          <div><Label>接入地址（留空则按当前请求推断）</Label><Input value={baseURL} onChange={(e) => setBaseURL(e.target.value)} placeholder="http://host:8787/v1" /></div>
          <div><Label>渠道密钥 *</Label><Input value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder="sk-nv-..." /></div>
          <p className="text-xs text-muted-foreground">创建后将生成可粘贴到 new-api/sapi 的接入配置（含明文密钥，仅此一次）。</p>
          <Button className="w-full" onClick={() => addM.mutate()} disabled={addM.isPending || !name || !apiKey}>创建并复制配置</Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function ChannelConfigBtn({ id, name }: { id: number; name: string }) {
  const [open, setOpen] = useState(false)
  const q = useQuery({
    queryKey: ['channel-config', id],
    queryFn: () => api.channelConfig(id),
    enabled: open,
  })
  const cfg = q.data?.config
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild><Button size="icon" variant="ghost" title="查看接入配置"><Copy className="h-4 w-4" /></Button></DialogTrigger>
      <DialogContent className="max-w-2xl">
        <DialogHeader><DialogTitle>{name} · 接入配置</DialogTitle></DialogHeader>
        {cfg ? (
          <div className="space-y-3">
            <p className="text-xs text-muted-foreground">粘贴到 new-api/sapi 的渠道配置（密钥为创建时返回的明文，此处不再回显）。</p>
            <pre className="max-h-80 overflow-auto rounded-md bg-muted p-3 text-xs">{JSON.stringify(cfg, null, 2)}</pre>
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Badge variant="outline">{cfg.type}</Badge>
              <span>{cfg.models?.length ?? 0} 个可用模型</span>
              <span className="truncate">计费回采：{cfg.usage_url}</span>
            </div>
            <Button size="sm" variant="outline" onClick={() => { navigator.clipboard?.writeText(JSON.stringify(cfg, null, 2)); toast.success('已复制') }}><Copy className="h-4 w-4" /> 复制 JSON</Button>
          </div>
        ) : q.isFetching ? <p className="text-sm text-muted-foreground">加载中…</p> : <p className="text-sm text-muted-foreground">无配置</p>}
      </DialogContent>
    </Dialog>
  )
}

// ─── Webhook 事件回调 ────────────────────────────────────────────────────
function WebhookCard() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['webhook'], queryFn: api.getWebhook })
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [events, setEvents] = useState<string[]>([])
  const [hydrated, setHydrated] = useState(false)

  // 首次加载回填表单
  if (q.data && !hydrated) {
    setUrl(q.data.url || '')
    setSecret(q.data.secret && !q.data.secret.startsWith('••••') ? q.data.secret : '')
    setEvents(q.data.events ? q.data.events.split(',').map((s) => s.trim()).filter(Boolean) : [])
    setHydrated(true)
  }

  const saveM = useMutation({
    mutationFn: () => api.putWebhook({
      url,
      // secret 为空或掩码占位时不覆盖（后端保留原值）
      secret: secret.startsWith('••••') ? '' : secret,
      events: events.join(','),
    }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['webhook'] }); toast.success('Webhook 配置已保存') },
    onError: (e: any) => toast.error(e.message),
  })
  const testM = useMutation({
    mutationFn: () => api.testWebhook(),
    onSuccess: () => toast.success('测试事件已发送，请检查接收端'),
    onError: (e: any) => toast.error('测试失败：' + e.message),
  })

  const hasSecretMask = !!q.data?.secret && q.data.secret.startsWith('••••')

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><Webhook className="h-5 w-5" /> Webhook 事件回调</CardTitle>
        <p className="mt-1 text-xs text-muted-foreground">请求成功 / 失败 / 限流 / 熔断时异步 POST JSON 通知，带 HMAC-SHA256 签名</p>
      </CardHeader>
      <CardContent className="space-y-4">
        <div>
          <Label>回调 URL</Label>
          <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/hooks/n-slmcrs" />
          <p className="mt-1 text-xs text-muted-foreground">留空则禁用 webhook（不外发任何事件）</p>
        </div>
        <div>
          <Label>签名密钥（HMAC-SHA256）</Label>
          <Input
            value={hasSecretMask && !secret ? '••••（已设置，留空保留）' : secret}
            onChange={(e) => setSecret(e.target.value)}
            placeholder={hasSecretMask ? '已设置，输入新值则覆盖' : '可选，用于请求头 X-N-SLMCRS-Signature'}
            onFocus={() => { if (hasSecretMask && !secret) setSecret('') }}
          />
        </div>
        <div>
          <Label>触发事件</Label>
          <div className="flex flex-wrap gap-2 pt-1">
            {EVENT_OPTIONS.map((ev) => {
              const on = events.includes(ev)
              return (
                <button
                  key={ev}
                  type="button"
                  onClick={() => setEvents((prev) => on ? prev.filter((x) => x !== ev) : [...prev, ev])}
                  className={`rounded-md border px-3 py-1 text-xs transition-colors ${on ? 'border-primary bg-primary/15 text-primary' : 'text-muted-foreground hover:bg-muted'}`}
                >
                  {ev}
                </button>
              )
            })}
            <span className="self-center text-xs text-muted-foreground">（不选=全部事件）</span>
          </div>
        </div>
        <Separator />
        <div className="flex items-center gap-2">
          <Button onClick={() => saveM.mutate()} disabled={saveM.isPending}>保存配置</Button>
          <Button variant="outline" onClick={() => testM.mutate()} disabled={testM.isPending || !url}><Send className="h-4 w-4" /> 发送测试</Button>
          {q.data && <Badge variant={q.data.url ? 'success' : 'secondary'}>{q.data.url ? '已启用' : '未启用'}</Badge>}
        </div>
      </CardContent>
    </Card>
  )
}
