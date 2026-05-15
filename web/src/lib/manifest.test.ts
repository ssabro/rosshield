// PWA manifest 검증 테스트입니다 (Phase 5 PWA Stage 1, design doc §7).
//
// `web/public/manifest.webmanifest` 정적 JSON이 W3C App Manifest 표준 필수
// 키를 갖추는지 회귀 차단합니다. Stage 2에서 vite-plugin-pwa가 plugin 인라인
// 정의로 덮어쓸 예정이나, 그 이전까지 본 테스트가 staticfile 무결성을
// 보장합니다.

import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

// __dirname은 ESM 환경에선 부재 — vitest는 src 기준 cwd를 보장하지 않으므로
// process.cwd() (web/) 기준 상대 경로로 접근합니다.
const MANIFEST_PATH = resolve(process.cwd(), 'public', 'manifest.webmanifest')

describe('manifest.webmanifest', () => {
  const raw = readFileSync(MANIFEST_PATH, 'utf-8')

  it('유효 JSON 입니다', () => {
    expect(() => JSON.parse(raw)).not.toThrow()
  })

  describe('필수 키', () => {
    const manifest = JSON.parse(raw) as Record<string, unknown>

    it('name + short_name 갖춥니다', () => {
      expect(manifest.name).toBe('rosshield Console')
      expect(manifest.short_name).toBe('rosshield')
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
