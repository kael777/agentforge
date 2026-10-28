# Base images are pinned by digest, not just by tag: a tag can be moved, a
# digest cannot. Renovate or Dependabot can bump these; a human should not.
FROM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w -X github.com/kael777/agentforge/backend/api/internal/buildinfo.Version=${VERSION} -X github.com/kael777/agentforge/backend/api/internal/buildinfo.Commit=${GIT_COMMIT} -X github.com/kael777/agentforge/backend/api/internal/buildinfo.BuildTime=${BUILD_TIME}" \
    -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.22@sha256:2e0b81d60597f449baa56dc25d355367869d2dca4cd57e866f1e2b52af3eb55f

LABEL org.opencontainers.image.title="agentforge-api" \
      org.opencontainers.image.description="AgentForge HTTP API" \
      org.opencontainers.image.source="https://github.com/kael777/agentforge"

# The container runs as a non-root user with no shell and no home directory.
RUN addgroup -S app && adduser -S -G app -H -s /sbin/nologin app
WORKDIR /app
COPY --from=builder /out/api /app/api
COPY --from=builder /out/migrate /app/migrate

USER app
EXPOSE 55555

# Probes the public liveness endpoint from inside the container, so it proves
# the process can answer and nothing more.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:55555/healthz || exit 1

ENTRYPOINT ["/app/api"]
