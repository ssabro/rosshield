import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'

import { StatusBadge } from './StatusBadge'

describe('StatusBadge', () => {
  it('renders default label per status kind', () => {
    render(<StatusBadge status="success" />)
    expect(screen.getByText('성공')).toBeInTheDocument()
  })

  it('allows label override (i18n caller resolves t())', () => {
    render(<StatusBadge status="running" label="Scanning…" />)
    expect(screen.getByText('Scanning…')).toBeInTheDocument()
  })

  it('shows animated dot for running status (no static svg icon)', () => {
    const { container } = render(<StatusBadge status="running" />)
    // running 변형은 animated dot (svg 대신 span pair)을 사용.
    expect(container.querySelector('svg')).toBeNull()
  })

  it('shows icon for non-running statuses', () => {
    const { container } = render(<StatusBadge status="failed" />)
    expect(container.querySelector('svg')).not.toBeNull()
  })

  it('exposes data-status for QA selectors', () => {
    const { container } = render(<StatusBadge status="paused" />)
    expect(container.firstChild).toHaveAttribute('data-status', 'paused')
  })
})
