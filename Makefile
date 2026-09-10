.PHONY: all build test test-layer-contracts clean generate lint deps check-type-domains check-no-legacy check-no-semantics-creep

# Variables
GO := go
GOFLAGS := -p=1
PKG := ./...

all: deps lint test build

# Dependencies
deps:
	$(GO) mod download
	$(GO) mod tidy

# Build
build: build-cmd build-pkg

build-cmd:
	$(GO) build $(GOFLAGS) -o bin/tlrpc-gen ./cmd/tlrpc-gen

build-pkg:
	$(GO) build $(GOFLAGS) $(PKG)

# Testing
test:
	$(GO) test $(GOFLAGS) $(PKG)

# Includes generated-package compilation and the unchanged-initializer fixture.
test-layer-contracts:
	$(GO) test $(GOFLAGS) ./internal/parser ./internal/generator ./cmd/tlrpc-gen . ./mtproto -run 'Layer|Boxed|Contract|MethodResponse|RuntimeSender|EncodeBudget|Expired|Reintroduced' -count=1

test-short:
	$(GO) test $(GOFLAGS) -short $(PKG)

test-integration:
	$(GO) test $(GOFLAGS) -tags=integration ./tests/...

bench:
	$(GO) test $(GOFLAGS) -bench=. -benchmem $(PKG)

coverage:
	$(GO) test $(GOFLAGS) -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -html=coverage.out -o coverage.html

# Code Generation
generate: generate-test generate-examples generate-compat

generate-test:
	$(GO) run ./cmd/tlrpc-gen --schema=testdata/schemas/framework_acceptance.tl --out=internal/testdata/gen --package=gen

generate-examples:
	$(GO) run ./cmd/tlrpc-gen --schema=examples/echo/schema.tl --out=examples/echo/gen

# Minimal Telegram compatibility fixture owned by TLRPC, not tgserver output.
generate-compat:
	$(GO) run ./cmd/tlrpc-gen --schema=testdata/schema-217.tl --out=examples/gen --package=gen --layer=217

# Linting
lint:
	golangci-lint run --concurrency 1 ./...
	$(MAKE) check-type-domains
	$(MAKE) check-no-withtransport
	$(MAKE) check-no-legacy
	$(MAKE) check-no-semantics-creep

check-type-domains:
	./scripts/check_type_domains.sh

check-no-withtransport:
	./scripts/check_no_withtransport.sh

check-no-legacy:
	./scripts/check_no_legacy.sh

check-no-semantics-creep:
	./scripts/check_no_semantics_creep.sh

fmt:
	$(GO) fmt ./...

# Cleaning
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html
	find . -type f -name '*.test' -delete

# Development
run-example: build
	./bin/tlrpc-gen --schema=examples/echo/schema.tl --out=examples/echo/gen
	$(GO) run ./examples/echo

# CI
ci: deps lint test build
