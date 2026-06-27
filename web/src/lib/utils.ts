import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

// cn 合并 Tailwind 类名（shadcn 标准工具），后者覆盖前者冲突。
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
