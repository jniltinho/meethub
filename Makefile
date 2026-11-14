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

.PHONY: all build image init clean help tools check test test-db test-load fmt

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
	$(TOOLS_RUN) sh -c 'golangci-lint fmt ./... && cd frontend; npm ci --no-audit --no-fund >/dev/null; npx vp fmt'

check: tools ## gofmt/goimports, go vet, golangci-lint, govulncheck, frontend lint + type check
	$(TOOLS_RUN) sh -c 'set -e; \
	  test -z "$$(gofmt -l $$(git ls-files "*.go" 2>/dev/null || find . -name "*.go" -not -path "./frontend/*"))" || { gofmt -l .; echo "gofmt: files above need formatting"; exit 1; }; \
	  go vet ./...; golangci-lint run ./...; govulncheck ./...; \
	  cd frontend; npm ci --no-audit --no-fund >/dev/null; npm run check'

COMPOSE_TEST := docker compose -f deployments/compose/compose.test.yml
TEST_NET     := meethub-test_default
REDIS_ENV    := -e MEETHUB_TEST_REDIS_ADDR=redis:6379
DB_ENV       := $(REDIS_ENV) -e MEETHUB_TEST_MARIADB_ADDR=mariadb:3306 -e MEETHUB_TEST_POSTGRES_ADDR=postgres:5432 \
                -e MEETHUB_TEST_S3_ADDR=s3:9000

# with-stack runs $(2) in the tools container on the test network after starting the test services $(1). A failed
# start fails the target; the services are deleted on exit, failure or interruption (trap).
define with-stack
	@trap '$(COMPOSE_TEST) --profile db down -v --remove-orphans >/dev/null 2>&1' EXIT INT TERM; \
	$(COMPOSE_TEST) $(1) up -d --wait --quiet-pull >/dev/null || { echo "the test services did not start"; exit 1; }; \
	docker run --rm -u $(shell id -u):$(shell id -g) -v $(CURDIR):/src -v meethub-cache:/cache \
	  --network $(TEST_NET) $(3) $(TOOLS_IMAGE) sh -c '$(2)'
endef

test: tools ## Go tests with -race (with the test Redis) and frontend unit tests
	$(call with-stack,,set -e; go test -race -count=1 ./...; cd frontend; npm ci --no-audit --no-fund >/dev/null; npm test -- --run --passWithNoTests,$(REDIS_ENV))

test-db: tools ## Go tests with -race against SQLite, MariaDB, PostgreSQL and Redis
	$(call with-stack,--profile db,go test -race -count=1 ./...,$(DB_ENV))

# No -race here: the load test measures latency (design D13) and prints the thresholds it enforces.
test-load: tools ## Load test: 1 000 connections, @channel to 500 members, slow consumers, a Redis restart
	$(call with-stack,,go test -tags load -run TestLoad -count=1 -v -timeout 10m ./cmd,$(REDIS_ENV))

# ── Compose stack ──────────────────────────────────────────────────────────────
DB          ?= sqlite
DBS         := sqlite mariadb postgres
COMPOSE_DIR := deployments/compose
db-file      = $(if $(filter sqlite,$(1)),,-f $(COMPOSE_DIR)/compose.$(1).yml)
COMPOSE      = docker compose --env-file .env -f $(COMPOSE_DIR)/compose.yml $(call db-file,$(DB))

.PHONY: check-db up down logs dev demo test-e2e

check-db:
	@$(if $(filter $(DB),$(DBS)),true,{ echo "DB must be one of: $(DBS) (got '$(DB)')"; exit 1; })

up: check-db init ## Start MeetHub + Redis (DB=sqlite|mariadb|postgres) on http://localhost:8080
	VERSION=$(VERSION) COMMIT=$(COMMIT) BUILDTIME=$(BUILDTIME) $(COMPOSE) up -d --build --wait
	@echo "MeetHub is up: http://localhost:$${MEETHUB_PORT:-8080} (first run: the setup token is in 'make logs')"

down: ## Stop the stack (volumes are kept; add V=1 to delete them)
	$(COMPOSE) down $(if $(V),-v,)

logs: ## Follow the server log
	$(COMPOSE) logs -f meethub

dev: check-db init ## Vite dev server with hot reload on http://localhost:5173 (and the stack behind it)
	$(COMPOSE) -f $(COMPOSE_DIR)/compose.dev.yml up --build

# demo fills the stack of make up with the sample data of the mockup (the Mattermost reference of scripts/reference has
# the same people): on an install without users it first creates the owner ana@example.com. The password is public:
# demo data only, never on a real install.
DEMO_URL      ?= http://localhost:8080
DEMO_PASSWORD ?= Meethub-demo-2026!
PYTHON_IMAGE  := python:3.13-alpine@sha256:9ef1a599f863231c3d91e941389bfdc4a2d6070557243d54df1a4e57015b960e

# the password reaches the recipe through the environment, never as shell source (it may hold quotes)
demo: export MEETHUB_DEMO_PASSWORD := $(DEMO_PASSWORD)
demo: check-db ## Seed the running stack with sample users, teams, channels and conversations (demo data only)
	@test "$$($(COMPOSE) exec -T meethub /meethub user list --json)" != "[]" || \
	  printf '%s\n' "$$MEETHUB_DEMO_PASSWORD" | $(COMPOSE) exec -T meethub /meethub user create \
	    --email ana@example.com --name 'Ana Silva' --role owner --org-name MeetHub --password-stdin
	@docker run --rm --network host -v $(CURDIR)/scripts/demo:/demo:ro -e MEETHUB_DEMO_PASSWORD \
	  -e MEETHUB_DEMO_PICTURES $(PYTHON_IMAGE) python3 -I /demo/seed_chat.py $(DEMO_URL)
	@echo "Log in at $(DEMO_URL) as ana@example.com (owner), bruno@example.com (admin), carla@example.com (member)" \
	  "or diego@example.com (guest) — password: $$MEETHUB_DEMO_PASSWORD"

# test-e2e runs the Playwright suite against a fresh install for each database; containers and volumes are deleted
# afterwards, also on failure. E2E_DBS=sqlite limits the matrix.
E2E_DBS ?= $(DBS)
E2E_COMPOSE = docker compose --env-file .env -f $(COMPOSE_DIR)/compose.yml $(call db-file,$(1)) -f $(COMPOSE_DIR)/compose.e2e.yml

# Two checkouts testing at once need their own image tag and Compose project, or each replaces the other's:
#   COMPOSE_PROJECT_NAME=meethub-e2e-2 make test-e2e E2E_IMAGE=meethub:e2e-2
E2E_IMAGE ?= meethub:e2e
test-e2e: export E2E_IMAGE := $(E2E_IMAGE)
test-e2e: init ## Browser tests (flows, themes, contrast, screenshots, a11y) on SQLite, MariaDB and PostgreSQL
	@mkdir -p frontend/node_modules # else Docker creates the runner's mount point as root and make check cannot write it
	docker build -q $(BUILD_ARGS) --build-arg FRONTEND_MODE=test --target runtime -t $(E2E_IMAGE) . >/dev/null
	@$(foreach db,$(E2E_DBS),( \
	  trap '$(call E2E_COMPOSE,$(db)) --profile runner down -v --remove-orphans >/dev/null 2>&1' EXIT INT TERM; \
	  echo "── e2e on $(db)"; \
	  $(call E2E_COMPOSE,$(db)) up -d --wait >/dev/null || { echo "the stack did not start on $(db)"; exit 1; }; \
	  E2E_UID=$$(id -u) E2E_GID=$$(id -g) $(call E2E_COMPOSE,$(db)) run --rm playwright \
	) || exit $$?;)
