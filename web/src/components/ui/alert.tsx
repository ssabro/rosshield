/**
 * Simple alert component for error and info messages.
 */

import React from 'react'

export interface AlertProps extends React.HTMLAttributes<HTMLDivElement> {
  readonly variant?: 'default' | 'destructive'
}

export const Alert = React.forwardRef<HTMLDivElement, AlertProps>(
  ({ className = '', variant = 'default', ...props }, ref) => {
    const variantClasses =
      variant === 'destructive'
        ? 'border-red-200 bg-red-50 text-red-800'
        : 'border-slate-200 bg-slate-50 text-slate-800'

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
