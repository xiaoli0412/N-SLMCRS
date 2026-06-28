import { NavLink, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import {
  LayoutDashboard, Boxes, Activity, KeyRound, Share2, Bot, ScrollText,
  DatabaseBackup, Settings, ShieldAlert, LogOut, Menu, X, MessageSquare,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Toaster, toast } from 'sonner'
import { api, getToken, setToken, clearToken } from '@/api'
import { ThemeToggle } from './theme-provider'
import { toggleLang } from '@/i18n'
import { Button, Input, Label, Dialog, DialogContent, DialogHeader, DialogTitle } from './ui'
import { cn } from '@/lib/utils'

const NAV = [
  { to: '/', icon: LayoutDashboard, key: 'overview' },
  { to: '/models', icon: Boxes, key: 'models' },
  { to: '/playground', icon: MessageSquare, key: 'playground' },
  { to: '/circuit', icon: ShieldAlert, key: 'circuit' },
  { to: '/operations', icon: Activity, key: 'operations' },
  { to: '/keys', icon: KeyRound, key: 'keys' },
  { to: '/distribution', icon: Share2, key: 'distribution' },
  { to: '/autopilot', icon: Bot, key: 'autopilot' },
  { to: '/logs', icon: ScrollText, key: 'logs' },
  { to: '/backup', icon: DatabaseBackup, key: 'backup' },
  { to: '/settings', icon: Settings, key: 'settings' },
] as const

export function Layout({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const authed = !!getToken()

  if (!authed) return <LoginGate>{children}</LoginGate>

  return (
    <div className="flex h-screen overflow-hidden bg-background">
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-40 w-60 shrink-0 border-r bg-sidebar transition-transform md:static md:translate-x-0',
          open ? 'translate-x-0' : '-translate-x-full',
        )}
      >
        <div className="flex h-14 items-center gap-2 border-b px-4">
          <Logo />
          <span className="font-semibold tracking-tight">N-SLMCRS</span>
        </div>
        <nav className="flex flex-col gap-0.5 p-2">
          {NAV.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.to === '/'}
              onClick={() => setOpen(false)}
              className={({ isActive }) =>
                cn(
                  'flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors',
                  isActive
                    ? 'bg-primary/10 text-primary font-medium'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                )
              }
            >
              <n.icon className="h-4 w-4" />
              {t(`nav.${n.key}`)}
            </NavLink>
          ))}
        </nav>
      </aside>

      {open && (
        <div className="fixed inset-0 z-30 bg-black/40 md:hidden" onClick={() => setOpen(false)} />
      )}

      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-14 items-center gap-2 border-b px-4">
          <Button variant="ghost" size="icon" className="md:hidden" onClick={() => setOpen((v) => !v)}>
            {open ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
          </Button>
          <div className="flex-1" />
          <LangToggle />
          <ThemeToggle />
          <Button variant="ghost" size="sm" onClick={() => { clearToken(); location.reload() }}>
            <LogOut className="h-4 w-4" />
          </Button>
        </header>
        <main className="flex-1 overflow-auto p-6">{children}</main>
      </div>
      <Toaster position="top-right" richColors />
    </div>
  )
}

function Logo() {
  return (
    <div className="flex h-7 w-7 items-center justify-center rounded-md bg-primary text-primary-foreground">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 2 2 7v10l10 5 10-5V7z" opacity="0.9" />
      </svg>
    </div>
  )
}

function LangToggle() {
  const { i18n } = useTranslation()
  return (
    <button
      onClick={toggleLang}
      className="inline-flex h-9 items-center rounded-md border border-border px-2 text-xs font-medium text-muted-foreground hover:bg-muted"
    >
      {i18n.language === 'zh' ? 'EN' : '中'}
    </button>
  )
}

// LoginGate：未登录则显示登录框；登录后若需强制改密则弹改密框。
function LoginGate({ children }: { children: ReactNode }) {
  const [token, setTok] = useState('')
  const [changing, setChanging] = useState(false)
  const [cur, setCur] = useState('')
  const [next, setNext] = useState('')
  const qc = useQueryClient()
  const nav = useNavigate()

  const statusQ = useQuery({
    queryKey: ['auth-status'],
    queryFn: api.authStatus,
    enabled: !!getToken(),
  })

  const loginM = useMutation({
    mutationFn: () => api.login(token),
    onSuccess: (r) => {
      setToken(token)
      if (r.must_change_password) setChanging(true)
      else { qc.invalidateQueries(); nav('/') }
      toast.success('登录成功')
    },
    onError: () => toast.error('令牌无效'),
  })

  const changeM = useMutation({
    mutationFn: () => api.changePassword(cur, next),
    onSuccess: () => { setToken(next); setChanging(false); qc.invalidateQueries(); nav('/'); toast.success('密码已更新') },
    onError: () => toast.error('改密失败'),
  })

  if (getToken() && statusQ.data && !statusQ.data.must_change_password) {
    return <Layout>{children}</Layout>
  }

  return (
    <div className="aurora-bg flex min-h-screen items-center justify-center bg-background p-4">
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        className="w-full max-w-sm rounded-xl border bg-card/80 p-8 shadow-xl backdrop-blur-md"
      >
        <div className="mb-6 flex flex-col items-center gap-2">
          <Logo />
          <h1 className="text-xl font-semibold">N-SLMCRS Gateway</h1>
          <p className="text-xs text-muted-foreground">管理控制台</p>
        </div>
        <div className="space-y-3">
          <Label>管理令牌</Label>
          <Input type="password" value={token} onChange={(e) => setTok(e.target.value)} placeholder="admin token" onKeyDown={(e) => e.key === 'Enter' && loginM.mutate()} />
          <Button className="w-full" disabled={!token || loginM.isPending} onClick={() => loginM.mutate()}>
            {loginM.isPending ? '登录中…' : '登录'}
          </Button>
        </div>
      </motion.div>

      <Dialog open={changing || (statusQ.data?.must_change_password ?? false)} onOpenChange={setChanging}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>首次登录请修改管理令牌</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div>
              <Label>当前令牌</Label>
              <Input type="password" value={cur} onChange={(e) => setCur(e.target.value)} />
            </div>
            <div>
              <Label>新令牌</Label>
              <Input type="password" value={next} onChange={(e) => setNext(e.target.value)} />
            </div>
            <Button className="w-full" disabled={!cur || !next} onClick={() => changeM.mutate()}>
              修改并登录
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
