VERSION ?= $(shell git describe --tags --always)
GO_LDFLAGS := -X main.version=$(VERSION)
WEB_DIST := internal/web/dist
GOLANGCI_LINT := $(shell command -v golangci-lint 2>/dev/null || echo $(shell go env GOPATH)/bin/golangci-lint)

.PHONY: all build web dev lint test test-integration image clean

all: build

build: web
	go build -ldflags "$(GO_LDFLAGS)" -o bin/nsl ./cmd/nsl

web: web/node_modules
	cd web && npm run build

web/node_modules: web/package.json web/package-lock.json
	cd web && npm ci
	@touch web/node_modules

dev: web
	@trap 'kill 0' EXIT INT TERM; \
		(cd web && npm run dev) & \
		go run ./cmd/nsl serve & \
		wait

lint: web
	$(GOLANGCI_LINT) run
	cd web && npm run lint && npm run format:check && npm run typecheck

test: web
	go test ./...
	cd web && npm run test

test-integration: web
	go test -tags integration -p 1 ./...

image:
	docker build -t nsl/node images/node

clean:
	rm -rf bin $(WEB_DIST)
