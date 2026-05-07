GO ?= go
BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/rosshield-server

.PHONY: all build test vet fmt tidy lint ci clean openapi web-install web-dev web-build web-test web-types web-e2e web-e2e-install compose-build compose-up compose-down compose-smoke pg-migrate-up pg-migrate-down pg-migrate-status pg-migrate-create

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

# C4 — Playwright E2E (smoke).
web-e2e-install:
	cd web && pnpm exec playwright install --with-deps chromium

web-e2e:
	cd web && pnpm exec playwright test

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

# E22-A — PostgreSQL 마이그레이션 (golang-migrate CLI 사용).
# 설치: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
# 사용:
#   make pg-migrate-up   PG_DSN='postgres://user:pass@host:5432/db?sslmode=disable'
#   make pg-migrate-down PG_DSN='postgres://...'
#   make pg-migrate-status PG_DSN='postgres://...'
#   make pg-migrate-create NAME=add_widgets
#
# 주의: PG 마이그레이션 본격 적용은 후속 stage(E22-B 이상). 현재는 0001 만 존재.
PG_MIGRATIONS_DIR := internal/platform/storage/postgres/migrations
PG_DSN ?=

pg-migrate-up:
	@test -n "$(PG_DSN)" || { echo "PG_DSN required (e.g. PG_DSN='postgres://...?sslmode=disable')"; exit 1; }
	migrate -path $(PG_MIGRATIONS_DIR) -database "$(PG_DSN)" up

pg-migrate-down:
	@test -n "$(PG_DSN)" || { echo "PG_DSN required"; exit 1; }
	migrate -path $(PG_MIGRATIONS_DIR) -database "$(PG_DSN)" down 1

pg-migrate-status:
	@test -n "$(PG_DSN)" || { echo "PG_DSN required"; exit 1; }
	migrate -path $(PG_MIGRATIONS_DIR) -database "$(PG_DSN)" version

pg-migrate-create:
	@test -n "$(NAME)" || { echo "NAME required (e.g. NAME=add_widgets)"; exit 1; }
	migrate create -ext sql -dir $(PG_MIGRATIONS_DIR) -seq $(NAME)
