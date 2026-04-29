GO ?= go
BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/rosshield-server

.PHONY: all build test vet fmt tidy lint ci clean openapi

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

clean:
	rm -rf $(BIN_DIR)
