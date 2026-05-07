// rosshield Web Console i18n 사전.
//
// C5 i18n 회수 — 가벼운 자체 구현 (i18next/react-intl 없이).
// dict 키는 namespace.subkey 점 표기, 값은 string. interpolation은 현재 미지원
// (필요 시 t(key, vars) 시그니처 확장).
//
// 한글이 1차 언어, 영어는 fallback 겸 보조. 두 dict는 같은 키를 공유해야 한다 —
// 누락 키는 t()가 missing 표시(`[missing key]`)로 노출하므로 추가 시 양쪽 동기 갱신.

export const ko = {
  'app.brand': 'rosshield',
  'app.brand.subtitle': 'Security Console',
  'app.version': 'v0.1.0 · Phase 2',

  'nav.robots': '로봇',
  'nav.scans': '스캔',
  'nav.findings': 'Findings',
  'nav.compliance': 'Compliance',
  'nav.advisor': 'Advisor',
  'nav.reports': '리포트',

  'header.theme.light': '라이트',
  'header.theme.dark': '다크',
  'header.theme.system': '시스템',
  'header.theme.tooltip': '테마: {label} (클릭으로 전환)',
  'header.theme.aria': '테마 ({label})',
  'header.locale.tooltip': '언어: {label} (클릭으로 전환)',
  'header.locale.aria': '언어 ({label})',
  'header.logout': '로그아웃',
  'header.user.aria': '현재 사용자',

  'login.title': 'rosshield Console',
  'login.description': '로봇 플릿 보안 감사 콘솔에 로그인합니다.',
  'login.email': '이메일',
  'login.password': '패스워드',
  'login.submit': '로그인',
  'login.submitting': '로그인 중…',
  'login.error.invalid': '이메일 또는 패스워드가 올바르지 않습니다',
  'login.footer': 'rosshield · v0.1.0 · Phase 2',

  'pages.robots.title': '로봇',
  'pages.robots.description': '테넌트에 등록된 로봇 목록입니다.',
  'pages.scans.title': '스캔',
  'pages.scans.description': '플릿과 벤치마크 팩을 선택해 새 스캔 세션을 시작합니다.',
  'pages.findings.title': 'Findings',
  'pages.findings.description':
    'drift·anomaly·peer detector가 산출한 활성 Insight입니다. 자동 생성은 scan 완료 시 일어나며, 수동으로 dismiss하면 활성 목록에서 사라집니다.',
  'pages.compliance.title': 'Compliance',
  'pages.compliance.description':
    '프레임워크별 프로필을 활성화하고, 스캔 세션 결과로부터 스냅샷을 생성합니다.',
  'pages.advisor.title': 'Advisor',
  'pages.reports.title': '리포트',
  'pages.reports.description': '생성된 리포트 목록과 서명 상태를 확인합니다.',

  'common.loading': '불러오는 중…',
  'common.empty.generic': '데이터가 없습니다',
} as const

export const en: Record<keyof typeof ko, string> = {
  'app.brand': 'rosshield',
  'app.brand.subtitle': 'Security Console',
  'app.version': 'v0.1.0 · Phase 2',

  'nav.robots': 'Robots',
  'nav.scans': 'Scans',
  'nav.findings': 'Findings',
  'nav.compliance': 'Compliance',
  'nav.advisor': 'Advisor',
  'nav.reports': 'Reports',

  'header.theme.light': 'Light',
  'header.theme.dark': 'Dark',
  'header.theme.system': 'System',
  'header.theme.tooltip': 'Theme: {label} (click to cycle)',
  'header.theme.aria': 'Theme ({label})',
  'header.locale.tooltip': 'Language: {label} (click to cycle)',
  'header.locale.aria': 'Language ({label})',
  'header.logout': 'Sign out',
  'header.user.aria': 'Current user',

  'login.title': 'rosshield Console',
  'login.description': 'Sign in to the robot fleet security audit console.',
  'login.email': 'Email',
  'login.password': 'Password',
  'login.submit': 'Sign in',
  'login.submitting': 'Signing in…',
  'login.error.invalid': 'Invalid email or password',
  'login.footer': 'rosshield · v0.1.0 · Phase 2',

  'pages.robots.title': 'Robots',
  'pages.robots.description': 'Robots registered to your tenant.',
  'pages.scans.title': 'Scans',
  'pages.scans.description':
    'Choose a fleet and benchmark pack to start a new scan session.',
  'pages.findings.title': 'Findings',
  'pages.findings.description':
    'Active insights produced by drift/anomaly/peer detectors. Generated on scan completion; dismissed insights leave the active list.',
  'pages.compliance.title': 'Compliance',
  'pages.compliance.description':
    'Activate framework profiles and generate snapshots from scan sessions.',
  'pages.advisor.title': 'Advisor',
  'pages.reports.title': 'Reports',
  'pages.reports.description':
    'Generated reports and their signature status.',

  'common.loading': 'Loading…',
  'common.empty.generic': 'No data',
}

export type DictKey = keyof typeof ko
export type Locale = 'ko' | 'en'

export const DICTS: Record<Locale, Record<DictKey, string>> = { ko, en }
