import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { MessageSquare, Sparkles } from 'lucide-react'

import { ApiError } from '@/api/errors'
import { PageHeader } from '@/components/layout/PageHeader'
import { EmptyState } from '@/components/layout/EmptyState'
import { useT } from '@/i18n/t'
import { toast } from '@/lib/toast'
import {
  useAdvisorConversation,
  useAdvisorConversations,
  useAskAdvisor,
} from '@/api/hooks'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { TextSkeleton } from '@/components/ui/skeleton'

import type { AdvisorConversation, AdvisorTurn } from '@/api/hooks'
import type { DictKey } from '@/i18n/dict'

const EXAMPLE_PROMPT_KEYS: ReadonlyArray<DictKey> = [
  'advisor.example.recent_fail',
  'advisor.example.critical_robots',
  'advisor.example.score_change',
]

// `/advisor` — LLM 옵트인 대화 (E19-3-B).
// - 좌: 대화 목록 (클릭 → 선택)
// - 우: 선택 대화의 turn 렌더링 + 새 질문 입력 (Ask)
// - 503 (LLM disabled) 응답 시 안내 EmptyState — 옵트인 활성화 방법 안내
//
// D-UI-1 Stage 4 — PageHeader badge로 "opt-in" 명시, EmptyState로 disabled UX
// 일관화, mutation feedback에 toast 추가.
// a11y-drilldown.test.tsx mount용 named export.
export function AdvisorPage(): React.ReactElement {
  const conversations = useAdvisorConversations()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const t = useT()

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.advisor.title')}
        description={t('advisor.subtitle.summary')}
        badge={
          <Badge
            variant="outline"
            className="flex items-center gap-1 text-[10px] tracking-wide"
          >
            <Sparkles className="size-3" aria-hidden />
            {t('advisor.badge.optIn')}
          </Badge>
        }
      />

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
  const t = useT()
  return (
    <aside className="flex flex-col gap-2 rounded-md border p-3">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium">{t('advisor.conversations.title')}</h2>
        <Button size="sm" variant="outline" onClick={onNew}>
          {t('advisor.conversations.new')}
        </Button>
      </div>

      {isPending && (
        <div className="space-y-2 py-1" aria-label={t('advisor.conversations.loading')}>
          <TextSkeleton className="h-3 w-3/4" />
          <TextSkeleton className="h-3 w-1/2" />
          <TextSkeleton className="h-3 w-2/3" />
        </div>
      )}
      {isError && (
        <p className="text-xs text-destructive">
          {error instanceof ApiError
            ? error.message
            : t('advisor.conversations.list.error')}
        </p>
      )}
      {!isPending && !isError && conversations.length === 0 && (
        <p className="rounded-md border border-dashed border-border bg-muted/30 px-3 py-4 text-center text-xs text-muted-foreground">
          {t('advisor.conversations.empty')}
          <br />
          {t('advisor.conversations.empty.cta')}
        </p>
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
                {c.title || t('advisor.conversations.untitled')}
              </div>
              <div className="text-[10px] text-muted-foreground">
                {new Date(c.updatedAt).toLocaleString()}
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
  const t = useT()

  const turns: AdvisorTurn[] = detail.data?.turns ?? []

  // ask 에러가 503 옵트인 안내인지 판단 — EmptyState로 별도 노출.
  const isOptInDisabled = ask.error instanceof ApiError && ask.error.status === 503

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
          toast.success(t('advisor.ask.success'))
        },
        onError: (err) => {
          // 503은 EmptyState로 표시하므로 toast 중복 회피.
          if (err instanceof ApiError && err.status === 503) return
          toast.error(t('advisor.ask.error.toast'), {
            description: resolveAskErrorMessage(err, t),
          })
        },
      },
    )
  }

  const showEmpty =
    !conversationId && turns.length === 0 && !ask.isPending && !detail.isPending

  return (
    <section className="flex min-h-[28rem] flex-col gap-3 rounded-md border bg-card p-4">
      {detail.isPending && conversationId && (
        <div className="space-y-2" aria-label={t('advisor.panel.detail.loading')}>
          <TextSkeleton className="h-4 w-1/3" />
          <TextSkeleton className="h-4 w-2/3" />
        </div>
      )}
      {detail.isError && (
        <p className="text-sm text-destructive">
          {detail.error instanceof ApiError
            ? detail.error.message
            : t('advisor.panel.detail.error')}
        </p>
      )}

      <ol className="flex flex-1 flex-col gap-4 overflow-y-auto">
        {showEmpty && (
          <li className="m-auto flex flex-col items-center gap-3 text-center">
            <div className="rounded-full bg-muted p-3 text-muted-foreground">
              <MessageSquare className="h-6 w-6" aria-hidden />
            </div>
            <div className="space-y-1">
              <p className="text-sm font-medium">{t('advisor.panel.empty.title')}</p>
              <p className="text-xs text-muted-foreground">
                {t('advisor.panel.empty.description')}
              </p>
            </div>
            <div className="flex flex-wrap justify-center gap-1.5 pt-1">
              {EXAMPLE_PROMPT_KEYS.map((k) => {
                const text = t(k)
                return (
                  <button
                    key={k}
                    type="button"
                    onClick={() => setQuestion(text)}
                    className="rounded-full border border-dashed border-border bg-muted/30 px-3 py-1 text-xs text-muted-foreground hover:bg-muted"
                  >
                    {text}
                  </button>
                )
              })}
            </div>
          </li>
        )}
        {turns.map((tn) => (
          <TurnView key={tn.id} turn={tn} />
        ))}
      </ol>

      {isOptInDisabled && (
        <EmptyState
          variant="no-permission"
          icon={Sparkles}
          size="sm"
          title={t('advisor.disabled.empty.title')}
          description={t('advisor.disabled.empty.description')}
        />
      )}

      {ask.isError && !isOptInDisabled && (
        <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
          {resolveAskErrorMessage(ask.error, t)}
        </div>
      )}

      <form onSubmit={onSubmit} className="flex flex-col gap-2 border-t pt-3">
        <Label htmlFor="ask-input" className="text-xs">
          {t('advisor.input.label')}
        </Label>
        <Textarea
          id="ask-input"
          placeholder={t('advisor.input.placeholder')}
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          rows={3}
          disabled={ask.isPending}
        />
        <div className="flex justify-end">
          <Button type="submit" disabled={!question.trim() || ask.isPending}>
            {ask.isPending
              ? t('advisor.input.submitting')
              : t('advisor.input.submit')}
          </Button>
        </div>
      </form>
    </section>
  )
}

function TurnView({ turn }: { turn: AdvisorTurn }): React.ReactElement {
  const isUser = turn.role === 'user'
  const isTool = turn.role === 'tool'
  const t = useT()

  // chat-like 정렬: user는 우측, assistant/tool은 좌측.
  const align = isUser ? 'items-end' : 'items-start'
  const bubble = isUser
    ? 'rounded-2xl rounded-tr-sm bg-primary px-4 py-2.5 text-sm text-primary-foreground whitespace-pre-wrap'
    : isTool
      ? 'rounded-2xl rounded-tl-sm bg-muted px-3 py-2 font-mono text-xs text-muted-foreground whitespace-pre-wrap'
      : 'rounded-2xl rounded-tl-sm border bg-card px-4 py-2.5 text-sm whitespace-pre-wrap'

  return (
    <li className={`flex flex-col gap-1 ${align}`}>
      <div className="flex max-w-[85%] items-center gap-2 px-1 text-[10px] text-muted-foreground">
        <Badge variant={roleVariant(turn.role)} className="text-[10px]">
          {turn.role}
        </Badge>
        {turn.llmProvider && (
          <span className="font-mono">
            {turn.llmProvider}/{turn.llmModel}
            {typeof turn.inputTokens === 'number' &&
              typeof turn.outputTokens === 'number' && (
                <> · {turn.inputTokens}+{turn.outputTokens} tok</>
              )}
            {typeof turn.costUsd === 'number' && turn.costUsd > 0 && (
              <> · ${turn.costUsd.toFixed(4)}</>
            )}
          </span>
        )}
        <span className="font-mono">
          {new Date(turn.createdAt).toLocaleString()}
        </span>
      </div>
      <div className={`max-w-[85%] ${bubble}`}>
        {turn.content ||
          (isTool ? t('advisor.turn.tool_result') : t('advisor.turn.no_content'))}
      </div>
      {turn.toolCalls && turn.toolCalls.length > 0 && (
        <div className="flex max-w-[85%] flex-col gap-0.5 rounded-md border border-dashed border-border bg-muted/30 px-2 py-1.5">
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
//   ApiError 503  → 옵트인 활성화 안내 (LLM disabled, dict key)
//   ApiError 그 외 → 서버 메시지 노출
//   비-ApiError    → 일반 fallback (dict key)
// 단위 테스트(advisor.test.tsx) 대상으로 export — t는 dict 키를 문자열로 푸는 함수.
export function resolveAskErrorMessage(
  err: unknown,
  t: (key: DictKey) => string,
): string {
  if (err instanceof ApiError && err.status === 503) {
    return t('advisor.error.disabled')
  }
  if (err instanceof ApiError) {
    return err.message
  }
  return t('advisor.error.fallback')
}

export const Route = createFileRoute('/_authenticated/advisor')({
  component: AdvisorPage,
})
