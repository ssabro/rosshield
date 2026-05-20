package setup

import "strings"

// quoteIdent는 PG identifier(테이블·PUBLICATION·SUBSCRIPTION 이름)를 double-quote
// escape합니다.
//
// PG spec: identifier 안의 `"`는 `""`로 escape. 본 함수는 모든 identifier를 항상
// double-quote로 감싸서 reserved word 충돌 방지. validateName에서 1차 sanitization을
// 했으므로 escape가 필요한 경우는 거의 없지만, 방어 차원에서 escape는 유지.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// escapeConnString은 conn string의 single quote를 두 개로 escape합니다.
//
// PG SQL string literal spec — two single-quotes represent one literal single quote.
// CREATE SUBSCRIPTION CONNECTION 안에 password가 single quote를 포함할 때 안전.
//
// 본 함수는 backslash escape는 처리하지 않습니다 (PG 기본 standard_conforming_strings=on
// 가정). 운영 시 password에 backslash 문자가 있으면 conn string PROPERTY 형식으로
// 분리해 전달하는 것을 권장 (운영 doc 참조).
func escapeConnString(s string) string {
	return strings.ReplaceAll(s, `'`, `''`)
}
