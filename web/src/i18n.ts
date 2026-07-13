import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

const resources = {
  zh: {
    translation: {
      nav: { overview: '概览', models: '模型广场', operations: '调度运营', keys: '上游密钥',
        distribution: '凭证与集成', autopilot: 'Auto-Pilot', logs: '日志中心', backup: '备份',
        settings: '设置', circuit: '模型熔断', playground: 'Chat 测试台', strategy: '策略引擎' },
      theme: { toggle: '切换主题', light: '亮色', dark: '暗色' },
      lang: { toggle: '中/英' },
      a11y: { skip: '跳到主内容', nav: '主导航', openMenu: '打开菜单', closeMenu: '关闭菜单', logout: '退出登录' },
      common: { search: '搜索', all: '全部', active: '可用', refresh: '刷新', save: '保存',
        cancel: '取消', confirm: '确认', delete: '删除', reset: '复位', test: '测试',
        sync: '立即同步', probeAll: '探活全部', sweep: '触发健康扫描', close: '关闭',
        loading: '加载中…', empty: '暂无数据', yes: '是', no: '否', loadMore: '加载更多',
        absolute: '绝对时间', relative: '相对时间' },
      cap: { chat: '对话', reasoning: '推理', code: '代码', vision: '视觉',
        embedding: '嵌入', rerank: '重排', safety: '安全', reward: '奖励',
        translation: '翻译', parsing: '解析' },
      circuit: { closed: '正常', open: '熔断中', half_open: '半开', permanent: '永久熔断',
        reset: '复位熔断', success_rate: '扫描成功率', bad_sweep: '连续坏扫描' },
      models: { plaza: '模型广场', secondary: '模型详情', params: '参数说明', health: '健康',
        probes: '探活', param_count: '参数量', context: '上下文', requests: '请求/h',
        success: '成功率', avail: '可用度', latency: '延迟', architecture: '架构',
        interfaces: '支持接口', pricing: '定价', license: '许可证', released: '发布',
        card: '模型卡' },
      strategy: {
        title: '策略引擎', subtitle: '切换调度策略——选择算法 / 扇出 / RPM 头寸 / 熔断参数的相干捆绑',
        active: '当前策略', recommended: '推荐', recommendHint: '按当前密钥数推荐',
        apply: '应用此策略', applied: '策略已切换', kernelOnline: '内核在线（权威）',
        kernelOffline: '内核离线（降级路径镜像）', keyCount: '可用密钥',
        selection: '选择算法', fanout: '扇出', breakerThreshold: '熔断阈值',
        breakerCooldown: '熔断冷却', rpmHeadroom: 'RPM 头寸', scenario: '适用场景',
        fanoutAuto: '调用方（调度器/Auto-Pilot）', fanoutFixed: '固定',
        algo: { weighted_random: '加权随机', round_robin: '轮转分发', least_inflight: '最少在途', strict_priority: '严格优先' },
        diffTitle: '切换后将发生', diffNoChange: '与当前策略一致',
      },
      logs: {
        title: '日志中心', subtitle: '结构化应用日志与操作审计',
        tabApp: '应用日志', tabAudit: '审计日志',
        traceId: 'trace_id', level: '级别', source: '来源',
        loadMore: '加载更多', noMore: '已加载全部',
        actor: '操作者', action: '动作', detail: '详情', ip: '来源 IP',
        allSources: '全部来源', allLevels: '全部级别',
      },
    },
  },
  en: {
    translation: {
      nav: { overview: 'Overview', models: 'Model Plaza', operations: 'Operations', keys: 'Upstream Keys',
        distribution: 'Credentials & Integrations', autopilot: 'Auto-Pilot', logs: 'Log Center',
        backup: 'Backup', settings: 'Settings', circuit: 'Model Circuit', playground: 'Playground', strategy: 'Strategy' },
      theme: { toggle: 'Toggle theme', light: 'Light', dark: 'Dark' },
      lang: { toggle: 'ZH/EN' },
      a11y: { skip: 'Skip to main content', nav: 'Main navigation', openMenu: 'Open menu', closeMenu: 'Close menu', logout: 'Log out' },
      common: { search: 'Search', all: 'All', active: 'Active', refresh: 'Refresh', save: 'Save',
        cancel: 'Cancel', confirm: 'Confirm', delete: 'Delete', reset: 'Reset', test: 'Test',
        sync: 'Sync now', probeAll: 'Probe all', sweep: 'Trigger health sweep', close: 'Close',
        loading: 'Loading…', empty: 'No data', yes: 'Yes', no: 'No', loadMore: 'Load more',
        absolute: 'Absolute time', relative: 'Relative time' },
      cap: { chat: 'Chat', reasoning: 'Reasoning', code: 'Code', vision: 'Vision',
        embedding: 'Embedding', rerank: 'Rerank', safety: 'Safety', reward: 'Reward',
        translation: 'Translation', parsing: 'Parsing' },
      circuit: { closed: 'Healthy', open: 'Open', half_open: 'Half-open', permanent: 'Permanent',
        reset: 'Reset circuit', success_rate: 'Sweep success rate', bad_sweep: 'Bad sweeps' },
      models: { plaza: 'Model Plaza', secondary: 'Model Detail', params: 'Specs', health: 'Health',
        probes: 'Probes', param_count: 'Params', context: 'Context', requests: 'Req/h',
        success: 'Success', avail: 'Availability', latency: 'Latency', architecture: 'Architecture',
        interfaces: 'Interfaces', pricing: 'Pricing', license: 'License', released: 'Released',
        card: 'Model card' },
      strategy: {
        title: 'Strategy Engine', subtitle: 'Switch scheduling strategy — a coherent bundle of selection algo / fan-out / RPM headroom / breaker params',
        active: 'Current', recommended: 'Recommended', recommendHint: 'Based on current key count',
        apply: 'Apply this strategy', applied: 'Strategy switched', kernelOnline: 'Kernel online (authoritative)',
        kernelOffline: 'Kernel offline (degrade-path mirror)', keyCount: 'Available keys',
        selection: 'Selection', fanout: 'Fan-out', breakerThreshold: 'Breaker threshold',
        breakerCooldown: 'Breaker cooldown', rpmHeadroom: 'RPM headroom', scenario: 'Scenario',
        fanoutAuto: 'caller (scheduler/Auto-Pilot)', fanoutFixed: 'fixed',
        algo: { weighted_random: 'Weighted random', round_robin: 'Round-robin', least_inflight: 'Least inflight', strict_priority: 'Strict priority' },
        diffTitle: 'On switch', diffNoChange: 'Same as current',
      },
      logs: {
        title: 'Log Center', subtitle: 'Structured application logs & operation audit',
        tabApp: 'App Logs', tabAudit: 'Audit Log',
        traceId: 'trace_id', level: 'Level', source: 'Source',
        loadMore: 'Load more', noMore: 'All loaded',
        actor: 'Actor', action: 'Action', detail: 'Detail', ip: 'Source IP',
        allSources: 'All sources', allLevels: 'All levels',
      },
    },
  },
}

i18n.use(initReactI18next).init({
  resources,
  lng: localStorage.getItem('lang') || 'zh',
  fallbackLng: 'zh',
  interpolation: { escapeValue: false },
})

export function toggleLang() {
  const next = i18n.language === 'zh' ? 'en' : 'zh'
  localStorage.setItem('lang', next)
  i18n.changeLanguage(next)
}

export default i18n
