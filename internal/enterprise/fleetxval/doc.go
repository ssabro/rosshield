// Package fleetxval implements D8-D2 fleet cross-validation (테넌트 내 robot 간
// scan 결과 상호 검증) — enterprise edition.
//
// 본 패키지는 enterprise build tag(`rosshield_enterprise`) 안에서만 실 구현이
// 빌드됩니다. 코어 빌드에서는 robot 간 cross-validation이 비활성화됩니다.
//
// 실제 알고리즘은 D8 KR 우선출원 완료 후 E32 stage에서 채워집니다.
// 설계: docs/design/13-patent-strategy.md §13.3, docs/design/phase5-backlog.md §E32.
package fleetxval
