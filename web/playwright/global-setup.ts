import { spawn, spawnSync, type ChildProcess } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync, existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { E2E_ADMIN } from './fixtures'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

// Playwright globalSetup — Go 서버 빌드 + admin/demo 시드 + 백그라운드 부팅.
//
// 단계:
//  1. tmpdir에 격리 dataDir 생성 (sqlite + keys 모두 여기로).
//  2. `go build` rosshield-server → bin/rosshield-server-e2e.
//  3. `seed admin` + `seed demo` 순차 실행 (멱등이지만 첫 호출 보장).
//  4. addr=127.0.0.1:E2E_BACKEND_PORT로 server 백그라운드 spawn.
//  5. /healthz 폴링 → 30초 안에 ok 못 받으면 throw.
//  6. globalThis로 ChildProcess + dataDir 핸들을 globalTeardown에 전달.
//
// CI 비-Linux 환경에서 -race는 cgo 의존이라 build 시 -tags 없이 진행 (기본).

const ROOT = path.resolve(__dirname, '..', '..')
const SERVER_PKG = './cmd/rosshield-server'
const PORT = Number(process.env.E2E_BACKEND_PORT ?? '8123')
const HEALTH_TIMEOUT_MS = 30_000
const POLL_INTERVAL_MS = 500

interface E2EState {
  server?: ChildProcess
  dataDir?: string
  binPath?: string
}

declare global {
  // eslint-disable-next-line no-var
  var __ROSSHIELD_E2E__: E2EState | undefined
}

function log(msg: string): void {
  // eslint-disable-next-line no-console
  console.log(`[playwright/global-setup] ${msg}`)
}

function binName(): string {
  return process.platform === 'win32' ? 'rosshield-server-e2e.exe' : 'rosshield-server-e2e'
}

function runStep(label: string, cmd: string, args: string[], opts: { cwd?: string; env?: NodeJS.ProcessEnv; input?: string } = {}): void {
  log(`${label}: ${cmd} ${args.join(' ')}`)
  const r = spawnSync(cmd, args, {
    cwd: opts.cwd ?? ROOT,
    env: { ...process.env, ...(opts.env ?? {}) },
    encoding: 'utf-8',
    stdio: opts.input != null ? ['pipe', 'pipe', 'pipe'] : 'inherit',
    input: opts.input,
  })
  if (r.status !== 0) {
    if (r.stdout) log(`stdout: ${r.stdout}`)
    if (r.stderr) log(`stderr: ${r.stderr}`)
    throw new Error(`${label} failed (exit=${r.status}): ${cmd} ${args.join(' ')}`)
  }
}

async function waitForHealth(url: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs
  let lastErr: unknown
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url)
      if (res.ok) {
        log(`health OK after ${Date.now() - (deadline - timeoutMs)}ms`)
        return
      }
      lastErr = new Error(`HTTP ${res.status}`)
    } catch (e) {
      lastErr = e
    }
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS))
  }
  throw new Error(`server did not become healthy at ${url} within ${timeoutMs}ms (last: ${String(lastErr)})`)
}

export default async function globalSetup(): Promise<void> {
  // 0) Web 빌드 결과 확인 — 없으면 즉시 안내 throw (시간 보호).
  //    Web 콘솔을 서빙하려면 internal/web/dist/index.html이 있어야 한다.
  const webDist = path.join(ROOT, 'internal', 'web', 'dist', 'index.html')
  if (!existsSync(webDist)) {
    throw new Error(
      `Web build artifact missing: ${webDist}\n` +
        `Run \`pnpm --dir web build\` (or \`make web-build\`) before \`pnpm exec playwright test\`.`,
    )
  }

  // 1) 격리 dataDir 준비.
  const dataDir = mkdtempSync(path.join(tmpdir(), 'rosshield-e2e-'))
  log(`dataDir = ${dataDir}`)

  // 2) bin 디렉터리 + go build.
  const binDir = path.join(ROOT, 'bin')
  const binPath = path.join(binDir, binName())
  if (!existsSync(binDir)) {
    runStep('mkdir bin', process.platform === 'win32' ? 'cmd' : 'mkdir', process.platform === 'win32' ? ['/c', 'mkdir', binDir] : ['-p', binDir])
  }
  runStep('go build', 'go', ['build', '-o', binPath, SERVER_PKG])

  // 3) seed admin + seed demo (Web 빌드 부재로 / 503이 나도 /api는 정상).
  //    --password-stdin으로 명령행 노출 방지.
  runStep(
    'seed admin',
    binPath,
    ['seed', 'admin', '--email', E2E_ADMIN.email, '--password-stdin', '--data-dir', dataDir],
    { input: E2E_ADMIN.password + '\n' },
  )
  runStep('seed demo', binPath, ['seed', 'demo', '--email', E2E_ADMIN.email, '--data-dir', dataDir])

  // 4) 백그라운드 부팅.
  const addr = `127.0.0.1:${PORT}`
  log(`spawn server addr=${addr}`)
  const server = spawn(binPath, ['-addr', addr, '-data-dir', dataDir], {
    cwd: ROOT,
    stdio: ['ignore', 'inherit', 'inherit'],
    env: { ...process.env },
  })
  server.on('exit', (code, signal) => {
    log(`server exited code=${code} signal=${signal}`)
  })

  // 5) 헬스체크.
  try {
    await waitForHealth(`http://${addr}/healthz`, HEALTH_TIMEOUT_MS)
  } catch (e) {
    server.kill('SIGTERM')
    rmSync(dataDir, { recursive: true, force: true })
    throw e
  }

  // 6) teardown용 핸들 보관.
  globalThis.__ROSSHIELD_E2E__ = { server, dataDir, binPath }

  // playwright config가 BASE_URL을 환경변수로 읽으므로 후속 worker에 전달.
  process.env.E2E_BACKEND_URL = `http://${addr}`
  // tests can read this if needed.
  writeFileSync(path.join(dataDir, '.e2e-base-url'), `http://${addr}`)
}
