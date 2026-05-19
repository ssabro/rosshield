// D-UI-2 — TruncatedId 공통 컴포넌트.
//
// 사용처: 긴 ULID/UUID(예: scs_01KRZFS5GWPFQ2EYA0W26DE22Y, fl_01...) 등을 테이블·
// 카드·dialog 헤더에 표시할 때 prefix + "…" + suffix 형식으로 축약.
//
// 기능:
//   - prefix N자(default 4: 도메인 접두 "scs_"/"fl_"/"ro_" 등을 그대로 유지) + ellipsis +
//     suffix N자(default 4: 마지막 식별자 일부)
//   - tooltip(`title`)으로 full ID 노출 — hover로 즉시 확인
//   - copy 버튼 클릭 시 clipboard에 full ID 기록 + 2초 동안 ✓ 표시
//   - row click 이벤트 충돌 방지: handleCopy에서 stopPropagation
//
// 디자인 원칙:
//   - presentational only — 데이터 fetch · 비즈 로직 0
//   - 단일 책임: ID 축약 표시 + 복사
//   - i18n key는 의도적으로 호출지 t() 풀이를 받지 않는다 — 짧은 영어 토스트 'ID copied'
//     수준이라면 i18n 의존을 피해 import 가벼움. (호출지에서 wrap 가능)

import { Check, Copy } from 'lucide-react'
import { useState } from 'react'

import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'

import type { MouseEvent } from 'react'

export interface TruncatedIdProps {
  /** 전체 ID 문자열 (예: "scs_01KRZFS5GWPFQ2EYA0W26DE22Y") */
  id: string
  /** prefix 보존 길이 (default 4 — 도메인 접두 "scs_" 등 유지) */
  prefixLen?: number
  /** suffix 보존 길이 (default 4 — 마지막 식별자 일부) */
  suffixLen?: number
  /** 복사 버튼 표시 여부 (default true) */
  showCopy?: boolean
  /** 외부 className 머지 */
  className?: string
}

/**
 * 긴 ULID/UUID를 prefix + … + suffix로 축약 표시한다.
 *
 * - `id.length`가 `prefixLen + suffixLen + 2`(ellipsis 2 character) 이하면 truncate 없이
 *   전체를 표시 — 짧은 ID(예: "tnA")가 들어와도 깨지지 않는다.
 * - title attribute로 full ID hover 표시 — assistive tech 호환.
 * - copy 버튼은 row click 등 상위 핸들러와 충돌하지 않도록 stopPropagation.
 */
export function TruncatedId({
  id,
  prefixLen = 4,
  suffixLen = 4,
  showCopy = true,
  className,
}: TruncatedIdProps): React.ReactElement {
  const [copied, setCopied] = useState(false)

  const tooShort = id.length <= prefixLen + suffixLen + 2
  const display = tooShort
    ? id
    : `${id.slice(0, prefixLen)}…${id.slice(-suffixLen)}`

  const handleCopy = async (e: MouseEvent<HTMLButtonElement>): Promise<void> => {
    e.stopPropagation()
    e.preventDefault()
    if (typeof navigator === 'undefined' || !navigator.clipboard) {
      toast.error('clipboard 접근 실패')
      return
    }
    try {
      await navigator.clipboard.writeText(id)
      setCopied(true)
      toast.success('ID 복사됨', { description: id })
      setTimeout(() => setCopied(false), 2000)
    } catch {
      toast.error('clipboard 접근 실패')
    }
  }

  return (
    <span
      className={cn('inline-flex items-center gap-1 font-mono text-xs', className)}
      title={id}
      data-truncated-id={id}
    >
      <span>{display}</span>
      {showCopy && (
        <button
          type="button"
          onClick={(e) => {
            void handleCopy(e)
          }}
          className="opacity-50 transition-opacity hover:opacity-100"
          aria-label={`${id} 복사`}
        >
          {copied ? (
            <Check className="size-3 text-green-600" />
          ) : (
            <Copy className="size-3" />
          )}
        </button>
      )}
    </span>
  )
}
