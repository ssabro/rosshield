GO ?= go
BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/rosshield-server

.PHONY: all build test vet fmt tidy lint ci clean openapi web-install web-dev web-build web-test web-types compose-build compose-up compose-down compose-smoke

all: ci

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(SERVER_BIN) ./cmd/rosshield-server

test:
	$(GO) test -count=1 ./...

test-race:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -l -w .

tidy:
	$(GO) mod tidy

lint:
	golangci-lint run ./...

OAPI_CODEGEN_VERSION := v2.4.1

openapi:
	@command -v oapi-codegen >/dev/null 2>&1 || { \
		echo "Installing oapi-codegen $(OAPI_CODEGEN_VERSION)..."; \
		$(GO) install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION); \
	}
	cd internal/api && oapi-codegen -config config.yaml ../../openapi/openapi.yaml
	@echo "✓ Generated internal/api/gen/openapi.gen.go"

ci: vet test build

# Web Console (E10) — pnpm 기반.
# 설계: docs/design/09-ui-and-clients.md §9.2.
web-install:
	cd web && pnpm install

web-dev:
	cd web && pnpm dev

web-build:
	cd web && pnpm build

# E10 Stage D — Vitest 단위 테스트 (jsdom + RTL).
web-test:
	cd web && pnpm test

# OpenAPI spec → TS 타입 자동 생성 (Stage B). 결과물은 git 커밋 대상.
web-types:
	cd web && npx openapi-typescript ../openapi/openapi.yaml -o src/api/types.ts
	@echo "✓ Generated web/src/api/types.ts"

# E11 Compose — Docker 온프렘 데모.
# 설계: deploy/compose/README.md.
compose-build:
	cd deploy/compose && docker compose build

compose-up:
	cd deploy/compose && docker compose up -d

compose-down:
	cd deploy/compose && docker compose down -v

compose-smoke:
	bash scripts/compose-smoke.sh

clean:
	rm -rf $(BIN_DIR)
