package converter

// selftest.go — 변환된 pack의 Self-Test fixture skeleton 생성기 (E12 Stage D · T5).
//
// 자동 변환된 check는 stdout에 `** PASS **`/`** FAIL **` 마커 둘 중 하나만 출력하므로
// fixture가 결정론적으로 만들어진다 — PASS 케이스 + FAIL 케이스. degraded check은
// sentinel rule이 stdout에 절대 매칭되지 않아 PASS 케이스를 만들 수 없으므로 skeleton을
// 생성하지 않고 별도 보고서 항목으로 남김(사용자가 수동 fixture 작성).
//
// 본 모듈은 외부 도메인을 import하지 않음 — 식별 로직은 evaluationRule JSON 구조 검사로 처리.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"go.yaml.in/yaml/v3"
)

// SelfTestSkeleton은 단일 check에 대한 자동 생성 fixture입니다.
type SelfTestSkeleton struct {
	CheckID string
	YAML    []byte // benchmark.ParseSelfTestYAML이 그대로 수용
}

// SelfTestReport는 Pack 전체의 skeleton 생성 결과를 분류해 담습니다.
//
// 합계: len(Skeletons) + len(Degraded) + len(Custom) == len(pack.Checks).
type SelfTestReport struct {
	Skeletons []SelfTestSkeleton // 자동 생성된 fixture
	Degraded  []string           // sentinel rule(degradedEvalRuleJSON)이 적용된 check ID들
	Custom    []string           // 자동 마커 매칭이 아닌 custom rule — 수동 fixture 필요
}

// passMarkerValue·degradedMarkerValue는 변환기가 사용하는 두 marker 문자열입니다.
//
// 식별 정밀도를 위해 single-op `contains` JSON 형태로 비교 — 다른 op나 nested 구조는
// custom rule(수동 fixture)로 분류.
const (
	passMarkerValue     = "** PASS **"
	degradedMarkerValue = "<degraded — Phase 2 fixture required>"
)

// GenerateSelfTestSkeletons는 Pack의 모든 check를 분류해 fixture skeleton 보고서를 만듭니다.
//
// PASS 마커 매칭 rule(`{"op":"contains","value":"** PASS **"}`) → skeleton 자동 생성.
// degraded sentinel rule → Degraded 목록에만 추가.
// 그 외(composite/regex/...) → Custom 목록에 추가, 사용자 수동 작성 필요.
//
// Degraded·Custom은 정렬된 순서로 반환 — 콘솔/리포트 출력 안정성.
func GenerateSelfTestSkeletons(p Pack) SelfTestReport {
	var report SelfTestReport
	for _, c := range p.Checks {
		switch classifyEvalRule(c.EvaluationRule) {
		case ruleClassPassMarker:
			yamlBytes, err := buildSelfTestYAML(c.ID)
			if err != nil {
				// builder는 정적 입력만 사용하므로 에러는 라이브러리 버그 — Custom으로 분류해 흘림.
				report.Custom = append(report.Custom, c.ID)
				continue
			}
			report.Skeletons = append(report.Skeletons, SelfTestSkeleton{
				CheckID: c.ID,
				YAML:    yamlBytes,
			})
		case ruleClassDegraded:
			report.Degraded = append(report.Degraded, c.ID)
		default:
			report.Custom = append(report.Custom, c.ID)
		}
	}
	sort.Strings(report.Degraded)
	sort.Strings(report.Custom)
	sort.Slice(report.Skeletons, func(i, j int) bool {
		return report.Skeletons[i].CheckID < report.Skeletons[j].CheckID
	})
	return report
}

type ruleClass int

const (
	ruleClassCustom ruleClass = iota
	ruleClassPassMarker
	ruleClassDegraded
)

// classifyEvalRule은 evaluationRule JSON을 단순 contains-marker 형태인지 검사합니다.
//
// 정확한 구조 매칭(op=contains + value=마커 + 추가 키 없음)을 요구 — false positive 방지.
func classifyEvalRule(rule json.RawMessage) ruleClass {
	if len(rule) == 0 {
		return ruleClassCustom
	}
	var node map[string]any
	if err := json.Unmarshal(rule, &node); err != nil {
		return ruleClassCustom
	}
	if len(node) != 2 {
		return ruleClassCustom
	}
	op, _ := node["op"].(string)
	value, _ := node["value"].(string)
	if op != "contains" {
		return ruleClassCustom
	}
	switch value {
	case passMarkerValue:
		return ruleClassPassMarker
	case degradedMarkerValue:
		return ruleClassDegraded
	}
	return ruleClassCustom
}

// buildSelfTestYAML은 PASS/FAIL 두 케이스 fixture YAML을 생성합니다.
//
// 출력 예시:
//
//	apiVersion: rosshield.io/v1
//	kind: SelfTest
//	metadata:
//	  checkId: <id>
//	spec:
//	  cases:
//	    - name: passes when stdout contains '** PASS **'
//	      input:
//	        stdout: '** PASS **'
//	        stderr: ""
//	        exitCode: 0
//	      expectedOutcome: PASS
//	    - name: fails when stdout contains '** FAIL **'
//	      input:
//	        stdout: '** FAIL **'
//	        stderr: ""
//	        exitCode: 0
//	      expectedOutcome: FAIL
func buildSelfTestYAML(checkID string) ([]byte, error) {
	doc := selftestSkeletonYAML{
		APIVersion: APIVersion,
		Kind:       "SelfTest",
		Metadata:   selftestSkeletonMeta{CheckID: checkID},
		Spec: selftestSkeletonSpec{
			Cases: []selftestSkeletonCase{
				{
					Name: fmt.Sprintf("passes when stdout contains %q", passMarkerValue),
					Input: selftestSkeletonInput{
						Stdout: passMarkerValue, ExitCode: 0,
					},
					ExpectedOutcome: "PASS",
				},
				{
					Name: fmt.Sprintf("fails when stdout contains %q", "** FAIL **"),
					Input: selftestSkeletonInput{
						Stdout: "** FAIL **", ExitCode: 0,
					},
					ExpectedOutcome: "FAIL",
				},
			},
		},
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("converter: encode selftest skeleton: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("converter: close selftest encoder: %w", err)
	}
	return buf.Bytes(), nil
}

// selftest YAML 와이어 형식 — `internal/domain/benchmark/selftest.go`의 selftestYAML과 1:1.
// (외부 도메인 import 없이 자체 정의 — 라운드트립은 테스트가 보장.)
type selftestSkeletonYAML struct {
	APIVersion string               `yaml:"apiVersion"`
	Kind       string               `yaml:"kind"`
	Metadata   selftestSkeletonMeta `yaml:"metadata"`
	Spec       selftestSkeletonSpec `yaml:"spec"`
}

type selftestSkeletonMeta struct {
	CheckID string `yaml:"checkId"`
}

type selftestSkeletonSpec struct {
	Cases []selftestSkeletonCase `yaml:"cases"`
}

type selftestSkeletonCase struct {
	Name            string                `yaml:"name"`
	Input           selftestSkeletonInput `yaml:"input"`
	ExpectedOutcome string                `yaml:"expectedOutcome"`
}

type selftestSkeletonInput struct {
	Stdout   string `yaml:"stdout"`
	Stderr   string `yaml:"stderr"`
	ExitCode int    `yaml:"exitCode"`
}
