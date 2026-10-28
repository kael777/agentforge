# Base images are pinned by digest, not just by tag: a tag can be moved, a
# digest cannot.
FROM golang:1.26-alpine@sha256:a2012b4a1d1f9f7e9f44e0d39b805a901e856b1dd80b4e6ee4338db547050308 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w" \
    -o /out/ws ./cmd/ws

FROM alpine:3.22@sha256:2e0b81d60597f449baa56dc25d355367869d2dca4cd57e866f1e2b52af3eb55f

LABEL org.opencontainers.image.title="agentforge-ws" \
      org.opencontainers.image.description="AgentForge realtime WebSocket service" \
      org.opencontainers.image.source="https://github.com/kael777/agentforge"

RUN addgroup -S app && adduser -S -G app -H -s /sbin/nologin app
WORKDIR /app
COPY --from=builder /out/ws /app/ws

USER app
EXPOSE 8090

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8090/healthz || exit 1

ENTRYPOINT ["/app/ws"]
