FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w" \
    -o /out/ws ./cmd/ws

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=builder /out/ws /app/ws
USER app
EXPOSE 8090
ENTRYPOINT ["/app/ws"]
