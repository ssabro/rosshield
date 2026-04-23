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

openapi:
	@echo "TODO: OpenAPI 번들 (Step 0.3)"

ci: vet test build

clean:
	rm -rf $(BIN_DIR)
