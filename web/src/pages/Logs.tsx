import { useState, useEffect, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search, Clock, History } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api, type AppLog, type AuditEntry } from '@/api'
import { Card, CardContent, Input, Badge, Button, EmptyState, ErrorState, Skeleton, Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui'
import { timeAgo } from '@/lib/utils'

const PAGE = 200
// v0.14：source 枚举对齐实际发射值（修旧版下拉与发射源错配）
const SOURCES = ['entry', 'server', 'scheduler', 'upstream', 'autopilot', 'modelhealth', 'ratelimit', 'data']
const LEVELS = ['debug', 'info', 'warn', 'error']

export default function Logs() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('app')
  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t('logs.title')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('logs.subtitle')}</p>
      </div>
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="app">{t('logs.tabApp')}</TabsTrigger>
          <TabsTrigger value="audit">{t('logs.tabAudit')}</TabsTrigger>
        </TabsList>
        <TabsContent value="app" className="mt-4"><AppLogs /></TabsContent>
        <TabsContent value="audit" className="mt-4"><AuditLogs /></TabsContent>
      </Tabs>
    </div>
  )
}

// ─── 应用日志（游标分页 + 自动刷新最新页）─────────────────────────────

function AppLogs() {
  const { t } = useTranslation()
  const [trace, setTrace] = useState('')
  const [level, setLevel] = useState('')
  const [source, setSource] = useState('')
  const [absTime, setAbsTime] = useState(false)
  const [older, setOlder] = useState<AppLog[]>([])
  const [hasMore, setHasMore] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)

  const filterKey = `${trace}|${level}|${source}`
  const buildParams = useCallback(() => {
    const p = new URLSearchParams()
    if (trace) p.set('trace_id', trace)
    if (level) p.set('level', level)
    if (source) p.set('source', source)
    p.set('limit', String(PAGE))
    return p
  }, [trace, level, source])

  const latestQ = useQuery({
    queryKey: ['logs-latest', filterKey],
    queryFn: () => api.getLogs('?' + buildParams().toString()),
    refetchInterval: 10_000,
  })
  const latest = latestQ.data?.data ?? []

  // 切换筛选条件时重置已加载的更早页
  useEffect(() => { setOlder([]); setHasMore(true) }, [filterKey])

  const oldest = older.length ? older[older.length - 1] : (latest.length ? latest[latest.length - 1] : null)
  const loadMore = async () => {
    if (!oldest) return
    setLoadingMore(true)
    try {
      const p = buildParams()
      p.set('before_ts', String(oldest.ts))
      p.set('before_id', String(oldest.id))
      const r = await api.getLogs('?' + p.toString())
      const next = r.data ?? []
      setOlder((prev) => [...prev, ...next])
      setHasMore(next.length >= PAGE)
    } catch {
      setHasMore(false)
    } finally {
      setLoadingMore(false)
    }
  }

  // 合并最新页 + 已加载更早页，按 id 去重（最新页刷新可能与更早页边界重叠）
  const seen = new Set<number>()
  const display = [...latest, ...older].filter((l) => {
    if (seen.has(l.id)) return false
    seen.add(l.id)
    return true
  })

  return (
    <div className="space-y-3">
      <Card>
        <CardContent className="p-3">
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative min-w-48 flex-1">
              <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input className="pl-8" placeholder={t('logs.traceId')} value={trace} onChange={(e) => setTrace(e.target.value)} />
            </div>
            <select className="h-9 rounded-md border bg-transparent px-2 text-sm" value={level} onChange={(e) => setLevel(e.target.value)}>
              <option value="">{t('logs.allLevels')}</option>
              {LEVELS.map((l) => <option key={l} value={l}>{l}</option>)}
            </select>
            <select className="h-9 rounded-md border bg-transparent px-2 text-sm" value={source} onChange={(e) => setSource(e.target.value)}>
              <option value="">{t('logs.allSources')}</option>
              {SOURCES.map((s) => <option key={s} value={s}>{s}</option>)}
            </select>
            <Button size="sm" variant="outline" onClick={() => setAbsTime((v) => !v)} title={absTime ? t('common.relative') : t('common.absolute')}>
              <Clock className="h-4 w-4" />
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-0">
          {latestQ.isLoading ? (
            <div className="space-y-2 p-3">{Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-5 w-full" />)}</div>
          ) : latestQ.isError ? (
            <ErrorState title={t('common.empty')} onRetry={() => latestQ.refetch()} />
          ) : display.length === 0 ? (
            <EmptyState title={t('common.empty')} />
          ) : (
            <>
              <div className="max-h-[60vh] overflow-auto font-mono text-xs">
                {display.map((l) => (
                  <div key={l.id} className="flex items-start gap-2 border-b px-3 py-1.5 hover:bg-muted/50">
                    <span className="shrink-0 text-muted-foreground">{absTime ? fmtAbsTime(l.ts) : timeAgo(l.ts)}</span>
                    <LevelBadge level={l.level} />
                    <span className="shrink-0 text-muted-foreground">{l.source}</span>
                    {l.trace_id && (
                      <button
                        className="shrink-0 text-primary/70 hover:underline"
                        onClick={() => setTrace(l.trace_id)}
                        title={l.trace_id}
                      >
                        {l.trace_id.slice(0, 8)}
                      </button>
                    )}
                    <span className="min-w-0 flex-1 break-all">{l.message}</span>
                    {l.context && <span className="shrink-0 text-muted-foreground/70">{l.context}</span>}
                  </div>
                ))}
              </div>
              {hasMore && display.length > 0 && (
                <div className="border-t p-2 text-center">
                  <Button size="sm" variant="outline" disabled={loadingMore} onClick={loadMore}>
                    <History className="mr-1 h-4 w-4" />
                    {loadingMore ? t('common.loading') : t('logs.loadMore')}
                  </Button>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

// ─── 审计日志（游标分页）──────────────────────────────────────────────

function AuditLogs() {
  const { t } = useTranslation()
  const [absTime, setAbsTime] = useState(false)
  const [older, setOlder] = useState<AuditEntry[]>([])
  const [hasMore, setHasMore] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)

  const latestQ = useQuery({
    queryKey: ['audit-latest'],
    queryFn: () => api.getAudit(`?limit=${PAGE}`),
    refetchInterval: 10_000,
  })
  const latest = latestQ.data?.data ?? []

  const oldest = older.length ? older[older.length - 1] : (latest.length ? latest[latest.length - 1] : null)
  const loadMore = async () => {
    if (!oldest) return
    setLoadingMore(true)
    try {
      const p = new URLSearchParams()
      p.set('before_ts', String(oldest.ts))
      p.set('before_id', String(oldest.id))
      p.set('limit', String(PAGE))
      const r = await api.getAudit('?' + p.toString())
      const next = r.data ?? []
      setOlder((prev) => [...prev, ...next])
      setHasMore(next.length >= PAGE)
    } catch {
      setHasMore(false)
    } finally {
      setLoadingMore(false)
    }
  }

  const seen = new Set<number>()
  const display = [...latest, ...older].filter((e) => {
    if (seen.has(e.id)) return false
    seen.add(e.id)
    return true
  })

  return (
    <Card>
      <CardContent className="p-0">
        <div className="flex justify-end p-2">
          <Button size="sm" variant="outline" onClick={() => setAbsTime((v) => !v)} title={absTime ? t('common.relative') : t('common.absolute')}>
            <Clock className="h-4 w-4" />
          </Button>
        </div>
        {latestQ.isLoading ? (
          <div className="space-y-2 p-3">{Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-5 w-full" />)}</div>
        ) : latestQ.isError ? (
          <ErrorState title={t('common.empty')} onRetry={() => latestQ.refetch()} />
        ) : display.length === 0 ? (
          <EmptyState title={t('common.empty')} />
        ) : (
          <>
            <div className="max-h-[60vh] overflow-auto font-mono text-xs">
              {display.map((e) => (
                <div key={e.id} className="flex items-start gap-2 border-b px-3 py-1.5 hover:bg-muted/50">
                  <span className="shrink-0 text-muted-foreground">{absTime ? fmtAbsTime(e.ts) : timeAgo(e.ts)}</span>
                  <Badge variant="secondary" className="shrink-0">{e.action}</Badge>
                  <span className="shrink-0 text-muted-foreground">{e.actor}</span>
                  <span className="min-w-0 flex-1 break-all text-muted-foreground">{e.detail}</span>
                  <span className="shrink-0 text-muted-foreground/70">{e.ip}</span>
                </div>
              ))}
            </div>
            {hasMore && display.length > 0 && (
              <div className="border-t p-2 text-center">
                <Button size="sm" variant="outline" disabled={loadingMore} onClick={loadMore}>
                  <History className="mr-1 h-4 w-4" />
                  {loadingMore ? t('common.loading') : t('logs.loadMore')}
                </Button>
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}

function LevelBadge({ level }: { level: string }) {
  const v = level === 'error' ? 'destructive' : level === 'warn' ? 'warning' : level === 'debug' ? 'secondary' : 'default'
  return <Badge variant={v as any} className="shrink-0 uppercase">{level}</Badge>
}

// fmtAbsTime 绝对时间 HH:MM:SS（日志中心常需精确时间戳）。
function fmtAbsTime(ts: number): string {
  if (!ts) return '—'
  const d = new Date(ts * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
