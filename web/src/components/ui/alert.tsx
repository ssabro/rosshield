/**
 * Simple alert component for error and info messages.
 */

import React from 'react'

export interface AlertProps extends React.HTMLAttributes<HTMLDivElement> {
  readonly variant?: 'default' | 'destructive'
}

export const Alert = React.forwardRef<HTMLDivElement, AlertProps>(
  ({ className = '', variant = 'default', ...props }, ref) => {
    // Visual review P0 #2 fix — hardcoded bg-red-50/bg-slate-50 제거.
    // 다크 모드 token (--destructive, --muted, --border, --foreground)으로 교체해
    // light·dark 모두 정상 대비.
    const variantClasses =
      variant === 'destructive'
        ? 'border-destructive/30 bg-destructive/10 text-destructive dark:text-destructive-foreground'
        : 'border-border bg-muted text-foreground'

    return (
      <div
        ref={ref}
        className={`rounded-lg border p-4 ${variantClasses} ${className}`}
        {...props}
      />
    )
  }
)
Alert.displayName = 'Alert'

export interface AlertDescriptionProps extends React.HTMLAttributes<HTMLDivElement> {}

export const AlertDescription = React.forwardRef<HTMLDivElement, AlertDescriptionProps>(
  ({ className = '', ...props }, ref) => (
    <div ref={ref} className={`text-sm ${className}`} {...props} />
  )
)
AlertDescription.displayName = 'AlertDescription'
