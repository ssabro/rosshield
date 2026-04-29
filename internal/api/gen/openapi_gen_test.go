// Package gen 생성 코드의 sanity 테스트.
//
// 본 테스트는 oapi-codegen 산출물이 결정론적으로 생성되었는지·
// 핵심 표면(types·ServerInterface·embedded spec)이 컴파일/런타임에서
// 올바르게 노출되는지를 검증합니다. 핸들러 동작 자체는 검증하지 않습니다
// (Step 0.3-β 범위 밖).
package gen

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestEmbeddedSpecIsLoadable는 GetSwagger()가 base64+gzip 디코딩 후
// kin-openapi loader로 파싱 가능한 spec을 반환하는지 확인합니다.
func TestEmbeddedSpecIsLoadable(t *testing.T) {
	t.Parallel()

	swagger, err := GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger() returned error: %v", err)
	}
	if swagger == nil {
		t.Fatal("GetSwagger() returned nil swagger")
	}
	if swagger.Info == nil {
		t.Fatal("swagger.Info is nil — spec did not deserialize correctly")
	}
	if swagger.Info.Title != "rosshield API" {
		t.Errorf("swagger.Info.Title = %q, want %q", swagger.Info.Title, "rosshield API")
	}
	if swagger.Info.Version == "" {
		t.Error("swagger.Info.Version is empty")
	}
	if swagger.Paths == nil || swagger.Paths.Len() == 0 {
		t.Error("swagger.Paths is empty — at least one operation expected")
	}
}

// TestModelTypesGenerated는 핵심 schema가 Go type으로 생성되었는지
// compile-time + zero-value check로 검증합니다.
func TestModelTypesGenerated(t *testing.T) {
	t.Parallel()

	// HealthStatus 및 enum 상수
	hs := HealthStatus{Status: Ok}
	if hs.Status != "ok" {
		t.Errorf("HealthStatus.Status = %q, want %q", hs.Status, "ok")
	}

	// ErrorEnvelope, Error, ErrorCategory
	env := ErrorEnvelope{
		Ok: false,
		Error: Error{
			Code:     "test.code",
			Category: Validation,
			Message:  "test message",
		},
	}
	if env.Error.Category != "validation" {
		t.Errorf("ErrorCategory Validation = %q, want %q", env.Error.Category, "validation")
	}

	// Envelope (success wrapper)
	var ok Envelope
	ok.Ok = true
	if !ok.Ok {
		t.Error("Envelope.Ok zero-set failed")
	}

	// Meta
	var m Meta
	m.RequestId = "req-123"
	if m.RequestId != "req-123" {
		t.Errorf("Meta.RequestId = %q, want %q", m.RequestId, "req-123")
	}
}

// nilHandler는 ServerInterface 컴파일 만족 검증용 stub입니다.
// Unimplemented는 이미 인터페이스를 만족하므로 임베딩으로 위임합니다.
type nilHandler struct {
	Unimplemented
}

// 컴파일 시점 인터페이스 만족 확인.
var _ ServerInterface = (*nilHandler)(nil)

// TestChiServerInterfaceGenerated는 ServerInterface가 정의되어 있고
// 모든 operationId가 메서드로 노출됐는지 확인합니다.
// (var _ assertion이 컴파일을 통과하면 핵심 메서드 셋이 갖춰진 것)
func TestChiServerInterfaceGenerated(t *testing.T) {
	t.Parallel()

	// Handler 함수가 ServerInterface 구현체를 받아 http.Handler를 반환해야 함.
	h := Handler(&nilHandler{})
	if h == nil {
		t.Fatal("Handler(&nilHandler{}) returned nil")
	}
	if _, ok := h.(http.Handler); !ok {
		t.Error("Handler return value does not satisfy http.Handler")
	}

	// HandlerFromMux 함수도 동일한 시그니처로 노출되는지 확인 (컴파일 시 보장,
	// 런타임에서도 chi.Router를 받아 http.Handler를 반환).
	router := chi.NewRouter()
	hMux := HandlerFromMux(&nilHandler{}, router)
	if hMux == nil {
		t.Error("HandlerFromMux returned nil")
	}
}

// TestServerInterfaceCoversAllOperations는 spec의 operationId 13개가
// 모두 ServerInterface 메서드로 매핑됐는지 인터페이스 어설션을 통한
// 간접 보장을 합니다 (직접 호출은 Unimplemented가 501을 반환).
func TestServerInterfaceCoversAllOperations(t *testing.T) {
	t.Parallel()

	var s ServerInterface = Unimplemented{}

	// nil이 아닌 인터페이스를 갖고 있으면 컴파일 시점 모든 메서드 셋이 통과한 것.
	if s == nil {
		t.Fatal("Unimplemented does not satisfy ServerInterface")
	}
}
