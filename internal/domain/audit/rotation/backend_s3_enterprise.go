//go:build rosshield_enterprise

// backend_s3_enterprise.go — AWS SDK v2 기반 S3 backend 실 구현 (BSL 1.1 enterprise).
//
// 본 파일은 build tag `rosshield_enterprise`가 켜진 빌드에서만 컴파일됩니다.
// 자세한 라이선스 조건은 리포 루트의 `LICENSE-ENTERPRISE` 참조.
//
// 라이선스 분리 배경 (D-AR-9, 2026-05-19):
//
//	코어 (Apache-2.0) — file backend (`backend_file.go`) + rotation 도메인 로직.
//	enterprise (BSL 1.1) — S3 backend (본 파일) + cosign keyless 등.
//	airgap profile 운영은 file backend로 무료 동작 — 라이선스 정합 + 제약 0.
//
// 설계 참조:
//   - docs/design/notes/audit-chain-rotation-design.md §D-AR-5 / §D-AR-9
//   - AWS SDK Go v2 — https://aws.github.io/aws-sdk-go-v2/docs/
//   - S3 호환 storage (MinIO·Wasabi·Backblaze B2) — `ForcePathStyle=true` + `EndpointURL` 설정.
//
// credential 로딩:
//
//	SDK default chain (env AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY → ~/.aws/credentials →
//	IAM Role for Service Accounts → EC2 instance profile)에 위임.
//	본 모듈은 명시 credential 입력을 받지 않음 — 운영 환경의 12-factor 일관성.

package rotation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// s3API는 S3Backend가 호출하는 AWS SDK v2 s3.Client의 최소 표면입니다.
//
// 본 interface로 협소하게 표현 — 테스트 시 fake S3 client 단순화 (minio container 없이 단위 검증 가능).
// 메서드 시그니처는 AWS SDK v2 s3.Client와 정확히 일치합니다 — 그래서 *s3.Client가
// 자동 만족합니다.
type s3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	PutBucketLifecycleConfiguration(ctx context.Context, params *s3.PutBucketLifecycleConfigurationInput, optFns ...func(*s3.Options)) (*s3.PutBucketLifecycleConfigurationOutput, error)
}

// S3Backend는 AWS S3 또는 S3 호환 storage를 cold archive backend로 사용합니다.
//
// URI 형식: s3://<bucket>/<prefix><key>
// 예: s3://acme-audit-archives/tn_acme/seg-000001.tar.gz
//
// 동시성: AWS SDK v2 s3.Client는 goroutine-safe — Put 동시 호출 안전.
// 같은 key 재 Put은 idempotent (S3 객체 덮어쓰기 — versioning 활성 시 새 version).
type S3Backend struct {
	client s3API
	cfg    S3Config
}

// NewS3Backend는 S3Config를 기반으로 S3Backend를 생성합니다.
//
// 실패 조건:
//   - cfg.Bucket 또는 cfg.Region 빈 값 → error
//   - SDK default config 로딩 실패 (예: 잘못된 ~/.aws/credentials 형식) → error
//
// 본 함수는 S3 API 호출을 수행하지 않습니다 — 실 PUT/GET 시점에 권한·네트워크 검증.
func NewS3Backend(ctx context.Context, cfg S3Config) (*S3Backend, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("rotation: S3Backend: cfg.Bucket required")
	}
	if cfg.Region == "" {
		return nil, errors.New("rotation: S3Backend: cfg.Region required")
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("rotation: S3Backend: load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.EndpointURL != "" {
			o.BaseEndpoint = aws.String(cfg.EndpointURL)
		}
		if cfg.ForcePathStyle {
			o.UsePathStyle = true
		}
	})

	b := &S3Backend{client: client, cfg: cfg}
	if cfg.LifecycleEnabled {
		if err := b.ApplyLifecyclePolicy(ctx); err != nil && !errors.Is(err, ErrLifecycleEmpty) {
			return nil, fmt.Errorf("rotation: S3Backend: apply lifecycle: %w", err)
		}
	}
	return b, nil
}

// newS3BackendWithClient은 fake/mock client를 주입할 수 있는 test-only 생성자입니다.
//
// 본 함수는 단위 테스트(`backend_s3_enterprise_test.go`)에서만 사용 — production 경로는
// NewS3Backend가 진입점.
func newS3BackendWithClient(client s3API, cfg S3Config) *S3Backend {
	return &S3Backend{client: client, cfg: cfg}
}

// Scheme는 "s3"를 반환합니다.
func (b *S3Backend) Scheme() string { return s3Scheme }

// Put은 cfg.Prefix + key 경로에 data를 PUT하고 s3:// URI를 반환합니다.
//
// key는 상대 경로 (예: "tn_acme/seg-000001.tar.gz"). 절대 경로·상위 escape ("../") 거부.
// SSE 설정 (cfg.ServerSideEncryption) 비어 있지 않으면 PUT 요청에 포함.
func (b *S3Backend) Put(ctx context.Context, key string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateRelKey(key); err != nil {
		return "", err
	}

	fullKey := b.cfg.Prefix + key
	input := &s3.PutObjectInput{
		Bucket: aws.String(b.cfg.Bucket),
		Key:    aws.String(fullKey),
		Body:   bytes.NewReader(data),
	}
	if b.cfg.ServerSideEncryption != "" {
		input.ServerSideEncryption = s3types.ServerSideEncryption(b.cfg.ServerSideEncryption)
		if b.cfg.KMSKeyID != "" {
			input.SSEKMSKeyId = aws.String(b.cfg.KMSKeyID)
		}
	}

	if _, err := b.client.PutObject(ctx, input); err != nil {
		return "", fmt.Errorf("rotation: S3Backend Put %s: %w", fullKey, err)
	}

	return s3Scheme + "://" + b.cfg.Bucket + "/" + fullKey, nil
}

// Get은 s3:// URI에서 객체 본문을 반환합니다. 없으면 ErrNotExist.
//
// URI bucket이 cfg.Bucket과 다르면 error — 본 backend가 다른 bucket을 읽지 못하도록
// 강제 (multi-bucket 운영은 별 S3Backend 인스턴스 필요).
func (b *S3Backend) Get(ctx context.Context, uri string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bucket, key, err := parseS3URI(uri)
	if err != nil {
		return nil, err
	}
	if bucket != b.cfg.Bucket {
		return nil, fmt.Errorf("rotation: S3Backend Get: uri bucket %q != cfg bucket %q", bucket, b.cfg.Bucket)
	}

	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return nil, ErrNotExist
		}
		return nil, fmt.Errorf("rotation: S3Backend Get %s/%s: %w", bucket, key, err)
	}
	defer func() { _ = out.Body.Close() }()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("rotation: S3Backend Get read body: %w", err)
	}
	return data, nil
}

// ApplyLifecyclePolicy는 S3 bucket에 lifecycle configuration을 PUT합니다.
//
// Rule ID = "rosshield-rotation" (고정) — 운영자가 이름으로 식별·삭제 가능.
// Filter Prefix = cfg.Prefix → 다른 application의 객체에 영향 없음.
// transitions/expire 모두 비어 있으면 ErrLifecycleEmpty (호출자 가드).
//
// 재적용은 idempotent — PutBucketLifecycleConfiguration은 기존 config를 덮어씁니다.
// 운영 중 transition 일수 변경은 본 메서드 재호출로 즉시 반영.
//
// MinIO·일부 S3 호환 storage는 GLACIER·DEEP_ARCHIVE transition을 silent ignore — error
// 반환 안 함. 호환성 보장 측면에서 본 함수도 silent OK (storage 측 정책 위임).
func (b *S3Backend) ApplyLifecyclePolicy(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(b.cfg.LifecycleTransitions) == 0 && b.cfg.LifecycleExpireDays <= 0 {
		return ErrLifecycleEmpty
	}

	rule := s3types.LifecycleRule{
		ID:     aws.String("rosshield-rotation"),
		Status: s3types.ExpirationStatusEnabled,
		Filter: &s3types.LifecycleRuleFilter{Prefix: aws.String(b.cfg.Prefix)},
	}
	for _, tr := range b.cfg.LifecycleTransitions {
		rule.Transitions = append(rule.Transitions, s3types.Transition{
			Days:         aws.Int32(tr.Days),
			StorageClass: s3types.TransitionStorageClass(tr.StorageClass),
		})
	}
	if b.cfg.LifecycleExpireDays > 0 {
		rule.Expiration = &s3types.LifecycleExpiration{
			Days: aws.Int32(b.cfg.LifecycleExpireDays),
		}
	}

	// MinIO 등 S3 호환 storage는 PutBucketLifecycleConfiguration에 Content-MD5 헤더를
	// 강제 요구합니다. AWS SDK v2는 ChecksumAlgorithm을 명시하면 SHA256/CRC32 등을
	// 자동 계산해 헤더에 채워줍니다 — AWS 본가는 양쪽 모두 허용.
	_, err := b.client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket:            aws.String(b.cfg.Bucket),
		ChecksumAlgorithm: s3types.ChecksumAlgorithmSha256,
		LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{
			Rules: []s3types.LifecycleRule{rule},
		},
	})
	if err != nil {
		return fmt.Errorf("rotation: S3Backend ApplyLifecyclePolicy: %w", err)
	}
	return nil
}

// Exists는 s3:// URI 객체 존재 여부를 반환합니다 (HEAD 요청 — body 다운로드 없음).
//
// 객체 없음은 (false, nil) — error는 권한·네트워크 등 backend 자체 실패 시만.
func (b *S3Backend) Exists(ctx context.Context, uri string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	bucket, key, err := parseS3URI(uri)
	if err != nil {
		return false, err
	}
	if bucket != b.cfg.Bucket {
		return false, fmt.Errorf("rotation: S3Backend Exists: uri bucket %q != cfg bucket %q", bucket, b.cfg.Bucket)
	}

	_, err = b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("rotation: S3Backend Exists %s/%s: %w", bucket, key, err)
	}
	return true, nil
}

// parseS3URI는 "s3://<bucket>/<key>" 형식 URI를 (bucket, key)로 분해합니다.
//
// 예:
//
//	"s3://acme-audit/tn_x/seg-001.tar.gz" → ("acme-audit", "tn_x/seg-001.tar.gz")
//	"s3://b/k"                            → ("b", "k")
func parseS3URI(uri string) (string, string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", fmt.Errorf("rotation: parse s3 uri %q: %w", uri, err)
	}
	if u.Scheme != s3Scheme {
		return "", "", fmt.Errorf("rotation: S3Backend got scheme %q, want %q", u.Scheme, s3Scheme)
	}
	if u.Host == "" {
		return "", "", fmt.Errorf("rotation: s3 uri %q missing bucket", uri)
	}
	key := strings.TrimPrefix(u.Path, "/")
	if key == "" {
		return "", "", fmt.Errorf("rotation: s3 uri %q missing key", uri)
	}
	return u.Host, key, nil
}

// isS3NotFound는 GetObject·HeadObject error가 객체 부재(NoSuchKey / NotFound 404)인지 판정합니다.
//
// AWS SDK v2는 GetObject NoSuchKey와 HeadObject NotFound를 다른 타입으로 분리하므로 둘 다 검사.
func isS3NotFound(err error) bool {
	var nsk *s3types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *s3types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	return false
}
