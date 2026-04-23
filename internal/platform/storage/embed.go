package storage

import "embed"

// MigrationsFS는 dialect별 마이그레이션 파일을 단일 바이너리에 포함시킵니다.
// 어플라이언스·온프렘 오프라인 배포 요구(R1 §4) 충족.
//
// 디렉터리 레이아웃:
//
//	migrations/sqlite/<sequence>_<name>.sql
//	migrations/pg/<sequence>_<name>.sql   (Phase 3에서 추가)
//
// sub-package에서 dialect별로 사용하려면 fs.Sub(MigrationsFS, "migrations/sqlite") 등.
//
//go:embed migrations
var MigrationsFS embed.FS
