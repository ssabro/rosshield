import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Plus } from 'lucide-react'

import { EmptyState } from './EmptyState'

// EmptyState 보강 — 기존 호출(11곳) 호환 + 신규 variant/size 검증.
describe('EmptyState', () => {
  it('renders title and description (backward compatible API)', () => {
    render(
      <EmptyState
        title="결과 없음"
        description="필터를 변경해 보세요"
      />,
    )
    expect(screen.getByText('결과 없음')).toBeInTheDocument()
    expect(screen.getByText('필터를 변경해 보세요')).toBeInTheDocument()
  })

  it('respects icon prop override over variant default', () => {
    const { container } = render(
      <EmptyState
        icon={Plus}
        title="추가하세요"
        variant="no-data"
      />,
    )
    // Plus 아이콘이 렌더되어야 함 (svg 1개 존재).
    expect(container.querySelector('svg')).not.toBeNull()
  })

  it('falls back to variant-default icon when icon prop omitted', () => {
    const { container } = render(
      <EmptyState title="검색 결과 없음" variant="search-no-result" />,
    )
    expect(container.querySelector('svg')).not.toBeNull()
  })

  it('renders action slot', () => {
    render(
      <EmptyState
        title="없음"
        action={<button>추가</button>}
      />,
    )
    expect(screen.getByRole('button', { name: '추가' })).toBeInTheDocument()
  })

  it('announces empty state via role=status', () => {
    render(<EmptyState title="비어있음" />)
    expect(screen.getByRole('status')).toBeInTheDocument()
  })

  it('applies size lg container class', () => {
    const { container } = render(
      <EmptyState title="X" size="lg" />,
    )
    const root = container.firstChild as HTMLElement
    expect(root.className).toContain('py-16')
  })
})
