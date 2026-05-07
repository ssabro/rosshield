// Package postgres는 PostgreSQL(pgx/v5) 어댑터의 storage.Storage 구현입니다.
//
// 본 파일은 PG 마이그레이션을 단일 바이너리에 embed 합니다.
// 어플라이언스·온프렘 오프라인 배포 요구(R1 §4) 충족.
//
// 디렉터리 레이아웃:
//
//	migrations/<sequence>_<name>.up.sql
//	migrations/<sequence>_<name>.down.sql
//
// 형식은 golang-migrate(R20-5 결정) 호환. 후속 stage(전체 0001~0019 변환)에서
// 추가 파일을 동일 패턴으로 채웁니다.
package postgres

import "embed"

// MigrationsFS는 dialect=postgres 마이그레이션을 단일 바이너리에 포함시킵니다.
//
//go:embed migrations
var MigrationsFS embed.FS
