//go:build !enterprise

package rotation

import (
	"context"
	"errors"
)

// ErrS3BackendNotAvailable는 build tag `enterprise` 없이 S3 backend를 호출할 때 반환됩니다.
//
// open-core 정합 (D-AR-9): S3 backend는 BSL 1.1 enterprise.
// 본 파일은 stub — Apache-2.0 코어 빌드에서는 NewS3Backend가 stub error를 반환합니다.
// 실제 AWS SDK v2 통합은 `//go:build enterprise` 분기 + 별 epic에서 진행.
var ErrS3BackendNotAvailable = errors.New(
	"rotation: S3 backend not available in this build (BSL 1.1 enterprise build tag required)")

// s3Scheme은 S3 backend URI 식별자입니다.
const s3Scheme = "s3"

// S3Config는 S3 backend 구성 (enterprise build에서만 의미).
//
// 본 stub은 필드를 선언만 — caller가 동일 호출부를 유지할 수 있도록.
type S3Config struct {
	// Region은 AWS region (예: "us-east-1").
	Region string
	// Bucket은 archive 저장 bucket.
	Bucket string
	// Prefix는 bucket 안의 key prefix (예: "audit-archives/").
	Prefix string
	// EndpointURL은 S3 호환 endpoint (MinIO·Wasabi 등). 비어 있으면 AWS 기본.
	EndpointURL string
}

// S3Backend는 enterprise build에서 AWS SDK v2 s3.Client를 wrap합니다.
//
// 본 (코어) build에서는 stub — 모든 메서드가 ErrS3BackendNotAvailable.
// NewS3Backend도 동일 stub error.
type S3Backend struct{}

// NewS3Backend (stub)는 코어 build에서 항상 ErrS3BackendNotAvailable을 반환합니다.
func NewS3Backend(_ S3Config) (*S3Backend, error) {
	return nil, ErrS3BackendNotAvailable
}

// Scheme는 "s3"를 반환합니다.
func (*S3Backend) Scheme() string { return s3Scheme }

// Put (stub).
func (*S3Backend) Put(_ context.Context, _ string, _ []byte) (string, error) {
	return "", ErrS3BackendNotAvailable
}

// Get (stub).
func (*S3Backend) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, ErrS3BackendNotAvailable
}

// Exists (stub).
func (*S3Backend) Exists(_ context.Context, _ string) (bool, error) {
	return false, ErrS3BackendNotAvailable
}
