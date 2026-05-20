//go:build !rosshield_enterprise

package rotation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
)

// TestS3Backend_StubReturnsNotAvailable은 코어(Apache-2.0) 빌드에서 NewS3Backend가
// ErrS3BackendNotAvailable을 반환함을 검증합니다.
//
// enterprise 빌드는 본 테스트가 컴파일에서 제외되며, 대신
// `backend_s3_enterprise_test.go`가 실 S3 client 동작을 검증합니다.
func TestS3Backend_StubReturnsNotAvailable(t *testing.T) {
	t.Parallel()

	_, err := rotation.NewS3Backend(context.Background(), rotation.S3Config{Region: "us-east-1", Bucket: "x"})
	if !errors.Is(err, rotation.ErrS3BackendNotAvailable) {
		t.Errorf("NewS3Backend error = %v, want ErrS3BackendNotAvailable", err)
	}
}

// TestS3Backend_StubApplyLifecycleNotAvailable는 코어 빌드에서 ApplyLifecyclePolicy도
// 동일하게 ErrS3BackendNotAvailable을 반환함을 검증합니다.
func TestS3Backend_StubApplyLifecycleNotAvailable(t *testing.T) {
	t.Parallel()

	stub := &rotation.S3Backend{}
	err := stub.ApplyLifecyclePolicy(context.Background())
	if !errors.Is(err, rotation.ErrS3BackendNotAvailable) {
		t.Errorf("ApplyLifecyclePolicy error = %v, want ErrS3BackendNotAvailable", err)
	}
}
