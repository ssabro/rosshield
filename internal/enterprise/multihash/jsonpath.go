//go:build rosshield_enterprise

// jsonpath.go — B-1 multi-hash sub-hash 용 경량 JSONPath extractor (enterprise edition).
//
// 본 파일은 multi-hash evidence의 sub-hash 단위 추출에 필요한 최소 JSONPath 구문만
// 지원합니다 — 외부 의존 0, 결정론적 동작 보장.
//
// 지원 구문 (spec-A 인접 D8-B1 sub-hash 명세, design doc §6.3):
//
//   - `$`                 : root 자체
//   - `$.foo`             : object field
//   - `$.foo.bar`         : nested field
//   - `$.foo[0]`          : array index (음수 거부, 범위 초과 → ErrPathNotFound)
//   - `$.foo[0].bar`      : 혼합
//   - `$.foo[*]`          : wildcard — 배열의 모든 element를 expand (v2)
//   - `$.foo[*].bar`      : wildcard 뒤 nested field
//   - `$.foo[*].bar[*]`   : 중첩 wildcard — cartesian product expand
//
// 미지원 (복잡 표현식 — design doc spec 의도적 제한):
//   - filter / slice / recursive descent (`..`)
//
// wildcard 결정론:
//   - 같은 evidence + 같은 wildcard path → 항상 같은 concrete path 시퀀스(index 오름차순).
//   - 빈 배열에 wildcard 적용 → 0개 sub-hash (에러 없음).
//   - 비배열 위치에 wildcard 적용 → ErrInvalidJSONPath (스펙).
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
	"sort"
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

// jsonPathStep는 path를 토큰화한 결과의 한 단계입니다 (object key 또는 array index 또는 wildcard).
//
// 3가지 종류 (mutually exclusive):
//   - object key:  key != "" && index == -1 && !wildcard
//   - array index: key == "" && index >= 0  && !wildcard
//   - wildcard:    key == "" && index == -1 && wildcard (v2 — `[*]`)
type jsonPathStep struct {
	key      string // object field name (index/wildcard 단계면 "")
	index    int    // array index (key/wildcard 단계면 -1)
	wildcard bool   // v2 — true이면 `[*]` (배열 전체 expand)
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
			// `[N]` — 십진 비음수 정수, 또는 `[*]` — wildcard (v2).
			i++
			start := i
			for i < len(rest) && rest[i] != ']' {
				i++
			}
			if i >= len(rest) {
				return nil, fmt.Errorf("%w: unterminated '[' at offset %d", ErrInvalidJSONPath, start)
			}
			tokenStr := rest[start:i]
			if tokenStr == "" {
				return nil, fmt.Errorf("%w: empty index in '[]' at offset %d", ErrInvalidJSONPath, start)
			}
			if tokenStr == "*" {
				// wildcard step — 배열 전체 expand (concrete index 산출은 expandJSONPath 단계).
				steps = append(steps, jsonPathStep{key: "", index: -1, wildcard: true})
				i++ // skip ']'
				break
			}
			idx, err := strconv.Atoi(tokenStr)
			if err != nil {
				return nil, fmt.Errorf("%w: non-integer index %q: %v", ErrInvalidJSONPath, tokenStr, err)
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
		if step.wildcard {
			// extractByPath는 concrete path만 처리합니다. wildcard가 남아 있다면
			// 호출자가 expandJSONPath를 거치지 않은 버그 — 명시적 거부.
			return nil, fmt.Errorf("%w: step %d wildcard not allowed in extractByPath (expand first)", ErrInvalidJSONPath, stepIdx)
		}
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

// expandJSONPath는 path를 evidence에 대해 wildcard expansion한 concrete path 리스트로
// 반환합니다 (v2).
//
// 동작:
//   - path에 `[*]`가 없으면 → []string{path} 단일 반환 (원본 그대로).
//   - path에 `[*]`가 있으면 → 해당 위치 배열의 모든 element index로 expand:
//     예) evidence={"checks":[{...},{...},{...}]}, path="$.checks[*].status"
//     → ["$.checks[0].status", "$.checks[1].status", "$.checks[2].status"]
//   - 중첩 wildcard는 cartesian product:
//     예) path="$.a[*].b[*]" → a의 길이 × 각 a[i].b의 길이 만큼 concrete path 생성.
//   - 빈 배열에 wildcard 적용 → 해당 expansion에서 0개 path 생성 (에러 없음).
//   - 비배열 위치에 wildcard 적용 → ErrInvalidJSONPath (스펙).
//   - wildcard 앞 segment에 object key가 부재하면 → ErrPathNotFound.
//
// 결정론: 반환되는 concrete path는 array index 오름차순(0,1,2,...). 같은 input은 항상
// 같은 output을 산출합니다.
//
// 반환 path 형식은 원본 syntax와 동일 (`$.foo[0].bar` 식 concrete index만 포함).
func expandJSONPath(evidence []byte, path string) ([]string, error) {
	steps, err := parseJSONPath(path)
	if err != nil {
		return nil, err
	}
	// wildcard가 한 개도 없으면 fast path — 원본 path를 그대로 반환합니다.
	hasWildcard := false
	for _, s := range steps {
		if s.wildcard {
			hasWildcard = true
			break
		}
	}
	if !hasWildcard {
		return []string{path}, nil
	}

	var root any
	dec := json.NewDecoder(bytes.NewReader(evidence))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("%w: evidence is not valid JSON: %v", ErrInvalidJSONPath, err)
	}

	// recursive expansion — 현재까지의 prefix(완성된 step 시퀀스) + 현재 traverse 위치(cur)에서
	// 남은 step을 처리하며 concrete path 누적.
	type frame struct {
		cur      any            // 현재 traverse 위치
		stepIdx  int            // 다음 처리할 step index
		concrete []jsonPathStep // 지금까지 확정된 concrete step 시퀀스
	}
	out := make([]string, 0, 4)
	stack := []frame{{cur: root, stepIdx: 0, concrete: nil}}
	for len(stack) > 0 {
		// pop (DFS) — 결정론 위해 LIFO인데, 끝에 push할 때 역순으로 넣어 index 오름차순 유지.
		fr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if fr.stepIdx == len(steps) {
			// 모든 step 소진 — concrete path 1건 확정.
			out = append(out, renderJSONPath(fr.concrete))
			continue
		}
		step := steps[fr.stepIdx]
		if step.wildcard {
			arr, ok := fr.cur.([]any)
			if !ok {
				return nil, fmt.Errorf("%w: step %d wildcard `[*]` requires array, got %T", ErrInvalidJSONPath, fr.stepIdx, fr.cur)
			}
			// 빈 배열 → 이 branch에서 frame 추가 없이 종료 (0건 산출).
			// 결정론: 작은 index가 먼저 처리되도록 큰 index를 먼저 push.
			for i := len(arr) - 1; i >= 0; i-- {
				next := make([]jsonPathStep, len(fr.concrete)+1)
				copy(next, fr.concrete)
				next[len(fr.concrete)] = jsonPathStep{key: "", index: i}
				stack = append(stack, frame{
					cur:      arr[i],
					stepIdx:  fr.stepIdx + 1,
					concrete: next,
				})
			}
			continue
		}
		if step.key != "" {
			obj, ok := fr.cur.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%w: step %d expected object for key %q", ErrPathNotFound, fr.stepIdx, step.key)
			}
			next, exists := obj[step.key]
			if !exists {
				return nil, fmt.Errorf("%w: step %d key %q absent", ErrPathNotFound, fr.stepIdx, step.key)
			}
			nextConcrete := make([]jsonPathStep, len(fr.concrete)+1)
			copy(nextConcrete, fr.concrete)
			nextConcrete[len(fr.concrete)] = step
			stack = append(stack, frame{cur: next, stepIdx: fr.stepIdx + 1, concrete: nextConcrete})
			continue
		}
		// concrete array index step.
		arr, ok := fr.cur.([]any)
		if !ok {
			return nil, fmt.Errorf("%w: step %d expected array for index %d", ErrPathNotFound, fr.stepIdx, step.index)
		}
		if step.index >= len(arr) {
			return nil, fmt.Errorf("%w: step %d index %d out of bounds (len=%d)", ErrPathNotFound, fr.stepIdx, step.index, len(arr))
		}
		nextConcrete := make([]jsonPathStep, len(fr.concrete)+1)
		copy(nextConcrete, fr.concrete)
		nextConcrete[len(fr.concrete)] = step
		stack = append(stack, frame{cur: arr[step.index], stepIdx: fr.stepIdx + 1, concrete: nextConcrete})
	}

	// DFS LIFO 처리로 index 오름차순 출력이 나오도록 push했지만, 단일 stack에서 nested
	// wildcard가 섞이면 순서가 흔들릴 수 있음 — 보장 위해 최종 sort.
	sort.Strings(out)
	return out, nil
}

// renderJSONPath는 concrete step 시퀀스(no wildcard)를 path 문자열로 직렬화합니다.
// 예) [{key:"foo"},{index:0},{key:"bar"}] → "$.foo[0].bar".
func renderJSONPath(steps []jsonPathStep) string {
	var b strings.Builder
	b.WriteByte('$')
	for _, s := range steps {
		if s.key != "" {
			b.WriteByte('.')
			b.WriteString(s.key)
			continue
		}
		// concrete array index.
		b.WriteByte('[')
		b.WriteString(strconv.Itoa(s.index))
		b.WriteByte(']')
	}
	return b.String()
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
