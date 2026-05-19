import { beforeEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'

import { useLocaleStore } from '@/i18n/store'
import { SeverityBadge } from './SeverityBadge'

// SeverityBadge — a11y review P0 대응: 색 + icon + text 3중 채널 검증.
describe('SeverityBadge', () => {
  beforeEach(() => {
    useLocaleStore.setState({ locale: 'ko' })
  })

  it('renders Korean label for each severity', () => {
    render(<SeverityBadge severity="critical" />)
    expect(screen.getByText('치명적')).toBeInTheDocument()
  })

  it('exposes data-severity attribute for QA/E2E selectors', () => {
    const { container } = render(<SeverityBadge severity="high" />)
    expect(container.firstChild).toHaveAttribute('data-severity', 'high')
  })

  it('respects showIcon=false', () => {
    const { container } = render(
      <SeverityBadge severity="medium" showIcon={false} />,
    )
    // showIcon false 시 svg(icon) 미포함.
    expect(container.querySelector('svg')).toBeNull()
  })

  it('renders icon by default for non-text channel', () => {
    const { container } = render(<SeverityBadge severity="low" />)
    expect(container.querySelector('svg')).not.toBeNull()
  })
})
