import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import * as TabsPrimitive from '@radix-ui/react-tabs'
import * as SwitchPrimitive from '@radix-ui/react-switch'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import * as TooltipPrimitive from '@radix-ui/react-tooltip'
import * as LabelPrimitive from '@radix-ui/react-label'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 cursor-pointer',
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-primary/90 shadow-sm',
        destructive: 'bg-destructive text-destructive-foreground hover:bg-destructive/90',
        outline: 'border border-border bg-transparent hover:bg-muted',
        secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
        ghost: 'hover:bg-muted',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-9 px-4 py-2',
        sm: 'h-8 rounded-md px-3 text-xs',
        lg: 'h-10 rounded-md px-6',
        icon: 'h-9 w-9',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}
export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button'
    return <Comp className={cn(buttonVariants({ variant, size }), className)} ref={ref} {...props} />
  },
)
Button.displayName = 'Button'

export const Card = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn('rounded-lg border bg-card text-card-foreground shadow-sm', className)} {...props} />
  ),
)
Card.displayName = 'Card'
export const CardHeader = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => <div ref={ref} className={cn('flex flex-col space-y-1.5 p-5', className)} {...props} />,
)
CardHeader.displayName = 'CardHeader'
export const CardTitle = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => <div ref={ref} className={cn('font-semibold leading-none tracking-tight', className)} {...props} />,
)
CardTitle.displayName = 'CardTitle'
export const CardContent = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => <div ref={ref} className={cn('p-5 pt-0', className)} {...props} />,
)
CardContent.displayName = 'CardContent'

const badgeVariants = cva(
  'inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium transition-colors',
  {
    variants: {
      variant: {
        default: 'border-transparent bg-primary/15 text-primary',
        secondary: 'border-transparent bg-secondary text-secondary-foreground',
        success: 'border-transparent bg-success/15 text-success',
        warning: 'border-transparent bg-warning/15 text-warning',
        destructive: 'border-transparent bg-destructive/15 text-destructive',
        outline: 'text-foreground',
      },
    },
    defaultVariants: { variant: 'default' },
  },
)
export interface BadgeProps extends React.HTMLAttributes<HTMLDivElement>, VariantProps<typeof badgeVariants> {}
export function Badge({ className, variant, ...props }: BadgeProps) {
  return <div className={cn(badgeVariants({ variant }), className)} {...props} />
}

export const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...props }, ref) => (
    <input
      ref={ref}
      className={cn(
        'flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50',
        className,
      )}
      {...props}
    />
  ),
)
Input.displayName = 'Input'

export const Textarea = React.forwardRef<HTMLTextAreaElement, React.TextareaHTMLAttributes<HTMLTextAreaElement>>(
  ({ className, ...props }, ref) => (
    <textarea
      ref={ref}
      className={cn('flex min-h-20 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring', className)}
      {...props}
    />
  ),
)
Textarea.displayName = 'Textarea'

export const Label = React.forwardRef<HTMLLabelElement, React.LabelHTMLAttributes<HTMLLabelElement> & LabelPrimitive.LabelProps>(
  ({ className, ...props }, ref) => <LabelPrimitive.Root ref={ref} className={cn('text-sm font-medium leading-none peer-disabled:opacity-70', className)} {...props} />,
)
Label.displayName = 'Label'

export const Tabs = TabsPrimitive.Root
export const TabsList = React.forwardRef<HTMLDivElement, React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>>(
  ({ className, ...props }, ref) => (
    <TabsPrimitive.List ref={ref} className={cn('inline-flex items-center justify-center rounded-md bg-muted p-1 text-muted-foreground', className)} {...props} />
  ),
)
TabsList.displayName = 'TabsList'
export const TabsTrigger = React.forwardRef<HTMLButtonElement, React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>>(
  ({ className, ...props }, ref) => (
    <TabsPrimitive.Trigger ref={ref} className={cn('inline-flex items-center justify-center whitespace-nowrap rounded-sm px-3 py-1.5 text-sm font-medium ring-offset-background transition-all focus-visible:outline-none data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm', className)} {...props} />
  ),
)
TabsTrigger.displayName = 'TabsTrigger'
export const TabsContent = React.forwardRef<HTMLDivElement, React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>>(
  ({ className, ...props }, ref) => <TabsPrimitive.Content ref={ref} className={cn('mt-3 ring-offset-background focus-visible:outline-none', className)} {...props} />,
)
TabsContent.displayName = 'TabsContent'

export const Switch = React.forwardRef<HTMLButtonElement, React.ComponentPropsWithoutRef<typeof SwitchPrimitive.Root>>(
  ({ className, ...props }, ref) => (
    <SwitchPrimitive.Root
      ref={ref}
      className={cn('peer inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50 data-[state=checked]:bg-primary data-[state=unchecked]:bg-muted', className)}
      {...props}
    >
      <SwitchPrimitive.Thumb className={cn('pointer-events-none block h-4 w-4 rounded-full bg-background shadow-lg ring-0 transition-transform data-[state=checked]:translate-x-4 data-[state=unchecked]:translate-x-0')} />
    </SwitchPrimitive.Root>
  ),
)
Switch.displayName = 'Switch'

export const Dialog = DialogPrimitive.Root
export const DialogTrigger = DialogPrimitive.Trigger
export const DialogClose = DialogPrimitive.Close
export const DialogContent = React.forwardRef<HTMLDivElement, React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>>(
  ({ className, children, ...props }, ref) => (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm data-[state=open]:animate-in data-[state=closed]:animate-out" />
      <DialogPrimitive.Content
        ref={ref}
        className={cn('fixed left-1/2 top-1/2 z-50 grid w-full max-w-lg -translate-x-1/2 -translate-y-1/2 gap-4 border bg-card p-6 shadow-lg rounded-lg', className)}
        {...props}
      >
        {children}
        <DialogPrimitive.Close className="absolute right-4 top-4 rounded-sm opacity-70 hover:opacity-100">
          <X className="h-4 w-4" />
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  ),
)
DialogContent.displayName = 'DialogContent'
export const DialogHeader = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn('flex flex-col space-y-1.5', className)} {...props} />
)
export const DialogTitle = React.forwardRef<HTMLHeadingElement, React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>>(
  ({ className, ...props }, ref) => <DialogPrimitive.Title ref={ref} className={cn('text-lg font-semibold', className)} {...props} />,
)
DialogTitle.displayName = 'DialogTitle'
export const DialogDescription = React.forwardRef<HTMLParagraphElement, React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>>(
  ({ className, ...props }, ref) => <DialogPrimitive.Description ref={ref} className={cn('text-sm text-muted-foreground', className)} {...props} />,
)
DialogDescription.displayName = 'DialogDescription'

// Sheet（侧滑抽屉，二阶面板用）
export const Sheet = DialogPrimitive.Root
export const SheetTrigger = DialogPrimitive.Trigger
export const SheetContent = React.forwardRef<HTMLDivElement, React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & { side?: 'left' | 'right'}>(
  ({ className, children, side = 'right', ...props }, ref) => (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/50 backdrop-blur-sm" />
      <DialogPrimitive.Content
        ref={ref}
        className={cn(
          'fixed z-50 gap-4 bg-card shadow-xl transition-transform flex flex-col h-full w-full max-w-xl',
          side === 'right' && 'inset-y-0 right-0 border-l data-[state=open]:animate-in data-[state=closed]:animate-out',
          side === 'left' && 'inset-y-0 left-0 border-r',
          className,
        )}
        {...props}
      >
        {children}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  ),
)
SheetContent.displayName = 'SheetContent'

export const TooltipProvider = TooltipPrimitive.Provider
export const Tooltip = TooltipPrimitive.Root
export const TooltipTrigger = TooltipPrimitive.Trigger
export const TooltipContent = React.forwardRef<HTMLDivElement, React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>>(
  ({ className, ...props }, ref) => (
    <TooltipPrimitive.Content ref={ref} className={cn('z-50 overflow-hidden rounded-md border bg-popover px-2 py-1 text-xs text-popover-foreground shadow-md', className)} {...props} />
  ),
)
TooltipContent.displayName = 'TooltipContent'

export function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('animate-pulse rounded-md bg-muted', className)} {...props} />
}

export function Separator({ className }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('shrink-0 bg-border h-px w-full', className)} />
}

export function Progress({ value, className }: { value: number; className?: string }) {
  return (
    <div className={cn('h-2 w-full overflow-hidden rounded-full bg-muted', className)}>
      <div
        className="h-full rounded-full bg-primary transition-all"
        style={{ width: `${Math.max(0, Math.min(100, value))}%` }}
      />
    </div>
  )
}

export function EmptyState({ icon, title, desc }: { icon?: React.ReactNode; title: string; desc?: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
      {icon && <div className="mb-3 opacity-40">{icon}</div>}
      <p className="text-sm font-medium">{title}</p>
      {desc && <p className="mt-1 text-xs">{desc}</p>}
    </div>
  )
}

// v0.13：统一错误态（查询失败展示 + 重试）。与 EmptyState/Skeleton 配套，
// 消除各页内联「载入失败」文案，保证 loading/empty/error 三态一致。
export function ErrorState({
  title,
  desc,
  onRetry,
}: {
  title: string
  desc?: string
  onRetry?: () => void
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-16 text-muted-foreground">
      <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-destructive/70">
        <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
        <line x1="12" y1="9" x2="12" y2="13" />
        <line x1="12" y1="17" x2="12.01" y2="17" />
      </svg>
      <p className="text-sm font-medium text-destructive">{title}</p>
      {desc && <p className="max-w-md text-center text-xs">{desc}</p>}
      {onRetry && (
        <Button size="sm" variant="outline" onClick={onRetry}>
          重试
        </Button>
      )}
    </div>
  )
}

// v0.13：泛型可排序数据表。统一各页内联 <table> 的分页/排序/空态/载入态，
// 保证列宽对齐与可访问性（th scope、button 标题）。客户端排序，适合单节点中小数据量。
export interface DataTableColumn<T> {
  key: string
  header: React.ReactNode
  cell: (row: T) => React.ReactNode
  className?: string
  sortable?: boolean
  sortValue?: (row: T) => string | number
}

export function DataTable<T>({
  columns,
  data,
  rowKey,
  loading,
  empty,
  error,
  onRetry,
  onRowClick,
  className,
}: {
  columns: DataTableColumn<T>[]
  data: T[]
  rowKey: (row: T) => string | number
  loading?: boolean
  empty?: React.ReactNode
  error?: React.ReactNode
  onRetry?: () => void
  onRowClick?: (row: T) => void
  className?: string
}) {
  const [sortKey, setSortKey] = React.useState<string | null>(null)
  const [sortDir, setSortDir] = React.useState<'asc' | 'desc'>('desc')

  const toggleSort = (col: DataTableColumn<T>) => {
    if (!col.sortable) return
    if (sortKey === col.key) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortKey(col.key)
      setSortDir('desc')
    }
  }

  let rows = data
  if (sortKey) {
    const col = columns.find((c) => c.key === sortKey)
    if (col?.sortValue) {
      const get = col.sortValue
      rows = [...data].sort((a, b) => {
        const va = get(a)
        const vb = get(b)
        if (va < vb) return sortDir === 'asc' ? -1 : 1
        if (va > vb) return sortDir === 'asc' ? 1 : -1
        return 0
      })
    }
  }

  const colSpan = columns.length
  const body = loading ? (
    <tr>
      <td colSpan={colSpan} className="py-10">
        <div className="flex justify-center"><Skeleton className="h-6 w-2/3" /></div>
      </td>
    </tr>
  ) : error ? (
    <tr>
      <td colSpan={colSpan}>
        {typeof error === 'string' ? (
          <ErrorState title={error} onRetry={onRetry} />
        ) : (
          error
        )}
      </td>
    </tr>
  ) : rows.length === 0 ? (
    <tr>
      <td colSpan={colSpan}>{empty ?? <EmptyState title="暂无数据" />}</td>
    </tr>
  ) : (
    rows.map((row) => (
      <tr
        key={rowKey(row)}
        onClick={onRowClick ? () => onRowClick(row) : undefined}
        className={cn('border-t', onRowClick && 'cursor-pointer hover:bg-muted/50')}
      >
        {columns.map((c) => (
          <td key={c.key} className={cn('py-2', c.className)}>
            {c.cell(row)}
          </td>
        ))}
      </tr>
    ))
  )

  return (
    <div className={cn('overflow-x-auto', className)}>
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs text-muted-foreground">
            {columns.map((c) => (
              <th
                key={c.key}
                scope="col"
                className={cn('pb-2', c.sortable && 'cursor-pointer select-none', c.className)}
                onClick={() => toggleSort(c)}
                aria-sort={sortKey === c.key ? (sortDir === 'asc' ? 'ascending' : 'descending') : undefined}
              >
                <span className="inline-flex items-center gap-1">
                  {c.header}
                  {c.sortable && sortKey === c.key && <span aria-hidden>{sortDir === 'asc' ? '▲' : '▼'}</span>}
                </span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>{body}</tbody>
      </table>
    </div>
  )
}
