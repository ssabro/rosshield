//go:build tools

// Package tools tracks Go tool dependencies that are not part of the runtime
// build. The `tools` build tag ensures this file is excluded from regular
// `go build` while `go mod tidy` keeps the imports in `go.mod` so version
// pinning is reproducible.
//
// 설치/실행:
//
//	go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
//	make openapi
package tools

import (
	_ "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"
)
