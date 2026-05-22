package otel

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Resource attribute keys (OpenTelemetry semantic conventions 1.x).
//
// semconv 패키지를 직접 import 하지 않는 이유: semconv major 버전이 SDK 와 따로
// 움직여 transitive drift 가 발생할 수 있음. 본 stage 는 attribute key 4 종만
// 사용 — string literal 로 직접 정의해 안정성 우선.
const (
	attrServiceName     = "service.name"
	attrServiceVersion  = "service.version"
	attrDeploymentEnv   = "deployment.environment"
	attrRosshieldRegion = "rosshield.region"
)

// newResource 는 service.name + version + environment + region 등 공통
// resource attribute 를 묶어 OTel SDK resource.Resource 로 반환합니다.
//
// service.name 은 반드시 포함 (OpenTelemetry spec 필수). 나머지 attribute 는
// 값이 빈 문자열이면 부착하지 않음 — 운영자가 명시적으로 제공한 정보만 라벨화.
//
// resource.WithFromEnv() 도 함께 호출 — OTEL_RESOURCE_ATTRIBUTES 등 표준
// env var 가 추가 attribute 를 inject 할 수 있게 허용.
func newResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		attribute.String(attrServiceName, serviceNameOrDefault(cfg.ServiceName)),
	}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, attribute.String(attrServiceVersion, cfg.ServiceVersion))
	}
	if cfg.Environment != "" {
		attrs = append(attrs, attribute.String(attrDeploymentEnv, cfg.Environment))
	}
	if cfg.Region != "" {
		attrs = append(attrs, attribute.String(attrRosshieldRegion, cfg.Region))
	}

	return resource.New(ctx,
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
		resource.WithProcessPID(),
		resource.WithHost(),
	)
}

func serviceNameOrDefault(name string) string {
	if name == "" {
		return DefaultServiceName
	}
	return name
}
