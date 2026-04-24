package benchmark

import (
	"bytes"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Self-Test 형식 (C6):
//
//   apiVersion: rosshield.io/v1
//   kind: SelfTest
//   metadata:
//     checkId: CIS-1.1.1.1
//   spec:
//     cases:
//       - name: "passes when cramfs not loaded"
//         input:
//           stdout: ""
//           stderr: ""
//           exitCode: 0
//         expectedOutcome: PASS
//       - name: "..."
//         input:
//           stdout: "cramfs 12345 0"
//         expectedOutcome: FAIL
//
// fixture 파일 자체가 없는 check는 self-test가 "degraded" — pack 신뢰도 낮음 표시.

const KindSelfTest = "SelfTest"

// SelfTestCase는 단일 fixture 케이스입니다.
type SelfTestCase struct {
	Name            string
	Input           EvalInput
	ExpectedOutcome EvalStatus
}

// SelfTestFailure는 케이스 한 건의 실패 상세입니다.
type SelfTestFailure struct {
	CaseName string
	Got      EvalStatus
	Want     EvalStatus
	Reason   string
}

// SelfTestResult는 단일 check의 self-test 결과입니다.
type SelfTestResult struct {
	CheckID  string
	Total    int
	Passed   int
	Failures []SelfTestFailure
	Degraded bool // fixture YAML이 없거나 case가 0개면 true (검증 불가능)
}

// AllPassed는 모든 케이스가 통과했고 degraded가 아닌지 반환합니다.
func (r SelfTestResult) AllPassed() bool {
	return !r.Degraded && len(r.Failures) == 0 && r.Passed == r.Total && r.Total > 0
}

// selftest YAML 와이어 형식.
type selftestYAML struct {
	APIVersion string           `yaml:"apiVersion"`
	Kind       string           `yaml:"kind"`
	Metadata   selftestMetaYAML `yaml:"metadata"`
	Spec       selftestSpecYAML `yaml:"spec"`
}

type selftestMetaYAML struct {
	CheckID string `yaml:"checkId"`
}

type selftestSpecYAML struct {
	Cases []selftestCaseYAML `yaml:"cases"`
}

type selftestCaseYAML struct {
	Name            string            `yaml:"name"`
	Input           selftestInputYAML `yaml:"input"`
	ExpectedOutcome string            `yaml:"expectedOutcome"`
}

type selftestInputYAML struct {
	Stdout   string `yaml:"stdout"`
	Stderr   string `yaml:"stderr"`
	ExitCode int    `yaml:"exitCode"`
}

// ParseSelfTestYAML은 fixture YAML 바이트를 파싱해 케이스 슬라이스를 반환합니다.
func ParseSelfTestYAML(data []byte) (checkID string, cases []SelfTestCase, err error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc selftestYAML
	if err := dec.Decode(&doc); err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrInvalidYAML, err)
	}
	if doc.APIVersion != APIVersion {
		return "", nil, fmt.Errorf("%w: %q", ErrUnknownAPIVersion, doc.APIVersion)
	}
	if doc.Kind != KindSelfTest {
		return "", nil, fmt.Errorf("%w: %q (want %q)", ErrUnknownKind, doc.Kind, KindSelfTest)
	}
	if strings.TrimSpace(doc.Metadata.CheckID) == "" {
		return "", nil, fmt.Errorf("%w: metadata.checkId required", ErrSchemaViolation)
	}

	out := make([]SelfTestCase, 0, len(doc.Spec.Cases))
	for i, c := range doc.Spec.Cases {
		want := EvalStatus(strings.ToUpper(strings.TrimSpace(c.ExpectedOutcome)))
		if want != StatusPass && want != StatusFail && want != StatusIndeterminate {
			return "", nil, fmt.Errorf("%w: cases[%d].expectedOutcome=%q",
				ErrSchemaViolation, i, c.ExpectedOutcome)
		}
		out = append(out, SelfTestCase{
			Name: c.Name,
			Input: EvalInput{
				Stdout:   c.Input.Stdout,
				Stderr:   c.Input.Stderr,
				ExitCode: c.Input.ExitCode,
			},
			ExpectedOutcome: want,
		})
	}
	return doc.Metadata.CheckID, out, nil
}

// RunCheckSelfTest는 단일 check + (선택) selftest fixture로 self-test를 실행합니다.
//
// fixtureYAML이 nil이면 SelfTestResult.Degraded = true (검증 불가능).
// EvaluationRule이 비어 있으면 ErrEmptyEvalRule (system error).
//
// 결과: 모든 케이스 통과 + Degraded=false → AllPassed() = true.
func RunCheckSelfTest(check Check, fixtureYAML []byte) (SelfTestResult, error) {
	res := SelfTestResult{CheckID: check.CheckID}

	if len(fixtureYAML) == 0 {
		res.Degraded = true
		return res, nil
	}

	checkID, cases, err := ParseSelfTestYAML(fixtureYAML)
	if err != nil {
		return SelfTestResult{}, err
	}
	if checkID != check.CheckID {
		return SelfTestResult{}, fmt.Errorf("%w: selftest checkId=%q != check.CheckID=%q",
			ErrSchemaViolation, checkID, check.CheckID)
	}
	if len(cases) == 0 {
		res.Degraded = true
		return res, nil
	}

	if len(check.EvaluationRule) == 0 {
		return SelfTestResult{}, ErrEmptyEvalRule
	}
	node, err := ParseEvalRule(check.EvaluationRule)
	if err != nil {
		return SelfTestResult{}, err
	}

	res.Total = len(cases)
	for _, c := range cases {
		got, evalErr := node.Eval(c.Input)
		if evalErr != nil {
			res.Failures = append(res.Failures, SelfTestFailure{
				CaseName: c.Name,
				Got:      "",
				Want:     c.ExpectedOutcome,
				Reason:   "system error: " + evalErr.Error(),
			})
			continue
		}
		if got.Status != c.ExpectedOutcome {
			res.Failures = append(res.Failures, SelfTestFailure{
				CaseName: c.Name,
				Got:      got.Status,
				Want:     c.ExpectedOutcome,
				Reason:   got.Reason,
			})
		} else {
			res.Passed++
		}
	}
	return res, nil
}

// RunPackSelfTests는 Pack의 모든 check에 대해 self-test를 실행합니다.
//
// fixtures map은 checkID → selftest YAML bytes. fixture 없는 check는 Degraded.
// pack 전체가 신뢰 가능하려면 모든 check가 AllPassed() 여야 함 (호출자 정책).
func RunPackSelfTests(pack Pack, fixtures map[string][]byte) ([]SelfTestResult, error) {
	out := make([]SelfTestResult, 0, len(pack.Checks))
	for _, check := range pack.Checks {
		fix := fixtures[check.CheckID] // nil이면 RunCheckSelfTest가 Degraded 처리
		res, err := RunCheckSelfTest(check, fix)
		if err != nil {
			return nil, fmt.Errorf("benchmark: self-test %q: %w", check.CheckID, err)
		}
		out = append(out, res)
	}
	return out, nil
}
