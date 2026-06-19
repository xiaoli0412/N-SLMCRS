import { useState, useEffect } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toggleLang } from '../i18n'
import { getToken, setToken, clearToken } from '../api'

const NAV = [
  { group: 'group_monitor', items: [
    { to: '/', key: 'overview', icon: '▣' },
    { to: '/operations', key: 'operations', icon: '📈' },
    { to: '/logs', key: 'logs', icon: '📋' },
  ]},
  { group: 'group_resource', items: [
    { to: '/models', key: 'models', icon: '⬢' },
    { to: '/keys', key: 'keys', icon: '🔑' },
  ]},
  { group: 'group_routing', items: [
    { to: '/distribution', key: 'distribution', icon: '↗' },
    { to: '/autopilot', key: 'autopilot', icon: '✦' },
  ]},
  { group: 'group_system', items: [
    { to: '/settings', key: 'settings', icon: '⚙' },
  ]},
]

export default function Layout() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const [dark, setDark] = useState(true)
  const [token, setTok] = useState(getToken())

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark)
  }, [dark])

  const handleLogin = (e: React.FormEvent) => {
    e.preventDefault()
    setToken(token)
    window.location.reload()
  }

  return (
    <div className="flex h-screen overflow-hidden bg-nv-dark">
      {/* 侧栏 */}
      <aside className="w-[210px] flex-shrink-0 border-r border-white/[0.04] bg-white/[0.015] p-2.5 overflow-y-auto">
        <div className="px-3.5 py-3 flex items-center gap-2 font-extrabold text-[15px]">
          <span className="w-[22px] h-[22px] flex items-center justify-center bg-gradient-to-br from-nv-green to-nv-green-dim rounded-md text-black shadow-nv-glow">⬢</span>
          <span className="text-white">N-SLMCRS</span>
          <span className="text-gray-600 text-[11px] font-normal px-1.5 py-0.5 rounded bg-white/[0.04] border border-white/[0.06]">v0.2</span>
        </div>
        <nav className="mt-1">
          {NAV.map((sec) => (
            <div key={sec.group} className="mt-3">
              <div className="px-3.5 pb-1.5 text-[10px] font-semibold tracking-wider text-gray-600 uppercase">
                {t(`nav.${sec.group}`)}
              </div>
              {sec.items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === '/'}
                  className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}
                >
                  <span className="w-[18px] text-center opacity-80 text-sm">{item.icon}</span>
                  {t(`nav.${item.key}`)}
                </NavLink>
              ))}
            </div>
          ))}
        </nav>
      </aside>

      {/* 主区 */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* 顶栏 */}
        <header className="h-13 px-6 flex items-center gap-3 border-b border-white/[0.05] bg-gradient-to-b from-nv-dark-50 to-nv-dark relative">
          <div className="absolute bottom-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-nv-green/40 to-transparent" />
          <div className="flex-1" />
          <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-white/[0.03] border border-white/[0.06] text-[12px] text-gray-400">
            <span className="status-dot ok animate-pulse-slow" />
            {t('common.running')}
          </div>
          <button
            onClick={toggleLang}
            className="px-2.5 py-1 rounded-md bg-white/[0.03] border border-white/[0.06] text-[12px] text-gray-400 hover:text-gray-200 hover:bg-white/[0.06] transition-colors"
          >
            🌐 {i18n.language === 'zh' ? '中' : 'EN'}
          </button>
          <button
            onClick={() => setDark(!dark)}
            className="px-2.5 py-1 rounded-md bg-white/[0.03] border border-white/[0.06] text-[12px] text-gray-400 hover:text-gray-200 transition-colors"
          >
            {dark ? '🌙' : '☀'}
          </button>
          {getToken() ? (
            <button
              onClick={() => { clearToken(); setTok(''); navigate('/'); window.location.reload() }}
              className="btn-ghost"
            >
              登出
            </button>
          ) : (
            <form onSubmit={handleLogin} className="flex items-center gap-1">
              <input
                value={token}
                onChange={(e) => setTok(e.target.value)}
                placeholder="Admin Token"
                className="input w-36 h-7 py-1"
                type="password"
              />
              <button type="submit" className="btn-primary py-1 text-xs">登录</button>
            </form>
          )}
        </header>

        {/* 内容 */}
        <main className="flex-1 overflow-y-auto bg-radial-gradient relative">
          <div className="absolute top-0 right-0 w-[300px] h-[300px] pointer-events-none -translate-y-10 translate-x-10"
            style={{ background: 'radial-gradient(circle, rgba(118,185,0,0.06), transparent 70%)' }} />
          <div className="relative p-6 max-w-[1400px] mx-auto">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
