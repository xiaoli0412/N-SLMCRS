import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api'
import { PageHeader, Spinner, EmptyState, Card } from '../components/ui'

const LEVELS = ['', 'INFO', 'WARN', 'ERROR']
const SOURCES = ['', 'entry', 'scheduler', 'ratelimit', 'upstream', 'modelmeta', 'admin']

export default function Logs() {
  const { t } = useTranslation()
  const [logs, setLogs] = useState<any[]>([])
  const [loading, setLoading] = useState(true)
  const [traceId, setTraceId] = useState('')
  const [level, setLevel] = useState('')
  const [source, setSource] = useState('')
  const [limit] = useState(200)

  const load = async () => {
    try {
      const params = new URLSearchParams()
      if (traceId) params.set('trace_id', traceId)
      if (level) params.set('level', level)
      if (source) params.set('source', source)
      params.set('limit', String(limit))
      const r = await api.getLogs(`?${params.toString()}`)
      setLogs(r.data || [])
    } catch { /* */ }
    setLoading(false)
  }

  useEffect(() => {
    const id = setTimeout(load, 100)
    return () => clearTimeout(id)
    // eslint-disable-next-line
  }, [level, source])

  const levelColor = (lv: string) =>
    lv === 'ERROR' ? 'text-red-400 bg-red-500/10 border-red-500/20'
    : lv === 'WARN' ? 'text-amber-400 bg-amber-400/10 border-amber-400/20'
    : 'text-nv-green bg-nv-green/10 border-nv-green/20'

  return (
    <>
      <PageHeader title={t('nav.logs')} en="Logs" subtitle="全链路追踪 · 按 trace_id / 级别 / 来源筛选" />

      {/* 过滤器 */}
      <Card className="mb-4">
        <div className="grid grid-cols-4 gap-3">
          <div>
            <label className="text-[11px] text-surface-muted">Trace ID</label>
            <input
              className="input mt-1"
              value={traceId}
              onChange={(e) => setTraceId(e.target.value)}
              placeholder="输入 trace_id 精确查询"
            />
          </div>
          <div>
            <label className="text-[11px] text-surface-muted">级别</label>
            <select className="input mt-1" value={level} onChange={(e) => setLevel(e.target.value)}>
              {LEVELS.map((l) => <option key={l} value={l}>{l || '全部'}</option>)}
            </select>
          </div>
          <div>
            <label className="text-[11px] text-surface-muted">来源</label>
            <select className="input mt-1" value={source} onChange={(e) => setSource(e.target.value)}>
              {SOURCES.map((s) => <option key={s} value={s}>{s || '全部'}</option>)}
            </select>
          </div>
          <div className="flex items-end gap-2">
            <button onClick={load} className="btn-primary flex-1">查询</button>
            <button onClick={() => { setTraceId(''); setLevel(''); setSource(''); setTimeout(load, 0) }} className="btn-ghost">重置</button>
          </div>
        </div>
      </Card>

      {/* 日志列表 */}
      {loading ? <Spinner /> : logs.length === 0 ? <EmptyState text="未找到匹配的日志记录" /> : (
        <div className="card overflow-hidden">
          <div className="max-h-[calc(100vh-280px)] overflow-y-auto">
            <table className="w-full text-[12px]">
              <thead className="sticky top-0">
                <tr className="bg-surface-card text-surface-muted text-[10.5px] uppercase tracking-wider border-b border-surface-border">
                  <th className="text-left px-3 py-2 font-semibold w-[150px]">时间</th>
                  <th className="text-left px-3 py-2 font-semibold w-[70px]">级别</th>
                  <th className="text-left px-3 py-2 font-semibold w-[90px]">来源</th>
                  <th className="text-left px-3 py-2 font-semibold w-[140px]">Trace ID</th>
                  <th className="text-left px-3 py-2 font-semibold">消息</th>
                </tr>
              </thead>
              <tbody>
                {logs.map((l, i) => (
                  <tr key={i} className="border-b border-surface-border/60 hover:bg-surface-card-hover">
                    <td className="px-3 py-2 text-surface-muted font-mono text-[11px] whitespace-nowrap">
                      {l.created_at ? new Date(l.created_at).toLocaleString('zh-CN', { hour12: false }) : '—'}
                    </td>
                    <td className="px-3 py-2">
                      <span className={`inline-block px-1.5 py-0.5 rounded text-[10px] border ${levelColor(l.level)}`}>
                        {l.level}
                      </span>
                    </td>
                    <td className="px-3 py-2 text-gray-400 font-mono text-[11px]">{l.source}</td>
                    <td className="px-3 py-2 text-surface-muted font-mono text-[10.5px]">{l.trace_id?.slice(0, 16) || '—'}</td>
                    <td className="px-3 py-2 text-gray-300">{l.message}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </>
  )
}
