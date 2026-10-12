FROM golang:1.27-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w -X gofiber-gorm-next/internal/buildinfo.Version=${VERSION} -X gofiber-gorm-next/internal/buildinfo.Commit=${GIT_COMMIT} -X gofiber-gorm-next/internal/buildinfo.BuildTime=${BUILD_TIME}" \
    -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/migrate ./cmd/migrate

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=builder /out/api /app/api
COPY --from=builder /out/migrate /app/migrate
USER app
EXPOSE 55555
ENTRYPOINT ["/app/api"]

