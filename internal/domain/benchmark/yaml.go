package benchmark

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// pack.yaml / checks/*.yaml의 와이어 형식 정의.
//
// 외부 입력이므로 strict mode(KnownFields=true)로 unknown 필드 거부 — schema drift 조기 발견.

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
	Description string `yaml:"description"`
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
	Description string `yaml:"description"`
	Severity    string `yaml:"severity"`
}

type checkSpecYAML struct {
	AuditCommand   string    `yaml:"auditCommand"`
	EvaluationRule yaml.Node `yaml:"evaluationRule"` // 임의 AST — Stage C에서 검증
	Rationale      string    `yaml:"rationale"`
	FixGuidance    string    `yaml:"fixGuidance"`
}

// ParsePackYAML은 pack.yaml 바이트를 Pack 메타로 파싱합니다 (체크 미포함).
//
// strict mode (KnownFields=true) — unknown 필드는 거부.
// apiVersion·kind 검증 + 필수 metadata 필드 검증.
func ParsePackYAML(data []byte) (Pack, error) {
	var doc packYAML
	if err := decodeStrict(data, &doc); err != nil {
		return Pack{}, fmt.Errorf("%w: %v", ErrInvalidYAML, err)
	}
	if doc.APIVersion != APIVersion {
		return Pack{}, fmt.Errorf("%w: %q", ErrUnknownAPIVersion, doc.APIVersion)
	}
	if doc.Kind != KindPack {
		return Pack{}, fmt.Errorf("%w: %q (want %q)", ErrUnknownKind, doc.Kind, KindPack)
	}
	if strings.TrimSpace(doc.Metadata.Name) == "" ||
		strings.TrimSpace(doc.Metadata.Version) == "" ||
		strings.TrimSpace(doc.Metadata.Vendor) == "" {
		return Pack{}, fmt.Errorf("%w: metadata.name/version/vendor required", ErrSchemaViolation)
	}

	return Pack{
		PackKey:       buildPackKey(doc.Metadata.Vendor, doc.Metadata.Name, doc.Metadata.Version),
		Name:          doc.Metadata.Name,
		Version:       doc.Metadata.Version,
		Vendor:        doc.Metadata.Vendor,
		Description:   doc.Metadata.Description,
		SchemaVersion: doc.Spec.SchemaVersion,
	}, nil
}

// ParseCheckYAML은 checks/<id>.yaml 바이트를 Check로 파싱합니다.
func ParseCheckYAML(data []byte) (Check, error) {
	var doc checkYAML
	if err := decodeStrict(data, &doc); err != nil {
		return Check{}, fmt.Errorf("%w: %v", ErrInvalidYAML, err)
	}
	if doc.APIVersion != APIVersion {
		return Check{}, fmt.Errorf("%w: %q", ErrUnknownAPIVersion, doc.APIVersion)
	}
	if doc.Kind != KindCheck {
		return Check{}, fmt.Errorf("%w: %q (want %q)", ErrUnknownKind, doc.Kind, KindCheck)
	}
	if strings.TrimSpace(doc.Metadata.ID) == "" || strings.TrimSpace(doc.Metadata.Title) == "" {
		return Check{}, fmt.Errorf("%w: metadata.id/title required", ErrSchemaViolation)
	}
	sev := Severity(strings.ToLower(strings.TrimSpace(doc.Metadata.Severity)))
	if sev == "" {
		sev = SeverityMedium
	}
	if !validSeverity(sev) {
		return Check{}, fmt.Errorf("%w: %q", ErrInvalidSeverity, doc.Metadata.Severity)
	}

	// EvaluationRule을 JSON으로 직렬화해 저장 — 외부 도구가 JSON 도구로 읽을 수 있게 통일.
	// Stage C의 AST 검증은 별도.
	var rule json.RawMessage
	if doc.Spec.EvaluationRule.Kind != 0 {
		ruleBytes, err := yamlNodeToJSON(doc.Spec.EvaluationRule)
		if err != nil {
			return Check{}, fmt.Errorf("%w: evaluationRule conversion: %v", ErrInvalidYAML, err)
		}
		rule = ruleBytes
	}

	return Check{
		CheckID:        doc.Metadata.ID,
		Title:          doc.Metadata.Title,
		Description:    doc.Metadata.Description,
		Severity:       sev,
		AuditCommand:   doc.Spec.AuditCommand,
		EvaluationRule: rule,
		Rationale:      doc.Spec.Rationale,
		FixGuidance:    doc.Spec.FixGuidance,
	}, nil
}

func decodeStrict(data []byte, dst any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // unknown 필드 거부 — schema drift 조기 발견(리서치 함정)
	return dec.Decode(dst)
}

func buildPackKey(vendor, name, version string) string {
	return fmt.Sprintf("%s-%s-%s",
		strings.ToLower(vendor),
		strings.ToLower(name),
		version)
}

func validSeverity(s Severity) bool {
	switch s {
	case SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return true
	}
	return false
}

// yamlNodeToJSON은 yaml.Node를 통해 임의의 evaluation rule AST를 JSON으로 변환합니다.
// yaml.Node를 map[string]any로 디코드 후 json.Marshal — JSON 호환 형식으로 통일.
func yamlNodeToJSON(node yaml.Node) ([]byte, error) {
	var generic any
	if err := node.Decode(&generic); err != nil {
		return nil, err
	}
	// YAML map은 map[string]any 또는 map[any]any가 될 수 있음 — JSON-compatible하게 정규화.
	normalized := normalizeYAMLValue(generic)
	return json.Marshal(normalized)
}

// normalizeYAMLValue는 YAML 디코드 결과를 JSON-compatible 타입으로 변환합니다.
// map[any]any → map[string]any, []any 재귀.
func normalizeYAMLValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalizeYAMLValue(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprintf("%v", k)] = normalizeYAMLValue(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalizeYAMLValue(val)
		}
		return out
	default:
		return x
	}
}
