// PWA Stage 2 — service worker 빌드 산출물 회귀 테스트.
//
// vite-plugin-pwa generateSW 모드(D-PWA-1)는 build 시 dist 루트에:
//   - sw.js          (서비스 워커 본체, Workbox 7 precaching + navigation fallback)
//   - workbox-*.js   (Workbox 런타임 청크 — 자산 hash 부착)
//   - manifest.webmanifest
// 를 자동 생성합니다. 본 테스트는 dist 생성 후 위 산출물 존재 + 비어있지 않음을
// 검증해 plugin 구성 회귀(예: registerType 변경, devOptions 토글 실수, plugin
// 누락 등)를 즉시 알람합니다.
//
// dist 미생성 시 skip — `pnpm build` 후 실행 대상.

import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

const DIST_DIR = resolve(process.cwd(), '..', 'internal', 'web', 'dist')

describe('dist/sw.js + workbox-*.js (vite-plugin-pwa generateSW 산출물)', () => {
  if (!existsSync(DIST_DIR)) {
    it.skip('dist 미생성 — `pnpm build` 선행 필요', () => {})
    return
  }

  it('sw.js 가 생성되고 비어있지 않습니다', () => {
    const swPath = resolve(DIST_DIR, 'sw.js')
    expect(existsSync(swPath), `sw.js missing at ${swPath}`).toBe(true)
    const size = statSync(swPath).size
    expect(size).toBeGreaterThan(0)
  })

  it('workbox-*.js (Workbox 런타임 청크) 가 1개 이상 생성됩니다', () => {
    const entries = readdirSync(DIST_DIR)
    const workboxChunks = entries.filter((name) => /^workbox-[\w-]+\.js$/.test(name))
    expect(workboxChunks.length, `workbox-*.js missing in dist (entries=${entries.join(',')})`).toBeGreaterThanOrEqual(1)
    for (const chunk of workboxChunks) {
      const size = statSync(resolve(DIST_DIR, chunk)).size
      expect(size, `${chunk} empty`).toBeGreaterThan(0)
    }
  })

  it('sw.js 는 Workbox precache + navigation fallback 패턴 포함합니다', () => {
    const sw = readFileSync(resolve(DIST_DIR, 'sw.js'), 'utf-8')
    // vite-plugin-pwa 1.0+ 의 generateSW는 AMD `define([...])` 패턴으로
    // workbox 청크를 import + precacheAndRoute + NavigationRoute(denylist:/api)
    // 호출. (구버전 importScripts 패턴은 1.0부터 AMD로 전환됨.)
    expect(sw).toMatch(/define\(\[.*workbox-/)
    expect(sw).toMatch(/precacheAndRoute/)
    expect(sw).toMatch(/NavigationRoute/)
    // /api/* 우회 정책(D-PWA-7) 회귀 차단.
    expect(sw).toMatch(/\\\/api\\\//)
  })

  it('manifest.webmanifest 가 dist 루트에 생성됩니다', () => {
    const manifestPath = resolve(DIST_DIR, 'manifest.webmanifest')
    expect(existsSync(manifestPath)).toBe(true)
  })
})
