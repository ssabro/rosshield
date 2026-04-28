// Package converter는 외부 baseline JSON을 rosshield pack 디렉터리로 변환하는
// 공통 표면을 제공합니다 (E12 Stage A 골격).
//
// 변환 입력은 포맷별로 다양(CIS·ROS2 framework·...)이지만 출력은 항상 동일한
// rosshield pack format(`apiVersion: rosshield.io/v1`)이므로, intermediate `Pack`/`Check`
// 표현으로 정규화한 뒤 `WriteToDir`로 직렬화합니다.
//
// 본 패키지는 외부 도메인을 import하지 않습니다 — pack format schema는
// `internal/domain/benchmark/yaml.go`와 동일한 와이어 형식을 자체 packYAML/checkYAML로 보유.
// 라운드트립 검증(`benchmark.ParsePackYAML`로 다시 파싱)은 테스트가 보장.
package converter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// API 상수 — `internal/domain/benchmark`와 동일.
const (
	APIVersion       = "rosshield.io/v1"
	KindPack         = "Pack"
	KindCheck        = "Check"
	DefaultSeverity  = "medium"
	DefaultSchemaVer = 1
)

// Pack은 변환 결과 intermediate 표현입니다.
//
// benchmark.Pack과 닮았지만 ID·TenantID·ManifestHash 등 DB 필드는 제외 — 변환은
// 와이어 형식만 책임지고 DB 인입은 InstallPack의 영역.
type Pack struct {
	Name          string
	Version       string
	Vendor        string
	Description   string
	SchemaVersion int
	Checks        []Check
}

// Check은 단일 체크의 변환 결과 intermediate입니다.
//
// EvaluationRule은 JSON RawMessage — convert 시점에 이미 화이트리스트 op AST로
// 정규화된 상태여야 합니다(Stage B passlogic.go가 그 책임). WriteToDir는 단순히
// JSON → YAML 재직렬화만 수행.
type Check struct {
	ID             string // 'CIS-1.1.1.1' 같은 사람 친화적 ID — 파일명도 됨
	Title          string
	Description    string
	Severity       string // "low"|"medium"|"high"|"critical" (빈 값은 medium)
	AuditCommand   string
	EvaluationRule json.RawMessage
	Rationale      string
	FixGuidance    string
}

// 와이어 형식 — `internal/domain/benchmark/yaml.go`의 packYAML/checkYAML과 1:1 호환.
// (오프라인 도구이므로 import 없이 자체 정의 — 변환 결과는 라운드트립 테스트로 등가성 보장.)

type packYAML struct {
	APIVersion string       `yaml:"apiVersion"`
	Kind       string       `yaml:"kind"`
	Metadata   packMetaYAML `yaml:"metadata"`
	Spec       packSpecYAML `yaml:"spec"`
}

type packMetaYAML struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Vendor      string `yaml:"vendor"`
	Description string `yaml:"description,omitempty"`
}

type packSpecYAML struct {
	SchemaVersion int `yaml:"schemaVersion"`
}

type checkYAML struct {
	APIVersion string        `yaml:"apiVersion"`
	Kind       string        `yaml:"kind"`
	Metadata   checkMetaYAML `yaml:"metadata"`
	Spec       checkSpecYAML `yaml:"spec"`
}

type checkMetaYAML struct {
	ID          string `yaml:"id"`
	Title       string `yaml:"title"`
	Description string `yaml:"description,omitempty"`
	Severity    string `yaml:"severity"`
}

type checkSpecYAML struct {
	AuditCommand   string `yaml:"auditCommand"`
	EvaluationRule any    `yaml:"evaluationRule"`
	Rationale      string `yaml:"rationale,omitempty"`
	FixGuidance    string `yaml:"fixGuidance,omitempty"`
}

// 에러.
var (
	ErrOutputExists     = errors.New("converter: output directory already exists")
	ErrEmptyPackName    = errors.New("converter: pack.Name is empty")
	ErrEmptyCheckID     = errors.New("converter: check.ID is empty")
	ErrDuplicateCheckID = errors.New("converter: duplicate check ID within pack")
	ErrInvalidEvalRule  = errors.New("converter: invalid evaluationRule JSON")
)

// MarshalPack은 intermediate Pack을 pack.yaml 바이트로 직렬화합니다.
//
// SchemaVersion이 0이면 DefaultSchemaVer(=1) 적용.
func MarshalPack(p Pack) ([]byte, error) {
	if p.Name == "" {
		return nil, ErrEmptyPackName
	}
	schemaVer := p.SchemaVersion
	if schemaVer == 0 {
		schemaVer = DefaultSchemaVer
	}
	doc := packYAML{
		APIVersion: APIVersion,
		Kind:       KindPack,
		Metadata: packMetaYAML{
			Name:        p.Name,
			Version:     p.Version,
			Vendor:      p.Vendor,
			Description: p.Description,
		},
		Spec: packSpecYAML{SchemaVersion: schemaVer},
	}
	return yaml.Marshal(doc)
}

// MarshalCheck은 intermediate Check을 checks/<id>.yaml 바이트로 직렬화합니다.
//
// EvaluationRule이 비어있으면 evaluationRule 필드는 null로 출력 — benchmark.ParseCheckYAML이
// 이를 빈 RawMessage로 받아 후속 ParseEvalRule이 ErrEmptyEvalRule을 반환합니다.
// 따라서 Stage B/C 변환기는 모든 check에 유효한 EvaluationRule을 채워야 합니다.
//
// Severity 기본값은 DefaultSeverity ("medium").
func MarshalCheck(c Check) ([]byte, error) {
	if c.ID == "" {
		return nil, ErrEmptyCheckID
	}
	severity := c.Severity
	if severity == "" {
		severity = DefaultSeverity
	}
	var ruleAny any
	if len(c.EvaluationRule) > 0 {
		if err := json.Unmarshal(c.EvaluationRule, &ruleAny); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidEvalRule, err)
		}
	}
	doc := checkYAML{
		APIVersion: APIVersion,
		Kind:       KindCheck,
		Metadata: checkMetaYAML{
			ID:          c.ID,
			Title:       c.Title,
			Description: c.Description,
			Severity:    severity,
		},
		Spec: checkSpecYAML{
			AuditCommand:   c.AuditCommand,
			EvaluationRule: ruleAny,
			Rationale:      c.Rationale,
			FixGuidance:    c.FixGuidance,
		},
	}
	return yaml.Marshal(doc)
}

// WriteToDir는 Pack을 outputDir에 펼칩니다.
//
//	outputDir/
//	  pack.yaml
//	  checks/
//	    <id>.yaml ...
//
// outputDir이 이미 존재하면 ErrOutputExists — 의도치 않은 덮어쓰기 차단.
// 부모 디렉터리는 호출자가 보장(존재하지 않으면 MkdirAll로 생성).
//
// duplicate ID는 ErrDuplicateCheckID로 즉시 거부 — 변환기가 ID 충돌을 흘려보내면
// 마지막 항목으로 silently 덮어쓰여지는 함정을 방지.
func WriteToDir(p Pack, outputDir string) error {
	if _, err := os.Stat(outputDir); err == nil {
		return fmt.Errorf("%w: %s", ErrOutputExists, outputDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("converter: stat output dir: %w", err)
	}

	checksDir := filepath.Join(outputDir, "checks")
	if err := os.MkdirAll(checksDir, 0o755); err != nil {
		return fmt.Errorf("converter: mkdir: %w", err)
	}

	packBytes, err := MarshalPack(p)
	if err != nil {
		return fmt.Errorf("converter: marshal pack: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "pack.yaml"), packBytes, 0o644); err != nil {
		return fmt.Errorf("converter: write pack.yaml: %w", err)
	}

	seen := make(map[string]struct{}, len(p.Checks))
	for _, c := range p.Checks {
		if c.ID == "" {
			return ErrEmptyCheckID
		}
		if _, dup := seen[c.ID]; dup {
			return fmt.Errorf("%w: %q", ErrDuplicateCheckID, c.ID)
		}
		seen[c.ID] = struct{}{}

		checkBytes, err := MarshalCheck(c)
		if err != nil {
			return fmt.Errorf("converter: marshal check %q: %w", c.ID, err)
		}
		path := filepath.Join(checksDir, c.ID+".yaml")
		if err := os.WriteFile(path, checkBytes, 0o644); err != nil {
			return fmt.Errorf("converter: write check %q: %w", c.ID, err)
		}
	}
	return nil
}
