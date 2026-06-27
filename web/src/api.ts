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
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const t = getToken()
  if (t) headers['X-Admin-Token'] = t
  const res = await fetch(path, { ...opts, headers: { ...headers, ...(opts.headers as any) } })
  if (!res.ok) {
    const txt = await res.text().catch(() => res.statusText)
    throw new Error(`${res.status}: ${txt}`)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

async function requestBlob(path: string, opts: RequestInit = {}): Promise<Blob> {
  const headers: Record<string, string> = {}
  const t = getToken()
  if (t) headers['X-Admin-Token'] = t
  const res = await fetch(path, { ...opts, headers: { ...headers, ...(opts.headers as any) } })
  if (!res.ok) throw new Error(`${res.status}`)
  return res.blob()
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
export interface BulkImportItem {
  key_mask?: string
  status: 'added' | 'duplicate' | 'invalid' | string
  reason?: string
}

export type ModelCapability =
  | 'chat' | 'reasoning' | 'code' | 'vision'
  | 'embedding' | 'rerank' | 'safety' | 'reward' | 'translation' | 'parsing'

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
  status: 'active' | 'gone' | 'disabled' | string
  last_seen_active_at: number
  synced_at: number
  request_count: number
  success_rate: number
  availability_score: number
  avg_latency_ms: number
  error_count: number
  last_probe_ts: number
  probe_ok: boolean
  probe_status: string
  probe_latency_ms: number
  max_tokens: number
  pricing_in: string
  pricing_out: string
  license: string
  input_modalities: string[]
  release_date: string
  card_url: string
  // v0.9：HF 富化架构 + 支持接口
  architecture: string
  supported_interfaces: string[]
  // v0.9：模型级熔断状态
  circuit_state: 'closed' | 'open' | 'half_open' | 'permanent' | string
  circuit_success_rate: number
  circuit_permanent: boolean
  circuit_open_until: number
}

export interface ModelCircuit {
  Model: string
  State: 'closed' | 'open' | 'half_open' | 'permanent' | string
  OpenUntil: number
  ConsecutiveFail: number
  SuccessRatePct: number
  BadSweepCount: number
  Permanent: boolean
  LastSweepAt: number
  UpdatedAt: number
}

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

export interface SchedulerSettings {
  circuit_threshold: number
  circuit_cooldown_sec: number
  default_concurrency: number
  max_concurrency: number
  request_timeout_sec: number
  mh_probe_count: number
  mh_probe_interval_sec: number
  mh_sweep_interval_sec: number
  mh_success_rate_floor: number
  mh_success_rate_threshold: number
  mh_bad_sweep_to_permanent: number
  mh_cooldown_base_sec: number
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

// v0.10：集成渠道（new-api / sapi 等下游中转网关对接）
export interface Channel {
  ID: number
  Name: string
  Type: string // newapi | sapi
  BaseURL: string
  APIKeyMask: string
  Enabled: boolean
  LastSyncAt: number
  TotalRequests: number
  CreatedAt: number
  UpdatedAt: number
}
// 渠道配置（管理员粘贴到 new-api/sapi 的接入配置，含明文密钥仅创建时返回一次）
export interface ChannelConfig {
  type: string
  name: string
  base_url: string
  api_key: string
  models: string[]
  usage_url: string
}
export interface AddChannelResp {
  channel: Channel
  config: ChannelConfig
}
// Webhook 事件回调配置（secret 为掩码占位，非明文）
export interface WebhookCfg {
  url: string
  secret: string
  events: string
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
export interface TimeSeriesPoint {
  ts: number
  count: number
  ok_count: number
  tokens: number
  rate: number
}
export interface KeyHealthEntry {
  key_mask: string
  status: string
  total_requests: number
  success_rate: number
  avg_latency_ms: number
  consecutive_fail: number
  ewma_rate: number
}

export type AutoPilotMode = 'manual' | 'assisted' | 'fullauto'
export type AutoPilotEngine = 'adaptive' | 'predict' | 'llm'
export type ActionKind =
  | 'set_concurrency' | 'set_weight_boost' | 'disable_key'
  | 'open_circuit' | 'revoke_credential'
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
  LLMBackendMode: string
  RecentTrace?: StepTrace[]
  AvailableKeyCount: number
  InflightRequests: number
  ClientConcurrencyTier: string
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
  Value: string
  UpdatedAt: number
}
export interface AutoPilotSnapshot {
  Keys: Array<{
    ID: number; Mask: string; Enabled: boolean; Status: string
    SuccessRate: number; ConsecFail: number; RPMRemaining: number
  }>
  Metrics: Metrics
  Series: TimeSeriesPoint[]
  CurrentConcurrency: number
  MaxConcurrency: number
  DefaultConcurrency: number
  AvailableKeyCount: number
  InflightRequests: number
  ClientConcurrencyTier: string
}

export interface BackupInfo {
  name: string
  size: number
  mod_time: number
}

export interface AppLog {
  ts: number
  trace_id: string
  level: string
  source: string
  message: string
  context: string
}

export const api = {
  authStatus: () => request<{ initialized: boolean; must_change_password: boolean }>('/api/admin/auth/status'),
  login: (token: string) =>
    request<{ ok: boolean; must_change_password: boolean; is_default: boolean }>('/api/admin/login', {
      method: 'POST', body: JSON.stringify({ token }),
    }),
  changePassword: (current: string, next: string) =>
    request<{ ok: boolean }>('/api/admin/change-password', {
      method: 'POST', body: JSON.stringify({ current, next }),
    }),

  listKeys: () => request<{ data: UpstreamKey[] }>('/api/admin/keys'),
  addKey: (data: { key_value: string; label?: string; email?: string; rpm_override?: number }) =>
    request<{ id: number }>('/api/admin/keys', { method: 'POST', body: JSON.stringify(data) }),
  bulkAddKeys: (data: { keys?: string[]; raw?: string; label?: string; email?: string; rpm_override?: number }) =>
    request<{ total: number; added: number; skipped: number; items: BulkImportItem[] }>(
      '/api/admin/keys/bulk', { method: 'POST', body: JSON.stringify(data) },
    ),
  deleteKey: (id: number) => request(`/api/admin/keys/${id}`, { method: 'DELETE' }),
  toggleKey: (id: number, enabled: boolean) =>
    request(`/api/admin/keys/${id}`, { method: 'PATCH', body: JSON.stringify({ enabled }) }),

  listCredentials: () => request<{ data: Credential[] }>('/api/admin/credentials'),
  addCredential: (data: { name?: string; rpm_limit?: number; allowed_models?: string }) =>
    request<{ id: number; credential: string; credential_mask: string }>('/api/admin/credentials', {
      method: 'POST', body: JSON.stringify(data),
    }),
  deleteCredential: (id: number) => request(`/api/admin/credentials/${id}`, { method: 'DELETE' }),

  getMetrics: (window = '1h', model?: string) => {
    const qs = new URLSearchParams({ window })
    if (model) qs.set('model', model)
    return request<Metrics>(`/api/admin/metrics?${qs.toString()}`)
  },
  getTimeSeries: (window = '1h', bucket = 60, model?: string) => {
    const qs = new URLSearchParams({ window: String(window), bucket: String(bucket) })
    if (model) qs.set('model', model)
    return request<{ data: TimeSeriesPoint[] }>(`/api/admin/timeseries?${qs.toString()}`)
  },
  getKeyHealth: (window = '1h', model?: string) => {
    const qs = new URLSearchParams({ window })
    if (model) qs.set('model', model)
    return request<{ data: KeyHealthEntry[] }>(`/api/admin/key-health?${qs.toString()}`)
  },

  listModels: () => request<ModelListResp>('/api/admin/models'),
  listModelsPlaza: (params?: { capability?: string; active_only?: boolean }) => {
    const qs = new URLSearchParams()
    if (params?.capability) qs.set('capability', params.capability)
    if (params?.active_only) qs.set('active_only', 'true')
    const suffix = qs.toString() ? `?${qs.toString()}` : ''
    return request<ModelListResp>(`/api/admin/models/plaza${suffix}`)
  },
  getModelDetail: (id: string) => request<ModelView>(`/api/admin/models/detail?id=${encodeURIComponent(id)}`),
  getModelTimeSeries: (id: string, window = '1h', bucket = 60) =>
    request<{ data: TimeSeriesPoint[] }>(
      `/api/admin/models/timeseries?id=${encodeURIComponent(id)}&window=${window}&bucket=${bucket}`,
    ),
  getModelProbes: (id: string, limit = 100) =>
    request<{ history: ProbeResult[]; latest: ProbeResult | null }>(
      `/api/admin/models/probes?id=${encodeURIComponent(id)}&limit=${limit}`,
    ),
  syncModels: () => request<{ ok: boolean }>('/api/admin/models/sync', { method: 'POST' }),
  testModel: (model: string) =>
    request<ProbeResult>('/api/admin/models/test', { method: 'POST', body: JSON.stringify({ model }) }),
  probeAllModels: () => request<{ ok: boolean }>('/api/admin/models/probe-all', { method: 'POST' }),

  // v0.9：模型级熔断
  listModelCircuit: (state?: string) => {
    const qs = state ? `?state=${state}` : ''
    return request<{ data: ModelCircuit[] }>(`/api/admin/models/circuit${qs}`)
  },
  healthSweep: () => request<{ ok: boolean }>('/api/admin/models/health-sweep', { method: 'POST' }),
  resetModelCircuit: (model: string) =>
    request<{ ok: boolean }>('/api/admin/models/circuit/reset', {
      method: 'POST', body: JSON.stringify({ model }),
    }),

  getSettings: () => request<SchedulerSettings>('/api/admin/settings'),
  putSettings: (s: Partial<SchedulerSettings>) =>
    request<{ ok: boolean; settings: SchedulerSettings }>('/api/admin/settings', {
      method: 'PUT', body: JSON.stringify(s),
    }),

  getLogs: (params: string) => request<{ data: AppLog[] }>(`/api/admin/logs${params}`),

  getAutopilotState: () => request<AutoPilotState>('/api/admin/autopilot/state'),
  getAutopilotSnapshot: () => request<AutoPilotSnapshot>('/api/admin/autopilot/snapshot'),
  setAutopilotMode: (mode: AutoPilotMode) =>
    request<{ ok: boolean; mode: string }>('/api/admin/autopilot/mode', {
      method: 'PUT', body: JSON.stringify({ mode }),
    }),
  setAutopilotEngine: (engine: AutoPilotEngine) =>
    request<{ ok: boolean; engine: string }>('/api/admin/autopilot/engine', {
      method: 'PUT', body: JSON.stringify({ engine }),
    }),
  listPending: () => request<{ data: PendingEntry[] }>('/api/admin/autopilot/pending'),
  approvePending: (key: string) =>
    request('/api/admin/autopilot/pending/' + encodeURIComponent(key) + '/approve', { method: 'POST' }),
  rejectPending: (key: string) =>
    request('/api/admin/autopilot/pending/' + encodeURIComponent(key) + '/reject', { method: 'POST' }),

  listBackups: () => request<{ data: BackupInfo[] }>('/api/admin/backup'),
  createBackup: () => request<{ ok: boolean; name: string }>('/api/admin/backup', { method: 'POST' }),
  deleteBackup: (file: string) => request('/api/admin/backup/' + encodeURIComponent(file), { method: 'DELETE' }),
  downloadBackup: (file: string) =>
    requestBlob('/api/admin/backup/' + encodeURIComponent(file)).then((b) => URL.createObjectURL(b)),

  // v0.10：集成钩子——渠道管理 + webhook 配置
  listChannels: () => request<{ data: Channel[] }>('/api/admin/hooks/channels'),
  addChannel: (data: { name: string; type: string; base_url?: string; api_key: string }) =>
    request<AddChannelResp>('/api/admin/hooks/channels', { method: 'POST', body: JSON.stringify(data) }),
  deleteChannel: (id: number) => request(`/api/admin/hooks/channels/${id}`, { method: 'DELETE' }),
  toggleChannel: (id: number, enabled: boolean) =>
    request(`/api/admin/hooks/channels/${id}`, { method: 'PATCH', body: JSON.stringify({ enabled }) }),
  channelConfig: (id: number) => request<{ config: ChannelConfig }>(`/api/admin/hooks/channels/${id}/config`),
  channelUsage: (id: number) =>
    request<{ channel_id: number; total_requests: number; window: string; total_tokens: number; success_rate: number }>(
      `/api/admin/hooks/channels/${id}/usage`,
    ),
  getWebhook: () => request<WebhookCfg>('/api/admin/hooks/webhook'),
  putWebhook: (cfg: WebhookCfg) =>
    request<{ ok: boolean }>('/api/admin/hooks/webhook', { method: 'PUT', body: JSON.stringify(cfg) }),
  testWebhook: () => request<{ ok: boolean }>('/api/admin/hooks/webhook/test', { method: 'POST' }),
}
