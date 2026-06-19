import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PageHeader, Card } from '../components/ui'

// 配置项分类
interface SettingField {
  key: string
  label: string
  desc: string
  type: 'text' | 'number' | 'switch'
  default: string | number | boolean
  group: string
}

const FIELDS: SettingField[] = [
  // 服务
  { key: 'port', label: '监听端口', desc: '网关 HTTP 监听端口', type: 'number', default: 8787, group: '服务' },
  { key: 'admin_token', label: '管理 Token', desc: '访问管理面板所需的 X-Admin-Token', type: 'text', default: '', group: '服务' },
  { key: 'gateway_token', label: '下游网关 Token', desc: '下游客户端调用 /v1/chat/completions 的鉴权 Token', type: 'text', default: '', group: '服务' },
  // 限流
  { key: 'default_rpm', label: '默认 RPM', desc: '每个 NVIDIA 密钥的官方限流（40 RPM）', type: 'number', default: 40, group: '限流调度' },
  { key: 'dispatch_concurrency', label: 'N 路并发度', desc: '热模型同时发起的请求数（先到先得）', type: 'number', default: 3, group: '限流调度' },
  { key: 'circuit_threshold', label: '熔断阈值', desc: '连续失败几次触发熔断', type: 'number', default: 5, group: '限流调度' },
  // 上游
  { key: 'chat_domain', label: 'Chat 域名', desc: '对话补全上游域名', type: 'text', default: 'integrate.api.nvidia.com', group: '上游 NVIDIA' },
  { key: 'retrieval_domain', label: 'Embedding 域名', desc: '向量 / 重排上游域名', type: 'text', default: 'ai.api.nvidia.com', group: '上游 NVIDIA' },
  { key: 'timeout_sec', label: '上游超时', desc: '单次上游请求超时秒数', type: 'number', default: 120, group: '上游 NVIDIA' },
  // 同步
  { key: 'model_sync_interval', label: '模型同步间隔', desc: '从 /v1/models 同步的周期（小时）', type: 'number', default: 24, group: '模型同步' },
  // 运维
  { key: 'log_level', label: '日志级别', desc: '记录到数据库的最低级别', type: 'text', default: 'INFO', group: '运维日志' },
  { key: 'log_retention_days', label: '日志保留', desc: '请求日志保留天数', type: 'number', default: 30, group: '运维日志' },
]

export default function Settings() {
  const { t } = useTranslation()
  const [values, setValues] = useState<Record<string, any>>(
    Object.fromEntries(FIELDS.map((f) => [f.key, f.default]))
  )
  const [saved, setSaved] = useState(false)
  const [tab, setTab] = useState('服务')

  const groups = [...new Set(FIELDS.map((f) => f.group))]
  const visible = FIELDS.filter((f) => f.group === tab)

  const save = () => {
    // 阶段一：仅本地预览，后续接入 /api/admin/settings 持久化
    setSaved(true)
    setTimeout(() => setSaved(false), 2000)
  }

  const set = (k: string, v: any) => setValues({ ...values, [k]: v })

  return (
    <>
      <PageHeader title={t('nav.settings')} en="Settings" subtitle="网关全局配置（阶段一为本地预览，后续将持久化到 settings 表）" />

      <div className="flex gap-5">
        {/* 分组 Tab */}
        <div className="w-[160px] flex-shrink-0">
          <div className="glass-card p-2">
            {groups.map((g) => (
              <button
                key={g}
                onClick={() => setTab(g)}
                className={`w-full text-left px-3 py-2 rounded-lg text-[12.5px] font-medium transition-colors ${
                  tab === g ? 'bg-nv-green/10 text-nv-green border border-nv-green/20' : 'text-gray-400 hover:bg-white/[0.03] border border-transparent'
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
                  <div className="text-[11px] text-gray-600 mt-0.5">{f.desc}</div>
                </div>
                <div className="w-[260px] flex-shrink-0">
                  {f.type === 'switch' ? (
                    <button
                      onClick={() => set(f.key, !values[f.key])}
                      className={`relative w-11 h-6 rounded-full transition-colors ${values[f.key] ? 'bg-nv-green' : 'bg-white/[0.08]'}`}
                    >
                      <span className={`absolute top-0.5 left-0.5 w-5 h-5 bg-white rounded-full transition-transform ${values[f.key] ? 'translate-x-5' : ''}`} />
                    </button>
                  ) : (
                    <input
                      type={f.type === 'number' ? 'number' : 'text'}
                      className="input"
                      value={values[f.key]}
                      onChange={(e) => set(f.key, f.type === 'number' ? +e.target.value : e.target.value)}
                    />
                  )}
                </div>
              </div>
            </Card>
          ))}

          {/* 操作 */}
          <div className="flex justify-end gap-2 pt-2">
            <button onClick={() => setValues(Object.fromEntries(FIELDS.map((f) => [f.key, f.default])))} className="btn-ghost">恢复默认</button>
            <button onClick={save} className="btn-primary">
              {saved ? '✓ 已保存' : t('common.save')}
            </button>
          </div>
        </div>
      </div>
    </>
  )
}
