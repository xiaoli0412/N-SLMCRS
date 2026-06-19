import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

i18n.use(initReactI18next).init({
  resources: {
    zh: {
      translation: {
        brand: 'N-SLMCRS 网关',
        nav: {
          group_monitor: '监控',
          group_resource: '资源',
          group_routing: '调度',
          group_system: '系统',
          overview: '概览',
          operations: '运维监控',
          logs: '日志中心',
          models: '模型目录',
          keys: '上游密钥',
          distribution: '接入分发',
          autopilot: '智能调度',
          settings: '系统设置',
        },
        common: {
          running: '服务运行中',
          success_rate: '整体成功率',
          throughput: '实时吞吐',
          upstream_keys: '上游密钥',
          tokens_today: '今日 Token',
          add: '添加',
          delete: '删除',
          save: '保存',
          enable: '启用',
          disable: '停用',
          status: '状态',
          actions: '操作',
          cooldown: '冷却中',
          create_credential: '签发下游凭证',
          sync_models: '立即同步模型',
          placeholder: '暂无数据',
        },
      },
    },
    en: {
      translation: {
        brand: 'N-SLMCRS Gateway',
        nav: {
          group_monitor: 'Monitor',
          group_resource: 'Resources',
          group_routing: 'Routing',
          group_system: 'System',
          overview: 'Overview',
          operations: 'Operations',
          logs: 'Logs',
          models: 'Model Catalog',
          keys: 'Upstream Keys',
          distribution: 'Distribution',
          autopilot: 'Auto-Pilot',
          settings: 'Settings',
        },
        common: {
          running: 'Running',
          success_rate: 'Success Rate',
          throughput: 'Throughput',
          upstream_keys: 'Upstream Keys',
          tokens_today: 'Tokens Today',
          add: 'Add',
          delete: 'Delete',
          save: 'Save',
          enable: 'Enable',
          disable: 'Disable',
          status: 'Status',
          actions: 'Actions',
          cooldown: 'Cooling',
          create_credential: 'Create Credential',
          sync_models: 'Sync Models Now',
          placeholder: 'No data',
        },
      },
    },
  },
  lng: localStorage.getItem('lang') || 'zh',
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
})

export const toggleLang = () => {
  const next = i18n.language === 'zh' ? 'en' : 'zh'
  localStorage.setItem('lang', next)
  i18n.changeLanguage(next)
}
