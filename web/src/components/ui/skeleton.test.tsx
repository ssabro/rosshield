import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'

import {
  CardSkeleton,
  PageSkeleton,
  Skeleton,
  TableRowSkeleton,
  TextSkeleton,
} from './skeleton'

describe('Skeleton primitives', () => {
  it('base Skeleton renders aria-hidden div with pulse class', () => {
    const { container } = render(<Skeleton className="h-8 w-8" />)
    const el = container.firstChild as HTMLElement
    expect(el).toHaveAttribute('aria-hidden')
    expect(el.className).toContain('animate-pulse')
  })

  it('TextSkeleton merges custom width via className', () => {
    const { container } = render(<TextSkeleton className="w-1/2" />)
    const el = container.firstChild as HTMLElement
    expect(el.className).toContain('w-1/2')
  })

  it('TableRowSkeleton renders requested row count', () => {
    const { container } = render(<TableRowSkeleton rows={3} columns={2} />)
    const wrapper = container.firstChild as HTMLElement
    expect(wrapper.children).toHaveLength(3)
  })

  it('CardSkeleton announces loading via role=status', () => {
    render(<CardSkeleton />)
    expect(screen.getByRole('status')).toBeInTheDocument()
  })

  it('PageSkeleton announces loading via role=status', () => {
    render(<PageSkeleton />)
    expect(screen.getByRole('status', { name: '페이지 불러오는 중' }))
      .toBeInTheDocument()
  })
})
