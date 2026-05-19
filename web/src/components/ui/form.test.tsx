import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from './form'
import { Input } from './input'

// Form pilot 검증 — react-hook-form + zod 통합 표준 패턴이 a11y 속성(htmlFor,
// aria-describedby, aria-invalid)을 자동 결선하는지 확인.

const schema = z.object({
  email: z.string().email('유효한 이메일을 입력하세요'),
})
type Values = z.infer<typeof schema>

function PilotForm({
  onSubmit,
}: {
  onSubmit?: (v: Values) => void
}): React.ReactElement {
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { email: '' },
    mode: 'onSubmit',
  })
  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((v) => onSubmit?.(v))}
        noValidate
      >
        <FormField
          control={form.control}
          name="email"
          render={({ field }) => (
            <FormItem>
              <FormLabel>이메일</FormLabel>
              <FormControl>
                <Input type="email" {...field} />
              </FormControl>
              <FormDescription>로그인에 사용한 이메일</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <button type="submit">제출</button>
      </form>
    </Form>
  )
}

describe('Form (shadcn/ui + react-hook-form + zod)', () => {
  it('wires label htmlFor → input id automatically', () => {
    render(<PilotForm />)
    const input = screen.getByLabelText('이메일')
    expect(input).toBeInTheDocument()
    expect(input.tagName).toBe('INPUT')
  })

  it('renders FormDescription with aria-describedby linkage', () => {
    render(<PilotForm />)
    const input = screen.getByLabelText('이메일')
    const describedBy = input.getAttribute('aria-describedby')
    expect(describedBy).toBeTruthy()
    // description 노드는 항상 존재 (id 일치).
    const descId = describedBy!.split(' ')[0]
    const desc = document.getElementById(descId)
    expect(desc?.textContent).toBe('로그인에 사용한 이메일')
  })

  it('shows zod validation error on invalid submit + sets aria-invalid', async () => {
    const user = userEvent.setup()
    render(<PilotForm />)
    await user.type(screen.getByLabelText('이메일'), 'not-an-email')
    await user.click(screen.getByRole('button', { name: '제출' }))
    expect(
      await screen.findByText('유효한 이메일을 입력하세요'),
    ).toBeInTheDocument()
    const input = screen.getByLabelText('이메일')
    expect(input).toHaveAttribute('aria-invalid', 'true')
  })

  it('invokes onSubmit with parsed values on valid input', async () => {
    const user = userEvent.setup()
    let captured: Values | null = null
    render(<PilotForm onSubmit={(v) => (captured = v)} />)
    await user.type(screen.getByLabelText('이메일'), 'a@b.com')
    await user.click(screen.getByRole('button', { name: '제출' }))
    // RHF submit는 microtask 후 호출되므로 짧게 양보.
    await new Promise((r) => setTimeout(r, 0))
    expect(captured).toEqual({ email: 'a@b.com' })
  })
})
