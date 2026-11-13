# MeetHub — every target runs in Docker; the host needs only Docker (and make).
# The only build target is linux/amd64; outputs go to dist/ (never committed).

VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILDTIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
IMAGE     ?= meethub
PLATFORM  := linux/amd64
DIST      := dist

BUILD_ARGS := --platform $(PLATFORM) \
	--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILDTIME=$(BUILDTIME)

.PHONY: all build image init clean help tools check test test-db fmt

all: build

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

build: ## Build dist/meethub (static linux/amd64, SPA embedded) inside Docker
	docker build $(BUILD_ARGS) --target export --output type=local,dest=$(DIST) .
	@ls -l $(DIST)/meethub

image: ## Build the runtime image $(IMAGE):$(VERSION) (distroless, non-root)
	docker build $(BUILD_ARGS) --target runtime -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

init: ## Create config.toml and .env from the examples when missing
	@test -f config.toml || { cp config.toml.example config.toml && echo "created config.toml"; }
	@test -f .env || { cp .env.example .env && echo "created .env"; }

clean: ## Remove build outputs
	rm -rf $(DIST)
	find web/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +

# ── checks and tests (tools image; repository mounted, caches in a volume) ────
TOOLS_IMAGE := meethub-tools
TOOLS_RUN   := docker run --rm -u $(shell id -u):$(shell id -g) -v $(CURDIR):/src -v meethub-cache:/cache \
	--network $(or $(TOOLS_NETWORK),bridge) $(TOOLS_ENV) $(TOOLS_IMAGE)

tools: ## Build the tools image (Go + gcc, golangci-lint, govulncheck, Node)
	@docker build -q -f docker/tools.Dockerfile -t $(TOOLS_IMAGE) docker >/dev/null
	@docker volume create meethub-cache >/dev/null
	@docker run --rm -v meethub-cache:/cache $(TOOLS_IMAGE) sh -c 'mkdir -p /cache/home && chmod -R 1777 /cache' 

fmt: tools ## Format Go and frontend sources
	$(TOOLS_RUN) sh -c 'golangci-lint fmt ./... && cd frontend && npm ci --no-audit --no-fund >/dev/null && npx vp fmt'

check: tools ## gofmt/goimports, go vet, golangci-lint, govulncheck, frontend lint + type check
	$(TOOLS_RUN) sh -c 'set -e; \
	  test -z "$$(gofmt -l $$(git ls-files "*.go" 2>/dev/null || find . -name "*.go" -not -path "./frontend/*"))" || { gofmt -l .; echo "gofmt: files above need formatting"; exit 1; }; \
	  go vet ./... && golangci-lint run ./... && govulncheck ./...; \
	  cd frontend && npm ci --no-audit --no-fund >/dev/null && npm run check'

COMPOSE_TEST := docker compose -f deployments/compose/compose.test.yml
TEST_NET     := meethub-test_default
REDIS_ENV    := -e MEETHUB_TEST_REDIS_ADDR=redis:6379
DB_ENV       := $(REDIS_ENV) -e MEETHUB_TEST_MARIADB_ADDR=mariadb:3306 -e MEETHUB_TEST_POSTGRES_ADDR=postgres:5432

# with-stack runs $(2) in the tools container on the test network after starting the test services $(1);
# the services are removed afterwards even when the tests fail.
define with-stack
	@$(COMPOSE_TEST) $(1) up -d --wait --quiet-pull >/dev/null; \
	status=0; \
	docker run --rm -u $(shell id -u):$(shell id -g) -v $(CURDIR):/src -v meethub-cache:/cache \
	  --network $(TEST_NET) $(3) $(TOOLS_IMAGE) sh -c '$(2)' || status=$$?; \
	$(COMPOSE_TEST) --profile db down -v --remove-orphans >/dev/null 2>&1; \
	exit $$status
endef

test: tools ## Go tests with -race (with the test Redis) and frontend unit tests
	$(call with-stack,,set -e; go test -race -count=1 ./...; cd frontend && npm ci --no-audit --no-fund >/dev/null && npm test -- --run --passWithNoTests,$(REDIS_ENV))

test-db: tools ## Go tests with -race against SQLite, MariaDB, PostgreSQL and Redis
	$(call with-stack,--profile db,go test -race -count=1 ./...,$(DB_ENV))
