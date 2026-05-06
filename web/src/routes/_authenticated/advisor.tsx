import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { ApiError } from '@/api/errors'
import {
  useAdvisorConversation,
  useAdvisorConversations,
  useAskAdvisor,
} from '@/api/hooks'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import type { AdvisorConversation, AdvisorTurn } from '@/api/hooks'

// `/advisor` — LLM 옵트인 대화 (E19-3-B).
// - 좌: 대화 목록 (클릭 → 선택)
// - 우: 선택 대화의 turn 렌더링 + 새 질문 입력 (Ask)
// - 503 (LLM disabled) 응답 시 안내 메시지 — 옵트인 활성화 방법 안내
function AdvisorPage(): React.ReactElement {
  const conversations = useAdvisorConversations()
  const [selectedId, setSelectedId] = useState<string | null>(null)

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Advisor</h1>
        <p className="text-sm text-muted-foreground">
          자연어 질문 → LLM이 read-only tool로 컨텍스트 수집 → 설명 생성. 옵트인
          기능이라 서버 시작 시{' '}
          <code className="rounded bg-muted px-1">--llm-provider=ollama</code> 또는{' '}
          <code className="rounded bg-muted px-1">=anthropic</code> 활성화가 필요합니다.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-[18rem_1fr]">
        <ConversationsList
          conversations={conversations.data ?? []}
          isPending={conversations.isPending}
          isError={conversations.isError}
          error={conversations.error}
          selectedId={selectedId}
          onSelect={(id) => setSelectedId(id)}
          onNew={() => setSelectedId(null)}
        />
        <ConversationPanel
          conversationId={selectedId}
          onConversationCreated={(id) => setSelectedId(id)}
        />
      </div>
    </div>
  )
}

function ConversationsList({
  conversations,
  isPending,
  isError,
  error,
  selectedId,
  onSelect,
  onNew,
}: {
  conversations: AdvisorConversation[]
  isPending: boolean
  isError: boolean
  error: unknown
  selectedId: string | null
  onSelect: (id: string) => void
  onNew: () => void
}): React.ReactElement {
  return (
    <aside className="flex flex-col gap-2 rounded-md border p-3">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium">대화</h2>
        <Button size="sm" variant="outline" onClick={onNew}>
          새 대화
        </Button>
      </div>

      {isPending && (
        <p className="text-xs text-muted-foreground">불러오는 중…</p>
      )}
      {isError && (
        <p className="text-xs text-destructive">
          {error instanceof ApiError ? error.message : '대화 목록을 불러올 수 없습니다'}
        </p>
      )}
      {!isPending && !isError && conversations.length === 0 && (
        <p className="text-xs text-muted-foreground">(없음 — 새 대화로 시작)</p>
      )}

      <ul className="flex flex-col gap-1">
        {conversations.map((c) => (
          <li key={c.id}>
            <button
              type="button"
              onClick={() => onSelect(c.id)}
              className={
                selectedId === c.id
                  ? 'w-full rounded-md bg-accent px-2 py-1.5 text-left text-sm text-accent-foreground'
                  : 'w-full rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted'
              }
            >
              <div className="truncate font-medium" title={c.title}>
                {c.title || '(제목 없음)'}
              </div>
              <div className="text-[10px] text-muted-foreground">
                {new Date(c.updatedAt).toLocaleString('ko-KR')}
              </div>
            </button>
          </li>
        ))}
      </ul>
    </aside>
  )
}

function ConversationPanel({
  conversationId,
  onConversationCreated,
}: {
  conversationId: string | null
  onConversationCreated: (id: string) => void
}): React.ReactElement {
  const detail = useAdvisorConversation(conversationId ?? undefined)
  const ask = useAskAdvisor()
  const [question, setQuestion] = useState('')

  const turns: AdvisorTurn[] = detail.data?.turns ?? []

  const onSubmit = (e: React.FormEvent): void => {
    e.preventDefault()
    if (!question.trim() || ask.isPending) return
    ask.mutate(
      {
        conversationId: conversationId ?? undefined,
        question: question.trim(),
      },
      {
        onSuccess: (data) => {
          setQuestion('')
          if (!conversationId) {
            onConversationCreated(data.conversationId)
          }
        },
      },
    )
  }

  return (
    <section className="flex min-h-[24rem] flex-col gap-3 rounded-md border p-4">
      {!conversationId && turns.length === 0 && !ask.isPending && (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          새 대화를 시작하려면 아래에 질문을 입력하세요.
        </div>
      )}

      {detail.isPending && conversationId && (
        <p className="text-xs text-muted-foreground">대화 불러오는 중…</p>
      )}
      {detail.isError && (
        <p className="text-sm text-destructive">
          {detail.error instanceof ApiError
            ? detail.error.message
            : '대화를 불러올 수 없습니다'}
        </p>
      )}

      <ol className="flex flex-1 flex-col gap-3 overflow-y-auto">
        {turns.map((t) => (
          <TurnView key={t.id} turn={t} />
        ))}
      </ol>

      {ask.isError && (
        <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
          {resolveAskErrorMessage(ask.error)}
        </div>
      )}

      <form onSubmit={onSubmit} className="flex flex-col gap-2 border-t pt-3">
        <Label htmlFor="ask-input" className="text-xs">
          질문
        </Label>
        <Textarea
          id="ask-input"
          placeholder="예: 이 robot의 마지막 scan에서 ros1_no_password_node check가 왜 fail했나요?"
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          rows={3}
          disabled={ask.isPending}
        />
        <div className="flex justify-end">
          <Button type="submit" disabled={!question.trim() || ask.isPending}>
            {ask.isPending ? 'Ask 중…' : 'Ask'}
          </Button>
        </div>
      </form>
    </section>
  )
}

function TurnView({ turn }: { turn: AdvisorTurn }): React.ReactElement {
  const isUser = turn.role === 'user'
  const isTool = turn.role === 'tool'

  return (
    <li className="flex flex-col gap-1">
      <div className="flex items-center gap-2">
        <Badge variant={roleVariant(turn.role)}>{turn.role}</Badge>
        {turn.llmProvider && (
          <span className="font-mono text-[10px] text-muted-foreground">
            {turn.llmProvider}/{turn.llmModel}
            {typeof turn.inputTokens === 'number' &&
              typeof turn.outputTokens === 'number' && (
                <>
                  {' '}
                  · {turn.inputTokens}+{turn.outputTokens} tok
                </>
              )}
            {typeof turn.costUsd === 'number' && turn.costUsd > 0 && (
              <> · ${turn.costUsd.toFixed(4)}</>
            )}
          </span>
        )}
        <span className="ml-auto text-[10px] text-muted-foreground">
          {new Date(turn.createdAt).toLocaleString('ko-KR')}
        </span>
      </div>
      <div
        className={
          isUser
            ? 'rounded-md bg-primary/10 p-3 text-sm whitespace-pre-wrap'
            : isTool
              ? 'rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap'
              : 'rounded-md border p-3 text-sm whitespace-pre-wrap'
        }
      >
        {turn.content || (isTool ? '(tool result)' : '(no content)')}
      </div>
      {turn.toolCalls && turn.toolCalls.length > 0 && (
        <div className="ml-3 flex flex-col gap-1 border-l-2 border-muted pl-3">
          {turn.toolCalls.map((tc) => (
            <div key={tc.id} className="text-xs">
              <span className="font-mono font-medium">→ {tc.toolName}</span>
              <span className="ml-2 text-muted-foreground">
                ({tc.durationMs}ms)
              </span>
              {tc.error && (
                <span className="ml-2 text-destructive">err: {tc.error}</span>
              )}
            </div>
          ))}
        </div>
      )}
    </li>
  )
}

// roleVariant는 advisor turn의 role을 shadcn Badge variant로 매핑합니다.
// 단위 테스트(advisor.test.tsx) 대상으로 export.
export function roleVariant(
  role: string,
): 'default' | 'destructive' | 'secondary' | 'outline' {
  switch (role) {
    case 'user':
      return 'default'
    case 'assistant':
      return 'secondary'
    case 'tool':
      return 'outline'
    default:
      return 'outline'
  }
}

// resolveAskErrorMessage는 useAskAdvisor의 에러를 사용자 가시 메시지로 매핑합니다.
//   ApiError 503  → 옵트인 활성화 안내 (LLM disabled)
//   ApiError 그 외 → 서버 메시지 노출
//   비-ApiError    → 일반 fallback
// 단위 테스트(advisor.test.tsx) 대상으로 export.
export function resolveAskErrorMessage(err: unknown): string {
  if (err instanceof ApiError && err.status === 503) {
    return 'Advisor가 비활성 상태입니다. 서버를 --llm-provider=ollama 또는 =anthropic으로 재시작하세요.'
  }
  if (err instanceof ApiError) {
    return err.message
  }
  return '질문 처리 중 오류가 발생했습니다'
}

export const Route = createFileRoute('/_authenticated/advisor')({
  component: AdvisorPage,
})
