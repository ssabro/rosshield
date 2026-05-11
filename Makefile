GO ?= go
BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/rosshield-server
AUDIT_VERIFY_BIN := $(BIN_DIR)/rosshield-audit-verify

.PHONY: all build build-enterprise audit-verify-build pack-tools-build pack-archive test test-enterprise vet vet-enterprise fmt tidy lint ci ci-enterprise clean openapi web-install web-dev web-build web-test web-types web-e2e web-e2e-install compose-build compose-up compose-down compose-smoke pg-migrate-up pg-migrate-down pg-migrate-status pg-migrate-create

all: ci

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(SERVER_BIN) ./cmd/rosshield-server

# E31 — enterprise build tag (`rosshield_enterprise`).
# D8 1순위 결합 청구항(A-1+B-1+C-1+D-3) 코드 분리 베이스. 출원 잠금(D8-4) 해제 후
# E32에서 실 알고리즘이 internal/enterprise/* 에 채워짐. 현재는 7 placeholder
# 패키지 + EditionTag 상수만.
build-enterprise:
	@mkdir -p $(BIN_DIR)
	$(GO) build -tags rosshield_enterprise -o $(BIN_DIR)/rosshield-server-enterprise ./cmd/rosshield-server

# E30 — 외부 감사인용 standalone 검증 binary (R30-4).
# stdlib + crypto/ed25519만 사용. 외부 의존 0 — release page에 단독 게시 가능.
audit-verify-build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(AUDIT_VERIFY_BIN) ./cmd/rosshield-audit-verify

# E12 — pack-tools (벤치마크 팩 변환·서명 CLI).
pack-tools-build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/pack-tools ./cmd/pack-tools

# E12 — built-in pack archive 빌드.
# packs/<name>/ source → packs/<name>.tar.gz (Ed25519 서명).
# DEV_PACK_SIGNER_KEY는 dev 머신의 raw 64-byte private key 파일.
# 첫 사용: make pack-tools-build && bin/pack-tools keygen -out scripts/dev-pack-signer.key -pub-out scripts/dev-pack-signer.pub.hex
# CI(release)는 별도 secret signer 사용 (별 workflow).
DEV_PACK_SIGNER_KEY ?= scripts/dev-pack-signer.key
PACKS_SOURCE := packs/cis-ubuntu-2404 packs/ros2-jazzy-baseline
PACKS_ARCHIVE := $(addsuffix .tar.gz,$(PACKS_SOURCE))

pack-archive: pack-tools-build
	@test -f $(DEV_PACK_SIGNER_KEY) || { \
		echo "ERROR: $(DEV_PACK_SIGNER_KEY) not found."; \
		echo "  Run: $(BIN_DIR)/pack-tools keygen -out $(DEV_PACK_SIGNER_KEY) -pub-out scripts/dev-pack-signer.pub.hex"; \
		exit 1; \
	}
	@for src in $(PACKS_SOURCE); do \
		echo "Archiving $$src ..."; \
		$(BIN_DIR)/pack-tools archive -input $$src -signer-key $(DEV_PACK_SIGNER_KEY) -output $$src.tar.gz -force || exit 1; \
	done
	@echo "✓ Built $(words $(PACKS_ARCHIVE)) pack archives ($(PACKS_ARCHIVE))"

test:
	$(GO) test -count=1 ./...

test-race:
	$(GO) test -race -count=1 ./...

# E31 — enterprise build tag 적용 테스트 (코어 테스트 + enterprise 표면 sanity).
test-enterprise:
	$(GO) test -tags rosshield_enterprise -count=1 ./...

vet:
	$(GO) vet ./...

# E31 — enterprise build tag 적용 vet.
vet-enterprise:
	$(GO) vet -tags rosshield_enterprise ./...

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
