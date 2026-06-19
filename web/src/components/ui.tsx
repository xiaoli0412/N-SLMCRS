// 可复用 UI 组件

export function PageHeader({ title, en, subtitle }: { title: string; en?: string; subtitle?: string }) {
  return (
    <div className="mb-5 animate-slide-up">
      <h1 className="text-[22px] font-bold text-white tracking-tight">
        {title}
        {en && <span className="ml-2 text-[11px] font-semibold text-nv-green bg-nv-green/10 border border-nv-green/20 px-2 py-0.5 rounded">{en}</span>}
      </h1>
      {subtitle && <p className="text-gray-600 text-[13px] mt-1">{subtitle}</p>}
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
      <div className="text-[10px] font-semibold uppercase tracking-wider text-gray-500">{label}</div>
      <div className={`text-[26px] font-bold mt-1.5 tracking-tight ${accent ? 'text-nv-green' : 'text-white'}`}>
        {value}
        {unit && <span className="text-[13px] text-gray-500 font-medium ml-0.5">{unit}</span>}
      </div>
      {trend && (
        <div className="text-[11px] text-gray-600 mt-0.5">
          <span className={trendUp ? 'text-nv-green' : 'text-red-400'}>{trendUp ? '↑' : '↓'} </span>
          {trend}
        </div>
      )}
    </div>
  )
}

export function StatusBadge({ status }: { status: string }) {
  const map: Record<string, { cls: string; dot: string }> = {
    active: { cls: 'text-nv-green bg-nv-green/10 border-nv-green/20', dot: 'ok' },
    cooling: { cls: 'text-amber-400 bg-amber-400/10 border-amber-400/20', dot: 'warn' },
    circuit_open: { cls: 'text-red-400 bg-red-500/10 border-red-500/20', dot: 'err' },
    disabled: { cls: 'text-gray-500 bg-white/[0.03] border-white/[0.06]', dot: '' },
  }
  const s = map[status] || map.disabled
  return (
    <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] border ${s.cls}`}>
      {s.dot && <span className={`status-dot ${s.dot}`} />}
      {status}
    </span>
  )
}

export function Spinner() {
  return (
    <div className="flex items-center justify-center py-12">
      <div className="w-6 h-6 border-2 border-nv-green/30 border-t-nv-green rounded-full animate-spin" />
    </div>
  )
}

export function EmptyState({ text }: { text: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 text-gray-600">
      <div className="text-3xl mb-2 opacity-50">⬢</div>
      <div className="text-sm">{text}</div>
    </div>
  )
}

export function Card({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={`glass-card p-5 ${className}`}>{children}</div>
}
