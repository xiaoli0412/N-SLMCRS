import { useState, useCallback } from 'react'
import { Database, Download, Trash2, Plus, RefreshCw, ShieldCheck } from 'lucide-react'
import { api, BackupInfo } from '../api'
import { PageHeader, Card, Spinner, EmptyState, Badge, Button } from '../components/ui'

// 字节数格式化（KB/MB）。
function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}

// Unix 秒转本地可读时间。
function fmtTime(ts: number): string {
  if (!ts) return '—'
  return new Date(ts * 1000).toLocaleString('zh-CN', { hour12: false })
}

export default function Backup() {
  const [list, setList] = useState<BackupInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false) // 立即备份/删除进行中
  const [msg, setMsg] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const r = await api.listBackups()
      setList(r.data || [])
    } catch (e: any) {
      setMsg({ kind: 'err', text: e?.message || '加载失败' })
    } finally {
      setLoading(false)
    }
  }, [])

  // 首次加载
  if (loading && list.length === 0 && !busy) {
    refresh()
  }

  const handleCreate = async () => {
    setBusy(true)
    setMsg(null)
    try {
      const r = await api.createBackup()
      setMsg({ kind: 'ok', text: `备份完成：${r.name}` })
      await refresh()
    } catch (e: any) {
      setMsg({ kind: 'err', text: e?.message || '备份失败' })
    } finally {
      setBusy(false)
    }
  }

  const handleDownload = async (file: string) => {
    try {
      const url = await api.downloadBackup(file)
      const a = document.createElement('a')
      a.href = url
      a.download = file
      document.body.appendChild(a)
      a.click()
      a.remove()
      // 释放对象 URL（稍延后以确保下载已触发）
      setTimeout(() => URL.revokeObjectURL(url), 10000)
    } catch (e: any) {
      setMsg({ kind: 'err', text: e?.message || '下载失败' })
    }
  }

  const handleDelete = async (file: string) => {
    if (!confirm(`确认删除备份 ${file}？此操作不可恢复。`)) return
    setBusy(true)
    try {
      await api.deleteBackup(file)
      setMsg({ kind: 'ok', text: `已删除 ${file}` })
      await refresh()
    } catch (e: any) {
      setMsg({ kind: 'err', text: e?.message || '删除失败' })
    } finally {
      setBusy(false)
    }
  }

  const totalSize = list.reduce((s, b) => s + b.size, 0)

  return (
    <>
      <PageHeader title="数据库备份" en="DB Backup" subtitle="SQLite 事务一致快照 · 定时轮转 · 按需下载" />

      {/* 操作栏 + 概要 */}
      <Card className="mb-4 !p-4 animate-slide-up">
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <div className="flex items-center gap-2">
            <Badge variant="info"><ShieldCheck className="w-3 h-3 inline mr-1" />VACUUM INTO 快照</Badge>
            <span className="text-[12px] text-surface-muted">
              共 {list.length} 份 · 合计 {fmtSize(totalSize)}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={refresh} disabled={busy}>
              <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} /> 刷新
            </Button>
            <Button variant="default" size="sm" onClick={handleCreate} disabled={busy}>
              {busy ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />}
              立即备份
            </Button>
          </div>
        </div>
        {msg && (
          <div className={`mt-3 text-[12px] ${msg.kind === 'ok' ? 'text-nv-green' : 'text-red-400'}`}>
            {msg.text}
          </div>
        )}
        <p className="text-[11px] text-surface-muted mt-3 leading-relaxed">
          备份采用 <code className="text-gray-300">VACUUM INTO</code> 在一个事务内产出整库快照，WAL 模式下也保证一致性（无撕裂）。
          定时备份默认每 24h 一次、保留最近 7 份（由 <code className="text-gray-300">BACKUP_INTERVAL</code> / <code className="text-gray-300">BACKUP_RETENTION</code> 环境变量控制）。
          恢复方式：停服 → 替换 <code className="text-gray-300">nslmcrs.db</code> → 重启。
        </p>
      </Card>

      {/* 备份列表 */}
      <Card className="animate-fade-in">
        <div className="text-[13px] font-semibold text-gray-200 mb-3 flex items-center gap-1.5">
          <Database className="w-3.5 h-3.5 text-nv-green" /> 备份文件
        </div>
        {loading ? <Spinner /> : list.length === 0 ? (
          <EmptyState text="暂无备份，点击「立即备份」生成第一份" />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-[12.5px]">
              <thead>
                <tr className="text-surface-muted text-[10.5px] uppercase tracking-wider border-b border-surface-border">
                  <th className="text-left px-3 py-2 font-semibold">文件名</th>
                  <th className="text-right px-3 py-2 font-semibold">大小</th>
                  <th className="text-left px-3 py-2 font-semibold">时间</th>
                  <th className="text-right px-3 py-2 font-semibold">操作</th>
                </tr>
              </thead>
              <tbody>
                {list.map((b) => (
                  <tr key={b.name} className="border-b border-surface-border/60 hover:bg-surface-card-hover">
                    <td className="px-3 py-2.5 font-mono text-[11.5px] text-gray-300">{b.name}</td>
                    <td className="px-3 py-2.5 text-right text-gray-400">{fmtSize(b.size)}</td>
                    <td className="px-3 py-2.5 text-gray-400">{fmtTime(b.mod_time)}</td>
                    <td className="px-3 py-2.5">
                      <div className="flex items-center justify-end gap-1">
                        <button onClick={() => handleDownload(b.name)} disabled={busy}
                          className="btn-ghost text-[11px] flex items-center gap-1 py-1 px-2" title="下载">
                          <Download className="w-3.5 h-3.5" /> 下载
                        </button>
                        <button onClick={() => handleDelete(b.name)} disabled={busy}
                          className="btn-ghost text-[11px] flex items-center gap-1 py-1 px-2 text-red-400 hover:text-red-300" title="删除">
                          <Trash2 className="w-3.5 h-3.5" /> 删除
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </>
  )
}
