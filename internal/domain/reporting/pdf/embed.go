package pdf

// embed.go — 한글 폰트(NanumGothic.ttf)를 바이너리에 박아 단일 바이너리 원칙(P3 에어갭)을
// 유지합니다. 폰트 파일은 SIL OFL-1.1 라이선스이며 `fonts/LICENSE.txt`와 함께 재배포됩니다.
//
// 결정성(R10-5): TTF byte 자체가 PDF stream에 포함되므로, 폰트 파일이 변하면 PDF byte도
// 변합니다. 골든 fixture(`testdata/golden_*.sha256`)가 회귀 가드 역할을 합니다.

import _ "embed"

//go:embed fonts/NanumGothic.ttf
var nanumGothicTTF []byte

// nanumGothicBytes는 임베드된 NanumGothic Regular TTF 데이터를 반환합니다. 폰트 파일이
// 누락된 빌드(`go:embed`로 0-byte slice가 박힌 경우)는 길이가 0이 됩니다 — 호출자가 사전
// 검증해야 합니다(`HasKoreanFont()`).
func nanumGothicBytes() []byte {
	return nanumGothicTTF
}

// HasKoreanFont는 한글 폰트가 임베드되어 사용 가능한지를 반환합니다. 외부에 노출하는
// 까닭은 테스트(graceful skip)와 호출자(영문 전용 fallback 결정)가 모두 사용하기 때문입니다.
func HasKoreanFont() bool {
	return len(nanumGothicTTF) > 0
}
