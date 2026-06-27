// API 客户端：与后端 /api/admin 交互。
// adminToken 从 localStorage 读取（管理面板登录时存入）。

const TOKEN_KEY = 'admin_token'

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setToken(t: string) {
  localStorage.setItem(TOKEN_KEY, t)
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  const t = getToken()
  if (t) headers['X-Admin-Token'] = t
  const res = await fetch(path, { ...opts, headers: { ...headers, ...(opts.headers as any) } })
  if (!res.ok) {
    const txt = await res.text().catch(() => res.statusText)
    throw new Error(`${res.status}: ${txt}`)
  }
  return res.json()
}

export interface UpstreamKey {
  id: number
  key_mask: string
  label: string
  email: string
  rpm_override: number
  enabled: boolean
  status: string
  consecutive_fail: number
}

// 批量导入单条结果
export interface BulkImportItem {
  key_mask?: string
  status: 'added' | 'duplicate' | 'invalid' | string
  reason?: string
}

// --- 模型广场 ---

// 模型能力类型（与后端 internal/modelcatalog 能力常量对齐）。
export type ModelCapability =
  | 'chat'
  | 'reasoning'
  | 'code'
  | 'vision'
  | 'embedding'
  | 'rerank'
  | 'safety'
  | 'reward'
  | 'translation'
  | 'parsing'

// ModelView 对应后端 admin.modelView（GET /api/admin/models 与 /api/admin/models/plaza 的条目）。
// v0.5+ 增可用度字段：被动聚合(availability_score/avg_latency_ms/error_count) + 主动探活(probe_*)。
export interface ModelView {
  id: string
  object: string
  created: number
  owned_by: string
  root: string
  capability: ModelCapability | string
  param_count: string
  context_length: number
  description: string
  is_active: boolean
  synced_at: number
  // 用量统计（近 1h，被动流量聚合）
  request_count: number
  success_rate: number // 0..100
  // 可用度（被动聚合 + 主动探活，仿 new-api）
  availability_score: number // 0..100 综合评分
  avg_latency_ms: number
  error_count: number
  last_probe_ts: number
  probe_ok: boolean
  probe_status: string // ok|error|timeout
  probe_latency_ms: number
}

// ProbeResult 单次模型探活结果（POST /api/admin/models/test 返回）。
export interface ProbeResult {
  model_id: string
  ts: number
  ok: boolean
  http_status: number
  latency_ms: number
  status: string
  error?: string
}

export interface ModelListResp {
  data: ModelView[]
  last_sync: number
  total: number
}

// SchedulerSettings 熔断 / 调度运行时配置（GET /api/admin/settings 返回的顶层对象）。
// 时长字段以「秒」为单位，与后端 settingsView 契约对齐。
export interface SchedulerSettings {
  circuit_threshold: number
  circuit_cooldown_sec: number
  default_concurrency: number
  max_concurrency: number
  request_timeout_sec: number
}

export interface Credential {
  id: number
  credential_mask: string
  name: string
  enabled: boolean
  rpm_limit: number
  allowed_models: string
  total_requests: number
}

export interface Metrics {
  Window: string
  TotalRequests: number
  SuccessRequests: number
  SuccessRate: number
  ErrorRequests: number
  RateLimited: number
  Timeouts: number
  TotalTokens: number
  AvgLatencyMS: number
  CurrentRPM: number
  PeakRPM: number
}

// --- Auto-Pilot ---

export type AutoPilotMode = 'manual' | 'assisted' | 'fullauto'
export type AutoPilotEngine = 'adaptive' | 'predict' | 'llm'
export type ActionKind =
  | 'set_concurrency'
  | 'set_weight_boost'
  | 'disable_key'
  | 'open_circuit'
  | 'revoke_credential'

export interface AutoPilotEvent {
  TS: number
  Engine: AutoPilotEngine | string
  Mode: AutoPilotMode | string
  Kind: ActionKind | string
  Detail: string
  Reason: string
  Confidence: number
  Applied: boolean
}

// StepTrace LLM 引擎 ReAct 推理轨迹单步（think/act/observe）。
export interface StepTrace {
  Step: number
  Role: 'think' | 'act' | 'observe' | string
  Content: string
  ToolName?: string
  ToolArgs?: string
  Error?: string
}

export interface AutoPilotState {
  Mode: AutoPilotMode | string
  Engine: AutoPilotEngine | string
  RuntimeConcurrency: number
  DefaultConcurrency: number
  MaxConcurrency: number
  DecisionsPerMin: number
  Interventions: number
  PendingCount: number
  RecentEvents: AutoPilotEvent[]
  // v0.5+ agent 化：LLM 后端模式 + 推理轨迹
  LLMBackendMode: string // stub|gateway
  RecentTrace?: StepTrace[]
}

export interface AutoPilotAction {
  Kind: ActionKind | string
  KeyID?: number
  CredID?: number
  Value?: number
  Reason: string
  Confidence: number
  Source: AutoPilotEngine | string
}

export interface PendingEntry {
  Key: string
  Value: string // JSON 编码的 AutoPilotAction
  UpdatedAt: number
}

export const api = {
  // 鉴权 / 改密（无需常规中间件）
  authStatus: () => request<{ initialized: boolean; must_change_password: boolean }>('/api/admin/auth/status'),
  login: (token: string) =>
    request<{ ok: boolean; must_change_password: boolean; is_default: boolean }>('/api/admin/login', {
      method: 'POST',
      body: JSON.stringify({ token }),
    }),
  changePassword: (current: string, next: string) =>
    request<{ ok: boolean }>('/api/admin/change-password', {
      method: 'POST',
      body: JSON.stringify({ current, next }),
    }),
  // 上游密钥
  listKeys: () => request<{ data: UpstreamKey[] }>('/api/admin/keys'),
  addKey: (data: { key_value: string; label?: string; email?: string; rpm_override?: number }) =>
    request<{ id: number }>('/api/admin/keys', { method: 'POST', body: JSON.stringify(data) }),
  bulkAddKeys: (data: { keys?: string[]; raw?: string; label?: string; email?: string; rpm_override?: number }) =>
    request<{ total: number; added: number; skipped: number; items: BulkImportItem[] }>(
      '/api/admin/keys/bulk',
      { method: 'POST', body: JSON.stringify(data) },
    ),
  deleteKey: (id: number) => request(`/api/admin/keys/${id}`, { method: 'DELETE' }),
  toggleKey: (id: number, enabled: boolean) =>
    request(`/api/admin/keys/${id}`, { method: 'PATCH', body: JSON.stringify({ enabled }) }),
  // 下游凭证
  listCredentials: () => request<{ data: Credential[] }>('/api/admin/credentials'),
  addCredential: (data: { name?: string; rpm_limit?: number; allowed_models?: string }) =>
    request<{ id: number; credential: string; credential_mask: string }>('/api/admin/credentials', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  deleteCredential: (id: number) => request(`/api/admin/credentials/${id}`, { method: 'DELETE' }),
  // 指标
  getMetrics: (window = '1h') => request<Metrics>(`/api/admin/metrics?window=${window}`),
  getTimeSeries: (window = '1h', bucket = 60) =>
    request<{ data: any[] }>(`/api/admin/timeseries?window=${window}&bucket=${bucket}`),
  getKeyHealth: (window = '1h') =>
    request<{ data: any[] }>(`/api/admin/key-health?window=${window}`),
  // 模型广场
  // listModels: 全部模型（含已失效），带用量统计。
  listModels: () => request<ModelListResp>('/api/admin/models'),
  // listModelsPlaza: 模型广场视图，支持 capability 过滤与仅可用过滤。
  listModelsPlaza: (params?: { capability?: string; active_only?: boolean }) => {
    const qs = new URLSearchParams()
    if (params?.capability) qs.set('capability', params.capability)
    qs.set('active_only', params?.active_only === false ? 'false' : 'true')
    const suffix = qs.toString() ? `?${qs.toString()}` : ''
    return request<ModelListResp>(`/api/admin/models/plaza${suffix}`)
  },
  syncModels: () => request<{ ok: boolean }>('/api/admin/models/sync', { method: 'POST' }),
  // 探活单个模型（可用度测试，仿 new-api）
  testModel: (model: string) =>
    request<ProbeResult>('/api/admin/models/test', {
      method: 'POST',
      body: JSON.stringify({ model }),
    }),
  // 探活所有 chat 模型
  probeAllModels: () => request<{ ok: boolean }>('/api/admin/models/probe-all', { method: 'POST' }),
  // 熔断 / 调度运行时配置（GET 返回当前值；PUT 落库 + 热生效，返回更新后的 settings）
  getSettings: () => request<SchedulerSettings>('/api/admin/settings'),
  putSettings: (s: SchedulerSettings) =>
    request<{ ok: boolean; settings: SchedulerSettings }>('/api/admin/settings', {
      method: 'PUT',
      body: JSON.stringify(s),
    }),
  // 日志
  getLogs: (params: string) => request<{ data: any[] }>(`/api/admin/logs${params}`),
  // Auto-Pilot
  getAutopilotState: () => request<AutoPilotState>('/api/admin/autopilot/state'),
  getAutopilotSnapshot: () => request<AutoPilotSnapshot>('/api/admin/autopilot/snapshot'),
  setAutopilotMode: (mode: AutoPilotMode) =>
    request<{ ok: boolean; mode: string }>('/api/admin/autopilot/mode', {
      method: 'PUT',
      body: JSON.stringify({ mode }),
    }),
  setAutopilotEngine: (engine: AutoPilotEngine) =>
    request<{ ok: boolean; engine: string }>('/api/admin/autopilot/engine', {
      method: 'PUT',
      body: JSON.stringify({ engine }),
    }),
  listPending: () => request<{ data: PendingEntry[] }>('/api/admin/autopilot/pending'),
  approvePending: (key: string) =>
    request('/api/admin/autopilot/pending/' + encodeURIComponent(key) + '/approve', { method: 'POST' }),
  rejectPending: (key: string) =>
    request('/api/admin/autopilot/pending/' + encodeURIComponent(key) + '/reject', { method: 'POST' }),
}

// AutoPilotSnapshot 对应后端 autopilot.Snapshot（GET /autopilot/snapshot）。
export interface AutoPilotSnapshot {
  Keys: Array<{
    ID: number
    Mask: string
    Enabled: boolean
    Status: string
    SuccessRate: number
    ConsecFail: number
    RPMRemaining: number
  }>
  Metrics: Metrics
  Series: Array<{ TS: number; Count: number; Tokens: number; Rate: number }>
  CurrentConcurrency: number
  MaxConcurrency: number
  DefaultConcurrency: number
}
