// PWA manifest 회귀 테스트입니다 (Phase 5 PWA Stage 2, design doc §7).
//
// Stage 1에서는 `web/public/manifest.webmanifest` 정적 파일을 직접 검증했으나
// Stage 2에서 vite-plugin-pwa가 vite.config.ts의 plugin 인라인 정의로
// manifest를 자동 생성 → dist/manifest.webmanifest를 새로 만듭니다(D-PWA-2).
//
// 본 테스트는 build 산출물(dist/manifest.webmanifest)이 design doc §6.2 인라인
// 정의와 일치하는지 검증합니다. dist 미생성 시 skip — `pnpm build` 후 실행 대상.

import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

// build outDir은 vite.config.ts에서 ../internal/web/dist로 지정. vitest의 cwd는
// web/이므로 그 기준 상대 경로로 접근합니다.
const MANIFEST_PATH = resolve(process.cwd(), '..', 'internal', 'web', 'dist', 'manifest.webmanifest')

describe('dist/manifest.webmanifest (vite-plugin-pwa 생성)', () => {
  if (!existsSync(MANIFEST_PATH)) {
    it.skip('dist 미생성 — `pnpm build` 선행 필요', () => {})
    return
  }
  const raw = readFileSync(MANIFEST_PATH, 'utf-8')

  it('유효 JSON 입니다', () => {
    expect(() => JSON.parse(raw)).not.toThrow()
  })

  describe('필수 키', () => {
    const manifest = JSON.parse(raw) as Record<string, unknown>

    it('name + short_name 갖춥니다', () => {
      expect(manifest.name).toBe('Lodestar 관리자 콘솔')
      expect(manifest.short_name).toBe('Lodestar')
    })

    it('description 갖춥니다', () => {
      expect(typeof manifest.description).toBe('string')
      expect((manifest.description as string).length).toBeGreaterThan(0)
    })

    it('start_url + display + scope 표준 값입니다', () => {
      expect(manifest.start_url).toBe('/')
      expect(manifest.display).toBe('standalone')
      expect(manifest.scope).toBe('/')
    })

    it('background_color + theme_color hex 입니다', () => {
      expect(manifest.background_color).toMatch(/^#[0-9a-fA-F]{6}$/)
      expect(manifest.theme_color).toMatch(/^#[0-9a-fA-F]{6}$/)
    })

    it('theme_color는 design doc §6.2 기준 #0a0a0a 입니다', () => {
      expect(manifest.theme_color).toBe('#0a0a0a')
    })
  })

  describe('icons 배열', () => {
    const manifest = JSON.parse(raw) as { icons: Array<Record<string, unknown>> }

    it('비어있지 않습니다', () => {
      expect(Array.isArray(manifest.icons)).toBe(true)
      expect(manifest.icons.length).toBeGreaterThan(0)
    })

    it('192x192 + 512x512 모두 포함합니다 (Lighthouse Installable 기준)', () => {
      const sizes = manifest.icons.map((icon) => icon.sizes as string)
      expect(sizes).toContain('192x192')
      expect(sizes).toContain('512x512')
    })

    it('각 아이콘은 src + sizes + type 갖춥니다', () => {
      for (const icon of manifest.icons) {
        expect(icon.src).toMatch(/^\/[\w-]+\.(png|svg)$/)
        expect(icon.sizes).toMatch(/^\d+x\d+$/)
        expect(icon.type).toMatch(/^image\//)
      }
    })

    it('maskable purpose 아이콘 1개 이상 (홈 화면 마스킹 호환)', () => {
      const maskable = manifest.icons.filter((icon) => {
        const purpose = (icon.purpose as string | undefined) ?? ''
        return purpose.split(/\s+/).includes('maskable')
      })
      expect(maskable.length).toBeGreaterThanOrEqual(1)
    })
  })
})
