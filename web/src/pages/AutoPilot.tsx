import { useEffect, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { RadarChart, PolarGrid, PolarAngleAxis, PolarRadiusAxis, Radar, ResponsiveContainer } from 'recharts'
import { PageHeader, Card, KpiCard, Spinner, EmptyState } from '../components/ui'
import {
  api,
  AutoPilotMode,
  AutoPilotEngine,
  AutoPilotState,
  PendingEntry,
  AutoPilotAction,
} from '../api'

// 三种模式：手动 / 辅助 / 全自动
const MODES = [
  { id: 'manual', name: '手动模式', en: 'Manual', icon: '✋', desc: '完全人工操作，调度器仅按权重轮询，不自动干预' },
  { id: 'assisted', name: '辅助模式', en: 'Assisted', icon: '🤝', desc: '可逆调参（并发/权重）即时生效；破坏性动作（禁用/熔断/吊销）进待审队列' },
  { id: 'fullauto', name: '全自动模式', en: 'Full-Auto', icon: '🚀', desc: 'AI 全权接管密钥启停 / 限流回退 / 熔断，零干预（破坏性动作需置信度≥0.7）' },
] as const

// 三种引擎：自适应算法 / 轻量预测 / LLM 决策
const ENGINES = [
  {
    id: 'adaptive', name: '自适应算法', en: 'Adaptive', icon: '⚖',
    tag: 'PID · EWMA',
    desc: '基于指数加权移动平均与 PID 反馈控制器，实时根据成功率/限流率动态调整每密钥权重与并发度。',
    pros: ['响应快（毫秒级）', '资源占用极低', '确定性可解释', '无需外部依赖'],
    fit: '稳态流量调度',
    color: '#76b900',
  },
  {
    id: 'predict', name: '轻量预测', en: 'Forecast', icon: '📊',
    tag: 'Holt-Winters',
    desc: '基于 Holt-Winters 三次指数平滑，对历史 24h 流量序列建模，提前预测限流窗口与密钥冷却需求。',
    pros: ['前瞻性预防限流', '可预测 Token 用量', '无需 GPU', '周期性场景优秀'],
    fit: '高峰预测 / 资源预算',
    color: '#38bdf8',
  },
  {
    id: 'llm', name: 'LLM 决策', en: 'LLM Brain', icon: '🧠',
    tag: 'Reasoning',
    desc: '调用大模型对当前全局面板做语义推理，给出策略解释与异常根因分析，并生成执行计划。',
    pros: ['可解释复杂决策', '处理未见过的异常', '自然语言审计', '策略可人工复核'],
    fit: '复杂故障 / 根因分析',
    color: '#a855f7',
  },
] as const

const POLL_MS = 5000

export default function AutoPilot() {
  const { t } = useTranslation()
  const [state, setState] = useState<AutoPilotState | null>(null)
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

  // 模拟雷达图：各引擎能力评估
  const radar = [
    { dim: '响应速度', adaptive: 95, predict: 70, llm: 30 },
    { dim: '前瞻性', adaptive: 40, predict: 90, llm: 65 },
    { dim: '可解释', adaptive: 75, predict: 60, llm: 98 },
    { dim: '资源占用', adaptive: 95, predict: 80, llm: 25 },
    { dim: '处理复杂度', adaptive: 50, predict: 60, llm: 95 },
    { dim: '稳定性', adaptive: 90, predict: 85, llm: 70 },
  ]

  return (
    <>
      <PageHeader title={t('nav.autopilot')} en="Auto-Pilot" subtitle="三模式 · 三引擎智能调度，可接管整个网关服务" />

      {error && (
        <div className="mb-3 px-4 py-2.5 rounded-lg border border-red-500/30 bg-red-500/10 text-[12px] text-red-300">
          {error}
        </div>
      )}

      {/* 运行模式切换 */}
      <div className="mb-4">
        <div className="text-[12px] font-semibold text-gray-400 mb-2 uppercase tracking-wider">运行模式 · Operation Mode</div>
        <div className="grid grid-cols-3 gap-3">
          {MODES.map((m) => (
            <button
              key={m.id}
              disabled={busy}
              onClick={() => onMode(m.id)}
              className={`p-4 rounded-xl border text-left transition-all disabled:opacity-50 ${
                mode === m.id
                  ? 'border-nv-green bg-nv-green/[0.06] shadow-nv-glow'
                  : 'border-white/[0.06] bg-white/[0.015] hover:border-white/[0.12]'
              }`}
            >
              <div className="flex items-center gap-2 mb-1.5">
                <span className="text-[20px]">{m.icon}</span>
                <div>
                  <div className={`text-[14px] font-bold ${mode === m.id ? 'text-nv-green' : 'text-gray-200'}`}>{m.name}</div>
                  <div className="text-[10px] text-gray-600">{m.en}</div>
                </div>
              </div>
              <div className="text-[11.5px] text-gray-500 leading-relaxed">{m.desc}</div>
            </button>
          ))}
        </div>
      </div>

      {/* 引擎选择 */}
      <div className="mb-4">
        <div className="text-[12px] font-semibold text-gray-400 mb-2 uppercase tracking-wider">调度引擎 · Scheduling Engine</div>
        <div className="grid grid-cols-3 gap-3">
          {ENGINES.map((e) => (
            <button
              key={e.id}
              disabled={busy}
              onClick={() => onEngine(e.id)}
              className={`p-4 rounded-xl border text-left transition-all disabled:opacity-50 ${
                engine === e.id ? 'border-nv-green bg-nv-green/[0.06] shadow-nv-glow' : 'border-white/[0.06] bg-white/[0.015] hover:border-white/[0.12]'
              }`}
            >
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <span className="text-[20px]">{e.icon}</span>
                  <div>
                    <div className={`text-[14px] font-bold ${engine === e.id ? 'text-nv-green' : 'text-gray-200'}`}>{e.name}</div>
                    <div className="text-[10px] text-gray-600">{e.en}</div>
                  </div>
                </div>
                <span className="text-[9px] px-1.5 py-0.5 rounded font-mono border" style={{
                  color: e.color, borderColor: `${e.color}40`, background: `${e.color}10`,
                }}>{e.tag}</span>
              </div>
              <div className="text-[11px] text-gray-500 leading-relaxed mb-2">{e.desc}</div>
              <ul className="space-y-0.5">
                {e.pros.map((p, i) => (
                  <li key={i} className="text-[10.5px] text-gray-400 flex items-center gap-1">
                    <span style={{ color: e.color }}>✓</span> {p}
                  </li>
                ))}
              </ul>
              <div className="mt-2 pt-2 border-t border-white/[0.04] text-[10px] text-gray-600">
                适用：<span className="text-gray-400">{e.fit}</span>
              </div>
            </button>
          ))}
        </div>
      </div>

      {/* 引擎能力雷达对比 + 当前状态 */}
      <div className="grid grid-cols-3 gap-3.5">
        <Card className="col-span-2">
          <div className="text-[13px] font-semibold text-gray-200 mb-3">
            引擎能力对比 <span className="text-gray-600 text-[11px] font-normal">/ Engine Capabilities</span>
          </div>
          <div className="h-[280px]">
            <ResponsiveContainer width="100%" height="100%">
              <RadarChart data={radar}>
                <PolarGrid stroke="rgba(255,255,255,0.08)" />
                <PolarAngleAxis dataKey="dim" tick={{ fill: '#888', fontSize: 11 }} />
                <PolarRadiusAxis domain={[0, 100]} tick={{ fill: '#444', fontSize: 9 }} stroke="rgba(255,255,255,0.05)" />
                <Radar name="自适应" dataKey="adaptive" stroke="#76b900" fill="#76b900" fillOpacity={0.25} strokeWidth={2} />
                <Radar name="预测" dataKey="predict" stroke="#38bdf8" fill="#38bdf8" fillOpacity={0.15} strokeWidth={2} />
                <Radar name="LLM" dataKey="llm" stroke="#a855f7" fill="#a855f7" fillOpacity={0.15} strokeWidth={2} />
              </RadarChart>
            </ResponsiveContainer>
          </div>
          <div className="flex justify-center gap-4 text-[11px] mt-1">
            <span className="flex items-center gap-1.5"><span className="w-2 h-2 rounded-full bg-nv-green" />自适应</span>
            <span className="flex items-center gap-1.5"><span className="w-2 h-2 rounded-full bg-[#38bdf8]" />预测</span>
            <span className="flex items-center gap-1.5"><span className="w-2 h-2 rounded-full bg-[#a855f7]" />LLM</span>
          </div>
        </Card>

        <div className="space-y-3">
          <Card>
            <div className="text-[11px] text-gray-500 mb-1">当前策略</div>
            {loading ? (
              <Spinner />
            ) : state ? (
              <>
                <div className="text-[15px] font-bold text-nv-green">
                  {MODES.find((m) => m.id === mode)?.name} · {ENGINES.find((e) => e.id === engine)?.name}
                </div>
                <div className="text-[10.5px] text-gray-600 mt-1">
                  {mode === 'manual' && 'AI 仅观察，不执行任何自动变更'}
                  {mode === 'assisted' && '可逆调参即时生效；破坏性动作进待审队列等人工确认'}
                  {mode === 'fullauto' && 'AI 将自动启停密钥、调整权重、触发熔断'}
                </div>
                <div className="mt-2 pt-2 border-t border-white/[0.04] text-[10.5px] text-gray-500 flex justify-between">
                  <span>并发度</span>
                  <span className="text-gray-300 font-mono">
                    {state.RuntimeConcurrency > 0 ? state.RuntimeConcurrency : `${state.DefaultConcurrency}(默认)`}
                    <span className="text-gray-600"> / 上限 {state.MaxConcurrency}</span>
                  </span>
                </div>
              </>
            ) : (
              <div className="text-[12px] text-gray-600">无数据</div>
            )}
          </Card>
          <div className="grid grid-cols-2 gap-2">
            <KpiCard label="调度决策" value={state?.DecisionsPerMin ?? 0} unit="/分" />
            <KpiCard label="自动干预" value={state?.Interventions ?? 0} accent />
          </div>
          <div className="glass-card p-4">
            <div className="text-[11px] text-gray-500 mb-2">最近事件</div>
            {state && state.RecentEvents && state.RecentEvents.length > 0 ? (
              <ul className="space-y-1.5 max-h-[140px] overflow-auto">
                {state.RecentEvents.map((ev, i) => (
                  <li key={i} className="text-[11px] leading-snug">
                    <div className="flex items-center gap-1.5">
                      <span className={`w-1.5 h-1.5 rounded-full ${ev.Applied ? 'bg-nv-green' : 'bg-gray-600'}`} />
                      <span className="text-gray-300">{ev.Kind}</span>
                      <span className="text-gray-600 text-[10px]">{new Date(ev.TS * 1000).toLocaleTimeString()}</span>
                    </div>
                    <div className="text-gray-500 pl-3 truncate" title={ev.Reason}>{ev.Reason}</div>
                  </li>
                ))}
              </ul>
            ) : (
              <div className="text-[11px] text-gray-600 text-center py-3">暂无自动调度事件</div>
            )}
          </div>
        </div>
      </div>

      {/* assisted 待审建议 */}
      {mode === 'assisted' && (
        <div className="mt-4">
          <div className="text-[12px] font-semibold text-gray-400 mb-2 uppercase tracking-wider">
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
                  <div key={p.Key} className="glass-card p-3 flex items-center justify-between gap-3">
                    <div className="min-w-0">
                      <div className="text-[12px] text-gray-200 font-mono">{act?.Kind || '未知动作'}</div>
                      <div className="text-[11px] text-gray-500 truncate" title={act?.Reason}>{act?.Reason || p.Value}</div>
                    </div>
                    <div className="flex gap-2 shrink-0">
                      <button
                        disabled={busy}
                        onClick={() => onApprove(p.Key)}
                        className="px-3 py-1 rounded text-[11px] font-semibold bg-nv-green/15 text-nv-green border border-nv-green/30 hover:bg-nv-green/25 disabled:opacity-50"
                      >批准</button>
                      <button
                        disabled={busy}
                        onClick={() => onReject(p.Key)}
                        className="px-3 py-1 rounded text-[11px] font-semibold bg-white/[0.04] text-gray-400 border border-white/[0.08] hover:bg-white/[0.08] disabled:opacity-50"
                      >驳回</button>
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
