//go:build rosshield_enterprise

// jsonpath.go — B-1 multi-hash sub-hash 용 경량 JSONPath extractor (enterprise edition).
//
// 본 파일은 multi-hash evidence의 sub-hash 단위 추출에 필요한 최소 JSONPath 구문만
// 지원합니다 — 외부 의존 0, 결정론적 동작 보장.
//
// 지원 구문 (spec-A 인접 D8-B1 sub-hash 명세, design doc §6.3):
//
//   - `$`              : root 자체
//   - `$.foo`          : object field
//   - `$.foo.bar`      : nested field
//   - `$.foo[0]`       : array index (음수 거부, 범위 초과 → ErrPathNotFound)
//   - `$.foo[0].bar`   : 혼합
//
// 미지원 (복잡 표현식 — design doc spec 의도적 제한):
//   - wildcard `$.foo[*]` (미래 확장 여지)
//   - filter / slice / recursive descent (`..`)
//
// canonical 직렬화: 추출된 sub-tree는 json.Marshal로 산출 (Go map은 key 정렬되므로
// MarshalIndent 없이 결정론적). 단 nested map은 json.Decoder가 map[string]any로
// 디코딩하므로 marshal 시 key 사전식 정렬되어 결정론 유지.
//
// 참조:
//   - docs/design/notes/phase7-public-transition-design.md §6.3 D8-B1 sub-hash
//   - docs/design/13-patent-strategy.md §13.3 청구권 B-1

package multihash

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// 오류 정의 — jsonpath 도메인.
var (
	// ErrInvalidJSONPath는 JSONPath 구문이 미지원이거나 형식 오류일 때 반환됩니다.
	ErrInvalidJSONPath = errors.New("multihash: invalid jsonpath")

	// ErrPathNotFound는 path가 구문상 유효하나 evidence 안 해당 위치에 값이 없을 때
	// 반환됩니다 (object key 부재 / array index 범위 초과 / 비-object 항목 traverse).
	ErrPathNotFound = errors.New("multihash: jsonpath not found in evidence")
)

// jsonPathStep는 path를 토큰화한 결과의 한 단계입니다 (object key 또는 array index).
type jsonPathStep struct {
	key   string // object field name (index 단계면 "")
	index int    // array index (key 단계면 -1)
}

// parseJSONPath는 path 문자열을 단계 시퀀스로 토큰화합니다.
//
// `$` 단독 → 빈 슬라이스 (root 자체).
// `$.foo[0].bar` → [{key:"foo"}, {index:0}, {key:"bar"}].
//
// 형식 오류 시 ErrInvalidJSONPath wrap 반환.
func parseJSONPath(path string) ([]jsonPathStep, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: empty path", ErrInvalidJSONPath)
	}
	if !strings.HasPrefix(path, "$") {
		return nil, fmt.Errorf("%w: must start with $, got %q", ErrInvalidJSONPath, path)
	}

	rest := path[1:]
	if rest == "" {
		return []jsonPathStep{}, nil
	}

	steps := make([]jsonPathStep, 0, 4)
	i := 0
	for i < len(rest) {
		switch rest[i] {
		case '.':
			// `.field` — 다음 `.`, `[`, end까지가 field name.
			i++
			start := i
			for i < len(rest) && rest[i] != '.' && rest[i] != '[' {
				i++
			}
			if start == i {
				return nil, fmt.Errorf("%w: empty field name after '.' at offset %d", ErrInvalidJSONPath, start)
			}
			steps = append(steps, jsonPathStep{key: rest[start:i], index: -1})
		case '[':
			// `[N]` — 십진 비음수 정수.
			i++
			start := i
			for i < len(rest) && rest[i] != ']' {
				i++
			}
			if i >= len(rest) {
				return nil, fmt.Errorf("%w: unterminated '[' at offset %d", ErrInvalidJSONPath, start)
			}
			idxStr := rest[start:i]
			if idxStr == "" {
				return nil, fmt.Errorf("%w: empty index in '[]' at offset %d", ErrInvalidJSONPath, start)
			}
			idx, err := strconv.Atoi(idxStr)
			if err != nil {
				return nil, fmt.Errorf("%w: non-integer index %q: %v", ErrInvalidJSONPath, idxStr, err)
			}
			if idx < 0 {
				return nil, fmt.Errorf("%w: negative index %d", ErrInvalidJSONPath, idx)
			}
			steps = append(steps, jsonPathStep{key: "", index: idx})
			i++ // skip ']'
		default:
			return nil, fmt.Errorf("%w: unexpected char %q at offset %d", ErrInvalidJSONPath, rest[i], i)
		}
	}
	return steps, nil
}

// extractByPath는 evidence(JSON bytes)에서 path 위치 값을 추출하여 canonical JSON으로
// 다시 직렬화합니다.
//
// evidence는 valid JSON이어야 합니다. 그렇지 않으면 ErrInvalidJSONPath 반환
// (parse 단계 실패 — 사용자가 LineHash 모드 등 다른 경로 선택 권장).
//
// 추출 결과는 json.Marshal로 다시 직렬화되며, Go map은 key 사전식 정렬되어
// 결정론 보장 (sub-hash 입력이 호스트마다 같음).
func extractByPath(evidence []byte, path string) ([]byte, error) {
	steps, err := parseJSONPath(path)
	if err != nil {
		return nil, err
	}

	var root any
	dec := json.NewDecoder(bytes.NewReader(evidence))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("%w: evidence is not valid JSON: %v", ErrInvalidJSONPath, err)
	}

	cur := root
	for stepIdx, step := range steps {
		if step.key != "" {
			obj, ok := cur.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%w: step %d expected object for key %q", ErrPathNotFound, stepIdx, step.key)
			}
			next, exists := obj[step.key]
			if !exists {
				return nil, fmt.Errorf("%w: step %d key %q absent", ErrPathNotFound, stepIdx, step.key)
			}
			cur = next
			continue
		}
		// array index 단계.
		arr, ok := cur.([]any)
		if !ok {
			return nil, fmt.Errorf("%w: step %d expected array for index %d", ErrPathNotFound, stepIdx, step.index)
		}
		if step.index >= len(arr) {
			return nil, fmt.Errorf("%w: step %d index %d out of bounds (len=%d)", ErrPathNotFound, stepIdx, step.index, len(arr))
		}
		cur = arr[step.index]
	}

	// 추출된 sub-tree를 canonical JSON으로 직렬화 — Go map은 key 사전식 정렬됨.
	out, err := canonicalMarshal(cur)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal extracted value: %v", ErrInvalidJSONPath, err)
	}
	return out, nil
}

// canonicalMarshal은 임의의 JSON value를 결정론적 바이트로 직렬화합니다.
//
// json.Marshal은 map[string]any에 대해 key를 사전식 정렬하므로, 같은 sub-tree에
// 대해 항상 같은 바이트 시퀀스를 산출합니다. 다만 json.Encoder의 trailing newline은
// 제거합니다 (canonical 규칙 일관).
func canonicalMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
