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

// --- v0.7 新增原语（大厂级设计系统扩展）---

// Skeleton 加载占位骨架（脉冲呼吸）。
export function Skeleton({ className = '' }: { className?: string }) {
  return <div className={cn('animate-pulse rounded-md bg-surface-card-hover', className)} />
}

// Separator 分隔线。
export function Separator({ className = '' }: { className?: string }) {
  return <div className={cn('h-px w-full bg-surface-border', className)} />
}

// Progress 进度条（替代手写 div）。
export function Progress({ value, className = '', indicatorClassName = '' }: { value: number; className?: string; indicatorClassName?: string }) {
  return (
    <div className={cn('h-1.5 w-full rounded-full bg-surface-card-hover overflow-hidden', className)}>
      <div
        className={cn('h-full rounded-full transition-all duration-500', indicatorClassName)}
        style={{ width: `${Math.max(0, Math.min(100, value))}%` }}
      />
    </div>
  )
}

// Tabs 选项卡（模型详情二/三级页用）。
interface TabsProps {
  tabs: Array<{ id: string; label: string; count?: number }>
  active: string
  onChange: (id: string) => void
  className?: string
}
export function Tabs({ tabs, active, onChange, className = '' }: TabsProps) {
  return (
    <div className={cn('flex items-center gap-1 border-b border-surface-border', className)}>
      {tabs.map((t) => (
        <button
          key={t.id}
          onClick={() => onChange(t.id)}
          className={cn(
            'px-3.5 py-2 text-[12.5px] font-medium transition-colors border-b-2 -mb-px',
            active === t.id
              ? 'border-nv-green text-nv-green'
              : 'border-transparent text-surface-muted hover:text-gray-200',
          )}
        >
          {t.label}
          {t.count !== undefined && (
            <span className="ml-1.5 text-[10px] text-surface-muted">{t.count}</span>
          )}
        </button>
      ))}
    </div>
  )
}

// Tooltip 极简悬停提示（CSS-only，无额外依赖）。
export function Tooltip({ label, children }: { label: string; children: ReactNode }) {
  return (
    <span className="relative group inline-flex">
      {children}
      <span className="pointer-events-none absolute left-1/2 -translate-x-1/2 bottom-full mb-1.5 px-2 py-1 rounded-md bg-surface-card border border-surface-border text-[10.5px] text-gray-200 whitespace-nowrap opacity-0 group-hover:opacity-100 transition-opacity z-50 shadow-lg">
        {label}
      </span>
    </span>
  )
}

// useCountUp 数字滚动动画（KPI 大数字入场）。
import { useEffect, useRef, useState } from 'react'
export function useCountUp(target: number, durationMs = 600) {
  const [val, setVal] = useState(0)
  const ref = useRef(0)
  useEffect(() => {
    const start = ref.current
    const t0 = performance.now()
    let raf = 0
    const step = (now: number) => {
      const p = Math.min(1, (now - t0) / durationMs)
      const eased = 1 - Math.pow(1 - p, 3)
      const v = start + (target - start) * eased
      ref.current = v
      setVal(v)
      if (p < 1) raf = requestAnimationFrame(step)
    }
    raf = requestAnimationFrame(step)
    return () => cancelAnimationFrame(raf)
  }, [target, durationMs])
  return val
}

// DataTable 通用表格（Keys/Health/Logs 复用，统一大厂级表头/悬浮/对齐）。
interface Column<T> {
  key: string
  header: string
  align?: 'left' | 'right'
  render: (row: T, i: number) => ReactNode
  className?: string
}
export function DataTable<T>({ columns, rows, rowKey, onRowClick, empty }: {
  columns: Array<Column<T>>
  rows: T[]
  rowKey: (row: T, i: number) => string | number
  onRowClick?: (row: T) => void
  empty?: ReactNode
}) {
  if (rows.length === 0 && empty) return <>{empty}</>
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[12.5px]">
        <thead>
          <tr className="bg-surface-card-hover text-surface-muted text-[10.5px] uppercase tracking-wider border-b border-surface-border">
            {columns.map((c) => (
              <th key={c.key} className={cn('px-3 py-2.5 font-semibold', c.align === 'right' ? 'text-right' : 'text-left', c.className)}>
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr
              key={rowKey(row, i)}
              onClick={onRowClick ? () => onRowClick(row) : undefined}
              className={cn('border-b border-surface-border/60 hover:bg-surface-card-hover transition-colors', onRowClick && 'cursor-pointer')}
            >
              {columns.map((c) => (
                <td key={c.key} className={cn('px-3 py-2.5', c.align === 'right' ? 'text-right' : 'text-left', c.className)}>
                  {c.render(row, i)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export { buttonVariants, badgeVariants }
