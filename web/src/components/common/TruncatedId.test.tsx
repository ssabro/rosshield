// TruncatedId 단위 테스트.
//
// 검증:
//   1. 긴 ID는 prefix + … + suffix로 축약
//   2. 짧은 ID(임계 이하)는 그대로 표시
//   3. title attribute에 full ID 보존
//   4. copy 버튼 클릭 → clipboard.writeText 호출
//   5. showCopy=false 시 button 부재

import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

import { TruncatedId } from './TruncatedId'

// sonner toast는 jsdom에서 portal 마운트 시 noisy — silence하되 호출 자체는 무영향.
vi.mock('@/lib/toast', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
    message: vi.fn(),
    promise: vi.fn(),
    dismiss: vi.fn(),
  },
}))

describe('TruncatedId', () => {
  it('긴 ID를 prefix + … + suffix로 축약', () => {
    render(<TruncatedId id="scs_01KRZFS5GWPFQ2EYA0W26DE22Y" />)
    // default prefixLen=4 (scs_), suffixLen=4 (E22Y)
    expect(screen.getByText('scs_…E22Y')).toBeInTheDocument()
  })

  it('짧은 ID(임계 이하)는 그대로 표시', () => {
    // prefixLen=4 + suffixLen=4 + ellipsis 2 = 10 이하면 그대로
    render(<TruncatedId id="ro_short" />)
    expect(screen.getByText('ro_short')).toBeInTheDocument()
  })

  it('title attribute로 full ID 보존', () => {
    const full = 'fl_01KRZFS5GWPFQ2EYA0W26DE22Y'
    const { container } = render(<TruncatedId id={full} />)
    const span = container.querySelector('[data-truncated-id]')
    expect(span).toHaveAttribute('title', full)
    expect(span).toHaveAttribute('data-truncated-id', full)
  })

  it('copy 버튼 클릭 → clipboard.writeText 호출', async () => {
    const user = userEvent.setup()
    const writeText = vi.fn().mockResolvedValue(undefined)
    // jsdom Navigator.clipboard는 getter — defineProperty로 mock 주입.
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })

    const full = 'scs_01KRZFS5GWPFQ2EYA0W26DE22Y'
    render(<TruncatedId id={full} />)
    const button = screen.getByRole('button', { name: `${full} 복사` })
    await user.click(button)

    expect(writeText).toHaveBeenCalledWith(full)
  })

  it('showCopy=false 시 button 부재', () => {
    render(<TruncatedId id="scs_01KRZFS5GWPFQ2EYA0W26DE22Y" showCopy={false} />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('prefixLen/suffixLen override 적용', () => {
    render(
      <TruncatedId
        id="tn_01KRZFS5GWPFQ2EYA0W26DE22Y"
        prefixLen={3}
        suffixLen={6}
        showCopy={false}
      />,
    )
    // prefix 3 (tn_) + … + suffix 6 (DE22Y) → wait, suffix=6 마지막 6자 = '6DE22Y'
    expect(screen.getByText('tn_…6DE22Y')).toBeInTheDocument()
  })
})
