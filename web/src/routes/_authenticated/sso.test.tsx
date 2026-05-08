// B4 — `/sso` 페이지 helper 단위 테스트.
//
// 페이지 마운트 자체는 TanStack Router 의존이라 회피 — 다른 페이지(integrations,
// advisor, compliance)와 동일한 패턴으로 helper export 함수만 검증.

import { describe, expect, it } from 'vitest'

import {
  displayProviderName,
  isValidUrl,
  parseScopes,
  providerTypeBadgeVariant,
  validateOIDCConfig,
  validateSAMLConfig,
} from './sso'

import type { SSOProvider } from '@/api/hooks'

describe('displayProviderName', () => {
  const base: SSOProvider = {
    id: 'ssop_01',
    type: 'oidc',
    name: 'Google',
    enabled: true,
    config: {},
    createdAt: '',
    updatedAt: '',
  }
  it('name이 있으면 그대로 사용', () => {
    expect(displayProviderName(base)).toBe('Google')
  })
  it('name이 공백뿐이면 id fallback', () => {
    expect(displayProviderName({ ...base, name: '   ' })).toBe('ssop_01')
  })
  it('name이 빈 문자열이면 id fallback', () => {
    expect(displayProviderName({ ...base, name: '' })).toBe('ssop_01')
  })
})

describe('providerTypeBadgeVariant', () => {
  it('oidc → default', () => {
    expect(providerTypeBadgeVariant('oidc')).toBe('default')
  })
  it('saml → secondary', () => {
    expect(providerTypeBadgeVariant('saml')).toBe('secondary')
  })
})

describe('isValidUrl', () => {
  it('http(s) URL 허용', () => {
    expect(isValidUrl('https://example.com')).toBe(true)
    expect(isValidUrl('http://idp.example.com/path')).toBe(true)
  })
  it('빈 문자열·잘못된 형식은 false', () => {
    expect(isValidUrl('')).toBe(false)
    expect(isValidUrl('not-a-url')).toBe(false)
  })
})

describe('parseScopes', () => {
  it('쉼표·공백 혼합을 분리 + 정렬', () => {
    // 정렬은 알파벳 오름차순.
    expect(parseScopes('openid, email, profile')).toEqual([
      'email',
      'openid',
      'profile',
    ])
    expect(parseScopes('openid email profile')).toEqual([
      'email',
      'openid',
      'profile',
    ])
  })
  it('빈 입력은 빈 배열', () => {
    expect(parseScopes('')).toEqual([])
    expect(parseScopes('   ')).toEqual([])
  })
  it('중복 제거 + 정렬', () => {
    expect(parseScopes('email, openid, openid, email')).toEqual([
      'email',
      'openid',
    ])
  })
})

describe('validateOIDCConfig', () => {
  it('필수 필드 모두 채워지면 ok', () => {
    const errs = validateOIDCConfig({
      issuer: 'https://accounts.google.com',
      clientId: 'cid',
      redirectUri: 'https://app.example.com/cb',
      scopes: ['openid', 'email'],
    })
    expect(errs).toEqual([])
  })
  it('issuer 비어있으면 issuer 에러', () => {
    const errs = validateOIDCConfig({
      issuer: '',
      clientId: 'cid',
      redirectUri: 'https://app.example.com/cb',
      scopes: ['openid'],
    })
    expect(errs).toContain('issuer')
  })
  it('clientId·redirectUri 비어있어도 각 키 에러', () => {
    const errs = validateOIDCConfig({
      issuer: 'https://accounts.google.com',
      clientId: '',
      redirectUri: '',
      scopes: ['openid'],
    })
    expect(errs).toContain('clientId')
    expect(errs).toContain('redirectUri')
  })
  it('issuer·redirectUri가 URL이 아니면 에러', () => {
    const errs = validateOIDCConfig({
      issuer: 'not-url',
      clientId: 'cid',
      redirectUri: 'also-bad',
      scopes: ['openid'],
    })
    expect(errs).toContain('issuer')
    expect(errs).toContain('redirectUri')
  })
  it('scopes 비어있으면 scopes 에러', () => {
    const errs = validateOIDCConfig({
      issuer: 'https://idp',
      clientId: 'cid',
      redirectUri: 'https://app/cb',
      scopes: [],
    })
    expect(errs).toContain('scopes')
  })
})

describe('validateSAMLConfig', () => {
  it('metadataUrl 또는 metadataXml 중 하나 + acsUrl 있으면 ok', () => {
    expect(
      validateSAMLConfig({
        metadataUrl: 'https://idp.example/metadata',
        metadataXml: '',
        acsUrl: 'https://app.example/acs',
      }),
    ).toEqual([])
    expect(
      validateSAMLConfig({
        metadataUrl: '',
        metadataXml: '<EntityDescriptor>...</EntityDescriptor>',
        acsUrl: 'https://app.example/acs',
      }),
    ).toEqual([])
  })
  it('둘 다 비어있으면 metadata 에러', () => {
    const errs = validateSAMLConfig({
      metadataUrl: '',
      metadataXml: '',
      acsUrl: 'https://app.example/acs',
    })
    expect(errs).toContain('metadata')
  })
  it('acsUrl 없으면 acsUrl 에러', () => {
    const errs = validateSAMLConfig({
      metadataUrl: 'https://idp.example/metadata',
      metadataXml: '',
      acsUrl: '',
    })
    expect(errs).toContain('acsUrl')
  })
  it('metadataUrl이 잘못된 URL이면 metadata 에러', () => {
    const errs = validateSAMLConfig({
      metadataUrl: 'not-a-url',
      metadataXml: '',
      acsUrl: 'https://app.example/acs',
    })
    expect(errs).toContain('metadata')
  })
  it('acsUrl이 잘못된 URL이면 acsUrl 에러', () => {
    const errs = validateSAMLConfig({
      metadataUrl: 'https://idp.example/metadata',
      metadataXml: '',
      acsUrl: 'not-a-url',
    })
    expect(errs).toContain('acsUrl')
  })
})
