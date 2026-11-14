# syntax=docker/dockerfile:1
# MeetHub — every build runs here; the host needs only Docker. Target: linux/amd64 only.
#   make build  →  docker build --target export --output dist .   (dist/meethub)
#   make image  →  docker build --target runtime -t meethub:<version> .
#   make image-allinone  →  docker build --target allinone -t meethub:<version>-allinone .

ARG NODE_IMAGE=node:24.16-bookworm-slim@sha256:2c87ef9bd3c6a3bd4b472b4bec2ce9d16354b0c574f736c476489d09f560a203
ARG GO_IMAGE=golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
# the Redis image of the Compose stack (deployments/compose/compose.yml): the all-in-one image is built on it
ARG ALLINONE_IMAGE=redis:8.2-alpine@sha256:da23c2478319ceb4903857d77a2fd7aa5137d9e9d4c8165dafbacedac6403e95

# ── frontend: Vue SPA → web/dist ──────────────────────────────────────────────
FROM ${NODE_IMAGE} AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY frontend/ ./
# FRONTEND_MODE=test adds the /__gallery page used by the browser tests (never in release builds)
ARG FRONTEND_MODE=production
RUN npm run build -- --mode ${FRONTEND_MODE}

# ── build: static Go binary with the SPA embedded ─────────────────────────────
FROM ${GO_IMAGE} AS build
RUN apk add --no-cache file
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=frontend /src/web/dist ./web/dist
ARG VERSION=dev
ARG COMMIT=none
ARG BUILDTIME=unknown
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w \
      -X github.com/jniltinho/meethub/internal/buildinfo.Version=${VERSION} \
      -X github.com/jniltinho/meethub/internal/buildinfo.Commit=${COMMIT} \
      -X github.com/jniltinho/meethub/internal/buildinfo.BuildTime=${BUILDTIME}" \
    -o /out/meethub . \
 && file /out/meethub | tee /dev/stderr | grep -q 'ELF 64-bit LSB executable, x86-64.*statically linked' \
 && mkdir -p /out/data /out/etc

# ── export: only the binary, at the stage root (docker build --output dist) ───
FROM scratch AS export
COPY --from=build /out/meethub /meethub

# ── allinone: the server and Redis in one container under supervisord ─────────
# Only with --target allinone (docker/allinone/); the default target stays the minimal runtime below.
FROM ${ALLINONE_IMAGE} AS allinone
LABEL org.opencontainers.image.source="https://github.com/jniltinho/meethub" \
      org.opencontainers.image.description="MeetHub all-in-one: the server and Redis in one container, SQLite on /data" \
      org.opencontainers.image.licenses="MIT"
# the same uid as the runtime image, so a /data volume can move between the two
RUN apk add --no-cache supervisor \
 && addgroup -S -g 65532 meethub \
 && adduser -S -D -H -u 65532 -G meethub -h /data -s /sbin/nologin meethub \
 && chown 65532:65532 /data
COPY --from=build /out/meethub /usr/local/bin/meethub
COPY docker/allinone/supervisord.conf /etc/supervisord.conf
COPY --chmod=755 docker/allinone/entrypoint.sh /usr/local/bin/allinone-entrypoint
# PYTHONWARNINGS: supervisor 4.2 imports the deprecated pkg_resources and would warn on every start and supervisorctl call
ENV MEETHUB_APP_DATA_DIR=/data \
    MEETHUB_HTTP_ADDR=:8080 \
    MEETHUB_REDIS_ADDR=127.0.0.1:6379 \
    PYTHONWARNINGS="ignore:pkg_resources is deprecated"
USER 65532:65532
WORKDIR /data
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 CMD ["meethub", "healthcheck"]
ENTRYPOINT ["allinone-entrypoint"]
CMD []

# ── runtime: distroless, non-root, /data volume ───────────────────────────────
FROM ${RUNTIME_IMAGE} AS runtime
COPY --from=build /out/meethub /meethub
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/etc /etc/meethub
ENV MEETHUB_DATA_DIR=/data \
    MEETHUB_HTTP_ADDR=:8080
USER 65532:65532
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 CMD ["/meethub", "healthcheck"]
ENTRYPOINT ["/meethub"]
CMD ["server"]
