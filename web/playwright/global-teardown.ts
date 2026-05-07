import { rmSync } from 'node:fs'

// globalTeardown — server 프로세스 종료 + dataDir 정리.
//
// SIGTERM으로 graceful shutdown 신호 → 5초 안에 안 끝나면 SIGKILL.
// dataDir은 retention 디버깅 시 PLAYWRIGHT_E2E_KEEP_DATA=1로 보존 가능.

function log(msg: string): void {
  // eslint-disable-next-line no-console
  console.log(`[playwright/global-teardown] ${msg}`)
}

function killGracefully(signal: NodeJS.Signals = 'SIGTERM', timeoutMs = 5_000): Promise<void> {
  return new Promise((resolve) => {
    const state = globalThis.__ROSSHIELD_E2E__
    if (!state?.server) {
      resolve()
      return
    }
    const child = state.server
    let settled = false
    const onExit = (): void => {
      if (settled) return
      settled = true
      log('server exited')
      resolve()
    }
    child.once('exit', onExit)
    try {
      child.kill(signal)
    } catch (e) {
      log(`kill signal failed: ${String(e)}`)
    }
    setTimeout(() => {
      if (settled) return
      log(`timeout — sending SIGKILL`)
      try {
        child.kill('SIGKILL')
      } catch (e) {
        log(`SIGKILL failed: ${String(e)}`)
      }
      // resolve regardless of confirmation
      setTimeout(onExit, 500)
    }, timeoutMs)
  })
}

export default async function globalTeardown(): Promise<void> {
  await killGracefully()

  const state = globalThis.__ROSSHIELD_E2E__
  if (state?.dataDir) {
    if (process.env.PLAYWRIGHT_E2E_KEEP_DATA === '1') {
      log(`preserving dataDir for inspection: ${state.dataDir}`)
    } else {
      try {
        rmSync(state.dataDir, { recursive: true, force: true })
        log(`removed dataDir: ${state.dataDir}`)
      } catch (e) {
        log(`dataDir cleanup failed (non-fatal): ${String(e)}`)
      }
    }
  }
  globalThis.__ROSSHIELD_E2E__ = undefined
}
