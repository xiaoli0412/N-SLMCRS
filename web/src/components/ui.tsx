// 可复用 UI 组件（shadcn 风格，扁平暗色主题）
import { cva, type VariantProps } from 'class-variance-authority'
import { Loader2 } from 'lucide-react'
import type { ButtonHTMLAttributes, HTMLAttributes, ReactNode } from 'react'
import { cn } from '../lib/utils'

// --- shadcn 风格原语 ---

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-1.5 rounded-lg text-sm font-medium transition-colors disabled:opacity-50 disabled:pointer-events-none focus:outline-none focus:ring-1 focus:ring-nv-green/40',
  {
    variants: {
      variant: {
        default: 'bg-nv-green text-black hover:bg-nv-green-bright font-semibold',
        outline: 'border border-surface-border text-gray-300 hover:bg-surface-card-hover hover:text-white',
        ghost: 'text-gray-300 hover:bg-surface-card-hover',
        subtle: 'bg-surface-card-hover text-gray-200 hover:bg-surface-border',
        destructive: 'bg-red-500/15 text-red-400 border border-red-500/30 hover:bg-red-500/25',
      },
      size: {
        default: 'px-4 py-2',
        sm: 'px-3 py-1.5 text-xs',
        icon: 'h-8 w-8 p-0',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

export interface ButtonProps
  extends ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {}

export function Button({ className, variant, size, ...props }: ButtonProps) {
  return <button className={cn(buttonVariants({ variant, size }), className)} {...props} />
}

const badgeVariants = cva(
  'inline-flex items-center gap-1 px-2 py-0.5 rounded-md border text-[11px] font-medium',
  {
    variants: {
      variant: {
        default: 'border-surface-border bg-surface-card-hover text-gray-300',
        success: 'border-nv-green/30 bg-nv-green/10 text-nv-green',
        warn: 'border-amber-400/30 bg-amber-400/10 text-amber-400',
        danger: 'border-red-500/30 bg-red-500/10 text-red-400',
        info: 'border-sky-500/30 bg-sky-500/10 text-sky-400',
      },
    },
    defaultVariants: { variant: 'default' },
  },
)

export interface BadgeProps
  extends HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />
}

// --- 既有组件（restyle，保持导出名以兼容所有页面）---

export function PageHeader({ title, en, subtitle }: { title: string; en?: string; subtitle?: string }) {
  return (
    <div className="mb-5 animate-slide-up">
      <h1 className="text-[22px] font-bold text-white tracking-tight">
        {title}
        {en && <Badge variant="success" className="ml-2 font-mono">{en}</Badge>}
      </h1>
      {subtitle && <p className="text-surface-muted text-[13px] mt-1">{subtitle}</p>}
    </div>
  )
}

interface KpiCardProps {
  label: string
  value: string | number
  unit?: string
  trend?: string
  trendUp?: boolean
  accent?: boolean
}

export function KpiCard({ label, value, unit, trend, trendUp, accent }: KpiCardProps) {
  return (
    <div className="kpi-card">
      <div className="text-[10px] font-semibold uppercase tracking-wider text-surface-muted">{label}</div>
      <div className={`text-[26px] font-bold mt-1.5 tracking-tight ${accent ? 'text-nv-green' : 'text-white'}`}>
        {value}
        {unit && <span className="text-[13px] text-surface-muted font-medium ml-0.5">{unit}</span>}
      </div>
      {trend && (
        <div className="text-[11px] text-surface-muted mt-0.5">
          <span className={trendUp ? 'text-nv-green' : 'text-red-400'}>{trendUp ? '↑' : '↓'} </span>
          {trend}
        </div>
      )}
    </div>
  )
}

export function StatusBadge({ status }: { status: string }) {
  const map: Record<string, { variant: 'success' | 'warn' | 'danger' | 'default'; dot: string }> = {
    active: { variant: 'success', dot: 'ok' },
    half_open: { variant: 'warn', dot: 'warn' },
    cooling: { variant: 'warn', dot: 'warn' },
    circuit_open: { variant: 'danger', dot: 'err' },
    disabled: { variant: 'default', dot: '' },
  }
  const s = map[status] || map.disabled
  return (
    <Badge variant={s.variant}>
      {s.dot && <span className={`status-dot ${s.dot}`} />}
      {status}
    </Badge>
  )
}

export function Spinner() {
  return (
    <div className="flex items-center justify-center py-12">
      <Loader2 className="w-6 h-6 text-nv-green animate-spin" />
    </div>
  )
}

export function EmptyState({ text }: { text: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 text-surface-muted">
      <div className="text-3xl mb-2 opacity-40">⬢</div>
      <div className="text-sm">{text}</div>
    </div>
  )
}

export function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <div className={cn('card p-5', className)}>{children}</div>
}

export { buttonVariants, badgeVariants }
