BINARY      := pasteai
MODULE      := github.com/pasteai/pasteai
CMD         := ./cmd/pasteai
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_FLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
GOBIN       := $(shell go env GOPATH)/bin

.PHONY: build install setup run test test-integration test-install test-all lint clean docker-restart release-dry-run help

## help: show this help
help:
	@grep -E '^## [a-z]' $(MAKEFILE_LIST) | sed 's/^## //' | \
		awk -F': ' '{ printf "  %-18s %s\n", $$1, $$2 }'

## build: compile binary to ./pasteai
build:
	go build $(BUILD_FLAGS) -o $(BINARY) $(CMD)

## install: install to GOPATH/bin and check PATH (run this after a fresh clone)
install:
	go mod download
	go install $(BUILD_FLAGS) $(CMD)
	@echo "✓ $(GOBIN)/$(BINARY) $$($(GOBIN)/$(BINARY) version)"
	@if echo ":$$PATH:" | grep -q ":$(GOBIN):"; then \
		echo "✓ $(GOBIN) is in your PATH"; \
	else \
		echo ""; \
		echo "⚠ Add $(GOBIN) to your PATH:"; \
		echo "  bash/zsh: echo 'export PATH=\"\$$PATH:$(GOBIN)\"' >> ~/.bashrc"; \
		echo "  fish:     fish_add_path $(GOBIN)"; \
	fi

## setup: install binary and configure MCP in ~/.claude.json
## setup: PASTEAI_MODE=automatic|manual|remote  PASTEAI_URL=...  PASTEAI_API_KEY=...
setup: install
	@_mode="$(PASTEAI_MODE)"; _url="$(PASTEAI_URL)"; _key="$(PASTEAI_API_KEY)"; \
	set -- "$(GOBIN)/$(BINARY)" setup; \
	[ -n "$$_mode" ] && set -- "$$@" -mode "$$_mode"; \
	[ -n "$$_url"  ] && set -- "$$@" -url "$$_url"; \
	[ -n "$$_key"  ] && set -- "$$@" -api-key "$$_key"; \
	"$$@"

## run: build and start the server on :8080
run: build
	./$(BINARY) serve

## test: run unit tests (fast, no Docker required)
test:
	go test -race -short ./...

## test-integration: run server + MCP integration tests (no Docker required)
test-integration:
	go test -race -timeout 120s ./test/integration/...

## test-install: run install + script tests (requires Docker daemon)
test-install:
	go test -race -timeout 120s ./test/...

## test-all: run all tests
test-all: test test-integration test-install

## lint: run go vet
lint:
	go vet ./...

## clean: remove build artifacts
clean:
	rm -f $(BINARY) coverage.out coverage.html

## docker-restart: rebuild image and restart compose
docker-restart:
	UID=$$(id -u) GID=$$(id -g) docker compose build --no-cache
	UID=$$(id -u) GID=$$(id -g) docker compose up -d

## release-dry-run: simulate a goreleaser release (requires goreleaser)
release-dry-run:
	goreleaser release --snapshot --clean --skip=validate
