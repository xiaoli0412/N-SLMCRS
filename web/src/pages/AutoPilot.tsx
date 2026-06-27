import { useEffect, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Bot, BrainCircuit, Gauge, Activity, CheckCircle2, XCircle, Wrench, Eye } from 'lucide-react'
import { PageHeader, Card, KpiCard, Spinner, EmptyState, Button, Badge, StatusBadge } from '../components/ui'
import {
  api,
  AutoPilotMode,
  AutoPilotEngine,
  AutoPilotState,
  AutoPilotSnapshot,
  PendingEntry,
  AutoPilotAction,
  StepTrace,
} from '../api'

const MODES = [
  { id: 'manual', name: '手动模式', en: 'Manual', icon: '✋', desc: '完全人工操作，调度器仅按权重轮询，不自动干预' },
  { id: 'assisted', name: '辅助模式', en: 'Assisted', icon: '🤝', desc: '可逆调参（并发/权重）即时生效；破坏性动作进待审队列' },
  { id: 'fullauto', name: '全自动模式', en: 'Full-Auto', icon: '🚀', desc: 'AI 全权接管密钥启停/限流回退/熔断（破坏性动作需置信度≥0.7）' },
] as const

const ENGINES = [
  { id: 'adaptive', name: '自适应算法', en: 'Adaptive', tag: 'PID · EWMA', desc: 'EWMA + PID 反馈，按成功率/限流动态调整权重与并发度。', color: '#76b900' },
  { id: 'predict', name: '轻量预测', en: 'Forecast', tag: 'Holt-Winters', desc: '三次指数平滑预测流量，提前预防限流窗口。', color: '#38bdf8' },
  { id: 'llm', name: 'LLM 决策', en: 'LLM Agent', tag: 'ReAct', desc: '真 agent 循环：LLM 思考→调用调度工具→读取观测→收手，产出可调试推理轨迹。', color: '#a855f7' },
] as const

const POLL_MS = 5000

// 推理轨迹单步图标
function traceIcon(role: string) {
  if (role === 'think') return <BrainCircuit className="w-3.5 h-3.5 text-purple-400" />
  if (role === 'act') return <Wrench className="w-3.5 h-3.5 text-sky-400" />
  if (role === 'observe') return <Eye className="w-3.5 h-3.5 text-gray-400" />
  return <Activity className="w-3.5 h-3.5" />
}

// tierVariant 客户端并发档位 → Badge 配色（越激进越醒目）。
function tierVariant(tier: string): 'default' | 'success' | 'warn' | 'danger' {
  if (tier.startsWith('peak')) return 'danger'
  if (tier.startsWith('high')) return 'warn'
  if (tier.startsWith('mid')) return 'success'
  return 'default'
}

export default function AutoPilot() {
  const { t } = useTranslation()
  const [state, setState] = useState<AutoPilotState | null>(null)
  const [snap, setSnap] = useState<AutoPilotSnapshot | null>(null)
  const [pending, setPending] = useState<PendingEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const [s, p] = await Promise.all([api.getAutopilotState(), api.listPending()])
      setState(s)
      setPending(p.data || [])
      setError('')
      // 快照（调试用，失败不阻塞）
      api.getAutopilotSnapshot().then(setSnap).catch(() => {})
    } catch (e: any) {
      setError(e?.message || String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    refresh()
    const id = setInterval(refresh, POLL_MS)
    return () => clearInterval(id)
  }, [refresh])

  const mode = (state?.Mode as AutoPilotMode) || 'assisted'
  const engine = (state?.Engine as AutoPilotEngine) || 'adaptive'
  const llmMode = state?.LLMBackendMode || ''
  const trace: StepTrace[] = state?.RecentTrace || []

  const apply = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await fn()
      await refresh()
    } catch (e: any) {
      setError(e?.message || String(e))
    } finally {
      setBusy(false)
    }
  }

  const onMode = (m: string) => apply(() => api.setAutopilotMode(m as AutoPilotMode))
  const onEngine = (e: string) => apply(() => api.setAutopilotEngine(e as AutoPilotEngine))
  const onApprove = (key: string) => apply(() => api.approvePending(key))
  const onReject = (key: string) => apply(() => api.rejectPending(key))

  return (
    <>
      <PageHeader title={t('nav.autopilot')} en="Auto-Pilot" subtitle="三模式 · 三引擎智能调度，LLM 引擎已 agent 化（ReAct 推理可调试）" />

      {error && (
        <div className="mb-3 px-4 py-2.5 rounded-lg border border-red-500/30 bg-red-500/10 text-[12px] text-red-300">
          {error}
        </div>
      )}

      {/* 运行模式 */}
      <div className="mb-4">
        <div className="text-[12px] font-semibold text-surface-muted mb-2 uppercase tracking-wider">运行模式 · Operation Mode</div>
        <div className="grid grid-cols-3 gap-3">
          {MODES.map((m) => (
            <button
              key={m.id}
              disabled={busy}
              onClick={() => onMode(m.id)}
              className={`card p-4 text-left transition-all disabled:opacity-50 hover:border-surface-border-hover ${
                mode === m.id ? 'border-nv-green/50 bg-nv-green/[0.04]' : ''
              }`}
            >
              <div className="flex items-center gap-2 mb-1.5">
                <span className="text-[20px]">{m.icon}</span>
                <div>
                  <div className={`text-[14px] font-bold ${mode === m.id ? 'text-nv-green' : 'text-gray-200'}`}>{m.name}</div>
                  <div className="text-[10px] text-surface-muted">{m.en}</div>
                </div>
              </div>
              <div className="text-[11.5px] text-surface-muted leading-relaxed">{m.desc}</div>
            </button>
          ))}
        </div>
      </div>

      {/* 引擎选择 */}
      <div className="mb-4">
        <div className="text-[12px] font-semibold text-surface-muted mb-2 uppercase tracking-wider">调度引擎 · Scheduling Engine</div>
        <div className="grid grid-cols-3 gap-3">
          {ENGINES.map((e) => (
            <button
              key={e.id}
              disabled={busy}
              onClick={() => onEngine(e.id)}
              className={`card p-4 text-left transition-all disabled:opacity-50 hover:border-surface-border-hover ${
                engine === e.id ? 'border-nv-green/50 bg-nv-green/[0.04]' : ''
              }`}
            >
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <span className="text-[20px]">{e.id === 'llm' ? '🧠' : e.id === 'predict' ? '📊' : '⚖'}</span>
                  <div>
                    <div className={`text-[14px] font-bold ${engine === e.id ? 'text-nv-green' : 'text-gray-200'}`}>{e.name}</div>
                    <div className="text-[10px] text-surface-muted">{e.en}</div>
                  </div>
                </div>
                <div className="flex items-center gap-1.5">
                  {e.id === 'llm' && llmMode && (
                    <Badge variant={llmMode === 'gateway' ? 'success' : 'warn'}>
                      {llmMode === 'gateway' ? <Bot className="w-3 h-3" /> : <BrainCircuit className="w-3 h-3" />}
                      {llmMode}
                    </Badge>
                  )}
                  <span className="text-[9px] px-1.5 py-0.5 rounded font-mono border" style={{
                    color: e.color, borderColor: `${e.color}40`, background: `${e.color}10`,
                  }}>{e.tag}</span>
                </div>
              </div>
              <div className="text-[11px] text-surface-muted leading-relaxed">{e.desc}</div>
            </button>
          ))}
        </div>
      </div>

      {/* 密钥健康快照（真实 snapshot，替代旧版假雷达）+ 当前状态 */}
      <div className="grid grid-cols-3 gap-3.5">
        <Card className="col-span-2">
          <div className="flex items-center gap-2 mb-3">
            <Gauge className="w-4 h-4 text-nv-green" />
            <div className="text-[13px] font-semibold text-gray-200">
              密钥健康快照 <span className="text-surface-muted text-[11px] font-normal">/ Live Snapshot（决策输入）</span>
            </div>
          </div>
          {!snap || !snap.Keys || snap.Keys.length === 0 ? (
            <EmptyState text="暂无密钥快照（需先添加上游密钥）" />
          ) : (
            <div className="space-y-2">
              {snap.Keys.map((k) => {
                const rate = Math.round((k.SuccessRate || 0) * 100)
                return (
                  <div key={k.ID} className="flex items-center gap-3 text-[11.5px]">
                    <span className="font-mono text-gray-300 w-[120px] truncate" title={k.Mask}>{k.Mask}</span>
                    <StatusBadge status={k.Status} />
                    <div className="flex-1 h-1.5 rounded-full bg-surface-card-hover overflow-hidden">
                      <div
                        className={`h-full rounded-full ${rate >= 80 ? 'bg-nv-green' : rate >= 50 ? 'bg-amber-400' : 'bg-red-500'}`}
                        style={{ width: `${rate}%` }}
                      />
                    </div>
                    <span className="text-surface-muted w-10 text-right">{rate}%</span>
                    <span className="text-surface-muted w-16 text-right">失败 {k.ConsecFail}</span>
                    <span className="text-surface-muted w-16 text-right">RPM {k.RPMRemaining}</span>
                  </div>
                )
              })}
            </div>
          )}
        </Card>

        <div className="space-y-3">
          <Card>
            <div className="text-[11px] text-surface-muted mb-1">当前策略</div>
            {loading ? (
              <Spinner />
            ) : state ? (
              <>
                <div className="text-[15px] font-bold text-nv-green">
                  {MODES.find((m) => m.id === mode)?.name} · {ENGINES.find((e) => e.id === engine)?.name}
                </div>
                <div className="text-[10.5px] text-surface-muted mt-1">
                  {mode === 'manual' && 'AI 仅观察，不执行任何自动变更'}
                  {mode === 'assisted' && '可逆调参即时生效；破坏性动作进待审队列'}
                  {mode === 'fullauto' && 'AI 自动启停密钥、调整权重、触发熔断'}
                </div>
                <div className="mt-2 pt-2 border-t border-surface-border text-[10.5px] text-surface-muted flex justify-between">
                  <span>并发度</span>
                  <span className="text-gray-300 font-mono">
                    {state.RuntimeConcurrency > 0 ? state.RuntimeConcurrency : `${state.DefaultConcurrency}(默认)`}
                    <span className="text-surface-muted"> / 上限 {state.MaxConcurrency}</span>
                  </span>
                </div>
                {/* v0.7：实时负载画像（客户端并发档位 + 可用 key 数 + 在途） */}
                <div className="mt-1 text-[10.5px] text-surface-muted flex justify-between">
                  <span>负载档位</span>
                  <span className="flex items-center gap-1.5">
                    <Badge variant={tierVariant(state.ClientConcurrencyTier)}>{state.ClientConcurrencyTier || 'unknown'}</Badge>
                    <span className="text-gray-400 font-mono">在途 {state.InflightRequests} · key {state.AvailableKeyCount}</span>
                  </span>
                </div>
                {llmMode && (
                  <div className="mt-1 text-[10.5px] text-surface-muted flex justify-between">
                    <span>LLM 后端</span>
                    <Badge variant={llmMode === 'gateway' ? 'success' : 'warn'}>{llmMode}</Badge>
                  </div>
                )}
              </>
            ) : (
              <div className="text-[12px] text-surface-muted">无数据</div>
            )}
          </Card>
          <div className="grid grid-cols-2 gap-2">
            <KpiCard label="调度决策" value={state?.DecisionsPerMin ?? 0} unit="/分" />
            <KpiCard label="自动干预" value={state?.Interventions ?? 0} accent />
          </div>
          <Card className="p-4">
            <div className="text-[11px] text-surface-muted mb-2">最近事件</div>
            {state && state.RecentEvents && state.RecentEvents.length > 0 ? (
              <ul className="space-y-1.5 max-h-[140px] overflow-auto">
                {state.RecentEvents.map((ev, i) => (
                  <li key={i} className="text-[11px] leading-snug">
                    <div className="flex items-center gap-1.5">
                      <span className={`w-1.5 h-1.5 rounded-full ${ev.Applied ? 'bg-nv-green' : 'bg-gray-600'}`} />
                      <span className="text-gray-300">{ev.Kind}</span>
                      <span className="text-surface-muted text-[10px]">{new Date(ev.TS * 1000).toLocaleTimeString()}</span>
                    </div>
                    <div className="text-surface-muted pl-3 truncate" title={ev.Reason}>{ev.Reason}</div>
                  </li>
                ))}
              </ul>
            ) : (
              <div className="text-[11px] text-surface-muted text-center py-3">暂无自动调度事件</div>
            )}
          </Card>
        </div>
      </div>

      {/* LLM 推理轨迹（agent 化可调试） */}
      {engine === 'llm' && trace.length > 0 && (
        <div className="mt-4">
          <div className="flex items-center gap-2 mb-2">
            <BrainCircuit className="w-4 h-4 text-purple-400" />
            <div className="text-[12px] font-semibold text-surface-muted uppercase tracking-wider">
              推理轨迹 · Reasoning Trace（{trace.length} 步）
            </div>
          </div>
          <Card className="p-4">
            <ol className="space-y-2">
              {trace.map((st, i) => (
                <li key={i} className="flex items-start gap-2 text-[11.5px]">
                  <span className="mt-0.5 shrink-0">{traceIcon(st.Role)}</span>
                  <div className="min-w-0">
                    <span className="text-surface-muted font-mono text-[10px] mr-1.5">
                      #{st.Step} {st.Role}
                      {st.ToolName && ` · ${st.ToolName}`}
                    </span>
                    <span className={`leading-snug ${st.Role === 'think' ? 'text-purple-300' : st.Role === 'act' ? 'text-sky-300' : 'text-gray-400'}`}>
                      {st.Error ? <span className="text-red-400">⚠ {st.Error}</span> : st.Content}
                    </span>
                  </div>
                </li>
              ))}
            </ol>
          </Card>
        </div>
      )}

      {/* assisted 待审建议 */}
      {mode === 'assisted' && (
        <div className="mt-4">
          <div className="text-[12px] font-semibold text-surface-muted mb-2 uppercase tracking-wider">
            待审建议 · Pending ({pending.length})
          </div>
          {pending.length === 0 ? (
            <EmptyState text="暂无待确认建议" />
          ) : (
            <div className="space-y-2">
              {pending.map((p) => {
                let act: AutoPilotAction | null = null
                try {
                  act = JSON.parse(p.Value) as AutoPilotAction
                } catch {
                  act = null
                }
                return (
                  <div key={p.Key} className="card p-3 flex items-center justify-between gap-3">
                    <div className="min-w-0">
                      <div className="text-[12px] text-gray-200 font-mono">{act?.Kind || '未知动作'}</div>
                      <div className="text-[11px] text-surface-muted truncate" title={act?.Reason}>{act?.Reason || p.Value}</div>
                    </div>
                    <div className="flex gap-2 shrink-0">
                      <Button variant="subtle" size="sm" disabled={busy} onClick={() => onApprove(p.Key)}>
                        <CheckCircle2 className="w-3.5 h-3.5" />批准
                      </Button>
                      <Button variant="destructive" size="sm" disabled={busy} onClick={() => onReject(p.Key)}>
                        <XCircle className="w-3.5 h-3.5" />驳回
                      </Button>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}
    </>
  )
}
