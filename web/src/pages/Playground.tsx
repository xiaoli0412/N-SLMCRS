import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Send, Square, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { api } from '@/api'
import {
  Card, CardContent, CardHeader, CardTitle, Button, Input, Label, Textarea, Switch, Badge,
} from '@/components/ui'
import { cn } from '@/lib/utils'

interface Msg { role: 'user' | 'assistant' | 'system'; content: string }

export default function Playground() {
  const mq = useQuery({ queryKey: ['models'], queryFn: api.listModels })
  const models = mq.data?.data?.filter((m) => m.is_active) ?? []

  const [model, setModel] = useState('')
  const [system, setSystem] = useState('')
  const [input, setInput] = useState('')
  const [temp, setTemp] = useState(0.7)
  const [maxTokens, setMaxTokens] = useState(512)
  const [stream, setStream] = useState(true)
  const [messages, setMessages] = useState<Msg[]>([])
  const [sending, setSending] = useState(false)
  const [latencyMs, setLatencyMs] = useState<number | null>(null)
  const [usage, setUsage] = useState<any>(null)
  const abortRef = useRef<AbortController | null>(null)

  // 默认选第一个可用模型
  if (!model && models.length > 0) setModel(models[0].id)

  async function send() {
    const userMsg = input.trim()
    if (!userMsg || !model || sending) return
    setInput('')
    setLatencyMs(null)
    setUsage(null)
    const msgs: Msg[] = [...messages, { role: 'user', content: userMsg }]
    if (system.trim()) msgs.unshift({ role: 'system', content: system.trim() })
    setMessages([...msgs, { role: 'assistant', content: '' }])

    const body: any = {
      model,
      messages: (system.trim() ? [{ role: 'system', content: system.trim() }] : []).concat(msgs.filter((m) => m.role !== 'system')),
      temperature: temp,
      max_tokens: maxTokens,
    }
    const start = performance.now()
    setSending(true)
    const ac = new AbortController()
    abortRef.current = ac
    try {
      if (stream) {
        const { content, usage } = await api.playgroundStream(
          body,
          (delta) => {
            setMessages((m) => {
              const next = [...m]
              const last = next[next.length - 1]
              if (last && last.role === 'assistant') next[next.length - 1] = { ...last, content: last.content + delta }
              return next
            })
          },
          ac.signal,
        )
        setMessages((m) => {
          const next = [...m]
          const last = next[next.length - 1]
          if (last && last.role === 'assistant' && content && !last.content) next[next.length - 1] = { ...last, content }
          return next
        })
        setUsage(usage)
      } else {
        const r = await api.playgroundChat({ ...body, stream: false })
        const text = r?.choices?.[0]?.message?.content ?? ''
        setMessages((m) => {
          const next = [...m]
          const last = next[next.length - 1]
          if (last && last.role === 'assistant') next[next.length - 1] = { ...last, content: text }
          return next
        })
        setUsage(r?.usage ?? null)
      }
      setLatencyMs(Math.round(performance.now() - start))
    } catch (e: any) {
      if (e?.name === 'AbortError') toast.info('已停止')
      else toast.error(e?.message ?? '请求失败')
    } finally {
      setSending(false)
      abortRef.current = null
    }
  }

  function stop() {
    abortRef.current?.abort()
  }

  function clearAll() {
    setMessages([])
    setLatencyMs(null)
    setUsage(null)
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Chat 测试台</h1>
          <p className="mt-1 text-sm text-muted-foreground">管理凭证直调 · 复用 N 路并发与熔断 · 仿 NVIDIA Studio</p>
        </div>
        <Button size="sm" variant="ghost" onClick={clearAll} disabled={messages.length === 0}>
          <Trash2 className="h-4 w-4" /> 清空
        </Button>
      </div>

      <div className="grid gap-4 lg:grid-cols-[1fr_320px]">
        {/* 会话区 */}
        <Card className="flex min-h-[480px] flex-col">
          <CardContent className="flex-1 space-y-3 overflow-auto p-4">
            {messages.length === 0 ? (
              <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                选择模型，输入消息后发送
              </div>
            ) : (
              messages.map((m, i) => (
                <div key={i} className={cn('flex', m.role === 'user' ? 'justify-end' : 'justify-start')}>
                  <div
                    className={cn(
                      'max-w-[85%] whitespace-pre-wrap break-words rounded-lg px-3 py-2 text-sm',
                      m.role === 'user'
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-muted text-foreground',
                    )}
                  >
                    {m.content || (sending && i === messages.length - 1 ? '…' : '')}
                  </div>
                </div>
              ))
            )}
          </CardContent>
          <div className="border-t p-3">
            <Textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="输入消息…（Ctrl+Enter 发送）"
              rows={3}
              onKeyDown={(e) => {
                if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') { e.preventDefault(); send() }
              }}
            />
            <div className="mt-2 flex items-center justify-between">
              <span className="text-xs text-muted-foreground">
                {latencyMs != null && `${latencyMs} ms`}
                {usage && ` · ${usage.total_tokens ?? 0} tokens`}
              </span>
              {sending ? (
                <Button size="sm" variant="destructive" onClick={stop}>
                  <Square className="h-4 w-4" /> 停止
                </Button>
              ) : (
                <Button size="sm" onClick={send} disabled={!input.trim() || !model}>
                  <Send className="h-4 w-4" /> 发送
                </Button>
              )}
            </div>
          </div>
        </Card>

        {/* 配置区 */}
        <Card className="h-fit">
          <CardHeader>
            <CardTitle className="text-base">参数</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-1.5">
              <Label>模型</Label>
              <select
                value={model}
                onChange={(e) => setModel(e.target.value)}
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm focus:outline-none focus:ring-1 focus:ring-ring"
              >
                {models.map((m) => (
                  <option key={m.id} value={m.id}>{m.id}</option>
                ))}
              </select>
            </div>
            <div className="space-y-1.5">
              <Label>系统提示（可选）</Label>
              <Textarea value={system} onChange={(e) => setSystem(e.target.value)} rows={3} placeholder="You are a helpful assistant." />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label>Temperature</Label>
                <Input type="number" step="0.1" min="0" max="2" value={temp} onChange={(e) => setTemp(Number(e.target.value))} />
              </div>
              <div className="space-y-1.5">
                <Label>Max tokens</Label>
                <Input type="number" min="1" value={maxTokens} onChange={(e) => setMaxTokens(Number(e.target.value))} />
              </div>
            </div>
            <div className="flex items-center justify-between">
              <Label>流式输出</Label>
              <Switch checked={stream} onCheckedChange={setStream} />
            </div>
            <div className="flex items-center gap-2 pt-1 text-xs text-muted-foreground">
              <Badge variant="secondary">{stream ? 'SSE' : 'JSON'}</Badge>
              <span>管理凭证鉴权，不计费</span>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
