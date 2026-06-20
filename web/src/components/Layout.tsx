import { useState, useEffect } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toggleLang } from '../i18n'
import { api, getToken, setToken, clearToken } from '../api'

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
  const [loginErr, setLoginErr] = useState('')
  const [loginBusy, setLoginBusy] = useState(false)
  // 强制改密状态
  const [mustChange, setMustChange] = useState(false)
  const [pwForm, setPwForm] = useState({ current: '', next: '', confirm: '' })
  const [pwErr, setPwErr] = useState('')
  const [pwBusy, setPwBusy] = useState(false)

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark)
  }, [dark])

  // 已登录时探测是否需强制改密（防止有人带着默认 admin token 直接跳过登录）
  useEffect(() => {
    if (!getToken()) return
    api.authStatus().then((s) => setMustChange(!!s.must_change_password)).catch(() => {})
  }, [])

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoginErr('')
    setLoginBusy(true)
    try {
      const r = await api.login(token)
      setToken(token)
      setMustChange(!!r.must_change_password)
      if (!r.must_change_password) {
        window.location.reload()
        return
      }
      // 需改密：保持页面，弹出改密层（保留已存 token，预填当前令牌）
      setPwForm({ current: token, next: '', confirm: '' })
    } catch (err: any) {
      setLoginErr(err?.message || '登录失败')
    } finally {
      setLoginBusy(false)
    }
  }

  const submitChange = async (e: React.FormEvent) => {
    e.preventDefault()
    setPwErr('')
    if (pwForm.next.length < 6) { setPwErr('新令牌至少 6 个字符'); return }
    if (pwForm.next.toUpperCase() === 'ADMIN') { setPwErr('新令牌不能使用初始默认值 ADMIN'); return }
    if (pwForm.next !== pwForm.confirm) { setPwErr('两次输入不一致'); return }
    setPwBusy(true)
    try {
      await api.changePassword(pwForm.current, pwForm.next)
      setToken(pwForm.next)
      setMustChange(false)
      window.location.reload()
    } catch (err: any) {
      setPwErr(err?.message || '修改失败')
    } finally {
      setPwBusy(false)
    }
  }

  return (
    <div className="flex h-screen overflow-hidden bg-nv-dark">
      {/* 侧栏 */}
      <aside className="w-[210px] flex-shrink-0 border-r border-white/[0.04] bg-white/[0.015] p-2.5 overflow-y-auto">
        <div className="px-3.5 py-3 flex items-center gap-2 font-extrabold text-[15px]">
          <span className="w-[22px] h-[22px] flex items-center justify-center bg-gradient-to-br from-nv-green to-nv-green-dim rounded-md text-black shadow-nv-glow">⬢</span>
          <span className="text-white">N-SLMCRS</span>
          <span className="text-gray-600 text-[11px] font-normal px-1.5 py-0.5 rounded bg-white/[0.04] border border-white/[0.06]">v0.4.0</span>
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
              onClick={() => { clearToken(); setTok(''); setMustChange(false); navigate('/'); window.location.reload() }}
              className="btn-ghost"
            >
              {t('common.logout')}
            </button>
          ) : (
            <form onSubmit={handleLogin} className="flex items-center gap-1">
              <input
                value={token}
                onChange={(e) => { setTok(e.target.value); setLoginErr('') }}
                placeholder="Admin Token（默认 ADMIN）"
                className="input w-44 h-7 py-1"
                type="password"
              />
              <button type="submit" disabled={loginBusy} className="btn-primary py-1 text-xs disabled:opacity-50">
                {loginBusy ? '...' : t('common.login')}
              </button>
              {loginErr && <span className="text-[11px] text-red-400 ml-1">{loginErr}</span>}
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

      {/* 强制修改密码模态（首登或仍用默认 admin 时弹出，遮挡全部交互） */}
      {mustChange && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/70 backdrop-blur-sm">
          <div className="w-[420px] glass-card p-6 animate-slide-up">
            <div className="flex items-center gap-2.5 mb-1">
              <span className="w-7 h-7 flex items-center justify-center rounded-md bg-amber-400/15 border border-amber-400/30 text-amber-400">⚠</span>
              <h2 className="text-[15px] font-bold text-white">{t('common.change_password')}</h2>
            </div>
            <p className="text-[12px] text-gray-500 mb-4 leading-relaxed">{t('common.must_change_password_hint')}</p>
            <form onSubmit={submitChange} className="space-y-3">
              <div>
                <label className="text-[11px] text-gray-500">{t('common.current_password')}</label>
                <input className="input mt-1" type="password" value={pwForm.current}
                  onChange={(e) => setPwForm({ ...pwForm, current: e.target.value })} required />
              </div>
              <div>
                <label className="text-[11px] text-gray-500">{t('common.new_password')}</label>
                <input className="input mt-1" type="password" value={pwForm.next}
                  onChange={(e) => { setPwForm({ ...pwForm, next: e.target.value }); setPwErr('') }} required />
                <div className="text-[10px] text-gray-600 mt-1">≥ 6 字符，且不能为 ADMIN</div>
              </div>
              <div>
                <label className="text-[11px] text-gray-500">{t('common.confirm_password')}</label>
                <input className="input mt-1" type="password" value={pwForm.confirm}
                  onChange={(e) => setPwForm({ ...pwForm, confirm: e.target.value })} required />
              </div>
              {pwErr && <div className="text-[12px] text-red-400">{pwErr}</div>}
              <button type="submit" disabled={pwBusy} className="btn-primary w-full disabled:opacity-50">
                {pwBusy ? t('common.saving') : t('common.save')}
              </button>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
