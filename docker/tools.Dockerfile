# syntax=docker/dockerfile:1
# Tools image for make check / make test: Go (with gcc for -race), golangci-lint, govulncheck and Node.
ARG GO_IMAGE=golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea
ARG NODE_IMAGE=node:24.16-bookworm-slim@sha256:2c87ef9bd3c6a3bd4b472b4bec2ce9d16354b0c574f736c476489d09f560a203

FROM ${NODE_IMAGE} AS node

FROM ${GO_IMAGE}
ARG GOLANGCI_LINT_VERSION=v2.15.0
ARG GOVULNCHECK_VERSION=v1.9.0
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
 && GOBIN=/usr/local/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION} \
 && GOBIN=/usr/local/bin go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION} \
 && rm -rf /root/.cache /go/pkg/mod \
 && mkdir -p /cache && chmod 1777 /cache
# caches live in the /cache volume so a non-root user (make passes -u $(id -u)) can write them
ENV GOCACHE=/cache/go-build GOMODCACHE=/cache/mod GOLANGCI_LINT_CACHE=/cache/golangci npm_config_cache=/cache/npm npm_config_update_notifier=false \
    HOME=/cache/home CGO_ENABLED=1
WORKDIR /src
