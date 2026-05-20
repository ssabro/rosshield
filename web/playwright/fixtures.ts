// Test fixtures for Playwright E2E (C4 scaffold).
//
// globalSetup이 admin user를 시드하므로, 모든 spec은 동일 자격증명을 공유한다.
// 비밀번호는 ≥12 chars 강제 (tenant.Service 도메인 검증).
//
// 페이지별 helper는 spec 파일에 인라인하지 않고 여기서 한 번만 정의 — 변경 일관성.

export const E2E_ADMIN = {
  email: 'e2e-admin@example.com',
  password: 'rosshield-e2e-pw1',
} as const

// Web Console 페이지의 한국어 라벨 — i18n 기본값(navigator.language fallback).
// dict.ts와 동기 갱신 필요.
export const KO_LABELS = {
  login: {
    title: 'Lodestar 관리자 콘솔',
    email: '이메일',
    password: '패스워드',
    submit: '로그인',
  },
  header: {
    logout: '로그아웃',
    userMenu: '사용자 메뉴', // D-P7-1: UserMenu DropdownMenuTrigger aria-label
    userProfile: '설정', // D-P7-1: dropdown menuitem '/settings'
  },
  nav: {
    overview: '개요',
    robots: '로봇',
    audit: '감사',
    compliance: 'Compliance',
  },
  compliance: {
    frameworkLabel: '프레임워크',
    frameworkVersionLabel: '프레임워크 버전',
    profileAdd: '프로필 추가',
  },
  robots: {
    heading: '로봇',
    createButton: '로봇 등록',
    createDialogHeading: '새 로봇 등록',
  },
} as const

export const EN_LABELS = {
  header: {
    logout: 'Sign out',
    userMenu: 'User menu',
  },
  nav: {
    overview: 'Overview',
  },
} as const
