package rotation

import (
	"context"
	"errors"
)

// ErrNotExist는 Get/Exists에서 archive 객체가 없을 때 반환됩니다.
var ErrNotExist = errors.New("rotation: archive object does not exist")

// Backend는 cold archive 저장소 추상화입니다.
//
// 구현체:
//   - FileBackend: 로컬 filesystem (Apache-2.0, default).
//   - S3Backend:   AWS SDK v2 (BSL 1.1 enterprise, build tag 또는 별 모듈).
//
// 모든 메서드는 context cancellation을 존중해야 합니다.
type Backend interface {
	// Put은 data를 key 경로에 저장하고 backend-specific URI를 반환합니다.
	// 같은 key에 재 Put은 idempotent (덮어쓰기 또는 sha256 비교 후 skip — 구현 선택).
	Put(ctx context.Context, key string, data []byte) (uri string, err error)

	// Get은 URI로 archive 본문을 받습니다.
	// URI가 backend scheme과 일치하지 않으면 error.
	// 객체 없으면 ErrNotExist.
	Get(ctx context.Context, uri string) ([]byte, error)

	// Exists는 URI 객체 존재 여부를 반환합니다.
	// 존재 안 함은 (false, nil) — error는 backend 자체 실패 시만.
	Exists(ctx context.Context, uri string) (bool, error)

	// Scheme는 backend가 처리하는 URI scheme (`file`, `s3`).
	Scheme() string
}
