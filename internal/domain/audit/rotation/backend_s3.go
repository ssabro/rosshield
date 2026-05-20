package rotation

import "errors"

// 본 파일은 build tag 무관 — S3Backend 공통 선언 (config struct + scheme 상수 + sentinel error)을 둡니다.
// 실 구현은 build tag별 분기:
//
//   - `backend_s3_stub.go`        (`//go:build !rosshield_enterprise`): NewS3Backend가 ErrS3BackendNotAvailable.
//   - `backend_s3_enterprise.go`  (`//go:build rosshield_enterprise`):  AWS SDK v2 실 구현 (BSL 1.1 enterprise).
//
// open-core 정합 (D-AR-9, 2026-05-19):
//
//	코어 (Apache-2.0) — file backend + rotation 도메인 로직.
//	enterprise (BSL 1.1) — S3 backend 실 구현. airgap profile은 file backend로 무료 동작.
//
// 자세한 라이선스 조건은 리포 루트의 `LICENSE-ENTERPRISE` 참조.

// ErrS3BackendNotAvailable은 build tag `rosshield_enterprise` 없이 S3 backend를 호출할 때 반환됩니다.
//
// caller는 errors.Is로 본 sentinel을 판정해 file backend로 fallback할 수 있습니다.
var ErrS3BackendNotAvailable = errors.New(
	"rotation: S3 backend not available in this build (BSL 1.1 enterprise build tag `rosshield_enterprise` required)")

// s3Scheme은 S3 backend URI 식별자입니다.
const s3Scheme = "s3"

// S3Config는 S3 backend 구성입니다.
//
// 본 struct는 build tag 무관 — caller가 단일 struct literal로 양쪽 빌드에서 사용 가능.
// stub 빌드에서는 모든 필드가 무시됩니다.
//
// credential은 AWS SDK default chain (env, ~/.aws/credentials, IRSA, EC2 instance profile)에
// 위임 — 본 struct는 식별자만. 명시 credential 입력 미지원 (12-factor 일관).
type S3Config struct {
	// Region은 AWS region (예: "us-east-1"). 필수.
	Region string
	// Bucket은 archive 저장 bucket. 필수.
	Bucket string
	// Prefix는 bucket 안의 key prefix (예: "audit-archives/tn_acme/"). 옵션.
	// 비어 있으면 bucket root에 저장.
	Prefix string
	// EndpointURL은 S3 호환 endpoint (MinIO·Wasabi·Backblaze B2 등). 비어 있으면 AWS 기본.
	EndpointURL string
	// ForcePathStyle은 S3 호환 storage에서 path-style addressing 강제.
	// MinIO·일부 self-hosted gateway 환경에서 true 필요.
	// AWS 기본 endpoint는 false 권장 (virtual-hosted-style).
	ForcePathStyle bool
	// ServerSideEncryption은 SSE 모드 ("AES256" 또는 "aws:kms"). 비어 있으면 SSE 미사용.
	ServerSideEncryption string
	// KMSKeyID는 SSE-KMS 사용 시 CMK ARN/ID. ServerSideEncryption="aws:kms"일 때만 유효.
	KMSKeyID string
	// LifecycleEnabled가 true이면 NewS3Backend·bootstrap에서 ApplyLifecyclePolicy를 자동 호출합니다.
	// false이면 lifecycle 미적용 (호출자가 별도로 ApplyLifecyclePolicy 호출 가능).
	LifecycleEnabled bool
	// LifecycleTransitions는 storage class 단계별 전환 규칙입니다 (S3 표준).
	// 빈 슬라이스 + LifecycleExpireDays=0 + LifecycleEnabled=true → ApplyLifecyclePolicy
	// 는 빈 rule을 만들지 않고 ErrLifecycleEmpty를 반환.
	// 일반 audit retention: [{Days: 365, Class: "STANDARD_IA"}, {Days: 1825, Class: "GLACIER"}].
	LifecycleTransitions []S3Transition
	// LifecycleExpireDays는 object 삭제까지 일수. 0이면 만료 없음(영구 보존).
	// 일부 customer는 컴플라이언스 요구로 N년 후 자동 삭제 필요.
	LifecycleExpireDays int32
}

// S3Transition은 lifecycle storage class 전환 규칙입니다.
//
// AWS S3 lifecycle Days 의미: object creation 후 N일 (transition 후 N일이 아님).
// StorageClass 허용값 (AWS SDK enum 그대로): "STANDARD_IA", "ONEZONE_IA",
// "INTELLIGENT_TIERING", "GLACIER", "GLACIER_IR", "DEEP_ARCHIVE".
//
// MinIO 등 S3 호환 storage는 GLACIER·DEEP_ARCHIVE 미지원 — STANDARD_IA만 처리하거나 silent ignore.
type S3Transition struct {
	Days         int32
	StorageClass string
}

// ErrLifecycleEmpty는 lifecycle 적용 시 transition + expire 모두 비어 있을 때 반환.
var ErrLifecycleEmpty = errors.New(
	"rotation: S3 lifecycle empty (LifecycleTransitions and LifecycleExpireDays both empty — nothing to apply)")
