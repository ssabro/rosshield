import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs))
}

// formatBytes — 1024-base human-readable size (B / KB / MB / GB / TB).
//
// 음수·NaN·Infinity는 '—' 반환 (UI에서 안전한 placeholder).
// 100 미만 단위는 소수점 1자리, 100 이상은 정수.
//
// 예: formatBytes(1024) → "1.0 KB", formatBytes(1500000) → "1.4 MB",
//     formatBytes(536870912) → "512 MB", formatBytes(0) → "0 B".
//
// 사용처 (B7 Stage 2-C 이후):
//   - /system 페이지 BackupsCard (백업 파일 크기)
//   - 향후 evidence·report 크기 표시 등
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '—'
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i += 1
  }
  const formatted = v >= 100 ? Math.round(v).toString() : v.toFixed(1)
  return `${formatted} ${units[i]}`
}
