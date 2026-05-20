//go:build !rosshield_enterprise

package rotation

import "context"

// 본 파일은 코어(Apache-2.0) 빌드에서만 컴파일됩니다 — enterprise 빌드는
// `backend_s3_enterprise.go`의 실 AWS SDK v2 구현이 대체합니다.
//
// stub은 모든 메서드가 ErrS3BackendNotAvailable을 반환 — caller (bootstrap)는
// errors.Is(err, ErrS3BackendNotAvailable)로 file backend로 graceful fallback합니다.

// S3Backend는 코어 빌드의 stub입니다 — 실 storage 호출 0.
type S3Backend struct{}

// NewS3Backend (stub)는 코어 build에서 항상 (nil, ErrS3BackendNotAvailable)을 반환합니다.
//
// ctx와 cfg는 무시됩니다 — enterprise 빌드와 동일 시그니처 유지를 위해 보존.
func NewS3Backend(_ context.Context, _ S3Config) (*S3Backend, error) {
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
