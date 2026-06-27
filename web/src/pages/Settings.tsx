import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, SchedulerSettings } from '../api'
import { PageHeader, Card } from '../components/ui'

// 配置字段定义：驱动渲染与提交。时长字段以「秒」与后端契约对齐。
interface SettingField {
  key: keyof SchedulerSettings
  label: string
  desc: string
  group: string
}

const FIELDS: SettingField[] = [
  // 限流调度
  { key: 'default_concurrency', label: '默认并发度', desc: 'N 路并发的先到先得路数（热模型同时发起的请求数）', group: '调度' },
  { key: 'max_concurrency', label: '最大并发上限', desc: '并发上限，防止失控（必须 ≥ 默认并发度）', group: '调度' },
  { key: 'request_timeout_sec', label: '请求超时（秒）', desc: '整体请求超时（含重试与 N 路等待）', group: '调度' },
  // 熔断
  { key: 'circuit_threshold', label: '熔断阈值', desc: '连续失败几次触发熔断', group: '熔断' },
  { key: 'circuit_cooldown_sec', label: '熔断冷却（秒）', desc: '初始冷却时长，触发后按指数退避（上限 10 分钟）', group: '熔断' },
]

export default function Settings() {
  const { t } = useTranslation()
  const [orig, setOrig] = useState<SchedulerSettings | null>(null) // 服务端当前值（恢复用）
  const [values, setValues] = useState<SchedulerSettings>({
    circuit_threshold: 5,
    circuit_cooldown_sec: 30,
    default_concurrency: 5,
    max_concurrency: 10,
    request_timeout_sec: 180,
  })
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')
  const [tab, setTab] = useState('调度')

  const load = async () => {
    try {
      const s = await api.getSettings()
      setOrig(s)
      setValues(s)
    } catch (e: any) {
      setError(e.message || '加载失败')
    }
    setLoading(false)
  }

  useEffect(() => { load() }, [])

  const groups = [...new Set(FIELDS.map((f) => f.group))]
  const visible = FIELDS.filter((f) => f.group === tab)

  // 是否有字段被改动（与 orig 对比）
  const dirty = orig !== null && FIELDS.some((f) => values[f.key] !== orig[f.key])

  const set = (k: keyof SchedulerSettings, v: number) => setValues({ ...values, [k]: v })

  const save = async () => {
    setError('')
    setSaving(true)
    try {
      const r = await api.putSettings(values)
      const next = r.settings
      setOrig(next)
      setValues(next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } catch (e: any) {
      setError(e.message || '保存失败')
    }
    setSaving(false)
  }

  const reset = () => {
    if (orig) setValues(orig)
    setError('')
  }

  return (
    <>
      <PageHeader title={t('nav.settings')} en="Settings" subtitle="熔断 / 调度运行时配置，保存后即时热生效并持久化（重启不丢失）" />

      {loading ? (
        <div className="text-surface-muted text-sm py-8 text-center">加载中…</div>
      ) : (
        <div className="flex gap-5">
          {/* 分组 Tab */}
          <div className="w-[160px] flex-shrink-0">
            <div className="card p-2">
              {groups.map((g) => (
                <button
                  key={g}
                  onClick={() => setTab(g)}
                  className={`w-full text-left px-3 py-2 rounded-lg text-[12.5px] font-medium transition-colors ${
                    tab === g ? 'bg-nv-green/10 text-nv-green border border-nv-green/20' : 'text-gray-400 hover:bg-surface-card-hover border border-transparent'
                  }`}
                >
                  {g}
                </button>
              ))}
            </div>
          </div>

          {/* 字段 */}
          <div className="flex-1 space-y-3">
            {visible.map((f) => (
              <Card key={f.key} className="!p-4">
                <div className="flex items-center justify-between gap-4">
                  <div className="flex-1">
                    <div className="text-[13px] font-semibold text-gray-200">{f.label}</div>
                    <div className="text-[11px] text-surface-muted mt-0.5">{f.desc}</div>
                  </div>
                  <div className="w-[120px] flex-shrink-0">
                    <input
                      type="number"
                      className="input"
                      value={values[f.key]}
                      onChange={(e) => set(f.key, +e.target.value)}
                    />
                  </div>
                </div>
              </Card>
            ))}

            {/* 操作 */}
            <div className="flex items-center justify-between pt-2">
              <div className="text-[11px] text-red-400 min-h-[16px]">{error}</div>
              <div className="flex gap-2">
                <button onClick={reset} disabled={!dirty || saving} className="btn-ghost disabled:opacity-40">
                  恢复原值
                </button>
                <button onClick={save} disabled={!dirty || saving} className="btn-primary disabled:opacity-40">
                  {saving ? t('common.saving') : saved ? '✓ 已保存' : t('common.save')}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
