// Package wasmrt implements D8-C1 WASM sandboxed evaluator (check rule WASM 실행
// + memory/CPU 제한) — enterprise edition.
//
// 본 패키지는 enterprise build tag(`rosshield_enterprise`) 안에서만 실 구현이
// 빌드됩니다. 코어 빌드에서는 Go-native check evaluator만 사용합니다.
//
// 실제 알고리즘은 D8 KR 우선출원 완료 후 E32 stage에서 채워집니다 (R40-3에서
// wazero vs wasmtime-go 결정 선행).
// 설계: docs/design/13-patent-strategy.md §13.3, docs/design/phase5-backlog.md §E32.
package wasmrt
