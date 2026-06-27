import { useState, useEffect } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Languages, LogOut, Activity } from 'lucide-react'
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
    { to: '/backup', key: 'backup', icon: '💽' },
    { to: '/settings', key: 'settings', icon: '⚙' },
  ]},
]

export default function Layout() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const [token, setTok] = useState(getToken())
  const [loginErr, setLoginErr] = useState('')
  const [loginBusy, setLoginBusy] = useState(false)
  // 强制改密状态
  const [mustChange, setMustChange] = useState(false)
  const [pwForm, setPwForm] = useState({ current: '', next: '', confirm: '' })
  const [pwErr, setPwErr] = useState('')
  const [pwBusy, setPwBusy] = useState(false)

  // 暗色主题（v0.6 精炼暗色，暂不提供亮色——亮色需逐页迁移文本色，留待后续）
  useEffect(() => {
    document.documentElement.classList.add('dark')
  }, [])

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
    <div className="flex h-screen overflow-hidden bg-surface">
      {/* 侧栏 */}
      <aside className="w-[210px] flex-shrink-0 border-r border-surface-border bg-surface-card/50 p-2.5 overflow-y-auto">
        <div className="px-3 py-3 flex items-center gap-2 font-extrabold text-[15px]">
          <span className="w-[22px] h-[22px] flex items-center justify-center bg-nv-green rounded-md text-black">⬢</span>
          <span className="text-white">N-SLMCRS</span>
          <span className="text-surface-muted text-[11px] font-normal px-1.5 py-0.5 rounded bg-surface-card-hover border border-surface-border">v0.8.0</span>
        </div>
        <nav className="mt-1">
          {NAV.map((sec) => (
            <div key={sec.group} className="mt-3">
              <div className="px-3 pb-1.5 text-[10px] font-semibold tracking-wider text-surface-muted uppercase">
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
        <header className="h-13 px-6 flex items-center gap-3 border-b border-surface-border bg-surface-card/50">
          <div className="flex-1" />
          <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-surface-card-hover border border-surface-border text-[12px] text-gray-400">
            <Activity className="w-3 h-3 text-nv-green" />
            {t('common.running')}
          </div>
          <button
            onClick={toggleLang}
            className="btn-outline"
            title="切换语言"
          >
            <Languages className="w-3.5 h-3.5" />
            {i18n.language === 'zh' ? '中' : 'EN'}
          </button>
          {getToken() ? (
            <button
              onClick={() => { clearToken(); setTok(''); setMustChange(false); navigate('/'); window.location.reload() }}
              className="btn-ghost"
            >
              <LogOut className="w-3.5 h-3.5" />
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
              <button type="submit" disabled={loginBusy} className="btn-primary py-1 text-xs">
                {loginBusy ? '...' : t('common.login')}
              </button>
              {loginErr && <span className="text-[11px] text-red-400 ml-1">{loginErr}</span>}
            </form>
          )}
        </header>

        {/* 内容 */}
        <main className="flex-1 overflow-y-auto bg-surface relative">
          <div className="relative p-6 max-w-[1400px] mx-auto">
            <Outlet />
          </div>
        </main>
      </div>

      {/* 强制修改密码模态（首登或仍用默认 admin 时弹出，遮挡全部交互） */}
      {mustChange && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/70 backdrop-blur-sm">
          <div className="w-[420px] card p-6 animate-slide-up">
            <div className="flex items-center gap-2.5 mb-1">
              <span className="w-7 h-7 flex items-center justify-center rounded-md bg-amber-400/15 border border-amber-400/30 text-amber-400">⚠</span>
              <h2 className="text-[15px] font-bold text-white">{t('common.change_password')}</h2>
            </div>
            <p className="text-[12px] text-surface-muted mb-4 leading-relaxed">{t('common.must_change_password_hint')}</p>
            <form onSubmit={submitChange} className="space-y-3">
              <div>
                <label className="text-[11px] text-surface-muted">{t('common.current_password')}</label>
                <input className="input mt-1" type="password" value={pwForm.current}
                  onChange={(e) => setPwForm({ ...pwForm, current: e.target.value })} required />
              </div>
              <div>
                <label className="text-[11px] text-surface-muted">{t('common.new_password')}</label>
                <input className="input mt-1" type="password" value={pwForm.next}
                  onChange={(e) => { setPwForm({ ...pwForm, next: e.target.value }); setPwErr('') }} required />
                <div className="text-[10px] text-surface-muted mt-1">≥ 6 字符，且不能为 ADMIN</div>
              </div>
              <div>
                <label className="text-[11px] text-surface-muted">{t('common.confirm_password')}</label>
                <input className="input mt-1" type="password" value={pwForm.confirm}
                  onChange={(e) => setPwForm({ ...pwForm, confirm: e.target.value })} required />
              </div>
              {pwErr && <div className="text-[12px] text-red-400">{pwErr}</div>}
              <button type="submit" disabled={pwBusy} className="btn-primary w-full">
                {pwBusy ? t('common.saving') : t('common.save')}
              </button>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
