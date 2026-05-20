//go:build rosshield_enterprise

// backend_s3_enterprise_test.go — S3Backend (BSL 1.1 enterprise) 단위 검증.
//
// 본 파일은 build tag `rosshield_enterprise`가 켜진 빌드에서만 컴파일됩니다.
//
// 전략: AWS 호출을 in-memory fake `s3API` 구현으로 대체 (docker/minio 불필요 — CI 친화).
// 실제 AWS endpoint·MinIO 컨테이너 통합은 별 `//go:build rosshield_enterprise && integration`
// 테스트로 분리 가능 (본 round 비목표 — design doc R2).

package rotation

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fakeS3 는 in-memory map 기반 s3API 구현입니다.
//
// 동시 안전 — Put/Get/Head 모두 mu로 보호.
// 동일 (bucket, key) 재 Put은 silent 덮어쓰기 (실 S3 versioning 미모방).
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte // key: "<bucket>/<key>"

	// putErr·getErr·headErr이 nil이 아니면 해당 메서드 호출 시 강제 반환 — 에러 경로 검증.
	putErr  error
	getErr  error
	headErr error

	// lastPut은 마지막 PutObject 입력을 보관 — SSE·Bucket·Key assertion.
	lastPut *s3.PutObjectInput
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string][]byte{}}
}

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return nil, f.putErr
	}
	data, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	key := aws.ToString(in.Bucket) + "/" + aws.ToString(in.Key)
	f.objects[key] = data
	f.lastPut = in
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	key := aws.ToString(in.Bucket) + "/" + aws.ToString(in.Key)
	data, ok := f.objects[key]
	if !ok {
		return nil, &s3types.NoSuchKey{}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(data))}, nil
}

func (f *fakeS3) HeadObject(_ context.Context, in *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.headErr != nil {
		return nil, f.headErr
	}
	key := aws.ToString(in.Bucket) + "/" + aws.ToString(in.Key)
	if _, ok := f.objects[key]; !ok {
		return nil, &s3types.NotFound{}
	}
	return &s3.HeadObjectOutput{}, nil
}

// === construction ===

func TestS3Backend_New_RequiresBucket(t *testing.T) {
	t.Parallel()
	_, err := NewS3Backend(context.Background(), S3Config{Region: "us-east-1"})
	if err == nil || !strings.Contains(err.Error(), "Bucket required") {
		t.Errorf("err = %v, want Bucket required", err)
	}
}

func TestS3Backend_New_RequiresRegion(t *testing.T) {
	t.Parallel()
	_, err := NewS3Backend(context.Background(), S3Config{Bucket: "x"})
	if err == nil || !strings.Contains(err.Error(), "Region required") {
		t.Errorf("err = %v, want Region required", err)
	}
}

func TestS3Backend_Scheme(t *testing.T) {
	t.Parallel()
	b := newS3BackendWithClient(newFakeS3(), S3Config{Bucket: "b", Region: "r"})
	if b.Scheme() != "s3" {
		t.Errorf("Scheme = %q, want s3", b.Scheme())
	}
}

// === Put ===

func TestS3Backend_PutGetExistsRoundTrip(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	b := newS3BackendWithClient(fake, S3Config{Bucket: "audit-bucket", Region: "us-west-2", Prefix: "archives/"})

	uri, err := b.Put(context.Background(), "tn_acme/seg-000001.tar.gz", []byte("payload-body"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	wantURI := "s3://audit-bucket/archives/tn_acme/seg-000001.tar.gz"
	if uri != wantURI {
		t.Errorf("uri = %q, want %q", uri, wantURI)
	}

	exists, err := b.Exists(context.Background(), uri)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Error("Exists = false after Put, want true")
	}

	got, err := b.Get(context.Background(), uri)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "payload-body" {
		t.Errorf("Get = %q, want payload-body", got)
	}
}

func TestS3Backend_Put_RejectsEscapeKey(t *testing.T) {
	t.Parallel()
	b := newS3BackendWithClient(newFakeS3(), S3Config{Bucket: "b", Region: "r"})
	if _, err := b.Put(context.Background(), "../escape.tar.gz", []byte("x")); err == nil {
		t.Error("expected error for ../escape key")
	}
}

func TestS3Backend_Put_RejectsAbsoluteKey(t *testing.T) {
	t.Parallel()
	b := newS3BackendWithClient(newFakeS3(), S3Config{Bucket: "b", Region: "r"})
	if _, err := b.Put(context.Background(), "/abs/key.tar.gz", []byte("x")); err == nil {
		t.Error("expected error for absolute key")
	}
}

func TestS3Backend_Put_RejectsEmptyKey(t *testing.T) {
	t.Parallel()
	b := newS3BackendWithClient(newFakeS3(), S3Config{Bucket: "b", Region: "r"})
	if _, err := b.Put(context.Background(), "", []byte("x")); err == nil {
		t.Error("expected error for empty key")
	}
}

func TestS3Backend_Put_PropagatesAPIError(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	fake.putErr = errors.New("simulated AccessDenied")
	b := newS3BackendWithClient(fake, S3Config{Bucket: "b", Region: "r"})

	_, err := b.Put(context.Background(), "k.tar.gz", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Errorf("err = %v, want wrapped AccessDenied", err)
	}
}

func TestS3Backend_Put_RespectsContextCancel(t *testing.T) {
	t.Parallel()
	b := newS3BackendWithClient(newFakeS3(), S3Config{Bucket: "b", Region: "r"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Put(ctx, "k.tar.gz", []byte("x")); err == nil {
		t.Error("expected ctx cancel error")
	}
}

// === SSE ===

func TestS3Backend_Put_AppliesSSE_AES256(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	b := newS3BackendWithClient(fake, S3Config{
		Bucket: "b", Region: "r",
		ServerSideEncryption: "AES256",
	})

	if _, err := b.Put(context.Background(), "k", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if fake.lastPut == nil {
		t.Fatal("lastPut nil")
	}
	if fake.lastPut.ServerSideEncryption != s3types.ServerSideEncryptionAes256 {
		t.Errorf("SSE = %q, want AES256", fake.lastPut.ServerSideEncryption)
	}
	if aws.ToString(fake.lastPut.SSEKMSKeyId) != "" {
		t.Errorf("SSEKMSKeyId = %q, want empty (AES256 path)", aws.ToString(fake.lastPut.SSEKMSKeyId))
	}
}

func TestS3Backend_Put_AppliesSSE_KMS(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	kmsArn := "arn:aws:kms:us-west-2:123456789012:key/abc-def"
	b := newS3BackendWithClient(fake, S3Config{
		Bucket: "b", Region: "r",
		ServerSideEncryption: "aws:kms",
		KMSKeyID:             kmsArn,
	})

	if _, err := b.Put(context.Background(), "k", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if fake.lastPut.ServerSideEncryption != s3types.ServerSideEncryptionAwsKms {
		t.Errorf("SSE = %q, want aws:kms", fake.lastPut.ServerSideEncryption)
	}
	if aws.ToString(fake.lastPut.SSEKMSKeyId) != kmsArn {
		t.Errorf("SSEKMSKeyId = %q, want %q", aws.ToString(fake.lastPut.SSEKMSKeyId), kmsArn)
	}
}

func TestS3Backend_Put_NoSSE_When_Empty(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	b := newS3BackendWithClient(fake, S3Config{Bucket: "b", Region: "r"})
	if _, err := b.Put(context.Background(), "k", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if fake.lastPut.ServerSideEncryption != "" {
		t.Errorf("SSE = %q, want empty", fake.lastPut.ServerSideEncryption)
	}
}

// === Get / Exists ===

func TestS3Backend_Get_ReturnsErrNotExistForMissingObject(t *testing.T) {
	t.Parallel()
	b := newS3BackendWithClient(newFakeS3(), S3Config{Bucket: "b", Region: "r"})
	_, err := b.Get(context.Background(), "s3://b/missing.tar.gz")
	if !errors.Is(err, ErrNotExist) {
		t.Errorf("err = %v, want ErrNotExist", err)
	}
}

func TestS3Backend_Exists_FalseForMissingObject(t *testing.T) {
	t.Parallel()
	b := newS3BackendWithClient(newFakeS3(), S3Config{Bucket: "b", Region: "r"})
	exists, err := b.Exists(context.Background(), "s3://b/missing.tar.gz")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("Exists = true for missing, want false")
	}
}

func TestS3Backend_Get_RejectsWrongBucket(t *testing.T) {
	t.Parallel()
	b := newS3BackendWithClient(newFakeS3(), S3Config{Bucket: "mine", Region: "r"})
	_, err := b.Get(context.Background(), "s3://other/k.tar.gz")
	if err == nil || !strings.Contains(err.Error(), "bucket") {
		t.Errorf("err = %v, want bucket mismatch", err)
	}
}

func TestS3Backend_Exists_RejectsWrongBucket(t *testing.T) {
	t.Parallel()
	b := newS3BackendWithClient(newFakeS3(), S3Config{Bucket: "mine", Region: "r"})
	_, err := b.Exists(context.Background(), "s3://other/k.tar.gz")
	if err == nil || !strings.Contains(err.Error(), "bucket") {
		t.Errorf("err = %v, want bucket mismatch", err)
	}
}

func TestS3Backend_Get_RejectsWrongScheme(t *testing.T) {
	t.Parallel()
	b := newS3BackendWithClient(newFakeS3(), S3Config{Bucket: "b", Region: "r"})
	_, err := b.Get(context.Background(), "file:///tmp/x")
	if err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Errorf("err = %v, want scheme mismatch", err)
	}
}

func TestS3Backend_Get_RespectsContextCancel(t *testing.T) {
	t.Parallel()
	b := newS3BackendWithClient(newFakeS3(), S3Config{Bucket: "b", Region: "r"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Get(ctx, "s3://b/x"); err == nil {
		t.Error("expected ctx cancel error")
	}
}

// === parseS3URI ===

func TestParseS3URI_HappyPath(t *testing.T) {
	t.Parallel()
	bucket, key, err := parseS3URI("s3://my-bucket/prefix/path/to/object.tar.gz")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if bucket != "my-bucket" {
		t.Errorf("bucket = %q, want my-bucket", bucket)
	}
	if key != "prefix/path/to/object.tar.gz" {
		t.Errorf("key = %q, want prefix/path/to/object.tar.gz", key)
	}
}

func TestParseS3URI_RejectsMissingBucket(t *testing.T) {
	t.Parallel()
	if _, _, err := parseS3URI("s3:///key-without-bucket"); err == nil {
		t.Error("expected error for missing bucket")
	}
}

func TestParseS3URI_RejectsMissingKey(t *testing.T) {
	t.Parallel()
	if _, _, err := parseS3URI("s3://only-bucket"); err == nil {
		t.Error("expected error for missing key")
	}
}

func TestParseS3URI_RejectsWrongScheme(t *testing.T) {
	t.Parallel()
	if _, _, err := parseS3URI("https://example.com/x"); err == nil {
		t.Error("expected error for wrong scheme")
	}
}

// === isS3NotFound ===

func TestIsS3NotFound_Recognizes_NoSuchKey(t *testing.T) {
	t.Parallel()
	if !isS3NotFound(&s3types.NoSuchKey{}) {
		t.Error("NoSuchKey not recognized")
	}
}

func TestIsS3NotFound_Recognizes_NotFound(t *testing.T) {
	t.Parallel()
	if !isS3NotFound(&s3types.NotFound{}) {
		t.Error("NotFound not recognized")
	}
}

func TestIsS3NotFound_RejectsOtherError(t *testing.T) {
	t.Parallel()
	if isS3NotFound(errors.New("AccessDenied")) {
		t.Error("AccessDenied incorrectly recognized as NotFound")
	}
}
