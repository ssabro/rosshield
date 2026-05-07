import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { Server } from 'lucide-react'

import { ApiError } from '@/api/errors'
import { useRobots } from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type { Robot } from '@/api/hooks'

// `/robots` — 로봇 목록 + fleet 필터.
// - 빈 결과: "(로봇 없음)"
// - 로딩: "불러오는 중…"
// - 에러: ApiError 메시지 표시
// 컬럼: 이름·호스트:포트·인증·심각도·태그
function RobotsPage(): React.ReactElement {
  const [fleetId, setFleetId] = useState('')
  const trimmed = fleetId.trim()
  const robots = useRobots(trimmed.length > 0 ? trimmed : undefined)
  const t = useT()

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.robots.title')}
        description={t('pages.robots.description')}
      />

      <div className="flex max-w-sm flex-col gap-2">
        <Label htmlFor="fleet-filter">Fleet ID 필터</Label>
        <Input
          id="fleet-filter"
          placeholder="예: production"
          value={fleetId}
          onChange={(e) => setFleetId(e.target.value)}
        />
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>이름</TableHead>
              <TableHead>호스트:포트</TableHead>
              <TableHead>인증</TableHead>
              <TableHead>심각도</TableHead>
              <TableHead>태그</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {robots.isPending && (
              <TableRow>
                <TableCell
                  colSpan={5}
                  className="text-center text-muted-foreground"
                >
                  불러오는 중…
                </TableCell>
              </TableRow>
            )}
            {robots.isError && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-destructive">
                  {robots.error instanceof ApiError
                    ? robots.error.message
                    : '로봇 목록을 불러올 수 없습니다'}
                </TableCell>
              </TableRow>
            )}
            {robots.isSuccess && robots.data.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="p-0">
                  <EmptyState
                    icon={Server}
                    title="등록된 로봇이 없습니다"
                    description="rosshield-server seed demo로 시연 데이터를 시드하거나, robot.Service.CreateRobot으로 추가하세요."
                    className="rounded-none border-0 bg-transparent"
                  />
                </TableCell>
              </TableRow>
            )}
            {robots.isSuccess &&
              robots.data.map((robot) => (
                <RobotRow key={robot.id} robot={robot} />
              ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function RobotRow({ robot }: { robot: Robot }): React.ReactElement {
  const tags = Array.isArray(robot.tags) ? (robot.tags as unknown[]) : []
  return (
    <TableRow>
      <TableCell className="font-medium">{robot.name}</TableCell>
      <TableCell className="font-mono text-xs">
        {robot.host}:{robot.port}
      </TableCell>
      <TableCell>{robot.authType}</TableCell>
      <TableCell>
        <Badge variant="secondary">{robot.criticality}</Badge>
      </TableCell>
      <TableCell>
        <div className="flex flex-wrap gap-1">
          {tags.length === 0 ? (
            <span className="text-xs text-muted-foreground">-</span>
          ) : (
            tags.map((tag, i) => (
              <Badge key={i} variant="outline">
                {String(tag)}
              </Badge>
            ))
          )}
        </div>
      </TableCell>
    </TableRow>
  )
}

export const Route = createFileRoute('/_authenticated/robots')({
  component: RobotsPage,
})
