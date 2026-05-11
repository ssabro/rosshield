// Package multihash implements D8-B1 multi-hash evidence (SHA256 + BLAKE3 + 옵션
// SHA3-512) — enterprise edition.
//
// 본 패키지는 enterprise build tag(`rosshield_enterprise`) 안에서만 실 구현이
// 빌드됩니다. 코어 빌드에서는 SHA256 단일 해시만 사용합니다.
//
// 실제 알고리즘은 D8 KR 우선출원 완료 후 E32 stage에서 채워집니다.
// 설계: docs/design/13-patent-strategy.md §13.3, docs/design/phase5-backlog.md §E32.
package multihash
