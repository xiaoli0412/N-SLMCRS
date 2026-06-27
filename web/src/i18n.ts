import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

const resources = {
  zh: {
    translation: {
      nav: { overview: '概览', models: '模型广场', operations: '调度运营', keys: '上游密钥',
        distribution: '凭证与集成', autopilot: 'Auto-Pilot', logs: '日志中心', backup: '备份',
        settings: '设置', circuit: '模型熔断' },
      theme: { toggle: '切换主题', light: '亮色', dark: '暗色' },
      lang: { toggle: '中/英' },
      common: { search: '搜索', all: '全部', active: '可用', refresh: '刷新', save: '保存',
        cancel: '取消', confirm: '确认', delete: '删除', reset: '复位', test: '测试',
        sync: '立即同步', probeAll: '探活全部', sweep: '触发健康扫描', close: '关闭',
        loading: '加载中…', empty: '暂无数据', yes: '是', no: '否' },
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
    },
  },
  en: {
    translation: {
      nav: { overview: 'Overview', models: 'Model Plaza', operations: 'Operations', keys: 'Upstream Keys',
        distribution: 'Credentials & Integrations', autopilot: 'Auto-Pilot', logs: 'Log Center',
        backup: 'Backup', settings: 'Settings', circuit: 'Model Circuit' },
      theme: { toggle: 'Toggle theme', light: 'Light', dark: 'Dark' },
      lang: { toggle: 'ZH/EN' },
      common: { search: 'Search', all: 'All', active: 'Active', refresh: 'Refresh', save: 'Save',
        cancel: 'Cancel', confirm: 'Confirm', delete: 'Delete', reset: 'Reset', test: 'Test',
        sync: 'Sync now', probeAll: 'Probe all', sweep: 'Trigger health sweep', close: 'Close',
        loading: 'Loading…', empty: 'No data', yes: 'Yes', no: 'No' },
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
