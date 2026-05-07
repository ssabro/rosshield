// siem.go — webhook payload를 SIEM 호환 형식(CEF·ECS)으로 변환합니다.
//
// 외부 dep 0 — stdlib `encoding/json`만 사용. SIEM 라이브러리 도입 X.
//
// CEF (Common Event Format): ArcSight·Splunk OOTB 호환. 단일 라인 텍스트.
//
//	포맷: CEF:0|<vendor>|<product>|<version>|<signatureID>|<name>|<severity>|<extension>
//
// ECS (Elastic Common Schema): Elastic Stack 호환. JSON.
//
//	포맷: { "@timestamp": ..., "event.dataset": ..., "event.severity": ..., ... }

package webhook

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SIEM 표준 메타.
const (
	cefVersion    = "0"
	cefVendor     = "rosshield"
	cefProduct    = "rosshield"
	cefAppVersion = "1.0"
	ecsVersion    = "8.11" // ECS 호환 버전 — Elastic 8.x.
)

// FormatCEF는 DomainEvent를 CEF 라인 1건으로 직렬화합니다.
//
// 결과는 Splunk·ArcSight·QRadar OOTB 파서가 인식 가능합니다 (E23.T4).
//
// 형식:
//
//	CEF:0|rosshield|rosshield|1.0|<event.type>|<event.type>|<severity>|tenant=<id> aggregate=<type>:<id> event_id=<id> rt=<ms>
//
// CEF severity: 0~10 (info=2, low=3, medium=5, high=7, critical=10).
// extension은 key=value 공백 구분, 값에 공백/특수문자가 있으면 escape.
func FormatCEF(evt DomainEvent) string {
	signatureID := string(evt.Type)
	name := string(evt.Type)
	sev := mapSeverityToCEF(evt.Severity)

	// extension key=value (RFC 호환 escape).
	pairs := []string{
		"tenant=" + cefEscape(string(evt.TenantID)),
		"event_id=" + cefEscape(evt.EventID),
		"rt=" + fmt.Sprintf("%d", evt.OccurredAt.UnixMilli()),
	}
	if evt.AggregateType != "" {
		pairs = append(pairs, "aggregate_type="+cefEscape(evt.AggregateType))
	}
	if evt.AggregateID != "" {
		pairs = append(pairs, "aggregate_id="+cefEscape(evt.AggregateID))
	}
	extension := strings.Join(pairs, " ")

	return fmt.Sprintf("CEF:%s|%s|%s|%s|%s|%s|%d|%s",
		cefVersion,
		cefEscape(cefVendor),
		cefEscape(cefProduct),
		cefEscape(cefAppVersion),
		cefEscape(signatureID),
		cefEscape(name),
		sev,
		extension,
	)
}

// FormatECS는 DomainEvent를 ECS-호환 JSON map으로 변환합니다.
//
// Elastic Stack(8.x ECS) ingest pipeline OOTB 호환. 호출자가 json.Marshal로 직렬화.
//
// 핵심 필드:
//
//	@timestamp        : RFC3339 OccurredAt (UTC)
//	event.dataset     : "rosshield.<event.type>"
//	event.kind        : "event"
//	event.action      : event.type 그대로
//	event.severity    : 0~3 (info/low/medium/high·critical)
//	event.id          : DomainEvent.EventID
//	organization.id   : TenantID
//	rosshield.payload : 원천 도메인 payload raw JSON (decoded)
func FormatECS(evt DomainEvent) map[string]any {
	out := map[string]any{
		"@timestamp":  evt.OccurredAt.UTC().Format(time.RFC3339Nano),
		"ecs.version": ecsVersion,
		"event": map[string]any{
			"dataset":  "rosshield." + string(evt.Type),
			"kind":     "event",
			"action":   string(evt.Type),
			"severity": mapSeverityToECS(evt.Severity),
			"id":       evt.EventID,
			"category": []string{"configuration"},
			"type":     []string{"info"},
		},
		"organization": map[string]any{
			"id": string(evt.TenantID),
		},
		"observer": map[string]any{
			"vendor":  cefVendor,
			"product": cefProduct,
			"version": cefAppVersion,
		},
	}
	if evt.AggregateType != "" || evt.AggregateID != "" {
		out["rosshield"] = map[string]any{
			"aggregate_type": evt.AggregateType,
			"aggregate_id":   evt.AggregateID,
		}
	}
	// 원천 payload가 valid JSON이면 nested object로, 아니면 raw string으로.
	if len(evt.Payload) > 0 {
		var nested any
		if err := json.Unmarshal(evt.Payload, &nested); err == nil {
			ros, _ := out["rosshield"].(map[string]any)
			if ros == nil {
				ros = map[string]any{}
				out["rosshield"] = ros
			}
			ros["payload"] = nested
		} else {
			ros, _ := out["rosshield"].(map[string]any)
			if ros == nil {
				ros = map[string]any{}
				out["rosshield"] = ros
			}
			ros["payload_raw"] = string(evt.Payload)
		}
	}
	return out
}

// mapSeverityToCEF는 도메인 severity 문자열을 CEF 0~10 정수로 매핑합니다.
//
// CEF severity 권장:
//   - 0~3: info
//   - 4~6: warning
//   - 7~8: error
//   - 9~10: critical
func mapSeverityToCEF(s string) int {
	switch strings.ToLower(s) {
	case "info":
		return 2
	case "low":
		return 3
	case "medium", "warning":
		return 5
	case "high", "error":
		return 7
	case "critical":
		return 10
	default:
		return 5 // 기본 medium.
	}
}

// mapSeverityToECS는 도메인 severity 문자열을 ECS event.severity 0~3 정수로 매핑합니다.
//
// ECS event.severity는 numeric — 0(low)~3(critical) 권장.
func mapSeverityToECS(s string) int {
	switch strings.ToLower(s) {
	case "info":
		return 0
	case "low":
		return 1
	case "medium", "warning":
		return 2
	case "high", "error", "critical":
		return 3
	default:
		return 2
	}
}

// cefEscape는 CEF header/extension 값에 포함된 특수문자를 escape합니다.
//
// CEF spec: backslash, pipe, equals 는 backslash-prefix.
func cefEscape(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`|`, `\|`,
		`=`, `\=`,
		"\n", `\n`,
		"\r", `\r`,
	)
	return r.Replace(s)
}
