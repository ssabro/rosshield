//go:build rosshield_enterprise && integration

// backend_s3_minio_integration_test.go — MinIO testcontainer 통합 검증 (D-AR-9).
//
// 실행:
//
//	go test -tags="rosshield_enterprise integration" -count=1 -run MinIO \
//	    ./internal/domain/audit/rotation/
//
// 본 파일은 build tag `rosshield_enterprise && integration` 양쪽이 모두 켜져야 컴파일됩니다.
// docker daemon 부재 시 testcontainers-go가 즉시 fail — t.Skip 가드로 CI 외 환경 우회.
//
// 검증 항목 (v0.6.8 한계 carryover + v0.6.9 후속):
//
//   - MinIOPutGetRoundTrip: 실 S3 호환 endpoint에 PUT → GET round-trip + 본문 정확성
//   - MinIOExists: HEAD/Exists 동작
//   - MinIOLifecycle: ApplyLifecyclePolicy 통신 성공 (Content-MD5 middleware 검증)
//   - MinIONotFound: 부재 객체 → Exists=false, Get → ErrNotExist
//
// 본 테스트는 fake s3API mock(backend_s3_enterprise_test.go)을 보완 — fake가 잡지 못하는
// AWS SDK ↔ MinIO 실 wire 호환성 (Region/PathStyle/Auth signature + legacy Content-MD5)을
// 검증합니다.

package rotation_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
)

const (
	minioImage      = "minio/minio:RELEASE.2024-12-18T13-15-44Z"
	minioRootUser   = "minio_root"
	minioRootPass   = "minio_test_secret"
	minioTestBucket = "rosshield-audit-it"
)

// minioFixture는 minio container + 그 안에 미리 만든 bucket을 노출합니다.
type minioFixture struct {
	endpoint string
	bucket   string
}

func setupMinIO(t *testing.T) minioFixture {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        minioImage,
		ExposedPorts: []string{"9000/tcp"},
		Cmd:          []string{"server", "/data"},
		Env: map[string]string{
			"MINIO_ROOT_USER":     minioRootUser,
			"MINIO_ROOT_PASSWORD": minioRootPass,
		},
		WaitingFor: wait.ForLog("API:").WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Skipf("minio container start failed (docker unavailable?): %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "9000")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}
	endpoint := fmt.Sprintf("http://%s:%s", host, port.Port())

	// bucket 생성 — MinIO는 미존재 bucket에 PUT 시 NoSuchBucket. testcontainers fixture
	// 책임으로 우선 만든다 (s3.CreateBucket — backend interface 외).
	client, err := newRawMinIOClient(ctx, endpoint)
	if err != nil {
		t.Fatalf("raw client: %v", err)
	}
	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(minioTestBucket),
	})
	if err != nil && !strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") {
		t.Fatalf("CreateBucket: %v", err)
	}

	return minioFixture{
		endpoint: endpoint,
		bucket:   minioTestBucket,
	}
}

// newRawMinIOClient는 MinIO endpoint에 static credential로 직접 붙는 SDK client를 만듭니다.
// backend 외 setup(CreateBucket)에서만 사용 — 검증 본체는 rotation.S3Backend 경유.
func newRawMinIOClient(ctx context.Context, endpoint string) (*s3.Client, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(minioRootUser, minioRootPass, "")),
	)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	}), nil
}

// newMinIOBackend는 MinIO endpoint에 붙는 rotation.S3Backend를 만듭니다.
//
// 본 helper는 NewS3Backend가 SDK default credential chain(env/IRSA/instance profile)을 쓰는 것을
// 우회하기 위해 env를 직접 set한 뒤 정리합니다 — 테스트 격리.
func newMinIOBackend(t *testing.T, fix minioFixture, cfg rotation.S3Config) *rotation.S3Backend {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", minioRootUser)
	t.Setenv("AWS_SECRET_ACCESS_KEY", minioRootPass)

	cfg.Bucket = fix.bucket
	cfg.Region = "us-east-1"
	cfg.EndpointURL = fix.endpoint
	cfg.ForcePathStyle = true

	b, err := rotation.NewS3Backend(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewS3Backend: %v", err)
	}
	return b
}

// TestS3Backend_MinIOPutGetRoundTrip — 실 MinIO endpoint에 PUT/GET round-trip.
func TestS3Backend_MinIOPutGetRoundTrip(t *testing.T) {
	fix := setupMinIO(t)
	b := newMinIOBackend(t, fix, rotation.S3Config{Prefix: "tn_acme/"})

	payload := []byte("hello-minio-integration")
	uri, err := b.Put(context.Background(), "seg-000001.tar.gz", payload)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !strings.HasPrefix(uri, "s3://"+fix.bucket+"/") {
		t.Errorf("uri = %q, want s3://%s/ prefix", uri, fix.bucket)
	}

	got, err := b.Get(context.Background(), uri)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("Get returned %q, want %q", got, payload)
	}
}

// TestS3Backend_MinIOExists — Exists round-trip 동작.
func TestS3Backend_MinIOExists(t *testing.T) {
	fix := setupMinIO(t)
	cfg := rotation.S3Config{Prefix: "existence/"}
	b := newMinIOBackend(t, fix, cfg)

	uri, err := b.Put(context.Background(), "seg-000002.tar.gz", []byte("payload"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	exists, err := b.Exists(context.Background(), uri)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Error("Exists = false right after Put")
	}
}

// TestS3Backend_MinIOLifecycle — ApplyLifecyclePolicy 실 wire 검증.
//
// MinIO는 RFC 1864 Content-MD5 헤더를 strict 요구 — backend의 legacy MD5 middleware
// 가 자동으로 헤더를 채워줍니다. 본 테스트는 NewS3Backend의 LifecycleEnabled 자동 적용
// 경로 + 명시 재호출 idempotency 모두 검증.
//
// 신규 MinIO release(2024+)는 transition StorageClass를 strict validate — remote tier
// (mc admin tier add) 미등록 시 STANDARD_IA·GLACIER·DEEP_ARCHIVE 모두 400 InvalidStorageClass
// 거부. 본 테스트는 통신 layer(Content-MD5 middleware) + Expiration rule 등록만 검증.
// Transition rule 자체의 직렬화는 backend_s3_enterprise_test.go(fake S3)가 cover.
func TestS3Backend_MinIOLifecycle(t *testing.T) {
	fix := setupMinIO(t)
	cfg := rotation.S3Config{
		Prefix:              "lifecycle/",
		LifecycleEnabled:    true,
		LifecycleExpireDays: 365,
	}
	// LifecycleEnabled=true라 NewS3Backend가 ApplyLifecyclePolicy를 자동 호출.
	// 성공해야 backend 생성 (middleware Content-MD5 헤더 정상 → MinIO 통과).
	b := newMinIOBackend(t, fix, cfg)

	// 명시 재호출도 idempotent — 검증 차원.
	if err := b.ApplyLifecyclePolicy(context.Background()); err != nil {
		t.Errorf("ApplyLifecyclePolicy (idempotent re-apply): %v", err)
	}
}

// TestS3Backend_MinIONotFound — 부재 객체 Exists=false + Get → ErrNotExist.
func TestS3Backend_MinIONotFound(t *testing.T) {
	fix := setupMinIO(t)
	b := newMinIOBackend(t, fix, rotation.S3Config{Prefix: "missing/"})

	uri := fmt.Sprintf("s3://%s/missing/seg-999999.tar.gz", fix.bucket)
	exists, err := b.Exists(context.Background(), uri)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("Exists = true for missing object")
	}

	_, err = b.Get(context.Background(), uri)
	if !errors.Is(err, rotation.ErrNotExist) {
		t.Errorf("Get error = %v, want ErrNotExist", err)
	}
}
