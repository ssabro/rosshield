package benchmark

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// Pack/Check JSON Schema (draft 2020-12).
//
// YAML 파싱 후 normalize된 map을 검증 — 외부 라이브러리(`santhosh-tekuri/jsonschema/v6`)는
// `map[string]any` 형태를 받음 (Go struct 직접 X — 리서치 함정).
//
// schema는 strict — 알려진 필드만, type/패턴 강제.

const packSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["apiVersion", "kind", "metadata", "spec"],
  "additionalProperties": false,
  "properties": {
    "apiVersion": { "type": "string", "const": "rosshield.io/v1" },
    "kind": { "type": "string", "const": "Pack" },
    "metadata": {
      "type": "object",
      "required": ["name", "version", "vendor"],
      "additionalProperties": false,
      "properties": {
        "name": { "type": "string", "minLength": 1, "pattern": "^[a-z0-9][a-z0-9-]*$" },
        "version": { "type": "string", "minLength": 1, "pattern": "^v?[0-9]+\\.[0-9]+\\.[0-9]+(-[A-Za-z0-9.-]+)?$" },
        "vendor": { "type": "string", "minLength": 1 },
        "description": { "type": "string" }
      }
    },
    "spec": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "schemaVersion": { "type": "integer", "minimum": 1 }
      }
    }
  }
}`

const checkSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["apiVersion", "kind", "metadata", "spec"],
  "additionalProperties": false,
  "properties": {
    "apiVersion": { "type": "string", "const": "rosshield.io/v1" },
    "kind": { "type": "string", "const": "Check" },
    "metadata": {
      "type": "object",
      "required": ["id", "title"],
      "additionalProperties": false,
      "properties": {
        "id": { "type": "string", "minLength": 1, "pattern": "^[A-Za-z0-9][A-Za-z0-9.\\-_]*$" },
        "title": { "type": "string", "minLength": 1 },
        "description": { "type": "string" },
        "severity": { "type": "string", "enum": ["low", "medium", "high", "critical"] }
      }
    },
    "spec": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "auditCommand": { "type": "string" },
        "evaluationRule": {},
        "rationale": { "type": "string" },
        "fixGuidance": { "type": "string" }
      }
    }
  }
}`

var (
	schemaOnce  sync.Once
	packSchema  *jsonschema.Schema
	checkSchema *jsonschema.Schema
	schemaErr   error
)

func compileSchemas() error {
	schemaOnce.Do(func() {
		c := jsonschema.NewCompiler()
		var packDoc, checkDoc any
		if err := json.Unmarshal([]byte(packSchemaJSON), &packDoc); err != nil {
			schemaErr = fmt.Errorf("benchmark: pack schema unmarshal: %w", err)
			return
		}
		if err := json.Unmarshal([]byte(checkSchemaJSON), &checkDoc); err != nil {
			schemaErr = fmt.Errorf("benchmark: check schema unmarshal: %w", err)
			return
		}
		if err := c.AddResource("pack.schema.json", packDoc); err != nil {
			schemaErr = fmt.Errorf("benchmark: pack schema add: %w", err)
			return
		}
		if err := c.AddResource("check.schema.json", checkDoc); err != nil {
			schemaErr = fmt.Errorf("benchmark: check schema add: %w", err)
			return
		}
		ps, err := c.Compile("pack.schema.json")
		if err != nil {
			schemaErr = fmt.Errorf("benchmark: pack schema compile: %w", err)
			return
		}
		cs, err := c.Compile("check.schema.json")
		if err != nil {
			schemaErr = fmt.Errorf("benchmark: check schema compile: %w", err)
			return
		}
		packSchema = ps
		checkSchema = cs
	})
	return schemaErr
}

// ValidatePackInstance는 YAML로부터 디코드된 generic Go value(map[string]any 등)를 schema로 검증합니다.
func ValidatePackInstance(instance any) error {
	if err := compileSchemas(); err != nil {
		return err
	}
	if err := packSchema.Validate(instance); err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaViolation, err)
	}
	return nil
}

// ValidateCheckInstance는 check YAML 디코드 결과를 schema로 검증합니다.
func ValidateCheckInstance(instance any) error {
	if err := compileSchemas(); err != nil {
		return err
	}
	if err := checkSchema.Validate(instance); err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaViolation, err)
	}
	return nil
}

// ValidatePackYAMLBytes는 YAML 바이트를 디코드 + schema 검증을 한 번에 수행합니다.
//
// strict YAML decode (KnownFields=true)로 schema 외 필드를 거부하고,
// 그 다음 jsonschema로 type·pattern·required 검증.
func ValidatePackYAMLBytes(data []byte) error {
	instance, err := decodeYAMLToGeneric(data)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidYAML, err)
	}
	return ValidatePackInstance(instance)
}

// ValidateCheckYAMLBytes는 check YAML 바이트를 디코드 + schema 검증.
func ValidateCheckYAMLBytes(data []byte) error {
	instance, err := decodeYAMLToGeneric(data)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidYAML, err)
	}
	return ValidateCheckInstance(instance)
}

// decodeYAMLToGeneric은 YAML을 schema 검증용 generic value로 디코드합니다.
// jsonschema는 map[string]any를 요구하므로 normalizeYAMLValue로 정규화.
func decodeYAMLToGeneric(data []byte) (any, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	// non-strict — schema가 additionalProperties:false로 unknown 필드 차단
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return normalizeYAMLValue(raw), nil
}
